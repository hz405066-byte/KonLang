package tests

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestCLIBuildAndRun проверяет полный путь: сборка CLI, build native,
// запуск автономного бинарника, образ .knc и дизассемблер.
func TestCLIBuildAndRun(t *testing.T) {
	if testing.Short() {
		t.Skip("интеграционный тест пропущен в -short")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go недоступен в PATH")
	}
	tmp := t.TempDir()
	moduleRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}

	bin := filepath.Join(tmp, "konlang")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	buildCmd := exec.Command("go", "build", "-o", bin, "./cmd/konlang")
	buildCmd.Dir = moduleRoot
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("сборка konlang: %v\n%s", err, out)
	}

	src := filepath.Join(tmp, "hello.kn")
	if err := os.WriteFile(src, []byte("print{\"cli-ok\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	native := filepath.Join(tmp, "hello")
	if runtime.GOOS == "windows" {
		native += ".exe"
	}
	out, err := exec.Command(bin, "build", "-o", native, src).CombinedOutput()
	if err != nil {
		t.Fatalf("build native: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "собрано") {
		t.Fatalf("нет сообщения о сборке: %s", out)
	}
	out, err = exec.Command(native).CombinedOutput()
	if err != nil {
		t.Fatalf("запуск native: %v\n%s", err, out)
	}
	if string(out) != "cli-ok\n" {
		t.Fatalf("native вывел %q", out)
	}

	image := filepath.Join(tmp, "hello.knc")
	out, err = exec.Command(bin, "build", "-target", "bc", "-o", image, src).CombinedOutput()
	if err != nil {
		t.Fatalf("build bc: %v\n%s", err, out)
	}
	out, err = exec.Command(bin, "run", image).CombinedOutput()
	if err != nil {
		t.Fatalf("run .knc: %v\n%s", err, out)
	}
	if string(out) != "cli-ok\n" {
		t.Fatalf("образ вывел %q", out)
	}
	out, err = exec.Command(bin, "disasm", image).CombinedOutput()
	if err != nil {
		t.Fatalf("disasm: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "PUSHK") || !strings.Contains(string(out), "HALT") {
		t.Fatalf("неожиданный дизассемблер:\n%s", out)
	}

	// dos-образ должен иметь кодировку CP866 и цель DOS в заголовке.
	dosImage := filepath.Join(tmp, "hello-dos.knc")
	if out, err := exec.Command(bin, "build", "-target", "dos", "-o", dosImage, src).CombinedOutput(); err != nil {
		t.Fatalf("build dos: %v\n%s", err, out)
	}
	data, err := os.ReadFile(dosImage)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 24 || string(data[:4]) != "KNBC" || data[5] != 1 || data[6] != 1 {
		preview := data
		if len(preview) > 8 {
			preview = preview[:8]
		}
		t.Fatalf("неверный заголовок dos-образа: % X", preview)
	}
}
