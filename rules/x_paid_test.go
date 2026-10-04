package rules

// The paid-{X} pins that are not the Entreat the Angels card itself: the
// activated-ability sibling of the spell-side CR 107.3i binding, and the
// spell that suspends mid-resolution and must resume with the paid X still
// in its Ctx.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestActivatedAbilityWithXInCostResolvesItsPaidX is the activated-ability
// sibling of the spell-side CR 107.3i binding: an ability whose Cost$
// carries {X} is asked (xAsk runs on the shared cast flow for abilities
// too), the payment charges WithX, and commitCast records the paid value on
// the minted ability stack object with a CastInfo event so resolveTop's
// ability branch can bind it into effects.Ctx.X. CounterNum$ X then answers
// the chosen value, not 0 -- before the CastInfo emission the AbilityPush's
// Amount (the ability index) was the only thing on the wire and o.X stayed
// 0 through resolution.
func TestActivatedAbilityWithXInCostResolvesItsPaidX(t *testing.T) {
	t.Parallel()
	src := "Name:Ballista\nManaCost:X X\nTypes:Artifact Creature Construct\nPT:2/2\n" +
		"A:AB$ PutCounter | Cost$ X G | CounterType$ P1P1 | CounterNum$ X | Defined$ Self | SpellDescription$ Put X +1/+1 counters on CARDNAME.\nOracle:x\n"
	e, cfg, id := newFixtureDeck(t, 113, src)
	id = putCreature(t, e, 0, src)
	addMana(t, e, 0, "GGG") // X = 2: {2}{G}
	e.Advance()
	opt := abilityOption(t, e, id, 0)
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "x" {
		t.Fatalf("X decision %+v", d)
	}
	submitChoices(t, e, 2)
	if len(e.G.Stack) != 1 {
		t.Fatalf("ability stack objects = %v", e.G.Stack)
	}
	abilityObj := e.G.Obj(e.G.Stack[0])
	if abilityObj.X != 2 {
		t.Fatalf("ability stack object X = %d, want 2 (CastInfo did not record the paid X)", abilityObj.X)
	}
	passUntilStackEmpty(t, e, 20)
	p1p1 := int32(0)
	for _, c := range e.G.Obj(id).Counters {
		if c.Kind == "P1P1" {
			p1p1 += c.N
		}
	}
	if p1p1 != 2 {
		t.Fatalf("+1/+1 counters on Ballista = %d, want 2", p1p1)
	}
	replayCheck(t, e, cfg)
}

// TestPaidXSurvivesAMidResolutionSuspension pins the same binding across a
// mid-resolution ask: a spell whose resolution asks a player keeps the paid X
// for the rest of the walk. The fixture discards one card from the
// opponent's hand (the discard ask) and then, via its SubAbility, creates
// TokenAmount$ X Angels -- so the token count proves the walk after the
// answer still carries X = 2.
func TestPaidXSurvivesAMidResolutionSuspension(t *testing.T) {
	t.Parallel()
	src := "Name:Torment\nManaCost:X B\nTypes:Sorcery\n" +
		"A:SP$ Discard | Mode$ TgtChoose | NumCards$ 1 | ValidTgts$ Opponent | SubAbility$ Angels | SpellDescription$ x\n" +
		"SVar:Angels:DB$ Token | TokenAmount$ X | TokenScript$ w_4_4_angel_flying | TokenOwner$ You\n" +
		"SVar:X:Count$xPaid\nOracle:x\n"
	e, cfg, id := newFixtureDeckWithTokens(t, 112, src)
	// The fixture deals no opening hand: move two Mountains from seat 1's
	// library into their hand via logged MoveZones, so the discard ask has
	// eligible cards to offer (the ask fires only when eligible > NumCards).
	for _, cid := range e.G.Zone(state.ZLibrary, 1)[:2] {
		e.emit(events.Event{Kind: events.MoveZone, Obj: cid, From: state.ZLibrary, To: state.ZHand})
	}
	opponentHand := len(e.G.Zone(state.ZHand, 1))
	if opponentHand < 2 {
		t.Fatalf("opponent hand %d, need 2+ eligible for the discard ask", opponentHand)
	}
	addMana(t, e, 0, "CCB") // X = 2: {2}{B}
	opts := castOptions(t, e)
	idx := -1
	for _, o := range opts {
		if o.Obj == id {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for Torment: %+v", opts)
	}
	submitChoices(t, e, idx)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "x" {
		t.Fatalf("X decision %+v", d)
	}
	submitChoices(t, e, 2)
	// Target ask: pick the opponent's option (the option carrying Player 1).
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("target decision %+v", d)
	}
	tgt := -1
	for _, o := range d.Options {
		if o.Player == 1 {
			tgt = o.Index
		}
	}
	if tgt < 0 {
		t.Fatalf("no opponent option in %+v", d.Options)
	}
	submitChoices(t, e, tgt)
	// Payment returns to priority; both seats pass so the spell resolves.
	submitChoices(t, e, 0)
	d = e.Pending()
	if d == nil || d.Player != 1 || d.Kind != decision.KPriority {
		t.Fatalf("seat 1 priority %+v", d)
	}
	submitChoices(t, e, 0)
	// The discard ask: the discarding player (the opponent) picks 1 of their
	// own cards, which suspends resolution until answered.
	d = e.Pending()
	if d == nil || d.Kind != decision.KModes || d.ResumeKind != "discard" {
		t.Fatalf("discard decision %+v", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	passUntilStackEmpty(t, e, 20)
	if got := len(e.G.Zone(state.ZHand, 1)); got != opponentHand-1 {
		t.Fatalf("opponent hand %d, want %d after the discard", got, opponentHand-1)
	}
	if got := countAngelTokens(t, e, 0); got != 2 {
		t.Fatalf("Angels on the battlefield = %d, want 2 (paid X lost across the suspension?)", got)
	}
	if e.G.Obj(id).Zone != state.ZGraveyard {
		t.Fatalf("Torment zone %s after resolving", e.G.Obj(id).Zone)
	}
	replayCheck(t, e, cfg)
}
