package hotfile

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

type Cache struct {
	mu sync.Mutex
	m  map[string]entry
}

type entry struct {
	mod  time.Time
	size int64
	data []byte
}

func New() *Cache { return &Cache{m: map[string]entry{}} }

func (c *Cache) Read(path string) ([]byte, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.m[path]; ok && e.mod.Equal(st.ModTime()) && e.size == st.Size() {
		return append([]byte(nil), e.data...), nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c.m[path] = entry{mod: st.ModTime(), size: st.Size(), data: append([]byte(nil), b...)}
	return b, nil
}

func (c *Cache) Text(path string) (string, error) { b, err := c.Read(path); return string(b), err }

func (c *Cache) JSON(path string, v any) error {
	b, err := c.Read(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}
