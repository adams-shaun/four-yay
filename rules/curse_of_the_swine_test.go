package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Curse of the Swine's Boars are owned by each exiled creature's LAST-KNOWN
// controller. Forge's script is
//
//	A:SP$ ChangeZone | ValidTgts$ Creature | TargetMin$ X | TargetMax$ X |
//	    Origin$ Battlefield | Destination$ Exile | RememberLKI$ True |
//	    SubAbility$ DBToken
//	SVar:DBToken:DB$ RepeatEach | UseImprinted$ True |
//	    DefinedCards$ DirectRemembered | RepeatSubAbility$ TokenBoar |
//	    SubAbility$ DBCleanup | ChangeZoneTable$ True
//	SVar:TokenBoar:DB$ Token | TokenScript$ g_2_2_boar |
//	    TokenOwner$ ImprintedController
//
// so the RepeatEach iterates the exiled objects and each iteration's
// TokenOwner$ ImprintedController is the exiled creature's controller. Two
// engine facts make that read non-trivial:
//
//  1. TokenOwner$ ImprintedController was not a recognised spelling in
//     effToken, so every Boar fell to the unrecognised-owner default -- the
//     resolving spell's controller (Curse's caster), wrong for every
//     opponent's creature.
//  2. events.Apply's Move resets a battlefield departure's controller to its
//     owner (CR 400.7), and Forge's RememberLKI$ True stores an LKI copy of
//     the exile (CardCopyService.getLKICopy). So the last-known controller
//     must be captured before the move: a creature that was STOLEN and then
//     exiled answers its taker, not its owner.
//
// This test therefore exiles one ordinary Bear (controller == owner == seat
// 1) and one Bear stolen to seat 0 (controller 0, owner 1). Both Boars must
// be created: one owned by seat 1, one by seat 0. Without the fix both are
// owned by seat 0 (the caster): the seat-1 Boar assertion fails.
func TestCurseOfTheSwineBoarsFollowTheExiledCreaturesController(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	curse := searchCorpusCard(t, reg, "Curse of the Swine")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	mountain := searchCorpusCard(t, reg, "Mountain")

	deck0 := make([]*cards.Card, 0, 40)
	deck0 = append(deck0, curse)
	for len(deck0) < 40 {
		deck0 = append(deck0, mountain)
	}
	deck1 := []*cards.Card{bear, bear}
	for len(deck1) < 40 {
		deck1 = append(deck1, mountain)
	}
	cfg := seatZeroStart(Config{Seed: 9202, Names: []string{"curser", "opponent"},
		Decks: [][]*cards.Card{deck0, deck1}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)

	// Two Bears onto seat 1's battlefield. Steal the first to seat 0 --
	// control 0, owner 1 -- so the LKI controller read is load-bearing.
	var bears []state.ObjID
	for _, id := range e.G.Zone(state.ZLibrary, 1) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Grizzly Bears" {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZBattlefield})
			bears = append(bears, id)
			if len(bears) == 2 {
				break
			}
		}
	}
	if len(bears) != 2 {
		t.Fatalf("opponent battlefield Bears = %d, want 2", len(bears))
	}
	e.emit(events.Event{Kind: events.ControlChange, Obj: bears[0], Player: 0})
	if c := e.G.Obj(bears[0]).Controller; c != 0 {
		t.Fatalf("stolen Bear controller = %d, want 0 (the steal did not take)", c)
	}
	if own := e.G.Obj(bears[0]).Owner; own != 1 {
		t.Fatalf("stolen Bear owner = %d, want 1 (owner and controller must differ for the LKI read)", own)
	}
	e.pending = nil
	e.priorityRound()

	// Curse of the Swine into seat 0's hand, funded for X = 2 ({2}{U}{U}).
	var cid state.ObjID
	for _, id := range e.G.Zone(state.ZLibrary, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Curse of the Swine" {
			cid = id
			break
		}
	}
	if cid == 0 {
		t.Fatal("Curse of the Swine not found in seat 0's library")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: cid, From: state.ZLibrary, To: state.ZHand})
	e.pending = nil
	e.priorityRound()
	addMana(t, e, 0, "UUCC")

	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == cid {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for Curse of the Swine: %+v", d.Options)
	}
	submitChoices(t, e, idx)

	// {X} announcement: X = 2.
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("X announcement decision = %+v, want a KChoose", d)
	}
	xIdx := -1
	for _, o := range d.Options {
		if o.Kind == "x" && o.Amount == 2 {
			xIdx = o.Index
		}
	}
	if xIdx < 0 {
		t.Fatalf("no X = 2 option: %+v", d.Options)
	}
	submitChoices(t, e, xIdx)

	// Two targets: both Bears, whichever order they are offered.
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("target decision = %+v, want a KTarget", d)
	}
	if d.Min != 2 || d.Max != 2 {
		t.Fatalf("target bounds = %d..%d, want 2..2 (X = 2)", d.Min, d.Max)
	}
	var picks []int
	for _, o := range d.Options {
		if o.Obj == bears[0] || o.Obj == bears[1] {
			picks = append(picks, o.Index)
		}
	}
	if len(picks) != 2 {
		t.Fatalf("Bear options in %+v, want both bears offered", d.Options)
	}
	submitChoices(t, e, picks...)
	passUntilStackEmpty(t, e, 30)

	// Precondition: both Bears actually left the battlefield (the exile the
	// Boars are supposed to mirror).
	for _, id := range bears {
		if z := e.G.Obj(id).Zone; z != state.ZExile {
			t.Fatalf("Bear %d zone = %v, want exile", id, z)
		}
	}

	// Exactly two Boars, one per exiled Bear, owned by that Bear's
	// last-known controller: the stolen Bear's taker (seat 0) and the
	// ordinary Bear's controller (seat 1).
	byOwner := map[state.PlayerID]int{}
	for _, o := range e.G.Objs {
		if o.IsToken && o.Zone == state.ZBattlefield && o.Face() != nil && o.Face().Name == "Boar Token" {
			byOwner[o.Owner]++
		}
	}
	if byOwner[0] != 1 || byOwner[1] != 1 {
		t.Fatalf("Boar owners = %v, want one for the stolen Bear's taker (seat 0) and one for the opponent's Bear (seat 1)", byOwner)
	}
	replayCheck(t, e, cfg)
}
