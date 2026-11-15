package vm

import (
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/konlang/konlang/internal/term"
	"github.com/konlang/konlang/internal/value"
)

func (m *VM) opPushK(at int) error {
	idx, err := m.readWord()
	if err != nil {
		return err
	}
	c, ok := m.prog.Constant(int(idx))
	if !ok {
		return m.fail(at, "константа #%d не найдена", idx)
	}
	switch c.Kind {
	case 1:
		m.push(value.IntV(int64(c.Int)))
	case 2:
		m.push(value.FloatV(c.Float))
	case 3:
		m.push(value.StrV(c.Str))
	default:
		return m.fail(at, "неизвестный тип константы %d", c.Kind)
	}
	return nil
}

func (m *VM) opIndex(at int) error {
	idx, err := m.pop()
	if err != nil {
		return err
	}
	base, err := m.pop()
	if err != nil {
		return err
	}
	i, ok := idx.Int64()
	if !ok {
		return m.fail(at, "индекс списка должен быть числом")
	}
	switch base.Kind {
	case value.List:
		if i < 0 || i >= int64(len(base.L)) {
			return m.fail(at, "индекс %d вне списка (длина %d)", i, len(base.L))
		}
		m.push(base.L[i])
		return nil
	case value.Str:
		r := []rune(base.S)
		if i < 0 || i >= int64(len(r)) {
			return m.fail(at, "индекс %d вне строки (длина %d)", i, len(r))
		}
		m.push(value.StrV(string(r[i])))
		return nil
	}
	return m.fail(at, "нельзя взять индекс у значения типа %s", base.Kind)
}

func (m *VM) opArith(at int, op string) error {
	b, err := m.pop()
	if err != nil {
		return err
	}
	a, err := m.pop()
	if err != nil {
		return err
	}
	v, err := m.arith(a, b, op, at)
	if err != nil {
		return err
	}
	m.push(v)
	return nil
}

func (m *VM) arith(a, b value.Value, op string, at int) (value.Value, error) {
	switch op {
	case "+":
		if (a.Kind == value.Str || b.Kind == value.Str) && !(a.IsNum() && b.IsNum()) {
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
		name := op
		if op == "/int" || op == "/float" {
			name = "/"
		}
		return value.NilV(), m.fail(at, "операция %q требует чисел, получено %q и %q", name, a.String(), b.String())
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
			return value.NilV(), m.fail(at, "деление на ноль")
		}
		if bothInt && a.I%b.I == 0 {
			return value.IntV(a.I / b.I), nil
		}
		return value.FloatV(af / bf), nil
	case "/int":
		if bf == 0 {
			return value.NilV(), m.fail(at, "деление на ноль")
		}
		return value.IntV(int64(af / bf)), nil
	case "/float":
		if bf == 0 {
			return value.NilV(), m.fail(at, "деление на ноль")
		}
		return value.FloatV(af / bf), nil
	case "%":
		bi, _ := b.Int64()
		if bi == 0 {
			return value.NilV(), m.fail(at, "остаток от деления на ноль")
		}
		ai, _ := a.Int64()
		return value.IntV(ai % bi), nil
	}
	return value.NilV(), m.fail(at, "неизвестный оператор %q", op)
}

func (m *VM) opNeg(at int) error {
	v, err := m.pop()
	if err != nil {
		return err
	}
	if v.Kind == value.Int {
		m.push(value.IntV(-v.I))
		return nil
	}
	f, ok := v.Float64()
	if !ok {
		return m.fail(at, "унарный минус к нечисловому значению %q", v.String())
	}
	m.push(value.FloatV(-f))
	return nil
}

func (m *VM) opCompare(at int, op string) error {
	b, err := m.pop()
	if err != nil {
		return err
	}
	a, err := m.pop()
	if err != nil {
		return err
	}
	var res bool
	switch op {
	case "==":
		res = value.Equal(a, b)
	case "!=":
		res = !value.Equal(a, b)
	case "<":
		res = value.Compare(a, b) < 0
	case "<=":
		res = value.Compare(a, b) <= 0
	case ">":
		res = value.Compare(a, b) > 0
	case ">=":
		res = value.Compare(a, b) >= 0
	}
	m.push(boolVal(res))
	return nil
}

func (m *VM) opLogic(at int, op string) error {
	b, err := m.pop()
	if err != nil {
		return err
	}
	a, err := m.pop()
	if err != nil {
		return err
	}
	if op == "and" {
		m.push(boolVal(a.Truth() && b.Truth()))
	} else {
		m.push(boolVal(a.Truth() || b.Truth()))
	}
	return nil
}

func (m *VM) opPrint(n int, xy bool, at int) error {
	vals, err := m.popN(n)
	if err != nil {
		return err
	}
	if xy {
		if n < 3 {
			return m.fail(at, "print с координатами: нужно x, y и текст")
		}
		x, _ := vals[0].Int64()
		y, _ := vals[1].Int64()
		m.screen.PrintAt(int(x), int(y), joinValues(vals[2:]))
	} else {
		m.screen.Print(joinValues(vals))
	}
	m.screen.Flush()
	return nil
}

func (m *VM) opInput(at int) error {
	name, err := m.readName()
	if err != nil {
		return err
	}
	typ, err := m.readByte()
	if err != nil {
		return err
	}
	limitV, err := m.pop()
	if err != nil {
		return err
	}
	prompt, err := m.pop()
	if err != nil {
		return err
	}
	limit := 0
	if li, ok := limitV.Int64(); ok {
		limit = int(li)
	}
	for {
		line, err := m.screen.ReadLine(prompt.String(), limit)
		if err != nil {
			if err == io.EOF {
				m.halted = true
				m.exit = 0
				return nil
			}
			if err == term.ErrInterrupt {
				m.halted = true
				m.exit = 130
				return nil
			}
			return m.fail(at, "ввод: %v", err)
		}
		v, ok := convertInput(line, inputTypeName(typ))
		if ok {
			m.setVar(name, v)
			return nil
		}
		m.screen.Print("Ожидается " + inputTypeLabel(typ) + ", попробуйте ещё раз.")
		m.screen.Flush()
	}
}

func (m *VM) opKey(at int) error {
	name, err := m.readName()
	if err != nil {
		return err
	}
	k, err := m.screen.ReadKey()
	if err != nil {
		if err == io.EOF {
			m.halted = true
			m.exit = 0
			return nil
		}
		return m.fail(at, "чтение клавиши: %v", err)
	}
	if k == "ctrl+\\" {
		m.halted = true
		m.exit = 130
		return nil
	}
	m.setVar(name, value.StrV(k))
	return nil
}

func (m *VM) opScrBg(at int) error {
	v, err := m.pop()
	if err != nil {
		return err
	}
	c := term.ColorCode(v.String())
	if c < 0 {
		return m.fail(at, "неизвестный цвет %q", v.String())
	}
	m.screen.SetBg(c)
	m.screen.Flush()
	return nil
}

func (m *VM) opRandom(at int) error {
	name, err := m.readName()
	if err != nil {
		return err
	}
	n, err := m.readWord()
	if err != nil {
		return err
	}
	if n == 0 {
		return m.fail(at, "random_%s: пустой список вариантов", name)
	}
	vals, err := m.popN(int(n))
	if err != nil {
		return err
	}
	m.setVar(name, vals[m.rng.Intn(len(vals))])
	return nil
}

func (m *VM) opStyle(at int) error {
	codeV, err := m.pop()
	if err != nil {
		return err
	}
	text, err := m.pop()
	if err != nil {
		return err
	}
	code, ok := codeV.Int64()
	if !ok {
		return m.fail(at, "код стиля должен быть числом")
	}
	m.push(value.StrV("\x1b[" + strconv.FormatInt(code, 10) + "m" + text.String() + "\x1b[0m"))
	return nil
}

func (m *VM) opColor(at int) error {
	v, err := m.pop()
	if err != nil {
		return err
	}
	c := term.ColorCode(v.String())
	if c < 0 {
		return m.fail(at, "неизвестный цвет %q", v.String())
	}
	code := 30 + c
	if c >= 8 {
		code = 90 + c - 8
	}
	m.push(value.IntV(int64(code)))
	return nil
}

func (m *VM) opStyleName(at int) error {
	v, err := m.pop()
	if err != nil {
		return err
	}
	key := strings.ToUpper(strings.TrimSpace(v.String()))
	code, ok := term.StyleNames[key]
	if !ok {
		return m.fail(at, "неизвестный стиль %q", v.String())
	}
	n, _ := strconv.Atoi(code)
	m.push(value.IntV(int64(n)))
	return nil
}

// ---------- преобразования и значения ----------

func (m *VM) cast(v value.Value, typ byte, at int) (value.Value, error) {
	switch typ {
	case 0:
		i, ok := v.Int64()
		if !ok {
			return value.NilV(), m.fail(at, "int: %q не число", v.String())
		}
		return value.IntV(i), nil
	case 1:
		f, ok := v.Float64()
		if !ok {
			return value.NilV(), m.fail(at, "float: %q не число", v.String())
		}
		return value.FloatV(f), nil
	case 2:
		r := []rune(v.String())
		if len(r) == 0 {
			return value.StrV(""), nil
		}
		return value.StrV(string(r[0])), nil
	case 3:
		return value.StrV(v.String()), nil
	}
	return v, nil
}

func valueLen(v value.Value) int {
	switch v.Kind {
	case value.List:
		return len(v.L)
	case value.Str:
		return term.VisibleLen(v.S)
	case value.Nil:
		return 0
	}
	return len([]rune(v.String()))
}

func joinValues(vals []value.Value) string {
	var sb strings.Builder
	for _, v := range vals {
		sb.WriteString(v.String())
	}
	return sb.String()
}

func boolVal(b bool) value.Value {
	if b {
		return value.IntV(1)
	}
	return value.IntV(0)
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

func inputTypeName(code byte) string {
	switch code {
	case 0:
		return "int"
	case 1:
		return "float"
	case 2:
		return "char"
	}
	return ""
}

func inputTypeLabel(code byte) string {
	switch code {
	case 0:
		return "целое число"
	case 1:
		return "дробное число"
	case 2:
		return "один символ"
	}
	return "значение"
}

func convertInput(line, typ string) (value.Value, bool) {
	switch typ {
	case "int":
		i, err := strconv.ParseInt(strings.TrimSpace(line), 10, 64)
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

// ---------- файлы ----------

func (m *VM) filePath(v value.Value) string {
	p := v.String()
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(m.dir, p)
}

func (m *VM) opFile(at int, name string) error {
	switch name {
	case "openFile":
		p, err := m.pop()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(m.filePath(p))
		if err != nil {
			return m.fail(at, "openFile: %v", err)
		}
		m.push(value.StrV(string(data)))
		return nil
	case "writeFile", "appendFile":
		dataV, err := m.pop()
		if err != nil {
			return err
		}
		p, err := m.pop()
		if err != nil {
			return err
		}
		path := m.filePath(p)
		data := dataV.String()
		if name == "writeFile" {
			if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
				return m.fail(at, "writeFile: %v", err)
			}
			return nil
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return m.fail(at, "appendFile: %v", err)
		}
		if _, err := f.WriteString(data); err != nil {
			f.Close()
			return m.fail(at, "appendFile: %v", err)
		}
		f.Close()
		return nil
	case "delFile":
		p, err := m.pop()
		if err != nil {
			return err
		}
		if err := os.Remove(m.filePath(p)); err != nil {
			return m.fail(at, "delFile: %v", err)
		}
		return nil
	case "searchFile":
		p, err := m.pop()
		if err != nil {
			return err
		}
		entries, err := os.ReadDir(m.filePath(p))
		if err != nil {
			return m.fail(at, "searchFile: %v", err)
		}
		var names []string
		for _, e := range entries {
			nm := e.Name()
			if e.IsDir() {
				nm += "/"
			}
			names = append(names, nm)
		}
		sort.Strings(names)
		items := make([]value.Value, len(names))
		for i, nm := range names {
			items[i] = value.StrV(nm)
		}
		m.push(value.ListV(items))
		return nil
	}
	return m.fail(at, "неизвестная файловая операция %s", name)
}
