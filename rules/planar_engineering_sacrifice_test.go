package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Feedback fb-20260916T201317Z-841054c5 ("planar engineering -- I was given no
// choice for which land to select"): the pre-ask engine took
// `eligible[:n]` with n hardcoded to 1, so "Sacrifice two lands" both picked
// the land itself and sacrificed only one. The sacrifice-asks merge fixed
// effSacrifice; these tests pin the reported card end to end on the REAL
// corpus card (Planar Engineering, SP$ Sacrifice | Defined$ You |
// SacValid$ Land | Amount$ 2 | SubAbility$ DBChangeZone), which is in the
// hearthhull-worldseed-landfall repo deck but not in any legacy golden deck,
// so no chain head depends on it.
//
// The helpers come from search_library_test.go (same package): the deck is
// built from compiled corpus cards only, so no Forge script text is
// committed here either.

// planarTestEngine deals seat 0 a 40-card deck whose first four cards are
// Planar Engineering, Rockfall Vale, a Swamp and an Island, followed by eight
// Forests and eight Mountains (the library's basic-land search pool) and
// Grizzly Bears, then moves bfLands (found by name in hand/library, in the
// order given) onto seat 0's battlefield. The battlefield order IS the order
// of bfLands, which is what lets the main test answer with lands that are
// NOT the first two in zone order -- the exact shape the old
// deterministic-first stand-in got wrong.
func planarTestEngine(t *testing.T, reg *cards.Registry, bfLands ...string) (*Engine, Config) {
	t.Helper()
	forest := searchCorpusCard(t, reg, "Forest")
	mountain := searchCorpusCard(t, reg, "Mountain")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	deck := []*cards.Card{
		searchCorpusCard(t, reg, "Planar Engineering"),
		searchCorpusCard(t, reg, "Rockfall Vale"),
		searchCorpusCard(t, reg, "Swamp"),
		searchCorpusCard(t, reg, "Island"),
	}
	for i := 0; i < 8; i++ {
		deck = append(deck, forest, mountain)
	}
	for len(deck) < 40 {
		deck = append(deck, bear)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = mountain
	}
	cfg := seatZeroStart(Config{Seed: 9203, Names: []string{"planar", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	for _, name := range bfLands {
		searchMoveByName(t, e, name, state.ZBattlefield)
	}
	return e, cfg
}

// castPlanar funds {3}{G} from the pool (no tap-to-pay in this build), casts
// the spell from hand, and returns the first mid-resolution decision that
// comes back after both seats pass priority.
func castPlanar(t *testing.T, e *Engine) *decision.Decision {
	t.Helper()
	id := searchMoveByName(t, e, "Planar Engineering", state.ZHand)
	addMana(t, e, 0, "GGGG")
	return castFixture(t, e, id, -1)
}

// sacrificeEvents collects the log's sacrifice moves.
func sacrificeEvents(t *testing.T, e *Engine) []events.Event {
	t.Helper()
	var out []events.Event
	for _, ev := range e.L.Events {
		if events.IsSacrifice(ev) {
			out = append(out, ev)
		}
	}
	return out
}

// objName renders an object's face name for failure messages.
func objName(t *testing.T, e *Engine, id state.ObjID) string {
	t.Helper()
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		t.Fatalf("object %d missing", id)
	}
	return o.Face().Name
}

// TestPlanarEngineeringSacrificeTwoLandsAsksAndHonoursTheAnswer is the
// reported scenario: seven lands on the battlefield (Rockfall Vale third,
// so the deterministic-first stand-in would have taken Forest+Mountain),
// Planar Engineering resolves.
//
// Pinned, by name:
//  1. the sacrifice ask is posed to the caster mid-resolution -- a KChoose
//     with Min == Max == 2, ResumeKind "sacrifice", one option per eligible
//     battlefield land (all seven, not a trimmed set);
//  2. the answered two lands -- NOT the first two in zone order -- are the
//     two sacrificed, each via a move_zone carrying the "sacrificed" marker;
//  3. the SubAbility$ DBChangeZone search still poses its own ask over the
//     library's basic lands (the half of the card that already worked) and
//     honours the answer;
//  4. the four searched lands enter the battlefield tapped.
func TestPlanarEngineeringSacrificeTwoLandsAsksAndHonoursTheAnswer(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := planarTestEngine(t, reg, "Forest", "Mountain", "Rockfall Vale", "Swamp", "Island", "Forest", "Mountain")
	d := castPlanar(t, e)

	// (1) the ask.
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("after casting Planar Engineering: %+v, want a KChoose sacrifice decision", d)
	}
	if d.Min != 2 || d.Max != 2 {
		t.Fatalf("sacrifice ask range = %d..%d, want 2..2 (Amount$ 2)", d.Min, d.Max)
	}
	if d.ResumeKind != "sacrifice" {
		t.Fatalf("sacrifice ask ResumeKind = %q, want \"sacrifice\"", d.ResumeKind)
	}
	if d.Player != 0 {
		t.Fatalf("sacrifice ask player = %d, want the caster (0)", d.Player)
	}
	battlefield := e.G.Zone(state.ZBattlefield, 0)
	if len(d.Options) != len(battlefield) {
		t.Fatalf("sacrifice ask has %d options, want one per eligible land (%d)", len(d.Options), len(battlefield))
	}
	for i, o := range d.Options {
		if o.Kind != "sacrifice" {
			t.Fatalf("option %d kind = %q, want \"sacrifice\"", i, o.Kind)
		}
		if o.Obj != battlefield[i] {
			t.Fatalf("option %d = obj %d, want battlefield zone-order land %d", i, o.Obj, battlefield[i])
		}
	}

	// (2) answer with the THIRD and FIFTH lands in zone order -- Rockfall
	// Vale and Island -- so honouring the answer is distinguishable from the
	// old deterministic first-two pick (Forest + Mountain).
	wantSac := []state.ObjID{battlefield[2], battlefield[4]}
	submitChoices(t, e, d.Options[2].Index, d.Options[4].Index)

	// (3) the search half asks next, mid-resolution.
	d = passUntilNonPriority(t, e, 20)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "search" {
		t.Fatalf("after the sacrifice answer: %+v, want the library search KChoose", d)
	}
	if d.Max != 4 {
		t.Fatalf("search ask max = %d, want 4 (ChangeNum$ 4)", d.Max)
	}
	basics := basicLibraryIDs(e)
	if len(d.Options) != len(basics) {
		t.Fatalf("search ask has %d options, want one per library basic land (%d) -- no bears", len(d.Options), len(basics))
	}
	wantSearched := []state.ObjID{d.Options[0].Obj, d.Options[2].Obj, d.Options[4].Obj, d.Options[6].Obj}
	submitChoices(t, e, d.Options[0].Index, d.Options[2].Index, d.Options[4].Index, d.Options[6].Index)
	if d = e.Pending(); d == nil || d.Kind != decision.KPriority {
		t.Fatalf("after the search answer pending = %+v, want priority", d)
	}

	// The two answered lands were the two sacrificed, each under the
	// "sacrificed" action marker; nothing else was.
	sacs := sacrificeEvents(t, e)
	if len(sacs) != 2 {
		t.Fatalf("got %d sacrifice moves, want exactly 2", len(sacs))
	}
	for i, id := range wantSac {
		if sacs[i].Obj != id {
			t.Fatalf("sacrifice %d = obj %d (%s), want answered land %d (%s)",
				i, sacs[i].Obj, objName(t, e, sacs[i].Obj), id, objName(t, e, id))
		}
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZGraveyard {
			t.Fatalf("answered land %d (%s) zone = %+v, want graveyard", id, objName(t, e, id), o)
		}
	}
	for _, id := range []state.ObjID{battlefield[0], battlefield[1], battlefield[3], battlefield[5], battlefield[6]} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("unanswered land %d (%s) left the battlefield: %+v", id, objName(t, e, id), o)
		}
	}

	// (4) the four searched lands entered tapped.
	for _, id := range wantSearched {
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("searched land %d not on the battlefield: %+v", id, o)
		}
		if !o.Tapped {
			t.Fatalf("searched land %d (%s) entered untapped, want Tapped$ True", id, objName(t, e, id))
		}
	}
	shuffled := false
	for _, ev := range e.L.Events {
		shuffled = shuffled || (ev.Kind == events.Shuffle && ev.Player == 0)
	}
	if !shuffled {
		t.Fatal("the post-search shuffle never happened")
	}
	replayCheck(t, e, cfg)
}

// TestPlanarEngineeringSacrificeNoAskWhenEligibleLEAmount pins the
// strict-supersets shape on the real card: with exactly Amount$ eligible
// lands the batch takes everything eligible and asks nothing -- a sacrifice
// ask would have no answer anyone could give differently. The very next ask
// is the search.
func TestPlanarEngineeringSacrificeNoAskWhenEligibleLEAmount(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := planarTestEngine(t, reg)
	land0 := searchMoveByName(t, e, "Rockfall Vale", state.ZBattlefield)
	land1 := searchMoveByName(t, e, "Forest", state.ZBattlefield)
	d := castPlanar(t, e)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "search" {
		t.Fatalf("after casting with exactly two eligible lands: %+v, want the search ask directly (no sacrifice ask)", d)
	}
	sacs := sacrificeEvents(t, e)
	if len(sacs) != 2 {
		t.Fatalf("got %d sacrifice moves, want both eligible lands without asking", len(sacs))
	}
	for _, id := range []state.ObjID{land0, land1} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZGraveyard {
			t.Fatalf("eligible land %d zone = %+v, want graveyard", id, o)
		}
	}
	picked := d.Options[0].Obj
	submitChoices(t, e, d.Options[0].Index)
	if d = e.Pending(); d == nil || d.Kind != decision.KPriority {
		t.Fatalf("after the search answer pending = %+v, want priority", d)
	}
	if o := e.G.Obj(picked); o != nil && o.Zone == state.ZBattlefield && !o.Tapped {
		t.Fatalf("searched land %d entered untapped, want Tapped$ True", picked)
	}
	replayCheck(t, e, cfg)
}

// TestPlanarEngineeringZeroEligibleSacrificesNothing: with no lands on the
// battlefield at all the sacrifice half is a no-op with no ask, and the
// search half still runs.
func TestPlanarEngineeringZeroEligibleSacrificesNothing(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := planarTestEngine(t, reg)
	d := castPlanar(t, e)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "search" {
		t.Fatalf("after casting with zero eligible lands: %+v, want the search ask directly (no sacrifice ask)", d)
	}
	if sacs := sacrificeEvents(t, e); len(sacs) != 0 {
		t.Fatalf("got %d sacrifice moves, want none with zero eligible lands", len(sacs))
	}
	picked := d.Options[0].Obj
	submitChoices(t, e, d.Options[0].Index)
	if d = e.Pending(); d == nil || d.Kind != decision.KPriority {
		t.Fatalf("after the search answer pending = %+v, want priority", d)
	}
	if o := e.G.Obj(picked); o != nil && o.Zone == state.ZBattlefield && !o.Tapped {
		t.Fatalf("searched land %d entered untapped, want Tapped$ True", picked)
	}
	replayCheck(t, e, cfg)
}
