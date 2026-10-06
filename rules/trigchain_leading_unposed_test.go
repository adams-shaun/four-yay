package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// Swordsman, Sharp Scoundrel (MSH): "attach up to one target Equipment you
// control to target creature you control". With no Equipment the root's Min-0
// slot is settled empty without an ask, so the creature ask is the chain's
// FIRST posed decision. XMage asks the Equipment slot first and would take the
// creature pick for it, so the record must say one slot led.
const swordsmanLeadingScenario = `{"name":"gen1-trigger#0.0","cr":["603.2"],"why":"generated level-B scenario","setup":{"p0":{"battlefield":["Swordsman, Sharp Scoundrel"%s],"hand":["Extremis Elite"]},"p1":{"battlefield":["Grizzly Bears"]}},"steps":[{"op":"cast","seat":0,"card":"p0:Extremis Elite","mana":"CR"},{"op":"resolve","seat":0,"answers":[%s]}]}`

func swordsmanTargetDecisions(t *testing.T, equipment, answers string) []OracleDecision {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	raw := strings.Replace(strings.Replace(swordsmanLeadingScenario, "%s", equipment, 1), "%s", answers, 1)
	res, err := RunOracleScenarioJSON(reg, []byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("scenario failed: %v\n%s", res.Fails, strings.Join(res.Transcript, "\n"))
	}
	var out []OracleDecision
	for _, d := range res.Decisions {
		if d.Kind == "target" {
			out = append(out, d)
		}
	}
	return out
}

func TestTrigChainCountsLeadingUnposedLinks(t *testing.T) {
	// No Equipment: the root slot is unposed, the creature ask is first.
	ds := swordsmanTargetDecisions(t, "", `{"kind":"target","pick":["Swordsman, Sharp Scoundrel (a)"]}`)
	if len(ds) != 1 {
		t.Fatalf("target decisions = %d (%+v), want the lone creature ask", len(ds), ds)
	}
	if ds[0].Resume != "trig_sub" {
		t.Fatalf("precondition: the creature ask is the chain link (Resume %q), not the root", ds[0].Resume)
	}
	if ds[0].LeadingUnposed != 1 {
		t.Fatalf("LeadingUnposed = %d, want 1 (the Equipment slot gorge settled empty)", ds[0].LeadingUnposed)
	}

	// With an Equipment the root slot IS posed: nothing leads the link.
	ds = swordsmanTargetDecisions(t, `,"Bonesplitter"`,
		`{"kind":"target","pick":["Bonesplitter"]},{"kind":"target","pick":["Swordsman, Sharp Scoundrel (a)"]}`)
	if len(ds) != 2 {
		t.Fatalf("target decisions = %d (%+v), want the Equipment ask and the creature ask", len(ds), ds)
	}
	for i, d := range ds {
		if d.LeadingUnposed != 0 {
			t.Fatalf("decision %d (%s) LeadingUnposed = %d, want 0: the root slot was posed", i, d.Resume, d.LeadingUnposed)
		}
	}
}
