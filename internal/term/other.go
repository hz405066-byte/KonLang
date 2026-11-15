//go:build !linux && !darwin && !windows

package term

// Заглушка для прочих платформ: TUI недоступен, работаем построчно.
func isTTY() bool                                { return false }
func terminalSize() (int, int)                   { return 80, 25 }
func newNativeScreen(w, h int, o Options) Screen { return nil }
