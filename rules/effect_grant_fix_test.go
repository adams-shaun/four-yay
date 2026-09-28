package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Fix round for the AGENTS.md "Known approximations" row about api:Effect's
// general registration (cli-20260922T225138Z-504a0e97):
//
//   1. parseStaticEffectGrant advertised AddPower$/AddToughness$ (the layer-7c
//      ADDITIVE parameters) but registered no LPT/SubModify effect, so an
//      Effect-delivered Vivien Reid emblem gave only its keywords. The printed
//      static scanner has always registered both, so the two routes disagreed.
//   2. effEffect's Triggers$ loop registered only the BecomeMonarch shape;
//      every other Effect-delivered trigger was silently dropped. The
//      SpellCast/ChangesZone/Phase modes now reach the trigger registry
//      through the same delayed registration the DelayedTrigger SA uses.
//
// Fixtures are authored inline (the licensing rule): Forge corpus .txt is
// GPL-3.0 and must never be committed.

// effectNoteTexts collects every Note text emitted so far. A test that asserts
// a characteristic changed must also assert the relevant fallback Note is
// ABSENT, or it would pass with the registration reverted to a no-op that
// happens to leave the board alone.
func effectNoteTexts(e *Engine) []string {
	var out []string
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note {
			out = append(out, ev.Text)
		}
	}
	return out
}

func effectNotesContaining(e *Engine, sub string) []string {
	var out []string
	for _, t := range effectNoteTexts(e) {
		if strings.Contains(t, sub) {
			out = append(out, t)
		}
	}
	return out
}

// TestEffectDeliveredContinuousGrantAppliesAddPower pins the layer-7c additive
// P/T half (the finding's Vivien Reid shape): an Effect-delivered
// `AddPower$ +2 | AddToughness$ +2 | AddKeyword$ Vigilance` must register BOTH
// layers. Before the fix only the keyword registration survived, so a 2/2
// creature stayed 2/2.
func TestEffectDeliveredContinuousGrantAppliesAddPower(t *testing.T) {
	t.Parallel()
	grant := card(t, "Name:EmblemOfMight\nManaCost:U\nTypes:Sorcery\n"+
		"A:SP$ Effect | StaticAbilities$ STMight\n"+
		"SVar:STMight:Mode$ Continuous | Affected$ Creature.YouCtrl | AddPower$ +2 | AddToughness$ +2 | AddKeyword$ Vigilance\n"+
		"Oracle:x\n")
	e := handEngine(t, grant)
	e.G.Players[0].Pool[state.MU] = 1
	bear := onBoard(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")

	// Precondition: the object is on the battlefield and neither the P/T nor
	// the keyword is already present before the grant resolves.
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: bear not on the battlefield: %+v", o)
	}
	if p, tg := e.Power(bear), e.Toughness(bear); p != 2 || tg != 2 {
		t.Fatalf("precondition: printed P/T = %d/%d, want 2/2", p, tg)
	}
	if e.HasKeyword(bear, "Vigilance") {
		t.Fatal("precondition: the bear already has Vigilance")
	}

	e.askPriority(0)
	castFirst(t, e, "cast")
	passUntilStackEmpty(t, e, 8)
	if len(e.G.Stack) != 0 {
		t.Fatalf("EmblemOfMight did not resolve: stack %v", e.G.Stack)
	}

	if notes := effectNotesContaining(e, "continuous effect Continuous unimplemented"); len(notes) != 0 {
		t.Fatalf("the general Continuous grant fell to the unimplemented Note: %v", notes)
	}
	if p, tg := e.Power(bear), e.Toughness(bear); p != 4 || tg != 4 {
		t.Fatalf("Effect-delivered AddPower$/AddToughness$ did not reach the layer walk: P/T = %d/%d, want 4/4", p, tg)
	}
	if !e.HasKeyword(bear, "Vigilance") {
		t.Fatal("the same body's AddKeyword$ did not register alongside its P/T")
	}
}

// TestEffectDeliveredContinuousGrantAppliesDynamicPTExpression pins the
// EXPRESSION semantics the finding called out: AddPower$/AddToughness$ carry a
// raw expression (here an SVar whose body is a Count$), not a numeric the
// parser collapsed. Two creatures you control make the count 2, so each
// affected creature gets +2; the two-value precondition (one vs two
// creatures) makes a hard-coded +1 or +2 fail.
func TestEffectDeliveredContinuousGrantAppliesDynamicPTExpression(t *testing.T) {
	t.Parallel()
	grant := card(t, "Name:EmblemOfBeasts\nManaCost:U\nTypes:Sorcery\n"+
		"A:SP$ Effect | StaticAbilities$ STBeasts\n"+
		"SVar:STBeasts:Mode$ Continuous | Affected$ Creature.YouCtrl | AddPower$ Might | AddToughness$ Might\n"+
		"SVar:Might:Count$Valid Creature.YouCtrl\n"+
		"Oracle:x\n")
	e := handEngine(t, grant)
	e.G.Players[0].Pool[state.MU] = 1
	a := onBoard(t, e, 0, "Name:Alpha\nManaCost:G\nTypes:Creature Elf\nPT:1/1\nOracle:x\n")
	b := onBoard(t, e, 0, "Name:Beta\nManaCost:G\nTypes:Creature Elf\nPT:1/1\nOracle:x\n")

	// Precondition: exactly the two affected creatures are on the battlefield
	// you control, so the Count$ the expression reads is genuinely two.
	if n := len(e.G.Zone(state.ZBattlefield, 0)); n != 2 {
		t.Fatalf("precondition: battlefield has %d objects, want 2", n)
	}
	if p := e.Power(a); p != 1 {
		t.Fatalf("precondition: Alpha power = %d, want 1", p)
	}

	e.askPriority(0)
	castFirst(t, e, "cast")
	passUntilStackEmpty(t, e, 8)

	if notes := effectNotesContaining(e, "continuous effect Continuous unimplemented"); len(notes) != 0 {
		t.Fatalf("the general Continuous grant fell to the unimplemented Note: %v", notes)
	}
	// The expression is a live count of the affected set: 2, not a literal 1
	// or a collapsed 0.
	if p, tg := e.Power(a), e.Toughness(a); p != 3 || tg != 3 {
		t.Fatalf("Alpha P/T = %d/%d, want 3/3 (1/1 base + Count$Valid 2)", p, tg)
	}
	if p, tg := e.Power(b), e.Toughness(b); p != 3 || tg != 3 {
		t.Fatalf("Beta P/T = %d/%d, want 3/3 (1/1 base + Count$Valid 2)", p, tg)
	}
}

// TestEffectDeliveredChangesZoneTriggerFires pins the ChangesZone half of the
// general Triggers$ registration (the Beck / First Day of Class shape). The
// Effect arms a one-shot "whenever a creature enters" promise; casting a
// creature afterwards must fire it. The trigger body loses life, an
// unambiguous observable, and the test also asserts the registration exists
// (the precondition the fire depends on).
func TestEffectDeliveredChangesZoneTriggerFires(t *testing.T) {
	t.Parallel()
	promise := card(t, "Name:CallTheHunt\nManaCost:U\nTypes:Sorcery\n"+
		"A:SP$ Effect | Triggers$ TrigEnter\n"+
		"SVar:TrigEnter:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Creature.YouCtrl | TriggerZones$ Command | Execute$ TrigPain\n"+
		"SVar:TrigPain:DB$ LoseLife | Defined$ You | LifeAmount$ 2\n"+
		"Oracle:x\n")
	creature := card(t, "Name:FreeBear\nManaCost:0\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e := handEngine(t, promise, creature)
	e.G.Players[0].Pool[state.MU] = 1

	e.askPriority(0)
	castFirst(t, e, "cast")
	passUntilStackEmpty(t, e, 8)

	// Precondition: the ChangesZone registration reached the delayed set.
	// Without it the later cast could not fire anything and the assertion
	// below would pass vacuously.
	var reg *state.DelayedTrigger
	for i := range e.G.Delayed {
		if e.G.Delayed[i].EventMode == "ChangesZone" {
			reg = &e.G.Delayed[i]
		}
	}
	if reg == nil {
		t.Fatalf("no ChangesZone delayed registration after the Effect resolved: %+v", e.G.Delayed)
	}
	if reg.Trigger != "TrigEnter" || reg.Execute != "TrigPain" {
		t.Fatalf("registration = %+v, want Trigger TrigEnter / Execute TrigPain", *reg)
	}
	if notes := effectNotesContaining(e, "continuous effect trigger ChangesZone unimplemented"); len(notes) != 0 {
		t.Fatalf("ChangesZone trigger was dropped with a Note: %v", notes)
	}

	lifeBefore := e.G.Players[0].Life
	// Cast the creature: it resolves onto the battlefield, which emits the
	// MoveZone the registration is armed on.
	e.askPriority(0)
	castFirst(t, e, "cast")
	passUntilStackEmpty(t, e, 12)

	if !e.G.Obj(e.G.Zone(state.ZBattlefield, 0)[len(e.G.Zone(state.ZBattlefield, 0))-1]).Face().IsCreature() {
		t.Fatal("precondition: the cast creature did not reach the battlefield")
	}
	if got := e.G.Players[0].Life; got != lifeBefore-2 {
		t.Fatalf("life = %d, want %d: the Effect-delivered ChangesZone trigger never fired", got, lifeBefore-2)
	}
}

// TestEffectDeliveredSpellCastTriggerFires pins the SpellCast half (the Bonus
// Round / swiftspear shape): the Effect arms "whenever you cast a spell", and
// a later cast fires it.
func TestEffectDeliveredSpellCastTriggerFires(t *testing.T) {
	t.Parallel()
	promise := card(t, "Name:ArcaneEcho\nManaCost:U\nTypes:Sorcery\n"+
		"A:SP$ Effect | Triggers$ TrigCast\n"+
		"SVar:TrigCast:Mode$ SpellCast | ValidCard$ Instant,Sorcery | ValidActivatingPlayer$ You | TriggerZones$ Command | Execute$ TrigPain\n"+
		"SVar:TrigPain:DB$ LoseLife | Defined$ You | LifeAmount$ 2\n"+
		"Oracle:x\n")
	spell := card(t, "Name:FreeSpark\nManaCost:0\nTypes:Instant\nA:SP$ Draw | Defined$ You | NumCards$ 0\nOracle:x\n")
	e := handEngine(t, promise, spell)
	e.G.Players[0].Pool[state.MU] = 1

	e.askPriority(0)
	castFirst(t, e, "cast")
	passUntilStackEmpty(t, e, 8)

	var found bool
	for i := range e.G.Delayed {
		if e.G.Delayed[i].EventMode == "SpellCast" && e.G.Delayed[i].Trigger == "TrigCast" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no SpellCast delayed registration after the Effect resolved: %+v", e.G.Delayed)
	}
	if notes := effectNotesContaining(e, "continuous effect trigger SpellCast unimplemented"); len(notes) != 0 {
		t.Fatalf("SpellCast trigger was dropped with a Note: %v", notes)
	}

	lifeBefore := e.G.Players[0].Life
	e.askPriority(0)
	castFirst(t, e, "cast")
	passUntilStackEmpty(t, e, 12)

	if got := e.G.Players[0].Life; got != lifeBefore-2 {
		t.Fatalf("life = %d, want %d: the Effect-delivered SpellCast trigger never fired", got, lifeBefore-2)
	}
}
