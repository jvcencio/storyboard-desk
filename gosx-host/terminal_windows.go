package main

import (
	"golang.org/x/sys/windows"
	"os"
)

func hideConsoleInput() (func(), error) {
	handle := windows.Handle(os.Stdin.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return nil, err
	}
	if err := windows.SetConsoleMode(handle, mode&^windows.ENABLE_ECHO_INPUT); err != nil {
		return nil, err
	}
	return func() { windows.SetConsoleMode(handle, mode) }, nil
}
