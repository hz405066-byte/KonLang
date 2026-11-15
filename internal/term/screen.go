package term

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// ErrInterrupt — Ctrl+C во время ввода строки.
var ErrInterrupt = errors.New("прервано пользователем (ctrl+c)")

func itoa(i int) string { return strconv.Itoa(i) }

// Screen — интерфейс экрана, который использует интерпретатор.
type Screen interface {
	Size() (int, int)
	Cursor() (int, int)
	Print(s string)
	PrintAt(x, y int, s string)
	Clear()
	SetBg(color int)
	Flush()
	ReadLine(prompt string, limit int) (string, error)
	ReadKey() (string, error)
	Beep(freq int)
	Close()
}

// Options — параметры создания экрана.
type Options struct {
	ForceScreen bool // --screen: буферный режим даже без терминала
	ForceLine   bool // принудительно построчный режим (тесты, --plain)
	NoColor     bool
	Width       int
	Height      int
	In          io.Reader
	Out         io.Writer
}

// New выбирает подходящую реализацию экрана.
func New(o Options) Screen {
	if o.Out == nil {
		o.Out = os.Stdout
	}
	if o.In == nil {
		o.In = os.Stdin
	}
	w, h := o.Width, o.Height
	if w <= 0 || h <= 0 {
		dw, dh := terminalSize()
		if w <= 0 {
			w = dw
		}
		if h <= 0 {
			h = dh
		}
	}
	if !o.ForceLine && isTTY() {
		if s := newNativeScreen(w, h, o); s != nil {
			return s
		}
	}
	if o.ForceScreen {
		return newDumpScreen(w, h, o)
	}
	return newLineScreen(o)
}

// ---------- построчный режим ----------

type lineScreen struct {
	out  io.Writer
	in   *bufio.Reader
	w, h int
	cx   int
	cy   int
	bg   int
}

func newLineScreen(o Options) Screen {
	w, h := o.Width, o.Height
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 25
	}
	return &lineScreen{out: o.Out, in: bufio.NewReader(o.In), w: w, h: h, bg: -1}
}

func (s *lineScreen) Size() (int, int)   { return s.w, s.h }
func (s *lineScreen) Cursor() (int, int) { return s.cx, s.cy }
func (s *lineScreen) Print(text string) {
	t := StripANSI(text)
	if strings.HasSuffix(t, "\n") {
		fmt.Fprint(s.out, t)
	} else {
		fmt.Fprint(s.out, t+"\n")
	}
	s.cy += strings.Count(t, "\n")
	if !strings.HasSuffix(t, "\n") {
		s.cy++
	}
	s.cx = 0
}
func (s *lineScreen) PrintAt(x, y int, text string) { s.Print(text) }
func (s *lineScreen) Clear() {
	s.cx, s.cy = 0, 0
}
func (s *lineScreen) SetBg(c int) { s.bg = c }
func (s *lineScreen) Flush()      {}
func (s *lineScreen) Beep(freq int) {
	fmt.Fprint(s.out, "\a")
}
func (s *lineScreen) Close() {}

func (s *lineScreen) ReadLine(prompt string, limit int) (string, error) {
	if prompt != "" {
		fmt.Fprint(s.out, StripANSI(prompt))
	}
	line, err := s.in.ReadString('\n')
	if err != nil && line == "" {
		return "", io.EOF
	}
	line = strings.TrimRight(line, "\r\n")
	if limit > 0 {
		r := []rune(line)
		if len(r) > limit {
			line = string(r[:limit])
		}
	}
	// эхо ввода: в построчном режиме воспроизводим то, что видно в терминале
	fmt.Fprint(s.out, line+"\n")
	s.cx = 0
	s.cy++
	return line, nil
}

func (s *lineScreen) ReadKey() (string, error) {
	r, _, err := s.in.ReadRune()
	if err != nil {
		return "", io.EOF
	}
	return keyFromRune(r), nil
}

func keyFromRune(r rune) string {
	switch r {
	case '\n', '\r':
		return "enter"
	case ' ':
		return "space"
	case '\t':
		return "tab"
	case 27:
		return "esc"
	case 8, 127:
		return "backspace"
	case 3:
		return "ctrl+c"
	case 4:
		return "ctrl+d"
	}
	if r < 32 {
		return "ctrl+" + string(rune('a'+r-1))
	}
	return string(r)
}

// ---------- буферный режим без терминала (--screen) ----------

type dumpScreen struct {
	buf *Buffer
	o   Options
	in  *bufio.Reader
}

func newDumpScreen(w, h int, o Options) Screen {
	return &dumpScreen{buf: NewBuffer(w, h), o: o, in: bufio.NewReader(o.In)}
}

func (s *dumpScreen) Size() (int, int)   { return s.buf.W, s.buf.H }
func (s *dumpScreen) Cursor() (int, int) { return s.buf.CX, s.buf.CY }
func (s *dumpScreen) Print(text string) {
	s.buf.WriteString(text)
	if !strings.HasSuffix(text, "\n") {
		s.buf.newline()
	}
}
func (s *dumpScreen) PrintAt(x, y int, text string) { s.buf.WriteAt(x, y, text) }
func (s *dumpScreen) Clear()                        { s.buf.Clear() }
func (s *dumpScreen) SetBg(c int) {
	s.buf.Fill.Bg = c
	s.buf.Clear()
}
func (s *dumpScreen) Flush()        {}
func (s *dumpScreen) Beep(freq int) {}
func (s *dumpScreen) Close() {
	fmt.Fprint(s.o.Out, s.buf.Text())
}
func (s *dumpScreen) ReadLine(prompt string, limit int) (string, error) {
	s.buf.WriteString(prompt)
	line, err := s.in.ReadString('\n')
	if err != nil && line == "" {
		return "", io.EOF
	}
	line = strings.TrimRight(line, "\r\n")
	if limit > 0 {
		r := []rune(line)
		if len(r) > limit {
			line = string(r[:limit])
		}
	}
	s.buf.WriteString(line)
	s.buf.newline()
	return line, nil
}
func (s *dumpScreen) ReadKey() (string, error) {
	r, _, err := s.in.ReadRune()
	if err != nil {
		return "", io.EOF
	}
	return keyFromRune(r), nil
}
