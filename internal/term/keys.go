package term

import "strings"

// specialKeys — имена спецклавиш KonLang.
var specialKeys = map[string]bool{
	"up": true, "down": true, "left": true, "right": true, "enter": true,
	"esc": true, "tab": true, "space": true, "backspace": true, "del": true,
	"home": true, "end": true, "pgup": true, "pgdn": true, "insert": true,
}

// IsSpecialKey сообщает, является ли имя клавиши специальным.
func IsSpecialKey(k string) bool {
	if specialKeys[k] {
		return true
	}
	return strings.HasPrefix(k, "f") && len(k) <= 3 && len(k) >= 2
}

// keyReader читает байты (с таймаутом для одиночного ESC).
type keyReader struct {
	blocking func() (byte, error)
	poll     func() (byte, bool)
}

// readKey разбирает ANSI-последовательность клавиатуры в имя клавиши KonLang.
func (kr keyReader) readKey() (string, error) {
	b, err := kr.blocking()
	if err != nil {
		return "", err
	}
	switch b {
	case 13, 10:
		return "enter", nil
	case 9:
		return "tab", nil
	case 32:
		return "space", nil
	case 127, 8:
		return "backspace", nil
	case 27:
		return kr.escape()
	}
	if b < 32 {
		return "ctrl+" + string(rune('a'+b-1)), nil
	}
	if b < 128 {
		return string(rune(b)), nil
	}
	// UTF-8: собираем многобайтовый символ
	r := []byte{b}
	extra := 0
	switch {
	case b&0xE0 == 0xC0:
		extra = 1
	case b&0xF0 == 0xE0:
		extra = 2
	case b&0xF8 == 0xF0:
		extra = 3
	}
	for i := 0; i < extra; i++ {
		nb, ok := kr.poll()
		if !ok {
			break
		}
		r = append(r, nb)
	}
	return string(r), nil
}

func (kr keyReader) escape() (string, error) {
	b, ok := kr.poll()
	if !ok {
		return "esc", nil
	}
	if b == 'O' {
		c, ok := kr.poll()
		if !ok {
			return "esc", nil
		}
		switch c {
		case 'P':
			return "f1", nil
		case 'Q':
			return "f2", nil
		case 'R':
			return "f3", nil
		case 'S':
			return "f4", nil
		case 'H':
			return "home", nil
		case 'F':
			return "end", nil
		case 'A':
			return "up", nil
		case 'B':
			return "down", nil
		case 'C':
			return "right", nil
		case 'D':
			return "left", nil
		}
		return "esc", nil
	}
	if b != '[' {
		// alt+<символ>
		if b >= 32 && b < 127 {
			return "alt+" + string(rune(b)), nil
		}
		return "esc", nil
	}
	// CSI: собираем параметры до финальной буквы
	var params []byte
	for {
		c, ok := kr.poll()
		if !ok {
			return "esc", nil
		}
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '~' {
			return csiKey(string(params), c), nil
		}
		params = append(params, c)
		if len(params) > 16 {
			return "esc", nil
		}
	}
}

func csiKey(params string, final byte) string {
	mod := ""
	num := params
	if i := strings.IndexByte(params, ';'); i >= 0 {
		num = params[:i]
		switch params[i+1:] {
		case "2":
			mod = "shift+"
		case "3":
			mod = "alt+"
		case "5":
			mod = "ctrl+"
		case "6":
			mod = "ctrl+shift+"
		}
	}
	base := ""
	switch final {
	case 'A':
		base = "up"
	case 'B':
		base = "down"
	case 'C':
		base = "right"
	case 'D':
		base = "left"
	case 'H':
		base = "home"
	case 'F':
		base = "end"
	case 'Z':
		return "shift+tab"
	case '~':
		switch num {
		case "1", "7":
			base = "home"
		case "2":
			base = "insert"
		case "3":
			base = "del"
		case "4", "8":
			base = "end"
		case "5":
			base = "pgup"
		case "6":
			base = "pgdn"
		case "11":
			base = "f1"
		case "12":
			base = "f2"
		case "13":
			base = "f3"
		case "14":
			base = "f4"
		case "15":
			base = "f5"
		case "17":
			base = "f6"
		case "18":
			base = "f7"
		case "19":
			base = "f8"
		case "20":
			base = "f9"
		case "21":
			base = "f10"
		case "23":
			base = "f11"
		case "24":
			base = "f12"
		default:
			return "esc"
		}
	default:
		return "esc"
	}
	return mod + base
}
