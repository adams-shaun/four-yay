package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// zenosChosenCardStrictScenario is the Zenos yae Galvus (FIN) defect, hand-
// authored: the ETB "choose a creature an opponent controls" is answered, the
// chosen bear is bounced by Unsummon, and the leave-the-battlefield trigger
// ("When the chosen creature leaves the battlefield, transform Zenos yae
// Galvus") -- whose ValidCard$ is Card.ChosenCardStrict evaluated against a
// trigger-matcher SpecContext that binds no chosen set -- must fire and put
// the transformed Shinryu, Transcendent Rival on the battlefield. Before the
// event-backed fallback in effects/filter.go, the predicate failed closed on
// the unbound ChosenValid and the trigger never fired.
const zenosChosenCardStrictScenario = `{"name":"zenos-chosen-ltb","setup":{"p0":{"hand":["Zenos yae Galvus","Unsummon"]},"p1":{"battlefield":["Grizzly Bears"]}},"steps":[{"op":"cast","seat":0,"card":"p0:Zenos yae Galvus","mana":"BBBBB"},{"op":"resolve","seat":0,"answers":[{"kind":"choose","pick":["p1:Grizzly Bears"]}]},{"op":"cast","seat":0,"card":"p0:Unsummon","mana":"U","targets":["p1:Grizzly Bears"]},{"op":"resolve","seat":0,"answers":[{"kind":"choose","pick":["p1"]}]}]}`

func TestZenosChosenCardStrictTransformTrigger(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	res, err := RunOracleScenarioJSON(reg, []byte(zenosChosenCardStrictScenario))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("scenario failed: %v\n%s", res.Fails, strings.Join(res.Transcript, "\n"))
	}
	final := res.Snapshots[len(res.Snapshots)-1]

	// Precondition: the setup bear really was on the battlefield for the
	// bounce to remove it through.
	if _, ok := snapPerm(res.Snapshots[0], "p1:Grizzly Bears"); !ok {
		t.Fatalf("setup: p1:Grizzly Bears is not on the battlefield")
	}
	// Precondition: the two faces under comparison actually differ. The
	// transformed permanent keeps its original Ref; the back face shows in
	// the projected Name.
	zenos, ok := snapPerm(final, "p0:Zenos yae Galvus")
	if !ok {
		for _, p := range final.Permanents {
			t.Logf("final permanents %+v", p)
		}
		t.Fatal("the Zenos permanent vanished without transforming")
	}
	if zenos.Name != "Shinryu, Transcendent Rival" {
		t.Fatalf("the transformed permanent reports %q, want the back face", zenos.Name)
	}
	for _, p := range final.Permanents {
		if p.Name == "Zenos yae Galvus" {
			t.Fatalf("%s still reports the front face; it never transformed", p.Ref)
		}
	}
	if _, ok := snapPerm(final, "p1:Grizzly Bears"); ok {
		t.Fatal("p1:Grizzly Bears was not bounced by Unsummon")
	}
}
