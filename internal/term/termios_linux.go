//go:build linux

package term

import "syscall"

const (
	getTermiosReq = syscall.TCGETS
	setTermiosReq = syscall.TCSETS
)
