package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
)

func TestPreventSentenceHeadsRegisteredForEveryCorpusCarrier(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	supported := effects.Supported()
	heads := []struct {
		primitive string
		want      int
	}{
		{"kw:Prevent all combat damage that would be dealt to CARDNAME.", 3},
		{"kw:Prevent all combat damage that would be dealt to and dealt by CARDNAME.", 1},
		{"kw:Prevent all damage that would be dealt to CARDNAME.", 5},
	}
	counts := make([]int, len(heads))
	carriers := 0
	for _, c := range reg.AllCards() {
		for _, primitive := range c.Primitives() {
			for i, head := range heads {
				if primitive != head.primitive {
					continue
				}
				counts[i]++
				carriers++
				if !supported[primitive] {
					t.Errorf("%q reports unsupported sentence head %q", c.Faces[0].Name, primitive)
				}
				for _, missing := range reg.Unsupported(c, supported) {
					if missing == primitive {
						t.Errorf("%q still reports %q unsupported", c.Faces[0].Name, primitive)
					}
				}
			}
		}
	}
	for i, head := range heads {
		if counts[i] != head.want {
			t.Errorf("corpus carriers of %q = %d, want %d", head.primitive, counts[i], head.want)
		}
	}
	if carriers != 9 {
		t.Errorf("corpus carriers of Prevent sentence heads = %d, want 9", carriers)
	}
}
