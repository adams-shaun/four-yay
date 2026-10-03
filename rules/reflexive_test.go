package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// CR 603.12 reflexive triggered abilities ("... When you do, <effect>."),
// spawned by DB$ ImmediateTrigger. Authored fixtures; the part under test is
// that the "when you do" half is a real triggered ability on the stack with
// its own target, not an effect inside the spawning resolution.

const reflexMist = "Name:Drowsy Mist\nManaCost:1 U\nTypes:Instant\n" +
	"A:SP$ GainLife | Defined$ You | LifeAmount$ 1 | SubAbility$ DBWhen | SpellDescription$ x\n" +
	"SVar:DBWhen:DB$ ImmediateTrigger | Execute$ TrigNap | TriggerDescription$ When you do, tap target creature.\n" +
	"SVar:TrigNap:DB$ Tap | ValidTgts$ Creature | TgtPrompt$ Select target creature\n" +
	"Oracle:x\n"

// Two instances: TriggerAmount$ 2 puts two separate abilities on the stack.
const reflexTwin = "Name:Twin Mist\nManaCost:1 U\nTypes:Instant\n" +
	"A:SP$ GainLife | Defined$ You | LifeAmount$ 1 | SubAbility$ DBWhen | SpellDescription$ x\n" +
	"SVar:DBWhen:DB$ ImmediateTrigger | TriggerAmount$ 2 | Execute$ TrigNap | TriggerDescription$ x\n" +
	"SVar:TrigNap:DB$ Tap | ValidTgts$ Creature | TgtPrompt$ Select target creature\n" +
	"Oracle:x\n"

// Static$ True is Forge's static trigger: it resolves immediately, inside the
// spawning resolution, and never uses the stack.
const reflexStatic = "Name:Instant Balm\nManaCost:1 U\nTypes:Instant\n" +
	"A:SP$ GainLife | Defined$ You | LifeAmount$ 1 | SubAbility$ DBWhen | SpellDescription$ x\n" +
	"SVar:DBWhen:DB$ ImmediateTrigger | Static$ True | Execute$ TrigMore\n" +
	"SVar:TrigMore:DB$ GainLife | Defined$ You | LifeAmount$ 5\n" +
	"Oracle:x\n"

// The untargeted twin of reflexStatic without the flag: still a stack object.
const reflexBalm = "Name:Slow Balm\nManaCost:1 U\nTypes:Instant\n" +
	"A:SP$ GainLife | Defined$ You | LifeAmount$ 1 | SubAbility$ DBWhen | SpellDescription$ x\n" +
	"SVar:DBWhen:DB$ ImmediateTrigger | Execute$ TrigMore | TriggerDescription$ x\n" +
	"SVar:TrigMore:DB$ GainLife | Defined$ You | LifeAmount$ 5\n" +
	"Oracle:x\n"

// The spawning resolution's remembered set rides onto the reflexive ability.
const reflexRecall = "Name:Recalling Mist\nManaCost:1 U\nTypes:Instant\n" +
	"A:SP$ Mill | NumCards$ 1 | Defined$ You | RememberMilled$ True | SubAbility$ DBWhen | SpellDescription$ x\n" +
	"SVar:DBWhen:DB$ ImmediateTrigger | Execute$ TrigBack | RememberObjects$ Remembered | SubAbility$ DBCleanup | TriggerDescription$ x\n" +
	"SVar:TrigBack:DB$ ChangeZone | Defined$ DelayTriggerRememberedLKI | Origin$ Graveyard | Destination$ Hand\n" +
	"SVar:DBCleanup:DB$ Cleanup | ClearRemembered$ True\n" +
	"Oracle:x\n"

const reflexOx = "Name:Pack Ox\nManaCost:3 G\nTypes:Creature Ox\nPT:4/4\nOracle:x\n"
const reflexCat = "Name:Alley Cat\nManaCost:W\nTypes:Creature Cat\nPT:1/1\nOracle:x\n"

// castReflex casts the fixture (no cast-time targets) and passes priority
// until the spell itself has left the stack, returning the first decision
// posed after that.
func castReflex(t *testing.T, e *Engine, spell state.ObjID) *decision.Decision {
	t.Helper()
	addMana(t, e, 0, "1U")
	submitChoices(t, e, castOptionFor(t, e, spell).Index)
	for i := 0; i < 20; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no decision while the spell resolves")
		}
		if e.G.Obj(spell).Zone != state.ZStack {
			return d
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("unexpected %s decision before the spell resolved: %+v", d.Kind, d)
		}
		passPriority(t, e)
	}
	t.Fatal("the spell never resolved")
	return nil
}

func reflexTargetOption(t *testing.T, d *decision.Decision, obj state.ObjID) int {
	t.Helper()
	for _, o := range d.Options {
		if o.Obj == obj {
			return o.Index
		}
	}
	t.Fatalf("object %d is not offered: %+v", obj, d.Options)
	return -1
}

// TestReflexiveTriggerGoesOnTheStackWithItsTarget: after the spell resolves
// the "when you do" half is put on the stack as a triggered ability, its
// target chosen as it is put there (a KTarget, CR 603.3d), and the effect
// happens only when that ability resolves -- both players get priority in
// between.
func TestReflexiveTriggerGoesOnTheStackWithItsTarget(t *testing.T) {
	e, cfg, spell := newFixtureDeck(t, 411, reflexMist, reflexOx)
	ox := moveSeeded(t, e, 0, reflexOx, state.ZBattlefield)
	life := e.G.Players[0].Life

	d := castReflex(t, e, spell)
	if got := e.G.Players[0].Life; got != life+1 {
		t.Fatalf("life = %d, want %d: the spell's own effect did not happen", got, life+1)
	}
	if d.Kind != decision.KTarget {
		t.Fatalf("after the spell resolved: %s %+v, want the reflexive trigger's target ask", d.Kind, d)
	}
	if len(e.G.Stack) != 1 || e.G.Obj(e.G.Stack[0]).Ability == nil {
		t.Fatalf("stack = %v, want exactly the reflexive triggered ability", e.G.Stack)
	}
	submitChoices(t, e, reflexTargetOption(t, d, ox))
	if d = e.Pending(); d == nil || d.Kind != decision.KPriority {
		t.Fatalf("after targeting: %+v, want priority with the ability on the stack", d)
	}
	if e.G.Obj(ox).Tapped {
		t.Fatal("the Ox is already tapped: the reflexive half resolved before anyone received priority")
	}
	if ab := e.G.Obj(e.G.Stack[0]); len(ab.Targets) != 1 || ab.Targets[0].Obj != ox {
		t.Fatalf("ability targets = %+v, want the Ox", ab.Targets)
	}
	passUntilStackEmpty(t, e, 20)
	if !e.G.Obj(ox).Tapped {
		t.Fatal("the Ox is untapped after the reflexive ability resolved")
	}
	replayCheck(t, e, cfg)
}

// TestReflexiveTriggerAmountMintsOneAbilityEach: TriggerAmount$ 2 is two
// reflexive abilities, each with its own target.
func TestReflexiveTriggerAmountMintsOneAbilityEach(t *testing.T) {
	e, cfg, spell := newFixtureDeck(t, 412, reflexTwin, reflexOx, reflexCat)
	ox := moveSeeded(t, e, 0, reflexOx, state.ZBattlefield)
	cat := moveSeeded(t, e, 0, reflexCat, state.ZBattlefield)

	d := castReflex(t, e, spell)
	picks := []state.ObjID{ox, cat}
	asked := 0
	for i := 0; i < 6 && d != nil && d.Kind != decision.KPriority; i++ {
		switch d.Kind {
		case decision.KTarget:
			submitChoices(t, e, reflexTargetOption(t, d, picks[asked]))
			asked++
		default:
			// The controller orders their two simultaneous triggers.
			idx := make([]int, 0, len(d.Options))
			for _, o := range d.Options {
				idx = append(idx, o.Index)
			}
			submitChoices(t, e, idx...)
		}
		d = e.Pending()
	}
	if asked != 2 {
		t.Fatalf("posed %d target asks, want 2 (one per reflexive ability)", asked)
	}
	if len(e.G.Stack) != 2 {
		t.Fatalf("stack depth = %d, want 2 reflexive abilities", len(e.G.Stack))
	}
	passUntilStackEmpty(t, e, 20)
	if !e.G.Obj(ox).Tapped || !e.G.Obj(cat).Tapped {
		t.Fatalf("tapped ox=%v cat=%v, want both", e.G.Obj(ox).Tapped, e.G.Obj(cat).Tapped)
	}
	replayCheck(t, e, cfg)
}

// TestStaticImmediateTriggerStaysInline: Static$ True resolves inside the
// spawning resolution; the same body without the flag waits on the stack.
func TestStaticImmediateTriggerStaysInline(t *testing.T) {
	t.Run("static", func(t *testing.T) {
		e, cfg, spell := newFixtureDeck(t, 413, reflexStatic)
		life := e.G.Players[0].Life
		castReflex(t, e, spell)
		if len(e.G.Stack) != 0 || e.G.Players[0].Life != life+6 {
			t.Fatalf("stack=%v life=%d, want an empty stack and %d (resolved inline)", e.G.Stack, e.G.Players[0].Life, life+6)
		}
		replayCheck(t, e, cfg)
	})
	t.Run("reflexive", func(t *testing.T) {
		e, cfg, spell := newFixtureDeck(t, 414, reflexBalm)
		life := e.G.Players[0].Life
		castReflex(t, e, spell)
		if len(e.G.Stack) != 1 || e.G.Players[0].Life != life+1 {
			t.Fatalf("stack=%v life=%d, want the untargeted reflexive ability on the stack and %d", e.G.Stack, e.G.Players[0].Life, life+1)
		}
		passUntilStackEmpty(t, e, 20)
		if got := e.G.Players[0].Life; got != life+6 {
			t.Fatalf("life = %d, want %d after the reflexive ability resolved", got, life+6)
		}
		replayCheck(t, e, cfg)
	})
}

// TestReflexiveTriggerCarriesTheRememberedSet: what the spawning resolution
// remembered is what the reflexive ability acts on, even though the spawner's
// own Cleanup ran long before the ability resolves.
func TestReflexiveTriggerCarriesTheRememberedSet(t *testing.T) {
	e, cfg, spell := newFixtureDeck(t, 415, reflexRecall)
	lib := e.G.Zone(state.ZLibrary, 0)
	top := lib[len(lib)-1]
	if e.G.Obj(top).Zone != state.ZLibrary {
		t.Fatal("PRECONDITION: no library card to mill")
	}
	castReflex(t, e, spell)
	milled := state.ObjID(0)
	for _, id := range e.G.Zone(state.ZGraveyard, 0) {
		if id != spell {
			milled = id
		}
	}
	if milled == 0 {
		t.Fatal("nothing was milled")
	}
	if len(e.G.Stack) != 1 {
		t.Fatalf("stack = %v, want the reflexive ability", e.G.Stack)
	}
	passUntilStackEmpty(t, e, 20)
	if z := e.G.Obj(milled).Zone; z != state.ZHand {
		t.Fatalf("milled card zone = %v, want hand (the reflexive ability returns the remembered card)", z)
	}
	replayCheck(t, e, cfg)
}
