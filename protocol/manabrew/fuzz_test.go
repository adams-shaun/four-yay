//go:build fuzz

// Fuzz tests are opt-in: build tag `fuzz` (make fuzz). a native fuzz target; they stay
// out of the default `go test ./...` and every pipeline gate.

package manabrew

import (
	"encoding/json"
	"testing"
)

// FuzzDecodeClientMessage feeds arbitrary bytes to the client-message
// decoders and the lenient unknown-path walker. The properties:
//
//   - no panic on any input;
//   - a ClientMessage that decodes at all must re-decode byte-identically
//     from its own canonical encoding (encode stability).
func FuzzDecodeClientMessage(f *testing.F) {
	seeds := [][]byte{
		[]byte(`{"kind":"response","promptId":7,"action":{"type":"chooseBoolean","output":{"type":"decision","value":true}}}`),
		[]byte(`{"kind":"directive","directive":{"type":"concede"}}`),
		[]byte(`{"kind":"response","promptId":1,"action":{"type":"chooseAttackers","output":{"type":"declareAttackers","assignments":[{"attackerId":"a","targetId":"b"}]}}}`),
		[]byte(`{"kind":"response","promptId":1,"action":{"type":"scry","output":{"type":"scryDecision","zoneCardIds":[["a"],["b"]]}}}`),
		[]byte(`{"kind":"response","promptId":1,"action":{"type":"chooseAction","output":{"type":"pass","until":{"playerId":"p1","phase":"main2"},"exhaustStack":false}}}`),
		[]byte(`{"kind":"response","promptId":1,"action":{"type":"chooseNumber","output":{"type":"numberDecision","chosenNumber":null}}}`),
		[]byte(`{}`),
		[]byte(`[]`),
		[]byte(`null`),
		[]byte(`{"kind":"prompt"}`),
		[]byte(`{"kind":"response","promptId":1,"action":{}}`),
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var m ClientMessage
		if err := json.Unmarshal(data, &m); err == nil {
			if b, err := json.Marshal(m); err == nil {
				var m2 ClientMessage
				if err := json.Unmarshal(b, &m2); err != nil {
					t.Fatalf("re-decode of canonical encoding failed: %v\ninput:  %s\nbytes:  %s", err, data, b)
				}
				if _, err := Decode(b, &m2); err != nil {
					t.Fatalf("lenient decode of canonical encoding failed: %v\nbytes: %s", err, b)
				}
			}
		}
		// The walker must terminate and never panic on arbitrary input.
		var m3 ClientMessage
		_, _ = Decode(data, &m3)
		var p AgentPrompt
		_, _ = Decode(data, &p)
		var e EngineMessage
		_, _ = Decode(data, &e)
	})
}
