// Package interp — древовидный интерпретатор KonLang (Linux/Windows/macOS).
package interp

import (
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/konlang/konlang/internal/ast"
	"github.com/konlang/konlang/internal/lexer"
	"github.com/konlang/konlang/internal/parser"
	"github.com/konlang/konlang/internal/term"
	"github.com/konlang/konlang/internal/value"
)

// Error — ошибка времени выполнения с позицией в исходнике.
type Error struct {
	Pos lexer.Pos
	Msg string
}

func (e *Error) Error() string {
	if e.Pos.Line == 0 {
		return "ошибка: " + e.Msg
	}
	return fmt.Sprintf("ошибка %s: %s", e.Pos, e.Msg)
}

// управляющие сигналы
type sigBreak struct{}
type sigReturn struct{ v value.Value }
type sigExit struct{ code int }

func (sigBreak) Error() string  { return "sc" }
func (sigReturn) Error() string { return "ret" }
func (s sigExit) Error() string { return fmt.Sprintf("cexit %d", s.code) }

// Options — настройки интерпретатора.
type Options struct {
	Screen term.Screen
	Dir    string // базовая папка для файлов и импортов
	Seed   int64  // 0 — случайный
	Trace  bool
}

// Interp — состояние программы.
type Interp struct {
	vars    map[string]value.Value
	funcs   map[string]*ast.Node
	libs    map[string]bool
	scope   map[string]value.Value // локальные параметры функции (nil вне функции)
	imports []string               // стек импортируемых библиотек
	screen  term.Screen
	rng     *rand.Rand
	dir     string
	depth   int
	cast    string
	trace   bool
	steps   int64
}

// New создаёт интерпретатор.
func New(o Options) *Interp {
	seed := o.Seed
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	dir := o.Dir
	if dir == "" {
		dir, _ = os.Getwd()
	}
	return &Interp{
		vars:   map[string]value.Value{},
		funcs:  map[string]*ast.Node{},
		libs:   map[string]bool{},
		screen: o.Screen,
		rng:    rand.New(rand.NewSource(seed)),
		dir:    dir,
		trace:  o.Trace,
	}
}

// Vars даёт доступ к переменным (для тестов и REPL).
func (in *Interp) Vars() map[string]value.Value { return in.vars }

// ExitCode хранит код завершения после Run.
var _ = io.EOF

// Run выполняет программу. Возвращает код завершения.
func (in *Interp) Run(prog *ast.Node) (int, error) {
	err := in.execBlock(prog)
	if err != nil {
		switch s := err.(type) {
		case sigExit:
			return s.code, nil
		case sigBreak:
			return 1, &Error{Msg: "sc вне цикла"}
		case sigReturn:
			return 0, nil
		}
		return 1, err
	}
	return 0, nil
}

func (in *Interp) errAt(pos lexer.Pos, format string, a ...any) error {
	return &Error{Pos: pos, Msg: fmt.Sprintf(format, a...)}
}

func (in *Interp) execBlock(b *ast.Node) error {
	if b == nil {
		return nil
	}
	for _, st := range b.Kids {
		if err := in.execStmt(st); err != nil {
			return err
		}
	}
	return nil
}

func (in *Interp) execStmt(n *ast.Node) error {
	in.steps++
	if in.trace {
		fmt.Fprintf(os.Stderr, "[trace] %s %s @%s\n", n.Kind, n.Name, n.Pos)
	}
	switch n.Kind {
	case ast.Kon:
		in.vars[n.Name] = value.CodeV(&value.Container{Name: n.Name, Body: n.Kids[0]})
		return nil

	case ast.Def:
		in.funcs[n.Name] = n
		return nil

	case ast.Print:
		kids := n.Kids
		if n.Flag == 1 && len(kids) >= 3 {
			x, err := in.eval(kids[0])
			if err != nil {
				return err
			}
			y, err := in.eval(kids[1])
			if err != nil {
				return err
			}
			xi, _ := x.Int64()
			yi, _ := y.Int64()
			text, err := in.concat(kids[2:])
			if err != nil {
				return err
			}
			in.screen.PrintAt(int(xi), int(yi), text)
		} else {
			text, err := in.concat(kids)
			if err != nil {
				return err
			}
			in.screen.Print(text)
		}
		in.screen.Flush()
		return nil

	case ast.Input:
		prompt, err := in.eval(n.Kids[0])
		if err != nil {
			return err
		}
		limit := 0
		if len(n.Kids) > 1 {
			lv, err := in.eval(n.Kids[1])
			if err != nil {
				return err
			}
			li, _ := lv.Int64()
			limit = int(li)
		}
		for {
			line, err := in.screen.ReadLine(prompt.String(), limit)
			if err != nil {
				if err == io.EOF {
					return sigExit{0}
				}
				if err == term.ErrInterrupt {
					return sigExit{130}
				}
				return in.errAt(n.Pos, "ввод: %v", err)
			}
			v, ok := convertInput(line, n.Str)
			if ok {
				in.setVar(n.Name, v)
				break
			}
			in.screen.Print("Ожидается " + typeName(n.Str) + ", попробуйте ещё раз.")
			in.screen.Flush()
		}
		return nil

	case ast.Key:
		k, err := in.screen.ReadKey()
		if err != nil {
			if err == io.EOF {
				return sigExit{0}
			}
			return in.errAt(n.Pos, "чтение клавиши: %v", err)
		}
		if k == "ctrl+\\" {
			return sigExit{130}
		}
		in.setVar(n.Name, value.StrV(k))
		return nil

	case ast.Clear:
		in.screen.Clear()
		in.screen.Flush()
		return nil

	case ast.Random:
		if len(n.Kids) == 0 {
			return in.errAt(n.Pos, "random_%s: пустой список вариантов", n.Name)
		}
		vals := make([]value.Value, len(n.Kids))
		for i, k := range n.Kids {
			v, err := in.eval(k)
			if err != nil {
				return err
			}
			vals[i] = v
		}
		in.setVar(n.Name, vals[in.rng.Intn(len(vals))])
		return nil

	case ast.Set:
		v, err := in.eval(n.Kids[0])
		if err != nil {
			return err
		}
		in.setVar(n.Name, v)
		return nil

	case ast.Exec:
		v, err := in.eval(n.Kids[0])
		if err != nil {
			return err
		}
		return in.execValue(v, n.Pos)

	case ast.Cycle:
		return in.execCycle(n)

	case ast.If:
		for i := 0; i+1 < len(n.Kids); i += 2 {
			c, err := in.eval(n.Kids[i])
			if err != nil {
				return err
			}
			if c.Truth() {
				return in.execBlock(n.Kids[i+1])
			}
		}
		if len(n.Aux) > 0 {
			return in.execBlock(n.Aux[0])
		}
		return nil

	case ast.Ret:
		if len(n.Kids) > 0 {
			v, err := in.eval(n.Kids[0])
			if err != nil {
				return err
			}
			return sigReturn{v}
		}
		return sigReturn{value.NilV()}

	case ast.Break:
		return sigBreak{}

	case ast.Exit:
		return sigExit{0}

	case ast.Time:
		v, err := in.eval(n.Kids[0])
		if err != nil {
			return err
		}
		units, _ := v.Int64()
		if units > 0 {
			time.Sleep(time.Duration(units) * 10 * time.Millisecond)
		}
		return nil

	case ast.Import:
		return in.importLib(n)

	case ast.ScrSize:
		w, h := in.screen.Size()
		in.vars["scr_w"] = value.IntV(int64(w))
		in.vars["scr_h"] = value.IntV(int64(h))
		return nil

	case ast.ScrCur:
		x, y := in.screen.Cursor()
		in.vars["cur_x"] = value.IntV(int64(x))
		in.vars["cur_y"] = value.IntV(int64(y))
		return nil

	case ast.ScrBg:
		v, err := in.eval(n.Kids[0])
		if err != nil {
			return err
		}
		c := term.ColorCode(v.String())
		if c < 0 {
			return in.errAt(n.Pos, "неизвестный цвет %q", v.String())
		}
		in.screen.SetBg(c)
		in.screen.Flush()
		return nil

	case ast.Beep:
		v, err := in.eval(n.Kids[0])
		if err != nil {
			return err
		}
		f, _ := v.Int64()
		in.screen.Beep(int(f))
		return nil

	case ast.FileOp:
		_, err := in.fileOp(n)
		return err

	case ast.ExprStmt:
		_, err := in.eval(n.Kids[0])
		return err
	}
	return in.errAt(n.Pos, "неизвестная конструкция %s", n.Kind)
}

func typeName(t string) string {
	switch t {
	case "int":
		return "целое число"
	case "float", "double":
		return "дробное число"
	case "char":
		return "один символ"
	}
	return "значение"
}

func convertInput(line, typ string) (value.Value, bool) {
	switch typ {
	case "int":
		s := strings.TrimSpace(line)
		i, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return value.NilV(), false
		}
		return value.IntV(i), true
	case "float", "double":
		s := strings.ReplaceAll(strings.TrimSpace(line), ",", ".")
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return value.NilV(), false
		}
		return value.FloatV(f), true
	case "char":
		r := []rune(line)
		if len(r) == 0 {
			return value.NilV(), false
		}
		return value.StrV(string(r[0])), true
	}
	return value.StrV(line), true
}

func (in *Interp) execCycle(n *ast.Node) error {
	var count int64 = -1
	if n.Kids[0] != nil {
		v, err := in.eval(n.Kids[0])
		if err != nil {
			return err
		}
		c, ok := v.Int64()
		if !ok {
			return in.errAt(n.Pos, "cycle: счётчик должен быть числом, получено %q", v.String())
		}
		count = c
	}
	for i := int64(0); count < 0 || i < count; i++ {
		err := in.execBlock(n.Kids[1])
		if err != nil {
			if _, ok := err.(sigBreak); ok {
				return nil
			}
			return err
		}
	}
	return nil
}

func (in *Interp) execValue(v value.Value, pos lexer.Pos) error {
	switch v.Kind {
	case value.Code:
		if v.C == nil || v.C.Body == nil {
			return in.errAt(pos, "пустой контейнер")
		}
		err := in.execBlock(v.C.Body)
		if _, ok := err.(sigReturn); ok {
			return nil
		}
		return err
	case value.Str:
		prog, err := parser.Parse(v.S)
		if err != nil {
			return in.errAt(pos, "exec: содержимое не является корректным блоком кода: %v", err)
		}
		err = in.execBlock(prog)
		if _, ok := err.(sigReturn); ok {
			return nil
		}
		return err
	}
	return in.errAt(pos, "exec: ожидается контейнер kon_X, получено %s", v.Kind)
}

func (in *Interp) importLib(n *ast.Node) error {
	name := n.Name
	switch name {
	case "style_text":
		in.libs[name] = true
		return nil
	}
	for _, active := range in.imports {
		if active == name {
			return in.errAt(n.Pos, "рекурсивный импорт библиотеки %q", name)
		}
	}
	// библиотека-файл: <имя>.kn рядом с программой
	path := filepath.Join(in.dir, name+".kn")
	if _, err := os.Stat(path); err == nil {
		prog, err := parser.ParseFile(path)
		if err != nil {
			return in.errAt(n.Pos, "библиотека %s: %v", name, err)
		}
		in.imports = append(in.imports, name)
		err = in.execBlock(prog)
		in.imports = in.imports[:len(in.imports)-1]
		if err != nil {
			return err
		}
		in.libs[name] = true
		return nil
	}
	return in.errAt(n.Pos, "неизвестная библиотека %q (нет встроенной и файла %s.kn)", name, name)
}

// ---------- переменные ----------

func (in *Interp) setVar(name string, v value.Value) {
	if in.scope != nil {
		if _, ok := in.scope[name]; ok {
			in.scope[name] = v
			return
		}
	}
	in.vars[name] = v
}

func (in *Interp) lookup(name string) (value.Value, bool) {
	if in.scope != nil {
		if v, ok := in.scope[name]; ok {
			return v, true
		}
	}
	v, ok := in.vars[name]
	return v, ok
}

// ---------- выражения ----------

func (in *Interp) concat(kids []*ast.Node) (string, error) {
	var sb strings.Builder
	for _, k := range kids {
		v, err := in.eval(k)
		if err != nil {
			return "", err
		}
		sb.WriteString(v.String())
	}
	return sb.String(), nil
}

func (in *Interp) eval(n *ast.Node) (value.Value, error) {
	if n == nil {
		return value.NilV(), nil
	}
	switch n.Kind {
	case ast.Str:
		return value.StrV(n.Str), nil
	case ast.Num:
		if n.Flag == 1 {
			return value.IntV(int64(n.Num)), nil
		}
		return value.FloatV(n.Num), nil
	case ast.Ident:
		if v, ok := in.lookup(n.Name); ok {
			return v, nil
		}
		// «голое слово» — строка (например, элементы списка)
		return value.StrV(n.Name), nil
	case ast.Concat:
		s, err := in.concat(n.Kids)
		if err != nil {
			return value.NilV(), err
		}
		return value.StrV(s), nil
	case ast.Ref:
		return in.evalRef(n.Kids[0])
	case ast.Call:
		return in.call(n)
	case ast.List:
		items := make([]value.Value, len(n.Kids))
		for i, k := range n.Kids {
			v, err := in.eval(k)
			if err != nil {
				return value.NilV(), err
			}
			items[i] = v
		}
		return value.ListV(items), nil
	case ast.Repeat:
		s, err := in.eval(n.Kids[0])
		if err != nil {
			return value.NilV(), err
		}
		c, err := in.eval(n.Kids[1])
		if err != nil {
			return value.NilV(), err
		}
		cnt, ok := c.Int64()
		if !ok {
			return value.NilV(), in.errAt(n.Pos, "повтор {x*n}: n должно быть числом")
		}
		return value.StrV(repeatStr(s.String(), cnt)), nil
	case ast.Index:
		return in.evalIndex(n)
	case ast.Unary:
		v, err := in.eval(n.Kids[0])
		if err != nil {
			return value.NilV(), err
		}
		if v.Kind == value.Int {
			return value.IntV(-v.I), nil
		}
		f, ok := v.Float64()
		if !ok {
			return value.NilV(), in.errAt(n.Pos, "унарный минус к нечисловому значению %q", v.String())
		}
		return value.FloatV(-f), nil
	case ast.Binary:
		return in.evalBinary(n)
	}
	return value.NilV(), in.errAt(n.Pos, "неизвестное выражение %s", n.Kind)
}

func repeatStr(s string, n int64) string {
	if n <= 0 || s == "" {
		return ""
	}
	if n > 100000 {
		n = 100000
	}
	return strings.Repeat(s, int(n))
}

func (in *Interp) evalRef(n *ast.Node) (value.Value, error) {
	switch n.Kind {
	case ast.Ident:
		if v, ok := in.lookup(n.Name); ok {
			return v, nil
		}
		if isRegister(n.Name) {
			return value.NilV(), nil // ещё не заполненный регистр
		}
		return value.NilV(), in.errAt(n.Pos, "неизвестный регистр или переменная [%s]", n.Name)
	default:
		return in.eval(n)
	}
}

func isRegister(name string) bool {
	for _, p := range []string{"inp_", "rand_", "key_", "kon_", "scr_", "cur_"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

func (in *Interp) evalIndex(n *ast.Node) (value.Value, error) {
	base, err := in.eval(n.Kids[0])
	if err != nil {
		return value.NilV(), err
	}
	idx, err := in.eval(n.Kids[1])
	if err != nil {
		return value.NilV(), err
	}
	i, ok := idx.Int64()
	if !ok {
		return value.NilV(), in.errAt(n.Pos, "индекс списка должен быть числом")
	}
	switch base.Kind {
	case value.List:
		if i < 0 || i >= int64(len(base.L)) {
			return value.NilV(), in.errAt(n.Pos, "индекс %d вне списка (длина %d)", i, len(base.L))
		}
		return base.L[i], nil
	case value.Str:
		r := []rune(base.S)
		if i < 0 || i >= int64(len(r)) {
			return value.NilV(), in.errAt(n.Pos, "индекс %d вне строки (длина %d)", i, len(r))
		}
		return value.StrV(string(r[i])), nil
	}
	return value.NilV(), in.errAt(n.Pos, "нельзя взять индекс у значения типа %s", base.Kind)
}

func (in *Interp) evalBinary(n *ast.Node) (value.Value, error) {
	op := n.Name
	// ленивые логические операции
	if op == "and" || op == "or" {
		a, err := in.eval(n.Kids[0])
		if err != nil {
			return value.NilV(), err
		}
		if op == "and" && !a.Truth() {
			return value.IntV(0), nil
		}
		if op == "or" && a.Truth() {
			return value.IntV(1), nil
		}
		b, err := in.eval(n.Kids[1])
		if err != nil {
			return value.NilV(), err
		}
		return boolVal(b.Truth()), nil
	}
	a, err := in.eval(n.Kids[0])
	if err != nil {
		return value.NilV(), err
	}
	b, err := in.eval(n.Kids[1])
	if err != nil {
		return value.NilV(), err
	}
	switch op {
	case "==":
		return boolVal(value.Equal(a, b)), nil
	case "!=":
		return boolVal(!value.Equal(a, b)), nil
	case "<":
		return boolVal(value.Compare(a, b) < 0), nil
	case ">":
		return boolVal(value.Compare(a, b) > 0), nil
	case "<=":
		return boolVal(value.Compare(a, b) <= 0), nil
	case ">=":
		return boolVal(value.Compare(a, b) >= 0), nil
	case "+":
		if a.Kind == value.Str || b.Kind == value.Str {
			if a.IsNum() && b.IsNum() {
				break
			}
			return value.StrV(a.String() + b.String()), nil
		}
	case "*":
		if a.Kind == value.Str && b.IsNum() {
			cnt, _ := b.Int64()
			return value.StrV(repeatStr(a.S, cnt)), nil
		}
		if b.Kind == value.Str && a.IsNum() {
			cnt, _ := a.Int64()
			return value.StrV(repeatStr(b.S, cnt)), nil
		}
	}
	af, aok := a.Float64()
	bf, bok := b.Float64()
	if !aok || !bok {
		return value.NilV(), in.errAt(n.Pos, "операция %q требует чисел, получено %q и %q", op, a.String(), b.String())
	}
	bothInt := a.Kind == value.Int && b.Kind == value.Int
	switch op {
	case "+":
		if bothInt {
			return value.IntV(a.I + b.I), nil
		}
		return value.FloatV(af + bf), nil
	case "-":
		if bothInt {
			return value.IntV(a.I - b.I), nil
		}
		return value.FloatV(af - bf), nil
	case "*":
		if bothInt {
			return value.IntV(a.I * b.I), nil
		}
		return value.FloatV(af * bf), nil
	case "/":
		if bf == 0 {
			return value.NilV(), in.errAt(n.Pos, "деление на ноль")
		}
		switch in.cast {
		case "int":
			return value.IntV(int64(af / bf)), nil
		case "float", "double":
			return value.FloatV(af / bf), nil
		}
		if bothInt && a.I%b.I == 0 {
			return value.IntV(a.I / b.I), nil
		}
		return value.FloatV(af / bf), nil
	case "%":
		bi, _ := b.Int64()
		if bi == 0 {
			return value.NilV(), in.errAt(n.Pos, "остаток от деления на ноль")
		}
		ai, _ := a.Int64()
		return value.IntV(ai % bi), nil
	}
	return value.NilV(), in.errAt(n.Pos, "неизвестный оператор %q", op)
}

func boolVal(b bool) value.Value {
	if b {
		return value.IntV(1)
	}
	return value.IntV(0)
}
