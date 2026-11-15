// Package term — кроссплатформенный терминальный слой KonLang (TUI/CUI).
//
// Три режима работы:
//   - LineScreen  — обычный построчный вывод (stdout не терминал, тесты, пайпы);
//   - AnsiScreen  — полноэкранный TUI на Linux/macOS (ANSI + termios);
//   - WinScreen   — полноэкранный TUI на Windows (Console API, без cgo);
//   - DumpScreen  — буферный режим без терминала (--screen), кадр печатается в конце.
package term

import (
	"strconv"
	"strings"
)

// Attr — атрибуты символа.
type Attr struct {
	Fg      int // -1 = по умолчанию, 0..7 обычные, 8..15 яркие
	Bg      int
	Bold    bool
	Dim     bool
	Italic  bool
	Under   bool
	Blink   bool
	Reverse bool
	Strike  bool
}

// DefaultAttr — «обычный» текст.
func DefaultAttr() Attr { return Attr{Fg: -1, Bg: -1} }

// ColorNames — имена цветов из справочника KonLang.
var ColorNames = map[string]int{
	"BLACK": 0, "RED": 1, "GREEN": 2, "YELLOW": 3,
	"BLUE": 4, "MAGENTA": 5, "CYAN": 6, "WHITE": 7, "GRAY": 8,
	"BRIGHT_BLACK": 8, "BRIGHT_RED": 9, "BRIGHT_GREEN": 10, "BRIGHT_YELLOW": 11,
	"BRIGHT_BLUE": 12, "BRIGHT_MAGENTA": 13, "BRIGHT_CYAN": 14, "BRIGHT_WHITE": 15,
}

// StyleNames — имена стилей из справочника.
var StyleNames = map[string]string{
	"BOLD": "1", "DIM": "2", "ITALIC": "3", "UNDERLINE": "4",
	"BLINK": "5", "REVERSE": "7", "STRIKE": "9",
}

// ColorCode возвращает SGR-код цвета по имени (или -1).
func ColorCode(name string) int {
	if c, ok := ColorNames[strings.ToUpper(strings.TrimSpace(name))]; ok {
		return c
	}
	return -1
}

// SGR собирает ANSI-последовательность для атрибута.
func (a Attr) SGR() string {
	var p []string
	p = append(p, "0")
	if a.Bold {
		p = append(p, "1")
	}
	if a.Dim {
		p = append(p, "2")
	}
	if a.Italic {
		p = append(p, "3")
	}
	if a.Under {
		p = append(p, "4")
	}
	if a.Blink {
		p = append(p, "5")
	}
	if a.Reverse {
		p = append(p, "7")
	}
	if a.Strike {
		p = append(p, "9")
	}
	if a.Fg >= 0 {
		if a.Fg < 8 {
			p = append(p, strconv.Itoa(30+a.Fg))
		} else {
			p = append(p, strconv.Itoa(90+a.Fg-8))
		}
	}
	if a.Bg >= 0 {
		if a.Bg < 8 {
			p = append(p, strconv.Itoa(40+a.Bg))
		} else {
			p = append(p, strconv.Itoa(100+a.Bg-8))
		}
	}
	return "\x1b[" + strings.Join(p, ";") + "m"
}

// applySGR применяет SGR-параметры к атрибуту.
func (a Attr) applySGR(params string) Attr {
	if params == "" {
		params = "0"
	}
	for _, part := range strings.Split(params, ";") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			n = 0
		}
		switch {
		case n == 0:
			a = DefaultAttr()
		case n == 1:
			a.Bold = true
		case n == 2:
			a.Dim = true
		case n == 3:
			a.Italic = true
		case n == 4:
			a.Under = true
		case n == 5:
			a.Blink = true
		case n == 7:
			a.Reverse = true
		case n == 9:
			a.Strike = true
		case n == 22:
			a.Bold, a.Dim = false, false
		case n == 23:
			a.Italic = false
		case n == 24:
			a.Under = false
		case n == 25:
			a.Blink = false
		case n == 27:
			a.Reverse = false
		case n == 29:
			a.Strike = false
		case n >= 30 && n <= 37:
			a.Fg = n - 30
		case n == 39:
			a.Fg = -1
		case n >= 40 && n <= 47:
			a.Bg = n - 40
		case n == 49:
			a.Bg = -1
		case n >= 90 && n <= 97:
			a.Fg = n - 90 + 8
		case n >= 100 && n <= 107:
			a.Bg = n - 100 + 8
		}
	}
	return a
}

// StripANSI убирает ANSI-последовательности из строки.
func StripANSI(s string) string {
	if !strings.ContainsRune(s, 0x1b) {
		return s
	}
	var b strings.Builder
	r := []rune(s)
	for i := 0; i < len(r); i++ {
		if r[i] == 0x1b {
			i++
			if i < len(r) && r[i] == '[' {
				for i < len(r) && !((r[i] >= 'a' && r[i] <= 'z') || (r[i] >= 'A' && r[i] <= 'Z')) {
					i++
				}
			}
			continue
		}
		b.WriteRune(r[i])
	}
	return b.String()
}

// VisibleLen — длина строки без ANSI-последовательностей.
func VisibleLen(s string) int { return len([]rune(StripANSI(s))) }
