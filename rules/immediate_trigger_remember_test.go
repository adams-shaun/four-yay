package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The ImmediateTrigger `RememberObjects$ Targeted` ticket (itr1). The
// "when you do" body of an ImmediateTrigger that names the resolution's own
// target previously fell into effImmediateTrigger's catch-all Note arm: it
// handed the body the capture-excluded parent remembered set (empty for a
// death trigger), so Behemoth of Vault 0's `Defined$ DelayTriggerRememberedLKI`
// Destroy resolved to nobody even after its PayEnergy<X> unless-cost was paid.
// The fix routes every explicit spelling outside the Remembered* family
// through the shared fail-closed knownDefinedTargets resolver, preferring
// Ctx.PickedTargets for the bare Targeted/ThisTargetedCard spellings exactly
// as effectRemembered's Targeted arm does (the AB-form's own pre-asked target
// arrives on PickedTargets).
//
// Both fixtures are real compiled corpus cards; Behemoth of Vault 0 is in the
// science-pip repo deck but NOT in any legacy golden deck, so TestHeads does
// not depend on its behaviour.

// itrFullEngine builds a two-seat game with the given seat-0 fixtures in its
// deck and a cheap nonland victim in seat 1's deck, so the tests can drive the
// real corpus cards end to end.
func itrFullEngine(t *testing.T, reg *cards.Registry, fixtures ...string) (*Engine, Config) {
	t.Helper()
	forest := searchCorpusCard(t, reg, "Forest")
	mountain := searchCorpusCard(t, reg, "Mountain")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	deck := make([]*cards.Card, 0, 40)
	for _, name := range fixtures {
		deck = append(deck, searchCorpusCard(t, reg, name))
	}
	for i := 0; i < 8; i++ {
		deck = append(deck, forest, mountain)
	}
	for len(deck) < 40 {
		deck = append(deck, bear)
	}
	// The victim seats in seat 1's opening library so a logged MoveZone
	// reaches it; the rest is filler.
	opp := []*cards.Card{searchCorpusCard(t, reg, "Grizzly Bears")}
	for len(opp) < 40 {
		opp = append(opp, mountain)
	}
	cfg := seatZeroStart(Config{Seed: 9713, Names: []string{"itr", "opp"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens, NameUniverse: reg.Cards})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}

// TestImmediateTriggerRememberObjectsTargetedPaysAndDestroys is the whole
// ticket: with the real Behemoth of Vault 0, the death trigger offers its
// nonland-permanent target, the unless-pay election pays the {E} cost, and the
// targeted permanent is actually destroyed.
func TestImmediateTriggerRememberObjectsTargetedPaysAndDestroys(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)

	// Precondition: the card really carries the spec under test.
	behemoth := searchCorpusCard(t, reg, "Behemoth of Vault 0")
	trigSA := cards.ResolveSVar(behemoth.Faces[0].SVars, "TrigImmediateTrig")
	if trigSA == nil || trigSA.API != "ImmediateTrigger" {
		t.Fatalf("corpus precondition: Behemoth's TrigImmediateTrig is %+v", trigSA)
	}
	if got := trigSA.Params["RememberObjects"]; got != "Targeted" {
		t.Fatalf("corpus precondition: RememberObjects$ = %q, want Targeted", got)
	}
	if got := trigSA.Params["ValidTgts"]; got == "" {
		t.Fatal("corpus precondition: the ImmediateTrigger carries no ValidTgts$")
	}

	e, _ := itrFullEngine(t, reg, "Behemoth of Vault 0")
	behemothID := searchMoveByName(t, e, "Behemoth of Vault 0", state.ZBattlefield)
	victim := searchMoveByNameSeat(t, e, 1, "Grizzly Bears", state.ZBattlefield)

	// Precondition: both objects are exactly where the rule reads them.
	if o := e.G.Obj(victim); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("corpus precondition: the victim is not on the battlefield: %+v", e.G.Obj(victim))
	}
	if o := e.G.Obj(behemothID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("corpus precondition: Behemoth is not on the battlefield: %+v", e.G.Obj(behemothID))
	}

	// Let Behemoth's ETB resolve so the controller has its four energy.
	passUntilStackEmpty(t, e, 20)
	energyBefore := e.G.Players[0].Counter("ENERGY")
	if energyBefore != 4 {
		t.Fatalf("precondition: Behemoth's ETB granted %d energy, want 4", energyBefore)
	}

	// Fire the dies trigger and drive to the target ask.
	killOnBattlefield(t, e, behemothID)
	d := passUntilNonPriority(t, e, 40)
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("pending = %+v, want the death trigger's target ask", d)
	}
	pick := -1
	for _, o := range d.Options {
		if o.Obj == victim {
			pick = o.Index
		}
	}
	if pick < 0 {
		t.Fatalf("precondition: the victim %d is not among the offered targets: %+v", victim, d.Options)
	}
	submitChoices(t, e, pick)

	// The unless-pay election: pay the energy.
	pay := passUntilNonPriority(t, e, 40)
	if pay == nil || pay.Kind != decision.KModes || pay.ResumeKind != "unless_pay" {
		t.Fatalf("pending = %+v, want the unless_pay gate", pay)
	}
	if len(pay.Options) == 0 {
		t.Fatal("unless_pay gate offered no pay option")
	}
	submitChoices(t, e, pay.Options[0].Index)
	passUntilStackEmpty(t, e, 40)

	// The energy really left the pool (X = the target's mana value; the victim
	// is a two-mana creature, so 4 -> 2), and the victim really moved.
	if got := e.G.Players[0].Counter("ENERGY"); got != 2 {
		t.Fatalf("energy after paying = %d, want 2 (was 4, X = victim's mana value)", got)
	}
	if o := e.G.Obj(victim); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("victim zone = %+v, want the graveyard (the Destroy body never acted)", o)
	}
	moved := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.Obj == victim && ev.To == state.ZGraveyard {
			moved = true
		}
	}
	if !moved {
		t.Fatal("no MoveZone to the graveyard was logged for the victim")
	}
}

// TestImmediateTriggerRememberObjectsTargetedWithoutCost is the cost-free
// sibling: Back for More's ImmediateTrigger carries the same
// `RememberObjects$ Targeted`, but no cost window (its target is the creature
// card its own ChangeZone returned). The fix must not be cost-specific, so the
// same resolution path feeds the Fight body its returned creature.
func TestImmediateTriggerRememberObjectsTargetedWithoutCost(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)

	back := searchCorpusCard(t, reg, "Back for More")
	trigSA := cards.ResolveSVar(back.Faces[0].SVars, "DBImmediateTrigger")
	if trigSA == nil || trigSA.API != "ImmediateTrigger" {
		t.Fatalf("corpus precondition: Back for More's DBImmediateTrigger is %+v", trigSA)
	}
	if got := trigSA.Params["RememberObjects"]; got != "Targeted" {
		t.Fatalf("corpus precondition: RememberObjects$ = %q, want Targeted", got)
	}

	// The spell's target is a creature card in seat 0's own graveyard; the
	// Fight's second target is a creature seat 1 controls. Drive the cast.
	e, _ := itrFullEngine(t, reg, "Back for More")
	spell := searchMoveByName(t, e, "Back for More", state.ZHand)
	// Seed a creature card in the graveyard to return (a real deck card moved
	// in through the logged event the engine folds).
	gyCard := searchMoveByName(t, e, "Grizzly Bears", state.ZGraveyard)
	if o := e.G.Obj(gyCard); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("precondition: seeded graveyard creature is not in the graveyard: %+v", e.G.Obj(gyCard))
	}
	victim := searchMoveByNameSeat(t, e, 1, "Grizzly Bears", state.ZBattlefield)

	addMana(t, e, 0, "BBBBGG")
	d := e.Pending()
	cast := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == spell {
			cast = o.Index
		}
	}
	if cast < 0 {
		t.Fatalf("Back for More not castable: %+v", d.Options)
	}
	submitChoices(t, e, cast)
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("pending after cast = %+v, want the graveyard-card target ask", d)
	}
	pick := -1
	for _, o := range d.Options {
		if o.Obj == gyCard {
			pick = o.Index
		}
	}
	if pick < 0 {
		t.Fatalf("precondition: the graveyard creature %d is not offered: %+v", gyCard, d.Options)
	}
	submitChoices(t, e, pick)
	// Both seats pass and the spell resolves: the ChangeZone returns the
	// creature, then the ImmediateTrigger's "when you do" half goes on the
	// stack as a reflexive triggered ability (CR 603.12). Its body is a Fight
	// with "up to one target creature you don't control", asked as the
	// ability is put on the stack. Without the RememberObjects$ Targeted fix
	// effImmediateTrigger fell into its catch-all arm and emitted its loud
	// Note instead, and DelayTriggerRemembered resolved to nobody.
	var ask *decision.Decision
	for i := 0; i < 40 && ask == nil; i++ {
		d = e.Pending()
		if d == nil {
			t.Fatal("no decision while the spell resolves")
		}
		if d.Kind == decision.KPriority {
			if len(e.G.Stack) == 0 {
				break
			}
			passPriority(t, e)
			continue
		}
		ask = d
	}
	if ask == nil || ask.Kind != decision.KTarget || ask.Min != 0 || ask.Max != 1 {
		t.Fatalf("after the spell resolved: %+v, want the reflexive Fight's up-to-one target ask", ask)
	}
	if o := e.G.Obj(gyCard); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("returned creature zone = %+v, want the battlefield", o)
	}
	pick = -1
	for _, o := range ask.Options {
		if o.Obj == victim {
			pick = o.Index
		}
	}
	if pick < 0 {
		t.Fatalf("the opponent's creature is not offered as the fight target: %+v", ask.Options)
	}
	submitChoices(t, e, pick)
	passUntilStackEmpty(t, e, 40)
	// The fight happened between the RETURNED creature (the instance's
	// remembered object) and the target: two 2/2s, both dead.
	if o := e.G.Obj(gyCard); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("returned creature zone = %+v, want the graveyard (it fought a 2/2)", o)
	}
	if o := e.G.Obj(victim); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("fight target zone = %+v, want the graveyard (it fought a 2/2)", o)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "ImmediateTrigger RememberObjects$ Targeted is not implemented") {
			t.Fatalf("the ImmediateTrigger body fell into the catch-all arm instead of resolving Targeted: %q", ev.Text)
		}
	}
}
