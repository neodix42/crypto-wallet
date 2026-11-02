package webui

import (
	"reflect"
	"strings"
	"testing"
)

func TestErrorNoticeContractIsBoundedAndCopied(t *testing.T) {
	v := publicFixture()
	v.ErrorNotice = &ErrorNotice{ID: "notice-1", Title: "Operation failed", Operation: "Recover deterministic accounts", Code: "ACCOUNT_RECOVERY_STORAGE", Message: "The recovered state could not be committed to encrypted storage.\nCheck permissions and free space."}
	if !validateView(v) {
		t.Fatal("complete bounded error rejected")
	}
	copy := cloneView(v)
	copy.ErrorNotice.Message = "caller changed copy"
	if copy.ErrorNotice == v.ErrorNotice || v.ErrorNotice.Message == copy.ErrorNotice.Message {
		t.Fatal("snapshot shares mutable error state")
	}
	for _, test := range []struct {
		field string
		max   int
		empty bool
	}{{"ID", 64, false}, {"Title", 256, false}, {"Operation", 256, true}, {"Code", 80, false}, {"Message", 8192, false}} {
		for _, value := range []string{strings.Repeat("x", test.max), strings.Repeat("x", test.max+1), "bad\x00", "bad\xff", ""} {
			copy := cloneView(v)
			reflect.ValueOf(copy.ErrorNotice).Elem().FieldByName(test.field).SetString(value)
			expected := value == strings.Repeat("x", test.max) || value == "" && test.empty
			if validateView(copy) != expected {
				t.Fatal("wrong error contract bound", test.field, len(value))
			}
		}
	}
}

func TestErrorAcknowledgementIsExactAndNeverOverridesPrivatePrompt(t *testing.T) {
	v := publicFixture()
	v.ErrorNotice = &ErrorNotice{ID: "first", Title: "Operation failed", Code: "REQUEST_DENIED", Message: "Reviewed failure reason."}
	if !validEvent(v, Event{ID: "ackError", Value: "first"}, false) {
		t.Fatal("current error could not be acknowledged")
	}
	for _, e := range []Event{{ID: "ackError", Value: "stale"}, {ID: "ackError"}, {ID: "open/ethereum"}, {ID: "tab", Value: "wallet/ethereum"}, {ID: "field/amount/ethereum", Value: "1"}} {
		if validEvent(v, e, false) {
			t.Fatal("stale acknowledgement or operation accepted during error", e.ID)
		}
	}
	if validEvent(v, Event{ID: "ackError", Value: "first"}, true) {
		t.Fatal("error acknowledgement replaced a real private prompt")
	}
	for _, id := range []string{"activity", "lock", "close"} {
		if !validEvent(v, Event{ID: id}, false) || !validEvent(v, Event{ID: id}, true) {
			t.Fatal("error dialog disabled lifecycle control", id)
		}
	}
	v.ErrorNotice = nil
	if validEvent(v, Event{ID: "ackError", Value: "first"}, false) {
		t.Fatal("dismissed error could be acknowledged again")
	}
}
