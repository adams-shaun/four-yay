package rules

// Colossal Grave-Reaver's death-adjacent batch trigger:
// "Whenever one or more creature cards are put into your graveyard from your
// library, put one of them onto the battlefield." Its body is
// `DB$ ChangeZone | ChooseFromDefined$ TriggeredCards | ChangeNum$ 1 |
// Mandatory$ True | Hidden$ True | Origin$ Graveyard | Destination$
// Battlefield` — the pick must be offered over (and taken from) exactly the
// creature cards the trigger CAPTURED (the moved batch, Ctx.Remembered), not
// over every card in the graveyard, and an unresolvable selector fails
// closed. This file is the engine-level pin for the TriggeredCards spelling.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// graveReaverEngine seats the REAL compiled Colossal Grave-Reaver on seat 0's
// battlefield in a two-seat game. Seat 0's library is Ornithopter over
// Mountains (a creature card among non-creatures, so the trigger's own
// ChangesZone filter -- ValidCards$ Creature.YouOwn -- is what would capture
// it) and its graveyard already holds Memnite, an unrelated creature that is
// in the origin zone but NOT a member of the captured set.
func graveReaverEngine(t *testing.T, seed uint64) (*Engine, state.ObjID, state.ObjID, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	reaver := mustCorpusCard(t, reg, "Colossal Grave-Reaver")
	ornithopter := mustCorpusCard(t, reg, "Ornithopter")
	memnite := mustCorpusCard(t, reg, "Memnite")
	for _, c := range []*cards.Card{reaver, ornithopter, memnite} {
		if c.Faces[0].Name == "" {
			t.Fatalf("corpus card without a name: %+v", c.Faces[0])
		}
	}
	// Preconditions the whole test rests on: the Reaver really prints the
	// ChooseFromDefined$ TriggeredCards leg, Ornithopter really is a creature
	// card (the trigger's filter must capture it), and Memnite really is a
	// creature too (so only the captured-set membership can separate them).
	var leg *cards.SA
	for _, face := range reaver.Faces {
		for name := range face.SVars {
			if sa := cards.ResolveSVar(face.SVars, name); sa != nil &&
				sa.Params["ChooseFromDefined"] == "TriggeredCards" {
				leg = sa
			}
		}
	}
	if leg == nil {
		t.Fatal("corpus pin moved: Colossal Grave-Reaver no longer carries ChooseFromDefined$ TriggeredCards")
	}
	if !ornithopter.Faces[0].IsCreature() || !memnite.Faces[0].IsCreature() {
		t.Fatal("corpus pin moved: Ornithopter/Memnite are not creature cards")
	}

	e := New(Config{Seed: seed, Names: []string{"a", "b"}, Tokens: reg.Tokens,
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}})
	for p := state.PlayerID(0); p < 2; p++ {
		e.G.SetZone(state.ZHand, p, nil)
	}
	// The Reaver is placed directly on the battlefield with NO enter event,
	// so its own "enters or attacks, mill three" trigger stays quiet: the
	// only trigger this test drives is the real library->graveyard move.
	rov := e.G.AddObject(reaver, 0)
	rov.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{rov.ID})
	orn := e.G.AddObject(ornithopter, 0)
	lib := append([]state.ObjID{orn.ID}, e.G.Zone(state.ZLibrary, 0)...)
	e.G.SetZone(state.ZLibrary, 0, lib)
	mem := e.G.AddObject(memnite, 0)
	mem.Zone = state.ZGraveyard
	e.G.SetZone(state.ZGraveyard, 0, []state.ObjID{mem.ID})

	// Preconditions: the source permanent is where the trigger reads it, the
	// candidate creature card is in the origin zone the body scans, and the
	// unrelated creature sits in the same origin zone to prove the pool (not
	// the zone) bounds the offer.
	if o := e.G.Obj(rov.ID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition failed: Grave-Reaver zone %v, want battlefield", o)
	}
	if o := e.G.Obj(orn.ID); o == nil || o.Zone != state.ZLibrary {
		t.Fatalf("precondition failed: Ornithopter zone %v, want library", o)
	}
	if o := e.G.Obj(mem.ID); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("precondition failed: Memnite zone %v, want graveyard", o)
	}
	if e.G.Obj(orn.ID).Owner != 0 || e.G.Obj(mem.ID).Owner != 0 {
		t.Fatal("precondition failed: candidates are not seat 0's own cards")
	}
	return e, rov.ID, orn.ID, mem.ID
}

// TestColossalGraveReaverChooseFromDefined drives the real triggering event
// (one creature card put into your graveyard from your library) and pins the
// whole leg: exactly the captured Ornithopter is offered -- the unrelated
// Memnite already in the graveyard is NOT -- and the answered pick puts the
// Ornithopter onto the battlefield while the Memnite stays in the graveyard.
func TestColossalGraveReaverChooseFromDefined(t *testing.T) {
	t.Parallel()
	e, rov, orn, mem := graveReaverEngine(t, 911)

	// The triggering event, as a real logged move.
	e.emit(events.Event{Kind: events.MoveZone, Obj: orn, From: state.ZLibrary, To: state.ZGraveyard})
	e.Advance()

	// Drain to the ChangeZone pick ask. Only a priority pass may appear
	// before it; anything else means the engine took a different shape.
	var ask *decision.Decision
	for i := 0; i < 40; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("engine stalled with no pending decision after the trigger event")
		}
		if d.Kind == decision.KChoose && d.ResumeSA != nil && d.ResumeSA.API == "ChangeZone" &&
			d.ResumeSA.Params["ChooseFromDefined"] == "TriggeredCards" {
			ask = d
			break
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("unexpected %v before the pick ask: %+v", d.Kind, d)
		}
		cfdPass(t, e, d)
	}
	if ask == nil {
		t.Fatal("the ChangeZone pick ask never appeared within 40 decisions")
	}
	// Mandatory$ True: the ask demands exactly one pick.
	if ask.Min != 1 || ask.Max != 1 {
		t.Fatalf("pick ask Min/Max = %d/%d, want 1/1 (Mandatory$ True, ChangeNum$ 1)", ask.Min, ask.Max)
	}
	assertPickOptions(t, e, ask, orn)
	if cfdOptionsContain(t, ask, mem) {
		t.Fatalf("the unrelated graveyard creature %d was offered; options: %+v", mem, ask.Options)
	}

	// Answer the pick with the Ornithopter's option index.
	idx := cfdOptionIndexFor(t, ask, orn)
	if err := e.Submit(decision.Intent{Seq: ask.Seq, Player: ask.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit pick: %v", err)
	}
	e.Advance()
	if got := e.G.Obj(orn).Zone; got != state.ZBattlefield {
		t.Fatalf("picked creature card zone = %v, want battlefield", got)
	}
	if got := e.G.Obj(mem).Zone; got != state.ZGraveyard {
		t.Fatalf("unrelated graveyard creature zone = %v, want graveyard", got)
	}
	if got := e.G.Obj(rov).Zone; got != state.ZBattlefield {
		t.Fatalf("precondition broken by resolution: Grave-Reaver zone %v, want battlefield", got)
	}
}

// assertPickOptions pins the offered set exactly: the ask is for the
// Ornithopter alone (a one-member captured batch), nonempty because the
// captured card really is in the origin zone.
func assertPickOptions(t *testing.T, e *Engine, d *decision.Decision, orn state.ObjID) {
	t.Helper()
	if len(d.Options) == 0 {
		t.Fatal("precondition failed: the offered pool is empty although the captured creature card is in the origin zone")
	}
	if len(d.Options) != 1 || d.Options[0].Obj != orn {
		t.Fatalf("options = %+v, want exactly the captured Ornithopter %d", d.Options, orn)
	}
}

func cfdOptionsContain(t *testing.T, d *decision.Decision, id state.ObjID) bool {
	t.Helper()
	for _, o := range d.Options {
		if o.Obj == id {
			return true
		}
	}
	return false
}

func cfdOptionIndexFor(t *testing.T, d *decision.Decision, id state.ObjID) int {
	t.Helper()
	for _, o := range d.Options {
		if o.Obj == id {
			return o.Index
		}
	}
	t.Fatalf("option for %d not offered: %+v", id, d.Options)
	return -1
}

func cfdPass(t *testing.T, e *Engine, d *decision.Decision) {
	t.Helper()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "pass" {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("priority decision with no pass option: %+v", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit pass: %v", err)
	}
}
