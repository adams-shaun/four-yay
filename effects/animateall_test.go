package effects

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// AnimateAll (task api-animateall): the ValidCards$ battlefield sweep with
// Animate's per-object registration. The unit tests assert on what got
// REGISTERED (the effects package has no layer computation); the end-to-end
// computed result is pinned in rules (mirror entity + Vedalken Humiliator).

// notesOf returns the emitted Note texts.
func notesOf(h *fakeHost) []string {
	var out []string
	for _, e := range h.log {
		if e.Kind == events.Note {
			out = append(out, e.Text)
		}
	}
	return out
}

// TestAnimateAllSweepRegistersSubSetAndAllCreatureTypes: the Mirror Entity
// shape at unit level — ValidCards$ Creature.YouCtrl, Power$/Toughness$ X,
// AddAllCreatureTypes$ True. Every creature YOU control gets the layer-7b
// SubSet and the layer-4 all-creature-types flag; the opponent's creature
// and the non-creature permanents get nothing.
func TestAnimateAllSweepRegistersSubSetAndAllCreatureTypes(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	Resolve(h, &Ctx{Controller: 0},
		sa(t, "DB$ AnimateAll | ValidCards$ Creature.YouCtrl | Power$ 4 | Toughness$ 4 | AddAllCreatureTypes$ True"))
	var pt, types int
	for _, ce := range h.continuous {
		switch ce.Source {
		case ids["myBear"], ids["myFlier"]:
		default:
			if ce.Layer == state.LPT || ce.Layer == state.LType {
				t.Fatalf("grant landed on a non-you-creature (source %d): %+v", ce.Source, ce)
			}
			continue
		}
		switch {
		case ce.Layer == state.LPT:
			pt++
			if ce.Sub != state.SubSet || !ce.HasSet || ce.SetPower != 4 || ce.SetToughness != 4 || !ce.UntilEOT {
				t.Fatalf("P/T effect = %+v", ce)
			}
		case ce.Layer == state.LType:
			types++
			if !ce.AddAllCreatureTypes || len(ce.AddTypes) != 0 {
				t.Fatalf("type effect = %+v", ce)
			}
		default:
			t.Fatalf("unexpected layer %d on a swept creature", ce.Layer)
		}
	}
	if pt != 2 || types != 2 {
		t.Fatalf("registered P/T=%d types=%d, want 2/2", pt, types)
	}
}

// TestAnimateAllDefaultFilterIsCreature: no ValidCards$ animates every
// creature on the battlefield, nothing else.
func TestAnimateAllDefaultFilterIsCreature(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	Resolve(h, &Ctx{Controller: 0}, sa(t, "DB$ AnimateAll | Types$ Golem"))
	sources := map[state.ObjID]bool{}
	for _, ce := range h.continuous {
		if ce.Layer != state.LType {
			t.Fatalf("unexpected non-type grant: %+v", ce)
		}
		sources[ce.Source] = true
	}
	for _, want := range []state.ObjID{ids["myBear"], ids["myFlier"], ids["theirBig"]} {
		if !sources[want] {
			t.Fatalf("creature id %d not animated", want)
		}
	}
	for _, bad := range []state.ObjID{ids["myLand"], ids["myArtifact"], ids["myWalker"]} {
		if sources[bad] {
			t.Fatalf("non-creature id %d animated", bad)
		}
	}
}

// TestAnimateAllWithNoPowerOrToughnessOnlyGrantsTypes keeps the Animate
// guard on the sweep: an AnimateAll that never names Power$/Toughness$ must
// not register a 0/0 base set on every creature it sweeps.
func TestAnimateAllWithNoPowerOrToughnessOnlyGrantsTypes(t *testing.T) {
	g, _ := board(t)
	h := &fakeHost{g: g}
	Resolve(h, &Ctx{Controller: 0}, sa(t, "DB$ AnimateAll | ValidCards$ Creature | Types$ Artifact Treasure"))
	if len(h.continuous) != 3 {
		t.Fatalf("continuous = %+v, want one type grant per swept creature", h.continuous)
	}
	for _, ce := range h.continuous {
		if ce.Layer != state.LType || ce.AddTypes[0] != "Artifact" || ce.AddTypes[1] != "Treasure" {
			t.Fatalf("type effect = %+v", ce)
		}
	}
	if notes := notesOf(h); len(notes) != 0 {
		t.Fatalf("unexpected notes: %v", notes)
	}
}

// TestAnimateAllPermanentDurationIsPermanent pins the shared Animate
// lifetime contract on the sweep: Duration$ Permanent registers wholly
// permanent effects (no UntilEOT), the default stays until-end-of-turn.
func TestAnimateAllPermanentDurationIsPermanent(t *testing.T) {
	g, _ := board(t)
	h := &fakeHost{g: g}
	Resolve(h, &Ctx{Controller: 0},
		sa(t, "DB$ AnimateAll | ValidCards$ Creature | Power$ 3 | Types$ Golem | Duration$ Permanent"))
	for _, ce := range h.continuous {
		if !ce.Permanent || ce.UntilEOT {
			t.Fatalf("Permanent animation registered %+v", ce)
		}
	}
}

// TestAnimateAllUnreadParametersNoteLoudly: the remaining AnimateAll gaps
// (RemoveKeywords$/RemoveAllAbilities$/Replacements$/CantHaveKeyword$/
// RemoveLandTypes$) must be LOUD — one note naming every unread parameter
// present — while the supported parameters still apply. staticAbilities$ is
// tested through the shared Animate registration path below.
// Triggers$ is READ since the Animate Triggers$ ticket: a named body the
// parser refuses (no SVar table here, so TrigSomething resolves to
// nothing) gets its own loud refuse-note instead of joining the unread
// list.
func TestAnimateStaticAbilitiesRegistersCantSacrifice(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	c := &Ctx{Controller: 0, Source: ids["myBear"], SVars: map[string]string{
		"SCantSac": "Mode$ CantSacrifice | ValidCard$ Card.Self",
	}}
	registerAnimateStaticAbilities(h, c, ids["myBear"], []string{"SCantSac"}, "UntilEOT", false, true, 0, "", "", nil)
	if len(h.continuous) != 1 {
		t.Fatalf("registered continuous effects = %d, want one", len(h.continuous))
	}
	ce := h.continuous[0]
	if ce.Source != ids["myBear"] || ce.Restriction != "CantSacrifice" || ce.RestrictParams["ValidCard"] != "Card.Self" {
		t.Fatalf("staticAbilities registration = %+v", ce)
	}
}

func TestAnimateAllUnreadParametersNoteLoudly(t *testing.T) {
	g, _ := board(t)
	h := &fakeHost{g: g}
	Resolve(h, &Ctx{Controller: 0}, sa(t,
		"DB$ AnimateAll | ValidCards$ Creature | Power$ 1 | RemoveAllAbilities$ True | Triggers$ TrigSomething | Replacements$ ReplSomething"))
	notes := notesOf(h)
	if len(notes) != 2 {
		t.Fatalf("notes = %v, want the unread-params note plus the refused Triggers$ body's note", notes)
	}
	var unread, refused bool
	for _, n := range notes {
		if strings.Contains(n, "RemoveAllAbilities$") &&
			strings.Contains(n, "Replacements$") {
			unread = true
		}
		if strings.Contains(n, "Triggers$ TrigSomething") {
			refused = true
		}
	}
	if !unread || !refused {
		t.Fatalf("notes = %v, want one note naming every unread parameter and one refusing the Triggers$ body", notes)
	}
	var pt int
	for _, ce := range h.continuous {
		if ce.Layer == state.LPT {
			pt++
		}
	}
	if pt != 3 {
		t.Fatalf("supported parameters must still apply (P/T grants = %d, want 3)", pt)
	}
}

// TestAnimateAllRemoveCardTypesStripsCardTypes: RemoveCardTypes$ rides the
// shared helper's one-line addition (state.ContinuousEffect.RemoveCardTypes,
// the Darksteel Mutation strip), so both primitives read it.
func TestAnimateAllRemoveCardTypesStripsCardTypes(t *testing.T) {
	g, _ := board(t)
	h := &fakeHost{g: g}
	Resolve(h, &Ctx{Controller: 0}, sa(t, "DB$ AnimateAll | ValidCards$ Creature | RemoveCardTypes$ True"))
	if len(h.continuous) != 3 {
		t.Fatalf("continuous = %+v, want one type grant per swept creature", h.continuous)
	}
	for _, ce := range h.continuous {
		if ce.Layer != state.LType || !ce.RemoveCardTypes {
			t.Fatalf("type effect = %+v, want RemoveCardTypes", ce)
		}
	}
}

// TestAnimateAllZoneGraveyardScopesTheGrant: Zone$ widens the sweep the way
// PumpAll's PumpZone$ does, and the registered effects carry the
// AffectedZone scope so the grant applies only while the card sits there.
func TestAnimateAllZoneGraveyardScopesTheGrant(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	Resolve(h, &Ctx{Controller: 0},
		sa(t, "DB$ AnimateAll | ValidCards$ Creature | Types$ Golem | Zone$ Graveyard"))
	// Only the graveyard instant/sorcery cards exist in the zone walk, and
	// neither is a Creature, so nothing matches here...
	if len(h.continuous) != 0 {
		t.Fatalf("continuous = %+v, want none (no creatures in graveyards)", h.continuous)
	}
	// ...but a creature card in the graveyard IS matched and carries the
	// AffectedZone scope.
	moveTo(g, ids["myBear"], state.ZGraveyard)
	h.continuous = nil
	h.log = nil
	Resolve(h, &Ctx{Controller: 0},
		sa(t, "DB$ AnimateAll | ValidCards$ Creature | Types$ Golem | Zone$ Graveyard"))
	if len(h.continuous) != 1 {
		t.Fatalf("continuous = %+v, want 1", h.continuous)
	}
	if ce := h.continuous[0]; ce.Source != ids["myBear"] || ce.AffectedZone != "Graveyard" {
		t.Fatalf("type effect = %+v", ce)
	}
}

// TestAnimateAllConditionPresentGateBlocksDispatch pins the shared pre-dispatch
// condition gate on AnimateAll (the Lord of the Nazgul acceptance case,
// issue agent-20260918T201425Z-dffc06a3): ConditionPresent$ with no
// ConditionDefined$ counts the battlefield (shape 2 in conditions.go), so a
// gated AnimateAll whose compare does not hold registers NOTHING, while the
// same body with a holding compare registers normally.
func TestAnimateAllConditionPresentGateBlocksDispatch(t *testing.T) {
	g, _ := board(t)
	h := &fakeHost{g: g}
	Resolve(h, &Ctx{Controller: 0}, sa(t,
		"DB$ AnimateAll | ValidCards$ Creature | Power$ 4 | ConditionPresent$ Creature | ConditionCompare$ GE9"))
	if len(h.continuous) != 0 {
		t.Fatalf("gate failed open: continuous = %+v, want none (3 creatures < 9)", h.continuous)
	}
	Resolve(h, &Ctx{Controller: 0}, sa(t,
		"DB$ AnimateAll | ValidCards$ Creature | Power$ 4 | ConditionPresent$ Creature | ConditionCompare$ GE1"))
	if len(h.continuous) == 0 {
		t.Fatal("holding compare must still register")
	}
}

// TestAnimateAllOpponentCreatureNeverSwept: YouCtrl scoping — the
// opponent's creature never animates even when the sweep matches its type.
func TestAnimateAllOpponentCreatureNeverSwept(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	Resolve(h, &Ctx{Controller: 0}, sa(t, "DB$ AnimateAll | ValidCards$ Creature.YouCtrl | Types$ Golem"))
	for _, ce := range h.continuous {
		if ce.Source == ids["theirBig"] {
			t.Fatal("AnimateAll with YouCtrl must not touch the opponent's creature")
		}
	}
}
