package strictjson

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// Text is a bounded identifier/input string. Unlike encoding/json's treatment
// of a plain Go string, an explicitly present JSON null is not an empty value.
// An omitted optional field still has its zero value.
type Text string

func (text *Text) UnmarshalJSON(raw []byte) error {
	if len(raw) == 0 || raw[0] != '"' || !utf8.Valid(raw) {
		return ErrInvalid
	}
	var value string
	if json.Unmarshal(raw, &value) != nil || len(value) > 4096 || strings.ContainsRune(value, 0) {
		return ErrInvalid
	}
	*text = Text(value)
	return nil
}
