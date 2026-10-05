package rules

// AttachedTo$-on-Token / Attach-Remembered census (task
// cli-20261004T233422Z-deefaac3, filed from the std3 XMage-compliance re-audit's
// WOE Role-cycle and MSH U.S.Agent findings).
//
// Two mechanisms were fixed at the root this ticket:
//
//   1. `DB$ Token | AttachedTo$ (ThisTargetedCard|Targeted)` on a sub-ability
//      that carries its OWN ValidTgts$: the attach destination is the targets
//      of the ability carrying the param (the sub's own pre-ask answer in
//      Ctx.PickedTargets), NOT the resolution's Ctx.Targets / the chain union.
//      Before the fix the CR 303.4g Aura gate (effects/token.go's
//      auraTokenWithheld) found no legal bearer and every Role token was
//      withheld. Reads: effects/context.go's definedSpecTargeted and
//      definedSpecThisTargetedCard via abilityTargetsForParam.
//
//   2. `DB$ Attach | Object$ Remembered`: Object$ Remembered names the WHOLE
//      remembered set, and Attach fastens every object whose own legality
//      admits the destination (effects/attach.go's attachObjectRemembered).
//      Before the fix only Remembered[0] was taken, so MSH U.S.Agent's Sturdy
//      Shield (remembered SECOND, after the triggering source) and Outfitted
//      Jouster's two conjured Equipment never attached.
//
// This census pins the corpus carriers of both shapes, so a corpus-pin bump
// that adds a new one fails here, named, rather than silently joining an
// untested class. It is a data census (the same shape as
// rules/amass_remembered_census_test.go), not a rules test: it reads the
// compiled corpus directly.
//
// Measured on the pin in the Makefile (95f04e8a04c8925fa97cb226fc3341cabcc90a53).

import (
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// attachedToTokenCarrier is one `DB$ Token` sub naming an AttachedTo$ selector.
type attachedToTokenCarrier struct {
	selector string
	// ownTargets is true when the Token SA carries its own ValidTgts$ key.
	ownTargets bool
	// inherit is true for a sub with no ValidTgts$ of its own (the selector
	// inherits the parent's targets).
	inherit bool
}

// attachRememberedCarrier is one `DB$ Attach | Object$ Remembered` SA.
type attachRememberedCarrier struct {
	// defined is the SA's own Defined$ value ("" when absent).
	defined string
}

func atBoolStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// attachedToTokenKey dedupes a card reached through both a trigger's Execute$
// body and the blanket SVar walk.
func attachedToTokenKey(c attachedToTokenCarrier) string {
	return c.selector + "|" + atBoolStr(c.ownTargets) + "|" + atBoolStr(c.inherit)
}

// atAbilityChains walks every ability chain a face owns: printed abilities,
// trigger bodies, replacement bodies and every SVar ability -- the same walk
// amassRememberedConsumers uses, so a chain reached two ways is deduped by the
// caller.
func atAbilityChains(f *cards.Face) []*cards.SA {
	if f == nil {
		return nil
	}
	var chains []*cards.SA
	chains = append(chains, f.Abilities...)
	for _, tr := range f.Triggers {
		chains = append(chains, tr.Effect)
	}
	for _, r := range f.Repls {
		chains = append(chains, r.With)
	}
	f.EachSVarAbility(func(sa *cards.SA) { chains = append(chains, sa) })
	return chains
}

// attachedToTokenCarriers finds every `Token` SA whose AttachedTo$ names
// ThisTargetedCard or Targeted, classified by whether it carries its own
// ValidTgts$.
func attachedToTokenCarriers(reg *cards.Registry) map[string][]attachedToTokenCarrier {
	out := map[string][]attachedToTokenCarrier{}
	seen := map[string]bool{}
	for _, c := range reg.Cards {
		for _, f := range c.Faces {
			for _, a := range atAbilityChains(f) {
				for sa := a; sa != nil; sa = sa.Sub {
					if sa.API != "Token" {
						continue
					}
					sel := strings.TrimSpace(sa.Params["AttachedTo"])
					if sel != "ThisTargetedCard" && sel != "Targeted" {
						continue
					}
					_, own := sa.Params["ValidTgts"]
					car := attachedToTokenCarrier{selector: sel, ownTargets: own, inherit: !own}
					key := f.Name + "|" + attachedToTokenKey(car)
					if seen[key] {
						continue
					}
					seen[key] = true
					out[f.Name] = append(out[f.Name], car)
				}
			}
		}
	}
	for k := range out {
		sort.Slice(out[k], func(i, j int) bool {
			return attachedToTokenKey(out[k][i]) < attachedToTokenKey(out[k][j])
		})
	}
	return out
}

// attachRememberedCarriers finds every `Attach` SA whose Object$ is
// Remembered, recording the SA's own Defined$ value.
func attachRememberedCarriers(reg *cards.Registry) map[string][]attachRememberedCarrier {
	out := map[string][]attachRememberedCarrier{}
	seen := map[string]bool{}
	for _, c := range reg.Cards {
		for _, f := range c.Faces {
			for _, a := range atAbilityChains(f) {
				for sa := a; sa != nil; sa = sa.Sub {
					if sa.API != "Attach" {
						continue
					}
					if strings.TrimSpace(sa.Params["Object"]) != "Remembered" {
						continue
					}
					car := attachRememberedCarrier{defined: strings.TrimSpace(sa.Params["Defined"])}
					key := f.Name + "|" + car.defined
					if seen[key] {
						continue
					}
					seen[key] = true
					out[f.Name] = append(out[f.Name], car)
				}
			}
		}
	}
	return out
}

// TestAttachedToTokenCensusNamesCarriers pins every corpus `DB$ Token` sub
// whose AttachedTo$ is ThisTargetedCard or Targeted, split by whether the sub
// carries its own ValidTgts$. A new carrier (a corpus-pin bump) fails here,
// named, so the class gets rules coverage rather than silently joining an
// untested set.
func TestAttachedToTokenCensusNamesCarriers(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	got := attachedToTokenCarriers(reg)

	// Precondition: the walk reached the class. A registry that failed to load
	// (or a walk that stopped linking chains) must fail loudly, not pass
	// vacuously.
	if len(got) == 0 {
		t.Fatal("census found no AttachedTo$ Token carriers at all; the corpus walk is broken")
	}

	// The four ThisTargetedCard carriers are the shape this ticket fixed: each
	// carries its own ValidTgts$. Compare both directions so new carriers fail
	// loudly instead of silently joining the untested class.
	wantThisTargeted := map[string]bool{
		"Cut In": true, "Eriette's Whisper": true,
		"Shatter the Oath": true, "Twisted Fealty": true,
	}
	gotThisTargeted := map[string]bool{}
	for name, cars := range got {
		for _, car := range cars {
			if car.selector == "ThisTargetedCard" {
				if !car.ownTargets {
					t.Errorf("%q has inheriting AttachedTo$ ThisTargetedCard; census class changed", name)
				}
				gotThisTargeted[name] = true
			}
		}
	}
	for name := range wantThisTargeted {
		if !gotThisTargeted[name] {
			t.Errorf("precondition: %q has no own-ValidTgts$ AttachedTo$ ThisTargetedCard carrier, got %+v", name, got[name])
		}
	}
	for name := range gotThisTargeted {
		if !wantThisTargeted[name] {
			t.Errorf("new AttachedTo$ ThisTargetedCard carrier %q is not classified in the census table", name)
		}
	}

	// The own-targets carriers of `AttachedTo$ Targeted` are the other half of
	// the fix. Pin them by name; a new one must be classified here. The IR keys
	// a split card's FACE, so the names below are face names. Measured 2026-10-04.
	wantOwnTargeted := map[string]bool{
		"Betroth the Beast":          true,
		"Charmed Clothier":           true,
		"Charming Scoundrel":         true,
		"Curse of the Werefox":       true,
		"Diminisher Witch":           true,
		"Ellivere of the Wild Court": true,
		"Embereth Veteran":           true,
		"Estrid, the Masked":         true,
		"Gadwick's First Duel":       true,
		"Garruk's Lost Wolf":         true,
		"Giant Inheritance":          true,
		"Guard Change":               true,
		"Living Lectern":             true,
		"Lord Skitter's Blessing":    true,
		"Merry Bards":                true,
		"Preston Garvey, Minuteman":  true,
		"Price of Beauty":            true,
		"Protective Parents":         true,
		"Questing Cosplayer":         true,
		"Redtooth Genealogist":       true,
		"Scriv, the Obligator":       true,
		"Selenia, the Cursed Heart":  true,
		"Spellbook Vendor":           true,
		"Spiteful Hexmage":           true,
		"Splashy Spellcaster":        true,
		"Syr Armont, the Redeemer":   true,
		"The Rani":                   true,
		"The Witch's Vanity":         true,
		"Witch's Mark":               true,
	}

	// Build the measured own-targets-Targeted and inherit-Targeted sets.
	gotOwnTargeted := map[string]bool{}
	gotInheritTargeted := map[string]bool{}
	for name, cars := range got {
		for _, car := range cars {
			if car.selector != "Targeted" {
				continue
			}
			if car.ownTargets {
				gotOwnTargeted[name] = true
			} else {
				gotInheritTargeted[name] = true
			}
		}
	}
	if len(gotOwnTargeted) == 0 {
		t.Fatal("precondition: no own-ValidTgts$ AttachedTo$ Targeted carrier found")
	}
	for name := range wantOwnTargeted {
		if !gotOwnTargeted[name] {
			t.Errorf("expected %q to carry AttachedTo$ Targeted with its own ValidTgts$", name)
		}
	}
	for name := range gotOwnTargeted {
		if !wantOwnTargeted[name] {
			t.Errorf("new own-ValidTgts$ AttachedTo$ Targeted carrier %q is not classified in the census table", name)
		}
	}
	if len(gotInheritTargeted) == 0 {
		t.Error("precondition: no inheriting AttachedTo$ Targeted carrier found; the split is broken")
	}

	// Both split buckets must be non-empty: the fix's two paths (own targets
	// and inherit) stay exercised.
	var own, inherit int
	for _, cars := range got {
		for _, car := range cars {
			if car.selector == "Targeted" {
				if car.ownTargets {
					own++
				} else {
					inherit++
				}
			}
		}
	}
	if own == 0 || inherit == 0 {
		t.Fatalf("Targeted split is degenerate: own=%d inherit=%d", own, inherit)
	}
}

// TestAttachRememberedCensusNamesCarriers pins every corpus
// `DB$ Attach | Object$ Remembered` carrier by card and its own Defined$
// value. A new carrier fails here, named, so the widened attach set gets
// rules coverage rather than silently joining an untested class.
//
// The IR keys a split card's FACE, so `Tony Stark` contributes its back face
// "The Invincible Iron Man". Measured 2026-10-04: 9 carriers (the brief said 7;
// the two extras are `Inventory Management` and `Vault 101: Birthday Party`,
// whose Object$/Defined$ keys sit in a different param order than the
// `DB$ Attach | Object$ Remembered` grep).
func TestAttachRememberedCensusNamesCarriers(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	got := attachRememberedCarriers(reg)

	if len(got) == 0 {
		t.Fatal("census found no Attach|Object$ Remembered carriers at all; the corpus walk is broken")
	}

	want := map[string]string{
		"Armored Skyhunter":           "ChosenCard",
		"Balan, Wandering Knight":     "Self",
		"Bruna, Light of Alabaster":   "Self",
		"Inventory Management":        "",
		"Outfitted Jouster":           "",
		"The Invincible Iron Man":     "Self",
		"The Unluckiest Planeswalker": "",
		"U.S.Agent, John Walker":      "Self",
		"Vault 101: Birthday Party":   "",
	}

	var gotNames, wantNames []string
	for n := range got {
		gotNames = append(gotNames, n)
	}
	for n := range want {
		wantNames = append(wantNames, n)
	}
	sort.Strings(gotNames)
	sort.Strings(wantNames)
	if strings.Join(gotNames, "; ") != strings.Join(wantNames, "; ") {
		t.Fatalf("Attach|Object$ Remembered carriers changed:\n got: %v\nwant: %v", gotNames, wantNames)
	}

	for name, wantDefined := range want {
		var gotDefined []string
		for _, car := range got[name] {
			gotDefined = append(gotDefined, car.defined)
		}
		joined := strings.Join(gotDefined, ",")
		if wantDefined == "" {
			if joined != "" {
				t.Errorf("%s: Defined$ = %q, want absent", name, joined)
			}
			continue
		}
		if !strings.Contains(joined, wantDefined) {
			t.Errorf("%s: Defined$ %q missing from %v", name, wantDefined, gotDefined)
		}
	}
}
