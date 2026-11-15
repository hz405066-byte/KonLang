package tests

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/konlang/konlang/internal/compiler"
	"github.com/konlang/konlang/internal/parser"
	"github.com/konlang/konlang/internal/term"
	"github.com/konlang/konlang/internal/vm"
)

func runVM(t *testing.T, c tc) string {
	t.Helper()
	path := filepath.Join("..", "examples", c.file)
	prog, err := parser.ParseFile(path)
	if err != nil {
		t.Fatalf("%s: %v", c.file, err)
	}
	dir, _ := filepath.Abs(filepath.Join("..", "examples"))
	image, err := compiler.CompileWith(compiler.Options{Dir: dir}, prog)
	if err != nil {
		t.Fatalf("%s: компиляция: %v", c.file, err)
	}
	var out bytes.Buffer
	scr := term.New(term.Options{ForceLine: true, In: strings.NewReader(c.in), Out: &out, Width: 80, Height: 25})
	defer scr.Close()
	m := vm.New(image, vm.Options{Screen: scr, Dir: dir, Seed: c.seed})
	if _, err := m.Run(); err != nil {
		t.Fatalf("%s: ВМ: %v", c.file, err)
	}
	return out.String()
}

// TestVMExamples прогоняет все эталонные примеры через компилятор и ВМ.
func TestVMExamples(t *testing.T) {
	for _, c := range cases {
		c := c
		t.Run(c.file, func(t *testing.T) {
			got := runVM(t, c)
			if got != c.want {
				t.Errorf("вывод ВМ не совпал\n--- получено ---\n%s\n--- ожидалось ---\n%s", got, c.want)
			}
		})
	}
}

// TestImageRoundTrip проверяет сохранение и загрузку образа байт-кода.
func TestImageRoundTrip(t *testing.T) {
	path := filepath.Join("..", "examples", "fact.kn")
	prog, err := parser.ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	image, err := compiler.Compile(prog)
	if err != nil {
		t.Fatal(err)
	}
	data, err := image.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := compiler.Unmarshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Disasm() != image.Disasm() {
		t.Fatal("дизассемблер загруженного образа отличается")
	}
	if loaded.Encoding != image.Encoding || loaded.Target != image.Target {
		t.Fatal("метаданные образа не сохранились")
	}
}

// TestVMExecString проверяет exec от строки и динамическое определение kon.
func TestVMExecString(t *testing.T) {
	src := `
kon_page{
  print{"page"}
  set{x=math{2+3}}
}
set{code="kon_dyn{print{\"dyn\"}}"}
exec[kon_page]
exec[code]
exec[kon_dyn]
print{"x="[x]}
`
	astProg, err := parser.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	image, err := compiler.Compile(astProg)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	scr := term.New(term.Options{ForceLine: true, In: strings.NewReader(""), Out: &out})
	m := vm.New(image, vm.Options{Screen: scr, Dir: t.TempDir()})
	if _, err := m.Run(); err != nil {
		t.Fatal(err)
	}
	want := "page\ndyn\nx=5\n"
	if out.String() != want {
		t.Fatalf("получено %q, ожидалось %q", out.String(), want)
	}
}

// TestVMErrors проверяет диагностику ВМ.
func TestVMErrors(t *testing.T) {
	bad := []string{
		`exec[42]`,
		`print{[missing]}`,
		`set{a=math{1/0}}`,
		`def_f[a]{ret[a]} print{f{}}`,
	}
	for _, src := range bad {
		prog, err := parser.Parse(src)
		if err != nil {
			continue
		}
		image, err := compiler.Compile(prog)
		if err != nil {
			continue
		}
		var out bytes.Buffer
		scr := term.New(term.Options{ForceLine: true, In: strings.NewReader(""), Out: &out})
		m := vm.New(image, vm.Options{Screen: scr, Dir: t.TempDir()})
		if _, err := m.Run(); err == nil {
			t.Errorf("ожидалась ошибка ВМ для %q", src)
		}
	}
}
