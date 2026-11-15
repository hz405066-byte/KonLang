package interp

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/konlang/konlang/internal/ast"
	"github.com/konlang/konlang/internal/term"
	"github.com/konlang/konlang/internal/value"
)

const maxDepth = 400

// call вызывает встроенную или пользовательскую функцию.
func (in *Interp) call(n *ast.Node) (value.Value, error) {
	name := n.Name
	switch name {
	case "math":
		if len(n.Kids) != 1 {
			return value.NilV(), in.errAt(n.Pos, "%s: нужен один аргумент", name)
		}
		old := in.cast
		in.cast = n.Str
		v, err := in.eval(n.Kids[0])
		in.cast = old
		if err != nil {
			return value.NilV(), err
		}
		return applyCast(v, n.Str, n, in)

	case "len":
		if len(n.Kids) != 1 {
			return value.NilV(), in.errAt(n.Pos, "%s: нужен один аргумент", name)
		}
		v, err := in.eval(n.Kids[0])
		if err != nil {
			return value.NilV(), err
		}
		switch v.Kind {
		case value.List:
			return value.IntV(int64(len(v.L))), nil
		case value.Str:
			return value.IntV(int64(term.VisibleLen(v.S))), nil
		case value.Nil:
			return value.IntV(0), nil
		}
		return value.IntV(int64(len([]rune(v.String())))), nil

	case "openFile", "writeFile", "appendFile", "searchFile", "delFile":
		return in.fileOp(n)

	case "int", "float", "double", "char", "str":
		if len(n.Kids) != 1 {
			return value.NilV(), in.errAt(n.Pos, "%s: нужен один аргумент", name)
		}
		v, err := in.eval(n.Kids[0])
		if err != nil {
			return value.NilV(), err
		}
		if name == "str" {
			return value.StrV(v.String()), nil
		}
		return applyCast(v, name, n, in)

	case "bold", "dim", "italic", "underline", "blink", "reverse", "strike",
		"color", "fg", "bg", "style", "reset":
		return in.styleCall(n)
	}

	fn, ok := in.funcs[name]
	if !ok {
		return value.NilV(), in.errAt(n.Pos, "неизвестная функция %s{%d арг.}", name, len(n.Kids))
	}
	if len(n.Kids) != len(fn.Aux) {
		return value.NilV(), in.errAt(n.Pos, "функция %s ожидает %d аргумент(ов), получено %d",
			name, len(fn.Aux), len(n.Kids))
	}
	if in.depth >= maxDepth {
		return value.NilV(), in.errAt(n.Pos, "слишком глубокая рекурсия (%s)", name)
	}
	args := make([]value.Value, len(n.Kids))
	for i, k := range n.Kids {
		v, err := in.eval(k)
		if err != nil {
			return value.NilV(), err
		}
		args[i] = v
	}
	scope := make(map[string]value.Value, len(args))
	for i, p := range fn.Aux {
		scope[p.Name] = args[i]
	}
	old := in.scope
	in.scope = scope
	in.depth++
	err := in.execBlock(fn.Kids[0])
	in.depth--
	in.scope = old
	if err != nil {
		if r, ok := err.(sigReturn); ok {
			return r.v, nil
		}
		return value.NilV(), err
	}
	return value.NilV(), nil
}

func applyCast(v value.Value, typ string, n *ast.Node, in *Interp) (value.Value, error) {
	switch typ {
	case "int":
		i, ok := v.Int64()
		if !ok {
			return value.NilV(), in.errAt(n.Pos, "int: %q не число", v.String())
		}
		return value.IntV(i), nil
	case "float", "double":
		f, ok := v.Float64()
		if !ok {
			return value.NilV(), in.errAt(n.Pos, "%s: %q не число", typ, v.String())
		}
		return value.FloatV(f), nil
	case "char":
		r := []rune(v.String())
		if len(r) == 0 {
			return value.StrV(""), nil
		}
		return value.StrV(string(r[0])), nil
	}
	return v, nil
}

// ---------- файлы ----------

func (in *Interp) path(v value.Value) string {
	p := v.String()
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(in.dir, p)
}

func (in *Interp) fileOp(n *ast.Node) (value.Value, error) {
	name := n.Name
	if len(n.Kids) == 0 {
		return value.NilV(), in.errAt(n.Pos, "%s: нужен путь к файлу", name)
	}
	pv, err := in.eval(n.Kids[0])
	if err != nil {
		return value.NilV(), err
	}
	path := in.path(pv)
	switch name {
	case "openFile":
		b, err := os.ReadFile(path)
		if err != nil {
			return value.NilV(), in.errAt(n.Pos, "openFile: %v", err)
		}
		return value.StrV(string(b)), nil

	case "writeFile", "appendFile":
		if len(n.Kids) < 2 {
			return value.NilV(), in.errAt(n.Pos, "%s: нужны путь и данные", name)
		}
		dv, err := in.eval(n.Kids[1])
		if err != nil {
			return value.NilV(), err
		}
		data := dv.String()
		if name == "writeFile" {
			if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
				return value.NilV(), in.errAt(n.Pos, "writeFile: %v", err)
			}
		} else {
			f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
			if err != nil {
				return value.NilV(), in.errAt(n.Pos, "appendFile: %v", err)
			}
			if _, err := f.WriteString(data); err != nil {
				f.Close()
				return value.NilV(), in.errAt(n.Pos, "appendFile: %v", err)
			}
			f.Close()
		}
		return value.NilV(), nil

	case "delFile":
		if err := os.Remove(path); err != nil {
			return value.NilV(), in.errAt(n.Pos, "delFile: %v", err)
		}
		return value.NilV(), nil

	case "searchFile":
		entries, err := os.ReadDir(path)
		if err != nil {
			return value.NilV(), in.errAt(n.Pos, "searchFile: %v", err)
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
		return value.ListV(items), nil
	}
	return value.NilV(), in.errAt(n.Pos, "неизвестная файловая операция %s", name)
}

// ---------- библиотека style_text ----------

var styleCodes = map[string]string{
	"bold": "1", "dim": "2", "italic": "3", "underline": "4",
	"blink": "5", "reverse": "7", "strike": "9",
}

func wrapStyle(code, text string) value.Value {
	return value.StrV("\x1b[" + code + "m" + text + "\x1b[0m")
}

func (in *Interp) styleCall(n *ast.Node) (value.Value, error) {
	name := n.Name
	if name == "reset" {
		return value.StrV("\x1b[0m"), nil
	}
	if !in.libs["style_text"] {
		return value.NilV(), in.errAt(n.Pos,
			"%s: подключите библиотеку командой inp[style_text]", n.Name)
	}
	if code, ok := styleCodes[name]; ok {
		if len(n.Kids) < 1 {
			return value.NilV(), in.errAt(n.Pos, "%s: нужен текст", name)
		}
		text, err := in.argText(n, 0)
		if err != nil {
			return value.NilV(), err
		}
		return wrapStyle(code, text), nil
	}
	// color{RED,"x"} / fg{...} / bg{...} / style{BOLD,"x"}
	if len(n.Kids) < 2 {
		return value.NilV(), in.errAt(n.Pos, "%s: нужны имя стиля/цвета и текст", name)
	}
	cv, err := in.eval(n.Kids[0])
	if err != nil {
		return value.NilV(), err
	}
	text, err := in.argText(n, 1)
	if err != nil {
		return value.NilV(), err
	}
	key := strings.ToUpper(strings.TrimSpace(cv.String()))
	switch name {
	case "style":
		if code, ok := term.StyleNames[key]; ok {
			return wrapStyle(code, text), nil
		}
		return value.NilV(), in.errAt(n.Pos, "неизвестный стиль %q", cv.String())
	case "color", "fg":
		c := term.ColorCode(key)
		if c < 0 {
			return value.NilV(), in.errAt(n.Pos, "неизвестный цвет %q", cv.String())
		}
		code := "38;5;" + itoa(c)
		if c < 8 {
			code = itoa(30 + c)
		} else if c < 16 {
			code = itoa(90 + c - 8)
		}
		return wrapStyle(code, text), nil
	case "bg":
		c := term.ColorCode(key)
		if c < 0 {
			return value.NilV(), in.errAt(n.Pos, "неизвестный цвет %q", cv.String())
		}
		code := itoa(40 + c)
		if c >= 8 {
			code = itoa(100 + c - 8)
		}
		return wrapStyle(code, text), nil
	}
	return value.NilV(), in.errAt(n.Pos, "неизвестная функция стиля %s", name)
}

func (in *Interp) argText(n *ast.Node, i int) (string, error) {
	if i >= len(n.Kids) {
		return "", nil
	}
	v, err := in.eval(n.Kids[i])
	if err != nil {
		return "", err
	}
	return v.String(), nil
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [12]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		p--
		b[p] = '-'
	}
	return string(b[p:])
}
