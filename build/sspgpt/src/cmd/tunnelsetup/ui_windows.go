//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

func showMessage(title, text string) {
	u := syscall.NewLazyDLL("user32.dll")
	p := u.NewProc("MessageBoxW")
	t, _ := syscall.UTF16PtrFromString(text)
	h, _ := syscall.UTF16PtrFromString(title)
	p.Call(0, uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(h)), 0x40)
}
