package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestFlyingKickDirectParentTargetPower pins the real-corpus composition of a
// direct NumDmg$ ref/property body. The parent creature is deliberately grown
// to 5 power, so a silent zero (or the default 1) is distinguishable.
func TestFlyingKickDirectParentTargetPower(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	kick := searchCorpusCard(t, reg, "Flying Kick")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	dreadmaw := searchCorpusCard(t, reg, "Colossal Dreadmaw")
	island := searchCorpusCard(t, reg, "Island")
	deck := []*cards.Card{kick, bear}
	for len(deck) < 40 {
		deck = append(deck, island)
	}
	opp := []*cards.Card{dreadmaw}
	for len(opp) < 40 {
		opp = append(opp, island)
	}
	cfg := seatZeroStart(Config{Seed: 9217, Names: []string{"caster", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	parent := splitMoveFromLibrary(t, e, 0, "Grizzly Bears")
	victim := splitMoveFromLibrary(t, e, 1, "Colossal Dreadmaw")
	e.emit(events.Event{Kind: events.CounterChange, Obj: parent, Counter: "P1P1", Amount: 3})
	e.pending = nil
	e.priorityRound()
	if e.G.Obj(parent).Zone != state.ZBattlefield || e.G.Obj(victim).Zone != state.ZBattlefield {
		t.Fatalf("precondition: parent/victim not on battlefield: %s/%s", e.G.Obj(parent).Zone, e.G.Obj(victim).Zone)
	}
	if got := e.Power(parent); got != 5 || got == e.Power(victim) {
		t.Fatalf("precondition: powers must be distinct and parent must be 5: %d/%d", got, e.Power(victim))
	}
	id := searchMoveByName(t, e, "Double Jump", state.ZHand)
	addMana(t, e, 0, "RR")
	alt := splitOption(t, e, id, "split_alt")
	if alt == nil {
		t.Fatalf("Flying Kick split offer missing: %+v", castOptions(t, e))
	}
	submitChoices(t, e, alt.Index)

	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("Flying Kick parent target pending=%+v, want target", d)
	}
	parentChoice := -1
	for _, o := range d.Options {
		if o.Obj == parent {
			parentChoice = o.Index
		}
	}
	if parentChoice < 0 {
		t.Fatalf("parent target %d was not offered: %+v", parent, d.Options)
	}
	submitChoices(t, e, parentChoice)

	d = passUntilPendingKind(t, e, decision.KChoose, 30)
	if d == nil || d.ResumeKind != "tgts" {
		t.Fatalf("Flying Kick damage target pending=%+v, want tgts", d)
	}
	victimChoice := -1
	for _, o := range d.Options {
		if o.Obj == victim {
			victimChoice = o.Index
		}
	}
	if victimChoice < 0 {
		t.Fatalf("victim %d was not offered: %+v", victim, d.Options)
	}
	submitChoices(t, e, victimChoice)
	for i := 0; i < 30 && len(e.G.Stack) > 0; i++ {
		d = e.Pending()
		if d == nil {
			t.Fatal("no decision while resolving Flying Kick")
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("unexpected pending decision while resolving Flying Kick: %+v", d)
		}
		pass := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				pass = o.Index
			}
		}
		if pass < 0 {
			t.Fatalf("priority has no pass option: %+v", d.Options)
		}
		submitChoices(t, e, pass)
	}
	if e.G.Obj(victim).Damage != 5 {
		t.Fatalf("Flying Kick marked %d damage, want exactly 5", e.G.Obj(victim).Damage)
	}
	if e.G.Obj(victim).Zone != state.ZBattlefield {
		t.Fatalf("victim left battlefield after 5 damage: %s", e.G.Obj(victim).Zone)
	}
	replayCheck(t, e, cfg)
}
