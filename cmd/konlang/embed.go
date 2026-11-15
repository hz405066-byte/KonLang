package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"runtime"

	"github.com/konlang/konlang/internal/compiler"
	"github.com/konlang/konlang/internal/term"
	"github.com/konlang/konlang/internal/vm"
)

// Автономный бинарник = обычный konlang + образ байт-кода + футер.
// Это позволяет `konlang build` получать исполняемый файл без внешнего ВМ.
const embedFooterSize = 16

var embedMagic = [8]byte{'K', 'N', 'L', 'A', 'N', 'G', '0', '1'}

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

func writeEmbedded(path string, image []byte) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("не удалось найти исполняемый файл konlang: %v", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return err
	}
	base, err := os.ReadFile(exe)
	if err != nil {
		return fmt.Errorf("не удалось прочитать %s: %v", exe, err)
	}
	out := make([]byte, 0, len(base)+len(image)+embedFooterSize)
	out = append(out, base...)
	out = append(out, image...)
	footer := make([]byte, embedFooterSize)
	copy(footer[:8], embedMagic[:])
	binary.LittleEndian.PutUint32(footer[8:12], uint32(len(image)))
	binary.LittleEndian.PutUint32(footer[12:16], crc32.ChecksumIEEE(image))
	out = append(out, footer...)
	mode := os.FileMode(0o755)
	if runtime.GOOS == "windows" {
		mode = 0o755
	}
	if err := os.WriteFile(path, out, mode); err != nil {
		return err
	}
	return nil
}

func embeddedImage() ([]byte, bool) {
	exe, err := os.Executable()
	if err != nil {
		return nil, false
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return nil, false
	}
	f, err := os.Open(exe)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.Size() < embedFooterSize {
		return nil, false
	}
	footer := make([]byte, embedFooterSize)
	if _, err := f.ReadAt(footer, st.Size()-embedFooterSize); err != nil {
		return nil, false
	}
	if !bytes.Equal(footer[:8], embedMagic[:]) {
		return nil, false
	}
	n := int64(binary.LittleEndian.Uint32(footer[8:12]))
	wantCRC := binary.LittleEndian.Uint32(footer[12:16])
	if n <= 0 || n > st.Size()-embedFooterSize {
		return nil, false
	}
	image := make([]byte, n)
	if _, err := f.ReadAt(image, st.Size()-embedFooterSize-n); err != nil {
		return nil, false
	}
	if crc32.ChecksumIEEE(image) != wantCRC {
		return nil, false
	}
	return image, true
}

// runEmbedded запускает образ, встроенный в автономный бинарник.
func runEmbedded(image []byte) int {
	prog, err := compiler.Unmarshal(image)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	scr := term.New(term.Options{})
	dir, _ := os.Getwd()
	m := vm.New(prog, vm.Options{
		Screen: scr,
		Dir:    dir,
		Trace:  os.Getenv("KONLANG_TRACE") != "",
	})
	code, err := m.Run()
	scr.Flush()
	scr.Close()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return code
}
