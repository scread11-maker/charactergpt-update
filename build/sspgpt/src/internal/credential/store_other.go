//go:build !windows

package credential

import (
	"encoding/json"
	"errors"
	"os"
	"strings"

	"sspgpt/v07/internal/profilepath"
)

type fileData struct {
	APIKey string `json:"api_key"`
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
	b, _ := json.Marshal(fileData{APIKey: key})
	return os.WriteFile(profilepath.CredentialsJSON(root), b, 0600)
}
func Load(root string) string {
	b, err := os.ReadFile(profilepath.CredentialsJSON(root))
	if err != nil {
		return ""
	}
	var x fileData
	if json.Unmarshal(b, &x) != nil {
		return ""
	}
	return strings.TrimSpace(x.APIKey)
}
func Clear(root string) error {
	err := os.Remove(profilepath.CredentialsJSON(root))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
