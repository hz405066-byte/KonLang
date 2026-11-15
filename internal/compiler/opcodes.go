package compiler

import "fmt"

// Opcode — команда виртуальной машины KonLang-16.
type Opcode byte

const (
	OpNOP       Opcode = 0x00
	OpPUSHK     Opcode = 0x01 // k16      — константа
	OpPUSHNIL   Opcode = 0x02
	OpPOP       Opcode = 0x03
	OpDUP       Opcode = 0x04
	OpLOAD      Opcode = 0x05 // name16   — переменная, иначе «голое слово»
	OpSTORE     Opcode = 0x06 // name16
	OpLOADREG   Opcode = 0x07 // name16   — [регистр], nil если пуст
	OpMKLIST    Opcode = 0x08 // n16
	OpINDEX     Opcode = 0x09
	OpCALL      Opcode = 0x0A // name16 argc8
	OpRETV      Opcode = 0x0B
	OpRETN      Opcode = 0x0C
	OpJMP       Opcode = 0x0D // a16
	OpJZ        Opcode = 0x0E // a16
	OpJNZ       Opcode = 0x0F // a16
	OpADD       Opcode = 0x10
	OpSUB       Opcode = 0x11
	OpMUL       Opcode = 0x12
	OpDIV       Opcode = 0x13
	OpMOD       Opcode = 0x14
	OpNEG       Opcode = 0x15
	OpEQ        Opcode = 0x16
	OpNE        Opcode = 0x17
	OpLT        Opcode = 0x18
	OpLE        Opcode = 0x19
	OpGT        Opcode = 0x1A
	OpGE        Opcode = 0x1B
	OpAND       Opcode = 0x1C
	OpOR        Opcode = 0x1D
	OpNOT       Opcode = 0x1E
	OpCONCAT    Opcode = 0x1F
	OpREPEAT    Opcode = 0x20
	OpLEN       Opcode = 0x21
	OpCAST      Opcode = 0x22 // t8 (0=int,1=float,2=char,3=str)
	OpPRINT     Opcode = 0x23 // n16
	OpPRINTXY   Opcode = 0x24 // n16 (x,y + n-2 значений)
	OpINPUT     Opcode = 0x25 // name16 t8
	OpKEY       Opcode = 0x26 // name16
	OpCLEAR     Opcode = 0x27
	OpSCRSIZE   Opcode = 0x28
	OpSCRCUR    Opcode = 0x29
	OpSCRBG     Opcode = 0x2A
	OpBEEP      Opcode = 0x2B
	OpRANDOM    Opcode = 0x2C // name16 n16
	OpEXEC      Opcode = 0x2D
	OpKON       Opcode = 0x2E // name16 a16
	OpDEF       Opcode = 0x2F // name16 a16 argc8 (далее argc × name16)
	OpFOPEN     Opcode = 0x30
	OpFWRITE    Opcode = 0x31
	OpFAPPEND   Opcode = 0x32
	OpFDEL      Opcode = 0x33
	OpFLIST     Opcode = 0x34
	OpTIME      Opcode = 0x35
	OpEXIT      Opcode = 0x36
	OpDIVI      Opcode = 0x37
	OpDIVF      Opcode = 0x38
	OpSTYLE     Opcode = 0x39 // код стиля + текст
	OpCOLOR     Opcode = 0x3A // имя цвета → код
	OpHALT      Opcode = 0x3B
	OpSTYLENAME Opcode = 0x3C // имя стиля → код
	OpIMPORT    Opcode = 0x3D // name16 — отметить подключённую библиотеку
)

type opInfo struct {
	name string
	ops  string // w — word, b — byte, n — имя, k — константа, a — адрес
}

var opTable = map[Opcode]opInfo{
	OpNOP: {"NOP", ""}, OpPUSHK: {"PUSHK", "k"}, OpPUSHNIL: {"PUSHNIL", ""},
	OpPOP: {"POP", ""}, OpDUP: {"DUP", ""}, OpLOAD: {"LOAD", "n"},
	OpSTORE: {"STORE", "n"}, OpLOADREG: {"LOADREG", "n"}, OpMKLIST: {"MKLIST", "w"},
	OpINDEX: {"INDEX", ""}, OpCALL: {"CALL", "nb"}, OpRETV: {"RETV", ""},
	OpRETN: {"RETN", ""}, OpJMP: {"JMP", "a"}, OpJZ: {"JZ", "a"}, OpJNZ: {"JNZ", "a"},
	OpADD: {"ADD", ""}, OpSUB: {"SUB", ""}, OpMUL: {"MUL", ""}, OpDIV: {"DIV", ""},
	OpMOD: {"MOD", ""}, OpNEG: {"NEG", ""}, OpEQ: {"EQ", ""}, OpNE: {"NE", ""},
	OpLT: {"LT", ""}, OpLE: {"LE", ""}, OpGT: {"GT", ""}, OpGE: {"GE", ""},
	OpAND: {"AND", ""}, OpOR: {"OR", ""}, OpNOT: {"NOT", ""}, OpCONCAT: {"CONCAT", ""},
	OpREPEAT: {"REPEAT", ""}, OpLEN: {"LEN", ""}, OpCAST: {"CAST", "b"},
	OpPRINT: {"PRINT", "w"}, OpPRINTXY: {"PRINTXY", "w"}, OpINPUT: {"INPUT", "nb"},
	OpKEY: {"KEY", "n"}, OpCLEAR: {"CLEAR", ""}, OpSCRSIZE: {"SCRSIZE", ""},
	OpSCRCUR: {"SCRCUR", ""}, OpSCRBG: {"SCRBG", ""}, OpBEEP: {"BEEP", ""},
	OpRANDOM: {"RANDOM", "nw"}, OpEXEC: {"EXEC", ""}, OpKON: {"KON", "na"},
	OpDEF: {"DEF", "nab"}, OpFOPEN: {"FOPEN", ""}, OpFWRITE: {"FWRITE", ""},
	OpFAPPEND: {"FAPPEND", ""}, OpFDEL: {"FDEL", ""}, OpFLIST: {"FLIST", ""},
	OpTIME: {"TIME", ""}, OpEXIT: {"EXIT", ""}, OpDIVI: {"DIVI", ""},
	OpDIVF: {"DIVF", ""}, OpSTYLE: {"STYLE", ""}, OpCOLOR: {"COLOR", ""},
	OpHALT: {"HALT", ""}, OpSTYLENAME: {"STYLENAME", ""},
	OpIMPORT: {"IMPORT", "n"},
}

// String возвращает мнемонику опкода.
func (op Opcode) String() string {
	if info, ok := opTable[op]; ok {
		return info.name
	}
	return fmt.Sprintf("0x%02X", byte(op))
}

// Disasm разбирает байт-код в читаемый текст.
func (p *Program) Disasm() string {
	var out []byte
	appendStr := func(s string) { out = append(out, []byte(s)...) }
	i := 0
	for i < len(p.Code) {
		start := i
		op := Opcode(p.Code[i])
		i++
		info, ok := opTable[op]
		if !ok {
			appendStr(fmt.Sprintf("%04X  ??? %02X\n", start, op))
			continue
		}
		line := fmt.Sprintf("%04X  %-8s", start, info.name)
		truncated := func() string {
			appendStr(fmt.Sprintf("%04X  %-8s <обрезано>\n", start, info.name))
			return string(out)
		}
		argc := 0
		for _, c := range info.ops {
			switch c {
			case 'w', 'k':
				if i+2 > len(p.Code) {
					return truncated()
				}
				v := int(p.Code[i]) | int(p.Code[i+1])<<8
				i += 2
				if c == 'k' {
					line += fmt.Sprintf(" #%d (%s)", v, p.ConstText(v))
				} else {
					line += fmt.Sprintf(" %d", v)
				}
			case 'a':
				if i+2 > len(p.Code) {
					return truncated()
				}
				v := int(p.Code[i]) | int(p.Code[i+1])<<8
				i += 2
				line += fmt.Sprintf(" ->%04X", v)
			case 'n':
				if i+2 > len(p.Code) {
					return truncated()
				}
				v := int(p.Code[i]) | int(p.Code[i+1])<<8
				i += 2
				line += fmt.Sprintf(" %s", p.NameAt(v))
			case 'b':
				if i >= len(p.Code) {
					return truncated()
				}
				v := p.Code[i]
				i++
				line += fmt.Sprintf(" %d", v)
				if op == OpCALL || op == OpDEF {
					argc = int(v)
				}
			}
		}
		if op == OpDEF {
			for k := 0; k < argc; k++ {
				if i+2 > len(p.Code) {
					return truncated()
				}
				v := int(p.Code[i]) | int(p.Code[i+1])<<8
				i += 2
				line += " " + p.NameAt(v)
			}
		}
		appendStr(line + "\n")
	}
	return string(out)
}
