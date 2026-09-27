package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// sharedCardTypeCorpusSA resolves the SA that carries
// TargetsWithSharedCardType$ on a real corpus card, but ONLY when that SA is
// reachable from the printed face: through a printed ability's SubAbility$
// chain, or through a printed trigger's Execute$ effect and its SubAbility$
// chain. There is deliberately no global SVar sweep — an SVar no printed line
// reaches must not satisfy the pin, because the pin exists to prove the
// printed card actually links the parameter through the resolver.
func sharedCardTypeCorpusSA(t *testing.T, cardName string) *cards.SA {
	t.Helper()
	reg := freshCorpusRegistry(t,
		"c/confusion_in_the_ranks.txt",
		"g/gauntlets_of_chaos.txt",
		"l/legerdemain.txt",
		"p/power_struggle.txt",
	)
	card, ok := reg.Lookup(cardName)
	if !ok || len(card.Faces) == 0 {
		t.Fatalf("precondition: corpus card %q is missing", cardName)
	}
	face := card.Faces[0]
	var find func(*cards.SA) *cards.SA
	find = func(sa *cards.SA) *cards.SA {
		for sa != nil {
			if sharedCardTypeRef(sa) != "" {
				return sa
			}
			sa = sa.Sub
		}
		return nil
	}
	for _, ab := range face.Abilities {
		if sa := find(ab); sa != nil {
			return sa
		}
	}
	for _, trig := range face.Triggers {
		if sa := find(trig.Effect); sa != nil {
			return sa
		}
	}
	t.Fatalf("precondition: %s has no printed ability or trigger link to an SA with TargetsWithSharedCardType$", cardName)
	return nil
}

// TestSharedCardTypeRealSAPin runs actual resolved ExchangeControl SAs from
// Legerdemain and Gauntlets of Chaos through target offer and CR 608.2b
// recheck. This pins the parser/linker-produced parameters, not synthetic SAs.
func TestSharedCardTypeRealSAPin(t *testing.T) {
	for _, tc := range []struct {
		name   string
		shared string
	}{
		{name: "Legerdemain", shared: "Artifact,Creature"},
		{name: "Gauntlets of Chaos", shared: "Artifact,Creature,Land"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sa := sharedCardTypeCorpusSA(t, tc.name)
			if sa.API != "ExchangeControl" {
				t.Fatalf("precondition: %s resolved API = %q, want ExchangeControl", tc.name, sa.API)
			}
			if got := sharedCardTypeRef(sa); got != "ParentTarget" {
				t.Fatalf("precondition: %s TargetsWithSharedCardType$ = %q, want ParentTarget", tc.name, got)
			}
			if got := sharedTypesRet(sa); got != tc.shared {
				t.Fatalf("precondition: %s TargetsWithSharedTypes$ = %q, want %q", tc.name, got, tc.shared)
			}

			e := newSeats(t, 2)
			parent := putBattlefield(t, e, 0, "Name:Parent\nTypes:Enchantment\nOracle:x\n")
			ref := putBattlefield(t, e, 0, "Name:Ref Artifact Enchantment\nTypes:Artifact Enchantment\nOracle:x\n")
			artifact := putBattlefield(t, e, 1, "Name:Other Artifact\nTypes:Artifact\nOracle:x\n")
			enchantment := putBattlefield(t, e, 1, "Name:Other Enchantment\nTypes:Enchantment\nOracle:x\n")
			e.emit(events.Event{Kind: events.TargetsChosen, Obj: parent, IDs: []state.ObjID{ref}})
			if len(e.G.Obj(parent).Targets) != 1 || e.G.Obj(parent).Targets[0].Obj != ref {
				t.Fatalf("precondition: parent target = %+v, want reference %d", e.G.Obj(parent).Targets, ref)
			}
			whitelist := sharedTypesWhitelist(sa)
			if !e.sharedCardTypeAdmits(artifact, ref, whitelist) {
				t.Fatal("precondition: Artifact must share a whitelisted type with the reference")
			}
			if e.sharedCardTypeAdmits(enchantment, ref, whitelist) {
				t.Fatal("precondition: Enchantment must share only a non-whitelisted type")
			}
			if !e.sharedCardTypeAdmits(enchantment, ref, nil) {
				t.Fatal("precondition: Enchantment must share a type absent the whitelist")
			}

			e.pending = nil
			e.askTarget(0, parent, sa)
			d := e.Pending()
			if d == nil {
				t.Fatal("real ExchangeControl SA posed no target decision")
			}
			if setPropOptionIndex(d, artifact) == -1 {
				t.Fatal("whitelisted Artifact was not offered by real SA")
			}
			if setPropOptionIndex(d, enchantment) != -1 {
				t.Fatal("non-whitelisted Enchantment was offered by real SA")
			}

			kept := e.legalTargets([]state.Target{{Obj: artifact}, {Obj: enchantment}}, sa, targetZones(sa), 0, parent, parent)
			if len(kept) != 1 || kept[0].Obj != artifact {
				t.Fatalf("real SA recheck = %+v, want only the whitelisted Artifact %d", kept, artifact)
			}
		})
	}
}

// TestSharedCardTypeRealIRCarriers pins the remaining corpus carriers to the
// parameter shapes on the SAs their printed lines actually link (Confusion
// and Power Struggle through their triggers' Execute$ effects; Gauntlets
// through its printed ability's SubAbility$ chain).
func TestSharedCardTypeRealIRCarriers(t *testing.T) {
	for _, tc := range []struct {
		name   string
		ref    string
		shared string
	}{
		{name: "Confusion in the Ranks", ref: "TriggeredCard"},
		{name: "Power Struggle", ref: "ParentTarget"},
		{name: "Gauntlets of Chaos", ref: "ParentTarget", shared: "Artifact,Creature,Land"},
	} {
		sa := sharedCardTypeCorpusSA(t, tc.name)
		if got := sharedCardTypeRef(sa); got != tc.ref {
			t.Fatalf("precondition: %s TargetsWithSharedCardType$ = %q, want %q", tc.name, got, tc.ref)
		}
		if got := sharedTypesRet(sa); got != tc.shared {
			t.Fatalf("precondition: %s TargetsWithSharedTypes$ = %q, want %q", tc.name, got, tc.shared)
		}
	}
}
