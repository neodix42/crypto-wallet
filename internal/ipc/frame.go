// Package ipc provides bounded, strictly versioned inherited-pipe messages.
package ipc

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"localwallet/internal/strictjson"
	"sync"
)

const MaxFrame = 1 << 20

var ErrFrame = errors.New("invalid IPC frame")

type Message struct {
	Version uint8           `json:"version"`
	ID      string          `json:"id"`
	Session string          `json:"session"`
	Method  string          `json:"method"`
	Body    json.RawMessage `json:"body"`
	Error   string          `json:"error,omitempty"`
}
type Channel struct {
	R       io.Reader
	W       io.Writer
	mu      sync.Mutex
	seen    map[string]bool
	session string
}

func (c *Channel) Read() (Message, error) {
	var m Message
	var h [4]byte
	if _, e := io.ReadFull(c.R, h[:]); e != nil {
		return m, e
	}
	n := binary.BigEndian.Uint32(h[:])
	if n == 0 || n > MaxFrame {
		return m, ErrFrame
	}
	b := make([]byte, n)
	if _, e := io.ReadFull(c.R, b); e != nil {
		return m, ErrFrame
	}
	if strictjson.Decode(b, &m) != nil || m.Version != 1 || len(m.ID) == 0 || len(m.ID) > 64 || len(m.Session) < 16 || len(m.Session) > 64 || len(m.Method) == 0 || len(m.Method) > 64 {
		return Message{}, ErrFrame
	}
	return m, nil
}

// Accept is used on the request side only. Responses may share their request ID.
func (c *Channel) Accept(m Message) error {
	if c.seen == nil {
		c.seen = map[string]bool{}
		c.session = m.Session
	}
	if m.Session != c.session || c.seen[m.ID] || len(c.seen) >= 100000 {
		return ErrFrame
	}
	c.seen[m.ID] = true
	return nil
}
func (c *Channel) Write(m Message) error {
	b, e := json.Marshal(m)
	if e != nil || len(b) > MaxFrame || len(b) == 0 {
		return ErrFrame
	}
	var h [4]byte
	binary.BigEndian.PutUint32(h[:], uint32(len(b)))
	c.mu.Lock()
	defer c.mu.Unlock()
	if e = writeAll(c.W, h[:]); e != nil {
		return e
	}
	return writeAll(c.W, b)
}
func writeAll(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, e := w.Write(b)
		if e != nil {
			return e
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return nil
}
