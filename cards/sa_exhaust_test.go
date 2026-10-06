package cards

import "testing"

func TestSAExhaustValueCompiled(t *testing.T) {
	reg := NewRegistry()
	card, diags := ParseBytes("exhaust.txt", []byte("Name:Exhaust\nTypes:Creature\nA:AB$ Pump | Cost$ T | Exhaust$ true\nA:AB$ Pump | Cost$ T | Exhaust$ False\nOracle:x\n"))
	if len(diags) != 0 {
		t.Fatalf("parse diagnostics: %+v", diags)
	}
	reg.Add(card)
	card, ok := reg.Lookup("Exhaust")
	if !ok || len(card.Faces) != 1 || len(card.Faces[0].Abilities) != 2 {
		t.Fatalf("precondition: ability fixtures did not compile: %+v", card)
	}
	abilities := card.Faces[0].Abilities
	if !abilities[0].exhaustBound || !abilities[0].exhaust {
		t.Fatalf("precondition: parsed Exhaust$ true was not compiled: bound=%v value=%v", abilities[0].exhaustBound, abilities[0].exhaust)
	}
	if abilities[1].exhaust || !abilities[1].exhaustBound {
		t.Fatalf("precondition: parsed Exhaust$ false compiled incorrectly: bound=%v value=%v", abilities[1].exhaustBound, abilities[1].exhaust)
	}
	if !abilities[0].ExhaustValue() {
		t.Fatal("compiled Exhaust$ true ability qualifier = false")
	}
	if abilities[1].ExhaustValue() {
		t.Fatal("compiled Exhaust$ False ability qualifier = true")
	}
	if !(&SA{Params: map[string]string{"Exhaust": "True"}}).ExhaustValue() {
		t.Fatal("unbound synthetic Exhaust$ True was not recognized")
	}
	// A write after binding must not be shadowed by the stale compiled fact.
	if abilities[1].ExhaustValue() {
		t.Fatal("precondition: parsed Exhaust$ False should read false before SetParam")
	}
	abilities[1].SetParam(PKExhaust, "True")
	if !abilities[1].ExhaustValue() {
		t.Fatal("SetParam(PKExhaust, True) after binding was shadowed by the compiled fact")
	}
}
