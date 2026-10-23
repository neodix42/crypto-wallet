package strictjson

import (
	"strings"
	"testing"
)

func TestTP041StrictSchemas(t *testing.T) {
	var v struct {
		A string `json:"a"`
		B []struct {
			A int `json:"a"`
		} `json:"b"`
	}
	for _, s := range []string{"", `{"a":"x","a":"y"}`, `{"unknown":1}`, `{"b":[{"a":1,"a":2}]}`, `{} {}`, `{`, strings.Repeat("[", 34) + strings.Repeat("]", 34), `{"a":1}`, `{"b":[`, strings.Repeat("x", (16<<20)+1)} {
		if Decode([]byte(s), &v) == nil {
			t.Fatalf("accepted bad schema %q", s[:min(len(s), 80)])
		}
	}
	if Decode([]byte(`{"a":"x","b":[{"a":1}]}`), &v) != nil {
		t.Fatal("valid schema")
	}
}
func FuzzTP012JSON(f *testing.F) {
	f.Add([]byte(`{"a":"ok"}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		var v struct {
			A string `json:"a"`
		}
		_ = Decode(b, &v)
	})
}
