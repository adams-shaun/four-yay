package rules

// exiledwithsource_ability_test.go pins the A:-line spelling of exile
// provenance: Mimeoplasm, Revered One names ExiledWithSource ONLY in its
// activated ability's `ValidTgts$ Creature.ExiledWithSource`; its ETB exile
// (a Hidden$ graveyard-origin ChangeZone) carries no mention. The engine must
// still record "exiled with Mimeoplasm" on that move, or the {2} clone ability
// is never offered.

import (
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// mimeoplasmScenario builds the cast-with-X=1 prelude: p0 holds Mimeoplasm and
// has Grizzly Bears in the graveyard and a Savannah Lions that setup placed
// into exile (a card exiled by NOTHING, so never "exiled with" Mimeoplasm).
func mimeoplasmScenario(activateTarget string) string {
	return `{"name":"mimeoplasm-revered-one-exiled-with","cr":["607.2a","706.2"],"why":"inline",` +
		`"setup":{"p0":{"hand":["Mimeoplasm, Revered One"],"graveyard":["Grizzly Bears"],"exile":["Savannah Lions"]}},"steps":[` +
		`{"op":"cast","seat":0,"card":"p0:Mimeoplasm, Revered One","mana":"CBGU","answers":[{"kind":"choose","pick":["X = 1"]}]},` +
		`{"op":"resolve","answers":[{"kind":"choose","pick":["Grizzly Bears"]}]},` +
		`{"op":"activate","seat":0,"card":"p0:Mimeoplasm, Revered One","ability_index":0,"mana":"CC","targets":["` + activateTarget + `"]},` +
		`{"op":"resolve"}]}`
}

func TestExiledWithSourceFromAbilityLineOffersMimeoplasmClone(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	res, err := RunOracleScenarioJSON(reg, []byte(mimeoplasmScenario("p0:Grizzly Bears")))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("scenario failed: %v\n%s", res.Fails, strings.Join(res.Transcript, "\n"))
	}
	// Preconditions, read at the checkpoint after the cast resolved: Mimeoplasm
	// is on the battlefield with its three counters and the Bears sit in exile
	// next to the setup-exiled Lions (so the ETB really exiled them).
	var after OracleSnapshot
	for _, s := range res.Snapshots {
		if _, ok := snapPerm(s, "p0:Mimeoplasm, Revered One"); ok {
			after = s
			break
		}
	}
	if after.Checkpoint == "" {
		t.Fatalf("Mimeoplasm never reached the battlefield: %+v", res.Snapshots)
	}
	if len(after.Players) == 0 || !slices.Contains(after.Players[0].Exile, "Grizzly Bears") || !slices.Contains(after.Players[0].Exile, "Savannah Lions") {
		t.Fatalf("precondition: p0 exile = %v, want Grizzly Bears (ETB) and Savannah Lions (setup)", after.Players[0].Exile)
	}
	// The clone activation resolved: Mimeoplasm (still the same object) now
	// wears the Bears' face, over its 0/0 base plus the three ETB counters.
	final := res.Snapshots[len(res.Snapshots)-1]
	p, ok := snapPerm(final, "p0:Mimeoplasm, Revered One")
	if !ok {
		t.Fatalf("Mimeoplasm left the battlefield: %+v", final.Permanents)
	}
	if p.Name != "Grizzly Bears" || p.PT != "3/3" || p.Counters["P1P1"] != 3 {
		t.Fatalf("Mimeoplasm after the clone = %+v, want a Grizzly Bears 3/3 with three +1/+1 counters", p)
	}
}

// A creature card that is in exile but was NOT exiled with Mimeoplasm is not a
// legal target of its clone ability: the provenance is per source, not "any
// exiled creature".
func TestExiledWithSourceFromAbilityLineExcludesUnrelatedExile(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	res, err := RunOracleScenarioJSON(reg, []byte(mimeoplasmScenario("p0:Savannah Lions")))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) == 0 {
		t.Fatalf("activating Mimeoplasm on an unrelated exiled card succeeded\n%s", strings.Join(res.Transcript, "\n"))
	}
	joined := strings.Join(res.Fails, "\n")
	if !strings.Contains(joined, "not offered") {
		t.Fatalf("failure = %q, want the activation reported as not offered (a different failure means the setup, not the target filter, broke)", joined)
	}
	// Precondition: the ETB exile did run (Bears exiled with Mimeoplasm), so the
	// refusal above is the filter discriminating between two exiled cards.
	for _, s := range res.Snapshots {
		if _, ok := snapPerm(s, "p0:Mimeoplasm, Revered One"); ok {
			if !slices.Contains(s.Players[0].Exile, "Grizzly Bears") || !slices.Contains(s.Players[0].Exile, "Savannah Lions") {
				t.Fatalf("precondition: p0 exile = %v, want both Bears and Lions", s.Players[0].Exile)
			}
			return
		}
	}
	t.Fatalf("Mimeoplasm never reached the battlefield: %+v", res.Snapshots)
}
