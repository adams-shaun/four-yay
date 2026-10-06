package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The two Craft material grammars this test exercises, spelled exactly as the
// LCI corpus carries them.
//
//   - The Enigma Jewel: "four or more nonlands with activated abilities" -- a
//     variable material count with an XMin4 floor.
//   - Throne of the Grim Captain: four typed slots, each filled by a distinct
//     card ("a Dinosaur, a Merfolk, a Pirate, and a Vampire").
const (
	enigmaKeyword = "K:Craft:8 U XMin4 ExileCtrlOrGrave<X/Permanent.Other+nonLand+hasAbility Activated>"
	throneKeyword = "K:Craft:4 ExileCtrlOrGrave<1/Dinosaur.Other> ExileCtrlOrGrave<1/Merfolk.Other> ExileCtrlOrGrave<1/Pirate.Other> ExileCtrlOrGrave<1/Vampire.Other>"
)

func craftDFCSource(keyword string) string {
	return "Name:Shapeshifter Front\nManaCost:0\nTypes:Artifact\n" + keyword +
		"\nAlternateMode:DoubleFaced\nOracle:x\n\nALTERNATE\nName:Shapeshifter Back\nManaCost:no cost\nTypes:Artifact Creature\nPT:5/5\nOracle:x\n"
}

// craftTestMaterial is a nontoken permanent with an activated mana ability,
// so it satisfies "hasAbility Activated" (The Enigma Jewel's material filter)
// and is a legal Craft material whenever its type matches.
func craftTestMaterial(name, types string) string {
	return "Name:" + name + "\nManaCost:0\nTypes:" + types + "\nPT:1/1\nA:AB$ Mana | Cost$ T | Produced$ C\nOracle:x\n"
}

// craftIndexOfKind reports the first offered option of a kind.
func craftIndexOfKind(d *decision.Decision, kind string) int {
	for _, o := range d.Options {
		if o.Kind == kind {
			return o.Index
		}
	}
	return -1
}

// craftIndexOfAmount reports the offered "x" option whose announced value is
// want, or -1.
func craftIndexOfAmount(d *decision.Decision, want int32) int {
	for _, o := range d.Options {
		if o.Kind == "x" && int32(o.Amount) == want {
			return o.Index
		}
	}
	return -1
}

// driveCraftActivation submits the Craft ability option for source, then
// answers every mid-resolution ask it poses: it announces the material count
// xWant when the cost carries an announced part (xWant < 0 means the cost has
// none), and pays each exilecost ask from picks in order -- one distinct object
// per slot, taking the wanted object where it is offered (a nil/short picks
// list takes the first remaining option).
//
// It stops once the source has transformed and fails if it never does, so a
// silently aborted craft is a loud test failure, never a vacuous pass.
func driveCraftActivation(t *testing.T, e *Engine, source state.ObjID, xWant int32, picks []state.ObjID) {
	t.Helper()
	option := abilityOption(t, e, source, 0)
	submitChoices(t, e, option.Index)
	pickAt := 0
	for i := 0; i < 80; i++ {
		if o := e.G.Obj(source); o != nil && o.FaceIdx == 1 {
			return
		}
		d := e.Pending()
		if d == nil {
			e.Advance()
			continue
		}
		switch {
		case d.Kind == decision.KChoose && craftIndexOfKind(d, "x") >= 0:
			idx := craftIndexOfAmount(d, xWant)
			if idx < 0 {
				t.Fatalf("no offered X = %d option: %+v", xWant, d.Options)
			}
			submitChoices(t, e, idx)
		case d.Kind == decision.KChoose && craftIndexOfKind(d, "exilecost") >= 0:
			// An announced material part asks for X cards in ONE decision
			// (Min == Max == X); a fixed slot asks for its literal count.
			// Pay d.Min distinct picks, taking each wanted object where it is
			// offered and falling back to the first remaining option.
			choices := make([]int, 0, d.Min)
			used := map[int]bool{}
			for k := 0; k < d.Min; k++ {
				var want state.ObjID
				if pickAt < len(picks) {
					want = picks[pickAt]
					pickAt++
				}
				idx := -1
				if want != 0 {
					for _, o := range d.Options {
						if o.Obj == want && !used[o.Index] {
							idx = o.Index
						}
					}
				}
				if idx < 0 {
					for _, o := range d.Options {
						if o.Kind == "exilecost" && !used[o.Index] {
							idx = o.Index
							break
						}
					}
				}
				if idx < 0 {
					t.Fatalf("material ask %+v has no pick for slot %d", d.Options, k)
				}
				used[idx] = true
				choices = append(choices, idx)
			}
			submitChoices(t, e, choices...)
		case d.Kind == decision.KPriority:
			// A priority round: pass. The priority menu is Min=Max=1, so an
			// empty answer is rejected; the pass option is the only way to
			// let the craft resolve.
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
					break
				}
			}
			if idx < 0 {
				t.Fatalf("priority decision has no pass option: %+v", d.Options)
			}
			submitChoices(t, e, idx)
		default:
			t.Fatalf("driveCraft: unexpected decision kind=%v min=%d max=%d opts=%+v", d.Kind, d.Min, d.Max, d.Options)
		}
	}
	o := e.G.Obj(source)
	if o == nil || o.FaceIdx != 1 {
		t.Fatalf("Craft never completed: source face = %+v after 80 decisions", o)
	}
}

// TestCraftEnigmaJewelXMinimumMaterials proves The Enigma Jewel's variable
// material count: with four qualifying nonlands the craft is offered, the
// announced X may be 4, and four legal materials settle it into the back face.
func TestCraftEnigmaJewelXMinimumMaterials(t *testing.T) {
	e, _, source := newFixtureDeck(t, 9201, craftDFCSource(enigmaKeyword))
	e.emit(events.Event{Kind: events.MoveZone, Obj: source, From: state.ZLibrary, To: state.ZBattlefield})
	var mats []state.ObjID
	for _, name := range []string{"Enigma Mat A", "Enigma Mat B", "Enigma Mat C", "Enigma Mat D", "Enigma Mat E"} {
		mats = append(mats, putBattlefield(t, e, 0, craftTestMaterial(name, "Creature")))
	}
	if e.G.Obj(source).Zone != state.ZBattlefield || len(e.G.Obj(source).Face().Abilities) == 0 ||
		e.G.Obj(source).Face().Abilities[0].ParamStr(cards.PKKeyword) != "Craft" {
		t.Fatal("precondition: Enigma Jewel source must be on the battlefield with its Craft ability installed")
	}
	// {8}{U} plus self-exile: float ten blue.
	addMana(t, e, 0, "UUUUUUUUUU")
	if _, ok := findAbilityOption(e, source, 0); !ok {
		t.Fatalf("Craft not offered with five qualifying nonlands; options = %+v", e.Pending().Options)
	}
	driveCraftActivation(t, e, source, 4, mats[:4])
	o := e.G.Obj(source)
	if o.FaceIdx != 1 {
		t.Fatalf("Enigma Jewel face = %d, want transformed (1)", o.FaceIdx)
	}
	for _, id := range mats[:4] {
		if z := e.G.Obj(id).Zone; z != state.ZExile {
			t.Fatalf("material %d zone = %v, want exile", id, z)
		}
	}
	remembered := map[state.ObjID]bool{}
	for _, target := range o.Remembered {
		remembered[target.Obj] = true
	}
	for _, id := range mats[:4] {
		if !remembered[id] {
			t.Fatalf("transformed source Remembered = %+v, want material %d", o.Remembered, id)
		}
	}
}

// TestCraftEnigmaJewelTooFewMaterialsRefused proves the XMin4 floor: with only
// three qualifying nonlands there is no legal announcement (CR 601.2b), so the
// craft never completes and the source stays on its front face.
func TestCraftEnigmaJewelTooFewMaterialsRefused(t *testing.T) {
	e, _, source := newFixtureDeck(t, 9202, craftDFCSource(enigmaKeyword))
	e.emit(events.Event{Kind: events.MoveZone, Obj: source, From: state.ZLibrary, To: state.ZBattlefield})
	for _, name := range []string{"Enigma Few A", "Enigma Few B", "Enigma Few C"} {
		putBattlefield(t, e, 0, craftTestMaterial(name, "Creature"))
	}
	addMana(t, e, 0, "UUUUUUUUUU")
	if e.G.Obj(source).FaceIdx != 0 {
		t.Fatal("precondition: source must start on the front face")
	}
	// The feature handler must have run: the expander installs the Craft
	// ability only for a shape it supports. Without this assertion a revert of
	// the shape support would make the "no transform" check below pass
	// vacuously (there would be no ability to activate at all).
	if len(e.G.Obj(source).Face().Abilities) == 0 ||
		e.G.Obj(source).Face().Abilities[0].ParamStr(cards.PKKeyword) != "Craft" {
		t.Fatal("precondition: the Craft ability must be installed for the XMin shape")
	}
	// The offer gate cannot see the floor (it prices each part's literal N,
	// which is 0 for an announced part), so the activation may be offered and
	// then abort. If it is offered, drive it and assert the X menu never
	// offered a value at or above the XMin4 floor -- the floor makes every
	// three-material announcement illegal (CR 601.2b).
	if _, ok := findAbilityOption(e, source, 0); ok {
		option := abilityOption(t, e, source, 0)
		submitChoices(t, e, option.Index)
		for i := 0; i < 20 && e.Pending() != nil; i++ {
			d := e.Pending()
			if d.Kind == decision.KChoose && craftIndexOfKind(d, "x") >= 0 {
				for _, o := range d.Options {
					if o.Kind == "x" && int32(o.Amount) >= 4 {
						t.Fatalf("XMin4 floor violated: three materials offered X = %d", o.Amount)
					}
				}
				submitChoices(t, e, craftIndexOfKind(d, "x"))
				continue
			}
			if d.Kind == decision.KChoose && craftIndexOfKind(d, "exilecost") >= 0 {
				submitChoices(t, e, craftIndexOfKind(d, "exilecost"))
				continue
			}
			if d.Kind == decision.KPriority {
				if idx := craftIndexOfKind(d, "pass"); idx >= 0 {
					submitChoices(t, e, idx)
					continue
				}
			}
			submitChoices(t, e)
		}
	}
	if o := e.G.Obj(source); o == nil || o.FaceIdx != 0 || o.Zone != state.ZBattlefield {
		t.Fatalf("too-few craft mutated the source: %+v", o)
	}
}

// TestCraftThroneMultiSlotMaterials proves Throne of the Grim Captain's
// four-slot shape: four distinct typed cards each fill their own slot, and one
// card can never fill two slots.
func TestCraftThroneMultiSlotMaterials(t *testing.T) {
	e, _, source := newFixtureDeck(t, 9203, craftDFCSource(throneKeyword))
	e.emit(events.Event{Kind: events.MoveZone, Obj: source, From: state.ZLibrary, To: state.ZBattlefield})
	dino := putBattlefield(t, e, 0, craftTestMaterial("Dino", "Creature Dinosaur"))
	merf := putBattlefield(t, e, 0, craftTestMaterial("Merf", "Creature Merfolk"))
	pirate := putBattlefield(t, e, 0, craftTestMaterial("Pirate", "Creature Pirate"))
	vamp := putBattlefield(t, e, 0, craftTestMaterial("Vamp", "Creature Vampire"))
	if e.G.Obj(source).Zone != state.ZBattlefield {
		t.Fatal("precondition: Throne source must be on the battlefield")
	}
	addMana(t, e, 0, "CCCCC") // {4} plus self-exile
	if _, ok := findAbilityOption(e, source, 0); !ok {
		t.Fatalf("Craft not offered with all four typed materials; options = %+v", e.Pending().Options)
	}
	driveCraftActivation(t, e, source, -1, []state.ObjID{dino, merf, pirate, vamp})
	if o := e.G.Obj(source); o.FaceIdx != 1 {
		t.Fatalf("Throne face = %d, want transformed (1)", o.FaceIdx)
	}
}

// TestCraftThroneOneCardCannotFillTwoSlots proves the distinct-materials rule:
// a single creature carrying all four types is a candidate for every slot, but
// the slots are paid one after another with distinct cards, so one permanent
// can never pay two. With that one creature alone the cost is unpayable and
// the activation is never offered.
func TestCraftThroneOneCardCannotFillTwoSlots(t *testing.T) {
	e, _, source := newFixtureDeck(t, 9204, craftDFCSource(throneKeyword))
	e.emit(events.Event{Kind: events.MoveZone, Obj: source, From: state.ZLibrary, To: state.ZBattlefield})
	putBattlefield(t, e, 0, craftTestMaterial("Chameleon", "Creature Dinosaur Merfolk Pirate Vampire"))
	addMana(t, e, 0, "CCCCC")
	if e.G.Obj(source).FaceIdx != 0 {
		t.Fatal("precondition: source must start on the front face")
	}
	if _, ok := findAbilityOption(e, source, 0); ok {
		t.Fatalf("Craft offered with one card for four slots; options = %+v", e.Pending().Options)
	}
}
