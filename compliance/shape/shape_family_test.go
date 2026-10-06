package shape

import "testing"

// TestOfKeysByTemplateFamily: every slot of one level-B family keys the same
// shape, so a ruling written for activate#0.1 classifies activate#0.3 too.
func TestOfKeysByTemplateFamily(t *testing.T) {
	a := diverge("Foo", "activate#0.1", "step 1 (resolve)", "p1.life", "18", "16")
	b := diverge("Foo", "activate#0.3", "step 1 (resolve)", "p1.life", "18", "16")
	sa, ok := Of(a)
	if !ok {
		t.Fatal("activate#0.1 has no shape")
	}
	sb, ok := Of(b)
	if !ok {
		t.Fatal("activate#0.3 has no shape")
	}
	if sa.Template != "activate" || sb.Template != "activate" {
		t.Fatalf("templates %q and %q, want the family activate", sa.Template, sb.Template)
	}
	if sa.Key() != sb.Key() {
		t.Errorf("family keys differ:\n %s\n %s", sa.Key(), sb.Key())
	}
}

// TestLevelAShapeIDUnchanged: a level-A template string has no '#', so the
// family is the string itself and every committed shape id and triage file
// name stays byte-identical. Pinned from the row used in shape_test.go.
func TestLevelAShapeIDUnchanged(t *testing.T) {
	s, ok := Of(diverge("Erode", "cast-resolve", "step 1 (resolve)", "p1.library_count", "39", "38"))
	if !ok {
		t.Fatal("no shape")
	}
	if s.Template != "cast-resolve" {
		t.Errorf("Template = %q, want the whole level-A string", s.Template)
	}
	const want = "cast-resolve.resolve.p1-library.1dc7b323"
	if got := s.Slug(); got != want {
		t.Errorf("level-A shape id changed:\n got %s\nwant %s", got, want)
	}
}
