//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

func hideProcess(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
