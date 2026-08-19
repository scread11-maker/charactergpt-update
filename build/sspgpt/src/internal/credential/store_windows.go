//go:build windows

package credential

import (
	"errors"
	"os"
	"strings"
	"syscall"
	"unsafe"

	"sspgpt/v07/internal/profilepath"
)

type dataBlob struct {
	cbData uint32
	pbData *byte
}

var (
	crypt32       = syscall.NewLazyDLL("crypt32.dll")
	kernel32      = syscall.NewLazyDLL("kernel32.dll")
	procProtect   = crypt32.NewProc("CryptProtectData")
	procUnprotect = crypt32.NewProc("CryptUnprotectData")
	procLocalFree = kernel32.NewProc("LocalFree")
)

const cryptprotectUIForbidden = 0x1

func blobFromBytes(b []byte) dataBlob {
	if len(b) == 0 {
		return dataBlob{}
	}
	return dataBlob{cbData: uint32(len(b)), pbData: &b[0]}
}

func blobBytes(b dataBlob) []byte {
	if b.cbData == 0 || b.pbData == nil {
		return nil
	}
	return append([]byte(nil), unsafe.Slice(b.pbData, int(b.cbData))...)
}

func protect(clear []byte) ([]byte, error) {
	in := blobFromBytes(clear)
	var out dataBlob
	r, _, e := procProtect.Call(
		uintptr(unsafe.Pointer(&in)),
		0,
		0,
		0,
		0,
		cryptprotectUIForbidden,
		uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		if e != nil && e != syscall.Errno(0) {
			return nil, e
		}
		return nil, errors.New("CryptProtectData failed")
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	return blobBytes(out), nil
}

func unprotect(cipher []byte) ([]byte, error) {
	in := blobFromBytes(cipher)
	var out dataBlob
	r, _, e := procUnprotect.Call(
		uintptr(unsafe.Pointer(&in)),
		0,
		0,
		0,
		0,
		cryptprotectUIForbidden,
		uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		if e != nil && e != syscall.Errno(0) {
			return nil, e
		}
		return nil, errors.New("CryptUnprotectData failed")
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	return blobBytes(out), nil
}

func Save(root, key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("empty api key")
	}
	dir := profilepath.Secrets(root)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	cipher, err := protect([]byte(key))
	if err != nil {
		return err
	}
	return os.WriteFile(profilepath.CredentialsDAT(root), cipher, 0600)
}

func Load(root string) string {
	cipher, err := os.ReadFile(profilepath.CredentialsDAT(root))
	if err != nil {
		return ""
	}
	clear, err := unprotect(cipher)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(clear))
}

func Clear(root string) error {
	err := os.Remove(profilepath.CredentialsDAT(root))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
