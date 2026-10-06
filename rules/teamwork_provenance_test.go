package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// A spell copy was not cast, but (like Conspire) carries the paid additional
// cost's provenance. Both readers must see the same value as the inherited bit.
func TestTeamworkPaidStackCopyReadsCostProvenance(t *testing.T) {
	e, _, reg := conspireEngine(t, "Go Nuts!", "Grizzly Bears")
	moveSeededCard(t, e, 1, searchCorpusCard(t, reg, "Grizzly Bears"), state.ZBattlefield)
	a := seedBattlefield(t, e, reg, "Goblin Piker")
	b := seedBattlefield(t, e, reg, "Grizzly Bears")
	spell := searchMoveByName(t, e, "Go Nuts!", state.ZHand)
	addMana(t, e, 0, "G")
	submitChoices(t, e, castOptMode(t, e.Pending().Options, spell, "teamworked").Index)
	d := teamworkAskOptions(t, e)
	submitChoices(t, e, teamworkOption(t, d, a), teamworkOption(t, d, b))
	finishTeamworkAnnouncement(t, e)
	if o := e.G.Obj(spell); o == nil || o.Zone != state.ZStack || !o.TeamworkPaid || o.CastFlags&state.FlagTeamworkPaid == 0 {
		t.Fatalf("precondition: paid spell not on stack: %+v", o)
	}
	e.emit(events.Event{Kind: events.StackCopy, Obj: spell, Player: 0})
	if len(e.G.Stack) != 2 {
		t.Fatalf("precondition: stack has no copy: %v", e.G.Stack)
	}
	copyID := e.G.Stack[1]
	if o := e.G.Obj(copyID); o == nil || !o.IsCopy || o.Zone != state.ZStack || o.CastFlags&state.FlagTeamworkPaid == 0 {
		t.Fatalf("precondition: copied paid-cost bit missing: %+v", o)
	}
	if got := effects.EvalCount(e, &effects.Ctx{Source: copyID}, "Count$Teamwork.2.1"); got != 2 {
		t.Fatalf("copy's Count$Teamwork = %d, want paid branch 2 (unpaid 1)", got)
	}
	teamworkConditionResult(t, e, copyID, true)
	e.emit(events.Event{Kind: events.MoveZone, Obj: spell, From: state.ZStack, To: state.ZGraveyard})
	if o := e.G.Obj(spell); o == nil || o.Zone != state.ZGraveyard || o.TeamworkPaid {
		t.Fatalf("spell leaving stack for non-battlefield zone retained paid fact: %+v", o)
	}
}

// Quantum Reduction is a printed Teamwork permanent. After resolution the
// permanent's ETB reader must see the paid fact, but a later departure must
// retire it. The inline GainLife gate stands in for an ETB ability reading
// Card.Self+Teamwork (the corpus has no such ETB carrier at this pin).
func TestTeamworkPermanentRetainsPaidFactForETB(t *testing.T) {
	e, _, reg := conspireEngine(t, "Quantum Reduction")
	tapper := seedBattlefield(t, e, reg, "Goblin Piker")
	seedBattlefield(t, e, reg, "Grizzly Bears")
	spell := searchMoveByName(t, e, "Quantum Reduction", state.ZHand)
	addMana(t, e, 0, "CU")
	submitChoices(t, e, castOptMode(t, e.Pending().Options, spell, "teamworked").Index)
	d := teamworkAskOptions(t, e)
	submitChoices(t, e, teamworkOption(t, d, tapper))
	finishTeamworkAnnouncement(t, e)
	if o := e.G.Obj(spell); o == nil || o.Zone != state.ZStack || !o.TeamworkPaid {
		t.Fatalf("precondition: paid enchantment not on stack: %+v", o)
	}
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(spell); o == nil || o.Zone != state.ZBattlefield || o.CastFlags&state.FlagTeamworkPaid == 0 {
		t.Fatalf("precondition: paid Teamwork permanent did not enter: %+v", o)
	}
	if got := effects.EvalCount(e, &effects.Ctx{Source: spell}, "Count$Teamwork.2.1"); got != 2 {
		t.Fatalf("ETB Count$Teamwork = %d, want paid 2 (unpaid 1)", got)
	}
	gate := &cards.SA{Kind: "DB", API: "GainLife", Params: map[string]string{
		"Defined": "You", "LifeAmount": "2", "ConditionDefined": "Self",
		"ConditionPresent": "Card.Self+Teamwork", "ConditionCompare": "EQ1",
	}}
	before := e.G.Players[0].Life
	effects.Resolve(e, &effects.Ctx{Source: spell, Controller: 0}, gate)
	if got := e.G.Players[0].Life; got != before+2 {
		t.Fatalf("ETB Card.Self+Teamwork gate: life %d -> %d, want %d", before, got, before+2)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: spell, From: state.ZBattlefield, To: state.ZGraveyard})
	if o := e.G.Obj(spell); o == nil || o.TeamworkPaid {
		t.Fatalf("departed permanent retained paid-cost fact: %+v", o)
	}
}
