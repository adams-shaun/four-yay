package rules

// Kernel-era restorations of the effects-package ForgetOtherRemembered$ /
// ForgetChosen$ tests the W3 legacy removal deleted (forget_continuation,
// forget_fetch_empty, forget_fetch_owner_confirm, forget_multiowner_hand,
// forget_multiowner, forget_paths, forget_remembered, forget_search). The
// legacy tests rebuilt a fresh Ctx from each decision's resume ride; under
// the kernel a resolution re-executes from its checkpoint, so what is left
// to pin is the BEHAVIOUR: each ask offers the pre-clear remembered
// candidates, every answer is honoured, the source's persistent memory ends
// as Forge leaves it, and the clear is recorded exactly once.
//
// The source is kr1Relic (a tap-ability artifact on seat 0's battlefield),
// seeded with its remembered set by a logged Choose "remembered".

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// kr1Creature is a vanilla creature fixture named name.
func kr1Creature(name string) string {
	return "Name:" + name + "\nManaCost:G\nTypes:Creature\nPT:2/2\nOracle:x\n"
}

func kr1Creatures(names ...string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		if strings.HasPrefix(n, "Name:") {
			out = append(out, n) // a raw fixture source
			continue
		}
		out = append(out, kr1Creature(n))
	}
	return out
}

// kr1ForgetBoard builds the relic game (seat 0 holds names0, seat 1
// names1 as vanilla creatures), puts the relic on the battlefield and
// empties both hands, ready for the test to place its cards.
func kr1ForgetBoard(t *testing.T, seed uint64, body string, names0, names1 []string) (*Engine, Config, state.ObjID) {
	t.Helper()
	e, cfg, id := kr1New(t, seed, kr1Relic(body), kr1Creatures(names0...), kr1Creatures(names1...))
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	kr1ClearHand(t, e, 0, 0)
	kr1ClearHand(t, e, 1, 0)
	return e, cfg, id
}

// kr1ClearCount counts src's clear-remembered Choose events.
func kr1ClearCount(e *Engine, src state.ObjID) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Choose && ev.Counter == "clear-remembered" && ev.Obj == src {
			n++
		}
	}
	return n
}

func kr1SameIDs(t *testing.T, what string, got []state.ObjID, want ...state.ObjID) {
	t.Helper()
	if !sameObjIDs(got, want) {
		t.Fatalf("%s = %v, want %v", what, got, want)
	}
}

// kr1Want asserts each id is in zone z.
func kr1Want(t *testing.T, e *Engine, z state.Zone, ids ...state.ObjID) {
	t.Helper()
	for _, id := range ids {
		if got := kr1Zone(e, id); got != z {
			t.Fatalf("object %d (%s) zone = %s, want %s", id, e.G.Obj(id).Face().Name, got, z)
		}
	}
}

// kr1Opt returns the option index for kind k ("yes"/"no") or fatal.
func kr1Opt(t *testing.T, d *decision.Decision, k string) int {
	t.Helper()
	for _, o := range d.Options {
		if o.Kind == k {
			return o.Index
		}
	}
	t.Fatalf("no %q option on %+v", k, d)
	return -1
}

// TestKr1HandMoveForgetOtherRememberedOwnersAsk (was
// TestHandMoveForgetOtherRememberedOwnersAsk): the owner-selected hand walk
// (DefinedPlayer$ RememberedOwner). Owner A's answer settles and clears the
// set, yet owner B's ask still offers B's two pre-clear candidates and B's
// answer moves; the source ends remembering exactly the two moved cards and
// the clear is recorded once.
func TestKr1HandMoveForgetOtherRememberedOwnersAsk(t *testing.T) {
	t.Parallel()
	e, cfg, src := kr1ForgetBoard(t, 200,
		"ChangeZone | Origin$ Hand | Destination$ Exile | DefinedPlayer$ RememberedOwner | ChangeType$ Creature.IsRemembered | ChangeNum$ 1 | Mandatory$ True | RememberChanged$ True | ForgetOtherRemembered$ True",
		[]string{"First Creature", "Extra A", "Stale Creature"}, []string{"Second Creature", "Extra B"})
	first := kr1Put(t, e, 0, "First Creature", state.ZHand)
	extraA := kr1Put(t, e, 0, "Extra A", state.ZHand)
	second := kr1Put(t, e, 1, "Second Creature", state.ZHand)
	extraB := kr1Put(t, e, 1, "Extra B", state.ZHand)
	stale := kr1Put(t, e, 0, "Stale Creature", state.ZExile)
	d := kr1Activate(t, e, src, first, extraA, second, extraB, stale)
	if d == nil || d.ResumeKind != "hand_move" || len(d.Options) != 2 {
		t.Fatalf("owner A not offered its two remembered candidates: %+v", d)
	}
	d = kr1Pick(t, e, kr1OptIndex(t, d, first))
	kr1Want(t, e, state.ZExile, first)
	if d == nil || d.ResumeKind != "hand_move" || len(d.Options) != 2 || d.Options[0].Obj != second || d.Options[1].Obj != extraB {
		t.Fatalf("owner B's remembered candidates lost after A's settle: %+v", d)
	}
	if d = kr1Pick(t, e, 0); d != nil {
		t.Fatalf("unexpected further ask %+v", d)
	}
	kr1Want(t, e, state.ZExile, first, second)
	kr1Want(t, e, state.ZHand, extraA, extraB)
	kr1SameIDs(t, "persistent remembered", kr1Remembered(e, src), first, second)
	if n := kr1ClearCount(e, src); n != 1 {
		t.Fatalf("clear-remembered recorded %d times, want 1", n)
	}
	replayCheck(t, e, cfg)
}

// TestKr1HandMoveForgetOtherRememberedHandAsk (was
// TestHandMoveForgetOtherRememberedHandAsk and
// TestChangeZoneHandForgetOtherRememberedAnsweredSelector): the whole-hand
// shape offers both remembered hand cards; the answered one is exiled, the
// other stays, the source remembers only the moved card, the clear is once.
func TestKr1HandMoveForgetOtherRememberedHandAsk(t *testing.T) {
	t.Parallel()
	e, cfg, src := kr1ForgetBoard(t, 201,
		"ChangeZone | Origin$ Hand | Destination$ Exile | ChangeType$ Card.IsRemembered | ChangeNum$ 1 | Mandatory$ True | RememberChanged$ True | ForgetOtherRemembered$ True",
		[]string{"Hand Pick", "Other Hand Pick", "Stale"}, nil)
	picked := kr1Put(t, e, 0, "Hand Pick", state.ZHand)
	other := kr1Put(t, e, 0, "Other Hand Pick", state.ZHand)
	stale := kr1Put(t, e, 0, "Stale", state.ZExile)
	d := kr1Activate(t, e, src, picked, other, stale)
	if d == nil || d.ResumeKind != "hand_move" || len(d.Options) != 2 {
		t.Fatalf("hand ask missing the remembered options: %+v", d)
	}
	if d = kr1Pick(t, e, kr1OptIndex(t, d, picked)); d != nil {
		t.Fatalf("unexpected further ask %+v", d)
	}
	kr1Want(t, e, state.ZExile, picked)
	kr1Want(t, e, state.ZHand, other)
	kr1SameIDs(t, "persistent remembered", kr1Remembered(e, src), picked)
	if n := kr1ClearCount(e, src); n != 1 {
		t.Fatalf("clear-remembered recorded %d times, want 1", n)
	}
	replayCheck(t, e, cfg)
}

// TestKr1DigForgetOtherRememberedAsk (was TestDigForgetOtherRememberedAsk):
// a two-player Dig over Creature.IsRemembered: player 0's answered take
// clears the set, and player 1's window ask still offers its pre-clear
// remembered cards; both answered takes are exiled, the untaken stay in the
// libraries, the clear is once.
func TestKr1DigForgetOtherRememberedAsk(t *testing.T) {
	t.Parallel()
	e, cfg, src := kr1ForgetBoard(t, 202,
		"Dig | DigNum$ 2 | ChangeNum$ 1 | ChangeValid$ Creature.IsRemembered | DestinationZone$ Exile | Defined$ You & Opponent | RememberChanged$ True | ForgetOtherRemembered$ True",
		[]string{"First Dug", "Second Dug", "Stale"}, []string{"Third Dug", "Fourth Dug"})
	stale := kr1Put(t, e, 0, "Stale", state.ZExile)
	w0 := kr1Top(t, e, 0, "First Dug", "Second Dug")
	w1 := kr1Top(t, e, 1, "Third Dug", "Fourth Dug")
	d := kr1Activate(t, e, src, w0[0], w0[1], w1[0], w1[1], stale)
	if d == nil || d.ResumeKind != "dig" || len(d.Options) != 2 {
		t.Fatalf("player 0 not offered its two remembered window cards: %+v", d)
	}
	d = kr1Pick(t, e, kr1OptIndex(t, d, w0[0]))
	if d == nil || d.ResumeKind != "dig" || len(d.Options) != 2 || d.Options[0].Obj != w1[0] {
		t.Fatalf("player 1's remembered window lost after player 0's answer: %+v", d)
	}
	if d = kr1Pick(t, e, 0); d != nil {
		t.Fatalf("unexpected further ask %+v", d)
	}
	kr1Want(t, e, state.ZExile, w0[0], w1[0])
	kr1Want(t, e, state.ZLibrary, w0[1], w1[1])
	if n := kr1ClearCount(e, src); n != 1 {
		t.Fatalf("clear-remembered recorded %d times, want 1", n)
	}
	replayCheck(t, e, cfg)
}

// TestKr1DigUntilForgetOtherRememberedAsk (was
// TestDigUntilForgetOtherRememberedAsk): the found-move election over a
// Creature.IsRemembered walk; accepting moves the found card to hand
// and RememberFound$ makes it the resolution's Remembered (a Defined$
// Remembered sub moves exactly it on); the filler below it is untouched and
// the clear is once.
func TestKr1DigUntilForgetOtherRememberedAsk(t *testing.T) {
	t.Parallel()
	e, cfg, src := kr1ForgetBoard(t, 203,
		"DigUntil | Valid$ Creature.IsRemembered | OptionalFoundMove$ True | FoundDestination$ Hand | RememberFound$ True | ForgetOtherRemembered$ True | SubAbility$ DBRead\n"+
			"SVar:DBRead:DB$ ChangeZone | Defined$ Remembered | Origin$ Hand | Destination$ Graveyard",
		[]string{"Found Creature", "Stale"}, nil)
	stale := kr1Put(t, e, 0, "Stale", state.ZExile)
	ids := kr1Top(t, e, 0, "Found Creature", "Mountain")
	d := kr1Activate(t, e, src, ids[0], stale)
	if d == nil || d.ResumeKind != "diguntil_move" {
		t.Fatalf("found-move election not posed: %+v", d)
	}
	if d = kr1Pick(t, e, kr1Opt(t, d, "yes")); d != nil {
		t.Fatalf("unexpected further ask %+v", d)
	}
	// RememberFound$ replaces the resolution's Remembered with the found
	// card, so the DBRead sub (Defined$ Remembered, hand -> graveyard)
	// moves exactly it on.
	kr1Want(t, e, state.ZGraveyard, ids[0])
	kr1Want(t, e, state.ZExile, stale)
	if lib := e.G.Zone(state.ZLibrary, 0); len(lib) == 0 || lib[0] != ids[1] {
		t.Fatalf("the filler below the found card was disturbed: library top %v", lib[:1])
	}
	if n := kr1ClearCount(e, src); n != 1 {
		t.Fatalf("clear-remembered recorded %d times, want 1", n)
	}
	replayCheck(t, e, cfg)
}

// TestKr1ChooseCardForgetOtherRememberedAsk (was
// TestChooseCardForgetOtherRememberedAsk): chooser 0's answer clears the
// set, chooser 1's pool still holds its pre-clear remembered creature, and
// the source ends remembering both picks with one clear.
func TestKr1ChooseCardForgetOtherRememberedAsk(t *testing.T) {
	t.Parallel()
	e, cfg, src := kr1ForgetBoard(t, 204,
		"ChooseCard | Defined$ You & Opponent | Amount$ 1 | Choices$ Creature.IsRemembered | ChoiceZone$ Graveyard | Mandatory$ True | RememberChosen$ True | ForgetOtherRemembered$ True",
		[]string{"First Kept", "Stale"}, []string{"Second Kept"})
	first := kr1Put(t, e, 0, "First Kept", state.ZGraveyard)
	kr1Put(t, e, 0, "Mountain", state.ZGraveyard)
	second := kr1Put(t, e, 1, "Second Kept", state.ZGraveyard)
	stale := kr1Put(t, e, 0, "Stale", state.ZExile)
	d := kr1Activate(t, e, src, first, second, stale)
	if d == nil || d.ResumeKind != "choice" || len(d.Options) == 0 {
		t.Fatalf("chooser 0 not asked: %+v", d)
	}
	d = kr1Pick(t, e, kr1OptIndex(t, d, first))
	if d == nil || len(d.Options) == 0 || d.Options[len(d.Options)-1].Obj != second {
		t.Fatalf("chooser 1's remembered pool lost after chooser 0's answer: %+v", d)
	}
	if d = kr1Pick(t, e, kr1OptIndex(t, d, second)); d != nil {
		t.Fatalf("unexpected further ask %+v", d)
	}
	kr1SameIDs(t, "persistent remembered", kr1Remembered(e, src), first, second)
	if n := kr1ClearCount(e, src); n != 1 {
		t.Fatalf("clear-remembered recorded %d times, want 1", n)
	}
	replayCheck(t, e, cfg)
}

const kr1OptionalHandFetch = "ChangeZone | Origin$ Hand | Destination$ Exile | DefinedPlayer$ You | ChangeType$ Creature.IsRemembered | ChangeNum$ 1 | Optional$ True | ForgetOtherRemembered$ True"

// TestKr1ChangeZoneHandOwnersForgetOtherRememberedOptionalConfirm (was
// TestChangeZoneHandOwnersForgetOtherRememberedOptionalConfirm): Optional$
// asks a yes/no BEFORE the pick and before any clear. Declining keeps the
// memory and picks nothing; accepting clears before the pick, whether the
// pick then takes nothing (the card stays) or takes the card (exiled).
func TestKr1ChangeZoneHandOwnersForgetOtherRememberedOptionalConfirm(t *testing.T) {
	t.Parallel()
	for i, branch := range []string{"decline", "accept pick-none", "accept pick"} {
		branch := branch
		seed := uint64(205 + i)
		t.Run(branch, func(t *testing.T) {
			t.Parallel()
			e, cfg, src := kr1ForgetBoard(t, seed, kr1OptionalHandFetch, []string{"Remembered"}, nil)
			card := kr1Put(t, e, 0, "Remembered", state.ZHand)
			d := kr1Activate(t, e, src, card)
			if d == nil || d.ResumeKind != "hand_move_confirm" || len(d.Options) != 2 || d.Options[0].Kind != "yes" || d.Options[1].Kind != "no" {
				t.Fatalf("the optional hand move did not ask yes/no first: %+v", d)
			}
			kr1SameIDs(t, "memory at the confirmation", kr1Remembered(e, src), card)
			if branch == "decline" {
				if d = kr1Pick(t, e, 1); d != nil {
					t.Fatalf("a declined confirmation posted a pick: %+v", d)
				}
				kr1Want(t, e, state.ZHand, card)
				kr1SameIDs(t, "memory after decline", kr1Remembered(e, src), card)
				replayCheck(t, e, cfg)
				return
			}
			d = kr1Pick(t, e, 0)
			if d == nil || d.ResumeKind != "hand_move" || len(d.Options) != 1 || d.Options[0].Obj != card {
				t.Fatalf("accepted confirmation did not post the card pick: %+v", d)
			}
			kr1SameIDs(t, "memory at the pick (cleared on accept)", kr1Remembered(e, src))
			if branch == "accept pick-none" {
				kr1Pick(t, e)
				kr1Want(t, e, state.ZHand, card)
			} else {
				kr1Pick(t, e, 0)
				kr1Want(t, e, state.ZExile, card)
			}
			kr1SameIDs(t, "memory after the pick", kr1Remembered(e, src))
			replayCheck(t, e, cfg)
		})
	}
}

// TestKr1ChangeZoneForgetOtherRememberedFetchEmpty (was
// TestChangeZoneForgetOtherRememberedFetchEmpty): the remembered card is not
// in the hand, so the pool is empty. Optional$ still asks whether to
// proceed: decline keeps the memory, accept clears it with no pick; a
// Mandatory$ empty fetch asks nothing and clears.
func TestKr1ChangeZoneForgetOtherRememberedFetchEmpty(t *testing.T) {
	t.Parallel()
	mandatory := "ChangeZone | Origin$ Hand | Destination$ Exile | DefinedPlayer$ You | ChangeType$ Creature.IsRemembered | ChangeNum$ 1 | Mandatory$ True | ForgetOtherRemembered$ True"
	for i, branch := range []string{"optional decline", "optional accept", "mandatory"} {
		branch := branch
		seed := uint64(208 + i)
		t.Run(branch, func(t *testing.T) {
			t.Parallel()
			body := kr1OptionalHandFetch
			if branch == "mandatory" {
				body = mandatory
			}
			e, cfg, src := kr1ForgetBoard(t, seed, body, []string{"Remembered", "Not Remembered"}, nil)
			card := kr1Put(t, e, 0, "Remembered", state.ZExile)
			other := kr1Put(t, e, 0, "Not Remembered", state.ZHand)
			d := kr1Activate(t, e, src, card)
			switch branch {
			case "mandatory":
				if d != nil {
					t.Fatalf("a mandatory empty pool posted a decision: %+v", d)
				}
				kr1SameIDs(t, "memory", kr1Remembered(e, src))
				if kr1HasNote(e, "unimplemented API ChangeZone") {
					t.Fatal("the ChangeZone handler did not run")
				}
			default:
				if d == nil || d.ResumeKind != "hand_move_confirm" {
					t.Fatalf("an optional empty pool must ask whether to proceed: %+v", d)
				}
				if branch == "optional decline" {
					if d = kr1Pick(t, e, kr1Opt(t, d, "no")); d != nil {
						t.Fatalf("unexpected ask %+v", d)
					}
					kr1SameIDs(t, "memory after decline", kr1Remembered(e, src), card)
				} else {
					if d = kr1Pick(t, e, kr1Opt(t, d, "yes")); d != nil {
						t.Fatalf("an accepted empty fetch posted a pick: %+v", d)
					}
					kr1SameIDs(t, "memory after accept", kr1Remembered(e, src))
				}
			}
			kr1Want(t, e, state.ZExile, card)
			kr1Want(t, e, state.ZHand, other)
			replayCheck(t, e, cfg)
		})
	}
}

// TestKr1ChangeZoneDefinedForgetOtherRememberedFetchEmpty (was
// TestChangeZoneDefinedForgetOtherRememberedFetchEmpty): an Optional$
// Defined$ Remembered library fetch whose card is in exile asks yes/no;
// declining keeps the memory, accepting clears it and moves nothing.
func TestKr1ChangeZoneDefinedForgetOtherRememberedFetchEmpty(t *testing.T) {
	t.Parallel()
	for i, accept := range []bool{false, true} {
		accept := accept
		seed := uint64(211 + i)
		name := "decline keeps"
		if accept {
			name = "accept clears with no move"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e, cfg, src := kr1ForgetBoard(t, seed,
				"ChangeZone | Origin$ Library | Destination$ Battlefield | Defined$ Remembered | Optional$ True | ForgetOtherRemembered$ True",
				[]string{"Remembered"}, nil)
			card := kr1Put(t, e, 0, "Remembered", state.ZExile)
			d := kr1Activate(t, e, src, card)
			if d == nil || d.ResumeKind != "defined_library_optional" {
				t.Fatalf("the optional defined fetch did not ask yes/no: %+v", d)
			}
			if accept {
				kr1Pick(t, e, kr1Opt(t, d, "yes"))
				kr1SameIDs(t, "memory after accept", kr1Remembered(e, src))
			} else {
				kr1Pick(t, e, kr1Opt(t, d, "no"))
				kr1SameIDs(t, "memory after decline", kr1Remembered(e, src), card)
			}
			kr1Want(t, e, state.ZExile, card)
			replayCheck(t, e, cfg)
		})
	}
}

// TestKr1ChangeZoneHandOwnersForgetOtherRememberedOptionalConfirmPerOwner
// (was TestChangeZoneHandOwnersForgetOtherRememberedOptionalConfirmPerOwner):
// each remembered owner gets its own confirmation; owner 0 declining keeps
// the memory and does not consume owner 1's; owner 1 accepting its empty
// fetch clears the memory and moves nothing.
func TestKr1ChangeZoneHandOwnersForgetOtherRememberedOptionalConfirmPerOwner(t *testing.T) {
	t.Parallel()
	e, cfg, src := kr1ForgetBoard(t, 213,
		"ChangeZone | Origin$ Hand | Destination$ Exile | DefinedPlayer$ RememberedOwner | ChangeType$ Creature.IsRemembered | ChangeNum$ 1 | Optional$ True | ForgetOtherRemembered$ True",
		[]string{"Owner zero"}, []string{"Owner one"})
	first := kr1Put(t, e, 0, "Owner zero", state.ZExile)
	second := kr1Put(t, e, 1, "Owner one", state.ZExile)
	d := kr1Activate(t, e, src, first, second)
	if d == nil || d.ResumeKind != "hand_move_confirm" || d.Player != 0 {
		t.Fatalf("owner 0 did not get the first confirmation: %+v", d)
	}
	d = kr1Pick(t, e, kr1Opt(t, d, "no"))
	if d == nil || d.ResumeKind != "hand_move_confirm" || d.Player != 1 {
		t.Fatalf("owner 1's confirmation was skipped after owner 0 declined: %+v", d)
	}
	kr1SameIDs(t, "memory after owner 0's decline", kr1Remembered(e, src), first, second)
	if d = kr1Pick(t, e, kr1Opt(t, d, "yes")); d != nil {
		t.Fatalf("owner 1's accepted empty fetch posted a pick: %+v", d)
	}
	kr1SameIDs(t, "memory after owner 1's accept", kr1Remembered(e, src))
	kr1Want(t, e, state.ZExile, first, second)
	replayCheck(t, e, cfg)
}

// TestKr1ChangeZoneHandOwnersForgetOtherRememberedResume (was
// TestChangeZoneHandOwnersForgetOtherRememberedResume): Optional$, owner
// selector RememberedOwner, NO RememberChanged$. Owner 0 confirms and
// exiles its card (clearing the memory to nothing); owner 1 must still be
// confirmed and offered its pre-clear remembered card, which then moves;
// the unremembered creature in owner 1's hand stays.
func TestKr1ChangeZoneHandOwnersForgetOtherRememberedResume(t *testing.T) {
	t.Parallel()
	e, cfg, src := kr1ForgetBoard(t, 214,
		"ChangeZone | Origin$ Hand | Destination$ Exile | DefinedPlayer$ RememberedOwner | ChangeType$ Creature.IsRemembered | ChangeNum$ 1 | Optional$ True | ForgetOtherRemembered$ True",
		[]string{"First", "Stale"}, []string{"Second", "Unremembered"})
	first := kr1Put(t, e, 0, "First", state.ZHand)
	second := kr1Put(t, e, 1, "Second", state.ZHand)
	unrem := kr1Put(t, e, 1, "Unremembered", state.ZHand)
	stale := kr1Put(t, e, 0, "Stale", state.ZExile)
	d := kr1Activate(t, e, src, first, second, stale)
	if d == nil || d.ResumeKind != "hand_move_confirm" || len(d.Options) != 2 {
		t.Fatalf("first ask is not the Optional$ confirmation: %+v", d)
	}
	d = kr1Pick(t, e, kr1Opt(t, d, "yes"))
	if d == nil || d.ResumeKind != "hand_move" || len(d.Options) != 1 || d.Options[0].Obj != first {
		t.Fatalf("owner 0 not offered its remembered card after confirm: %+v", d)
	}
	d = kr1Pick(t, e, 0)
	kr1Want(t, e, state.ZExile, first)
	if d == nil || d.ResumeKind != "hand_move_confirm" {
		t.Fatalf("owner 1's confirmation lost after owner 0's move: %+v", d)
	}
	d = kr1Pick(t, e, kr1Opt(t, d, "yes"))
	if d == nil || d.ResumeKind != "hand_move" || len(d.Options) != 1 || d.Options[0].Obj != second {
		t.Fatalf("owner 1's remembered card not offered after its confirm: %+v", d)
	}
	if d = kr1Pick(t, e, 0); d != nil {
		t.Fatalf("unexpected further ask %+v", d)
	}
	kr1Want(t, e, state.ZExile, first, second)
	kr1Want(t, e, state.ZHand, unrem)
	replayCheck(t, e, cfg)
}

// TestKr1ChangeZoneForgetOtherRememberedMultiOwner (was
// TestChangeZoneHiddenForgetOtherRememberedMultiOwner and
// TestChangeZoneSearchForgetOtherRememberedMultiOwner): the hidden-pick
// (graveyard) and library-search walks over two remembered owners offer
// each owner its pre-clear remembered card even after the first move
// clears the memory; both move and the source remembers both.
func TestKr1ChangeZoneForgetOtherRememberedMultiOwner(t *testing.T) {
	t.Parallel()
	for i, tc := range []struct {
		name, body string
		from, to   state.Zone
	}{
		{"hidden graveyard", "ChangeZone | Hidden$ True | Origin$ Graveyard | Destination$ Hand | DefinedPlayer$ RememberedOwner | ChangeType$ Creature.IsRemembered | ChangeNum$ 1 | Mandatory$ True | RememberChanged$ True | ForgetOtherRemembered$ True", state.ZGraveyard, state.ZHand},
		{"library search", "ChangeZone | Origin$ Library | Destination$ Exile | DefinedPlayer$ RememberedOwner | ChangeType$ Creature.IsRemembered | ChangeNum$ 1 | Mandatory$ True | RememberChanged$ True | ForgetOtherRemembered$ True", state.ZLibrary, state.ZExile},
	} {
		tc := tc
		seed := uint64(215 + i)
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e, cfg, src := kr1ForgetBoard(t, seed, tc.body, []string{"First", "Stale"}, []string{"Second"})
			var first, second state.ObjID
			if tc.from == state.ZLibrary {
				first, second = kr1Top(t, e, 0, "First")[0], kr1Top(t, e, 1, "Second")[0]
			} else {
				first, second = kr1Put(t, e, 0, "First", tc.from), kr1Put(t, e, 1, "Second", tc.from)
			}
			stale := kr1Put(t, e, 0, "Stale", state.ZExile)
			d := kr1Activate(t, e, src, first, second, stale)
			if d == nil || len(d.Options) != 1 || d.Options[0].Obj != first {
				t.Fatalf("owner 0 not offered its remembered card: %+v", d)
			}
			d = kr1Pick(t, e, 0)
			kr1Want(t, e, tc.to, first)
			if d == nil || len(d.Options) != 1 || d.Options[0].Obj != second {
				t.Fatalf("owner 1's remembered card lost after the first move: %+v", d)
			}
			if d = kr1Pick(t, e, 0); d != nil {
				t.Fatalf("unexpected further ask %+v", d)
			}
			kr1Want(t, e, tc.to, second)
			kr1SameIDs(t, "persistent remembered", kr1Remembered(e, src), first, second)
			replayCheck(t, e, cfg)
		})
	}
}

// TestKr1ChangeZoneHiddenForgetOtherRememberedSelector (was
// TestChangeZoneHiddenForgetOtherRememberedSelector): a hidden pick over
// Graveyard,Exile offers both remembered candidates; the answer returns the
// graveyard one and the source remembers only it.
func TestKr1ChangeZoneHiddenForgetOtherRememberedSelector(t *testing.T) {
	t.Parallel()
	e, cfg, src := kr1ForgetBoard(t, 217,
		"ChangeZone | Hidden$ True | Origin$ Graveyard,Exile | Destination$ Hand | ChangeType$ Creature.IsRemembered | ChangeNum$ 1 | RememberChanged$ True | ForgetOtherRemembered$ True",
		[]string{"Hidden Pick", "Stale"}, nil)
	picked := kr1Put(t, e, 0, "Hidden Pick", state.ZGraveyard)
	stale := kr1Put(t, e, 0, "Stale", state.ZExile)
	d := kr1Activate(t, e, src, picked, stale)
	if d == nil || len(d.Options) != 2 {
		t.Fatalf("hidden ask missing the remembered options: %+v", d)
	}
	kr1Pick(t, e, kr1OptIndex(t, d, picked))
	kr1Want(t, e, state.ZHand, picked)
	kr1SameIDs(t, "persistent remembered", kr1Remembered(e, src), picked)
	replayCheck(t, e, cfg)
}

// TestKr1ChangeZoneSearchForgetOtherRememberedReplacesSet (was
// TestChangeZoneSearchForgetOtherRememberedReplacesSet, Myr Incubator): the
// library search replaces the source's memory with the found card.
func TestKr1ChangeZoneSearchForgetOtherRememberedReplacesSet(t *testing.T) {
	t.Parallel()
	e, cfg, src := kr1ForgetBoard(t, 218,
		"ChangeZone | Origin$ Library | Destination$ Exile | ChangeType$ Artifact | ChangeNum$ 1 | RememberChanged$ True | ForgetOtherRemembered$ True",
		[]string{"Stale", "Name:Found Artifact\nManaCost:1\nTypes:Artifact\nOracle:x\n"}, nil)
	stale := kr1Put(t, e, 0, "Stale", state.ZExile)
	found := kr1Top(t, e, 0, "Found Artifact")[0]
	d := kr1Activate(t, e, src, stale)
	if d == nil || len(d.Options) != 1 || d.Options[0].Obj != found {
		t.Fatalf("search did not offer exactly the artifact: %+v", d)
	}
	kr1Pick(t, e, 0)
	kr1Want(t, e, state.ZExile, found)
	kr1SameIDs(t, "persistent remembered", kr1Remembered(e, src), found)
	replayCheck(t, e, cfg)
}

// TestKr1ChangeZoneHandForgetOtherRememberedClearsBeforeMoving (was
// TestChangeZoneHandForgetOtherRememberedClearsBeforeMoving): a hand move
// with RememberChanged$ replaces the stale memory with the moved card. (The
// old fixture carried no Optional$/Mandatory$ marker; a markerless hand move
// now fails loud with no move by design, so this one is Mandatory$.)
func TestKr1ChangeZoneHandForgetOtherRememberedClearsBeforeMoving(t *testing.T) {
	t.Parallel()
	e, cfg, src := kr1ForgetBoard(t, 219,
		"ChangeZone | Origin$ Hand | Destination$ Exile | ChangeNum$ 1 | Mandatory$ True | ForgetOtherRemembered$ True | RememberChanged$ True",
		[]string{"Hand Card", "Stale Card"}, nil)
	moving := kr1Put(t, e, 0, "Hand Card", state.ZHand)
	stale := kr1Put(t, e, 0, "Stale Card", state.ZExile)
	d := kr1Activate(t, e, src, stale)
	if d != nil {
		kr1Pick(t, e, kr1OptIndex(t, d, moving))
	}
	kr1Want(t, e, state.ZExile, moving)
	kr1SameIDs(t, "persistent remembered", kr1Remembered(e, src), moving)
	replayCheck(t, e, cfg)
}

// TestKr1ChooseCardForgetChosen (was
// TestChooseCardForgetChosenRemovesOnlyTheChosenObject and
// TestChooseCardWithoutForgetChosenKeepsBoth): ForgetChosen$ True removes
// exactly the chosen card from the source's memory (one forget-remembered
// event naming it) while the choice itself is still recorded as Chosen;
// without the parameter both stay remembered.
func TestKr1ChooseCardForgetChosen(t *testing.T) {
	t.Parallel()
	for i, forget := range []bool{true, false} {
		forget := forget
		seed := uint64(220 + i)
		name := "ForgetChosen"
		body := "ChooseCard | Defined$ You | Choices$ Card.IsRemembered | ChoiceZone$ Exile | Amount$ 1"
		if forget {
			body += " | ForgetChosen$ True"
		} else {
			name = "control"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e, cfg, src := kr1ForgetBoard(t, seed, body, []string{"Chosen Creature", "Kept Creature"}, nil)
			chosen := kr1Put(t, e, 0, "Chosen Creature", state.ZExile)
			kept := kr1Put(t, e, 0, "Kept Creature", state.ZExile)
			d := kr1Activate(t, e, src, chosen, kept)
			if d == nil || len(d.Options) != 2 {
				t.Fatalf("choose ask = %+v, want both remembered cards offered", d)
			}
			kr1Pick(t, e, kr1OptIndex(t, d, chosen))
			if !forget {
				kr1SameIDs(t, "persistent remembered (no ForgetChosen$)", kr1Remembered(e, src), chosen, kept)
				return
			}
			kr1SameIDs(t, "persistent remembered", kr1Remembered(e, src), kept)
			found := false
			for _, tg := range e.G.Obj(src).Chosen {
				if !tg.IsPlayer && tg.Obj == chosen {
					found = true
				}
			}
			if !found {
				t.Fatalf("the chosen card left the source's Chosen list: %+v", e.G.Obj(src).Chosen)
			}
			forgets := 0
			for _, ev := range e.L.Events {
				if ev.Kind == events.Choose && ev.Counter == "forget-remembered" {
					forgets++
					if len(ev.IDs) != 1 || ev.IDs[0] != chosen {
						t.Fatalf("forget event named %v, want [%d]", ev.IDs, chosen)
					}
				}
			}
			if forgets != 1 {
				t.Fatalf("forget-remembered events = %d, want 1", forgets)
			}
			replayCheck(t, e, cfg)
		})
	}
}
