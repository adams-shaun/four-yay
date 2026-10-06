package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// CantBeCast NumLimitEachTurn$ N ("can't cast more than N spells each turn"):
// the restriction binds a caster only once they have already cast N spells
// matching the static's own ValidCard$ this turn, counted from the event log.
// The offer (castRestrictedUsing) and the CR 601.2e recheck (recheckIllegal)
// answer from the one castLimitBinds rule.

// numLimitAtPriority re-asks seat 0's priority so the pending decision's
// options reflect the board the test just built.
func numLimitAtPriority(t *testing.T, e *Engine) {
	t.Helper()
	toMain1(t, e)
	e.priorityRound()
}

// numLimitCastAndResolve casts id from seat 0 through its cast option and
// resolves it, failing when the option is not offered.
func numLimitCastAndResolve(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	numLimitAtPriority(t, e)
	opt := castOptionNamed(e, id)
	if opt == nil {
		t.Fatalf("cast option for %d not offered", id)
	}
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 50)
	if o := e.G.Obj(id); o == nil || o.Zone == state.ZHand {
		t.Fatalf("object %d still in hand after its cast resolved", id)
	}
}

func TestCantBeCastNumLimitHighNoon(t *testing.T) {
	t.Parallel()
	e := handEngine(t, card(t, "Name:A\nManaCost:0\nTypes:Artifact\nOracle:x\n"),
		card(t, "Name:B\nManaCost:0\nTypes:Artifact\nOracle:x\n"))
	hand := e.G.Zone(state.ZHand, 0)
	if len(hand) != 2 {
		t.Fatalf("hand = %d cards, want 2", len(hand))
	}
	a, b := hand[0], hand[1]
	hn := onBoardCard(t, e, 0, corpusCard(t, "High Noon"))
	if o := e.G.Obj(hn); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: High Noon is not on the battlefield")
	}
	// Zero casts this turn: the first spell is offered (the defect refused it).
	if e.castRestricted(0, a) {
		t.Fatal("High Noon restricts seat 0's FIRST spell (NumLimitEachTurn$ 1 ignored)")
	}
	numLimitAtPriority(t, e)
	if castOptionNamed(e, a) == nil || castOptionNamed(e, b) == nil {
		t.Fatalf("first-spell options missing with zero casts: %+v", castOptions(t, e))
	}
	numLimitCastAndResolve(t, e, a)
	if n := e.SpellsCastThisTurnBy(0); n != 1 {
		t.Fatalf("seat 0 casts this turn = %d, want 1 (precondition)", n)
	}
	// The limit is spent: the second spell is refused, for seat 0 only.
	if !e.castRestricted(0, b) {
		t.Fatal("High Noon lets seat 0 cast a second spell this turn")
	}
	numLimitAtPriority(t, e)
	if castOptionNamed(e, b) != nil {
		t.Fatal("second spell still offered after the limit was spent")
	}
	if e.castRestricted(1, b) {
		t.Fatal("seat 0's cast consumed seat 1's limit (the limit is per caster)")
	}
}

// TestCantBeCastNumLimitRecheck: the CR 601.2e recheck answers like the
// offer. A candidate whose own PutOnStack is already logged (pushCast runs
// before recheckIllegal) is excluded, so a FIRST spell passes; with a prior
// matching cast the same proposal is reversed.
func TestCantBeCastNumLimitRecheck(t *testing.T) {
	t.Parallel()
	e := handEngine(t, card(t, "Name:A\nManaCost:0\nTypes:Artifact\nOracle:x\n"),
		card(t, "Name:B\nManaCost:0\nTypes:Artifact\nOracle:x\n"))
	hand := e.G.Zone(state.ZHand, 0)
	a, b := hand[0], hand[1]
	onBoardCard(t, e, 0, corpusCard(t, "High Noon"))
	// First spell, in flight: its own push is in the log.
	e.emit(events.Event{Kind: events.PutOnStack, Player: 0, Obj: a})
	if n := e.SpellsCastThisTurnBy(0); n != 1 {
		t.Fatalf("precondition: in-flight push not in the log (casts=%d)", n)
	}
	if e.recheckIllegal(&pendingCast{player: 0, card: a, ability: -1}) {
		t.Fatal("recheck reversed the FIRST spell: its own in-flight push was counted")
	}
	// Second spell proposed after a counted cast: reversed.
	if !e.recheckIllegal(&pendingCast{player: 0, card: b, ability: -1}) {
		t.Fatal("recheck accepted a second spell under High Noon")
	}
}

// TestCantBeCastNumLimitFilter: Ethersworn Canonist's Card.nonArtifact limit
// counts only nonartifact casts.
func TestCantBeCastNumLimitFilter(t *testing.T) {
	t.Parallel()
	e := handEngine(t, card(t, "Name:ArtA\nManaCost:0\nTypes:Artifact\nOracle:x\n"),
		card(t, "Name:ArtB\nManaCost:0\nTypes:Artifact\nOracle:x\n"),
		card(t, "Name:BearA\nManaCost:0\nTypes:Creature Bear\nPT:1/1\nOracle:x\n"),
		card(t, "Name:BearB\nManaCost:0\nTypes:Creature Bear\nPT:1/1\nOracle:x\n"))
	hand := e.G.Zone(state.ZHand, 0)
	artA, artB, bearA, bearB := hand[0], hand[1], hand[2], hand[3]
	onBoardCard(t, e, 0, corpusCard(t, "Ethersworn Canonist"))
	numLimitCastAndResolve(t, e, artA)
	// An artifact cast does not consume the nonartifact limit.
	if e.castRestricted(0, bearA) {
		t.Fatal("an artifact cast consumed Ethersworn Canonist's nonartifact limit")
	}
	numLimitCastAndResolve(t, e, bearA)
	if !e.castRestricted(0, bearB) {
		t.Fatal("a second nonartifact spell is allowed under Ethersworn Canonist")
	}
	if e.castRestricted(0, artB) {
		t.Fatal("Canonist restricts an artifact spell (outside its Card.nonArtifact filter)")
	}
}

// TestCantBeCastNumLimitFiresOfInvention: Fires has no ValidCard$ (read as
// Card), NumLimitEachTurn$ 2 binds the third spell, and Caster$ You.NonActive
// forbids its controller casting off their own turn.
func TestCantBeCastNumLimitFiresOfInvention(t *testing.T) {
	t.Parallel()
	e := handEngine(t, card(t, "Name:A\nManaCost:0\nTypes:Artifact\nOracle:x\n"),
		card(t, "Name:B\nManaCost:0\nTypes:Artifact\nOracle:x\n"),
		card(t, "Name:C\nManaCost:0\nTypes:Artifact\nOracle:x\n"))
	hand := e.G.Zone(state.ZHand, 0)
	a, b, c := hand[0], hand[1], hand[2]
	fi := onBoardCard(t, e, 0, corpusCard(t, "Fires of Invention"))
	if o := e.G.Obj(fi); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Fires of Invention is not on the battlefield")
	}
	if e.G.Active != 0 {
		t.Fatalf("precondition: active seat = %d, want 0", e.G.Active)
	}
	numLimitCastAndResolve(t, e, a)
	if e.castRestricted(0, b) {
		t.Fatal("Fires refuses the SECOND spell (limit is two)")
	}
	numLimitCastAndResolve(t, e, b)
	if !e.castRestricted(0, c) {
		t.Fatal("Fires lets seat 0 cast a THIRD spell this turn")
	}
	// Casting is for the controller's own turn only: seat 0 off its turn.
	e.G.Active = 1
	if !e.castRestricted(0, c) {
		t.Fatal("Fires lets seat 0 cast on an opponent's turn (Caster$ You.NonActive)")
	}
	// Seat 1 is not Fires' controller.
	if e.castRestricted(1, c) {
		t.Fatal("Fires of Invention restricts the opponent")
	}
}
