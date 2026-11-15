// Package value описывает динамические значения KonLang.
package value

import (
	"strconv"
	"strings"

	"github.com/konlang/konlang/internal/ast"
)

// Kind — тип значения.
type Kind int

const (
	Nil Kind = iota
	Int
	Float
	Str
	List
	Code
)

func (k Kind) String() string {
	switch k {
	case Nil:
		return "nil"
	case Int:
		return "int"
	case Float:
		return "float"
	case Str:
		return "str"
	case List:
		return "list"
	case Code:
		return "code"
	}
	return "?"
}

// Container — содержимое контейнера kon_X.
type Container struct {
	Name string
	Body *ast.Node
}

// Value — универсальное значение.
type Value struct {
	Kind Kind
	I    int64
	F    float64
	S    string
	L    []Value
	C    *Container
}

// Конструкторы.
func NilV() Value              { return Value{Kind: Nil} }
func IntV(i int64) Value       { return Value{Kind: Int, I: i} }
func FloatV(f float64) Value   { return Value{Kind: Float, F: f} }
func StrV(s string) Value      { return Value{Kind: Str, S: s} }
func ListV(l []Value) Value    { return Value{Kind: List, L: l} }
func CodeV(c *Container) Value { return Value{Kind: Code, C: c} }

// Num делает целое, если значение целое.
func Num(f float64) Value {
	if f == float64(int64(f)) {
		return IntV(int64(f))
	}
	return FloatV(f)
}

// FormatFloat печатает дробное число как в справочнике: до 4 знаков,
// лишние нули убираются (3.3333, 2.5, 12).
func FormatFloat(f float64) string {
	s := strconv.FormatFloat(f, 'f', 4, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimRight(s, ".")
	}
	if s == "-0" {
		s = "0"
	}
	return s
}

// String — текстовое представление значения.
func (v Value) String() string {
	switch v.Kind {
	case Nil:
		return ""
	case Int:
		return strconv.FormatInt(v.I, 10)
	case Float:
		return FormatFloat(v.F)
	case Str:
		return v.S
	case List:
		parts := make([]string, len(v.L))
		for i, e := range v.L {
			parts[i] = e.String()
		}
		return "{" + strings.Join(parts, ",") + "}"
	case Code:
		if v.C != nil {
			return "[" + v.C.Name + "]"
		}
		return "[code]"
	}
	return ""
}

// Truth — истинность значения.
func (v Value) Truth() bool {
	switch v.Kind {
	case Nil:
		return false
	case Int:
		return v.I != 0
	case Float:
		return v.F != 0
	case Str:
		return v.S != "" && v.S != "0"
	case List:
		return len(v.L) > 0
	case Code:
		return v.C != nil
	}
	return false
}

// IsNum — число ли это.
func (v Value) IsNum() bool { return v.Kind == Int || v.Kind == Float }

// Float64 приводит значение к дробному числу.
func (v Value) Float64() (float64, bool) {
	switch v.Kind {
	case Int:
		return float64(v.I), true
	case Float:
		return v.F, true
	case Str:
		s := strings.TrimSpace(v.S)
		if s == "" {
			return 0, false
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, false
		}
		return f, true
	}
	return 0, false
}

// Int64 приводит значение к целому.
func (v Value) Int64() (int64, bool) {
	switch v.Kind {
	case Int:
		return v.I, true
	case Float:
		return int64(v.F), true
	case Str:
		s := strings.TrimSpace(v.S)
		if i, err := strconv.ParseInt(s, 10, 64); err == nil {
			return i, true
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return int64(f), true
		}
	}
	return 0, false
}

// Compare сравнивает значения: -1, 0, 1.
func Compare(a, b Value) int {
	if a.IsNum() && b.IsNum() {
		if a.Kind == Int && b.Kind == Int {
			switch {
			case a.I < b.I:
				return -1
			case a.I > b.I:
				return 1
			}
			return 0
		}
		af, _ := a.Float64()
		bf, _ := b.Float64()
		switch {
		case af < bf:
			return -1
		case af > bf:
			return 1
		}
		return 0
	}
	if a.Kind == Str && b.IsNum() {
		if bf, ok := a.Float64(); ok {
			av := FloatV(bf)
			return Compare(av, b)
		}
	}
	if b.Kind == Str && a.IsNum() {
		if af, ok := b.Float64(); ok {
			bv := FloatV(af)
			return Compare(a, bv)
		}
	}
	as, bs := a.String(), b.String()
	return strings.Compare(as, bs)
}

// Equal — равенство значений.
func Equal(a, b Value) bool { return Compare(a, b) == 0 }
