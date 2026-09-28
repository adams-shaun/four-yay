package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// These tests pin task copyp1: the registered DB$ CopyPermanent primitive, on
// real corpus cards, end to end. Every one asserts there is no
// "unimplemented API CopyPermanent" note anywhere in the log, and that the
// whole game replays byte-identically from its log.
func noUnimplementedCopyPermanent(t *testing.T, e *Engine) {
	t.Helper()
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unimplemented API") {
			t.Fatalf("log carries an unimplemented-API note: %q", ev.Text)
		}
	}
}

// findTokenCopyOf returns the battlefield token whose face is card's and
// whose id is none of the skips (the original, a previously found copy), or
// fails.
func findTokenCopyOf(t *testing.T, e *Engine, card *cards.Card, skip ...state.ObjID) state.ObjID {
	t.Helper()
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if !o.IsToken || o.Card != card {
			continue
		}
		gone := false
		for _, s := range skip {
			if o.ID == s {
				gone = true
				break
			}
		}
		if gone {
			continue
		}
		return o.ID
	}
	t.Fatalf("no token copy of %q on the battlefield", card.Faces[0].Name)
	return 0
}

// exiledTo asserts the object left the battlefield through an exile move and
// now rests in the CR 704.5d token tombstone (a token that leaves the
// battlefield ceases to exist, so the zone after the exile move is ZCeased).
func exiledTo(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	if got := e.G.Obj(id).Zone; got == state.ZBattlefield || got == state.ZStack {
		t.Fatalf("copy %d is still on the battlefield, want exiled", id)
	}
	for _, ev := range e.L.Events {
		if (ev.Kind == events.MoveZone && ev.Obj == id && ev.To == state.ZExile) ||
			ev.Kind == events.MyriadCleanup {
			return
		}
	}
	t.Fatalf("copy %d never moved to exile", id)
}

// TestFlamerushRiderCopiesTheOtherAttackerExiledAtEndOfCombat is the
// ticket's headline carrier end to end: attack with Flamerush Rider plus one
// other creature, answer the trigger's target ask with the other attacker,
// and the copy exists -- tapped, attacking the same defender, printed
// characteristics of the copied bear -- and is exiled when the combat ends.
func TestFlamerushRiderCopiesTheOtherAttackerExiledAtEndOfCombat(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	rider, ok := reg.Lookup("Flamerush Rider")
	if !ok {
		t.Fatal("missing corpus card Flamerush Rider")
	}
	bear, ok := reg.Lookup("Runeclaw Bear")
	if !ok {
		t.Fatal("missing corpus card Runeclaw Bear")
	}
	deck := append(mountainDeck(t, 40), rider, bear)
	e := New(seatZeroStart(Config{Seed: 42, Names: []string{"attacker", "bystander", "defender"},
		Decks: [][]*cards.Card{deck, deck, deck}}))
	e.Advance()
	id := crAbortMove(t, e, 0, "Flamerush Rider", state.ZBattlefield)
	mine := crAbortMove(t, e, 0, "Runeclaw Bear", state.ZBattlefield)

	e.pending = nil
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepDeclareAttackers})
	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("want attackers decision, got %+v", d)
	}
	var picks []int
	for _, o := range d.Options {
		if o.Player == 2 && (o.Obj == id || o.Obj == mine) {
			picks = append(picks, o.Index)
		}
	}
	if len(picks) != 2 {
		t.Fatalf("attack-against-seat-2 options for both attackers not offered: %+v", d.Options)
	}
	crAbortAnswer(t, e, "attack declaration", picks...)

	// Flamerush's trigger fires and its Execute sub (DB$ CopyPermanent |
	// ValidTgts$ Creature.attacking+Other) poses the target ask at
	// placement, exactly the Master-of-Diversion precedent.
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("want target choice at placement, got %+v", d)
	}
	if len(d.Options) != 1 || d.Options[0].Obj != mine {
		t.Fatalf("target options = %+v, want only the other attacking bear", d.Options)
	}
	crAbortAnswer(t, e, "Flamerush copy target", 0)
	passUntilStackEmpty(t, e, 20)

	// The copy: a tapped, attacking battlefield token with the bear's
	// printed characteristics, controller 0, flagged for end-of-combat
	// exile -- and no unimplemented-API note anywhere.
	cid := findTokenCopyOf(t, e, bear, mine)
	o := e.G.Obj(cid)
	if !o.Tapped || !o.IsAttacking || o.Controller != 0 || o.Zone != state.ZBattlefield {
		t.Fatalf("Flamerush copy: tapped=%v attacking=%v controller=%d zone=%s",
			o.Tapped, o.IsAttacking, o.Controller, o.Zone)
	}
	if o.Attacking != 2 {
		t.Fatalf("Flamerush copy attacks %d, want seat 2", o.Attacking)
	}
	noUnimplementedCopyPermanent(t, e)

	// Leaving the end-of-combat step exiles the copy (the IsMyriad
	// MyriadCleanup semantics the AtEOT$ ExileCombat bit rides); the CR
	// 704.5d tombstone is the token's final resting state.
	e.pending = nil
	e.setStep(state.StepEndCombat)
	e.pending = nil
	e.setStep(state.StepEnd)
	exiledTo(t, e, cid)
	if got := e.G.Obj(mine).Zone; got != state.ZBattlefield {
		t.Fatalf("the copied bear itself moved: %s", got)
	}
	noUnimplementedCopyPermanent(t, e)
}

// TestMoltenEchoesCopiesEnteringCreatureExilesAtNextEndStep pins the
// TriggeredCardLKICopy source and the AtEOT$ Exile delayed registration: a
// nontoken Bear entering under a Molten Echoes whose chosen type is Bear
// creates a token copy of it, which the next end step's delayed trigger
// exiles. The PumpKeywords$ Haste rider is now implemented: the copy mints
// WITH haste (PumpKeywords$ with no PumpDuration$ = for as long as the copy
// exists) and no per-call skip note names the family.
func TestMoltenEchoesCopiesEnteringCreatureExilesAtNextEndStep(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Molten Echoes")
	molten := searchMoveByName(t, e, "Molten Echoes", state.ZBattlefield)

	// Molten Echoes is seated directly on the battlefield, so it did not pass
	// through an entry boundary. Seed the choice with its event representation.
	e.emit(events.Event{Kind: events.Choose, Obj: molten, Counter: "type", Text: "Bear"})

	bear := searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)
	e.putTriggersOnStack()
	passUntilStackEmpty(t, e, 20)

	bearCard := e.G.Obj(bear).Card
	cid := findTokenCopyOf(t, e, bearCard, bear)
	o := e.G.Obj(cid)
	if o.Controller != 0 || o.Zone != state.ZBattlefield || o.Tapped || o.IsAttacking {
		t.Fatalf("Molten Echoes copy: controller=%d zone=%s tapped=%v attacking=%v",
			o.Controller, o.Zone, o.Tapped, o.IsAttacking)
	}
	// The PumpKeywords$ Haste rider is implemented: the copy's derived
	// keyword set carries Haste for as long as the copy exists (no
	// PumpDuration$), and no skip note named the family.
	if !e.HasKeyword(cid, "Haste") {
		t.Fatal("the PumpKeywords$ Haste rider did not reach the copy")
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "PumpKeywords$") {
			t.Fatalf("implemented PumpKeywords$ rider still named by a skip note: %q", ev.Text)
		}
	}
	noUnimplementedCopyPermanent(t, e)

	// AtEOT$ Exile: the delayed registration fires at the beginning of the
	// next end step and exiles the copy. The copy now HAS Haste (the
	// implemented PumpKeywords$ rider), so it is a legal attacker this turn
	// -- driveToStepAll exists precisely for the combat asks a live
	// creature introduces, declining to attack.
	driveToStepAll(t, e, e.G.Turn, e.G.Active, state.StepEnd)
	passUntilStackEmpty(t, e, 20)
	exiledTo(t, e, cid)
	if got := e.G.Obj(bear).Zone; got != state.ZBattlefield {
		t.Fatalf("the copied bear itself moved: %s", got)
	}
	noUnimplementedCopyPermanent(t, e)
	replayCheck(t, e, cfg)
}

// TestGrowingRanksPopulatesTheCreatureTokenYouControl pins the Populate$
// arm: at the beginning of its controller's upkeep, Growing Ranks copies a
// creature token you control. With exactly one eligible token the
// deterministic candidate IS the answer, so no stand-in note is emitted.
func TestGrowingRanksPopulatesTheCreatureTokenYouControl(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Growing Ranks")
	searchMoveByName(t, e, "Growing Ranks", state.ZBattlefield)
	bear := searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)

	// Seed one creature token to populate from: a battlefield token copy of
	// the bear (the CardToken mint encore uses).
	want := e.G.NextID
	e.emit(events.Event{Kind: events.CardToken, Obj: bear, Player: 0})
	tok := e.G.Obj(want)
	if tok == nil || tok.Zone != state.ZBattlefield || !tok.IsToken {
		t.Fatalf("seeded token: %+v", tok)
	}

	// Growing Ranks' upkeep trigger: populate, no target ask, one copy of
	// the only eligible creature token. Seat 0's next upkeep is two global
	// turns away (seat 1's whole turn passes in between).
	driveToStep(t, e, e.G.Turn+2, 0, state.StepUpkeep)
	passUntilStackEmpty(t, e, 20)
	bearCard := e.G.Obj(bear).Card
	cid := findTokenCopyOf(t, e, bearCard, bear, want)
	if cid == want {
		t.Fatal("populate copied the seed token onto itself instead of minting a new token")
	}
	if got := e.G.Obj(cid).Controller; got != 0 {
		t.Fatalf("populated token controller = %d, want 0", got)
	}
	// The populated copy is a real battlefield permanent (CR 706.2):
	// IsToken+IsCopy+ZBattlefield must not read as ephemeral, or the
	// projection hides a token its controller controls.
	if e.G.Obj(cid).Ephemeral() {
		t.Fatalf("populated token %d reports Ephemeral(); a battlefield copy is a real permanent", cid)
	}
	if v := view.Project(e.G, nil, 0, nil); !viewShowsObject(v.Players[0].Battlefield, cid) {
		t.Fatalf("populated token %d is missing from its controller's battlefield view", cid)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "Populate$") {
			t.Fatalf("no stand-in note is owed with exactly one eligible token: %q", ev.Text)
		}
	}
	noUnimplementedCopyPermanent(t, e)
	replayCheck(t, e, cfg)
}

// TestRedoubledStormsingerCopiesEachTokenThatEnteredThisTurn pins the exact
// shape the filed report named end to end on its real corpus card: the attack
// trigger's `DB$ CopyPermanent | Defined$ Valid
// Creature.token+YouCtrl+ThisTurnEntered | TokenTapped$ True | TokenAttacking$
// True | AtEOT$ Sacrifice`. The bare battlefield "Defined$ Valid <filter>"
// form is the load-bearing part: effCopyPermanent resolves Defined$ through
// knownDefinedTargets (FAIL-CLOSED, never a silent source fallback), so before
// definedSpec recognised the bare Valid form every such line emitted
// "CopyPermanent source ... is not resolvable; no copy" and minted nothing.
//
// The oracle text is "for each creature token you control that entered this
// turn, create a tapped and attacking token that's a copy of that token. At
// the beginning of the next end step, sacrifice those tokens": this test seeds
// TWO such tokens and asserts BOTH are copied (the whole sweep, not the first),
// the copies enter tapped and attacking the attacked seat, and the next end
// step sacrifices exactly the copies while leaving the originals.
func TestRedoubledStormsingerCopiesEachTokenThatEnteredThisTurn(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Redoubled Stormsinger")
	st := searchMoveByName(t, e, "Redoubled Stormsinger", state.ZBattlefield)
	bear := searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)

	// Seat 0's turn 2: summoning sickness is gone, so the Stormsinger can
	// attack; the two seed tokens enter DURING this turn so the
	// ThisTurnEntered predicate admits them (a token seeded on turn 1 would
	// fall out of the filter, which is the card's own timing).
	e.pending = nil
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	seedA := e.G.NextID
	e.emit(events.Event{Kind: events.CardToken, Obj: bear, Player: 0})
	seedB := e.G.NextID
	e.emit(events.Event{Kind: events.CardToken, Obj: bear, Player: 0})
	for _, id := range []state.ObjID{seedA, seedB} {
		if o := e.G.Obj(id); o == nil || !o.IsToken || o.Zone != state.ZBattlefield {
			t.Fatalf("seed token %d not on the battlefield: %+v", id, o)
		}
	}

	e.emit(events.Event{Kind: events.StepChange, Step: state.StepDeclareAttackers})
	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("want attackers decision, got %+v", d)
	}
	var picks []int
	for _, o := range d.Options {
		if o.Obj == st {
			picks = append(picks, o.Index)
		}
	}
	if len(picks) == 0 {
		t.Fatalf("Redoubled Stormsinger not offered as an attacker: %+v", d.Options)
	}
	crAbortAnswer(t, e, "attack declaration", picks...)
	passUntilStackEmpty(t, e, 30)

	// The trigger must have resolved the unresolvable-source note, and it
	// must have minted one copy per eligible token (2), both tapped and
	// attacking the attacked seat.
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "is not resolvable") {
			t.Fatalf("bare Defined$ Valid was not recognised: %q", ev.Text)
		}
	}
	noUnimplementedCopyPermanent(t, e)
	bearCard := e.G.Obj(bear).Card
	copyA := findTokenCopyOf(t, e, bearCard, seedA, seedB)
	copyB := findTokenCopyOf(t, e, bearCard, seedA, seedB, copyA)
	for _, cid := range []state.ObjID{copyA, copyB} {
		o := e.G.Obj(cid)
		if !o.Tapped || !o.IsAttacking || o.Controller != 0 || o.Zone != state.ZBattlefield {
			t.Fatalf("copy %d: tapped=%v attacking=%v controller=%d zone=%s",
				cid, o.Tapped, o.IsAttacking, o.Controller, o.Zone)
		}
		if o.Attacking != 1 {
			t.Fatalf("copy %d attacks %d, want seat 1", cid, o.Attacking)
		}
	}
	if len(e.G.Zone(state.ZBattlefield, 0)) == 0 {
		t.Fatal("seat 0's battlefield is empty after the copies entered")
	}
	replayCheck(t, e, cfg)

	// AtEOT$ Sacrifice: the next end step sacrifices exactly the copies; the
	// two original tokens (which are NOT the delayed registration's source)
	// stay on the battlefield. TWO delayed triggers fire at once for the same
	// controller, so the engine poses a CR 603.3b trigger-order ask before
	// pushing them -- answer it in offered order, then drain.
	driveToStep(t, e, e.G.Turn, 0, state.StepEnd)
	for e.putTriggersOnStack() {
		answerTriggerOrders(t, e)
		passUntilStackEmpty(t, e, 30)
	}
	passUntilStackEmpty(t, e, 30)
	for _, cid := range []state.ObjID{copyA, copyB} {
		if z := e.G.Obj(cid).Zone; z == state.ZBattlefield {
			t.Fatalf("copy %d survived the end-step sacrifice (zone %s)", cid, z)
		}
	}
	for _, id := range []state.ObjID{seedA, seedB} {
		if z := e.G.Obj(id).Zone; z != state.ZBattlefield {
			t.Fatalf("original seed token %d was sacrificed (zone %s), want it kept", id, z)
		}
	}
	noUnimplementedCopyPermanent(t, e)
	replayCheck(t, e, cfg)
}
