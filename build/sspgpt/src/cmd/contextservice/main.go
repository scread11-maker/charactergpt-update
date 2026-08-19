package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"sspgpt/v07/internal/paths"
	"sspgpt/v07/internal/singleinstance"
)

const version = "0.7.1-fix12"
const listenAddr = "127.0.0.1:8782"

type config struct {
	AutoTunnel  bool   `json:"auto_tunnel"`
	Cloudflared string `json:"cloudflared"`
}
type service struct {
	root         string
	log          *log.Logger
	secret       string
	mu           sync.RWMutex
	publicURL    string
	tunnel       *exec.Cmd
	shutdownOnce sync.Once
}
type rpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}
type rpcResp struct {
	JSONRPC string  `json:"jsonrpc"`
	ID      any     `json:"id,omitempty"`
	Result  any     `json:"result,omitempty"`
	Error   *rpcErr `json:"error,omitempty"`
}
type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
type toolCall struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

func main() {
	root := paths.GhostRoot()
	if !singleinstance.Acquire("ContextService", root) {
		return
	}
	plug := filepath.Join(root, "Plug")
	_ = os.MkdirAll(plug, 0755)
	_ = os.MkdirAll(filepath.Join(root, "logs"), 0755)
	lf, _ := os.OpenFile(filepath.Join(root, "logs", "link_context.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	s := &service{root: root, log: log.New(lf, "", log.LstdFlags|log.Lmicroseconds)}
	s.secret = s.loadSecret()
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.health)
	mux.HandleFunc("/status", s.status)
	mux.HandleFunc("/mcp", s.mcp)
	mux.HandleFunc("/shutdown", s.shutdown)
	go s.maybeTunnel()
	s.log.Printf("ContextService %s listening=%s", version, listenAddr)
	if err := http.ListenAndServe(listenAddr, mux); err != nil {
		s.log.Printf("server stopped: %v", err)
	}
}
func (s *service) loadSecret() string {
	p := filepath.Join(s.root, "Plug", "link.secret")
	if b, e := os.ReadFile(p); e == nil && strings.TrimSpace(string(b)) != "" {
		return strings.TrimSpace(string(b))
	}
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	x := hex.EncodeToString(b)
	_ = os.WriteFile(p, []byte(x), 0600)
	return x
}
func (s *service) cfg() config {
	c := config{Cloudflared: "cloudflared.exe"}
	if b, e := os.ReadFile(filepath.Join(s.root, "Plug", "link_config.json")); e == nil {
		_ = json.Unmarshal(b, &c)
	}
	return c
}
func (s *service) auth(r *http.Request) bool {
	t := strings.TrimSpace(r.URL.Query().Get("token"))
	if t == "" {
		t = strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	}
	return t != "" && t == s.secret
}
func (s *service) health(w http.ResponseWriter, r *http.Request) {
	write(w, 200, map[string]any{"ok": true, "service": "CharacterGPTContextService", "version": version})
}
func (s *service) status(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	u := s.publicURL
	s.mu.RUnlock()
	endpoint := "http://" + listenAddr + "/mcp?token=" + s.secret
	if u != "" {
		endpoint = strings.TrimRight(u, "/") + "/mcp?token=" + s.secret
	}
	write(w, 200, map[string]any{"ok": true, "version": version, "public_url": u, "mcp_url": endpoint, "tunnel": u != ""})
}
func (s *service) shutdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		write(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST required"})
		return
	}
	started := false
	s.shutdownOnce.Do(func() {
		started = true
		s.mu.Lock()
		if s.tunnel != nil && s.tunnel.Process != nil {
			_ = s.tunnel.Process.Kill()
		}
		s.tunnel = nil
		s.publicURL = ""
		s.mu.Unlock()
		s.log.Printf("SHUTDOWN_BEGIN tunnel_stopped=true")
		go func() {
			time.Sleep(120 * time.Millisecond)
			s.log.Printf("SHUTDOWN_COMPLETE")
			os.Exit(0)
		}()
	})
	write(w, http.StatusAccepted, map[string]any{"ok": true, "started": started, "service": "CharacterGPTContextService", "version": version})
}
func (s *service) mcp(w http.ResponseWriter, r *http.Request) {
	if !s.auth(r) {
		write(w, 401, map[string]string{"error": "unauthorized"})
		return
	}
	if r.Method != http.MethodPost {
		write(w, 405, map[string]string{"error": "POST required"})
		return
	}
	var q rpcReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 2<<20)).Decode(&q); err != nil {
		writeRPC(w, rpcResp{JSONRPC: "2.0", ID: q.ID, Error: &rpcErr{-32700, err.Error()}})
		return
	}
	res, er := s.dispatch(r.Context(), q)
	writeRPC(w, rpcResp{JSONRPC: "2.0", ID: q.ID, Result: res, Error: er})
}
func (s *service) dispatch(ctx context.Context, q rpcReq) (any, *rpcErr) {
	switch q.Method {
	case "initialize":
		return map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "SSPGPT-Muna", "version": version}}, nil
	case "notifications/initialized":
		return map[string]any{}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": tools()}, nil
	case "tools/call":
		var tc toolCall
		if err := json.Unmarshal(q.Params, &tc); err != nil {
			return nil, &rpcErr{-32602, err.Error()}
		}
		out, err := s.callRuntime(ctx, tc.Name, tc.Arguments)
		if err != nil {
			return map[string]any{"content": []map[string]any{{"type": "text", "text": err.Error()}}, "isError": true}, nil
		}
		b, _ := json.Marshal(out)
		return map[string]any{"content": []map[string]any{{"type": "text", "text": string(b)}}, "structuredContent": out}, nil
	default:
		return nil, &rpcErr{-32601, "method not found"}
	}
}
func tools() []map[string]any {
	o := func(props map[string]any, req ...string) map[string]any {
		return map[string]any{"type": "object", "properties": props, "required": req, "additionalProperties": false}
	}
	s := map[string]any{"type": "string"}
	n := map[string]any{"type": "number"}
	i := map[string]any{"type": "integer"}
	return []map[string]any{
		{"name": "activate_character_link", "description": "Activate Muna's single linked embodiment session and return the session id plus stable local character profile.", "inputSchema": o(map[string]any{})},
		{"name": "get_character_context", "description": "Read bounded authoritative local state, current Shell embodiment capabilities, and optional semantic memory context once near the start of a linked turn.", "inputSchema": o(map[string]any{"session_id": s, "semantic_query": s}, "session_id")},
		{"name": "begin_character_reaction", "description": "Mark a new web user turn as received and give Muna immediate local presence. Do not send raw user text.", "inputSchema": o(map[string]any{"session_id": s, "external_turn_id": s, "expression": s, "intensity": n}, "session_id", "external_turn_id")},
		{"name": "request_bridge_reaction", "description": "Ask the local Bridge Secondary Brain for one bounded Traditional-Chinese acknowledgement using only a semantic digest.", "inputSchema": o(map[string]any{"session_id": s, "external_turn_id": s, "semantic_digest": s, "reaction_intent": s}, "session_id", "external_turn_id", "semantic_digest")},
		{"name": "update_character_thinking", "description": "Send one observable thinking milestone only. Never send chain-of-thought or scratchpad.", "inputSchema": o(map[string]any{"session_id": s, "external_turn_id": s, "milestone": map[string]any{"type": "string", "enum": []string{"start", "progress", "difficulty", "resolved"}}, "expression": s, "intensity": n}, "session_id", "external_turn_id", "milestone")},
		{"name": "begin_character_response", "description": "Transition the linked turn to responding immediately before the web answer becomes visible.", "inputSchema": o(map[string]any{"session_id": s, "external_turn_id": s, "expression": s, "response_length": i}, "session_id", "external_turn_id")},
		{"name": "commit_linked_chat", "description": "Commit exactly one completed linked turn through Runtime affect and the ordinary EpisodeCommitV2 memory loop.", "inputSchema": o(map[string]any{"session_id": s, "external_turn_id": s, "status": s, "user_request": s, "web_response": s, "request_summary": s, "response_summary": s, "topic": s, "outcome": s, "reaction_emotion": s, "expression": s}, "session_id", "external_turn_id", "reaction_emotion")},
		{"name": "abort_linked_chat", "description": "Abort an active linked turn without final affect mutation or completed memory.", "inputSchema": o(map[string]any{"session_id": s, "external_turn_id": s, "reason": s}, "session_id", "external_turn_id")},
		{"name": "deactivate_character_link", "description": "Release the linked session and restore ordinary local Primary-Brain mode.", "inputSchema": o(map[string]any{"session_id": s}, "session_id")},
	}
}
func (s *service) callRuntime(ctx context.Context, name string, args map[string]any) (map[string]any, error) {
	route := map[string]string{"activate_character_link": "/linked/session/activate", "get_character_context": "/linked/context", "begin_character_reaction": "/linked/turn/begin", "request_bridge_reaction": "/linked/turn/bridge-reaction", "update_character_thinking": "/linked/turn/thinking", "begin_character_response": "/linked/turn/response", "commit_linked_chat": "/linked/turn/commit", "abort_linked_chat": "/linked/turn/abort", "deactivate_character_link": "/linked/session/deactivate"}[name]
	if route == "" {
		return nil, fmt.Errorf("unknown tool %s", name)
	}
	b, _ := json.Marshal(args)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://127.0.0.1:8770"+route, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&out)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Runtime %s: %v", resp.Status, out)
	}
	return out, nil
}
func (s *service) maybeTunnel() {
	c := s.cfg()
	if !c.AutoTunnel {
		return
	}
	exe := c.Cloudflared
	if !filepath.IsAbs(exe) {
		exe = filepath.Join(s.root, "Plug", exe)
	}
	if _, e := os.Stat(exe); e != nil {
		s.log.Printf("TUNNEL_SKIP error=%v", e)
		return
	}
	cmd := exec.Command(exe, "tunnel", "--url", "http://"+listenAddr, "--no-autoupdate")
	hideProcess(cmd)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return
	}
	cmd.Stdout = cmd.Stderr
	if err = cmd.Start(); err != nil {
		s.log.Printf("TUNNEL_START error=%v", err)
		return
	}
	s.mu.Lock()
	s.tunnel = cmd
	s.mu.Unlock()
	re := regexp.MustCompile(`https://[a-zA-Z0-9-]+\\.trycloudflare\\.com`)
	scan := bufio.NewScanner(stderr)
	for scan.Scan() {
		line := scan.Text()
		s.log.Printf("TUNNEL %s", line)
		if u := re.FindString(line); u != "" {
			s.mu.Lock()
			s.publicURL = u
			s.mu.Unlock()
			_ = os.WriteFile(filepath.Join(s.root, "Plug", "connection_url.txt"), []byte(u+"/mcp?token="+s.secret+"\n"), 0600)
		}
	}
	_ = cmd.Wait()
}
func write(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
func writeRPC(w http.ResponseWriter, v rpcResp) { write(w, 200, v) }
