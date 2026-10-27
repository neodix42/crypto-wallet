package supervisor

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"localwallet/internal/uiprompt"
)

func TestCoreActivityPipeFailureRevokesAndReapsSecretBearingSession(t *testing.T) {
	child := &Child{SelfRole: "--internal-core", Environment: "testnet"}
	t.Cleanup(child.Kill)
	prompted := false
	child.OnPrompt = func(raw json.RawMessage) error {
		if _, err := uiprompt.DecodePrompt(raw); err != nil {
			return err
		}
		prompted = true
		// A real inherited-pipe child is blocked on its private prompt. Replace
		// only this test's writer to exercise a broken transport after delivery.
		child.channel.W = brokenPrivatePipe{}
		return child.SendActivity()
	}
	err := child.Call("unlock", struct{}{}, nil)
	if !prompted || err == nil || strings.Contains(err.Error(), "synthetic") || child.cmd != nil || child.channel != nil || child.prompt != nil || child.Generation() == 0 {
		t.Fatal("failed activity write retained a child/capability or exposed transport details", err)
	}
	if child.SendActivity() == nil || child.ReplyPrompt([]byte(`{"promptId":"00000000000000000000000000000001","accepted":true,"values":["PUBLIC-FIXTURE"]}`)) == nil {
		t.Fatal("a reaped activity session retained reply or activity authority")
	}
}

func TestCoreActivityAcknowledgmentBudgetDoesNotDropOrForgePromptAuthorization(t *testing.T) {
	child := &Child{SelfRole: "--internal-core", Environment: "testnet"}
	t.Cleanup(child.Kill)
	child.OnPrompt = func(raw json.RawMessage) error {
		prompt, err := uiprompt.DecodePrompt(raw)
		if err != nil {
			return err
		}
		// The real peer writes each acknowledgment, but this callback holds its
		// reader until replying. Pace actual native events through the production
		// rate limit; do not forge counters, frame results or coverage data.
		for index := 0; index < 8; index++ {
			if index != 0 {
				time.Sleep(time.Second + time.Millisecond)
			}
			if err := child.SendActivity(); err != nil {
				return err
			}
		}
		time.Sleep(time.Second + time.Millisecond)
		if err := child.SendActivity(); err == nil || child.Generation() != 0 {
			t.Error("unconsumed activity frames escaped their resource bound or changed session")
		}
		reply, _ := json.Marshal(uiprompt.Reply{PromptID: prompt.ID, Accepted: true, Values: []string{"PUBLIC-FIXTURE-VALUE"}})
		return child.ReplyPrompt(reply)
	}
	var output struct{ OK bool }
	if err := child.Call("unlock", struct{}{}, &output); err != nil || !output.OK {
		t.Fatal("bounded activity acknowledgments broke the exact valid prompt exchange", err)
	}
	child.life.Lock()
	pending := len(child.activityIDs)
	child.life.Unlock()
	if pending != 0 {
		t.Fatal("completed exact acknowledgments retained pending capabilities")
	}
}

func TestChildRejectsUnreviewedNativeBackendEnvironment(t *testing.T) {
	t.Setenv("GDK_BACKEND", "PUBLIC-INVALID-GRAPHICS-BACKEND")
	child := &Child{SelfRole: "--internal-core", Environment: "testnet"}
	t.Cleanup(child.Kill)
	if err := child.start(); err != nil {
		t.Fatal("owned self-core failed to start", err)
	}
	for _, value := range child.cmd.Env {
		if strings.HasPrefix(value, "GDK_BACKEND=") {
			t.Fatal("unreviewed native backend reached the child environment")
		}
	}
}
