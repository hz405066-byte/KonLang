// konlang — интерпретатор и компилятор языка KonLang (редакция 5 «Curses»).
package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/konlang/konlang/internal/ast"
	"github.com/konlang/konlang/internal/compiler"
	"github.com/konlang/konlang/internal/interp"
	"github.com/konlang/konlang/internal/lexer"
	"github.com/konlang/konlang/internal/parser"
	"github.com/konlang/konlang/internal/term"
	"github.com/konlang/konlang/internal/vm"
)

// Version — версия инструмента.
const Version = "1.1.0"

func main() {
	if image, ok := embeddedImage(); ok {
		os.Exit(runEmbedded(image))
	}
	args := os.Args[1:]
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}
	cmd, rest := args[0], args[1:]
	var err error
	switch cmd {
	case "run":
		err = cmdRun(rest)
	case "vm":
		err = cmdRun(rest)
	case "build":
		err = cmdBuild(rest)
	case "disasm":
		err = cmdDisasm(rest)
	case "repl":
		err = cmdREPL(rest)
	case "check":
		err = cmdCheck(rest)
	case "ast":
		err = cmdAST(rest)
	case "tokens":
		err = cmdTokens(rest)
	case "version", "--version", "-v":
		fmt.Printf("konlang %s (KonLang редакция 5 «Curses»)\n", Version)
	case "help", "--help", "-h":
		usage()
	default:
		// konlang program.kn — короткая форма запуска
		if strings.HasSuffix(cmd, ".kn") {
			err = cmdRun(args)
		} else {
			fmt.Fprintf(os.Stderr, "неизвестная команда %q\n\n", cmd)
			usage()
			os.Exit(2)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Print(`konlang — интерпретатор/компилятор KonLang v5 (Curses)

Использование:
  konlang run [флаги] файл.kn     запустить через древовидный интерпретатор
  konlang run --vm [флаги] файл.kn  скомпилировать и запустить в байт-код-ВМ
  konlang run [флаги] файл.knc    запустить готовый образ байт-кода
  konlang vm файл.knc             то же, что run файл.knc
  konlang build [флаги] файл.kn   собрать автономный бинарник или образ .knc
  konlang disasm файл.knc         показать дизассемблер образа
  konlang repl                    интерактивная оболочка
  konlang check файл.kn           проверка синтаксиса и компиляции
  konlang ast файл.kn             показать дерево разбора
  konlang tokens файл.kn          показать токены
  konlang version                 версия

Флаги run:
  --vm            выполнить скомпилированный байт-код (ВМ), а не дерево разбора
  --screen        буферный TUI-режим даже без терминала (кадр печатается в конце)
  --width N       ширина экрана (по умолчанию — размер терминала, иначе 80)
  --height N      высота экрана (по умолчанию 25)
  --seed N        зерно генератора случайных чисел (для повторяемости)
  --trace         трассировка конструкций/опкодов в stderr

Флаги build:
  -target native|bc|dos|pros  native — автономный бинарник с встроенной ВМ
                              (по умолчанию); bc — переносимый образ .knc;
                              dos/pros — образ в CP866 для внешнего рантайма
  -o файл                     имя результата
  --emit-asm                  сохранить NASM-файл с данными образа
  --emit-bc                   сохранить дизассемблер байт-кода в .disasm.txt
`)
}

// ---------- run ----------

type runFlags struct {
	screen bool
	vm     bool
	width  int
	height int
	seed   int64
	trace  bool
	file   string
}

func parseRunFlags(args []string) (runFlags, error) {
	f := runFlags{}
	i := 0
	for i < len(args) {
		a := args[i]
		switch {
		case a == "--screen":
			f.screen = true
		case a == "--vm" || a == "--bytecode":
			f.vm = true
		case a == "--trace":
			f.trace = true
		case a == "--width" || a == "--height" || a == "--seed":
			if i+1 >= len(args) {
				return f, fmt.Errorf("флаг %s требует значения", a)
			}
			n, err := strconv.Atoi(args[i+1])
			if err != nil {
				return f, fmt.Errorf("флаг %s: %v", a, err)
			}
			switch a {
			case "--width":
				f.width = n
			case "--height":
				f.height = n
			case "--seed":
				f.seed = int64(n)
			}
			i++
		case strings.HasPrefix(a, "-"):
			return f, fmt.Errorf("неизвестный флаг %s", a)
		default:
			if f.file != "" {
				return f, fmt.Errorf("указано несколько файлов")
			}
			f.file = a
		}
		i++
	}
	if f.file == "" {
		return f, fmt.Errorf("не указан файл .kn")
	}
	return f, nil
}

func cmdRun(args []string) error {
	f, err := parseRunFlags(args)
	if err != nil {
		return err
	}
	dir, _ := filepath.Abs(filepath.Dir(f.file))

	var (
		tree  *ast.Node
		image *compiler.Program
	)
	if strings.HasSuffix(strings.ToLower(f.file), ".knc") {
		if f.vm {
			return fmt.Errorf("%s: это уже образ байт-кода", f.file)
		}
		image, err = loadImage(f.file)
		if err != nil {
			return err
		}
	} else {
		tree, err = parser.ParseFile(f.file)
		if err != nil {
			return err
		}
		if f.vm {
			image, err = compiler.CompileWith(compiler.Options{Dir: dir}, tree)
			if err != nil {
				return err
			}
		}
	}

	scr := term.New(term.Options{
		ForceScreen: f.screen,
		Width:       f.width,
		Height:      f.height,
	})
	code := 0
	if image != nil {
		m := vm.New(image, vm.Options{Screen: scr, Dir: dir, Seed: f.seed, Trace: f.trace})
		code, err = m.Run()
	} else {
		in := interp.New(interp.Options{Screen: scr, Dir: dir, Seed: f.seed, Trace: f.trace})
		code, err = in.Run(tree)
	}
	scr.Flush()
	scr.Close()
	if err != nil {
		return err
	}
	if code != 0 {
		os.Exit(code)
	}
	return nil
}

func loadImage(path string) (*compiler.Program, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	p, err := compiler.Unmarshal(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", path, err)
	}
	return p, nil
}

// ---------- build / disasm ----------

type buildFlags struct {
	target  string
	out     string
	emitBC  bool
	emitASM bool
	file    string
}

func parseBuildFlags(args []string) (buildFlags, error) {
	f := buildFlags{target: "native"}
	i := 0
	for i < len(args) {
		a := args[i]
		switch {
		case a == "-target" || a == "--target":
			if i+1 >= len(args) {
				return f, fmt.Errorf("флаг %s требует значения", a)
			}
			f.target = args[i+1]
			i++
		case a == "-o" || a == "--output":
			if i+1 >= len(args) {
				return f, fmt.Errorf("флаг %s требует значения", a)
			}
			f.out = args[i+1]
			i++
		case a == "--emit-bc":
			f.emitBC = true
		case a == "--emit-asm":
			f.emitASM = true
		case strings.HasPrefix(a, "-"):
			return f, fmt.Errorf("неизвестный флаг %s", a)
		default:
			if f.file != "" {
				return f, fmt.Errorf("указано несколько файлов")
			}
			f.file = a
		}
		i++
	}
	if f.file == "" {
		return f, fmt.Errorf("не указан файл .kn")
	}
	return f, nil
}

func cmdBuild(args []string) error {
	f, err := parseBuildFlags(args)
	if err != nil {
		return err
	}
	var target compiler.Target
	var encoding compiler.Encoding
	native := false
	switch strings.ToLower(f.target) {
	case "native", "exe":
		target, encoding, native = compiler.TargetPortable, compiler.EncodingUTF8, true
	case "bc", "portable", "image", "knc":
		target, encoding = compiler.TargetPortable, compiler.EncodingUTF8
	case "dos":
		target, encoding = compiler.TargetDOS, compiler.EncodingCP866
	case "pros":
		target, encoding = compiler.TargetProS, compiler.EncodingCP866
	default:
		return fmt.Errorf("неизвестная цель %q (ожидается native, bc, dos или pros)", f.target)
	}

	prog, err := parser.ParseFile(f.file)
	if err != nil {
		return err
	}
	dir, _ := filepath.Abs(filepath.Dir(f.file))
	image, err := compiler.CompileWith(compiler.Options{
		Encoding: encoding,
		Target:   target,
		Dir:      dir,
	}, prog)
	if err != nil {
		return err
	}
	data, err := image.Marshal()
	if err != nil {
		return err
	}
	out := f.out
	if out == "" {
		base := strings.TrimSuffix(filepath.Base(f.file), filepath.Ext(f.file))
		if native {
			base += exeSuffix()
		} else {
			base += ".knc"
		}
		out = filepath.Join(filepath.Dir(f.file), base)
	}
	if native {
		if err := writeEmbedded(out, data); err != nil {
			return err
		}
	} else if err := os.WriteFile(out, data, 0o644); err != nil {
		return err
	}
	kind := image.Target.String()
	size := len(data)
	if native {
		kind = "native"
		if st, statErr := os.Stat(out); statErr == nil {
			size = int(st.Size())
		}
	}
	fmt.Printf("собрано: %s (%d байт, цель %s, кодировка %s)\n",
		out, size, kind, image.Encoding)

	stem := strings.TrimSuffix(out, filepath.Ext(out))
	if f.emitBC {
		path := stem + ".disasm.txt"
		if err := os.WriteFile(path, []byte(image.Disasm()), 0o644); err != nil {
			return err
		}
		fmt.Printf("байт-код: %s\n", path)
	}
	if f.emitASM {
		path := stem + ".asm"
		if err := os.WriteFile(path, []byte(imageNASM(data, out)), 0o644); err != nil {
			return err
		}
		fmt.Printf("NASM-данные: %s\n", path)
	}
	return nil
}

// imageNASM сохраняет полный образ как данные для NASM (для встраивания в рантайм).
func imageNASM(data []byte, source string) string {
	var sb strings.Builder
	sb.WriteString("; KonLang bytecode image (формат KNBC)\n")
	fmt.Fprintf(&sb, "; source: %s\n", source)
	sb.WriteString("BITS 16\nkn_image:\n")
	for i, b := range data {
		if i%16 == 0 {
			sb.WriteString("    db ")
		} else {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, "0x%02X", b)
		if i%16 == 15 || i == len(data)-1 {
			sb.WriteString("\n")
		}
	}
	sb.WriteString("kn_image_end:\n")
	return sb.String()
}

func cmdDisasm(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("не указан файл .knc")
	}
	p, err := loadImage(args[0])
	if err != nil {
		return err
	}
	fmt.Printf("; цель: %s, кодировка: %s, код: %d байт, пул констант: %d байт\n",
		p.Target, p.Encoding, len(p.Code), len(p.Consts))
	fmt.Print(p.Disasm())
	return nil
}

// ---------- check / ast / tokens ----------

func cmdCheck(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("не указан файл")
	}
	for _, f := range args {
		if strings.HasSuffix(strings.ToLower(f), ".knc") {
			if _, err := loadImage(f); err != nil {
				return fmt.Errorf("%s: %v", f, err)
			}
			fmt.Printf("%s: OK (образ байт-кода)\n", f)
			continue
		}
		prog, err := parser.ParseFile(f)
		if err != nil {
			return fmt.Errorf("%s: %v", f, err)
		}
		dir, _ := filepath.Abs(filepath.Dir(f))
		if _, err := compiler.CompileWith(compiler.Options{Dir: dir}, prog); err != nil {
			return fmt.Errorf("%s: %v", f, err)
		}
		fmt.Printf("%s: OK\n", f)
	}
	return nil
}

func cmdAST(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("не указан файл")
	}
	prog, err := parser.ParseFile(args[0])
	if err != nil {
		return err
	}
	var out []string
	ast.Dump(prog, "", &out)
	fmt.Println(strings.Join(out, "\n"))
	return nil
}

func cmdTokens(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("не указан файл")
	}
	b, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	toks, err := lexer.Lex(string(b))
	if err != nil {
		return err
	}
	for _, t := range toks {
		fmt.Printf("%-8s %-10s %q\n", t.Pos, t.Kind, t.Text)
	}
	return nil
}

// ---------- repl ----------

func cmdREPL(args []string) error {
	fmt.Printf("KonLang %s REPL — :help справка, :quit выход\n", Version)
	scr := term.New(term.Options{})
	defer scr.Close()
	wd, _ := os.Getwd()
	in := interp.New(interp.Options{Screen: scr, Dir: wd})
	rd := bufio.NewReader(os.Stdin)
	var buf strings.Builder
	for {
		if buf.Len() == 0 {
			fmt.Print("kn> ")
		} else {
			fmt.Print("... ")
		}
		line, err := rd.ReadString('\n')
		if err != nil {
			fmt.Println()
			return nil
		}
		line = strings.TrimRight(line, "\r\n")
		if buf.Len() == 0 {
			switch strings.TrimSpace(line) {
			case ":quit", ":q", ":exit":
				return nil
			case ":help", ":h":
				fmt.Println(":quit — выход, :vars — переменные, :clear — очистить экран, :help — справка")
				continue
			case ":vars":
				for k, v := range in.Vars() {
					fmt.Printf("%s = %s (%s)\n", k, v.String(), v.Kind)
				}
				continue
			case "":
				continue
			}
		}
		buf.WriteString(line)
		buf.WriteString("\n")
		src := buf.String()
		if !balanced(src) {
			continue
		}
		buf.Reset()
		prog, err := parser.Parse(src)
		if err != nil {
			fmt.Println(err)
			continue
		}
		if _, err := in.Run(prog); err != nil {
			fmt.Println(err)
		}
		scr.Flush()
	}
}

func balanced(s string) bool {
	depth := 0
	inStr := false
	for _, r := range s {
		if inStr {
			if r == '"' {
				inStr = false
			}
			continue
		}
		switch r {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
		}
	}
	return depth <= 0 && !inStr
}
