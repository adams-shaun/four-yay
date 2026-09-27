package decision

import (
	"encoding/json"
	"testing"
)

func TestManaSymbolOptionWireFieldIsOptional(t *testing.T) {
	ordinary, err := json.Marshal(Option{Index: 0, Kind: "pass", Label: "Pass priority"})
	if err != nil {
		t.Fatal(err)
	}
	colour, err := json.Marshal(Option{Index: 1, Kind: "mana", Label: "Add B", ManaSymbol: "B"})
	if err != nil {
		t.Fatal(err)
	}
	var ordinaryFields, colourFields map[string]json.RawMessage
	if err := json.Unmarshal(ordinary, &ordinaryFields); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(colour, &colourFields); err != nil {
		t.Fatal(err)
	}
	if _, exists := ordinaryFields["mana_symbol"]; exists {
		t.Fatalf("ordinary option unexpectedly serializes mana_symbol: %s", ordinary)
	}
	var symbol string
	if err := json.Unmarshal(colourFields["mana_symbol"], &symbol); err != nil {
		t.Fatal(err)
	}
	if symbol != "B" {
		t.Fatalf("wire mana_symbol = %q, want B (%s)", symbol, colour)
	}
}
