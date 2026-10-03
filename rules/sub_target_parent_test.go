package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// A SubAbility$ whose own ValidTgts$ names the PARENT's target ("exile up to
// one target Equipment attached to that creature"). Authored fixtures; the
// part under test is the sub's `<Type>.AttachedTo ParentTarget` target spec
// and what the sub does when nothing can be chosen for it.

const subTgtBlastExile = "Name:Stripping Blast\nManaCost:1 R\nTypes:Instant\n" +
	"A:SP$ DealDamage | NumDmg$ 2 | ValidTgts$ Creature | SubAbility$ DBStrip | SpellDescription$ x\n" +
	"SVar:DBStrip:DB$ ChangeZone | Origin$ Battlefield | Destination$ Exile | ValidTgts$ Equipment.AttachedTo ParentTarget | TargetMin$ 0 | TargetMax$ 1\n" +
	"Oracle:x\n"

// The same shape through the generic (non-ChangeZone) mid-resolution ask.
const subTgtBlastDestroy = "Name:Rusting Blast\nManaCost:1 R\nTypes:Instant\n" +
	"A:SP$ DealDamage | NumDmg$ 2 | ValidTgts$ Creature | SubAbility$ DBRust | SpellDescription$ x\n" +
	"SVar:DBRust:DB$ Destroy | ValidTgts$ Equipment.AttachedTo ParentTarget | TargetMin$ 0 | TargetMax$ 1\n" +
	"Oracle:x\n"

const (
	subTgtOx    = "Name:Pack Ox\nManaCost:3 G\nTypes:Creature Ox\nPT:4/4\nOracle:x\n"
	subTgtBlade = "Name:Tin Blade\nManaCost:1\nTypes:Artifact Equipment\nOracle:x\n"
	subTgtHelm  = "Name:Tin Helm\nManaCost:1\nTypes:Artifact Equipment\nOracle:x\n"
	subTgtLoose = "Name:Loose Buckler\nManaCost:1\nTypes:Artifact Equipment\nOracle:x\n"
)

// castAtOx casts the fixture spell at the Ox and passes until either the
// stack is empty or a non-priority decision is pending.
func castAtOx(t *testing.T, e *Engine, spell, ox state.ObjID) *decision.Decision {
	t.Helper()
	addMana(t, e, 0, "1R")
	submitChoices(t, e, castOptionFor(t, e, spell).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("after proposing the spell: %+v, want the root target ask", d)
	}
	pick := -1
	for _, o := range d.Options {
		if o.Obj == ox {
			pick = o.Index
		}
	}
	if pick < 0 {
		t.Fatalf("the Ox is not offered as the root target: %+v", d.Options)
	}
	submitChoices(t, e, pick)
	for i := 0; i < 20 && len(e.G.Stack) > 0; i++ {
		d = e.Pending()
		if d == nil {
			t.Fatal("no decision with a spell on the stack")
		}
		if d.Kind != decision.KPriority {
			return d
		}
		passPriority(t, e)
	}
	return nil
}

// TestSubTargetAttachedToParentTargetIsOffered: at resolution the sub's own
// target ask offers exactly the Equipment attached to the parent's target --
// not the unattached one, not nothing -- and the chosen one is exiled while
// the damaged creature stays (CR 115.1, 608.2b).
func TestSubTargetAttachedToParentTargetIsOffered(t *testing.T) {
	for _, tc := range []struct {
		name, spell string
		gone        state.Zone
	}{
		{"change_zone", subTgtBlastExile, state.ZExile},
		{"generic_api", subTgtBlastDestroy, state.ZGraveyard},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, cfg, spell := newFixtureDeck(t, 311, tc.spell, subTgtOx, subTgtBlade, subTgtHelm, subTgtLoose)
			ox := moveSeeded(t, e, 0, subTgtOx, state.ZBattlefield)
			blade := moveSeeded(t, e, 0, subTgtBlade, state.ZBattlefield)
			helm := moveSeeded(t, e, 0, subTgtHelm, state.ZBattlefield)
			loose := moveSeeded(t, e, 0, subTgtLoose, state.ZBattlefield)
			e.emit(events.Event{Kind: events.Attach, Obj: blade, IDs: []state.ObjID{ox}})
			e.emit(events.Event{Kind: events.Attach, Obj: helm, IDs: []state.ObjID{ox}})

			d := castAtOx(t, e, spell, ox)
			if d == nil || d.Kind != decision.KChoose {
				t.Fatalf("resolution posed %+v, want the sub's own Equipment target ask", d)
			}
			if d.Min != 0 || d.Max != 1 {
				t.Fatalf("sub target bounds = %d..%d, want 0..1 (up to one)", d.Min, d.Max)
			}
			pick, offered := -1, map[state.ObjID]bool{}
			for _, o := range d.Options {
				offered[o.Obj] = true
				if o.Obj == helm {
					pick = o.Index
				}
			}
			if len(d.Options) != 2 || !offered[blade] || !offered[helm] || offered[loose] || offered[ox] {
				t.Fatalf("sub target offer = %+v, want exactly the two Equipment attached to the Ox", d.Options)
			}
			submitChoices(t, e, pick)
			passUntilStackEmpty(t, e, 20)

			if z := e.G.Obj(helm).Zone; z != tc.gone {
				t.Fatalf("chosen Equipment zone = %v, want %v", z, tc.gone)
			}
			if z := e.G.Obj(blade).Zone; z != state.ZBattlefield {
				t.Fatalf("unchosen Equipment zone = %v, want battlefield", z)
			}
			if o := e.G.Obj(ox); o.Zone != state.ZBattlefield || o.Damage != 2 {
				t.Fatalf("Ox zone=%v damage=%d, want battlefield with 2 damage", o.Zone, o.Damage)
			}
			replayCheck(t, e, cfg)
		})
	}
}

// TestSubWithNoLegalTargetDoesNotActOnTheParentTarget: with nothing attached
// the sub has no legal target, so it does nothing. It must NOT fall through
// to the parent's target -- the creature that was only damaged stays on the
// battlefield (it used to be exiled / destroyed outright).
func TestSubWithNoLegalTargetDoesNotActOnTheParentTarget(t *testing.T) {
	for _, tc := range []struct{ name, spell string }{
		{"change_zone", subTgtBlastExile},
		{"generic_api", subTgtBlastDestroy},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, cfg, spell := newFixtureDeck(t, 312, tc.spell, subTgtOx, subTgtLoose)
			ox := moveSeeded(t, e, 0, subTgtOx, state.ZBattlefield)
			loose := moveSeeded(t, e, 0, subTgtLoose, state.ZBattlefield)

			if d := castAtOx(t, e, spell, ox); d != nil {
				t.Fatalf("resolution posed %+v, want no ask (no Equipment is attached to the target)", d)
			}
			if o := e.G.Obj(ox); o.Zone != state.ZBattlefield || o.Damage != 2 {
				t.Fatalf("Ox zone=%v damage=%d, want battlefield with 2 damage: the sub acted on the parent's target", o.Zone, o.Damage)
			}
			if z := e.G.Obj(loose).Zone; z != state.ZBattlefield {
				t.Fatalf("unattached Equipment zone = %v, want battlefield", z)
			}
			replayCheck(t, e, cfg)
		})
	}
}
