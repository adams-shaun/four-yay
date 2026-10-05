package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Tasha's +1 announces its graveyard ChangeZone link before paying loyalty.
// Three opponents exist, but only two have eligible graveyard cards: the
// OneEach maximum must use represented controllers, not the raw opponent count.
func TestTashaOneEachChangeZoneSubTargetsUseControllerCapacity(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	tasha := mustCorpusCard(t, reg, "Tasha, the Witch Queen")
	bolt := mustCorpusCard(t, reg, "Lightning Bolt")
	if len(tasha.Faces[0].Abilities) == 0 {
		t.Fatal("precondition: Tasha has no activated abilities")
	}
	root := tasha.Faces[0].Abilities[0]
	sub := root.Sub
	if root.API != "Draw" || sub == nil || sub.API != "ChangeZone" ||
		sub.Params["Origin"] != "Graveyard" || sub.Params["TargetMin"] != "0" ||
		sub.Params["TargetMax"] != "OneEach" || sub.Params["TargetsForEachPlayer"] != "True" ||
		sub.Params["ValidTgts"] != "Instant.OppCtrl,Sorcery.OppCtrl" {
		t.Fatalf("precondition: Tasha's +1 ChangeZone target shape changed: root %+v, sub %+v", root, sub)
	}
	cfg := seatZeroStart(Config{Seed: 42, Names: []string{"a", "b", "c", "d"}, Decks: [][]*cards.Card{
		append(mountainDeck(t, 40), tasha),
		append(mountainDeck(t, 40), bolt, bolt),
		append(mountainDeck(t, 40), bolt),
		mountainDeck(t, 40),
	}})
	e := New(cfg)
	e.Advance()
	tashaID := moveSeededCard(t, e, 0, tasha, state.ZBattlefield)
	a := moveSeededCard(t, e, 1, bolt, state.ZGraveyard)
	b := moveSeededCard(t, e, 1, bolt, state.ZGraveyard)
	c := moveSeededCard(t, e, 2, bolt, state.ZGraveyard)
	if o := e.G.Obj(tashaID); o == nil || o.Zone != state.ZBattlefield || o.Counter("LOYALTY") < 1 {
		t.Fatalf("precondition: Tasha must be a battlefield walker with loyalty to pay +1: %+v", o)
	}
	for _, item := range []struct {
		id    state.ObjID
		owner state.PlayerID
	}{{a, 1}, {b, 1}, {c, 2}} {
		if o := e.G.Obj(item.id); o == nil || o.Zone != state.ZGraveyard || o.Controller != item.owner {
			t.Fatalf("precondition: %d must be in opponent %d's graveyard and controlled by them: %+v", item.id, item.owner, o)
		}
	}
	if a == b || a == c || b == c {
		t.Fatal("precondition: targets must be distinct objects")
	}
	if rawMin, rawMax := e.resolvedTargetBounds(0, tashaID, sub, 0); rawMin != 0 || rawMax != 3 {
		t.Fatalf("precondition: plain OneEach bound %d..%d, want 0..3 (different from eligible capacity 2)", rawMin, rawMax)
	}
	e.priorityRound()
	submitChoices(t, e, abilityOption(t, e, tashaID, 0).Index)
	d := castSubAsk(t, e)
	if d.Player != 0 || d.Min != 0 || d.Max != 2 {
		t.Fatalf("Tasha's cast_sub bounds = player %d, %d..%d, want 0, 0..2", d.Player, d.Min, d.Max)
	}
	indices := map[state.ObjID]int{}
	groups := map[state.ObjID]string{}
	for _, o := range d.Options {
		if o.Obj != a && o.Obj != b && o.Obj != c {
			t.Fatalf("unexpected graveyard option %+v", o)
		}
		indices[o.Obj], groups[o.Obj] = o.Index, o.Group
		if o.Controller != e.G.Obj(o.Obj).Controller {
			t.Fatalf("option %+v has wrong controller", o)
		}
	}
	if len(d.Options) != 3 || len(indices) != 3 || groups[a] == "" || groups[a] != groups[b] || groups[a] == groups[c] {
		t.Fatalf("options %+v groups %v: want two targets from seat 1 and one from seat 2, grouped per opponent", d.Options, groups)
	}
	intent := func(ids ...state.ObjID) decision.Intent {
		choices := make([]int, 0, len(ids))
		for _, id := range ids {
			choices = append(choices, indices[id])
		}
		return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}
	}
	if err := d.Validate(intent(a, b)); err == nil {
		t.Fatal("two targets from one opponent were accepted")
	}
	if err := d.Validate(intent(a, c)); err != nil {
		t.Fatalf("one target from each opponent rejected: %v", err)
	}
	if err := e.Submit(intent(a, c)); err != nil {
		t.Fatalf("announcing both opponents' graveyard targets failed: %v", err)
	}
}
