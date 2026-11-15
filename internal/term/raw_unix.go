//go:build linux || darwin

package term

import (
	"os"
	"strconv"
	"syscall"
	"unsafe"
)

var (
	savedTermios syscall.Termios
	rawOn        bool
)

func ioctlTermios(fd, req uintptr, t *syscall.Termios) error {
	_, _, e := syscall.Syscall6(syscall.SYS_IOCTL, fd, req, uintptr(unsafe.Pointer(t)), 0, 0, 0)
	if e != 0 {
		return e
	}
	return nil
}

func isTTY() bool {
	var t syscall.Termios
	return ioctlTermios(os.Stdout.Fd(), getTermiosReq, &t) == nil
}

type winsize struct {
	Row, Col, Xpixel, Ypixel uint16
}

func terminalSize() (int, int) {
	var ws winsize
	_, _, e := syscall.Syscall6(syscall.SYS_IOCTL, os.Stdout.Fd(), uintptr(syscall.TIOCGWINSZ),
		uintptr(unsafe.Pointer(&ws)), 0, 0, 0)
	if e == 0 && ws.Col > 0 && ws.Row > 0 {
		return int(ws.Col), int(ws.Row)
	}
	w, _ := strconv.Atoi(os.Getenv("COLUMNS"))
	h, _ := strconv.Atoi(os.Getenv("LINES"))
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 25
	}
	return w, h
}

// enterRaw включает «сырой» режим с таймаутом чтения 100 мс.
func enterRaw() error {
	if rawOn {
		return nil
	}
	if err := ioctlTermios(os.Stdin.Fd(), getTermiosReq, &savedTermios); err != nil {
		return err
	}
	t := savedTermios
	t.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP |
		syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON
	t.Oflag &^= syscall.OPOST
	t.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	t.Cflag &^= syscall.CSIZE | syscall.PARENB
	t.Cflag |= syscall.CS8
	t.Cc[syscall.VMIN] = 0
	t.Cc[syscall.VTIME] = 1
	if err := ioctlTermios(os.Stdin.Fd(), setTermiosReq, &t); err != nil {
		return err
	}
	rawOn = true
	return nil
}

func exitRaw() {
	if !rawOn {
		return
	}
	ioctlTermios(os.Stdin.Fd(), setTermiosReq, &savedTermios)
	rawOn = false
}

// readByteTimeout читает байт с таймаутом ~100 мс (false, если ничего нет).
func readByteTimeout() (byte, bool) {
	var buf [1]byte
	n, err := os.Stdin.Read(buf[:])
	if err != nil || n == 0 {
		return 0, false
	}
	return buf[0], true
}

// readByte блокирующее чтение одного байта.
func readByte() (byte, error) {
	for {
		b, ok := readByteTimeout()
		if ok {
			return b, nil
		}
	}
}
