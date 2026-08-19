//go:build !windows

package localinfer

import "syscall"

func hiddenSysProcAttr() *syscall.SysProcAttr { return nil }
func detectNVIDIAGPU() (bool, string)         { return false, "" }
