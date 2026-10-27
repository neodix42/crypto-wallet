package walletcore

import (
	"bytes"
	"errors"
	"io"
	"localwallet/internal/ipc"
	"localwallet/internal/supervisor"
	"testing"
)

type promptFailedWriter struct{}

func (promptFailedWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestPromptRelayRequiresLiveOriginalRequestAndForwardsOpaqueBody(t *testing.T) {
	var wire bytes.Buffer
	c := &core{child: &supervisor.Child{}, promptPending: true, requestID: "original", requestSession: "public-session-0123456789", requestEpoch: 3, epoch: 3, publicChannel: &ipc.Channel{W: &wire}}
	body := []byte(`{"id":"0123456789abcdef0123456789abcdef","kind":"input","title":"Public fixture","labels":["Password"]}`)
	if e := c.relayPrompt(body); e != nil {
		t.Fatal(e)
	}
	got, e := (&ipc.Channel{R: &wire}).Read()
	if e != nil || got.ID != c.requestID || got.Session != c.requestSession || got.Method != "prompt" || !bytes.Equal(got.Body, body) {
		t.Fatal("prompt lost exact request/body binding")
	}
	for _, mutate := range []func(*core){func(c *core) { c.promptPending = false }, func(c *core) { c.epoch++ }, func(c *core) { c.requestID = "" }, func(c *core) { c.requestSession = "" }, func(c *core) { c.publicChannel = nil }} {
		copy := &core{child: c.child, promptPending: c.promptPending, requestID: c.requestID, requestSession: c.requestSession, requestEpoch: c.requestEpoch, epoch: c.epoch, publicChannel: c.publicChannel}
		mutate(copy)
		if copy.relayPrompt(body) == nil {
			t.Fatal("prompt outside current operation accepted")
		}
	}
	c.publicChannel = &ipc.Channel{W: promptFailedWriter{}}
	if e := c.relayPrompt(body); !errors.Is(e, io.ErrClosedPipe) {
		t.Fatal("prompt pipe failure ignored")
	}
	answer := ipc.Message{Version: 1, ID: c.requestID, Session: c.requestSession, Method: "promptReply", Body: []byte(`{"promptId":"0123456789abcdef0123456789abcdef","accepted":false}`)}
	for _, mutate := range []func(*ipc.Message){func(m *ipc.Message) { m.Version = 2 }, func(m *ipc.Message) { m.ID = "foreign" }, func(m *ipc.Message) { m.Session = "foreign-session" }, func(m *ipc.Message) { m.Method = "unlock" }, func(m *ipc.Message) { m.Error = "PUBLIC-canary" }} {
		copy := answer
		mutate(&copy)
		if c.replyPrompt(copy) == nil {
			t.Fatal("uncorrelated prompt answer accepted")
		}
	}
	if c.replyPrompt(answer) == nil {
		t.Fatal("answer without supervisor capability accepted")
	}
	c.lock()
	if c.replyPrompt(answer) == nil {
		t.Fatal("late answer accepted after lock")
	}
}
