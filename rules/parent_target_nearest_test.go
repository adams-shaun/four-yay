package rules

import (
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestFlourishingGrappleBearsDealPowerToAngel: Flourishing Grapple targets an
// opponent's red-or-white creature (the root Animate) and a creature its
// caster controls (the DBPump link); its untargeted DBDamage link reads
// `DamageSource$ ParentTarget` and `NumDmg$ X` with X =
// ParentTargeted$CardPower. Forge's ParentTarget names the targets of the
// NEAREST targeting parent in the chain -- DBPump's Grizzly Bears, not the
// root's Serra Angel -- so the Bears deal 2 damage to the Angel (the Oracle
// text, and XMage agrees).
func TestFlourishingGrappleBearsDealPowerToAngel(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{lookup(t, reg, "Flourishing Grapple"), lookup(t, reg, "Grizzly Bears")},
		[]*cards.Card{lookup(t, reg, "Serra Angel")})
	bears := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	angel := moveByName(t, e, 1, "Serra Angel", state.ZBattlefield)
	grapple := moveByName(t, e, 0, "Flourishing Grapple", state.ZHand)
	e.pending = nil
	addMana(t, e, 0, "G")
	submitChoices(t, e, castOptionFor(t, e, grapple).Index)
	castGrappleAnswering(t, e, angel, bears)

	if o := e.G.Obj(angel); o.Zone != state.ZBattlefield || o.Damage != 2 {
		t.Fatalf("Serra Angel zone=%v damage=%d, want battlefield with 2 damage from the Bears", o.Zone, o.Damage)
	}
	if o := e.G.Obj(bears); o.Damage != 0 {
		t.Fatalf("Grizzly Bears damage=%d, want 0 (the Bears are the source, not a recipient)", o.Damage)
	}
	replayCheck(t, e, cfg)
}

// castGrappleAnswering answers every target ask the cast and its resolution
// pose -- the root's (the opponent's creature) and the DBPump link's (the
// caster's creature), whether announced at cast or asked mid-resolution --
// and passes priority until the stack is empty.
func castGrappleAnswering(t *testing.T, e *Engine, root, link state.ObjID) {
	t.Helper()
	for i := 0; i < 40; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no pending decision")
		}
		if d.Kind == decision.KPriority {
			if len(e.G.Stack) == 0 {
				return
			}
			passPriority(t, e)
			continue
		}
		pick := -1
		for _, want := range []state.ObjID{root, link} {
			for _, o := range d.Options {
				if o.Obj == want && pick < 0 {
					pick = o.Index
				}
			}
		}
		if pick < 0 {
			t.Fatalf("decision %v offers neither the root nor the link target: %+v", d.Kind, d.Options)
		}
		submitChoices(t, e, pick)
	}
	t.Fatal("stack never emptied")
}

// TestHomesicknessStunsTheTappedCreatures: Homesickness's root targets a
// PLAYER and its DBTap link targets up to two creatures; the untargeted
// DBPutCounter link's `Defined$ ParentTarget` is DBTap's creatures (the
// nearest targeting parent), so the tapped creature gets the stun counter --
// it used to read the root's player and put the counters nowhere.
func TestHomesicknessStunsTheTappedCreatures(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{lookup(t, reg, "Homesickness")},
		[]*cards.Card{lookup(t, reg, "Serra Angel")})
	angel := moveByName(t, e, 1, "Serra Angel", state.ZBattlefield)
	spell := moveByName(t, e, 0, "Homesickness", state.ZHand)
	e.pending = nil
	addMana(t, e, 0, "UUUUUU")
	submitChoices(t, e, castOptionFor(t, e, spell).Index)
	for i := 0; i < 40; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no pending decision")
		}
		if d.Kind == decision.KPriority {
			if len(e.G.Stack) == 0 {
				break
			}
			passPriority(t, e)
			continue
		}
		pick := -1
		for _, o := range d.Options {
			if (o.Kind == "player" && o.Player == 1) || o.Obj == angel {
				pick = o.Index
			}
		}
		if pick < 0 {
			t.Fatalf("decision %v offers neither seat 1 nor the Angel: %+v", d.Kind, d.Options)
		}
		submitChoices(t, e, pick)
	}
	o := e.G.Obj(angel)
	if !o.Tapped || o.Counter("STUN") != 1 {
		t.Fatalf("Serra Angel tapped=%v stun=%d, want tapped with one stun counter", o.Tapped, o.Counter("STUN"))
	}
	replayCheck(t, e, cfg)
}

// parentTargetLinkCarriers is every corpus card with an ability chain (A:
// lines, trigger Execute$ bodies, replacement ReplaceWith$ bodies) in which an
// UNTARGETED SubAbility$ link reads ParentTarget/ParentTargeted* -- directly
// in a parameter or through an SVar a parameter names -- AFTER a TARGETING
// SubAbility$ link. Forge resolves the referent to the nearest targeting
// parent (SpellAbility.getParentTargetingCard), so these links read that
// link's targets, not the root's: effects.parentLinkTargets. Measured
// 2026-10-03 at FORGE_REF.
var parentTargetLinkCarriers = []string{
	"Barbarian Guides",
	"Chandra's Revolution",
	"Cruel Entertainment",
	"Fight for the Throne",
	"Flourishing Grapple",
	"Glyph of Delusion",
	"Homesickness",
	"Intruder's Inquisition",
	"Mind Spiral",
	"Mindblaze",
	"Panther Pounce",
	"Rhino, Terrible Trampler",
	"Skyshroud Ambush",
	"Stolen Uniform",
	"Stunning Shot",
	"Survey Mechan",
	"Thorin, Mountain-king",
	"Trygon Prime",
	"Winterthorn Blessing",
}

// TestParentTargetLinkCensus pins the class both ways: a carrier that drifts
// out of the shape, or a new corpus chain that enters it, fails here.
func TestParentTargetLinkCensus(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	seen := map[string]bool{}
	for _, c := range reg.AllCards() {
		for _, f := range c.Faces {
			if f == nil {
				continue
			}
			var roots []*cards.SA
			roots = append(roots, f.Abilities...)
			for _, tr := range f.Triggers {
				roots = append(roots, tr.Effect)
			}
			for _, r := range f.Repls {
				roots = append(roots, r.With)
			}
			for _, root := range roots {
				if readsParentTargetAfterTargetingLink(f, root) {
					seen[c.Faces[0].Name] = true
				}
			}
		}
	}
	got := make([]string, 0, len(seen))
	for name := range seen {
		got = append(got, name)
	}
	sort.Strings(got)
	want := append([]string(nil), parentTargetLinkCarriers...)
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("ParentTarget-after-targeting-link carriers:\n  got  %q\n  want %q", got, want)
	}
}

// readsParentTargetAfterTargetingLink is the census shape over one chain.
func readsParentTargetAfterTargetingLink(f *cards.Face, root *cards.SA) bool {
	targetingLink := false
	for d, sa := 0, root; sa != nil && d < 64; d, sa = d+1, sa.Sub {
		_, own := sa.Params["ValidTgts"]
		if d > 0 && !own && targetingLink && linkReadsParentTarget(f, sa) {
			return true
		}
		if d > 0 && own {
			targetingLink = true
		}
	}
	return false
}

func linkReadsParentTarget(f *cards.Face, sa *cards.SA) bool {
	for k, v := range sa.Params {
		if k == "SubAbility" {
			continue
		}
		if strings.Contains(v, "ParentTarget") {
			return true
		}
		sv, ok := f.SVars[strings.TrimSpace(v)]
		if ok && strings.Contains(sv, "ParentTarget") && !isAbilityBody(sv) {
			return true
		}
	}
	return false
}

func isAbilityBody(sv string) bool {
	for _, p := range []string{"DB$", "AB$", "SP$", "Mode$"} {
		if strings.HasPrefix(strings.TrimSpace(sv), p) {
			return true
		}
	}
	return false
}
