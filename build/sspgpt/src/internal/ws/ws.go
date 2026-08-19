package ws

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
)

const magic = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

type Conn struct {
	c  net.Conn
	r  *bufio.Reader
	mu sync.Mutex
}

func Upgrade(w http.ResponseWriter, r *http.Request) (*Conn, error) {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return nil, errors.New("not websocket")
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		return nil, errors.New("missing key")
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		return nil, errors.New("no hijacker")
	}
	c, rw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}
	h := sha1.Sum([]byte(key + magic))
	accept := base64.StdEncoding.EncodeToString(h[:])
	fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", accept)
	if err := rw.Flush(); err != nil {
		c.Close()
		return nil, err
	}
	return &Conn{c: c, r: rw.Reader}, nil
}

func (c *Conn) Close() error { return c.c.Close() }

func (c *Conn) ReadText() (string, error) {
	for {
		h, err := c.r.ReadByte()
		if err != nil {
			return "", err
		}
		h2, err := c.r.ReadByte()
		if err != nil {
			return "", err
		}
		op := h & 0x0f
		masked := h2&0x80 != 0
		n := int64(h2 & 0x7f)
		if n == 126 {
			var x uint16
			if err = binary.Read(c.r, binary.BigEndian, &x); err != nil {
				return "", err
			}
			n = int64(x)
		}
		if n == 127 {
			var x uint64
			if err = binary.Read(c.r, binary.BigEndian, &x); err != nil {
				return "", err
			}
			if x > 4<<20 {
				return "", errors.New("frame too large")
			}
			n = int64(x)
		}
		var mask [4]byte
		if masked {
			if _, err = io.ReadFull(c.r, mask[:]); err != nil {
				return "", err
			}
		}
		p := make([]byte, n)
		if _, err = io.ReadFull(c.r, p); err != nil {
			return "", err
		}
		if masked {
			for i := range p {
				p[i] ^= mask[i%4]
			}
		}
		switch op {
		case 0x1:
			return string(p), nil
		case 0x8:
			return "", io.EOF
		case 0x9:
			_ = c.writeFrame(0xA, p)
		}
	}
}

func (c *Conn) WriteText(s string) error { return c.writeFrame(0x1, []byte(s)) }
func (c *Conn) writeFrame(op byte, p []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	var h []byte
	n := len(p)
	if n < 126 {
		h = []byte{0x80 | op, byte(n)}
	} else if n <= 65535 {
		h = make([]byte, 4)
		h[0] = 0x80 | op
		h[1] = 126
		binary.BigEndian.PutUint16(h[2:], uint16(n))
	} else {
		h = make([]byte, 10)
		h[0] = 0x80 | op
		h[1] = 127
		binary.BigEndian.PutUint64(h[2:], uint64(n))
	}
	if _, err := c.c.Write(h); err != nil {
		return err
	}
	_, err := c.c.Write(p)
	return err
}
