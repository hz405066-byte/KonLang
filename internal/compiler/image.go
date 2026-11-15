package compiler

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
)

const (
	imageMagic   = "KNBC"
	imageVersion = 1
	imageHeader  = 24
)

// Marshal собирает переносимый образ байт-кода (.knc).
//
// Формат:
//
//	0   magic "KNBC"
//	4   version (1)
//	5   encoding (0=utf-8, 1=cp866)
//	6   target (0=portable, 1=dos, 2=pros)
//	7   reserved
//	8   code length   (uint32, не более 65535)
//	12  consts length (uint32, не более 65535)
//	16  names length  (uint32, не более 65535)
//	20  crc32(payload) (uint32)
//	24  payload: code || consts || names
func (p *Program) Marshal() ([]byte, error) {
	if p.Encoding > EncodingCP866 {
		return nil, fmt.Errorf("образ: неизвестная кодировка %d", p.Encoding)
	}
	if p.Target > TargetProS {
		return nil, fmt.Errorf("образ: неизвестная цель %d", p.Target)
	}
	if len(p.Code) > 0xFFFF || len(p.Consts) > 0xFFFF || len(p.Names) > 0xFFFF {
		return nil, fmt.Errorf("образ: секция больше 65535 байт")
	}
	payload := make([]byte, 0, len(p.Code)+len(p.Consts)+len(p.Names))
	payload = append(payload, p.Code...)
	payload = append(payload, p.Consts...)
	payload = append(payload, p.Names...)

	out := make([]byte, imageHeader, imageHeader+len(payload))
	copy(out[0:4], imageMagic)
	out[4] = imageVersion
	out[5] = byte(p.Encoding)
	out[6] = byte(p.Target)
	binary.LittleEndian.PutUint32(out[8:], uint32(len(p.Code)))
	binary.LittleEndian.PutUint32(out[12:], uint32(len(p.Consts)))
	binary.LittleEndian.PutUint32(out[16:], uint32(len(p.Names)))
	binary.LittleEndian.PutUint32(out[20:], crc32.ChecksumIEEE(payload))
	out = append(out, payload...)
	return out, nil
}

// Unmarshal разбирает образ байт-кода, созданный Marshal.
func Unmarshal(data []byte) (*Program, error) {
	if len(data) < imageHeader {
		return nil, fmt.Errorf("образ байт-кода: слишком короткий файл")
	}
	if !bytes.Equal(data[0:4], []byte(imageMagic)) {
		return nil, fmt.Errorf("образ байт-кода: неверная сигнатура %q", data[0:4])
	}
	if data[4] != imageVersion {
		return nil, fmt.Errorf("образ байт-кода: неподдерживаемая версия %d", data[4])
	}
	if data[5] > byte(EncodingCP866) {
		return nil, fmt.Errorf("образ байт-кода: неизвестная кодировка %d", data[5])
	}
	if data[6] > byte(TargetProS) {
		return nil, fmt.Errorf("образ байт-кода: неизвестная цель %d", data[6])
	}
	codeLen := int(binary.LittleEndian.Uint32(data[8:]))
	constLen := int(binary.LittleEndian.Uint32(data[12:]))
	nameLen := int(binary.LittleEndian.Uint32(data[16:]))
	wantCRC := binary.LittleEndian.Uint32(data[20:])
	if codeLen < 0 || constLen < 0 || nameLen < 0 ||
		codeLen > 0xFFFF || constLen > 0xFFFF || nameLen > 0xFFFF ||
		codeLen > len(data) || constLen > len(data) || nameLen > len(data) ||
		codeLen+constLen > len(data)-imageHeader ||
		codeLen+constLen+nameLen != len(data)-imageHeader {
		return nil, fmt.Errorf("образ байт-кода: повреждённые размеры секций")
	}
	payload := data[imageHeader:]
	if crc32.ChecksumIEEE(payload) != wantCRC {
		return nil, fmt.Errorf("образ байт-кода: контрольная сумма не совпала")
	}
	p := &Program{
		Code:      append([]byte(nil), payload[:codeLen]...),
		Consts:    append([]byte(nil), payload[codeLen:codeLen+constLen]...),
		Names:     append([]byte(nil), payload[codeLen+constLen:]...),
		Encoding:  Encoding(data[5]),
		Target:    Target(data[6]),
		nameTexts: map[int]string{},
	}
	if err := p.decodeMetadata(); err != nil {
		return nil, err
	}
	if len(p.Code) == 0 {
		return nil, fmt.Errorf("образ байт-кода: пустой код")
	}
	return p, nil
}

func (p *Program) decodeMetadata() error {
	// Константы: uint16 n, n × uint16 offset, затем kind + data.
	if len(p.Consts) < 2 {
		return fmt.Errorf("образ байт-кода: пустой пул констант")
	}
	n := int(binary.LittleEndian.Uint16(p.Consts[:2]))
	if 2+2*n > len(p.Consts) {
		return fmt.Errorf("образ байт-кода: повреждена таблица констант")
	}
	p.consts = make([]Constant, 0, n)
	p.constTexts = make([]string, 0, n)
	for i := 0; i < n; i++ {
		off := int(binary.LittleEndian.Uint16(p.Consts[2+2*i:]))
		if off < 0 || off >= len(p.Consts) {
			return fmt.Errorf("образ байт-кода: смещение константы %d вне пула", i)
		}
		kind := p.Consts[off]
		c := Constant{Kind: kind}
		var text string
		switch kind {
		case 1, 2:
			if off+5 > len(p.Consts) {
				return fmt.Errorf("образ байт-кода: обрезанная числовая константа %d", i)
			}
			c.Int = int32(binary.LittleEndian.Uint32(p.Consts[off+1:]))
			if kind == 2 {
				c.Float = float64(c.Int) / 10000
				text = formatDec(c.Int)
			} else {
				text = fmt.Sprintf("%d", c.Int)
			}
		case 3:
			if off+3 > len(p.Consts) {
				return fmt.Errorf("образ байт-кода: обрезанная строковая константа %d", i)
			}
			size := int(binary.LittleEndian.Uint16(p.Consts[off+1:]))
			if off+3+size > len(p.Consts) {
				return fmt.Errorf("образ байт-кода: строка константы %d выходит за пул", i)
			}
			c.Str = decodeText(p.Consts[off+3:off+3+size], p.Encoding)
			text = fmt.Sprintf("%q", c.Str)
		default:
			return fmt.Errorf("образ байт-кода: неизвестный тип константы %d", kind)
		}
		p.consts = append(p.consts, c)
		p.constTexts = append(p.constTexts, text)
	}

	// Имена: последовательность NUL-терминированных строк.
	for off := 0; off < len(p.Names); {
		end := bytes.IndexByte(p.Names[off:], 0)
		if end < 0 {
			return fmt.Errorf("образ байт-кода: имя без завершающего NUL")
		}
		p.nameTexts[off] = decodeText(p.Names[off:off+end], p.Encoding)
		off += end + 1
	}
	return nil
}
