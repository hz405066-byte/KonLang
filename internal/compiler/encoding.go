package compiler

import "strings"

// Encoding — кодировка строк и имён внутри образа байт-кода.
type Encoding byte

const (
	// EncodingUTF8 — переносимый образ для встроенной ВМ (все символы).
	EncodingUTF8 Encoding = 0
	// EncodingCP866 — образ для MS-DOS / x16-PRos (альтернативная ГОСТ).
	EncodingCP866 Encoding = 1
)

func (e Encoding) String() string {
	switch e {
	case EncodingCP866:
		return "cp866"
	default:
		return "utf-8"
	}
}

// Target — платформа, для которой собран образ.
type Target byte

const (
	TargetPortable Target = 0
	TargetDOS      Target = 1
	TargetProS     Target = 2
)

func (t Target) String() string {
	switch t {
	case TargetDOS:
		return "dos"
	case TargetProS:
		return "pros"
	default:
		return "portable"
	}
}

func encodeText(s string, e Encoding) []byte {
	if e == EncodingCP866 {
		return ToCP866(s)
	}
	return []byte(s)
}

func decodeText(b []byte, e Encoding) string {
	if e == EncodingCP866 {
		return FromCP866(b)
	}
	return string(b)
}

var cp866Reverse map[byte]rune

func init() {
	cp866Reverse = make(map[byte]rune, len(cp866)+96)
	// Базовая ASCII-часть.
	for r := rune(0x20); r < 0x7F; r++ {
		cp866Reverse[byte(r)] = r
	}
	cp866Reverse['\n'] = '\n'
	cp866Reverse['\r'] = '\r'
	cp866Reverse['\t'] = '\t'
	for r, b := range cp866 {
		if _, exists := cp866Reverse[b]; !exists {
			cp866Reverse[b] = r
		}
	}
}

// FromCP866 перекодирует CP866 в UTF-8.
func FromCP866(b []byte) string {
	var sb strings.Builder
	sb.Grow(len(b))
	for _, c := range b {
		if c < 0x80 {
			sb.WriteByte(c)
			continue
		}
		if r, ok := cp866Reverse[c]; ok {
			sb.WriteRune(r)
		} else {
			sb.WriteByte('?')
		}
	}
	return sb.String()
}
