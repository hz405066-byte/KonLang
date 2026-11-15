package tests

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/konlang/konlang/internal/interp"
	"github.com/konlang/konlang/internal/parser"
	"github.com/konlang/konlang/internal/term"
)

type tc struct {
	file string
	in   string
	seed int64
	want string
}

var cases = []tc{
	{file: "hello.kn", want: "Hello, World!\n"},
	{file: "math.kn", want: "2 + 3 = 5\n10 / 3 = 3.3333\n10 / 3 (int) = 3\n10 / 3 (float) = 3.3333\n"},
	{file: "frame.kn", want: "╔══════════╗\n║          ║\n║          ║\n╚══════════╝\n"},
	{file: "func_sum.kn", want: "2 + 3 = 5\n"},
	{file: "square.kn", want: "Квадрат 5 = 25\n"},
	{file: "max.kn", want: "max(7,3) = 7\nmax(2,9) = 9\n"},
	{file: "fact.kn", want: "5! = 120\n7! = 5040\n"},
	{file: "cycle_break.kn", want: "Шаг 0\nШаг 1\nШаг 2\n"},
	{file: "cycle5.kn", want: strings.Repeat("Я не буду разговаривать в классе.\n", 5)},
	{file: "lists.kn", want: "Всего: 3\nПервое: яблоко\nВторое: груша\n"},
	{file: "list_loop.kn", want: "0. яблоко\n1. груша\n2. слива\n"},
	{file: "input_types.kn", in: "7\n5\n", want: "Первое число: 7\nВторое число: 5\nСумма: 12\n"},
	{file: "input_limit.kn", in: "RUS\n", want: "Код страны (2 символа): RU\nВы ввели: RU\n"},
	{file: "calc.kn", in: "10\n4\n", want: "a = 10\nb = 4\na + b = 14\na - b = 6\na * b = 40\na / b = 2.5\n"},
	{file: "mult_table.kn", in: "3\n", want: "Число: 3\n3 × 1 = 3\n3 × 2 = 6\n3 × 3 = 9\n3 × 4 = 12\n3 × 5 = 15\n3 × 6 = 18\n3 × 7 = 21\n3 × 8 = 24\n3 × 9 = 27\n"},
	{file: "cycle_var.kn", in: "3\n", want: "Сколько раз? 3\nСтрока\nСтрока\nСтрока\n"},
	{file: "io.kn", in: "KonLang\n", want: "Ведите текст KonLang\nВы ввели: KonLang [Интересное слово]\n^\n2 + 3 = 5\n"},
	{file: "menu_list.kn", in: "1\n", want: "0 — Привет\n1 — Случайное слово\n2 — Выход\nВыбор: 1\n"},
	{file: "screen_frame.kn", want: "╔════════════════════╗\n║                    ║\n║  KonLang           ║\n║                    ║\n╚════════════════════╝\n"},
	{file: "guess.kn", in: "5\n7\n6\n3\n1\n2\n4\n8\n9\n10\n", seed: 7,
		want: "Угадайте число от 1 до 10.\nВаш вариант: 5\nБольше\nВаш вариант: 7\nУгадали за 2 попыток!\n"},
	{file: "pages.kn", in: "1\n0\n2\n", want: "=== ГЛАВНАЯ ===\n1 — О программе\n2 — Выход\n-> 1\n" +
		"=== О ПРОГРАММЕ ===\nKonLang v5\n0 — Назад\n-> 0\n" +
		"=== ГЛАВНАЯ ===\n1 — О программе\n2 — Выход\n-> 2\n"},
	{file: "styles.kn", want: "Жирный и подчёркнутый\nЗелёный текст красный\n Белый на синем \nМигающий Инверсия\n"},
	{file: "files.kn", want: "--- Содержимое ---\nПривет, файл!\n"},
	{file: "log.kn", in: "раз\nдва\nтри\n", want: "Строка: раз\nСтрока: два\nСтрока: три\n--- Лог ---\nраз\nдва\nтри\n"},
	{file: "keys.kn", in: "a\x1b", want: "Нажмите клавишу (esc — выход):\nКлавиша: a\nКлавиша: esc\n"},
}

func runKN(t *testing.T, c tc) string {
	t.Helper()
	path := filepath.Join("..", "examples", c.file)
	prog, err := parser.ParseFile(path)
	if err != nil {
		t.Fatalf("%s: %v", c.file, err)
	}
	var out bytes.Buffer
	scr := term.New(term.Options{ForceLine: true, In: strings.NewReader(c.in), Out: &out, Width: 80, Height: 25})
	defer scr.Close()
	dir, _ := filepath.Abs(filepath.Join("..", "examples"))
	in := interp.New(interp.Options{Screen: scr, Dir: dir, Seed: c.seed})
	if _, err := in.Run(prog); err != nil {
		t.Fatalf("%s: %v", c.file, err)
	}
	return out.String()
}

func TestExamples(t *testing.T) {
	for _, c := range cases {
		c := c
		t.Run(c.file, func(t *testing.T) {
			got := runKN(t, c)
			if got != c.want {
				t.Errorf("вывод не совпал\n--- получено ---\n%s\n--- ожидалось ---\n%s", got, c.want)
			}
		})
	}
}

// TestAllExamplesParse проверяет, что все примеры разбираются.
func TestAllExamplesParse(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "examples", "*.kn"))
	if err != nil || len(files) == 0 {
		t.Fatal("примеры не найдены")
	}
	for _, f := range files {
		if _, err := parser.ParseFile(f); err != nil {
			t.Errorf("%s: %v", filepath.Base(f), err)
		}
	}
}

// TestScreenMode проверяет координатный вывод в буферном режиме.
func TestScreenMode(t *testing.T) {
	src := `print{3,2,"X"}print{0,0,"Y"}`
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	scr := term.New(term.Options{ForceScreen: true, ForceLine: false, In: strings.NewReader(""), Out: &out, Width: 20, Height: 5})
	in := interp.New(interp.Options{Screen: scr, Dir: os.TempDir()})
	if _, err := in.Run(prog); err != nil {
		t.Fatal(err)
	}
	scr.Close()
	want := "Y\n\n   X\n"
	if out.String() != want {
		t.Errorf("получено %q, ожидалось %q", out.String(), want)
	}
}

// TestErrors проверяет диагностику.
func TestErrors(t *testing.T) {
	bad := []string{
		`print{"x"`,
		`set{a=}`,
		`if[[a]>1]{`,
		`cycle3{ print{"x"} `,
		`print{"a"}` + "\n" + `exec[42]`,
	}
	for _, src := range bad {
		prog, err := parser.Parse(src)
		if err != nil {
			continue
		}
		var out bytes.Buffer
		scr := term.New(term.Options{ForceLine: true, In: strings.NewReader(""), Out: &out})
		in := interp.New(interp.Options{Screen: scr, Dir: os.TempDir()})
		if _, err := in.Run(prog); err == nil {
			t.Errorf("ожидалась ошибка для %q", src)
		}
	}
}
