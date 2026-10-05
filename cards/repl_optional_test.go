package cards

import "testing"

func TestReplOptionalValueCompiled(t *testing.T) {
	reg := NewRegistry()
	card, diags := ParseBytes("optional.txt", []byte("Name:Optional\nTypes:Enchantment\nR:Event$ Draw | Optional$ true\nOracle:x\n"))
	if len(diags) != 0 {
		t.Fatalf("parse diagnostics: %+v", diags)
	}
	reg.Add(card)
	card, ok := reg.Lookup("Optional")
	if !ok || len(card.Faces) != 1 || len(card.Faces[0].Repls) != 1 {
		t.Fatalf("precondition: replacement fixture did not compile: %+v", card)
	}
	if !card.Faces[0].Repls[0].OptionalValue() {
		t.Fatal("compiled Optional$ true replacement flag = false")
	}
	plain := Repl{Params: map[string]string{"Optional": "True"}}
	if !plain.OptionalValue() {
		t.Fatal("unbound synthetic Optional$ True was not recognized")
	}
	no := Repl{Params: map[string]string{"Optional": "False"}}
	if no.OptionalValue() {
		t.Fatal("Optional$ False was recognized as true")
	}
}
