package ipc

import (
	"bytes"
	"encoding/binary"
	"io"
	"strings"
	"testing"
)

func good() Message {
	return Message{Version: 1, ID: "1", Session: strings.Repeat("a", 32), Method: "accounts", Body: []byte(`{}`)}
}
func TestTP041FramesAndReplay(t *testing.T) {
	var b bytes.Buffer
	c := Channel{R: &b, W: &b}
	m := good()
	if e := c.Write(m); e != nil {
		t.Fatal(e)
	}
	got, e := c.Read()
	if e != nil || got.ID != m.ID {
		t.Fatal(e)
	}
	if c.Accept(got) != nil || c.Accept(got) == nil {
		t.Fatal("replay")
	}
	got.ID = "2"
	got.Session = strings.Repeat("b", 32)
	if c.Accept(got) == nil {
		t.Fatal("peer binding")
	}
	for _, n := range []uint32{0, MaxFrame + 1, 100} {
		var h [4]byte
		binary.BigEndian.PutUint32(h[:], n)
		b.Write(h[:])
		if _, e := c.Read(); e == nil {
			t.Fatal("length/truncation")
		}
		b.Reset()
	}
	for _, s := range []string{`{}`, `{"version":2,"id":"1","session":"aaaaaaaaaaaaaaaa","method":"accounts","body":{}}`, `{"version":1,"id":"1","session":"short","method":"accounts","body":{}}`} {
		var h [4]byte
		binary.BigEndian.PutUint32(h[:], uint32(len(s)))
		b.Write(h[:])
		b.WriteString(s)
		if _, e := c.Read(); e == nil {
			t.Fatal("schema")
		}
	}
	if e := c.Write(Message{Body: []byte("bad")}); e == nil {
		t.Fatal("invalid JSON")
	}
	if e := c.Write(Message{Body: []byte(`"` + strings.Repeat("a", MaxFrame) + `"`)}); e == nil {
		t.Fatal("max output")
	}
	if _, e := c.Read(); e != io.EOF {
		t.Fatal("EOF")
	}
}

type zeroWriter struct{}

func (zeroWriter) Write([]byte) (int, error) { return 0, nil }
func TestTP041ShortWrite(t *testing.T) {
	if writeAll(zeroWriter{}, []byte{1}) != io.ErrShortWrite {
		t.Fatal("short write")
	}
	c := Channel{W: zeroWriter{}}
	if c.Write(good()) == nil {
		t.Fatal("frame short write")
	}
}
func FuzzTP012IPC(f *testing.F) {
	f.Add([]byte{0, 0, 0, 2, '{', '}'})
	f.Fuzz(func(t *testing.T, b []byte) { c := Channel{R: bytes.NewReader(b)}; _, _ = c.Read() })
}
