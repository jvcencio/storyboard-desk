package main

import (
	"golang.org/x/sys/unix"
	"os"
)

func hideConsoleInput() (func(), error) {
	fd := int(os.Stdin.Fd())
	state, err := unix.IoctlGetTermios(fd, unix.TIOCGETA)
	if err != nil {
		return nil, err
	}
	before := *state
	state.Lflag &^= unix.ECHO
	if err = unix.IoctlSetTermios(fd, unix.TIOCSETA, state); err != nil {
		return nil, err
	}
	return func() { unix.IoctlSetTermios(fd, unix.TIOCSETA, &before) }, nil
}
