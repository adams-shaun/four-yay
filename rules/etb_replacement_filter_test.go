// kw:ETBReplacement's trailing FILTER field (the 5th colon field) is the
// replacement's real ValidCard$ spec, and the trailing ZONE field (4th) is
// the source's ActiveZones$ — the follow-up pins the brief's named carriers
// on top of the zone file's Dearly Departed / Bramblewood Paragon pins
// (rules/etbreplacement_zone_test.go). Before the expansion read the fields
// (commit dcfa4d4a) every expanded repl carried the Card.Self placeholder,
// so a filter carrier like Venser, Visionary Traveler admitted ONLY its own
// entry and Metallic Mimic pumped itself instead of later creatures of the
// chosen type.
//
// The carriers are real corpus cards; the fixtures are handEngine engines
// (rules/legal_test.go) with corpusAlternativeCard hand members. Metallic
// Mimic is in the avengers-assemble repo deck but in no legacy golden deck,
// so no chain head depends on any behaviour pinned here.

package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestVenserVisionaryTravelerFilterGate is the brief's four-way pin:
//   - Venser's OWN entry takes nothing (he is a planeswalker; the filter is
//     Creature.!token+YouCtrl+!wasCastFromYourHand),
//   - a raw-move creature he controls takes 2 P1P1 (never cast),
//   - a creature CAST from his hand takes nothing (the !wasCastFromYourHand
//     provenance, task castprov3, evaluated through the Moved matcher),
//   - an opponent's creature takes nothing (YouCtrl).
func TestVenserVisionaryTravelerFilterGate(t *testing.T) {
	t.Parallel()
	e := handEngine(t,
		corpusAlternativeCard(t, "Venser, Visionary Traveler"),
		corpusAlternativeCard(t, "Grizzly Bears"),
		corpusAlternativeCard(t, "Grizzly Bears"))
	hand := e.G.Zone(state.ZHand, 0)
	venser, castBear, rawBear := hand[0], hand[1], hand[2]

	// Own entry: a planeswalker entering matches no Creature filter.
	placeFromHand(t, e, venser)
	if got := e.G.Obj(venser).Counter("P1P1"); got != 0 {
		t.Fatalf("Venser entered with %d P1P1, want 0 (the oracle grants nothing on his own entry)", got)
	}

	// Raw-move creature (never cast): the gate holds.
	placeFromHand(t, e, rawBear)
	if got := e.G.Obj(rawBear).Counter("P1P1"); got != 2 {
		t.Fatalf("raw-move creature entered with %d P1P1, want 2 (not cast from hand)", got)
	}

	// Cast-from-hand creature: the provenance gate denies.
	e.G.Players[0].Pool[state.MC], e.G.Players[0].Pool[state.MG] = 1, 1
	castMode(t, e, castBear, "")
	finishCast(t, e, castBear)
	if got := e.G.Obj(castBear).Counter("P1P1"); got != 0 {
		t.Fatalf("cast-from-hand creature entered with %d P1P1, want 0 (!wasCastFromYourHand must deny)", got)
	}

	// Opponent's creature: YouCtrl denies.
	o := e.G.AddObject(corpusAlternativeCard(t, "Grizzly Bears"), 1)
	o.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 1, []state.ObjID{o.ID})
	placeFromHand(t, e, o.ID)
	if got := e.G.Obj(o.ID).Counter("P1P1"); got != 0 {
		t.Fatalf("opponent's creature entered with %d P1P1, want 0 (YouCtrl must deny)", got)
	}
}

// chooseETBType answers the entry-boundary "as this enters" creature-type
// ask with the option labelled want, failing loudly if the pending decision is
// not that ask or the label is not offered.
func chooseETBType(t *testing.T, e *Engine, want string) {
	t.Helper()
	d := e.Pending()
	if d == nil {
		e.resolveTop()
		d = e.Pending()
	}
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "etb" || len(d.Options) == 0 || d.Options[0].Kind != "type" {
		t.Fatalf("expected the as-enters type ask, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Label == want {
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("type %q not offered: %+v", want, d.Options)
}

// TestVolverKickGatesEntryCounters pins the self-entry filter with a kick
// gate (Card.Self+kicked <n>): before the filter was read the Volver's two
// ETBReplacement lines fired unconditionally (2+1 counters on every entry);
// now the plain cast enters bare, the {1}{U} kicker (part 1) takes the
// Strength counters and the {B} kicker (part 2) the Pumped one.
func TestVolverKickGatesEntryCounters(t *testing.T) {
	t.Parallel()
	e := handEngine(t,
		corpusAlternativeCard(t, "Anavolver"),
		corpusAlternativeCard(t, "Anavolver"),
		corpusAlternativeCard(t, "Anavolver"))
	ids := e.G.Zone(state.ZHand, 0)

	e.G.Players[0].Pool[state.MC], e.G.Players[0].Pool[state.MG] = 3, 1
	castMode(t, e, ids[0], "")
	finishCast(t, e, ids[0])
	if got := e.G.Obj(ids[0]).Counter("P1P1"); got != 0 {
		t.Fatalf("plain-cast Anavolver entered with %d P1P1, want 0 (neither kick gate holds)", got)
	}

	e.G.Players[0].Pool[state.MC], e.G.Players[0].Pool[state.MG], e.G.Players[0].Pool[state.MU] = 4, 1, 1
	castMode(t, e, ids[1], "kicked1")
	finishCast(t, e, ids[1])
	if got := e.G.Obj(ids[1]).Counter("P1P1"); got != 2 {
		t.Fatalf("kicked1 Anavolver entered with %d P1P1, want 2 (Card.Self+kicked 1)", got)
	}

	e.G.Players[0].Pool[state.MC], e.G.Players[0].Pool[state.MG], e.G.Players[0].Pool[state.MB] = 3, 1, 1
	castMode(t, e, ids[2], "kicked2")
	finishCast(t, e, ids[2])
	if got := e.G.Obj(ids[2]).Counter("P1P1"); got != 1 {
		t.Fatalf("kicked2 Anavolver entered with %d P1P1, want 1 (Card.Self+kicked 2)", got)
	}
}

// TestMasterBiomancerOtherFilterXCounters pins the same filter class with a
// non-literal counter count: the Biomancer's CounterNum$ X (SVar X:
// Count$CardPower, a 2/4) gives each OTHER creature you control its power in
// +1/+1 counters, and its own entry nothing.
func TestMasterBiomancerOtherFilterXCounters(t *testing.T) {
	t.Parallel()
	e := handEngine(t,
		corpusAlternativeCard(t, "Master Biomancer"),
		corpusAlternativeCard(t, "Grizzly Bears"))
	biomancer, bear := e.G.Zone(state.ZHand, 0)[0], e.G.Zone(state.ZHand, 0)[1]
	placeFromHand(t, e, biomancer)
	if got := e.G.Obj(biomancer).Counter("P1P1"); got != 0 {
		t.Fatalf("Biomancer entered with %d P1P1, want 0 (Creature.YouCtrl+Other)", got)
	}
	placeFromHand(t, e, bear)
	if got := e.G.Obj(bear).Counter("P1P1"); got != 2 {
		t.Fatalf("later creature entered with %d P1P1, want 2 (CounterNum$ X = Count$CardPower = 2)", got)
	}
}
