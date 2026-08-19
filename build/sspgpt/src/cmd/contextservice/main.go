package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"sspgpt/v07/internal/paths"
	"sspgpt/v07/internal/singleinstance"
)

const version = "0.7.1-fix15-mcp"
const listenAddr = "127.0.0.1:8782"
const runtimeBase = "http://127.0.0.1:8770"
const maxInvokeBody = 512 << 10

type invokeRequest struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type service struct {
	root         string
	log          *log.Logger
	secret       string
	client       *http.Client
	mu           sync.Mutex
	activeSession string
	activeTurn    string
	lastActivity  time.Time
	shutdownOnce sync.Once
}

var runtimeRoutes = map[string]string{
	"activate_character_link":       "/linked/session/activate",
	"get_character_context":         "/linked/context",
	"begin_character_reaction":      "/linked/turn/begin",
	"request_bridge_reaction":       "/linked/turn/bridge-reaction",
	"update_character_thinking":     "/linked/turn/thinking",
	"begin_character_response":      "/linked/turn/response",
	"commit_linked_chat":            "/linked/turn/commit",
	"abort_linked_chat":             "/linked/turn/abort",
	"deactivate_character_link":     "/linked/session/deactivate",
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
	s := &service{
		root: root,
		log: log.New(lf, "", log.LstdFlags|log.Lmicroseconds),
		client: &http.Client{Timeout: 30 * time.Second},
		lastActivity: time.Now(),
	}
	s.secret = s.loadInternalSecret()
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.health)
	mux.HandleFunc("/status", s.status)
	mux.HandleFunc("/invoke", s.invoke)
	mux.HandleFunc("/shutdown", s.shutdown)
	go s.watchdog()
	s.log.Printf("ContextService %s listening=%s boundary=private_runtime_adapter", version, listenAddr)
	if err := http.ListenAndServe(listenAddr, mux); err != nil {
		s.log.Printf("server stopped: %v", err)
	}
}

func (s *service) loadInternalSecret() string {
	p := filepath.Join(s.root, "Plug", "internal.secret")
	if b, err := os.ReadFile(p); err == nil && strings.TrimSpace(string(b)) != "" {
		return strings.TrimSpace(string(b))
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	v := hex.EncodeToString(b)
	if err := os.WriteFile(p, []byte(v), 0600); err != nil {
		panic(err)
	}
	return v
}

func (s *service) authorized(r *http.Request) bool {
	got := strings.TrimSpace(r.Header.Get("X-SSPGPT-Internal-Secret"))
	if got == "" || len(got) != len(s.secret) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.secret)) == 1
}

func (s *service) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]any{"ok": true, "service": "CharacterGPTContextService", "version": version})
}

func (s *service) status(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	sid, turn := s.activeSession, s.activeTurn
	s.mu.Unlock()
	writeJSON(w, 200, map[string]any{
		"ok": true,
		"version": version,
		"boundary": "private_runtime_adapter",
		"active_session": sid != "",
		"active_turn": turn != "",
	})
}

func (s *service) invoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST required"})
		return
	}
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var in invokeRequest
	dec := json.NewDecoder(io.LimitReader(r.Body, maxInvokeBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	route, ok := runtimeRoutes[in.Name]
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown linked tool"})
		return
	}
	if len(in.Arguments) == 0 {
		in.Arguments = json.RawMessage(`{}`)
	}
	out, status, err := s.callRuntime(r.Context(), route, in.Arguments)
	if err != nil {
		writeJSON(w, status, map[string]any{"ok": false, "error": err.Error(), "runtime": out})
		return
	}
	s.observeSuccess(in.Name, in.Arguments, out)
	writeJSON(w, 200, out)
}

func (s *service) callRuntime(ctx context.Context, route string, args json.RawMessage) (map[string]any, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, runtimeBase+route, bytes.NewReader(args))
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, http.StatusBadGateway, err
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&out); err != nil {
		return nil, http.StatusBadGateway, fmt.Errorf("Runtime response decode: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return out, resp.StatusCode, fmt.Errorf("Runtime %s", resp.Status)
	}
	return out, 200, nil
}

func (s *service) observeSuccess(name string, args json.RawMessage, out map[string]any) {
	var a struct {
		SessionID      string `json:"session_id"`
		ExternalTurnID string `json:"external_turn_id"`
	}
	_ = json.Unmarshal(args, &a)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastActivity = time.Now()
	switch name {
	case "activate_character_link":
		if v, _ := out["session_id"].(string); v != "" {
			s.activeSession = v
			s.activeTurn = ""
		}
	case "begin_character_reaction":
		s.activeTurn = a.ExternalTurnID
	case "commit_linked_chat", "abort_linked_chat":
		s.activeTurn = ""
	case "deactivate_character_link":
		s.activeSession = ""
		s.activeTurn = ""
	}
}

func (s *service) watchdog() {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for range t.C {
		s.mu.Lock()
		sid, turn, last := s.activeSession, s.activeTurn, s.lastActivity
		s.mu.Unlock()
		if sid == "" {
			continue
		}
		idle := time.Since(last)
		if turn != "" && idle > 5*time.Minute {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			b, _ := json.Marshal(map[string]any{"session_id": sid, "external_turn_id": turn, "reason": "contextservice_watchdog"})
			_, _, _ = s.callRuntime(ctx, "/linked/turn/abort", b)
			cancel()
			s.mu.Lock()
			s.activeTurn = ""
			s.lastActivity = time.Now()
			s.mu.Unlock()
			s.log.Printf("WATCHDOG_ABORT session=%s turn=%s", sid, turn)
		}
		if idle > 10*time.Minute {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			b, _ := json.Marshal(map[string]any{"session_id": sid})
			_, _, _ = s.callRuntime(ctx, "/linked/session/deactivate", b)
			cancel()
			s.mu.Lock()
			s.activeSession, s.activeTurn = "", ""
			s.mu.Unlock()
			s.log.Printf("WATCHDOG_DEACTIVATE session=%s", sid)
		}
	}
}

func (s *service) shutdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST required"})
		return
	}
	started := false
	s.shutdownOnce.Do(func() {
		started = true
		s.mu.Lock()
		sid := s.activeSession
		s.mu.Unlock()
		if sid != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			b, _ := json.Marshal(map[string]any{"session_id": sid})
			_, _, _ = s.callRuntime(ctx, "/linked/session/deactivate", b)
			cancel()
		}
		s.log.Printf("SHUTDOWN_BEGIN linked_session_released=%t", sid != "")
		go func() {
			time.Sleep(120 * time.Millisecond)
			s.log.Printf("SHUTDOWN_COMPLETE")
			os.Exit(0)
		}()
	})
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "started": started, "service": "CharacterGPTContextService", "version": version})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
