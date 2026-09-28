package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const tappedQManaSource = "Name:Q Elf\nTypes:Creature Elf\nA:AB$ Mana | Cost$ Q | Produced$ G\nOracle:x\n"

func makeQSourceReadyAndTapped(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	if e.G.Obj(id).SummonSick {
		t.Fatal("precondition: Q source remained summoning sick after its controller's turn began")
	}
	e.emit(events.Event{Kind: events.Tap, Obj: id, Player: 0})
	if !e.G.Obj(id).Tapped {
		t.Fatal("precondition: Q source must be tapped")
	}
}

func TestTappedQManaSourceAcrossAvailabilityAndCastWindow(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	q := onBoard(t, e, 0, tappedQManaSource)
	makeQSourceReadyAndTapped(t, e, q)
	if got := e.AvailableMana(0).Total(); got != 1 {
		t.Fatalf("AvailableMana with tapped Q source = %d, want 1", got)
	}
	if got := e.PotentialMana(0).Total(); got != 1 {
		t.Fatalf("PotentialMana with tapped Q source = %d, want 1", got)
	}
	units := e.windowManaUnits(0)
	if len(units) != 1 || units[0].id != q {
		t.Fatalf("payment-window units = %+v, want tapped Q source %d", units, q)
	}

	// A tapped {T} source is not a substitute for a tapped {Q} source.
	tSource := onBoard(t, e, 0, "Name:T Elf\nTypes:Creature Elf\nA:AB$ Mana | Cost$ T | Produced$ R\nOracle:x\n")
	makeQSourceReadyAndTapped(t, e, tSource)
	if got := e.AvailableMana(0).Total(); got != 1 {
		t.Fatalf("tapped {T} source was counted alongside the Q source: total=%d", got)
	}
	if got := e.PotentialMana(0).Total(); got != 1 {
		t.Fatalf("PotentialMana counted a tapped {T} source: total=%d", got)
	}
	if got := e.windowManaUnits(0); len(got) != 1 || got[0].id != q {
		t.Fatalf("window admitted a tapped {T} source: %+v", got)
	}

	castUnits := e.castWindowUnits(&pendingCast{player: 0})
	if !e.castWindowReachable(0, ParseCost("G"), e.G.Players[0].Pool, e.G.Players[0].Snow,
		e.G.Players[0].ManaUnits(), e.G.Players[0].Life, nil, castUnits) {
		t.Fatal("cast-window reachability did not count the tapped Q source")
	}
}

func TestTappedQManaSourcePaysLiveCastWindow(t *testing.T) {
	t.Parallel()
	spellText := "Name:Live Q Probe\nManaCost:G\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n"
	e, _, spell := newFixtureDeck(t, 7731, spellText)
	toMain1(t, e)
	q := onBoard(t, e, 0, tappedQManaSource)
	makeQSourceReadyAndTapped(t, e, q)
	if units := e.castWindowUnits(&pendingCast{player: 0}); len(units) == 0 || units[0].id != q {
		t.Fatalf("cast-window capacity did not price the tapped Q source: %+v", units)
	}
	// Like other cast-window reachability tests, propose directly: ordinary
	// priority legalActions do not offer casts whose cost needs the payment
	// window to discover a source.
	e.beginCast(0, decision.Option{Kind: "cast", Obj: spell})
	e.Advance()
	if d := e.Pending(); d == nil || d.Kind != decision.KChoose {
		t.Fatalf("want live CR 601.2g payment window, got %+v", d)
	}
	if !e.G.Obj(q).Tapped {
		t.Fatal("precondition: Q source must still be tapped when the cast payment window opens")
	}
	tapWindowSource(t, e, q)
	if o := e.G.Obj(q); o == nil || o.Tapped {
		t.Fatalf("Q source after live payment = %+v, want untapped", o)
	}
	if o := e.G.Obj(spell); o == nil || o.Zone != state.ZStack {
		t.Fatalf("spell after Q payment = %+v, want stack", o)
	}
}

func TestTappedQManaAbilityPaysCompositeDiscardCost(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	q := onBoard(t, e, 0, "Name:Discard Q Elf\nTypes:Creature Elf\nA:AB$ Mana | Cost$ Q Discard<1/Card> | Produced$ G\nOracle:x\n")
	if cost := e.parseCost(e.G.Obj(q).Face().ManaAbilities()[0].Params["Cost"]); !cost.Untap || len(cost.Discard) != 1 {
		t.Fatalf("precondition: composite ability cost parsed as %#v, want {Q} plus one discard", cost)
	}
	makeQSourceReadyAndTapped(t, e, q)
	discard := e.G.AddObject(card(t, "Name:Discard Fodder\nTypes:Sorcery\nA:SP$ Draw | Num$ 1\nOracle:x\n"), 0)
	discard.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), discard.ID))
	e.pending = nil
	e.askPriority(0)
	activate := -1
	for _, option := range e.Pending().Options {
		if option.Kind == "activate" && option.Obj == q {
			activate = option.Index
			break
		}
	}
	if activate < 0 {
		t.Fatalf("tapped non-sick Q ability not offered: %+v", e.Pending().Options)
	}
	submitChoices(t, e, activate)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("discard cost choice = %+v", d)
	}
	chosen := -1
	for _, option := range d.Options {
		if option.Obj == discard.ID {
			chosen = option.Index
			break
		}
	}
	if chosen < 0 {
		t.Fatalf("discard fodder not offered: %+v", d.Options)
	}
	submitChoices(t, e, chosen)
	if o := e.G.Obj(q); o == nil || o.Tapped {
		t.Fatalf("source after composite Q cost = %+v, want untapped", o)
	}
	untapped := false
	for _, event := range e.L.Events {
		if event.Kind == events.Untap && event.Obj == q && event.Text == "untapped as a cost" {
			untapped = true
		}
	}
	if !untapped {
		t.Fatal("composite {Q} cost did not emit its untap event")
	}
	if o := e.G.Obj(discard.ID); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("discard fodder after cost = %+v, want graveyard", o)
	}
	if got := e.G.Players[0].Pool[state.MG]; got != 1 {
		t.Fatalf("mana after composite Q cost = %d, want {G}", got)
	}
}
