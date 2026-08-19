//go:build windows

package localinfer

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func hiddenSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}

func detectNVIDIAGPU() (bool, string) {
	candidates := []string{"nvidia-smi.exe"}
	if p := os.Getenv("ProgramW6432"); p != "" {
		candidates = append(candidates, filepath.Join(p, "NVIDIA Corporation", "NVSMI", "nvidia-smi.exe"))
	}
	if p := os.Getenv("ProgramFiles"); p != "" {
		candidates = append(candidates, filepath.Join(p, "NVIDIA Corporation", "NVSMI", "nvidia-smi.exe"))
	}
	seen := map[string]bool{}
	for _, exe := range candidates {
		k := strings.ToLower(exe)
		if seen[k] {
			continue
		}
		seen[k] = true
		cmd := exec.Command(exe, "--query-gpu=name,memory.total", "--format=csv,noheader,nounits")
		cmd.SysProcAttr = hiddenSysProcAttr()
		b, err := cmd.Output()
		if err != nil {
			continue
		}
		line := strings.TrimSpace(string(b))
		if line == "" {
			continue
		}
		if i := strings.IndexByte(line, '\n'); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		fields := strings.Split(line, ",")
		return true, strings.TrimSpace(fields[0])
	}
	return false, ""
}
