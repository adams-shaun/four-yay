package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// This file covers the GainLife<N/Player...> cost head: the three corpus
// AlternativeCost cards that let a payer have an opponent gain life instead of
// paying the printed mana cost (Invigorate, Reverent Silence, Skyshroud
// Cutter). Before the head existed ParseCost reported the token as Unknown
// (priced one phantom generic) and altCostParse withheld the whole
// alternative, so the option never appeared.

const gainLifeForestSrc = "Name:Forest\nTypes:Basic Land Forest\nOracle:x\n"
const gainLifeBearSrc = "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

// TestGainLifeCostTokenParses is the parse-level half: the head is recognised
// in both corpus spellings, with no Unknown, no phantom generic mana, and one
// GainLife part carrying N, the raw player spec and the each marker.
func TestGainLifeCostTokenParses(t *testing.T) {
	t.Parallel()
	cases := []struct {
		token string
		n     int32
		spec  string
		each  bool
	}{
		{"GainLife<3/Player.Opponent>", 3, "Player.Opponent", false},
		{"GainLife<6/Player.Other/*>", 6, "Player.Other", true},
		{"GainLife<5/Player.Other/*>", 5, "Player.Other", true},
	}
	for _, tc := range cases {
		c := ParseCost(tc.token)
		if len(c.Unknown) != 0 {
			t.Errorf("%s: Unknown = %v, want empty", tc.token, c.Unknown)
		}
		if c.Generic != 0 {
			t.Errorf("%s: Generic = %d, want 0 (no phantom pip)", tc.token, c.Generic)
		}
		if len(c.GainLife) != 1 {
			t.Fatalf("%s: GainLife parts = %d, want 1", tc.token, len(c.GainLife))
		}
		got := c.GainLife[0]
		if got.N != tc.n || got.Spec != tc.spec || got.Each != tc.each {
			t.Errorf("%s: part = {N:%d Spec:%q Each:%v}, want {N:%d Spec:%q Each:%v}",
				tc.token, got.N, got.Spec, got.Each, tc.n, tc.spec, tc.each)
		}
	}
}

// gainLifeAltOption returns the first alternative-cost cast option for id
// (AltCostIndex > 0), or nil.
func gainLifeAltOption(opts []decision.Option, id state.ObjID) *decision.Option {
	for i := range opts {
		o := opts[i]
		if o.Kind == "cast" && o.Obj == id && o.AltCostIndex > 0 {
			return &o
		}
	}
	return nil
}

// putBattlefield (target_setprops_test.go) adds a fixture card onto p's
// battlefield through a MoveZone event and returns its id.

// TestInvigorateGainLifeAlternativeCostOfferedAndPaid drives the whole
// alternative: with an EMPTY mana pool, Invigorate, a Forest on the
// battlefield and a target creature available, the life-gain alternative is
// offered; taking it has the opponent gain 3 life and the spell still
// resolves its Pump.
func TestInvigorateGainLifeAlternativeCostOfferedAndPaid(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Invigorate"))
	id := e.G.Zone(state.ZHand, 0)[0]
	if o := e.G.Obj(id); o == nil || o.Face() == nil || o.Face().Name != "Invigorate" {
		t.Fatalf("fixture card is not Invigorate: %+v", o)
	}
	forest := putBattlefield(t, e, 0, gainLifeForestSrc)
	bear := putBattlefield(t, e, 0, gainLifeBearSrc)
	// Precondition: the Forest and the target creature are on the battlefield
	// where IsPresent$ Forest.YouCtrl and the Pump target walk read them.
	for _, want := range []state.ObjID{forest, bear} {
		if o := e.G.Obj(want); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("object %d not on the battlefield: %+v", want, o)
		}
	}
	// Precondition: the pool is empty, so ONLY the alternative can pay.
	if e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("precondition: seat 0 pool = %d, want empty", e.G.Players[0].Pool.Total())
	}
	oppBefore := e.G.Players[1].Life

	alt := gainLifeAltOption(e.legalActions(0), id)
	if alt == nil {
		t.Fatalf("no life-gain alternative offered: %+v", e.legalActions(0))
	}
	e.beginCast(0, *alt)
	// Invigorate's Pump asks for its creature target.
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget || len(d.Options) == 0 {
		t.Fatalf("expected a target decision, got %+v", d)
	}
	bearBefore := e.Power(bear)
	submitChoices(t, e, d.Options[0].Index)
	// Resolve the spell (the alternative was paid as part of the cast, so the
	// life gain happened before this resolve).
	e.resolveTop()

	if got := e.G.Players[1].Life; got != oppBefore+3 {
		t.Errorf("opponent life = %d, want %d (gain 3)", got, oppBefore+3)
	}
	if got := e.G.Players[0].Life; got != 20 {
		t.Errorf("payer life = %d, want 20 (payer does not gain)", got)
	}
	// The spell resolved: Bear got +4/+4 and Invigorate left the stack.
	if got := e.Power(bear); got != bearBefore+4 {
		t.Errorf("target power = %d, want %d (Pump +4/+4)", got, bearBefore+4)
	}
	if o := e.G.Obj(id); o == nil || o.Zone == state.ZStack {
		t.Errorf("Invigorate still on the stack after resolution: %+v", o)
	}
}

// TestReverentSilenceGainLifeEachOtherPlayerPaid pins the /* spelling:
// Reverent Silence's "each other player gains 6 life" pays the other seat and
// leaves the payer at 20.
func TestReverentSilenceGainLifeEachOtherPlayerPaid(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Reverent Silence"))
	id := e.G.Zone(state.ZHand, 0)[0]
	if o := e.G.Obj(id); o == nil || o.Face() == nil || o.Face().Name != "Reverent Silence" {
		t.Fatalf("fixture card is not Reverent Silence: %+v", o)
	}
	forest := putBattlefield(t, e, 0, gainLifeForestSrc)
	if o := e.G.Obj(forest); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Forest not on the battlefield: %+v", o)
	}
	if e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("precondition: seat 0 pool = %d, want empty", e.G.Players[0].Pool.Total())
	}
	oppBefore := e.G.Players[1].Life

	alt := gainLifeAltOption(e.legalActions(0), id)
	if alt == nil {
		t.Fatalf("no life-gain alternative offered: %+v", e.legalActions(0))
	}
	e.beginCast(0, *alt)
	e.resolveTop()

	if got := e.G.Players[1].Life; got != oppBefore+6 {
		t.Errorf("opponent life = %d, want %d (each other player gains 6)", got, oppBefore+6)
	}
	if got := e.G.Players[0].Life; got != 20 {
		t.Errorf("payer life = %d, want 20 (the payer is not an other player)", got)
	}
}

// TestGainLifeAlternativeWithoutForestNotOffered pins that the IsPresent$
// gate still binds: the same board minus the Forest offers no alternative,
// and the head being parsed does not leak a free cast.
func TestGainLifeAlternativeWithoutForestNotOffered(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Invigorate"))
	id := e.G.Zone(state.ZHand, 0)[0]
	bear := putBattlefield(t, e, 0, gainLifeBearSrc)
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: target creature not on the battlefield: %+v", o)
	}
	// Precondition: no Forest anywhere on seat 0's battlefield.
	for _, cand := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(cand); o != nil && o.Face() != nil && o.Face().Name == "Forest" {
			t.Fatalf("precondition: a Forest is on the battlefield: %+v", o.Face().Name)
		}
	}
	if e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("precondition: seat 0 pool = %d, want empty", e.G.Players[0].Pool.Total())
	}
	if alt := gainLifeAltOption(e.legalActions(0), id); alt != nil {
		t.Fatalf("life-gain alternative offered without a Forest: %+v", alt)
	}
	// The paid cast is ALSO withheld (2 G cannot be funded from an empty
	// pool), so nothing about this card is castable -- the head did not leak
	// a phantom free cast.
	if hasCastOption(e.legalActions(0), id) {
		t.Fatalf("Invigorate is castable with no Forest and an empty pool: %+v", e.legalActions(0))
	}
	// The life-gain handler must not have run for the withheld alternative:
	// no LifeChange toward either seat from the un-cast spell.
	for _, ev := range e.L.Events {
		if ev.Kind == events.LifeChange {
			t.Fatalf("life changed with no cast: %+v", ev)
		}
	}
}
