package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestOracleBackFaceStackSourceNamesCurrentFace pins that a back-face setup
// permanent's ability on the stack is named by the face it is on (Dire
// Blunderbuss), not by the front-face setup ref (Dire Flail), while a
// front-face permanent keeps its own name.
func TestOracleBackFaceStackSourceNamesCurrentFace(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup("Dire Flail")
	if !ok || len(c.Faces) < 2 || c.Faces[0].Name == c.Faces[1].Name {
		t.Fatalf("Dire Flail is not a two-named-face card in the corpus")
	}
	front, back := c.Faces[0].Name, c.Faces[1].Name

	run := func(backFace bool) []OracleSnapStack {
		sc := `{"name":"backface-stack","setup":{"p0":{"battlefield":["Dire Flail","Grizzly Bears"]` +
			map[bool]string{true: `,"back_face":["Dire Flail"]`, false: ``}[backFace] +
			`},"p1":{}},"steps":[{"op":"activate","seat":0,"card":"p0:Dire Flail","mana":"R","targets":["p0:Grizzly Bears"]}]}`
		res, err := RunOracleScenarioJSON(reg, []byte(sc))
		if err != nil {
			t.Fatalf("run (backFace=%v): %v", backFace, err)
		}
		var withStack []OracleSnapStack
		for _, s := range res.Snapshots {
			if len(s.Stack) > 0 {
				withStack = s.Stack
				break
			}
		}
		if len(withStack) == 0 {
			t.Fatalf("backFace=%v: no snapshot has a stack item; the activation never reached the stack", backFace)
		}
		return withStack
	}

	got := run(true)
	if got[0].Kind != "ability" || !strings.HasSuffix(got[0].Source, ":"+back) {
		t.Errorf("back-face stack source = %q (%s), want an ability named %q", got[0].Source, got[0].Kind, back)
	}
	got = run(false)
	if got[0].Kind != "ability" || !strings.HasSuffix(got[0].Source, ":"+front) {
		t.Errorf("front-face stack source = %q, want %q", got[0].Source, front)
	}
}
