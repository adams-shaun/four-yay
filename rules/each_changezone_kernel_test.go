package rules

// Kernel-era restorations of the effects-package ChangeType$ EACH tests the
// W3 legacy removal deleted (each_changetype, each_object_pick,
// each_zero_count): the per-type structured pick on the library-search,
// hand and hidden-pick paths, driven through a real engine.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const (
	kr1Forest = "Name:Forest\nTypes:Basic Land Forest\nOracle:x\n"
	kr1Plains = "Name:Plains\nTypes:Basic Land Plains\nOracle:x\n"
)

// kr1ClearHand returns every card in seat p's hand except keep to the
// library with logged MoveZones, so a hand-origin pick sees only the cards
// a test puts there.
func kr1ClearHand(t *testing.T, e *Engine, p state.PlayerID, keep state.ObjID) {
	t.Helper()
	for _, id := range append([]state.ObjID(nil), e.G.Zone(state.ZHand, p)...) {
		if id != keep {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZLibrary, Player: p})
		}
	}
}

// kr1Yes answers a yes/no confirmation with its "yes" option.
func kr1Yes(t *testing.T, e *Engine, resume string) *decision.Decision {
	t.Helper()
	d := e.Pending()
	if d == nil || d.ResumeKind != resume {
		t.Fatalf("pending = %+v, want the %s confirmation", d, resume)
	}
	for _, o := range d.Options {
		if o.Kind == "yes" {
			return kr1Answer(t, e, d.Kind, d.Player, o.Index)
		}
	}
	t.Fatalf("no yes option on %+v", d)
	return nil
}

const kr1VergeBody = "A:SP$ ChangeZone | Origin$ Library | Destination$ Battlefield | ChangeType$ EACH Forest & Plains | Tapped$ True"

// TestKr1KrosanVergeEachSearchAsksPerTypeAndHonoursTheAnswer (was
// TestKrosanVergeEachSearchAsksPerTypeAndHonoursTheAnswer): Krosan Verge's
// search over a library holding Forests AND Plains asks the controller for
// 0..2 cards, the Forests grouped "0" and the Plainses "1" in library
// order; one-per-group is the only legal shape, and the answered Forest and
// Plains enter tapped before exactly one shuffle.
func TestKr1KrosanVergeEachSearchAsksPerTypeAndHonoursTheAnswer(t *testing.T) {
	t.Parallel()
	e, cfg, id := kr1New(t, 150, kr1Sorcery("VergeSearch", kr1VergeBody),
		[]string{kr1Forest, kr1Forest, kr1Plains, kr1Plains}, nil)
	addMana(t, e, 0, "B")
	ids := kr1Top(t, e, 0, "Forest", "Forest", "Plains", "Plains")
	d := kr1Cast(t, e, id)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "search" || d.Player != 0 {
		t.Fatalf("decision = %+v, want a KChoose search for seat 0", d)
	}
	if d.Min != 0 || d.Max != 2 {
		t.Fatalf("range = %d..%d, want 0..2", d.Min, d.Max)
	}
	if d.Prompt != "Search a library: choose one card of each listed type" {
		t.Fatalf("prompt = %q", d.Prompt)
	}
	if len(d.Options) != 4 {
		t.Fatalf("%d options, want 4: %+v", len(d.Options), d.Options)
	}
	for i, w := range []struct {
		id           state.ObjID
		group, label string
	}{{ids[0], "0", "Forest"}, {ids[1], "0", "Forest"}, {ids[2], "1", "Plains"}, {ids[3], "1", "Plains"}} {
		o := d.Options[i]
		if o.Obj != w.id || o.Group != w.group || o.Label != w.label || o.Kind != "search" {
			t.Fatalf("option %d = %+v, want obj %d group %q label %q", i, o, w.id, w.group, w.label)
		}
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{0, 1}}); err == nil {
		t.Fatal("two same-group choices accepted")
	}
	mark := len(e.L.Events)
	if p := kr1Answer(t, e, decision.KChoose, 0, 1, 2); p != nil {
		t.Fatalf("unexpected follow-up ask %+v", p)
	}
	for _, f := range []state.ObjID{ids[1], ids[2]} {
		if o := e.G.Obj(f); o.Zone != state.ZBattlefield || !o.Tapped {
			t.Fatalf("fetched land %d zone %s tapped %v, want a tapped permanent", f, o.Zone, o.Tapped)
		}
	}
	shuffles := 0
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.Shuffle && ev.Player == 0 {
			shuffles++
		}
	}
	if shuffles != 1 {
		t.Fatalf("shuffles = %d, want 1", shuffles)
	}
	replayCheck(t, e, cfg)
}

// TestKr1EachSearchOffersOnlyTheTypesWithCandidates (was
// TestEachSearchOffersOnlyTheTypesWithCandidates): with Forests and no
// Plains only the Forest group is offered (Max 1) and the answered Forest
// moves.
func TestKr1EachSearchOffersOnlyTheTypesWithCandidates(t *testing.T) {
	t.Parallel()
	e, cfg, id := kr1New(t, 151, kr1Sorcery("VergeSearch", kr1VergeBody), []string{kr1Forest, kr1Forest}, nil)
	addMana(t, e, 0, "B")
	ids := kr1Top(t, e, 0, "Forest", "Forest")
	d := kr1Cast(t, e, id)
	if d == nil || d.Min != 0 || d.Max != 1 || len(d.Options) != 2 {
		t.Fatalf("decision = %+v, want 0..1 over the Forest group only", d)
	}
	if d.Options[0].Group != "0" || d.Options[1].Group != "0" {
		t.Fatalf("groups = %q/%q, want one Forest group", d.Options[0].Group, d.Options[1].Group)
	}
	kr1Answer(t, e, decision.KChoose, 0, kr1OptIndex(t, d, ids[1]))
	if kr1Zone(e, ids[1]) != state.ZBattlefield || kr1Zone(e, ids[0]) != state.ZLibrary {
		t.Fatalf("zones = %s/%s, want only the answered Forest moved", kr1Zone(e, ids[0]), kr1Zone(e, ids[1]))
	}
	replayCheck(t, e, cfg)
}

// kr1EachGroups checks d offers exactly crea in group "0" and land in "1"
// (and never other), returning their option indices.
func kr1EachGroups(t *testing.T, d *decision.Decision, crea, land, other state.ObjID) (int, int) {
	t.Helper()
	if d.Min != 0 || d.Max != 2 || len(d.Options) != 2 {
		t.Fatalf("decision = %+v, want 0..2 over one option per listed type", d)
	}
	ci, li := -1, -1
	for _, o := range d.Options {
		switch o.Obj {
		case crea:
			ci = o.Index
			if o.Group != "0" {
				t.Fatalf("creature group = %q, want \"0\"", o.Group)
			}
		case land:
			li = o.Index
			if o.Group != "1" {
				t.Fatalf("land group = %q, want \"1\"", o.Group)
			}
		case other:
			t.Fatal("a card matching neither listed type was offered")
		}
	}
	if ci < 0 || li < 0 {
		t.Fatalf("options %+v do not cover both listed types", d.Options)
	}
	return ci, li
}

// TestKr1EachHandMoveAsksOneOfEachListedType (was
// TestEachHandMoveAsksOneOfEachListedType, Michelangelo Improvisers' shape):
// Optional$ True poses the confirm gate first, then a one-of-EACH pick
// (Max 2, one Group per type, the instant excluded), and the answered pair
// both enter the battlefield.
func TestKr1EachHandMoveAsksOneOfEachListedType(t *testing.T) {
	t.Parallel()
	body := "A:SP$ ChangeZone | Origin$ Hand | Destination$ Battlefield | ChangeType$ EACH Creature & Land | Optional$ True"
	e, cfg, id := kr1New(t, 152, kr1Sorcery("Improvise", body), []string{
		"Name:Grizzly\nManaCost:G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n", kr1Forest,
		"Name:Bolt\nManaCost:R\nTypes:Instant\nOracle:x\n"}, nil)
	addMana(t, e, 0, "B")
	kr1ClearHand(t, e, 0, id)
	crea := kr1Put(t, e, 0, "Grizzly", state.ZHand)
	land := kr1Put(t, e, 0, "Forest", state.ZHand)
	inst := kr1Put(t, e, 0, "Bolt", state.ZHand)
	kr1Cast(t, e, id)
	d := kr1Yes(t, e, "hand_move_confirm")
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "hand_move" {
		t.Fatalf("decision = %+v, want a KChoose hand_move", d)
	}
	ci, li := kr1EachGroups(t, d, crea, land, inst)
	kr1Answer(t, e, decision.KChoose, 0, ci, li)
	if kr1Zone(e, crea) != state.ZBattlefield || kr1Zone(e, land) != state.ZBattlefield || kr1Zone(e, inst) != state.ZHand {
		t.Fatalf("zones crea/land/inst = %s/%s/%s, want battlefield/battlefield/hand", kr1Zone(e, crea), kr1Zone(e, land), kr1Zone(e, inst))
	}
	replayCheck(t, e, cfg)
}

// TestKr1EachHiddenPickAsksOneOfEachListedType (was
// TestEachHiddenPickAsksOneOfEachListedType, Druidic Ritual's shape): from
// the graveyard, the confirm gate then a one-of-EACH pick, and the answered
// creature and land both return to hand.
func TestKr1EachHiddenPickAsksOneOfEachListedType(t *testing.T) {
	t.Parallel()
	body := "A:SP$ ChangeZone | Origin$ Graveyard | Destination$ Hand | Hidden$ True | ChangeType$ EACH Creature.YouOwn & Land.YouOwn | Optional$ True"
	e, cfg, id := kr1New(t, 153, kr1Sorcery("Ritual", body), []string{
		"Name:Zed\nManaCost:B\nTypes:Creature Zombie\nPT:2/2\nOracle:x\n", kr1Forest,
		"Name:Page\nManaCost:U\nTypes:Instant\nOracle:x\n"}, nil)
	addMana(t, e, 0, "B")
	crea := kr1Put(t, e, 0, "Zed", state.ZGraveyard)
	land := kr1Put(t, e, 0, "Forest", state.ZGraveyard)
	other := kr1Put(t, e, 0, "Page", state.ZGraveyard)
	kr1Cast(t, e, id)
	d := kr1Yes(t, e, "hidden_pick_confirm")
	if d == nil || d.ResumeKind != "hidden_pick" {
		t.Fatalf("decision = %+v, want a hidden_pick", d)
	}
	ci, li := kr1EachGroups(t, d, crea, land, other)
	kr1Answer(t, e, d.Kind, d.Player, ci, li)
	if kr1Zone(e, crea) != state.ZHand || kr1Zone(e, land) != state.ZHand || kr1Zone(e, other) != state.ZGraveyard {
		t.Fatalf("zones = %s/%s/%s, want hand/hand/graveyard", kr1Zone(e, crea), kr1Zone(e, land), kr1Zone(e, other))
	}
	replayCheck(t, e, cfg)
}

// TestKr1EachExplicitZeroChangeNumSelectsNothing (was the engine subcases of
// TestEachExplicitZeroChangeNumSelectsNothing): an explicit ChangeNum$ 0 on
// the EACH hand, library-search and hidden-pick paths poses no pick (the
// Optional$ confirm gate still runs first where present) and moves nothing.
func TestKr1EachExplicitZeroChangeNumSelectsNothing(t *testing.T) {
	t.Parallel()
	for i, tc := range []struct {
		name, body, confirm string
		zone                state.Zone
	}{
		{"hand", "Origin$ Hand | Destination$ Battlefield | Optional$ True", "hand_move_confirm", state.ZHand},
		{"library_search", "Origin$ Library | Destination$ Hand", "", state.ZLibrary},
		{"hidden_pick", "Origin$ Graveyard | Destination$ Hand | Hidden$ True | Optional$ True", "hidden_pick_confirm", state.ZGraveyard},
	} {
		tc := tc
		seed := uint64(154 + i)
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := "A:SP$ ChangeZone | " + tc.body + " | ChangeType$ EACH Creature & Land.Forest | ChangeNum$ 0"
			e, cfg, id := kr1New(t, seed, kr1Sorcery("ZeroProbe", body), []string{
				"Name:ZeroCreature\nManaCost:G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n", kr1Forest}, nil)
			addMana(t, e, 0, "B")
			kr1ClearHand(t, e, 0, id)
			var crea, land state.ObjID
			if tc.zone == state.ZLibrary {
				ids := kr1Top(t, e, 0, "ZeroCreature", "Forest")
				crea, land = ids[0], ids[1]
			} else {
				crea = kr1Put(t, e, 0, "ZeroCreature", tc.zone)
				land = kr1Put(t, e, 0, "Forest", tc.zone)
			}
			d := kr1Cast(t, e, id)
			if tc.confirm != "" {
				d = kr1Yes(t, e, tc.confirm)
			}
			if d != nil {
				t.Fatalf("zero-count EACH posed a choice: %+v", d)
			}
			for _, c := range []state.ObjID{crea, land} {
				if z := kr1Zone(e, c); z != tc.zone {
					t.Fatalf("candidate %d ended in %s, want it to remain in %s", c, z, tc.zone)
				}
			}
			if kr1HasNote(e, "unimplemented API ChangeZone") {
				t.Fatal("the ChangeZone handler did not run")
			}
			replayCheck(t, e, cfg)
		})
	}
}
