package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sspgpt/v07/internal/paths"
	"time"
)

func main() {
	root := paths.GhostRoot()
	plug := filepath.Join(root, "Plug")
	_ = os.MkdirAll(plug, 0755)
	cfg := map[string]any{"auto_tunnel": true, "cloudflared": "cloudflared.exe"}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	_ = os.WriteFile(filepath.Join(plug, "link_config.json"), b, 0644)
	msg := "已啟用 ChatGPT Quick Tunnel。\n請關閉再重新開啟「ChatGPT連動」。\n連線網址會寫入 Plug\\connection_url.txt。"
	cl := &http.Client{Timeout: 600 * time.Millisecond}
	if resp, e := cl.Get("http://127.0.0.1:8782/status"); e == nil {
		resp.Body.Close()
		msg = "設定已儲存。若連動目前已開啟，請重新連線以套用 Tunnel。"
	}
	showMessage("CharacterGPT 連線設定", msg)
}
