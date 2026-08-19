package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sspgpt/v07/internal/paths"
	"strings"
)

func main() {
	root := paths.GhostRoot()
	b, err := os.ReadFile(filepath.Join(root, "Plug", "link.secret"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "link secret unavailable:", err)
		return
	}
	token := strings.TrimSpace(string(b))
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 4096), 4<<20)
	enc := json.NewEncoder(os.Stdout)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		req, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:8782/mcp?token="+token, bytes.NewReader(line))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "error": map[string]any{"code": -32000, "message": err.Error()}})
			continue
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		var x any
		if json.Unmarshal(raw, &x) == nil {
			_ = enc.Encode(x)
		}
	}
}
