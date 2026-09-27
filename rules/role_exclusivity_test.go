package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Role-token exclusivity: the second sentence of every Role token's rules
// text is "(If you control another Role on it, put that one into the
// graveyard.)" -- a creature carries at most one Role at a time. The sweep
// lives in Engine.emit's Attach clause, so it is reachable from BOTH attach
// paths (effects/token.go's AttachedTo$ mint and effects/attach.go's Attach
// SA) and fires BEFORE the new Attach applies. These pins drive real corpus
// carriers onto a fixture bear; the sweep itself is the MoveZone the SBAs
// use, so the departed token is moved to its owner's graveyard and then (CR
// 704.5d, the ordinary token SBA) ceases to exist at the next state-based
// pass -- the durable evidence is the logged sweep MoveZone, not the tombstone.

// castRoyalRole casts corpus Royal Treatment from seat 0's hand at the bear
// and returns the id of the minted Royal Role token.
func castRoyalRole(t *testing.T, e *Engine, bear state.ObjID) state.ObjID {
	t.Helper()
	moveByName(t, e, 0, "Royal Treatment", state.ZHand)
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "G", Amount: 1})
	e.priorityRound()
	castNamed(t, e, "Royal Treatment")
	td := drainUntilAsk(t, e, 30)
	if td == nil || td.Kind != decision.KTarget {
		t.Fatalf("Royal Treatment target ask missing: %+v", td)
	}
	bearIdx := -1
	for _, o := range td.Options {
		if o.Obj == bear {
			bearIdx = o.Index
		}
	}
	if bearIdx < 0 {
		t.Fatalf("the bear is not a legal Royal Role target: %+v", td.Options)
	}
	submitChoices(t, e, bearIdx)
	passUntilStackEmpty(t, e, 30)
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		o := e.G.Obj(id)
		if o == nil || !o.IsToken || o.AttachedTo != bear {
			continue
		}
		if f := o.Face(); f != nil && f.Name == "Royal" {
			return id
		}
	}
	t.Fatalf("no Royal Role token attached to the bear")
	return 0
}

// sweepMoveZoneInLog reports whether the log carries the exclusivity MoveZone
// for oldID (battlefield -> graveyard).
func sweepMoveZoneInLog(e *Engine, oldID state.ObjID) bool {
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.Obj == oldID &&
			ev.From == state.ZBattlefield && ev.To == state.ZGraveyard &&
			ev.Text == "another Role on it: the old Role goes to the graveyard" {
			return true
		}
	}
	return false
}

// TestRoleExclusivityOldRoleGoesToGraveyard pins the sweep on the same Role
// kind twice: a second Royal Role attaching to a bearer that already carries
// one puts the OLD Role into its owner's graveyard before the new Attach
// applies, so the bearer ends with exactly one Role and its power reflects
// only the surviving one.
func TestRoleExclusivityOldRoleGoesToGraveyard(t *testing.T) {
	reg := choiceCorpusRegistry(t)
	e := corpusEngine(t, reg,
		[]*cards.Card{choiceCorpusCard(t, "Royal Treatment"), choiceCorpusCard(t, "Royal Treatment")}, nil)
	bear := battlefieldFixture(t, e, 0, "Name:Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")

	first := castRoyalRole(t, e, bear)
	if e.Power(bear) != 3 || e.Toughness(bear) != 3 {
		t.Fatalf("enchanted bear after the first Role = %d/%d, want 3/3",
			e.Power(bear), e.Toughness(bear))
	}
	second := castRoyalRole(t, e, bear)

	count := 0
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		o := e.G.Obj(id)
		if o != nil && o.IsToken && o.AttachedTo == bear && e.isRole(o) {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("the bear carries %d Role tokens, want exactly 1", count)
	}
	if first == second {
		t.Fatalf("the second Royal Treatment did not mint a new token")
	}
	if old := e.G.Obj(first); old != nil && old.Zone == state.ZBattlefield {
		t.Fatalf("the old Role is still on the battlefield (zone %v)", old.Zone)
	}
	if old := e.G.Obj(first); old != nil && old.Zone != state.ZGraveyard && old.Zone != state.ZCeased {
		t.Fatalf("the old Role left the battlefield to %v, want graveyard (then ceased)", old.Zone)
	}
	if !sweepMoveZoneInLog(e, first) {
		t.Fatalf("no exclusivity MoveZone for the old Role in the log")
	}
	if e.G.Obj(second).AttachedTo != bear {
		t.Fatalf("the surviving Role is not attached to the bear: %+v", e.G.Obj(second))
	}
	if e.Power(bear) != 3 || e.Toughness(bear) != 3 {
		t.Fatalf("bear after the sweep = %d/%d, want 3/3 (two Roles would be 4/4)",
			e.Power(bear), e.Toughness(bear))
	}
}

// TestRoleExclusivityDifferentKindSweepsOldRole pins the sweep across two
// different Role kinds: Wicked Role (Charming Scoundrel's token mode) first,
// then Royal Treatment on the same bear -- the Wicked Role goes to the
// graveyard, the Royal Role survives attached, and the bearer's power
// reflects only the surviving Role.
func TestRoleExclusivityDifferentKindSweepsOldRole(t *testing.T) {
	reg := choiceCorpusRegistry(t)
	e := corpusEngine(t, reg,
		[]*cards.Card{choiceCorpusCard(t, "Charming Scoundrel"), choiceCorpusCard(t, "Royal Treatment")}, nil)
	bear := battlefieldFixture(t, e, 0, "Name:Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")

	moveByName(t, e, 0, "Charming Scoundrel", state.ZHand)
	for _, s := range []string{"R", "C"} {
		e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: s, Amount: 1})
	}
	e.priorityRound()
	castNamed(t, e, "Charming Scoundrel")
	passToKind(t, e, decision.KModes)
	d := e.Pending()
	modeIdx := -1
	for _, o := range d.Options {
		if o.Label == "Create a Wicked Role token attached to target creature you control." {
			modeIdx = o.Index
		}
	}
	if modeIdx < 0 {
		t.Fatalf("token mode not offered: %+v", d.Options)
	}
	submitChoices(t, e, modeIdx)
	td := drainUntilAsk(t, e, 30)
	if td == nil || td.Kind != decision.KTarget {
		t.Fatalf("role target ask missing: %+v", td)
	}
	bearIdx := -1
	for _, o := range td.Options {
		if o.Obj == bear {
			bearIdx = o.Index
		}
	}
	if bearIdx < 0 {
		t.Fatalf("the bear is not a legal role target: %+v", td.Options)
	}
	submitChoices(t, e, bearIdx)
	passUntilStackEmpty(t, e, 30)
	var wicked state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		o := e.G.Obj(id)
		if o != nil && o.IsToken && o.AttachedTo == bear {
			if f := o.Face(); f != nil && f.Name == "Wicked" {
				wicked = id
			}
		}
	}
	if wicked == 0 {
		t.Fatalf("no Wicked Role token attached to the bear")
	}

	royal := castRoyalRole(t, e, bear)

	count := 0
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		o := e.G.Obj(id)
		if o != nil && o.IsToken && o.AttachedTo == bear && e.isRole(o) {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("the bear carries %d Role tokens, want exactly 1", count)
	}
	if old := e.G.Obj(wicked); old != nil && old.Zone == state.ZBattlefield {
		t.Fatalf("the Wicked Role is still on the battlefield (zone %v)", old.Zone)
	}
	if !sweepMoveZoneInLog(e, wicked) {
		t.Fatalf("no exclusivity MoveZone for the Wicked Role in the log")
	}
	if e.G.Obj(royal).AttachedTo != bear {
		t.Fatalf("the Royal Role is not attached to the bear: %+v", e.G.Obj(royal))
	}
	if e.Power(bear) != 3 || e.Toughness(bear) != 3 {
		t.Fatalf("bear after the sweep = %d/%d, want 3/3 (both Roles would be 4/4)",
			e.Power(bear), e.Toughness(bear))
	}
}
