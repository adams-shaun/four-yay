package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// ChosenCtrl — the chosen-player-referent object predicate in a filter spec
// (Card.ChosenCtrl, Creature.ChosenCtrl, ...): "objects the chosen player
// controls". The filing card is Skullwinder, whose ETB's second leg is a
// hidden-origin ChangeZone with `ChangeType$ Card.ChosenCtrl`,
// `DefinedPlayer$ ChosenPlayer`, `Chooser$ ChosenPlayer`: the chosen
// opponent picks a card from their OWN graveyard and returns it to their
// hand. Before the predicate existed the ChangeType$ spec matched nothing,
// so the second leg was a silent no-op (only the caster's card returned).

// TestSkullwinderETBReturnsCastersAndChosenOpponentsCard is the filing
// card, end to end on the real corpus card: the ETB returns the caster's
// own graveyard card, the ChoosePlayer ask names an opponent, and that
// chosen opponent returns a card from their own graveyard to their hand.
func TestSkullwinderETBReturnsCastersAndChosenOpponentsCard(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	skullwinder := mustCorpusCard(t, reg, "Skullwinder")
	mine := mustCorpusCard(t, reg, "Grizzly Bears")
	theirs := mustCorpusCard(t, reg, "Hill Giant")

	e, cfg := tokenReplGameSeats(t, 74, []*cards.Card{skullwinder, mine}, []*cards.Card{theirs})

	// One card in each graveyard, moved there through a real logged move.
	mineID := moveSeededCard(t, e, 0, mine, state.ZGraveyard)
	theirsID := moveSeededCard(t, e, 1, theirs, state.ZGraveyard)
	// Precondition: both really sit in the graveyards the ETB reads.
	if o := e.G.Obj(mineID); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("my card precondition: %+v", o)
	}
	if o := e.G.Obj(theirsID); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("opponent card precondition: %+v", o)
	}

	// Skullwinder's ETB fires from a real battlefield entry.
	skullID := moveSeededCard(t, e, 0, skullwinder, state.ZBattlefield)
	e.pending = nil
	e.Advance()

	// First leg: the target ask for a card in MY graveyard. Only one card is
	// eligible, so the engine can legitimately auto-choose it with no ask
	// (the strict-supersets convention); answer the ask only if it is posed.
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		idx := indexOfObjOption(d, mineID)
		if idx < 0 {
			t.Fatalf("first-leg target ask does not offer my graveyard card: %+v", d.Options)
		}
		submitChoices(t, e, idx)
	}

	// Resolve the trigger to reach the second leg's ChoosePlayer ask. The
	// trigger objects sit on the stack, so pass priority while they drain;
	// the drain stops when the mid-resolution ChoosePlayer ask is posed.
	for d := e.Pending(); d != nil && d.Kind == decision.KPriority && len(e.G.Stack) > 0; d = e.Pending() {
		submitChoicePass(t, e)
	}

	// Second leg: the ChoosePlayer ask names an opponent, then that opponent
	// returns a card from their own graveyard. The first leg's resolution
	// advanced the same trigger to the ChoosePlayer ask.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("ChoosePlayer ask: %+v", d)
	}
	// Precondition: the ask really offers opponent seat 1, not seat 0.
	oppIdx := -1
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == 1 {
			oppIdx = o.Index
		}
	}
	if oppIdx < 0 {
		t.Fatalf("ChoosePlayer ask does not offer opponent seat 1: %+v", d.Options)
	}
	submitChoices(t, e, oppIdx)

	// The caster's card returned to hand before the second leg.
	if o := e.G.Obj(mineID); o == nil || o.Zone != state.ZHand {
		t.Fatalf("my card after first leg: zone %v, want hand", o.Zone)
	}

	// The hidden-origin pick is posed to the CHOSEN opponent (seat 1), over
	// their own graveyard card.
	pick := e.Pending()
	if pick == nil || pick.Kind != decision.KChoose || pick.Player != 1 {
		t.Fatalf("chosen opponent's graveyard pick: %+v", pick)
	}
	pickIdx := indexOfObjOption(pick, theirsID)
	if pickIdx < 0 {
		t.Fatalf("chosen opponent's pick does not offer their graveyard card: %+v", pick.Options)
	}
	submitChoices(t, e, pickIdx)
	passUntilStackEmpty(t, e, 20)

	// The chosen opponent returned their card to their own hand.
	if o := e.G.Obj(theirsID); o == nil || o.Zone != state.ZHand {
		t.Fatalf("chosen opponent's card after ETB: zone %v, want hand", o.Zone)
	}
	if o := e.G.Obj(skullID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Skullwinder precondition after ETB: %+v", o)
	}
	replayCheck(t, e, cfg)
}

// TestChosenCtrlPredicateMatchesOnlyTheChosenPlayersObjects is the
// vocabulary-level leaf: with a chosen player recorded on the source, every
// <base>.ChosenCtrl spelling admits that player's objects and rejects
// another seat's. It covers the sibling bases the corpus uses (Card,
// Permanent, Creature, Land, Artifact) and the Count$Valid site, so the
// whole predicate class -- not just Skullwinder's Card spelling -- is
// pinned.
func TestChosenCtrlPredicateMatchesOnlyTheChosenPlayersObjects(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 2)
	src := e.G.AddObject(card(t, "Name:Src\nTypes:Artifact\nOracle:x\n"), 0)
	// Seat 0 owns a creature, a land and an artifact; seat 1 owns counterparts.
	seatOf := map[state.PlayerID][]state.ObjID{}
	add := func(p state.PlayerID, src string) state.ObjID {
		o := e.G.AddObject(card(t, src), p)
		e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
		seatOf[p] = append(seatOf[p], o.ID)
		return o.ID
	}
	mineCreature := add(0, "Name:Mine Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	mineLand := add(0, "Name:Mine Land\nTypes:Land\nOracle:x\n")
	mineArtifact := add(0, "Name:Mine Rock\nTypes:Artifact\nOracle:x\n")
	theirsCreature := add(1, "Name:Theirs Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	theirsLand := add(1, "Name:Theirs Land\nTypes:Land\nOracle:x\n")
	theirsArtifact := add(1, "Name:Theirs Rock\nTypes:Artifact\nOracle:x\n")
	// Precondition: every object really sits on its controller's battlefield.
	for p, ids := range seatOf {
		for _, id := range ids {
			if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield || o.Controller != p {
				t.Fatalf("precondition: object %d (controller %d): %+v", id, p, o)
			}
		}
	}

	bases := []string{"Card", "Permanent", "Creature", "Land", "Artifact"}
	match := func(bases []string, id state.ObjID) bool {
		for _, b := range bases {
			if effects.MatchesSpecFrom(e.G, b+".ChosenCtrl", id, 0, src.ID) {
				return true
			}
		}
		return false
	}
	// No chosen player yet: every spelling fails closed.
	for _, id := range []state.ObjID{mineCreature, mineLand, mineArtifact} {
		if match(bases, id) {
			t.Fatalf("ChosenCtrl matched object %d with no chosen player recorded", id)
		}
	}
	// Choose player 1 on the source, through the same Choose event fold the
	// ChoosePlayer effect uses.
	e.emit(events.Event{Kind: events.Choose, Obj: src.ID, Counter: "chosen", IDs: []state.ObjID{state.PlayerRef(1)}})
	if o := e.G.Obj(src.ID); o == nil || len(o.Chosen) == 0 {
		t.Fatalf("precondition: chosen player not recorded on the source: %+v", o)
	}
	// Seat 1's objects match every base spelling; seat 0's match none.
	for base, id := range map[string]state.ObjID{"Creature": theirsCreature, "Land": theirsLand, "Artifact": theirsArtifact} {
		if !effects.MatchesSpecFrom(e.G, base+".ChosenCtrl", id, 0, src.ID) {
			t.Fatalf("%s.ChosenCtrl did not match the chosen player's object", base)
		}
	}
	if !match(bases, theirsCreature) {
		t.Fatalf("no ChosenCtrl base matched the chosen player's creature")
	}
	for _, id := range []state.ObjID{mineCreature, mineLand, mineArtifact} {
		if match(bases, id) {
			t.Fatalf("ChosenCtrl matched seat 0's object %d though seat 1 is chosen", id)
		}
	}
	// The Count$Valid site reads the same matcher: seat 0 controls 1 creature,
	// seat 1 controls 2 creatures once a second is added.
	add(1, "Name:Second Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	ctx := &effects.Ctx{Controller: 0, Source: src.ID, Chosen: e.G.Obj(src.ID).Chosen, ChosenValid: true}
	if got := effects.EvalCount(e, ctx, "Count$Valid Creature.ChosenCtrl"); got != 2 {
		t.Fatalf("Count$Valid Creature.ChosenCtrl = %d, want 2 (the chosen player's creatures)", got)
	}
}
