// Package parser строит дерево разбора KonLang.
package parser

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/konlang/konlang/internal/ast"
	"github.com/konlang/konlang/internal/lexer"
)

// Error — синтаксическая ошибка с позицией.
type Error struct {
	Pos lexer.Pos
	Msg string
}

func (e *Error) Error() string { return fmt.Sprintf("синтаксис %s: %s", e.Pos, e.Msg) }

type parser struct {
	toks    []lexer.Token
	src     []rune
	i       int
	listCtx bool // внутри set{a={...}} / random_X{...} одиночные {} — список
}

// Parse разбирает исходник программы.
func Parse(src string) (*ast.Node, error) {
	toks, err := lexer.Lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks, src: []rune(src)}
	prog := ast.N(ast.Program, lexer.Pos{Line: 1, Col: 1})
	for !p.at(lexer.EOF) {
		st, err := p.stmt()
		if err != nil {
			return nil, err
		}
		if st != nil {
			prog.Kids = append(prog.Kids, st)
		}
	}
	return prog, nil
}

// ParseFile разбирает файл .kn.
func ParseFile(path string) (*ast.Node, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(string(b))
}

// ---------- вспомогательное ----------

func (p *parser) cur() lexer.Token { return p.toks[p.i] }
func (p *parser) peek(n int) lexer.Token {
	if p.i+n >= len(p.toks) {
		return p.toks[len(p.toks)-1]
	}
	return p.toks[p.i+n]
}
func (p *parser) at(k lexer.Kind) bool { return p.cur().Kind == k }
func (p *parser) atIdent(s string) bool {
	return p.cur().Kind == lexer.Ident && p.cur().Text == s
}
func (p *parser) atOp(s string) bool { return p.cur().Kind == lexer.Op && p.cur().Text == s }
func (p *parser) next() lexer.Token {
	t := p.toks[p.i]
	if p.i < len(p.toks)-1 {
		p.i++
	}
	return t
}
func (p *parser) errf(format string, a ...any) error {
	return &Error{Pos: p.cur().Pos, Msg: fmt.Sprintf(format, a...)}
}
func (p *parser) expect(k lexer.Kind) (lexer.Token, error) {
	if !p.at(k) {
		return lexer.Token{}, p.errf("ожидается %s, найдено %q", k, p.cur().Text)
	}
	return p.next(), nil
}
func (p *parser) expectIdent(s string) error {
	if !p.atIdent(s) {
		return p.errf("ожидается %q, найдено %q", s, p.cur().Text)
	}
	p.next()
	return nil
}

var typeNames = map[string]bool{"int": true, "float": true, "double": true, "char": true}

var fileOps = map[string]bool{
	"openFile": true, "writeFile": true, "appendFile": true,
	"searchFile": true, "delFile": true,
}

// splitCmd отделяет числовой «хвост» слитной команды: cycle5 → (cycle,5).
func splitCmd(name string, prefix string) (string, bool) {
	if !strings.HasPrefix(name, prefix) {
		return "", false
	}
	rest := name[len(prefix):]
	if rest == "" {
		return "", true
	}
	for _, r := range rest {
		if r < '0' || r > '9' {
			return "", false
		}
	}
	return rest, true
}

// ---------- конструкции ----------

func (p *parser) block() (*ast.Node, error) {
	b := ast.N(ast.Block, p.cur().Pos)
	for !p.at(lexer.RBrace) {
		if p.at(lexer.EOF) {
			return nil, p.errf("незакрытый блок: ожидается }")
		}
		st, err := p.stmt()
		if err != nil {
			return nil, err
		}
		if st != nil {
			b.Kids = append(b.Kids, st)
		}
	}
	return b, nil
}

func (p *parser) stmt() (*ast.Node, error) {
	// комментарий * ... *
	if p.atOp("*") {
		p.next()
		for !p.atOp("*") {
			if p.at(lexer.EOF) {
				return nil, p.errf("незакрытый комментарий: ожидается *")
			}
			p.next()
		}
		p.next()
		return nil, nil
	}
	t := p.cur()
	if t.Kind != lexer.Ident {
		// выражение как конструкция (например, вызов функции)
		e, err := p.expr()
		if err != nil {
			return nil, err
		}
		n := ast.N(ast.ExprStmt, t.Pos)
		n.Kids = []*ast.Node{e}
		return n, nil
	}
	name := t.Text
	pos := t.Pos

	switch {
	case name == "print":
		p.next()
		return p.printStmt(pos)
	case strings.HasPrefix(name, "input_"):
		p.next()
		return p.inputStmt(pos, "inp_"+name[len("input_"):])
	case strings.HasPrefix(name, "key_"):
		p.next()
		n := ast.N(ast.Key, pos)
		n.Name = name
		return n, nil
	case strings.HasPrefix(name, "kon_"):
		p.next()
		if _, err := p.expect(lexer.LBrace); err != nil {
			return nil, err
		}
		body, err := p.block()
		if err != nil {
			return nil, err
		}
		p.next() // }
		n := ast.N(ast.Kon, pos)
		n.Name, n.Kids = name, []*ast.Node{body}
		return n, nil
	case strings.HasPrefix(name, "def_"):
		p.next()
		return p.defStmt(pos, name[len("def_"):])
	case strings.HasPrefix(name, "random_"):
		p.next()
		return p.randomStmt(pos, "rand_"+name[len("random_"):])
	case name == "set":
		p.next()
		return p.setStmt(pos)
	case name == "clear":
		p.next()
		return ast.N(ast.Clear, pos), nil
	case name == "sc":
		p.next()
		return ast.N(ast.Break, pos), nil
	case name == "cexit":
		p.next()
		return ast.N(ast.Exit, pos), nil
	case name == "scr_size":
		p.next()
		return ast.N(ast.ScrSize, pos), nil
	case name == "scr_cur":
		p.next()
		return ast.N(ast.ScrCur, pos), nil
	case name == "scr_bg":
		p.next()
		return p.braceCallStmt(pos, ast.ScrBg, "scr_bg")
	case name == "scr_beep":
		p.next()
		return p.braceCallStmt(pos, ast.Beep, "scr_beep")
	case name == "ret":
		p.next()
		n := ast.N(ast.Ret, pos)
		if p.at(lexer.LBracket) {
			e, err := p.bracketExpr()
			if err != nil {
				return nil, err
			}
			n.Kids = []*ast.Node{e}
		}
		return n, nil
	case name == "exec":
		p.next()
		e, err := p.bracketExpr()
		if err != nil {
			return nil, err
		}
		n := ast.N(ast.Exec, pos)
		n.Kids = []*ast.Node{e}
		return n, nil
	case name == "start":
		p.next()
		if _, err := p.expect(lexer.Colon); err != nil {
			return nil, err
		}
		if err := p.expectIdent("exec"); err != nil {
			return nil, err
		}
		e, err := p.bracketExpr()
		if err != nil {
			return nil, err
		}
		n := ast.N(ast.Exec, pos)
		n.Flag = 1 // start:exec — «выгрузить» контейнер
		n.Kids = []*ast.Node{e}
		return n, nil
	case name == "if":
		p.next()
		return p.ifStmt(pos)
	case name == "inp":
		p.next()
		if _, err := p.expect(lexer.LBracket); err != nil {
			return nil, err
		}
		lib, err := p.expect(lexer.Ident)
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(lexer.RBracket); err != nil {
			return nil, err
		}
		n := ast.N(ast.Import, pos)
		n.Name = lib.Text
		return n, nil
	case fileOps[name]:
		p.next()
		return p.braceCallStmt(pos, ast.FileOp, name)
	case name == "math", name == "len":
		e, err := p.expr()
		if err != nil {
			return nil, err
		}
		n := ast.N(ast.ExprStmt, pos)
		n.Kids = []*ast.Node{e}
		return n, nil
	}

	if rest, ok := splitCmd(name, "cycle"); ok {
		p.next()
		return p.cycleStmt(pos, rest)
	}
	if rest, ok := splitCmd(name, "time"); ok {
		p.next()
		n := ast.N(ast.Time, pos)
		if rest != "" {
			v, _ := strconv.ParseFloat(rest, 64)
			num := ast.N(ast.Num, pos)
			num.Num = v
			n.Kids = []*ast.Node{num}
		} else {
			e, err := p.expr()
			if err != nil {
				return nil, err
			}
			n.Kids = []*ast.Node{e}
		}
		return n, nil
	}

	// всё прочее — выражение
	e, err := p.expr()
	if err != nil {
		return nil, err
	}
	n := ast.N(ast.ExprStmt, pos)
	n.Kids = []*ast.Node{e}
	return n, nil
}

func (p *parser) braceCallStmt(pos lexer.Pos, k ast.Kind, name string) (*ast.Node, error) {
	if _, err := p.expect(lexer.LBrace); err != nil {
		return nil, err
	}
	n := ast.N(k, pos)
	n.Name = name
	args, err := p.argList(lexer.RBrace)
	if err != nil {
		return nil, err
	}
	n.Kids = args
	if _, err := p.expect(lexer.RBrace); err != nil {
		return nil, err
	}
	return n, nil
}

func (p *parser) printStmt(pos lexer.Pos) (*ast.Node, error) {
	if _, err := p.expect(lexer.LBrace); err != nil {
		return nil, err
	}
	n := ast.N(ast.Print, pos)
	var groups [][]*ast.Node
	var cur []*ast.Node
	for !p.at(lexer.RBrace) {
		if p.at(lexer.EOF) {
			return nil, p.errf("незакрытый print: ожидается }")
		}
		if p.at(lexer.Comma) {
			p.next()
			groups = append(groups, cur)
			cur = nil
			continue
		}
		before := p.i
		e, err := p.expr()
		if err != nil {
			return nil, err
		}
		if p.i == before {
			return nil, p.errf("не удалось разобрать аргумент print")
		}
		cur = append(cur, e)
	}
	p.next() // }
	groups = append(groups, cur)

	// print{x,y,"текст"} — координатная форма
	if len(groups) >= 3 && isNumericGroup(groups[0]) && isNumericGroup(groups[1]) {
		n.Flag = 1
	}
	for _, g := range groups {
		n.Kids = append(n.Kids, g...)
	}
	if len(n.Kids) == 0 {
		empty := ast.N(ast.Str, pos)
		n.Kids = []*ast.Node{empty}
	}
	return n, nil
}

func isNumericGroup(g []*ast.Node) bool {
	if len(g) != 1 {
		return false
	}
	if g[0].Kind == ast.Num {
		return true
	}
	if g[0].Kind == ast.Unary && len(g[0].Kids) == 1 && g[0].Kids[0].Kind == ast.Num {
		return true
	}
	return false
}

func (p *parser) inputStmt(pos lexer.Pos, reg string) (*ast.Node, error) {
	if _, err := p.expect(lexer.LBrace); err != nil {
		return nil, err
	}
	n := ast.N(ast.Input, pos)
	n.Name = reg
	if p.at(lexer.Ident) && typeNames[p.cur().Text] && p.peek(1).Kind != lexer.Comma && p.peek(1).Kind != lexer.RBrace {
		n.Str = p.next().Text
	}
	prompt, err := p.expr()
	if err != nil {
		return nil, err
	}
	n.Kids = []*ast.Node{prompt}
	if p.at(lexer.Comma) {
		p.next()
		lim, err := p.expr()
		if err != nil {
			return nil, err
		}
		n.Kids = append(n.Kids, lim)
	}
	if _, err := p.expect(lexer.RBrace); err != nil {
		return nil, err
	}
	return n, nil
}

func (p *parser) defStmt(pos lexer.Pos, name string) (*ast.Node, error) {
	if _, err := p.expect(lexer.LBracket); err != nil {
		return nil, err
	}
	n := ast.N(ast.Def, pos)
	n.Name = name
	for !p.at(lexer.RBracket) {
		id, err := p.expect(lexer.Ident)
		if err != nil {
			return nil, err
		}
		param := ast.N(ast.Ident, id.Pos)
		param.Name = id.Text
		n.Aux = append(n.Aux, param)
		if p.at(lexer.Comma) {
			p.next()
			continue
		}
		break
	}
	if _, err := p.expect(lexer.RBracket); err != nil {
		return nil, err
	}
	if len(n.Aux) > 8 {
		return nil, p.errf("функция %s: максимум 8 параметров", name)
	}
	if _, err := p.expect(lexer.LBrace); err != nil {
		return nil, err
	}
	body, err := p.block()
	if err != nil {
		return nil, err
	}
	p.next()
	n.Kids = []*ast.Node{body}
	return n, nil
}

func (p *parser) randomStmt(pos lexer.Pos, reg string) (*ast.Node, error) {
	if _, err := p.expect(lexer.LBrace); err != nil {
		return nil, err
	}
	n := ast.N(ast.Random, pos)
	n.Name = reg
	oldCtx := p.listCtx
	p.listCtx = true
	args, err := p.argList(lexer.RBrace)
	p.listCtx = oldCtx
	if err != nil {
		return nil, err
	}
	p.next()
	n.Kids = args
	return n, nil
}

func (p *parser) setStmt(pos lexer.Pos) (*ast.Node, error) {
	if _, err := p.expect(lexer.LBrace); err != nil {
		return nil, err
	}
	id, err := p.expect(lexer.Ident)
	if err != nil {
		return nil, err
	}
	if !p.atOp("=") {
		return nil, p.errf("ожидается '=' в set{%s=...}", id.Text)
	}
	p.next()
	oldCtx := p.listCtx
	p.listCtx = true
	val, err := p.expr()
	p.listCtx = oldCtx
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.RBrace); err != nil {
		return nil, err
	}
	n := ast.N(ast.Set, pos)
	n.Name = id.Text
	n.Kids = []*ast.Node{val}
	return n, nil
}

func (p *parser) cycleStmt(pos lexer.Pos, digits string) (*ast.Node, error) {
	n := ast.N(ast.Cycle, pos)
	if digits != "" {
		v, _ := strconv.ParseFloat(digits, 64)
		num := ast.N(ast.Num, pos)
		num.Num = v
		n.Kids = []*ast.Node{num, nil}
	} else if p.at(lexer.LBrace) {
		n.Kids = []*ast.Node{nil, nil}
	} else {
		cnt, err := p.expr()
		if err != nil {
			return nil, err
		}
		n.Kids = []*ast.Node{cnt, nil}
	}
	if _, err := p.expect(lexer.LBrace); err != nil {
		return nil, err
	}
	body, err := p.block()
	if err != nil {
		return nil, err
	}
	p.next()
	n.Kids[1] = body
	return n, nil
}

func (p *parser) ifStmt(pos lexer.Pos) (*ast.Node, error) {
	n := ast.N(ast.If, pos)
	cond, err := p.bracketExpr()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.LBrace); err != nil {
		return nil, err
	}
	body, err := p.block()
	if err != nil {
		return nil, err
	}
	p.next()
	n.Kids = append(n.Kids, cond, body)
	for {
		if p.atIdent("eli") {
			p.next()
			c, err := p.bracketExpr()
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(lexer.LBrace); err != nil {
				return nil, err
			}
			b, err := p.block()
			if err != nil {
				return nil, err
			}
			p.next()
			n.Kids = append(n.Kids, c, b)
			continue
		}
		if p.atIdent("els") {
			p.next()
			if _, err := p.expect(lexer.LBrace); err != nil {
				return nil, err
			}
			b, err := p.block()
			if err != nil {
				return nil, err
			}
			p.next()
			n.Aux = []*ast.Node{b}
		}
		break
	}
	return n, nil
}

func (p *parser) bracketExpr() (*ast.Node, error) {
	if _, err := p.expect(lexer.LBracket); err != nil {
		return nil, err
	}
	e, err := p.expr()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.RBracket); err != nil {
		return nil, err
	}
	return e, nil
}

// argList разбирает аргументы через запятую до закрывающего токена.
// Каждый аргумент — последовательность выражений, которые склеиваются
// как строки: appendFile{"log.txt",[inp_0]"\n"}.
func (p *parser) argList(end lexer.Kind) ([]*ast.Node, error) {
	var args []*ast.Node
	for !p.at(end) {
		if p.at(lexer.EOF) {
			return nil, p.errf("незакрытый список аргументов")
		}
		var group []*ast.Node
		for !p.at(end) && !p.at(lexer.Comma) {
			if p.at(lexer.EOF) {
				return nil, p.errf("незакрытый список аргументов")
			}
			before := p.i
			e, err := p.expr()
			if err != nil {
				return nil, err
			}
			if p.i == before {
				return nil, p.errf("не удалось разобрать аргумент")
			}
			group = append(group, e)
		}
		if len(group) == 1 {
			// В литералах списков/random одиночное слово — строка,
			// а не переменная. Для значения переменной используется [имя].
			if p.listCtx && group[0].Kind == ast.Ident {
				lit := ast.N(ast.Str, group[0].Pos)
				lit.Str = group[0].Name
				args = append(args, lit)
			} else {
				args = append(args, group[0])
			}
		} else if len(group) > 1 {
			// В списках «голые слова» через пробел — одна строка:
			// set{menu={Привет,Случайное слово,Выход}}
			allBare := p.listCtx
			for _, g := range group {
				if g.Kind != ast.Ident {
					allBare = false
					break
				}
			}
			if allBare {
				first := group[0].Pos.Off
				lastTok := group[len(group)-1]
				end := lastTok.Pos.Off + len([]rune(lastTok.Name))
				if first >= 0 && end <= len(p.src) && first < end {
					lit := ast.N(ast.Str, group[0].Pos)
					lit.Str = strings.TrimSpace(string(p.src[first:end]))
					args = append(args, lit)
				} else {
					c := ast.N(ast.Concat, group[0].Pos)
					c.Kids = group
					args = append(args, c)
				}
			} else {
				c := ast.N(ast.Concat, group[0].Pos)
				c.Kids = group
				args = append(args, c)
			}
		}
		if p.at(lexer.Comma) {
			p.next()
			continue
		}
		break
	}
	return args, nil
}

// ---------- выражения ----------

func precedence(t lexer.Token) int {
	if t.Kind == lexer.Ident {
		switch t.Text {
		case "or":
			return 1
		case "and":
			return 2
		}
		return 0
	}
	if t.Kind != lexer.Op {
		return 0
	}
	switch t.Text {
	case "==", "!=":
		return 3
	case "<", ">", "<=", ">=":
		return 4
	case "+", "-":
		return 5
	case "*", "/", "%":
		return 6
	}
	return 0
}

func (p *parser) expr() (*ast.Node, error) { return p.binary(1) }

func (p *parser) binary(min int) (*ast.Node, error) {
	left, err := p.unary()
	if err != nil {
		return nil, err
	}
	for {
		pr := precedence(p.cur())
		if pr == 0 || pr < min {
			return left, nil
		}
		op := p.next()
		right, err := p.binary(pr + 1)
		if err != nil {
			return nil, err
		}
		n := ast.N(ast.Binary, op.Pos)
		n.Name = op.Text
		n.Kids = []*ast.Node{left, right}
		left = n
	}
}

func (p *parser) unary() (*ast.Node, error) {
	if p.atOp("-") || p.atOp("+") {
		op := p.next()
		e, err := p.unary()
		if err != nil {
			return nil, err
		}
		if op.Text == "+" {
			return e, nil
		}
		n := ast.N(ast.Unary, op.Pos)
		n.Name = "-"
		n.Kids = []*ast.Node{e}
		return n, nil
	}
	return p.primary()
}

func (p *parser) primary() (*ast.Node, error) {
	t := p.cur()
	switch t.Kind {
	case lexer.String:
		p.next()
		n := ast.N(ast.Str, t.Pos)
		n.Str = t.Text
		return n, nil
	case lexer.Number:
		p.next()
		v, err := strconv.ParseFloat(t.Text, 64)
		if err != nil {
			return nil, &Error{Pos: t.Pos, Msg: "некорректное число " + t.Text}
		}
		n := ast.N(ast.Num, t.Pos)
		n.Num = v
		if !strings.Contains(t.Text, ".") {
			n.Flag = 1 // целый литерал
		}
		return n, nil
	case lexer.LParen:
		p.next()
		e, err := p.expr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(lexer.RParen); err != nil {
			return nil, err
		}
		return e, nil
	case lexer.LBracket:
		p.next()
		e, err := p.expr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(lexer.RBracket); err != nil {
			return nil, err
		}
		n := ast.N(ast.Ref, t.Pos)
		n.Kids = []*ast.Node{e}
		return n, nil
	case lexer.LBrace:
		p.next()
		items, err := p.argList(lexer.RBrace)
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(lexer.RBrace); err != nil {
			return nil, err
		}
		if len(items) == 1 && p.atOp("*") {
			p.next()
			cnt, err := p.unary()
			if err != nil {
				return nil, err
			}
			n := ast.N(ast.Repeat, t.Pos)
			n.Kids = []*ast.Node{items[0], cnt}
			return n, nil
		}
		// {"═"*10} или {"текст"{" "*3}} — группировка/повтор, а не список
		if len(items) == 1 && !p.listCtx {
			return items[0], nil
		}
		n := ast.N(ast.List, t.Pos)
		n.Kids = items
		return n, nil
	case lexer.Ident:
		return p.identExpr()
	}
	return nil, p.errf("неожиданный токен %q в выражении", t.Text)
}

func (p *parser) identExpr() (*ast.Node, error) {
	t := p.next()
	name := t.Text

	// math{int a+b}
	if name == "math" && p.at(lexer.LBrace) {
		p.next()
		n := ast.N(ast.Call, t.Pos)
		n.Name = "math"
		if p.at(lexer.Ident) && typeNames[p.cur().Text] {
			n.Str = p.next().Text
		}
		e, err := p.expr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(lexer.RBrace); err != nil {
			return nil, err
		}
		n.Kids = []*ast.Node{e}
		return n, nil
	}
	// len[x]
	if name == "len" && p.at(lexer.LBracket) {
		e, err := p.bracketExpr()
		if err != nil {
			return nil, err
		}
		n := ast.N(ast.Call, t.Pos)
		n.Name = "len"
		n.Kids = []*ast.Node{e}
		return n, nil
	}
	// name{args}
	if p.at(lexer.LBrace) {
		p.next()
		oldCtx := p.listCtx
		p.listCtx = false
		args, err := p.argList(lexer.RBrace)
		p.listCtx = oldCtx
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(lexer.RBrace); err != nil {
			return nil, err
		}
		n := ast.N(ast.Call, t.Pos)
		n.Name = name
		n.Kids = args
		return n, nil
	}
	// список.индекс
	if p.at(lexer.Dot) {
		p.next()
		base := ast.N(ast.Ident, t.Pos)
		base.Name = name
		var idx *ast.Node
		switch p.cur().Kind {
		case lexer.Number:
			it := p.next()
			v, _ := strconv.ParseFloat(it.Text, 64)
			idx = ast.N(ast.Num, it.Pos)
			idx.Num = v
			idx.Flag = 1
		case lexer.LBracket:
			e, err := p.bracketExpr()
			if err != nil {
				return nil, err
			}
			idx = e
		case lexer.Ident:
			it := p.next()
			idx = ast.N(ast.Ident, it.Pos)
			idx.Name = it.Text
		default:
			return nil, p.errf("ожидается индекс списка после '.'")
		}
		n := ast.N(ast.Index, t.Pos)
		n.Kids = []*ast.Node{base, idx}
		return n, nil
	}
	n := ast.N(ast.Ident, t.Pos)
	n.Name = name
	return n, nil
}
