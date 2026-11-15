//go:build windows

package term

import (
	"encoding/binary"
	"errors"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

var (
	kernel32                       = syscall.NewLazyDLL("kernel32.dll")
	procGetStdHandle               = kernel32.NewProc("GetStdHandle")
	procGetConsoleMode             = kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode             = kernel32.NewProc("SetConsoleMode")
	procGetConsoleScreenBufferInfo = kernel32.NewProc("GetConsoleScreenBufferInfo")
	procWriteConsoleOutputW        = kernel32.NewProc("WriteConsoleOutputW")
	procSetConsoleCursorPosition   = kernel32.NewProc("SetConsoleCursorPosition")
	procReadConsoleInputW          = kernel32.NewProc("ReadConsoleInputW")
	procBeep                       = kernel32.NewProc("Beep")
)

type coord struct{ X, Y int16 }
type smallRect struct{ Left, Top, Right, Bottom int16 }
type charInfo struct {
	Char uint16
	Attr uint16
}
type consoleScreenBufferInfo struct {
	Size              coord
	CursorPosition    coord
	Attributes        uint16
	Window            smallRect
	MaximumWindowSize coord
}

func stdHandle(id int32) syscall.Handle {
	h, _, _ := procGetStdHandle.Call(uintptr(id))
	return syscall.Handle(h)
}

func outHandle() syscall.Handle { return stdHandle(-11) }
func inHandle() syscall.Handle  { return stdHandle(-10) }

func isTTY() bool {
	var mode uint32
	r, _, _ := procGetConsoleMode.Call(uintptr(outHandle()), uintptr(unsafe.Pointer(&mode)))
	return r != 0
}

func terminalSize() (int, int) {
	var info consoleScreenBufferInfo
	r, _, _ := procGetConsoleScreenBufferInfo.Call(uintptr(outHandle()), uintptr(unsafe.Pointer(&info)))
	if r != 0 {
		w := int(info.Window.Right-info.Window.Left) + 1
		h := int(info.Window.Bottom-info.Window.Top) + 1
		if w > 0 && h > 0 {
			return w, h
		}
	}
	return 80, 25
}

// ansiToWin переводит ANSI-цвет в атрибуты консоли Windows.
var ansiToWin = [8]uint16{0, 4, 2, 6, 1, 5, 3, 7}

func winAttr(a Attr) uint16 {
	var v uint16 = 7 // светло-серый на чёрном
	if a.Fg >= 0 {
		f := a.Fg
		if f < 8 {
			v = (v &^ 0x0f) | ansiToWin[f]
		} else {
			v = (v &^ 0x0f) | ansiToWin[f-8] | 8
		}
	}
	if a.Bg >= 0 {
		b := a.Bg
		var bg uint16
		if b < 8 {
			bg = ansiToWin[b]
		} else {
			bg = ansiToWin[b-8] | 8
		}
		v = (v &^ 0xf0) | (bg << 4)
	}
	if a.Bold {
		v |= 8
	}
	if a.Dim {
		v &^= 8
	}
	if a.Under {
		v |= 0x8000 // COMMON_LVB_UNDERSCORE
	}
	if a.Reverse {
		v |= 0x4000 // COMMON_LVB_REVERSE_VIDEO
	}
	return v
}

type winScreen struct {
	buf     *Buffer
	prev    *Buffer
	cells   []charInfo
	savedIn uint32
}

func newNativeScreen(w, h int, o Options) Screen {
	s := &winScreen{buf: NewBuffer(w, h), prev: NewBuffer(w, h)}
	s.cells = make([]charInfo, w*h)
	procGetConsoleMode.Call(uintptr(inHandle()), uintptr(unsafe.Pointer(&s.savedIn)))
	// сырой ввод: без эха, без построчной буферизации, Ctrl+C приходит как клавиша
	mode := s.savedIn &^ uint32(0x0001|0x0002|0x0004)
	procSetConsoleMode.Call(uintptr(inHandle()), uintptr(mode))
	s.Clear()
	s.Flush()
	return s
}

func (s *winScreen) Size() (int, int)   { return s.buf.W, s.buf.H }
func (s *winScreen) Cursor() (int, int) { return s.buf.CX, s.buf.CY }
func (s *winScreen) Print(text string) {
	s.buf.WriteString(text)
	if !strings.HasSuffix(text, "\n") {
		s.buf.newline()
	}
}
func (s *winScreen) PrintAt(x, y int, text string) { s.buf.WriteAt(x, y, text) }
func (s *winScreen) Clear()                        { s.buf.Clear() }
func (s *winScreen) SetBg(c int) {
	s.buf.Fill.Bg = c
	s.buf.Clear()
}
func (s *winScreen) Beep(freq int) {
	if freq < 37 {
		freq = 37
	}
	if freq > 32767 {
		freq = 32767
	}
	procBeep.Call(uintptr(freq), 120)
}

func (s *winScreen) Flush() {
	w, h := s.buf.W, s.buf.H
	minRow, maxRow := h, -1
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*w + x
			c := s.buf.Cells[i]
			if c != s.prev.Cells[i] {
				if y < minRow {
					minRow = y
				}
				if y > maxRow {
					maxRow = y
				}
			}
			u := utf16.Encode([]rune{c.Ch})
			ch := uint16(' ')
			if len(u) > 0 {
				ch = u[0]
			}
			s.cells[i] = charInfo{Char: ch, Attr: winAttr(c.A)}
			s.prev.Cells[i] = c
		}
	}
	if maxRow >= 0 {
		size := coord{X: int16(w), Y: int16(h)}
		from := coord{X: 0, Y: int16(minRow)}
		region := smallRect{Left: 0, Top: int16(minRow), Right: int16(w - 1), Bottom: int16(maxRow)}
		procWriteConsoleOutputW.Call(uintptr(outHandle()),
			uintptr(unsafe.Pointer(&s.cells[0])),
			uintptr(*(*uint32)(unsafe.Pointer(&size))),
			uintptr(*(*uint32)(unsafe.Pointer(&from))),
			uintptr(unsafe.Pointer(&region)))
	}
	pos := coord{X: int16(s.buf.CX), Y: int16(s.buf.CY)}
	procSetConsoleCursorPosition.Call(uintptr(outHandle()), uintptr(*(*uint32)(unsafe.Pointer(&pos))))
}

func (s *winScreen) Close() {
	procSetConsoleMode.Call(uintptr(inHandle()), uintptr(s.savedIn))
}

// ---- ввод ----

// readKeyRaw читает одно событие клавиатуры: имя клавиши KonLang.
func (s *winScreen) readKeyRaw() (string, error) {
	var rec [20]byte
	var n uint32
	for {
		r, _, e := procReadConsoleInputW.Call(uintptr(inHandle()),
			uintptr(unsafe.Pointer(&rec[0])), 1, uintptr(unsafe.Pointer(&n)))
		if r == 0 {
			return "", errors.New("ReadConsoleInput: " + e.Error())
		}
		if n == 0 {
			continue
		}
		if binary.LittleEndian.Uint16(rec[0:2]) != 0x0001 { // KEY_EVENT
			continue
		}
		down := binary.LittleEndian.Uint32(rec[4:8])
		if down == 0 {
			continue
		}
		vk := binary.LittleEndian.Uint16(rec[10:12])
		ch := binary.LittleEndian.Uint16(rec[14:16])
		ctrl := binary.LittleEndian.Uint32(rec[16:20])
		return mapWinKey(vk, ch, ctrl), nil
	}
}

func mapWinKey(vk, ch uint16, state uint32) string {
	alt := state&(0x0001|0x0002) != 0
	ctl := state&(0x0004|0x0008) != 0
	shift := state&0x0010 != 0
	mod := ""
	if ctl {
		mod = "ctrl+"
	} else if alt {
		mod = "alt+"
	}
	switch {
	case vk >= 0x70 && vk <= 0x7B:
		return mod + "f" + itoa(int(vk-0x70+1))
	}
	switch vk {
	case 0x26:
		return mod + "up"
	case 0x28:
		return mod + "down"
	case 0x25:
		return mod + "left"
	case 0x27:
		return mod + "right"
	case 0x21:
		return mod + "pgup"
	case 0x22:
		return mod + "pgdn"
	case 0x24:
		return mod + "home"
	case 0x23:
		return mod + "end"
	case 0x2D:
		return mod + "insert"
	case 0x2E:
		return mod + "del"
	case 0x0D:
		return mod + "enter"
	case 0x1B:
		return "esc"
	case 0x09:
		if shift {
			return "shift+tab"
		}
		return mod + "tab"
	case 0x08:
		return mod + "backspace"
	case 0x20:
		return mod + "space"
	}
	if ch == 0 {
		return "esc"
	}
	if ch == 13 || ch == 10 {
		return mod + "enter"
	}
	if ch == 9 {
		if shift {
			return "shift+tab"
		}
		return "tab"
	}
	if ch == 27 {
		return "esc"
	}
	if ch == 8 {
		return "backspace"
	}
	if ch < 32 {
		return "ctrl+" + string(rune('a'+ch-1))
	}
	r := rune(ch)
	if shift && r >= 'a' && r <= 'z' {
		r -= 32
	}
	return mod + string(r)
}

func (s *winScreen) ReadKey() (string, error) {
	s.Flush()
	return s.readKeyRaw()
}

func (s *winScreen) ReadLine(prompt string, limit int) (string, error) {
	s.buf.WriteString(prompt)
	sx, sy := s.buf.CX, s.buf.CY
	var text []rune
	s.Flush()
	for {
		key, err := s.readKeyRaw()
		if err != nil {
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
		s.buf.WriteAt(sx, sy, string(text)+" ")
		s.buf.CX, s.buf.CY = sx+len(text), sy
		s.Flush()
	}
}
