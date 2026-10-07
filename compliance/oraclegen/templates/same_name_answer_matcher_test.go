package templates

import "testing"

// The census's own matcher must reject an answer that resolves to exactly
// one object when that object is not the pick, and must not let a sibling
// alias go uncounted.
func TestSameNameAnswerMatcherRejectsSibling(t *testing.T) {
	options := []string{"p0:Wastes#27", "p0:Wastes#39"}
	// Precondition: the two candidates share a name, so the pick is ambiguous.
	if refName(options[0]) != refName(options[1]) {
		t.Fatalf("fixture candidates do not share a name: %q vs %q", options[0], options[1])
	}
	cases := []struct {
		values []string
		pick   string
		seg    string
		bucket string
	}{
		{[]string{"@p0:Wastes#27^@p0:Wastes#39"}, "p0:Wastes#39", "@p0:Wastes#39", "alias"},
		{[]string{"@p0:Wastes#27", "@p0:Wastes#39"}, "p0:Wastes#39", "@p0:Wastes#39", "alias"},
		// The step answered the sibling only: counted, and unresolved.
		{[]string{"@p0:Wastes#27"}, "p0:Wastes#39", "@p0:Wastes#27", "unresolved"},
		// A bare name matches both: unresolved.
		{[]string{"Wastes"}, "p0:Wastes#39", "Wastes", "unresolved"},
		// A number answer names nothing.
		{[]string{"2"}, "p0:Wastes#39", "", ""},
	}
	for _, c := range cases {
		seg := segmentFor(c.values, c.pick)
		if seg != c.seg {
			t.Errorf("segmentFor(%q, %q) = %q, want %q", c.values, c.pick, seg, c.seg)
			continue
		}
		if seg == "" {
			continue
		}
		if bucket, _ := classifyAnswer(seg, c.pick, options); bucket != c.bucket {
			t.Errorf("classifyAnswer(%q, %q) = %q, want %q", seg, c.pick, bucket, c.bucket)
		}
	}
}
