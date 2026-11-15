package term

import "strings"

// Cell — знакоместо экрана.
type Cell struct {
	Ch rune
	A  Attr
}

// Buffer — виртуальный экран (знакоместа + курсор + текущий атрибут).
type Buffer struct {
	W, H  int
	Cells []Cell
	CX    int
	CY    int
	Cur   Attr
	Fill  Attr // атрибут фона для очистки
}

// NewBuffer создаёт буфер размером w×h.
func NewBuffer(w, h int) *Buffer {
	if w < 1 {
		w = 80
	}
	if h < 1 {
		h = 25
	}
	b := &Buffer{W: w, H: h, Cur: DefaultAttr(), Fill: DefaultAttr()}
	b.Cells = make([]Cell, w*h)
	b.Clear()
	return b
}

// Clear очищает буфер и ставит курсор в 0,0.
func (b *Buffer) Clear() {
	c := Cell{Ch: ' ', A: b.Fill}
	for i := range b.Cells {
		b.Cells[i] = c
	}
	b.CX, b.CY = 0, 0
}

// Resize меняет размер с сохранением содержимого.
func (b *Buffer) Resize(w, h int) {
	if w == b.W && h == b.H {
		return
	}
	nb := NewBuffer(w, h)
	nb.Fill = b.Fill
	nb.Clear()
	for y := 0; y < h && y < b.H; y++ {
		for x := 0; x < w && x < b.W; x++ {
			nb.Cells[y*w+x] = b.Cells[y*b.W+x]
		}
	}
	nb.CX, nb.CY = b.CX, b.CY
	if nb.CX >= w {
		nb.CX = w - 1
	}
	if nb.CY >= h {
		nb.CY = h - 1
	}
	*b = *nb
}

func (b *Buffer) put(x, y int, r rune) {
	if x < 0 || y < 0 || x >= b.W || y >= b.H {
		return
	}
	b.Cells[y*b.W+x] = Cell{Ch: r, A: b.Cur}
}

// Scroll сдвигает экран на строку вверх.
func (b *Buffer) Scroll() {
	copy(b.Cells, b.Cells[b.W:])
	blank := Cell{Ch: ' ', A: b.Fill}
	for i := (b.H - 1) * b.W; i < len(b.Cells); i++ {
		b.Cells[i] = blank
	}
}

func (b *Buffer) newline() {
	b.CX = 0
	b.CY++
	if b.CY >= b.H {
		b.Scroll()
		b.CY = b.H - 1
	}
}

// WriteString пишет строку в позиции курсора, понимая \n, \t и ANSI-SGR.
func (b *Buffer) WriteString(s string) {
	r := []rune(s)
	for i := 0; i < len(r); i++ {
		c := r[i]
		switch {
		case c == 0x1b:
			// ESC [ ... m — атрибуты; прочие escape-последовательности игнорируем
			j := i + 1
			if j < len(r) && r[j] == '[' {
				j++
				start := j
				for j < len(r) && !((r[j] >= 'a' && r[j] <= 'z') || (r[j] >= 'A' && r[j] <= 'Z')) {
					j++
				}
				if j < len(r) {
					if r[j] == 'm' {
						b.Cur = b.Cur.applySGR(string(r[start:j]))
					}
					i = j
				} else {
					i = j - 1
				}
			}
		case c == '\n':
			b.newline()
		case c == '\r':
			b.CX = 0
		case c == '\t':
			for k := 0; k < 8-(b.CX%8); k++ {
				b.put(b.CX, b.CY, ' ')
				b.CX++
				if b.CX >= b.W {
					b.newline()
				}
			}
		default:
			if b.CX >= b.W {
				b.newline()
			}
			b.put(b.CX, b.CY, c)
			b.CX++
		}
	}
}

// WriteAt пишет строку с указанных координат.
func (b *Buffer) WriteAt(x, y int, s string) {
	b.CX, b.CY = x, y
	if b.CY < 0 {
		b.CY = 0
	}
	if b.CY >= b.H {
		b.CY = b.H - 1
	}
	if b.CX < 0 {
		b.CX = 0
	}
	b.WriteString(s)
}

// Text возвращает содержимое буфера как текст (для дампа/тестов).
func (b *Buffer) Text() string {
	var sb strings.Builder
	last := 0
	for y := 0; y < b.H; y++ {
		line := make([]rune, b.W)
		for x := 0; x < b.W; x++ {
			line[x] = b.Cells[y*b.W+x].Ch
		}
		s := strings.TrimRight(string(line), " ")
		if s != "" {
			last = y + 1
		}
	}
	for y := 0; y < last; y++ {
		line := make([]rune, b.W)
		for x := 0; x < b.W; x++ {
			line[x] = b.Cells[y*b.W+x].Ch
		}
		sb.WriteString(strings.TrimRight(string(line), " "))
		sb.WriteByte('\n')
	}
	return sb.String()
}
