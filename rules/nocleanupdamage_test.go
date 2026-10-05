package rules

// stat:NoCleanupDamage (CR 514.2) — "Damage isn't removed from this creature
// during cleanup steps." The cleanup step's damage-removal pass
// (rules/combat.go cleanupBody) removed every permanent's marked damage
// unconditionally; a carrier of the static must keep it. Ancient Adamantoise
// is the Standard carrier, six more exist corpus-wide.
//
// The test marks damage on two creatures -- a carrier and a control -- runs
// the real cleanup body, and asserts only the control is healed. Both
// preconditions are asserted so a vacuous setup (no static, no damage) fails
// loudly.

import (
	"sort"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
)

const noCleanupDamageSrc = "Name:Test Adamantoise\nManaCost:5 G G G\n" +
	"Types:Creature Turtle\nPT:8/20\n" +
	"S:Mode$ NoCleanupDamage | ValidCard$ Card.Self | Description$ Damage isn't removed from this creature during cleanup steps.\n" +
	"Oracle:x\n"

func TestNoCleanupDamageStaticKeepsMarkedDamage(t *testing.T) {
	t.Parallel()
	e := threeSeatEngine(t)
	turtle := onBoardCard(t, e, 0, card(t, noCleanupDamageSrc))
	control := onBoardCard(t, e, 0, card(t, "Name:Test Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"))

	// Preconditions: the static is live and selects the carrier, both are
	// creatures, and both carry marked damage to start.
	if got := e.activeStatics("NoCleanupDamage"); len(got) != 1 {
		t.Fatalf("precondition: activeStatics(NoCleanupDamage) = %d entries, want 1", len(got))
	}
	if !e.IsCreature(turtle) || !e.IsCreature(control) {
		t.Fatal("precondition: both fixtures must be creatures on the battlefield")
	}
	e.G.Obj(turtle).Damage = 3
	e.G.Obj(control).Damage = 2

	e.cleanupBody()

	if got := e.G.Obj(turtle).Damage; got != 3 {
		t.Fatalf("NoCleanupDamage carrier lost marked damage in cleanup: Damage = %d, want 3", got)
	}
	if got := e.G.Obj(control).Damage; got != 0 {
		t.Fatalf("control creature's damage not removed in cleanup: Damage = %d, want 0", got)
	}
}

// TestNoCleanupDamagePrimitiveIsRegistered pins the support declaration the
// skip gate reads.
func TestNoCleanupDamagePrimitiveIsRegistered(t *testing.T) {
	if !effects.Supported()["stat:NoCleanupDamage"] {
		t.Fatal(`effects.Supported() is missing "stat:NoCleanupDamage"`)
	}
}

// TestNoCleanupDamageClassCensus pins the corpus-wide carrier set so a
// corpus-pin bump that adds a new carrier fails loudly.
//
// This is the PRINTED-STATIC class (S:Mode$ NoCleanupDamage), which is the set
// the coverage walk emits `stat:NoCleanupDamage` for. Three further corpus
// cards deliver the mode through `Animate | staticAbilities$` (Melt Through,
// Switchgrass Grazer, Victory of the Pyrohammer); that delivery route is not
// registered for this mode (effects/leavebattlefield.go
// registerAnimateStaticAbilities accepts only the cost statics and
// CantSacrifice/CantBlockUnless/CantAttackUnless), so those cards neither carry
// the coverage token nor are enforced. That remainder is filed as a follow-up
// ticket; adding a carrier to it does not move this pin.
var noCleanupDamageCarriers = []string{
	"Ancient Adamantoise",
	"Case of the Market Melee",
	"Patient Zero",
	"Uthgardt Fury",
}

func TestNoCleanupDamageClassCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	got := map[string]bool{}
	for _, c := range reg.Cards {
		for _, f := range c.Faces {
			if f == nil {
				continue
			}
			for _, st := range f.Statics {
				if st.Mode == "NoCleanupDamage" {
					got[f.Name] = true
				}
			}
			// Effect-delivered carriers (a body whose StaticAbilities$ names the
			// mode in an SVar) are reached only through EachRawEffectChild, the
			// same source Face.Primitives reads for the stat: coverage token.
			f.EachRawEffectChild(func(ch cards.EffectChild) {
				if ch.Static != nil && ch.Static.Mode == "NoCleanupDamage" {
					got[f.Name] = true
				}
			})
		}
	}
	list := make([]string, 0, len(got))
	for n := range got {
		list = append(list, n)
	}
	sort.Strings(list)
	want := append([]string(nil), noCleanupDamageCarriers...)
	sort.Strings(want)
	if len(list) != len(want) {
		t.Fatalf("NoCleanupDamage carriers: got %d, want %d\ngot:  %s\nwant: %s", len(list), len(want), joinQuoted(list), joinQuoted(want))
	}
	for i := range want {
		if list[i] != want[i] {
			t.Fatalf("NoCleanupDamage carrier %d = %q, want %q\ngot:  %s\nwant: %s", i, list[i], want[i], joinQuoted(list), joinQuoted(want))
		}
	}
}
