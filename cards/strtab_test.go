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
