// Package vm исполняет переносимый байт-код KonLang на хост-платформе.
//
// Байт-код создаётся пакетом compiler и может быть сохранён в .knc-образ.
// ВМ использует те же значения, экран и файловые функции, что и древовидный
// интерпретатор, поэтому обе ветки исполнения дают одинаковый результат.
package vm

import (
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/konlang/konlang/internal/compiler"
	"github.com/konlang/konlang/internal/term"
	"github.com/konlang/konlang/internal/value"
)

const maxDepth = 400

// Error — ошибка времени выполнения байт-кода.
type Error struct {
	Addr int
	Msg  string
}

func (e *Error) Error() string {
	if e.Addr < 0 {
		return "ВМ: " + e.Msg
	}
	return fmt.Sprintf("ВМ %04X: %s", e.Addr, e.Msg)
}

// Options — настройки виртуальной машины.
type Options struct {
	Screen term.Screen
	Dir    string // папка для файлов и exec-строк
	Seed   int64  // 0 — случайный
	Trace  bool   // трассировка опкодов в stderr
}

type frameKind int

const (
	frameCall frameKind = iota
	frameExec
	frameProgram
)

type frame struct {
	retProg *compiler.Program
	retIP   int
	scope   map[string]value.Value
	kind    frameKind
}

type codeRef struct {
	prog *compiler.Program
	addr int
}

type function struct {
	prog   *compiler.Program
	addr   int
	params []string
}

// VM — состояние исполнения образа байт-кода.
type VM struct {
	prog  *compiler.Program
	ip    int
	stack []value.Value

	vars  map[string]value.Value
	funcs map[string]*function
	codes map[*value.Container]codeRef
	libs  map[string]bool
	scope map[string]value.Value

	frames []frame

	screen  term.Screen
	dir     string
	rng     *rand.Rand
	trace   bool
	steps   int64
	depth   int
	halted  bool
	exit    int
	failure *Error
}

// New создаёт ВМ для образа байт-кода.
func New(prog *compiler.Program, o Options) *VM {
	seed := o.Seed
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	dir := o.Dir
	if dir == "" {
		dir, _ = os.Getwd()
	}
	scr := o.Screen
	if scr == nil {
		scr = term.New(term.Options{})
	}
	return &VM{
		prog:   prog,
		vars:   map[string]value.Value{},
		funcs:  map[string]*function{},
		codes:  map[*value.Container]codeRef{},
		libs:   map[string]bool{},
		screen: scr,
		dir:    dir,
		rng:    rand.New(rand.NewSource(seed)),
		trace:  o.Trace,
	}
}

// Vars даёт доступ к переменным (для тестов и отладки).
func (m *VM) Vars() map[string]value.Value { return m.vars }

// Run выполняет программу и возвращает код завершения.
func (m *VM) Run() (int, error) {
	for !m.halted {
		at := m.ip
		if err := m.step(); err != nil {
			if e, ok := err.(*Error); ok {
				return 1, e
			}
			return 1, &Error{Addr: at, Msg: err.Error()}
		}
	}
	return m.exit, nil
}

func (m *VM) step() error {
	if m.ip < 0 || m.ip >= len(m.prog.Code) {
		return m.fail(m.ip, "счётчик команд вне байт-кода")
	}
	at := m.ip
	op := compiler.Opcode(m.prog.Code[m.ip])
	m.ip++
	if m.trace {
		fmt.Fprintf(os.Stderr, "[vm] %04X %s\n", at, op)
	}
	switch op {
	case compiler.OpNOP:
		return nil
	case compiler.OpPUSHK:
		return m.opPushK(at)
	case compiler.OpPUSHNIL:
		m.push(value.NilV())
		return nil
	case compiler.OpPOP:
		_, err := m.pop()
		return err
	case compiler.OpDUP:
		v, err := m.peek()
		if err != nil {
			return err
		}
		m.push(v)
		return nil
	case compiler.OpLOAD:
		name, err := m.readName()
		if err != nil {
			return err
		}
		m.push(m.load(name))
		return nil
	case compiler.OpSTORE:
		name, err := m.readName()
		if err != nil {
			return err
		}
		v, err := m.pop()
		if err != nil {
			return err
		}
		m.setVar(name, v)
		return nil
	case compiler.OpLOADREG:
		name, err := m.readName()
		if err != nil {
			return err
		}
		v, err := m.loadReg(name, at)
		if err != nil {
			return err
		}
		m.push(v)
		return nil
	case compiler.OpMKLIST:
		n, err := m.readWord()
		if err != nil {
			return err
		}
		items, err := m.popN(int(n))
		if err != nil {
			return err
		}
		m.push(value.ListV(items))
		return nil
	case compiler.OpINDEX:
		return m.opIndex(at)
	case compiler.OpCALL:
		return m.opCall(at)
	case compiler.OpRETV:
		v, err := m.pop()
		if err != nil {
			return err
		}
		return m.doReturn(v, true)
	case compiler.OpRETN:
		return m.doReturn(value.NilV(), false)
	case compiler.OpJMP:
		target, err := m.readWord()
		if err != nil {
			return err
		}
		return m.jump(int(target), at)
	case compiler.OpJZ:
		target, err := m.readWord()
		if err != nil {
			return err
		}
		v, err := m.pop()
		if err != nil {
			return err
		}
		if !v.Truth() {
			return m.jump(int(target), at)
		}
		return nil
	case compiler.OpJNZ:
		target, err := m.readWord()
		if err != nil {
			return err
		}
		v, err := m.pop()
		if err != nil {
			return err
		}
		if v.Truth() {
			return m.jump(int(target), at)
		}
		return nil
	case compiler.OpADD:
		return m.opArith(at, "+")
	case compiler.OpSUB:
		return m.opArith(at, "-")
	case compiler.OpMUL:
		return m.opArith(at, "*")
	case compiler.OpDIV:
		return m.opArith(at, "/")
	case compiler.OpMOD:
		return m.opArith(at, "%")
	case compiler.OpNEG:
		return m.opNeg(at)
	case compiler.OpEQ:
		return m.opCompare(at, "==")
	case compiler.OpNE:
		return m.opCompare(at, "!=")
	case compiler.OpLT:
		return m.opCompare(at, "<")
	case compiler.OpLE:
		return m.opCompare(at, "<=")
	case compiler.OpGT:
		return m.opCompare(at, ">")
	case compiler.OpGE:
		return m.opCompare(at, ">=")
	case compiler.OpAND:
		return m.opLogic(at, "and")
	case compiler.OpOR:
		return m.opLogic(at, "or")
	case compiler.OpNOT:
		v, err := m.pop()
		if err != nil {
			return err
		}
		m.push(boolVal(!v.Truth()))
		return nil
	case compiler.OpCONCAT:
		b, err := m.pop()
		if err != nil {
			return err
		}
		a, err := m.pop()
		if err != nil {
			return err
		}
		m.push(value.StrV(a.String() + b.String()))
		return nil
	case compiler.OpREPEAT:
		cnt, err := m.pop()
		if err != nil {
			return err
		}
		s, err := m.pop()
		if err != nil {
			return err
		}
		n, ok := cnt.Int64()
		if !ok {
			return m.fail(at, "повтор {x*n}: n должно быть числом")
		}
		m.push(value.StrV(repeatStr(s.String(), n)))
		return nil
	case compiler.OpLEN:
		v, err := m.pop()
		if err != nil {
			return err
		}
		m.push(value.IntV(int64(valueLen(v))))
		return nil
	case compiler.OpCAST:
		t, err := m.readByte()
		if err != nil {
			return err
		}
		v, err := m.pop()
		if err != nil {
			return err
		}
		res, err := m.cast(v, t, at)
		if err != nil {
			return err
		}
		m.push(res)
		return nil
	case compiler.OpPRINT:
		n, err := m.readWord()
		if err != nil {
			return err
		}
		return m.opPrint(int(n), false, at)
	case compiler.OpPRINTXY:
		n, err := m.readWord()
		if err != nil {
			return err
		}
		return m.opPrint(int(n), true, at)
	case compiler.OpINPUT:
		return m.opInput(at)
	case compiler.OpKEY:
		return m.opKey(at)
	case compiler.OpCLEAR:
		m.screen.Clear()
		m.screen.Flush()
		return nil
	case compiler.OpSCRSIZE:
		w, h := m.screen.Size()
		m.vars["scr_w"] = value.IntV(int64(w))
		m.vars["scr_h"] = value.IntV(int64(h))
		return nil
	case compiler.OpSCRCUR:
		x, y := m.screen.Cursor()
		m.vars["cur_x"] = value.IntV(int64(x))
		m.vars["cur_y"] = value.IntV(int64(y))
		return nil
	case compiler.OpSCRBG:
		return m.opScrBg(at)
	case compiler.OpBEEP:
		v, err := m.pop()
		if err != nil {
			return err
		}
		f, _ := v.Int64()
		m.screen.Beep(int(f))
		return nil
	case compiler.OpRANDOM:
		return m.opRandom(at)
	case compiler.OpEXEC:
		return m.opExec(at)
	case compiler.OpKON:
		return m.opKon(at)
	case compiler.OpDEF:
		return m.opDef(at)
	case compiler.OpFOPEN:
		return m.opFile(at, "openFile")
	case compiler.OpFWRITE:
		return m.opFile(at, "writeFile")
	case compiler.OpFAPPEND:
		return m.opFile(at, "appendFile")
	case compiler.OpFDEL:
		return m.opFile(at, "delFile")
	case compiler.OpFLIST:
		return m.opFile(at, "searchFile")
	case compiler.OpTIME:
		v, err := m.pop()
		if err != nil {
			return err
		}
		units, _ := v.Int64()
		if units > 0 {
			time.Sleep(time.Duration(units) * 10 * time.Millisecond)
		}
		return nil
	case compiler.OpEXIT:
		m.halted = true
		m.exit = 0
		return nil
	case compiler.OpDIVI:
		return m.opArith(at, "/int")
	case compiler.OpDIVF:
		return m.opArith(at, "/float")
	case compiler.OpSTYLE:
		return m.opStyle(at)
	case compiler.OpCOLOR:
		return m.opColor(at)
	case compiler.OpSTYLENAME:
		return m.opStyleName(at)
	case compiler.OpIMPORT:
		name, err := m.readName()
		if err != nil {
			return err
		}
		m.libs[name] = true
		return nil
	case compiler.OpHALT:
		return m.opHalt()
	}
	return m.fail(at, "неизвестный опкод 0x%02X", byte(op))
}

// ---------- стек и операнды ----------

func (m *VM) push(v value.Value) { m.stack = append(m.stack, v) }

func (m *VM) pop() (value.Value, error) {
	if len(m.stack) == 0 {
		return value.NilV(), m.fail(m.ip-1, "пустой стек значений")
	}
	v := m.stack[len(m.stack)-1]
	m.stack = m.stack[:len(m.stack)-1]
	return v, nil
}

func (m *VM) peek() (value.Value, error) {
	if len(m.stack) == 0 {
		return value.NilV(), m.fail(m.ip-1, "пустой стек значений")
	}
	return m.stack[len(m.stack)-1], nil
}

func (m *VM) popN(n int) ([]value.Value, error) {
	if n < 0 || n > len(m.stack) {
		return nil, m.fail(m.ip-1, "недостаточно значений на стеке (нужно %d)", n)
	}
	items := make([]value.Value, n)
	for i := n - 1; i >= 0; i-- {
		items[i] = m.stack[len(m.stack)-1]
		m.stack = m.stack[:len(m.stack)-1]
	}
	return items, nil
}

func (m *VM) readByte() (byte, error) {
	if m.ip >= len(m.prog.Code) {
		return 0, m.fail(m.ip, "неожиданный конец байт-кода")
	}
	b := m.prog.Code[m.ip]
	m.ip++
	return b, nil
}

func (m *VM) readWord() (uint16, error) {
	if m.ip+2 > len(m.prog.Code) {
		return 0, m.fail(m.ip, "неожиданный конец байт-кода")
	}
	w := uint16(m.prog.Code[m.ip]) | uint16(m.prog.Code[m.ip+1])<<8
	m.ip += 2
	return w, nil
}

func (m *VM) readName() (string, error) {
	off, err := m.readWord()
	if err != nil {
		return "", err
	}
	name := m.prog.NameAt(int(off))
	if name == "?" {
		return "", m.fail(m.ip-3, "неизвестное имя (смещение %d)", off)
	}
	return name, nil
}

func (m *VM) jump(target, at int) error {
	if target < 0 || target > len(m.prog.Code) {
		return m.fail(at, "переход за пределы байт-кода: %d", target)
	}
	m.ip = target
	return nil
}

func (m *VM) fail(at int, format string, a ...any) error {
	return &Error{Addr: at, Msg: fmt.Sprintf(format, a...)}
}

// ---------- переменные и области видимости ----------

func (m *VM) setVar(name string, v value.Value) {
	if m.scope != nil {
		if _, ok := m.scope[name]; ok {
			m.scope[name] = v
			return
		}
	}
	m.vars[name] = v
}

func (m *VM) lookup(name string) (value.Value, bool) {
	if m.scope != nil {
		if v, ok := m.scope[name]; ok {
			return v, true
		}
	}
	v, ok := m.vars[name]
	return v, ok
}

func (m *VM) load(name string) value.Value {
	if v, ok := m.lookup(name); ok {
		return v
	}
	// «голое слово» — строка (элементы списков и т. п.).
	return value.StrV(name)
}

func (m *VM) loadReg(name string, at int) (value.Value, error) {
	if v, ok := m.lookup(name); ok {
		return v, nil
	}
	if isRegister(name) {
		return value.NilV(), nil
	}
	return value.NilV(), m.fail(at, "неизвестный регистр или переменная [%s]", name)
}

func isRegister(name string) bool {
	for _, p := range []string{"inp_", "rand_", "key_", "kon_", "scr_", "cur_"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// ---------- вызовы и возвраты ----------

func (m *VM) doReturn(v value.Value, withValue bool) error {
	if len(m.frames) == 0 {
		// ret в корне программы — завершение, как в интерпретаторе.
		m.halted = true
		m.exit = 0
		return nil
	}
	f := m.frames[len(m.frames)-1]
	m.frames = m.frames[:len(m.frames)-1]
	m.prog = f.retProg
	m.ip = f.retIP
	m.scope = f.scope
	switch f.kind {
	case frameCall:
		if m.depth > 0 {
			m.depth--
		}
		if !withValue {
			v = value.NilV()
		}
		m.push(v)
	case frameExec, frameProgram:
		// Значение ret из контейнера/exec-строки отбрасывается.
	}
	return nil
}

func (m *VM) opCall(at int) error {
	name, err := m.readName()
	if err != nil {
		return err
	}
	argc, err := m.readByte()
	if err != nil {
		return err
	}
	fn, ok := m.funcs[name]
	if !ok {
		return m.fail(at, "неизвестная функция %s{%d арг.}", name, argc)
	}
	if len(fn.params) != int(argc) {
		return m.fail(at, "функция %s ожидает %d аргумент(ов), получено %d",
			name, len(fn.params), argc)
	}
	if m.depth >= maxDepth {
		return m.fail(at, "слишком глубокая рекурсия (%s)", name)
	}
	args, err := m.popN(int(argc))
	if err != nil {
		return err
	}
	scope := make(map[string]value.Value, len(args))
	for i, p := range fn.params {
		scope[p] = args[i]
	}
	m.frames = append(m.frames, frame{
		retProg: m.prog,
		retIP:   m.ip,
		scope:   m.scope,
		kind:    frameCall,
	})
	m.scope = scope
	m.depth++
	m.prog = fn.prog
	m.ip = fn.addr
	return nil
}

func (m *VM) opExec(at int) error {
	v, err := m.pop()
	if err != nil {
		return err
	}
	switch v.Kind {
	case value.Code:
		ref, ok := m.codes[v.C]
		if !ok {
			return m.fail(at, "exec: неизвестный контейнер %q", v.String())
		}
		m.frames = append(m.frames, frame{
			retProg: m.prog,
			retIP:   m.ip,
			scope:   m.scope,
			kind:    frameExec,
		})
		m.prog = ref.prog
		m.ip = ref.addr
		return nil
	case value.Str:
		prog, err := compiler.CompileString(v.S, compiler.Options{
			Dir:               m.dir,
			Libs:              m.libList(),
			AllowUnknownCalls: true,
		})
		if err != nil {
			return m.fail(at, "exec: содержимое не является корректным блоком кода: %v", err)
		}
		m.frames = append(m.frames, frame{
			retProg: m.prog,
			retIP:   m.ip,
			scope:   m.scope,
			kind:    frameProgram,
		})
		m.prog = prog
		m.ip = 0
		return nil
	}
	return m.fail(at, "exec: ожидается контейнер kon_X, получено %s", v.Kind)
}

// libList перечисляет подключённые библиотеки для компиляции exec-строк.
func (m *VM) libList() []string {
	names := make([]string, 0, len(m.libs))
	for name := range m.libs {
		names = append(names, name)
	}
	return names
}

func (m *VM) opKon(at int) error {
	name, err := m.readName()
	if err != nil {
		return err
	}
	addr, err := m.readWord()
	if err != nil {
		return err
	}
	c := &value.Container{Name: name}
	m.codes[c] = codeRef{prog: m.prog, addr: int(addr)}
	m.setVar(name, value.CodeV(c))
	return nil
}

func (m *VM) opDef(at int) error {
	name, err := m.readName()
	if err != nil {
		return err
	}
	addr, err := m.readWord()
	if err != nil {
		return err
	}
	argc, err := m.readByte()
	if err != nil {
		return err
	}
	params := make([]string, int(argc))
	for i := range params {
		p, err := m.readName()
		if err != nil {
			return err
		}
		params[i] = p
	}
	m.funcs[name] = &function{prog: m.prog, addr: int(addr), params: params}
	return nil
}

func (m *VM) opHalt() error {
	if len(m.frames) == 0 {
		m.halted = true
		return nil
	}
	f := m.frames[len(m.frames)-1]
	m.frames = m.frames[:len(m.frames)-1]
	m.prog = f.retProg
	m.ip = f.retIP
	m.scope = f.scope
	if f.kind == frameCall && m.depth > 0 {
		m.depth--
	}
	return nil
}
