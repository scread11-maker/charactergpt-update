package singleinstance

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strings"
)

func mutexName(component, root string) string {
	clean := strings.ToLower(filepath.Clean(root))
	sum := sha256.Sum256([]byte(clean))
	return fmt.Sprintf("Local\\SSPGPT_v07_%s_%x", component, sum[:8])
}
