package compiler

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/konlang/konlang/internal/ast"
	"github.com/konlang/konlang/internal/lexer"
	"github.com/konlang/konlang/internal/parser"
)

// Program — готовый образ байт-кода.
type Program struct {
	Code   []byte // байт-код
	Consts []byte // пул констант с таблицей смещений
	Names  []byte // NUL-терминированные имена

	Encoding Encoding // кодировка строк и имён
	Target   Target   // целевая платформа

	consts     []Constant
	constTexts []string
	nameTexts  map[int]string
}

// Constant — декодированная константа байт-кода.
type Constant struct {
	Kind  byte // 1=int, 2=dec (4 знака), 3=str
	Int   int32
	Float float64
	Str   string
}

// Constant возвращает константу по индексу.
func (p *Program) Constant(i int) (Constant, bool) {
	if i < 0 || i >= len(p.consts) {
		return Constant{}, false
	}
	return p.consts[i], true
}

// Options — параметры компиляции.
type Options struct {
	Encoding Encoding // по умолчанию UTF-8
	Target   Target   // по умолчанию portable
	Dir      string   // папка для inp[библиотека]

	// Libs — уже подключённые библиотеки. Используется при компиляции
	// exec-строк, чтобы унаследовать inp[style_text] из внешней программы.
	Libs []string
	// AllowUnknownCalls разрешает вызовы функций, определённых вне фрагмента
	// (exec-строка может обращаться к функциям основной программы).
	AllowUnknownCalls bool
}

// ConstText — текст константы для дизассемблера.
func (p *Program) ConstText(i int) string {
	if i < 0 || i >= len(p.constTexts) {
		return "?"
	}
	return p.constTexts[i]
}

// NameAt — имя по смещению в блоке имён.
func (p *Program) NameAt(off int) string {
	if s, ok := p.nameTexts[off]; ok {
		return s
	}
	return "?"
}

// Error — ошибка компиляции с позицией.
type Error struct {
	Pos lexer.Pos
	Msg string
}

func (e *Error) Error() string {
	if e.Pos.Line == 0 {
		return "компилятор: " + e.Msg
	}
	return fmt.Sprintf("компилятор %s: %s", e.Pos, e.Msg)
}

type constEntry struct {
	kind byte // 1=int, 2=dec4, 3=str
	data []byte
}

type loopInfo struct {
	breaks []int
}

type builder struct {
	code       []byte
	entries    []constEntry
	consts     []Constant
	constIdx   map[string]int
	constTexts []string
	names      map[string]int
	namesBuf   []byte
	nameTexts  map[int]string
	loops      []*loopInfo
	cast       string
	libs       map[string]bool
	tmpN       int
	defs       map[string]int
	calls      map[string]lexer.Pos
	opts       Options
	imports    []string
	err        error
}

// Compile превращает дерево разбора в переносимый образ байт-кода (UTF-8).
func Compile(root *ast.Node) (*Program, error) {
	return CompileWith(Options{}, root)
}

// CompileWith превращает дерево разбора в образ с заданными параметрами.
func CompileWith(o Options, root *ast.Node) (*Program, error) {
	if o.Encoding > EncodingCP866 {
		return nil, &Error{Msg: fmt.Sprintf("неизвестная кодировка %d", o.Encoding)}
	}
	if o.Target > TargetProS {
		return nil, &Error{Msg: fmt.Sprintf("неизвестная цель %d", o.Target)}
	}
	b := &builder{
		constIdx:  map[string]int{},
		names:     map[string]int{},
		nameTexts: map[int]string{},
		libs:      map[string]bool{},
		defs:      map[string]int{},
		calls:     map[string]lexer.Pos{},
		opts:      o,
	}
	for _, name := range o.Libs {
		b.libs[name] = true
	}
	for _, st := range root.Kids {
		b.stmt(st)
		if b.err != nil {
			return nil, b.err
		}
	}
	b.emit(OpHALT)
	if !o.AllowUnknownCalls {
		for name, pos := range b.calls {
			if _, ok := b.defs[name]; !ok {
				return nil, &Error{Pos: pos, Msg: "неизвестная функция " + name + "{}"}
			}
		}
	}
	if len(b.code) > 0xFFFF {
		return nil, &Error{Msg: "программа слишком велика для байт-кода (максимум 65535 байт)"}
	}
	if len(b.namesBuf) > 0xFFFF {
		return nil, &Error{Msg: "пул имён слишком велик для байт-кода (максимум 65535 байт)"}
	}
	if len(b.entries) > 0xFFFF {
		return nil, &Error{Msg: "слишком много констант для байт-кода (максимум 65535)"}
	}
	if size := b.constPoolSize(); size > 0xFFFF {
		return nil, &Error{Msg: fmt.Sprintf("пул констант слишком велик для байт-кода (%d байт, максимум 65535)", size)}
	}
	p := &Program{
		Code:       b.code,
		Names:      b.namesBuf,
		Encoding:   o.Encoding,
		Target:     o.Target,
		consts:     b.consts,
		constTexts: b.constTexts,
		nameTexts:  b.nameTexts,
	}
	p.Consts = b.buildConsts()
	return p, nil
}

// CompileFile разбирает .kn и компилирует его, подключая файловые библиотеки
// относительно папки исходника.
func CompileFile(path string) (*Program, error) {
	prog, err := parser.ParseFile(path)
	if err != nil {
		return nil, err
	}
	dir, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		dir = filepath.Dir(path)
	}
	return CompileWith(Options{Dir: dir}, prog)
}

// CompileString разбирает и компилирует фрагмент кода (для exec-строк).
func CompileString(src string, o Options) (*Program, error) {
	prog, err := parser.Parse(src)
	if err != nil {
		return nil, err
	}
	return CompileWith(o, prog)
}

func (b *builder) fail(pos lexer.Pos, format string, a ...any) {
	if b.err == nil {
		b.err = &Error{Pos: pos, Msg: fmt.Sprintf(format, a...)}
	}
}

// ---------- генерация ----------

func (b *builder) emit(op Opcode) int {
	at := len(b.code)
	b.code = append(b.code, byte(op))
	return at
}
func (b *builder) emitW(op Opcode, v int) int {
	at := b.emit(op)
	b.code = append(b.code, byte(v), byte(v>>8))
	return at
}
func (b *builder) emitB(op Opcode, v byte) {
	b.emit(op)
	b.code = append(b.code, v)
}
func (b *builder) word(v int) { b.code = append(b.code, byte(v), byte(v>>8)) }
func (b *builder) here() int  { return len(b.code) }
func (b *builder) patch(at int, target int) {
	b.code[at] = byte(target)
	b.code[at+1] = byte(target >> 8)
}
func (b *builder) jmp(op Opcode) int {
	at := b.emit(op)
	b.word(0)
	return at + 1
}
func (b *builder) jumpTo(op Opcode, target int) {
	b.emit(op)
	b.word(target)
}

func (b *builder) name(s string) int {
	if b.err != nil {
		return 0
	}
	if off, ok := b.names[s]; ok {
		return off
	}
	raw := encodeText(s, b.opts.Encoding)
	off := len(b.namesBuf)
	if off+len(raw)+1 > 0xFFFF {
		b.fail(lexer.Pos{}, "имя %q не помещается в пул имён (максимум 65535 байт)", s)
		return 0
	}
	b.names[s] = off
	b.nameTexts[off] = s
	b.namesBuf = append(b.namesBuf, raw...)
	b.namesBuf = append(b.namesBuf, 0)
	return off
}

func (b *builder) constInt(v int32) int {
	key := fmt.Sprintf("i%d", v)
	if i, ok := b.constIdx[key]; ok {
		return i
	}
	var d [4]byte
	binary.LittleEndian.PutUint32(d[:], uint32(v))
	return b.addConst(key, constEntry{kind: 1, data: d[:]}, fmt.Sprintf("%d", v))
}

func (b *builder) constDec(v int32) int {
	key := fmt.Sprintf("f%d", v)
	if i, ok := b.constIdx[key]; ok {
		return i
	}
	var d [4]byte
	binary.LittleEndian.PutUint32(d[:], uint32(v))
	return b.addConst(key, constEntry{kind: 2, data: d[:]}, fmt.Sprintf("%s", formatDec(v)))
}

func (b *builder) constStr(s string) int {
	key := "s" + s
	if i, ok := b.constIdx[key]; ok {
		return i
	}
	raw := encodeText(s, b.opts.Encoding)
	if len(raw) > 0xFFFF {
		b.fail(lexer.Pos{}, "строковая константа слишком длинная (%d байт, максимум 65535)", len(raw))
		return 0
	}
	d := make([]byte, 2+len(raw))
	binary.LittleEndian.PutUint16(d[:2], uint16(len(raw)))
	copy(d[2:], raw)
	return b.addConst(key, constEntry{kind: 3, data: d}, fmt.Sprintf("%q", s))
}

func (b *builder) addConst(key string, e constEntry, text string) int {
	if b.err != nil {
		return 0
	}
	i := len(b.entries)
	b.entries = append(b.entries, e)
	b.constIdx[key] = i
	b.constTexts = append(b.constTexts, text)
	c := Constant{Kind: e.kind}
	switch e.kind {
	case 1:
		c.Int = int32(binary.LittleEndian.Uint32(e.data))
	case 2:
		c.Int = int32(binary.LittleEndian.Uint32(e.data))
		c.Float = float64(c.Int) / 10000
	case 3:
		n := int(binary.LittleEndian.Uint16(e.data[:2]))
		if n > len(e.data)-2 {
			n = len(e.data) - 2
		}
		c.Str = decodeText(e.data[2:2+n], b.opts.Encoding)
	}
	b.consts = append(b.consts, c)
	return i
}

func (b *builder) constPoolSize() int {
	size := 2 + 2*len(b.entries)
	for _, e := range b.entries {
		size += 1 + len(e.data)
	}
	return size
}

func (b *builder) buildConsts() []byte {
	n := len(b.entries)
	out := make([]byte, 2+2*n)
	binary.LittleEndian.PutUint16(out[:2], uint16(n))
	off := len(out)
	for i, e := range b.entries {
		binary.LittleEndian.PutUint16(out[2+2*i:], uint16(off))
		out = append(out, e.kind)
		out = append(out, e.data...)
		off += 1 + len(e.data)
	}
	return out
}

func formatDec(v int32) string {
	n := int64(v)
	neg := n < 0
	if neg {
		n = -n
	}
	whole := n / 10000
	frac := n % 10000
	s := fmt.Sprintf("%d.%04d", whole, frac)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if neg {
		s = "-" + s
	}
	return s
}

func (b *builder) pushInt(v int32) { b.emitW(OpPUSHK, b.constInt(v)) }

// ---------- конструкции ----------

func (b *builder) block(n *ast.Node) {
	if n == nil {
		return
	}
	for _, st := range n.Kids {
		b.stmt(st)
		if b.err != nil {
			return
		}
	}
}

func (b *builder) stmt(n *ast.Node) {
	if b.err != nil {
		return
	}
	switch n.Kind {
	case ast.Kon:
		b.emit(OpKON)
		b.word(b.name(n.Name))
		addrPos := len(b.code)
		b.word(0)
		skip := b.jmp(OpJMP)
		b.patch(addrPos, b.here())
		b.block(n.Kids[0])
		b.emit(OpRETN)
		b.patch(skip, b.here())

	case ast.Def:
		if len(n.Aux) > 8 {
			b.fail(n.Pos, "функция %s: максимум 8 параметров", n.Name)
			return
		}
		b.defs[n.Name] = len(n.Aux)
		b.emit(OpDEF)
		b.word(b.name(n.Name))
		addrPos := len(b.code)
		b.word(0)
		b.code = append(b.code, byte(len(n.Aux)))
		for _, p := range n.Aux {
			b.word(b.name(p.Name))
		}
		skip := b.jmp(OpJMP)
		b.patch(addrPos, b.here())
		b.block(n.Kids[0])
		b.emit(OpRETN)
		b.patch(skip, b.here())

	case ast.Print:
		for _, k := range n.Kids {
			b.expr(k)
		}
		if n.Flag == 1 {
			b.emitW(OpPRINTXY, len(n.Kids))
		} else {
			b.emitW(OpPRINT, len(n.Kids))
		}

	case ast.Input:
		b.expr(n.Kids[0])
		if len(n.Kids) > 1 {
			b.expr(n.Kids[1])
		} else {
			b.emit(OpPUSHNIL)
		}
		b.emit(OpINPUT)
		b.word(b.name(n.Name))
		b.code = append(b.code, typeCode(n.Str))

	case ast.Key:
		b.emitW(OpKEY, b.name(n.Name))

	case ast.Clear:
		b.emit(OpCLEAR)

	case ast.Random:
		if len(n.Kids) == 0 {
			b.fail(n.Pos, "random: пустой список вариантов")
			return
		}
		for _, k := range n.Kids {
			b.expr(k)
		}
		b.emit(OpRANDOM)
		b.word(b.name(n.Name))
		b.word(len(n.Kids))

	case ast.Set:
		b.expr(n.Kids[0])
		b.emitW(OpSTORE, b.name(n.Name))

	case ast.Exec:
		b.expr(n.Kids[0])
		b.emit(OpEXEC)

	case ast.Cycle:
		b.cycle(n)

	case ast.If:
		var ends []int
		for i := 0; i+1 < len(n.Kids); i += 2 {
			b.expr(n.Kids[i])
			next := b.jmp(OpJZ)
			b.block(n.Kids[i+1])
			ends = append(ends, b.jmp(OpJMP))
			b.patch(next, b.here())
		}
		if len(n.Aux) > 0 {
			b.block(n.Aux[0])
		}
		for _, e := range ends {
			b.patch(e, b.here())
		}

	case ast.Ret:
		if len(n.Kids) > 0 {
			b.expr(n.Kids[0])
			b.emit(OpRETV)
		} else {
			b.emit(OpRETN)
		}

	case ast.Break:
		if len(b.loops) == 0 {
			b.fail(n.Pos, "sc вне цикла")
			return
		}
		l := b.loops[len(b.loops)-1]
		l.breaks = append(l.breaks, b.jmp(OpJMP))

	case ast.Time:
		b.expr(n.Kids[0])
		b.emit(OpTIME)

	case ast.Exit:
		b.emit(OpEXIT)

	case ast.Import:
		b.importLib(n)

	case ast.ScrSize:
		b.emit(OpSCRSIZE)
	case ast.ScrCur:
		b.emit(OpSCRCUR)
	case ast.ScrBg:
		b.expr(n.Kids[0])
		b.emit(OpSCRBG)
	case ast.Beep:
		b.expr(n.Kids[0])
		b.emit(OpBEEP)

	case ast.FileOp:
		b.fileOp(n)
		b.emit(OpPOP)

	case ast.ExprStmt:
		b.expr(n.Kids[0])
		b.emit(OpPOP)

	default:
		b.fail(n.Pos, "конструкция %s не поддерживается компилятором", n.Kind)
	}
}

// importLib подключает встроенную библиотеку или .kn-файл рядом с программой.
func (b *builder) importLib(n *ast.Node) {
	if n.Name == "style_text" {
		b.libs[n.Name] = true
		// Маркер нужен ВМ: exec-строки наследуют подключённые библиотеки.
		b.emitW(OpIMPORT, b.name(n.Name))
		return
	}
	if b.opts.Dir == "" {
		b.fail(n.Pos, "библиотека %q: не задана папка поиска", n.Name)
		return
	}
	for _, name := range b.imports {
		if name == n.Name {
			b.fail(n.Pos, "рекурсивный импорт библиотеки %q", n.Name)
			return
		}
	}
	path := filepath.Join(b.opts.Dir, n.Name+".kn")
	src, err := os.ReadFile(path)
	if err != nil {
		b.fail(n.Pos, "неизвестная библиотека %q (нет встроенной и файла %s)", n.Name, path)
		return
	}
	prog, err := parser.Parse(string(src))
	if err != nil {
		b.fail(n.Pos, "библиотека %s: %v", n.Name, err)
		return
	}
	b.imports = append(b.imports, n.Name)
	for _, st := range prog.Kids {
		b.stmt(st)
		if b.err != nil {
			break
		}
	}
	b.imports = b.imports[:len(b.imports)-1]
	if b.err == nil {
		b.libs[n.Name] = true
	}
}

func typeCode(t string) byte {
	switch t {
	case "int":
		return 0
	case "float", "double":
		return 1
	case "char":
		return 2
	}
	return 3
}

func (b *builder) cycle(n *ast.Node) {
	b.tmpN++
	cnt := fmt.Sprintf("$c%d", b.tmpN)
	idx := fmt.Sprintf("$i%d", b.tmpN)
	finite := n.Kids[0] != nil
	if finite {
		b.expr(n.Kids[0])
		b.emitB(OpCAST, 0) // счётчик цикла целый, как в интерпретаторе
		b.emitW(OpSTORE, b.name(cnt))
		b.pushInt(0)
		b.emitW(OpSTORE, b.name(idx))
	}
	top := b.here()
	var condJump int = -1
	if finite {
		// Отрицательный счётчик означает бесконечный цикл (как в интерпретаторе):
		// count < 0 || index < count.
		b.emitW(OpLOADREG, b.name(cnt))
		b.pushInt(0)
		b.emit(OpLT)
		negative := b.jmp(OpJNZ)
		b.emitW(OpLOADREG, b.name(idx))
		b.emitW(OpLOADREG, b.name(cnt))
		b.emit(OpLT)
		condJump = b.jmp(OpJZ)
		b.patch(negative, b.here())
	}
	l := &loopInfo{}
	b.loops = append(b.loops, l)
	b.block(n.Kids[1])
	b.loops = b.loops[:len(b.loops)-1]
	if finite {
		b.emitW(OpLOADREG, b.name(idx))
		b.pushInt(1)
		b.emit(OpADD)
		b.emitW(OpSTORE, b.name(idx))
	}
	b.jumpTo(OpJMP, top)
	end := b.here()
	if condJump >= 0 {
		b.patch(condJump, end)
	}
	for _, br := range l.breaks {
		b.patch(br, end)
	}
}

func (b *builder) fileOp(n *ast.Node) {
	switch n.Name {
	case "openFile":
		if !b.needArgs(n, 1) {
			return
		}
		b.expr(n.Kids[0])
		b.emit(OpFOPEN)
	case "writeFile", "appendFile":
		if !b.needArgs(n, 2) {
			return
		}
		b.expr(n.Kids[0])
		b.expr(n.Kids[1])
		if n.Name == "writeFile" {
			b.emit(OpFWRITE)
		} else {
			b.emit(OpFAPPEND)
		}
		b.emit(OpPUSHNIL) // файловые команды возвращают nil как выражение
	case "delFile":
		if !b.needArgs(n, 1) {
			return
		}
		b.expr(n.Kids[0])
		b.emit(OpFDEL)
		b.emit(OpPUSHNIL)
	case "searchFile":
		if !b.needArgs(n, 1) {
			return
		}
		b.expr(n.Kids[0])
		b.emit(OpFLIST)
	default:
		b.fail(n.Pos, "неизвестная файловая операция %s", n.Name)
	}
}

func (b *builder) needArgs(n *ast.Node, k int) bool {
	if len(n.Kids) < k {
		b.fail(n.Pos, "%s: нужно аргументов — %d", n.Name, k)
		return false
	}
	return true
}

func (b *builder) oneArg(n *ast.Node) bool {
	if len(n.Kids) != 1 {
		b.fail(n.Pos, "%s: нужен один аргумент", n.Name)
		return false
	}
	return true
}

// ---------- выражения ----------

func (b *builder) expr(n *ast.Node) {
	if b.err != nil || n == nil {
		return
	}
	switch n.Kind {
	case ast.Str:
		b.emitW(OpPUSHK, b.constStr(n.Str))
	case ast.Num:
		if n.Flag == 1 {
			if n.Num > 2147483647 || n.Num < -2147483648 {
				b.fail(n.Pos, "число %v не помещается в 32 бита", n.Num)
				return
			}
			b.pushInt(int32(n.Num))
		} else {
			v := math.Round(n.Num * 10000)
			if v > 2147483647 || v < -2147483648 {
				b.fail(n.Pos, "дробное число %v вне диапазона ±214748.3647", n.Num)
				return
			}
			b.emitW(OpPUSHK, b.constDec(int32(v)))
		}
	case ast.Ident:
		b.emitW(OpLOAD, b.name(n.Name))
	case ast.Ref:
		inner := n.Kids[0]
		if inner.Kind == ast.Ident {
			b.emitW(OpLOADREG, b.name(inner.Name))
		} else {
			b.expr(inner)
		}
	case ast.Concat:
		for i, k := range n.Kids {
			b.expr(k)
			if i > 0 {
				b.emit(OpCONCAT)
			}
		}
	case ast.List:
		for _, k := range n.Kids {
			b.expr(k)
		}
		b.emitW(OpMKLIST, len(n.Kids))
	case ast.Repeat:
		b.expr(n.Kids[0])
		b.expr(n.Kids[1])
		b.emit(OpREPEAT)
	case ast.Index:
		b.expr(n.Kids[0])
		b.expr(n.Kids[1])
		b.emit(OpINDEX)
	case ast.Unary:
		b.expr(n.Kids[0])
		b.emit(OpNEG)
	case ast.Binary:
		b.binary(n)
	case ast.Call:
		b.call(n)
	default:
		b.fail(n.Pos, "выражение %s не поддерживается компилятором", n.Kind)
	}
}

func (b *builder) binary(n *ast.Node) {
	op := n.Name
	if op == "and" || op == "or" {
		b.expr(n.Kids[0])
		b.emit(OpNOT)
		b.emit(OpNOT) // нормализуем к 0/1
		b.emit(OpDUP)
		var j int
		if op == "and" {
			j = b.jmp(OpJZ) // ложь — результат уже на стеке
		} else {
			j = b.jmp(OpJNZ) // истина — результат уже на стеке
		}
		b.emit(OpPOP)
		b.expr(n.Kids[1])
		b.emit(OpNOT)
		b.emit(OpNOT)
		b.patch(j, b.here())
		return
	}
	b.expr(n.Kids[0])
	b.expr(n.Kids[1])
	switch op {
	case "+":
		b.emit(OpADD)
	case "-":
		b.emit(OpSUB)
	case "*":
		b.emit(OpMUL)
	case "/":
		switch b.cast {
		case "int":
			b.emit(OpDIVI)
		case "float", "double":
			b.emit(OpDIVF)
		default:
			b.emit(OpDIV)
		}
	case "%":
		b.emit(OpMOD)
	case "==":
		b.emit(OpEQ)
	case "!=":
		b.emit(OpNE)
	case "<":
		b.emit(OpLT)
	case "<=":
		b.emit(OpLE)
	case ">":
		b.emit(OpGT)
	case ">=":
		b.emit(OpGE)
	default:
		b.fail(n.Pos, "неизвестный оператор %q", op)
	}
}

var styleCodes = map[string]int{
	"bold": 1, "dim": 2, "italic": 3, "underline": 4,
	"blink": 5, "reverse": 7, "strike": 9,
}

var colorCodes = map[string]int{
	"BLACK": 30, "RED": 31, "GREEN": 32, "YELLOW": 33,
	"BLUE": 34, "MAGENTA": 35, "CYAN": 36, "WHITE": 37, "GRAY": 90,
	"BRIGHT_BLACK": 90, "BRIGHT_RED": 91, "BRIGHT_GREEN": 92, "BRIGHT_YELLOW": 93,
	"BRIGHT_BLUE": 94, "BRIGHT_MAGENTA": 95, "BRIGHT_CYAN": 96, "BRIGHT_WHITE": 97,
}

func (b *builder) call(n *ast.Node) {
	name := n.Name
	switch name {
	case "math":
		if !b.oneArg(n) {
			return
		}
		old := b.cast
		b.cast = n.Str
		b.expr(n.Kids[0])
		b.cast = old
		switch n.Str {
		case "int":
			b.emitB(OpCAST, 0)
		case "float", "double":
			b.emitB(OpCAST, 1)
		case "char":
			b.emitB(OpCAST, 2)
		}
		return
	case "len":
		if !b.oneArg(n) {
			return
		}
		b.expr(n.Kids[0])
		b.emit(OpLEN)
		return
	case "int", "float", "double", "char", "str":
		if !b.oneArg(n) {
			return
		}
		b.expr(n.Kids[0])
		b.emitB(OpCAST, typeCode(name))
		return
	case "openFile", "writeFile", "appendFile", "searchFile", "delFile":
		b.fileOp(n)
		return
	case "bold", "dim", "italic", "underline", "blink", "reverse", "strike":
		if !b.libs["style_text"] {
			b.fail(n.Pos, "%s: подключите inp[style_text]", name)
			return
		}
		if !b.needArgs(n, 1) {
			return
		}
		b.expr(n.Kids[0])
		b.pushInt(int32(styleCodes[name]))
		b.emit(OpSTYLE)
		return
	case "style", "color", "fg", "bg":
		if !b.libs["style_text"] {
			b.fail(n.Pos, "%s: подключите inp[style_text]", name)
			return
		}
		if !b.needArgs(n, 2) {
			return
		}
		b.expr(n.Kids[1])
		b.styleCodeExpr(n.Kids[0], name)
		b.emit(OpSTYLE)
		return
	case "reset":
		b.emitW(OpPUSHK, b.constStr("\x1b[0m"))
		return
	}
	argc := len(n.Kids)
	if argc > 8 {
		b.fail(n.Pos, "%s: слишком много аргументов (максимум 8)", name)
		return
	}
	for _, k := range n.Kids {
		b.expr(k)
	}
	b.calls[name] = n.Pos
	b.emit(OpCALL)
	b.word(b.name(name))
	b.code = append(b.code, byte(argc))
}

// styleCodeExpr кладёт на стек числовой SGR-код для цвета/стиля.
func (b *builder) styleCodeExpr(n *ast.Node, fn string) {
	lit := ""
	switch n.Kind {
	case ast.Ident:
		lit = n.Name
	case ast.Str:
		lit = n.Str
	}
	if lit != "" {
		key := strings.ToUpper(lit)
		if fn == "style" {
			if c, ok := styleCodes[strings.ToLower(key)]; ok {
				b.pushInt(int32(c))
				return
			}
			b.fail(n.Pos, "неизвестный стиль %q", lit)
			return
		}
		if c, ok := colorCodes[key]; ok {
			if fn == "bg" {
				c += 10
			}
			b.pushInt(int32(c))
			return
		}
		b.fail(n.Pos, "неизвестный цвет %q", lit)
		return
	}
	b.expr(n)
	if fn == "style" {
		b.emit(OpSTYLENAME)
		return
	}
	b.emit(OpCOLOR)
	if fn == "bg" {
		b.pushInt(10)
		b.emit(OpADD)
	}
}
