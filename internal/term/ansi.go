//go:build linux || darwin

package term

import (
	"bufio"
	"errors"
	"io"
	"strings"
)

type ansiScreen struct {
	buf  *Buffer
	prev *Buffer
	out  *bufio.Writer
	kr   keyReader
}

func newNativeScreen(w, h int, o Options) Screen {
	s := &ansiScreen{
		buf:  NewBuffer(w, h),
		prev: NewBuffer(w, h),
		out:  bufio.NewWriter(o.Out),
	}
	s.kr = keyReader{blocking: readByte, poll: readByteTimeout}
	if err := enterRaw(); err != nil {
		return nil
	}
	// альтернативный экран, очистка, курсор в начало
	s.out.WriteString("\x1b[?1049h\x1b[0m\x1b[2J\x1b[H")
	s.out.Flush()
	s.prev.Clear()
	for i := range s.prev.Cells {
		s.prev.Cells[i].Ch = 0 // форсируем полную перерисовку
	}
	return s
}

func (s *ansiScreen) Size() (int, int)   { return s.buf.W, s.buf.H }
func (s *ansiScreen) Cursor() (int, int) { return s.buf.CX, s.buf.CY }

func (s *ansiScreen) Print(text string) {
	s.buf.WriteString(text)
	if !strings.HasSuffix(text, "\n") {
		s.buf.newline()
	}
}
func (s *ansiScreen) PrintAt(x, y int, text string) { s.buf.WriteAt(x, y, text) }
func (s *ansiScreen) Clear()                        { s.buf.Clear() }
func (s *ansiScreen) SetBg(c int) {
	s.buf.Fill.Bg = c
	s.buf.Clear()
}
func (s *ansiScreen) Beep(freq int) { s.out.WriteString("\a"); s.out.Flush() }

// Flush рисует только изменившиеся знакоместа.
func (s *ansiScreen) Flush() {
	w, h := s.buf.W, s.buf.H
	var sb strings.Builder
	last := Attr{Fg: -2, Bg: -2}
	for y := 0; y < h; y++ {
		x := 0
		for x < w {
			i := y*w + x
			if s.buf.Cells[i] == s.prev.Cells[i] {
				x++
				continue
			}
			sb.WriteString(cursorTo(x, y))
			for x < w {
				i = y*w + x
				c := s.buf.Cells[i]
				if c == s.prev.Cells[i] {
					break
				}
				if c.A != last {
					sb.WriteString(c.A.SGR())
					last = c.A
				}
				sb.WriteRune(c.Ch)
				s.prev.Cells[i] = c
				x++
			}
		}
	}
	sb.WriteString("\x1b[0m")
	sb.WriteString(cursorTo(s.buf.CX, s.buf.CY))
	s.out.WriteString(sb.String())
	s.out.Flush()
}

func cursorTo(x, y int) string {
	return "\x1b[" + itoa(y+1) + ";" + itoa(x+1) + "H"
}

func (s *ansiScreen) Close() {
	s.out.WriteString("\x1b[0m\x1b[?25h\x1b[?1049l")
	s.out.Flush()
	exitRaw()
}

func (s *ansiScreen) ReadLine(prompt string, limit int) (string, error) {
	s.buf.WriteString(prompt)
	sx, sy := s.buf.CX, s.buf.CY
	var text []rune
	s.Flush()
	for {
		key, err := s.kr.readKey()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return "", io.EOF
			}
			return "", err
		}
		switch key {
		case "enter":
			s.buf.newline()
			s.Flush()
			return string(text), nil
		case "ctrl+c":
			return "", ErrInterrupt
		case "backspace":
			if len(text) > 0 {
				text = text[:len(text)-1]
			}
		default:
			if len([]rune(key)) == 1 && key >= " " {
				if limit <= 0 || len(text) < limit {
					text = append(text, []rune(key)...)
				}
			}
		}
		line := string(text) + " "
		s.buf.WriteAt(sx, sy, line)
		s.buf.CX, s.buf.CY = sx+len(text), sy
		s.Flush()
	}
}

func (s *ansiScreen) ReadKey() (string, error) {
	s.Flush()
	return s.kr.readKey()
}
