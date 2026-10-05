package rules

// stat:CantBeSuspected (CR 702.157) — "Enchanted creature ... can't become
// suspected." MKM's Airtight Alibi is the Standard carrier
// (`ValidCard$ Creature.EnchantedBy`) and the ONLY corpus carrier. The
// designation is applied in exactly one place (effects' effAlterAttribute, an
// events.AlterAttribute with Text "Suspected"); this file pins that the
// prohibition read by rules.cantBeSuspected actually suppresses it.

import (
	"sort"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// cantBeSuspectedStatic is the Airtight Alibi shape, scoped to a Bear so the
// ValidCard$ read can be pinned: only a Bear must be protected.
const cantBeSuspectedStatic = "Name:Test Alibi\nManaCost:1 G\nTypes:Enchantment\n" +
	"S:Mode$ CantBeSuspected | ValidCard$ Creature.Bear | Description$ Bears can't become suspected.\n" +
	"Oracle:x\n"

// TestCantBeSuspectedSuppressesTheDesignation is the behavioural leaf: an
// AlterAttribute-Suspected resolution against a creature a live
// CantBeSuspected static scopes leaves it unsuspected, while an unscoped
// creature on the same board is suspected. Both the static's liveness and the
// effect's own run are asserted, so a vacuous setup fails.
func TestCantBeSuspectedSuppressesTheDesignation(t *testing.T) {
	t.Parallel()
	e := threeSeatEngine(t)
	bear := onBoard(t, e, 0, "Name:Test Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	soldier := onBoard(t, e, 0, "Name:Test Soldier\nManaCost:1 W\nTypes:Creature Soldier\nPT:2/2\nOracle:x\n")
	onBoard(t, e, 0, cantBeSuspectedStatic)

	// Precondition: the static is live and scopes exactly the Bear.
	if got := len(e.activeStatics("CantBeSuspected")); got != 1 {
		t.Fatalf("precondition: expected one live CantBeSuspected static, got %d", got)
	}
	if !cantBeSuspected(e, bear) {
		t.Fatal("precondition: the Bear should be protected by the live static")
	}
	if cantBeSuspected(e, soldier) {
		t.Fatal("precondition: the Soldier is outside ValidCard$ Creature.Bear and must not be protected")
	}

	if !suppressSuspectedEvent(e, events.Event{Kind: events.AlterAttribute, Obj: bear,
		Text: suspectedAttribute, Amount: 1}) {
		t.Fatal("precondition: the event-level prohibition must select the Bear's Suspected event")
	}

	suspect := func(id state.ObjID) {
		sa := &cards.SA{Kind: "DB", API: "AlterAttribute", Params: map[string]string{"Attributes": "Suspected", "ValidTgts": "Creature"}}
		effects.Resolve(e, &effects.Ctx{Source: 0, Controller: 0,
			Targets: []state.Target{{Obj: id}}, TargetsOffered: true}, sa)
	}
	suspect(bear)
	suspect(soldier)

	// The handler ran and the unscoped creature really became suspected, so
	// the protected creature's non-suspected state is not "nothing happened".
	if !e.G.Obj(soldier).Suspected {
		t.Fatal("precondition: the unscoped Soldier did not become suspected; the effect path under test did not run")
	}
	if e.G.Obj(bear).Suspected {
		t.Fatal("CantBeSuspected did not suppress the Suspected designation on the protected Bear")
	}
}

// TestCantBeSuspectedRemovalStillApplies pins that the prohibition only blocks
// the activating direction: an Activate$ False (un-suspect) still clears a
// designation, so the CantBeSuspected family can never strand a suspected
// permanent.
func TestCantBeSuspectedRemovalStillApplies(t *testing.T) {
	t.Parallel()
	e := threeSeatEngine(t)
	bear := onBoard(t, e, 0, "Name:Test Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	// Mark it suspected before the prohibition enters, modeling a pre-existing
	// designation that the static does not remove.
	e.emit(events.Event{Kind: events.AlterAttribute, Obj: bear, Text: "Suspected", Amount: 1})
	onBoard(t, e, 0, cantBeSuspectedStatic)
	if !cantBeSuspected(e, bear) {
		t.Fatal("precondition: the Bear should be protected by the live static")
	}
	if !e.G.Obj(bear).Suspected {
		t.Fatal("precondition: the direct AlterAttribute fold did not set the designation")
	}

	sa := &cards.SA{Kind: "DB", API: "AlterAttribute", Params: map[string]string{"Attributes": "Suspected", "Activate": "False", "ValidTgts": "Creature"}}
	effects.Resolve(e, &effects.Ctx{Source: 0, Controller: 0,
		Targets: []state.Target{{Obj: bear}}, TargetsOffered: true}, sa)
	if e.G.Obj(bear).Suspected {
		t.Fatal("Activate$ False did not clear the Suspected designation under a CantBeSuspected static")
	}
}

// TestCantBeSuspectedPrimitiveIsRegistered pins the support declaration the
// skip gate reads.
func TestCantBeSuspectedPrimitiveIsRegistered(t *testing.T) {
	if !effects.Supported()["stat:CantBeSuspected"] {
		t.Fatal(`effects.Supported() is missing "stat:CantBeSuspected"`)
	}
}

// cantBeSuspectedCarriers is the corpus-wide class (one card).
var cantBeSuspectedCarriers = []string{"Airtight Alibi"}

// TestCantBeSuspectedClassCensus pins the carrier set so a corpus-pin bump
// that adds a carrier fails loudly. It walks both delivery routes (printed S:
// and Effect-delivered bodies).
func TestCantBeSuspectedClassCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	found := cantBeSuspectedCorpusCarriers(reg)
	list := make([]string, 0, len(found))
	for n := range found {
		list = append(list, n)
	}
	sort.Strings(list)
	want := append([]string(nil), cantBeSuspectedCarriers...)
	sort.Strings(want)
	if len(list) != len(want) {
		t.Fatalf("CantBeSuspected carriers: got %d, want %d\ngot:  %s\nwant: %s", len(list), len(want), joinQuoted(list), joinQuoted(want))
	}
	for i := range want {
		if list[i] != want[i] {
			t.Fatalf("CantBeSuspected carrier %d = %q, want %q\ngot: %s", i, list[i], want[i], joinQuoted(list))
		}
	}
}

func cantBeSuspectedCorpusCarriers(reg *cards.Registry) map[string]bool {
	got := map[string]bool{}
	for _, c := range reg.Cards {
		for _, f := range c.Faces {
			if f == nil {
				continue
			}
			for _, st := range f.Statics {
				if st.Mode == "CantBeSuspected" {
					got[f.Name] = true
				}
			}
			f.EachRawEffectChild(func(ch cards.EffectChild) {
				if ch.Static != nil && ch.Static.Mode == "CantBeSuspected" {
					got[f.Name] = true
				}
			})
		}
	}
	return got
}
