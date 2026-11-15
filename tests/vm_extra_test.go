package tests

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/konlang/konlang/internal/compiler"
	"github.com/konlang/konlang/internal/interp"
	"github.com/konlang/konlang/internal/parser"
	"github.com/konlang/konlang/internal/term"
	"github.com/konlang/konlang/internal/vm"
)

// runBoth выполняет один и тот же исходник интерпретатором и в ВМ.
func runBoth(t *testing.T, src, input, dir string, seed int64) (string, string) {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}

	var treeOut bytes.Buffer
	scr := term.New(term.Options{ForceLine: true, In: strings.NewReader(input), Out: &treeOut, Width: 80, Height: 25})
	in := interp.New(interp.Options{Screen: scr, Dir: dir, Seed: seed})
	if _, err := in.Run(prog); err != nil {
		t.Fatalf("интерпретатор: %v", err)
	}
	scr.Close()

	image, err := compiler.CompileWith(compiler.Options{Dir: dir}, prog)
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	var vmOut bytes.Buffer
	scr2 := term.New(term.Options{ForceLine: true, In: strings.NewReader(input), Out: &vmOut, Width: 80, Height: 25})
	m := vm.New(image, vm.Options{Screen: scr2, Dir: dir, Seed: seed})
	if _, err := m.Run(); err != nil {
		t.Fatalf("ВМ: %v", err)
	}
	scr2.Close()
	return treeOut.String(), vmOut.String()
}

func checkBoth(t *testing.T, src, input string) string {
	t.Helper()
	want, got := runBoth(t, src, input, t.TempDir(), 0)
	if want != got {
		t.Fatalf("интерпретатор и ВМ разошлись\n--- дерево ---\n%s\n--- ВМ ---\n%s", want, got)
	}
	return got
}

// TestVMExpressionParity сравнивает интерпретатор и ВМ на выражениях.
func TestVMExpressionParity(t *testing.T) {
	cases := []string{
		`print{math{10%3}}`,
		`print{"a"+"b"}print{"x"*3}`,
		`print{len["привет"]}print{len[{1,2,3}]}`,
		`print{int{"42"}}print{float{"2.5"}}print{double{7}}`,
		`print{math{int 10/3}}print{math{float 10/3}}print{math{10/3}}`,
		`set{a=2}set{b=3}if[[a]<[b] and [b]>[a]]{print{"and"}}if[[a]>[b] or [b]>[a]]{print{"or"}}`,
		`set{st="BOLD"}set{cl="GREEN"}inp[style_text]print{style{[st],"x"}}print{color{[cl],"y"}}`,
		`inp[style_text]print{color{BRIGHT_RED,"x"}}print{bg{BRIGHT_BLUE,"y"}}`,
		`inp[style_text]set{c="BRIGHT_CYAN"}print{fg{[c],"z"}}set{b="BRIGHT_WHITE"}print{bg{[b],"w"}}`,
		`set{x=1}set{l={foo,bar,[x],2}}print{len[l]}print{l.0}print{l.2}`,
		`print{math{double 1/3}}`,
		`print{"a\nb"}`,
		`print{12.3456}print{1.0001}print{0.9999}print{-12.3456}`,
		`set{i=0}cycle[2.5]{print{"x"}set{i=math{[i]+1}}if[[i]>=5]{sc}}`,
		`set{i=0}cycle[-1]{print{"y"}set{i=math{[i]+1}}if[[i]>=3]{sc}}`,
		`def_twice[x]{ret[math{[x]*2}]}exec["print{twice{21}}"]`,
		`inp[style_text]exec["print{bold{\"b\"}}"]`,
		`inp[style_text]print{len[color{GREEN,"abc"}]}`,
		`print{1<2}print{"2"<3}print{"a"=="a"}print{char{"abc"}}print{str{12}}`,
	}
	for _, src := range cases {
		got := checkBoth(t, src, "")
		if got == "" {
			t.Errorf("пустой вывод для %q", src)
		}
	}
}

// TestVMInputParity проверяет повторный ввод при неверном типе.
func TestVMInputParity(t *testing.T) {
	src := `input_0{int"число: "}print{"вы ввели "[inp_0]}`
	out := checkBoth(t, src, "нет\n7\n")
	if !strings.Contains(out, "Ожидается целое число") {
		t.Fatalf("нет сообщения о неверном типе: %q", out)
	}
	if !strings.HasSuffix(out, "вы ввели 7\n") {
		t.Fatalf("неверный результат: %q", out)
	}
}

// TestVMFileLibrary проверяет inp[библиотека] из .kn-файла.
func TestVMFileLibrary(t *testing.T) {
	dir := t.TempDir()
	lib := `def_twice[x]{ret[math{2*[x]}]}
kon_hi{print{"hi"}}
`
	if err := os.WriteFile(filepath.Join(dir, "mylib.kn"), []byte(lib), 0o644); err != nil {
		t.Fatal(err)
	}
	src := `inp[mylib]print{twice{21}}exec["print{twice{22}}"]exec[kon_hi]`
	want, got := runBoth(t, src, "", dir, 0)
	if want != got {
		t.Fatalf("интерпретатор и ВМ разошлись: %q vs %q", want, got)
	}
	if got != "42\n44\nhi\n" {
		t.Fatalf("получено %q", got)
	}
}

// TestRecursiveLibraryRejected проверяет защиту от рекурсивного inp[].
func TestRecursiveLibraryRejected(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "loop.kn"), []byte("inp[loop]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prog, err := parser.Parse(`inp[loop]`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compiler.CompileWith(compiler.Options{Dir: dir}, prog); err == nil ||
		!strings.Contains(err.Error(), "рекурсивный импорт") {
		t.Fatalf("компилятор: ожидалась ошибка рекурсивного импорта, получено %v", err)
	}
	scr := term.New(term.Options{ForceLine: true, In: strings.NewReader(""), Out: &bytes.Buffer{}})
	defer scr.Close()
	in := interp.New(interp.Options{Screen: scr, Dir: dir})
	if _, err := in.Run(prog); err == nil || !strings.Contains(err.Error(), "рекурсивный импорт") {
		t.Fatalf("интерпретатор: ожидалась ошибка рекурсивного импорта, получено %v", err)
	}
}

// TestCompilerStaticChecks проверяет статические ограничения компилятора.
func TestCompilerStaticChecks(t *testing.T) {
	prog, err := parser.Parse(`missing_fn{}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compiler.Compile(prog); err == nil || !strings.Contains(err.Error(), "неизвестная функция") {
		t.Fatalf("ожидалась ошибка неизвестной функции, получено %v", err)
	}
	// exec-строка компилируется с AllowUnknownCalls, поэтому там имя проверяется в ВМ.
	if _, err := parser.Parse(`def_f[a,b,c,d,e,f,g,h,i]{}`); err == nil {
		t.Fatal("ожидалась ошибка: больше 8 параметров")
	}
}

// TestBuiltinArityErrors проверяет, что нехватка аргументов даёт ошибку, а не панику.
func TestBuiltinArityErrors(t *testing.T) {
	cases := []string{
		`print{len{}}`,
		`print{int{}}`,
		`print{len{1,2}}`,
		`print{int{1,2}}`,
		`inp[style_text]print{bold{}}`,
		`inp[style_text]print{color{RED}}`,
		`openFile{}`,
		`writeFile{"x"}`,
	}
	for _, src := range cases {
		prog, err := parser.Parse(src)
		if err != nil {
			continue
		}
		if _, err := compiler.Compile(prog); err == nil {
			t.Errorf("компилятор принял %q", src)
		}
		scr := term.New(term.Options{ForceLine: true, In: strings.NewReader(""), Out: &bytes.Buffer{}})
		in := interp.New(interp.Options{Screen: scr, Dir: t.TempDir()})
		if _, err := in.Run(prog); err == nil {
			t.Errorf("интерпретатор принял %q", src)
		}
		scr.Close()
	}
}

// TestImageCP866 проверяет образ для DOS/ProS и обратное декодирование.
func TestImageCP866(t *testing.T) {
	src := `set{фрукты={яблоко,груша,слива}}print{"Всего: "[len[фрукты]]}print{фрукты.1}`
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	image, err := compiler.CompileWith(compiler.Options{
		Encoding: compiler.EncodingCP866,
		Target:   compiler.TargetDOS,
	}, prog)
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
	if loaded.Encoding != compiler.EncodingCP866 || loaded.Target != compiler.TargetDOS {
		t.Fatal("метаданные CP866-образа потеряны")
	}
	var out bytes.Buffer
	scr := term.New(term.Options{ForceLine: true, In: strings.NewReader(""), Out: &out})
	m := vm.New(loaded, vm.Options{Screen: scr, Dir: t.TempDir()})
	if _, err := m.Run(); err != nil {
		t.Fatal(err)
	}
	if out.String() != "Всего: 3\nгруша\n" {
		t.Fatalf("получено %q", out.String())
	}
}

// TestCompileTooLargeConstant проверяет, что длинная строка не обрезается молча.
func TestCompileTooLargeConstant(t *testing.T) {
	src := fmt.Sprintf(`print{"%s"}`, strings.Repeat("a", 70000))
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if _, err := compiler.Compile(prog); err == nil {
		t.Fatal("ожидалась ошибка о слишком длинной константе")
	} else if !strings.Contains(err.Error(), "константа") {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
}

// TestDisasmTruncated проверяет, что дизассемблер не паникует на обрезанном коде.
func TestDisasmTruncated(t *testing.T) {
	for _, code := range [][]byte{
		{byte(compiler.OpPUSHK)},
		{byte(compiler.OpJMP), 0x01},
		{byte(compiler.OpCALL), 0x00},
		{byte(compiler.OpDEF), 0x00, 0x00, 0x01},
	} {
		p := &compiler.Program{Code: code}
		text := p.Disasm()
		if !strings.Contains(text, "обрезано") {
			t.Errorf("для % X получено %q", code, text)
		}
	}
}

// TestImageExecImport проверяет, что маркер IMPORT переживает сохранение образа.
func TestImageExecImport(t *testing.T) {
	src := `inp[style_text]exec["print{bold{\"B\"}}"]`
	prog, err := parser.Parse(src)
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
	if !strings.Contains(loaded.Disasm(), "IMPORT") {
		t.Fatal("в образе нет команды IMPORT")
	}
	var out bytes.Buffer
	scr := term.New(term.Options{ForceLine: true, In: strings.NewReader(""), Out: &out})
	m := vm.New(loaded, vm.Options{Screen: scr, Dir: t.TempDir()})
	if _, err := m.Run(); err != nil {
		t.Fatal(err)
	}
	if out.String() != "B\n" {
		t.Fatalf("получено %q", out.String())
	}
}

// TestImageCorrupt проверяет отказ от повреждённого образа.
func TestImageCorrupt(t *testing.T) {
	prog, err := parser.Parse(`print{"x"}`)
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
	if _, err := compiler.Unmarshal(data[:len(data)-1]); err == nil {
		t.Fatal("ожидалась ошибка для обрезанного образа")
	}
	data[len(data)-1] ^= 0xFF
	if _, err := compiler.Unmarshal(data); err == nil {
		t.Fatal("ожидалась ошибка контрольной суммы")
	}
}
