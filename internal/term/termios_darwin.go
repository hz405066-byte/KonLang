//go:build darwin

package term

import "syscall"

const (
	getTermiosReq = syscall.TIOCGETA
	setTermiosReq = syscall.TIOCSETA
)
