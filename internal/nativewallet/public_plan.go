package nativewallet

import (
	"bytes"
	"encoding/json"
	"errors"
	"unicode/utf8"

	"localwallet/internal/model"
	"localwallet/internal/strictjson"
)

var errPublicPlan = errors.New("invalid public transaction payload")

// The coordinator's public wire encodes protocol payload bytes as JSON strings.
// Decode only known Go transaction DTOs; renderer data and private signer/model
// decoding retain their existing policies. Never re-marshal payload objects:
// their exact integer values and original digest-bound key order must survive.
func publicPlanPayload(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return raw, nil
	}
	if len(raw) > 1<<20 || !utf8.Valid(raw) {
		return nil, errPublicPlan
	}
	value := raw
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var text string
		if json.Unmarshal(raw, &text) != nil {
			return nil, errPublicPlan
		}
		value = json.RawMessage(text)
		trimmed = bytes.TrimSpace(value)
	}
	if len(value) > 1<<20 || !utf8.Valid(value) || len(trimmed) < 2 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' {
		return nil, errPublicPlan
	}
	var object map[string]json.RawMessage
	if strictjson.Decode(value, &object) != nil || object == nil {
		return nil, errPublicPlan
	}
	return value, nil
}

func normalizePublicPlans(out any) error {
	switch value := out.(type) {
	case *model.Plan:
		if value == nil {
			return nil
		}
		payload, err := publicPlanPayload(value.Payload)
		if err != nil {
			return err
		}
		value.Payload = payload
	case *journalPage:
		if value == nil {
			return nil
		}
		// Reject the whole page without partially changing earlier operations.
		payloads := make([]json.RawMessage, len(value.Operations))
		for i, operation := range value.Operations {
			payload, err := publicPlanPayload(operation.Plan.Payload)
			if err != nil {
				return err
			}
			payloads[i] = payload
		}
		for i := range value.Operations {
			value.Operations[i].Plan.Payload = payloads[i]
		}
	case *pendingPaymentResponse:
		if value != nil && value.Operation != nil {
			return normalizePublicPlans(&value.Operation.Plan)
		}
	}
	return nil
}
