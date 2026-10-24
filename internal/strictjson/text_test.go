package strictjson

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBoundedTextRejectsNullTypeAndIdentifierInjection(t *testing.T) {
	var text Text
	for _, raw := range [][]byte{nil, []byte(`null`), []byte(`1`), []byte(`true`), []byte(`[]`), []byte(`{}`), []byte(`"unterminated`), []byte(`"\u0000"`), []byte(`"` + strings.Repeat("x", 4097) + `"`), {'"', 255, '"'}} {
		text = "unchanged"
		if text.UnmarshalJSON(raw) == nil || text != "unchanged" {
			t.Fatal("invalid text accepted or partially mutated")
		}
	}
	for _, value := range []string{"", strings.Repeat("x", 4096), "public-identifier-é"} {
		raw, _ := json.Marshal(value)
		if text.UnmarshalJSON(raw) != nil || string(text) != value {
			t.Fatal("valid bounded text rejected")
		}
	}
	var body struct {
		Cursor Text `json:"cursor"`
	}
	if Decode([]byte(`{}`), &body) != nil || body.Cursor != "" {
		t.Fatal("optional field changed")
	}
	if Decode([]byte(`{"cursor":null}`), &body) == nil {
		t.Fatal("null collapsed to omitted field")
	}
}
