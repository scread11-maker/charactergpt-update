//go:build windows

package singleinstance

import (
	"syscall"
	"unsafe"
)

const errorAlreadyExists syscall.Errno = 183

var (
	kernel32     = syscall.NewLazyDLL("kernel32.dll")
	createMutex  = kernel32.NewProc("CreateMutexW")
	mutexHandles []syscall.Handle
)

// Acquire returns false when another process for the same CharacterGPT
// component and Ghost root already owns the mutex. The handle is intentionally
// kept open for the lifetime of the process and is released by Windows on exit.
func Acquire(component, root string) bool {
	name, err := syscall.UTF16PtrFromString(mutexName(component, root))
	if err != nil {
		return true
	}
	r1, _, callErr := createMutex.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if r1 == 0 {
		// Do not prevent startup on an unexpected mutex API failure; the existing
		// listener bind remains the final safety boundary.
		return true
	}
	if errno, ok := callErr.(syscall.Errno); ok && errno == errorAlreadyExists {
		syscall.CloseHandle(syscall.Handle(r1))
		return false
	}
	mutexHandles = append(mutexHandles, syscall.Handle(r1))
	return true
}
