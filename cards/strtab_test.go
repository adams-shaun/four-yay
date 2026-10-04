package cards

import "testing"

func TestStrTable(t *testing.T) {
	tb := NewStrTable(StrEntry[int]{"b", 2}, StrEntry[int]{"a", 1}, StrEntry[int]{"b", 9}, StrEntry[int]{"", 7})
	for k, want := range map[string]int{"a": 1, "b": 2, "": 7} {
		if v, ok := tb.Get(k); !ok || v != want {
			t.Fatalf("Get(%q) = %d,%v", k, v, ok)
		}
	}
	if _, ok := tb.Get("c"); ok {
		t.Fatal("c present")
	}
	s := NewNameSet("x", "y", "x")
	if !s.Has("x") || !s.Has("y") || s.Has("z") {
		t.Fatal("NameSet")
	}
}

func TestStrCodesHashedIndex(t *testing.T) {
	var ents []StrEntry[uint16]
	keys := []string{"", "A", "B", "AB", "BA", "Self", "Parent", "TriggeredSource", "TriggeredSources",
		"TriggeredSourceLKICopy", "Player.Opponent", "Player.Other", "ChosenCard", "ChosenPlayer"}
	for i := 0; i < 300; i++ {
		keys = append(keys, "Key"+string(rune('a'+i%26))+string(rune('A'+i/26)))
	}
	for i, k := range keys {
		ents = append(ents, StrEntry[uint16]{Key: k, Val: uint16(i + 1)})
	}
	c := NewStrCodes(ents...)
	for i, k := range keys {
		if got := c.Code(k); got != uint16(i+1) {
			t.Fatalf("Code(%q) = %d, want %d", k, got, i+1)
		}
	}
	for _, k := range []string{"C", "Selff", "Key", "KeyzZ", "Player.", "TriggeredSourceX"} {
		if got := c.Code(k); got != 0 {
			t.Fatalf("Code(%q) = %d, want 0", k, got)
		}
	}
	var empty StrCodes[uint16]
	if empty.Code("x") != 0 {
		t.Fatal("zero StrCodes")
	}
	var es NameSet
	if es.Has("") {
		t.Fatal("zero NameSet")
	}
}
