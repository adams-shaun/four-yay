package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// sacrificeTriggerEffect returns the REAL compiled enter-the-battlefield
// trigger whose Execute is a Sacrifice carrying UnlessCost$ DamageYou<N> —
// Vexing Devil and Longhorn Firebeast, the two corpus cards with the shape —
// so the ask and answer contract below is asserted against the real card
// parameters, never a hand-built bag.
func sacrificeTriggerEffect(t *testing.T, name string) (*cards.Card, *cards.SA) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("corpus has no %q", name)
	}
	for _, f := range c.Faces {
		for _, tr := range f.Triggers {
			if tr.Effect != nil && tr.Effect.API == "Sacrifice" && tr.Effect.Params["UnlessCost"] != "" {
				return c, tr.Effect
			}
		}
	}
	t.Fatalf("corpus card %q has no Sacrifice trigger with an UnlessCost$", name)
	return nil, nil
}

// upkeepSacrificeSA returns the REAL compiled upkeep trigger Execute for a
// named card whose Sacrifice carries an UnlessCost$ (Whipstitched Zombie:
// DB$ Sacrifice | UnlessPayer$ You | UnlessCost$ B — the plain-mana echo
// population; Mercenary Knight: UnlessCost$ Discard<1/Creature> — the
// unpriceable population).
func upkeepSacrificeSA(t *testing.T, name string) *cards.SA {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("corpus has no %q", name)
	}
	for _, f := range c.Faces {
		for _, tr := range f.Triggers {
			if tr.Effect != nil && tr.Effect.API == "Sacrifice" && tr.Effect.Params["UnlessCost"] != "" {
				return tr.Effect
			}
		}
	}
	t.Fatalf("corpus card %q has no Sacrifice trigger with an UnlessCost$", name)
	return nil
}

// devilBoard seats a named damage-offer Devil on seat ctlr's battlefield.
func devilBoard(t *testing.T, name string, ctlr state.PlayerID) (*fakeHost, state.ObjID, *cards.SA) {
	t.Helper()
	c, sac := sacrificeTriggerEffect(t, name)
	h := newHost(t, 2)
	o := h.g.AddObject(c, ctlr)
	o.Zone = state.ZBattlefield
	h.g.SetZone(state.ZBattlefield, ctlr, append(h.g.Zone(state.ZBattlefield, ctlr), o.ID))
	return h, o.ID, sac
}

// devilMoves counts how many times id moved battlefield -> graveyard.
func devilMoves(h *fakeHost, id state.ObjID) int {
	n := 0
	for _, ev := range h.log {
		if ev.Kind == events.MoveZone && ev.Obj == id && ev.From == state.ZBattlefield && ev.To == state.ZGraveyard {
			n++
		}
	}
	return n
}

// playerDamage counts Damage events naming player p and returns their total.
func playerDamage(h *fakeHost, p state.PlayerID) int {
	n := 0
	for _, ev := range h.log {
		if ev.Kind == events.Damage && ev.Player == p {
			n += int(ev.Amount)
		}
	}
	return n
}

// TestVexingDevilOffersEachOpponentTheDamage is the head regression test for
// fb-20260916T070855Z-0532c816: the REAL compiled Vexing Devil trigger is
// driven through effSacrifice, and the opponent must be ASKED (the pre-fix
// engine sacrificed the Devil unconditionally, asked nobody, dealt no
// damage). Asserts the ask's shape, then both branches: accepting (the
// UnlessPay "pay" answer) sacrifices the Devil;
// declining (every opponent, here the single one) leaves it in play.
func TestVexingDevilOffersEachOpponentTheDamage(t *testing.T) {
	h, devil, sac := devilBoard(t, "Vexing Devil", 0)
	if got := sac.Params["UnlessCost"]; got != "DamageYou<4>" {
		t.Fatalf("compiled UnlessCost = %q, want DamageYou<4>", got)
	}
	if sac.Params["UnlessPayer"] != "Opponent" || sac.Params["UnlessSwitched"] != "True" {
		t.Fatalf("compiled payer/switch = %q/%q, want Opponent/True",
			sac.Params["UnlessPayer"], sac.Params["UnlessSwitched"])
	}
	ah := &fx42AskHost{fakeHost: *h}
	ah.g = h.g

	ctx := &Ctx{Source: devil, Controller: 0}
	Resolve(ah, ctx, sac)

	// First pass: exactly one ask, to the opponent (seat 1), a KModes
	// pay/decline whose options carry the real damage amount, resuming as
	// unless_pay against this SA with the opponent cursor at 0.
	if len(ah.asks) != 1 {
		t.Fatalf("posed %d decisions, want exactly 1 (the opponent's damage offer)", len(ah.asks))
	}
	d := ah.asks[0]
	if d.Player != 1 {
		t.Fatalf("offer player = seat %d, want the opponent seat 1", d.Player)
	}
	if d.Kind != decision.KModes || d.Min != 1 || d.Max != 1 || len(d.Options) != 2 {
		t.Fatalf("offer decision = %+v, want a Min==Max==1 two-option KModes", d)
	}
	if d.ResumeKind != "unless_pay" || d.ResumeSA != sac || d.ResumeTarget != 0 {
		t.Fatalf("offer resume = kind %q target %d, want unless_pay at opponent 0", d.ResumeKind, d.ResumeTarget)
	}
	if got := d.Options[0].Label; got != "Take 4 damage" {
		t.Fatalf("accept label = %q, want the rendered take-4 offer (not raw script)", got)
	}
	if got := d.Prompt; len(got) == 0 || got[:13] != "Vexing Devil " {
		t.Fatalf("prompt = %q, want it to name the Devil and the damage, not raw script", got)
	}

	// Answered pass, accepted (rules' unless-pay settlement has already
	// emitted the damage; UnlessPay is "pay"): the sacrifice proceeds.
	ah.suspended = false
	ctx.UnlessPay = "pay"
	Resolve(ah, ctx, sac)
	if len(ah.asks) != 1 {
		t.Fatalf("accepted pass posed %d more decisions, want none", len(ah.asks)-1)
	}
	if devilMoves(&ah.fakeHost, devil) != 1 {
		t.Fatalf("accepted Devil moved to the graveyard %d times, want exactly 1", devilMoves(&ah.fakeHost, devil))
	}
	if n := playerDamage(&ah.fakeHost, 1); n != 0 {
		t.Fatalf("effects emitted %d damage on the accepted pass; payment events belong to rules", n)
	}
}

// TestVexingDevilEveryOpponentDeclineLeavesItInPlay pins the decline branch:
// the sole opponent refuses, no second ask is posed, and the Devil stays.
func TestVexingDevilEveryOpponentDeclineLeavesItInPlay(t *testing.T) {
	h, devil, sac := devilBoard(t, "Vexing Devil", 0)
	ah := &fx42AskHost{fakeHost: *h}
	ah.g = h.g
	ctx := &Ctx{Source: devil, Controller: 0}
	Resolve(ah, ctx, sac)
	if len(ah.asks) != 1 {
		t.Fatalf("posed %d decisions, want 1", len(ah.asks))
	}
	ah.suspended = false
	ctx.UnlessPay = "decline"
	Resolve(ah, ctx, sac)
	if devilMoves(&ah.fakeHost, devil) != 0 {
		t.Fatalf("declining Devil was sacrificed; it must stay on the battlefield")
	}
	if n := playerDamage(&ah.fakeHost, 1); n != 0 {
		t.Fatalf("declining seat took %d damage, want none", n)
	}
}

// TestVexingDevilDeclineCursorAsksTheNextOpponent pins the turn-order offer
// chain: with two alive opponents, the first decline moves the offer to the
// second opponent (ResumeTarget 1), and the second decline ends it.
func TestVexingDevilDeclineCursorAsksTheNextOpponent(t *testing.T) {
	c, sac := sacrificeTriggerEffect(t, "Vexing Devil")
	h := newHost(t, 3)
	o := h.g.AddObject(c, 0)
	o.Zone = state.ZBattlefield
	h.g.SetZone(state.ZBattlefield, 0, append(h.g.Zone(state.ZBattlefield, 0), o.ID))
	ah := &fx42AskHost{fakeHost: *h}
	ah.g = h.g

	ctx := &Ctx{Source: o.ID, Controller: 0}
	Resolve(ah, ctx, sac)
	if len(ah.asks) != 1 || ah.asks[0].Player != 1 {
		t.Fatalf("first offer = %v, want seat 1 (turn order after the controller)", ah.asks)
	}
	ah.suspended = false
	ctx.UnlessPay, ctx.UnlessNext = "decline", 0
	Resolve(ah, ctx, sac)
	if len(ah.asks) != 2 || ah.asks[1].Player != 2 {
		t.Fatalf("after the first decline the offers = %v, want a second ask to seat 2", ah.asks)
	}
	if ah.asks[1].ResumeTarget != 1 {
		t.Fatalf("second offer ResumeTarget = %d, want 1", ah.asks[1].ResumeTarget)
	}
	ah.suspended = false
	ctx.UnlessPay, ctx.UnlessNext = "decline", 1
	Resolve(ah, ctx, sac)
	if len(ah.asks) != 2 {
		t.Fatalf("after the last decline %d decisions were posed, want none", len(ah.asks)-2)
	}
	if devilMoves(&ah.fakeHost, o.ID) != 0 || h.g.Obj(o.ID).Zone != state.ZBattlefield {
		t.Fatalf("every-opponent-declined Devil did not stay on the battlefield")
	}
}

// TestLonghornFirebeastOfferRendersItsFive pins the second corpus card of the
// shape: same ask contract, damage amount 5.
func TestLonghornFirebeastOfferRendersItsFive(t *testing.T) {
	h, beast, sac := devilBoard(t, "Longhorn Firebeast", 0)
	if got := sac.Params["UnlessCost"]; got != "DamageYou<5>" {
		t.Fatalf("compiled UnlessCost = %q, want DamageYou<5>", got)
	}
	ah := &fx42AskHost{fakeHost: *h}
	ah.g = h.g
	ctx := &Ctx{Source: beast, Controller: 0}
	Resolve(ah, ctx, sac)
	if len(ah.asks) != 1 {
		t.Fatalf("posed %d decisions, want 1", len(ah.asks))
	}
	if got := ah.asks[0].Options[0].Label; got != "Take 5 damage" {
		t.Fatalf("accept label = %q, want the rendered take-5 offer", got)
	}
	ah.suspended = false
	ctx.UnlessPay = "pay"
	Resolve(ah, ctx, sac)
	if devilMoves(&ah.fakeHost, beast) != 1 {
		t.Fatalf("accepted Firebeast moved to the graveyard %d times, want 1", devilMoves(&ah.fakeHost, beast))
	}
}

// TestVexingDevilNoAskHostDeclines pins the R-9 stand-in the shared unless
// gate gives every API: a host that cannot ask records the deterministic
// decline — the Devil stays in play and nobody takes damage. In a real
// engine this path is unreachable (Engine.Ask always returns true); the
// damage-offer ask is what production exercises.
func TestVexingDevilNoAskHostDeclines(t *testing.T) {
	h, devil, sac := devilBoard(t, "Vexing Devil", 0)
	ctx := &Ctx{Source: devil, Controller: 0}
	Resolve(h, ctx, sac)
	if devilMoves(h, devil) != 0 {
		t.Fatalf("no-ask stand-in Devil moved %d times, want 0 (deterministic decline)", devilMoves(h, devil))
	}
	if n := playerDamage(h, 1); n != 0 {
		t.Fatalf("no-ask stand-in dealt seat 1 %d damage, want 0", n)
	}
}

// TestUpkeepSacrificePaySparesDeclineSacrifices drives the REAL compiled
// TestUpkeepSacrificePaySparesDeclineSacrifices drives the REAL compiled
// Whipstitched Zombie upkeep Execute (DB$ Sacrifice | UnlessPayer$ You |
// UnlessCost$ B) through the plain-mana unless-pay gate: the controller is
// asked; "pay" spares the permanent; "decline" sacrifices it.
func TestUpkeepSacrificePaySparesDeclineSacrifices(t *testing.T) {
	h, zombie, _ := devilBoard(t, "Whipstitched Zombie", 0)
	sac := upkeepSacrificeSA(t, "Whipstitched Zombie")
	ah := &fx42AskHost{fakeHost: *h}
	ah.g = h.g
	ctx := &Ctx{Source: zombie, Controller: 0}
	Resolve(ah, ctx, sac)
	if len(ah.asks) != 1 {
		t.Fatalf("posed %d decisions, want 1", len(ah.asks))
	}
	d := ah.asks[0]
	if d.Player != 0 {
		t.Fatalf("pay ask player = seat %d, want the controller seat 0 (UnlessPayer$ You)", d.Player)
	}
	if d.ResumeKind != "unless_pay" || d.ResumeSA != sac {
		t.Fatalf("pay ask resume = %q, want unless_pay against the Sacrifice SA", d.ResumeKind)
	}
	if got := d.Options[0].Label; got != "Pay B" {
		t.Fatalf("pay label = %q, want the rendered mana cost", got)
	}
	// Paid: spared.
	ah.suspended = false
	ctx.UnlessPay = "pay"
	Resolve(ah, ctx, sac)
	if devilMoves(&ah.fakeHost, zombie) != 0 || h.g.Obj(zombie).Zone != state.ZBattlefield {
		t.Fatalf("paid Whipstitched Zombie was sacrificed; a paid unless-pay spares it")
	}
	// Declined (fresh resolution): sacrificed.
	ah2 := &fx42AskHost{fakeHost: *h}
	ah2.g = h.g
	ctx2 := &Ctx{Source: zombie, Controller: 0}
	Resolve(ah2, ctx2, sac)
	ah2.suspended = false
	ctx2.UnlessPay = "decline"
	Resolve(ah2, ctx2, sac)
	if devilMoves(&ah2.fakeHost, zombie) != 1 {
		t.Fatalf("declined Whipstitched Zombie moved %d times, want 1 (sacrificed)", devilMoves(&ah2.fakeHost, zombie))
	}
}

// TestUnpriceableSacrificeKeepsTodaysBehaviour pins the blast-radius
// boundary: an unpriceable non-mana, non-damage UnlessCost$ (Mercenary
// Knight's Discard<1/Creature>) is a hard decline — the shared gate poses
// the ask (so the offer stays on the wire) and the deterministic decline
// keeps the pre-gate behaviour: the unconditional first-pass sacrifice.
func TestUnpriceableSacrificeKeepsTodaysBehaviour(t *testing.T) {
	h, knight, _ := devilBoard(t, "Mercenary Knight", 0)
	sac := upkeepSacrificeSA(t, "Mercenary Knight")
	if sac.Params["UnlessCost"] != "Discard<1/Creature>" {
		t.Fatalf("compiled UnlessCost = %q, want Discard<1/Creature>", sac.Params["UnlessCost"])
	}
	ah := &fx42AskHost{fakeHost: *h}
	ah.g = h.g
	ctx := &Ctx{Source: knight, Controller: 0}
	Resolve(ah, ctx, sac)
	if len(ah.asks) != 1 {
		t.Fatalf("unpriceable UnlessCost$ posed %d decisions, want the 1 ask the shared gate records", len(ah.asks))
	}
	ah.suspended = false
	ctx.UnlessPay = "decline"
	Resolve(ah, ctx, sac)
	if devilMoves(&ah.fakeHost, knight) != 1 {
		t.Fatalf("unpriceable UnlessCost$ sacrifice moved %d times, want 1", devilMoves(&ah.fakeHost, knight))
	}
}

// TestParseDamageUnlessCostSpellings pins the recognised vs unrecognised
// spellings of the damage-offer parser: exactly DamageYou<N> with a positive
// integer literal; an X, a bare SVar name or another bracket form fails
// closed.
func TestParseDamageUnlessCostSpellings(t *testing.T) {
	ok := map[string]int{"DamageYou<4>": 4, " DamageYou<5>": 5, "DamageYou<1>": 1}
	for in, want := range ok {
		n, dmg := ParseDamageUnlessCost(in)
		if !dmg || n != want {
			t.Fatalf("ParseDamageUnlessCost(%q) = %d,%v, want %d,true", in, n, dmg, want)
		}
	}
	for _, in := range []string{"X", "DamageYou<X>", "DamageYou<0>", "DamageYou<-2>", "PayLife<5>", "Discard<1/Card>", "1", "DamageYou<", "damageYou<4>"} {
		if _, dmg := ParseDamageUnlessCost(in); dmg {
			t.Fatalf("ParseDamageUnlessCost(%q) reported the damage offer shape", in)
		}
	}
}
