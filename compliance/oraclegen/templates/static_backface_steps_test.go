package templates

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestStaticBackFaceScenarioStepsIsArray pins that a step-less back-face
// static scenario serialises "steps" as [] rather than null: the XMage
// driver casts "steps" to a JSON array and a null crashed the whole set's
// replay (Clay-Fired Bricks in LCI, Aang and The Legend of Kyoshi in TLA).
func TestStaticBackFaceScenarioStepsIsArray(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup("Clay-Fired Bricks")
	if !ok || len(c.Faces) < 2 {
		t.Skip("Clay-Fired Bricks not in the corpus")
	}
	it := staticBackFaceScenario(c.Faces[1], "Clay-Fired Bricks", levelb.Requirement{Face: 1}, nil, nil)
	b, err := json.Marshal(it)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"steps":null`) {
		t.Fatalf("step-less scenario serialised steps as null: %s", b)
	}
}
