// Package lexer разбивает исходник KonLang (.kn) на токены.
//
// Особенность синтаксиса KonLang: команда и аргументы пишутся слитно
// (print{"x"}, cycle5{...}, time100), поэтому лексер максимально «тупой»,
// а всю смысловую разборку делает парсер.
package lexer

import (
	"fmt"
	"strings"
	"unicode"
)

// Kind — тип токена.
type Kind int

const (
	EOF Kind = iota
	Ident
	Number
	String
	LBrace   // {
	RBrace   // }
	LBracket // [
	RBracket // ]
	LParen   // (
	RParen   // )
	Comma    // ,
	Dot      // .
	Colon    // :
	Op       // + - * / % = == != < > <= >=
)

func (k Kind) String() string {
	switch k {
	case EOF:
		return "EOF"
	case Ident:
		return "имя"
	case Number:
		return "число"
	case String:
		return "строка"
	case LBrace:
		return "{"
	case RBrace:
		return "}"
	case LBracket:
		return "["
	case RBracket:
		return "]"
	case LParen:
		return "("
	case RParen:
		return ")"
	case Comma:
		return ","
	case Dot:
		return "."
	case Colon:
		return ":"
	case Op:
		return "оператор"
	}
	return "?"
}

// Pos — позиция в исходнике (1-based строка/колонка).
type Pos struct {
	Line int
	Col  int
	Off  int
}

func (p Pos) String() string { return fmt.Sprintf("%d:%d", p.Line, p.Col) }

// Token — один токен.
type Token struct {
	Kind Kind
	Text string // для String — уже раскодированное значение
	Raw  string
	Pos  Pos
}

// Error — лексическая ошибка с позицией.
type Error struct {
	Pos Pos
	Msg string
}

func (e *Error) Error() string { return fmt.Sprintf("лексер %s: %s", e.Pos, e.Msg) }

type lexer struct {
	src  []rune
	i    int
	line int
	col  int
	out  []Token
}

// Lex разбирает исходник на токены.
func Lex(src string) ([]Token, error) {
	l := &lexer{src: []rune(src), line: 1, col: 1}
	if err := l.run(); err != nil {
		return nil, err
	}
	l.out = append(l.out, Token{Kind: EOF, Pos: l.pos()})
	return l.out, nil
}

func (l *lexer) pos() Pos { return Pos{Line: l.line, Col: l.col, Off: l.i} }

func (l *lexer) peek(n int) rune {
	if l.i+n >= len(l.src) {
		return 0
	}
	return l.src[l.i+n]
}

func (l *lexer) adv() rune {
	c := l.src[l.i]
	l.i++
	if c == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return c
}

func (l *lexer) emit(k Kind, text string, p Pos) {
	l.out = append(l.out, Token{Kind: k, Text: text, Pos: p})
}

func isIdentStart(c rune) bool {
	return c == '_' || unicode.IsLetter(c)
}
func isIdentPart(c rune) bool {
	return c == '_' || unicode.IsLetter(c) || unicode.IsDigit(c)
}
func isDigit(c rune) bool { return c >= '0' && c <= '9' }

func (l *lexer) run() error {
	for l.i < len(l.src) {
		c := l.peek(0)
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			l.adv()
		case c == '"':
			if err := l.lexString(); err != nil {
				return err
			}
		case isDigit(c):
			l.lexNumber()
		case isIdentStart(c):
			p := l.pos()
			var b strings.Builder
			for l.i < len(l.src) && isIdentPart(l.peek(0)) {
				b.WriteRune(l.adv())
			}
			l.emit(Ident, b.String(), p)
		default:
			if err := l.lexPunct(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (l *lexer) lexString() error {
	p := l.pos()
	l.adv() // открывающая кавычка
	var b strings.Builder
	for {
		if l.i >= len(l.src) {
			return &Error{Pos: p, Msg: "незакрытая строка (ожидается \")"}
		}
		c := l.adv()
		if c == '"' {
			break
		}
		if c == '\\' {
			if l.i >= len(l.src) {
				return &Error{Pos: p, Msg: "незавершённая escape-последовательность"}
			}
			e := l.adv()
			switch e {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case '0':
				b.WriteByte(0)
			case 'e':
				b.WriteByte(27)
			case '\\':
				b.WriteByte('\\')
			case '"':
				b.WriteByte('"')
			default:
				b.WriteRune('\\')
				b.WriteRune(e)
			}
			continue
		}
		b.WriteRune(c)
	}
	l.emit(String, b.String(), p)
	return nil
}

func (l *lexer) lexNumber() {
	p := l.pos()
	var b strings.Builder
	for l.i < len(l.src) && isDigit(l.peek(0)) {
		b.WriteRune(l.adv())
	}
	// дробная часть: 3.14 (но «список.0» разбирается как Ident + Dot + Number)
	if l.peek(0) == '.' && isDigit(l.peek(1)) {
		b.WriteRune(l.adv())
		for l.i < len(l.src) && isDigit(l.peek(0)) {
			b.WriteRune(l.adv())
		}
	}
	l.emit(Number, b.String(), p)
}

func (l *lexer) lexPunct() error {
	p := l.pos()
	c := l.adv()
	switch c {
	case '{':
		l.emit(LBrace, "{", p)
	case '}':
		l.emit(RBrace, "}", p)
	case '[':
		l.emit(LBracket, "[", p)
	case ']':
		l.emit(RBracket, "]", p)
	case '(':
		l.emit(LParen, "(", p)
	case ')':
		l.emit(RParen, ")", p)
	case ',':
		l.emit(Comma, ",", p)
	case ':':
		l.emit(Colon, ":", p)
	case '.':
		l.emit(Dot, ".", p)
	case '+', '-', '*', '/', '%', '=', '<', '>', '!':
		if l.i < len(l.src) && l.peek(0) == '=' {
			l.adv()
			l.emit(Op, string(c)+"=", p)
		} else {
			if c == '!' {
				return &Error{Pos: p, Msg: "одиночный '!' не поддерживается, используйте !="}
			}
			l.emit(Op, string(c), p)
		}
	default:
		return &Error{Pos: p, Msg: fmt.Sprintf("неизвестный символ %q", string(c))}
	}
	return nil
}
