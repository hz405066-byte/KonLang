// Package ast описывает дерево разбора KonLang.
//
// Узлы намеренно сделаны «универсальными» (Kind + поля), это заметно
// упрощает и интерпретатор, и компилятор байт-кода.
package ast

import "github.com/konlang/konlang/internal/lexer"

// Kind — вид узла.
type Kind int

const (
	// --- конструкции ---
	Program Kind = iota
	Block
	Kon     // kon_X{...}
	Def     // def_X[a,b]{...}
	Print   // print{...}
	Input   // input_X{...}
	Key     // key_X
	Clear   // clear
	Random  // random_X{a,b,c}
	Set     // set{a=...}
	Exec    // exec[kon_X] / start:exec[kon_X]
	Cycle   // cycle[N]{...}
	If      // if[]{} eli[]{} els{}
	Ret     // ret[x] / ret
	Break   // sc
	Time    // timeN
	Exit    // cexit
	Import  // inp[lib]
	ScrSize // scr_size
	ScrCur  // scr_cur
	ScrBg   // scr_bg{COLOR}
	Beep    // scr_beep{freq}
	FileOp  // openFile / writeFile / appendFile / searchFile / delFile
	ExprStmt

	// --- выражения ---
	Str    // "текст"
	Num    // 12 / 3.5
	Ident  // имя (переменная или «голое слово»)
	Ref    // [выражение]
	Call   // name{args}
	List   // {a,b,c}
	Repeat // {expr*count}
	Binary // a+b, a==b, a and b ...
	Unary  // -a
	Index  // список.индекс
	Concat // склейка нескольких выражений подряд
)

var kindNames = map[Kind]string{
	Program: "Program", Block: "Block", Kon: "Kon", Def: "Def", Print: "Print",
	Input: "Input", Key: "Key", Clear: "Clear", Random: "Random", Set: "Set",
	Exec: "Exec", Cycle: "Cycle", If: "If", Ret: "Ret", Break: "Break",
	Time: "Time", Exit: "Exit", Import: "Import", ScrSize: "ScrSize",
	ScrCur: "ScrCur", ScrBg: "ScrBg", Beep: "Beep", FileOp: "FileOp",
	ExprStmt: "ExprStmt", Str: "Str", Num: "Num", Ident: "Ident", Ref: "Ref",
	Call: "Call", List: "List", Repeat: "Repeat", Binary: "Binary",
	Unary: "Unary", Index: "Index", Concat: "Concat",
}

func (k Kind) String() string {
	if s, ok := kindNames[k]; ok {
		return s
	}
	return "?"
}

// Node — узел дерева.
type Node struct {
	Kind Kind
	Pos  lexer.Pos

	Name string  // имя команды/переменной/оператора
	Str  string  // строковый литерал или тип (int/float/double/char)
	Num  float64 // числовой литерал
	Flag int     // служебный флаг (print с координатами, число ветвей if и т.п.)

	Kids []*Node // основные дочерние узлы
	Aux  []*Node // дополнительные (параметры def, тело els)
}

// N — короткий конструктор.
func N(k Kind, pos lexer.Pos) *Node { return &Node{Kind: k, Pos: pos} }

// Dump печатает дерево (для `konlang ast`).
func Dump(n *Node, indent string, out *[]string) {
	if n == nil {
		*out = append(*out, indent+"<nil>")
		return
	}
	s := indent + n.Kind.String()
	if n.Name != "" {
		s += " " + n.Name
	}
	if n.Kind == Str {
		s += " \"" + n.Str + "\""
	}
	if n.Kind == Num {
		s += " " + trimNum(n.Num)
	}
	if n.Str != "" && n.Kind != Str {
		s += " :" + n.Str
	}
	*out = append(*out, s)
	for _, k := range n.Kids {
		Dump(k, indent+"  ", out)
	}
	for _, k := range n.Aux {
		Dump(k, indent+"  ~ ", out)
	}
}

func trimNum(f float64) string {
	if f == float64(int64(f)) {
		return itoa(int64(f))
	}
	return ftoa(f)
}
