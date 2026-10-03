package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The Mode$ families the "Discard takes the front card" approximation row
// named: Random (CR 701.8b's random discard), the choosing modes the row
// grouped with it (LookYouChoose / YouChoose / RevealTgtChoose), the Mode$
// Hand | Optional$ True may-discard election, and the multi-target walk's
// per-target cursor. The helpers come from cardflow_discard_test.go
// (discardBoard, creature, land, inZone) and primitives_test.go (mkCard, sa,
// askHost): a 2-seat board whose seat 0 is the caster and seat 1 the target.

// discardRngHost is an askHost whose Rand replays a scripted sequence of raw
// [0,n) draws, so a Random discard's picks are known. Exhausted scripts
// degrade to 0 (the same convention dice_test's scriptedRollHost uses).
type discardRngHost struct {
	askHost
	seq []int
	i   int
}

func (h *discardRngHost) Rand(n int) int {
	if h.i < len(h.seq) {
		v := h.seq[h.i]
		h.i++
		return v % n
	}
	return 0
}

// standinNotes counts the R-9 "no engine host to ask" stand-in Notes in a
// host's log.
func standinNotes(log []events.Event) int {
	n := 0
	for _, ev := range log {
		if ev.Kind == events.Note && ev.Text == "discards its first card (no engine host to ask)" {
			n++
		}
	}
	return n
}

// TestDiscardRandomUsesTheEngineRng pins CR 701.8b: a Mode$ Random discard
// does NOT take the front card — the engine's own seeded RNG picks
// NumCards$ cards out of the DiscardValid$-filtered hand. The script [1, 1]
// draws bird (index 1 of [frog, bird, cat]) and then cat (index 1 of the
// remaining [frog, cat]): two DISTINCT cards, without replacement, and the
// front card (frog) and the land stay. No seat is asked and no stand-in Note
// is recorded — the randomness is the rule, not a missing ask.
func TestDiscardRandomUsesTheEngineRng(t *testing.T) {
	ah, c, ids := discardBoard(t,
		creature(t, "Frog"), land(t, "Islet"), creature(t, "Bird"), creature(t, "Cat"))
	rng := &discardRngHost{askHost: askHost{fakeHost: fakeHost{g: ah.g, log: ah.log}}, seq: []int{1, 1}}
	s := sa(t, "SP$ Discard | ValidTgts$ Player | Mode$ Random | NumCards$ 2 | DiscardValid$ Card.nonLand")

	if rng.g.Zone(state.ZHand, 1)[0] != ids[0] {
		t.Fatal("precondition: the hand's front card is not frog — the no-replacement assertion below would not discriminate")
	}
	effDiscard(rng, c, s)

	if rng.asked != nil {
		t.Fatal("a Random discard posed a decision — randomness is not a choice")
	}
	if !inZone(rng.g, state.ZGraveyard, 1, ids[2]) {
		t.Fatal("the RNG's first pick (bird, index 1) was not discarded — the discard took the front card instead")
	}
	if !inZone(rng.g, state.ZGraveyard, 1, ids[3]) {
		t.Fatal("the RNG's second pick (cat, index 1 of the remaining pool) was not discarded — the draw replaced")
	}
	if inZone(rng.g, state.ZGraveyard, 1, ids[0]) {
		t.Fatal("the front card (frog) was discarded — Random still takes hand[0]")
	}
	if !inZone(rng.g, state.ZHand, 1, ids[0]) || !inZone(rng.g, state.ZHand, 1, ids[1]) {
		t.Fatal("a card the RNG never picked (frog or the land) left the hand")
	}
	if standinNotes(rng.log) != 0 {
		t.Fatal("the Random arm recorded the R-9 stand-in Note — randomness is the rule, not a degradation")
	}
}

// TestDiscardChooseModesPoseNoAskWhenNothingIsEligible guards the ask-shape's
// empty-pool edge (the ask_empty contract): a choosing-mode discard into a
// hand with zero DiscardValid$-eligible cards resolves silently — no decision
// (it would be unanswerable), no stand-in Note (skipping an unanswerable ask
// is correct resolution, not a degradation).
func TestDiscardChooseModesPoseNoAskWhenNothingIsEligible(t *testing.T) {
	ah, ctx, _ := discardBoard(t, land(t, "Islet"))
	s := sa(t, "SP$ Discard | ValidTgts$ Player | Mode$ LookYouChoose | NumCards$ 1 | DiscardValid$ Card.nonLand")

	effDiscard(ah, ctx, s)
	if ah.asked != nil {
		t.Fatal("a LookYouChoose over an all-land hand posed an unanswerable decision")
	}
	if standinNotes(ah.log) != 0 {
		t.Fatal("the empty-pool skip recorded the R-9 stand-in Note")
	}
	if len(ah.g.Zone(state.ZHand, 1)) != 1 {
		t.Fatal("the land left the hand although nothing was eligible")
	}
}

// TestDiscardDefinedAppliesTheRememberRiders pins the Mode$ Defined arm on the
// SHARED discard-and-remember path every other mode uses. DefinedCards$ names
// the cards (Breathstealer's Crypt's "that player discards it"), and because
// the arm routes through discardAndRemember it applies RememberDiscarded$ and
// RememberDiscardingPlayers$ per card -- both the resolution's Ctx.Remembered
// set and the source object's event-backed remembered list. Emitting
// events.Discard directly, as the arm used to, moves the card but records
// neither, so a chained "for each card discarded this way" reads nothing.
func TestDiscardDefinedAppliesTheRememberRiders(t *testing.T) {
	ah, ctx, ids := discardBoard(t, creature(t, "Frog"), creature(t, "Bird"), creature(t, "Cat"))
	s := sa(t, "SP$ Discard | ValidTgts$ Player | Mode$ Defined | DefinedCards$ Remembered"+
		" | RememberDiscarded$ True | RememberDiscardingPlayers$ True")
	// The named cards are deliberately NOT the front of hand, so a front-card
	// discard could not pass, and there are two of them, so a stale hand slice
	// would show up as a skipped second card.
	ctx.Remembered = []state.Target{{Obj: ids[1]}, {Obj: ids[2]}}
	if ah.g.Zone(state.ZHand, 1)[0] != ids[0] {
		t.Fatal("precondition: the hand's front card is not frog — the named-card assertions would not discriminate")
	}

	effDiscard(ah, ctx, s)

	for _, want := range []state.ObjID{ids[1], ids[2]} {
		if !inZone(ah.g, state.ZGraveyard, 1, want) {
			t.Fatalf("the DefinedCards$ card %d was not discarded", want)
		}
	}
	if !inZone(ah.g, state.ZHand, 1, ids[0]) {
		t.Fatal("the un-named front card (frog) was discarded — Defined ignored DefinedCards$")
	}
	// RememberDiscarded$: both halves. Ctx.Remembered already held the two
	// named cards, so the discriminating half is the event-backed one the
	// source object carries.
	var remembered []state.ObjID
	for _, ev := range ah.log {
		if ev.Kind == events.Choose && ev.Counter == "remembered" && ev.Obj == ctx.Source {
			for _, id := range ev.IDs {
				// RememberDiscardingPlayers$ shares this event channel and
				// writes the discarding player as a PlayerRef; this test
				// pins the CARD half (ids are 1..n, player refs live in a
				// disjoint high-bit space).
				if _, isPlayer := id.PlayerRef(); isPlayer {
					continue
				}
				remembered = append(remembered, id)
			}
		}
	}
	if len(remembered) != 2 || remembered[0] != ids[1] || remembered[1] != ids[2] {
		t.Fatalf("RememberDiscarded$ recorded %v, want the two discarded cards %v — the Defined arm bypassed discardAndRemember",
			remembered, []state.ObjID{ids[1], ids[2]})
	}
	// RememberDiscardingPlayers$: the discarding player joins the set once.
	seen := 0
	for _, tg := range ctx.Remembered {
		if tg.IsPlayer && tg.Player == 1 {
			seen++
		}
	}
	if seen != 1 {
		t.Fatalf("RememberDiscardingPlayers$ recorded the discarder %d time(s), want exactly 1", seen)
	}
}
