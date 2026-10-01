package botpolicy

import (
	"math/rand/v2"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// abilityPriority builds a main-phase priority decision offering exactly one
// "ability" option (source Obj src, labelled lab) in front of the unavoidable
// "pass" option, the shape legalActions emits when the deciding seat's only
// worth-thinking-about action is one activated ability. Min/Max 1 is the
// engine's real priority shape, so Decide's answer must be exactly one of
// them, and rules/legal.go always offers "pass" second-to-last and "concede"
// last (M2d-3) — the concede is omitted here because no branch ever touches
// it and the extra option just narrows the error message noise.
func abilityPriority(src state.ObjID, lab string) *decision.Decision {
	return &decision.Decision{Seq: 1, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "ability", Obj: src, Label: lab, Attach: true},
			{Index: 1, Kind: "pass"},
		}}
}

// TestA1NoOpEquipNotChosen pins rule A1 (ability.go): an "ability" option
// on an equipment that is ALREADY attached to a permanent is a provable
// no-op — the equip can only re-site the equipment, changing nothing the
// policy can value — so it is never activated and the policy passes
// instead. This is the choice that froze the Sai deck forever (a free
// Lightning Greaves re-equipped onto the creature it already carried, every
// main-phase priority). The gate deletes A1's already-attached branch
// (equipNoOp returns false for the attached case) and this test fails: the
// no-op ability is then worth taking, and the first-option habit would
// pick it over pass.
func TestA1NoOpEquipNotChosen(t *testing.T) {
	// Perilous Myr (22) is the controller's only creature; the equipment whose
	// ability is offered (41) is already attached to it.
	b := Board{IsMain: true,
		Creatures: TableOf(map[state.ObjID]Creature{22: {Power: 2, Toughness: 2, Controller: 0}}),
		Cards:     TableOf(map[state.ObjID]Card{41: {AttachedTo: 22}}),
	}
	d := abilityPriority(41, "Lightning Greaves: Equip 0")
	in := Decide(b, d, rng(1))
	if err := d.Validate(in); err != nil {
		t.Fatalf("intent %+v failed Validate: %v", in, err)
	}
	if len(in.Choices) != 1 {
		t.Fatalf("priority = %+v, want exactly one choice", in)
	}
	if got := d.Options[in.Choices[0]].Kind; got != "pass" {
		t.Fatalf("A1 no-op equip = %s (option %d), want pass chosen, never the no-op ability", got, in.Choices[0])
	}
}

// TestA1NoLegalTargetEquipNotChosen pins A1's no-legal-target case (ability.go):
// an equipment whose controller has no battlefield creature at all cannot
// attach anywhere, so its Equip would fizzle and change nothing — the policy
// passes instead of re-activating a free ability that can never land. This
// is the wiped-board twin of TestA1NoOpEquipNotChosen, and it is exactly how
// a free equip freezes a game once the creature that survived died: the
// re-attach rule catches it while a creature remains, this one after they
// are all gone. The gate deletes the no-creature branch of equipNoOp and
// this test fails: the ability is then worth taking again and chosen over
// pass.
func TestA1NoLegalTargetEquipNotChosen(t *testing.T) {
	// No creatures at all; the equipment sits unattached on an empty board.
	b := Board{IsMain: true,
		Creatures: TableOf(map[state.ObjID]Creature{}),
		Cards:     TableOf(map[state.ObjID]Card{41: {AttachedTo: 0}}),
	}
	d := abilityPriority(41, "Lightning Greaves: Equip 0")
	in := Decide(b, d, rng(1))
	if err := d.Validate(in); err != nil {
		t.Fatalf("intent %+v failed Validate: %v", in, err)
	}
	if len(in.Choices) != 1 {
		t.Fatalf("priority = %+v, want exactly one choice", in)
	}
	if got := d.Options[in.Choices[0]].Kind; got != "pass" {
		t.Fatalf("A1 no-legal-target equip = %s (option %d), want pass chosen, never an equip with no creature to attach to", got, in.Choices[0])
	}
}

// TestA1AlreadyAttachedEquipDeclined pins the fix-round-2 A1 rule by name
// (ability.go): an equipment that is ALREADY attached to any permanent has
// its Equip declined — an already-attached permanent's activated ability is
// never worth re-activating — even on a board where a second, more
// threatening own creature (99) would be a plausible different bearer. This
// is exactly the board the fix-round-1 A1 got wrong: it PREDICTED the
// target (the 4/4 99) and, because the prediction was not the bearer,
// concluded the equip "moves" the equipment. The target branch that
// actually answers the follow-up decision elects the bearer instead, so the
// activation re-attached the equipment to the creature it already sat on
// and the policy looped forever. equipNoOp now reads AttachedTo itself —
// no prediction — so the two cannot disagree: this equip is declined and
// the policy falls through to its explicit pass.
func TestA1AlreadyAttachedEquipDeclined(t *testing.T) {
	// 22 carries the equipment; 99 is a more threatening own creature the
	// old projection would have elected as the equip target.
	b := Board{IsMain: true,
		Creatures: TableOf(map[state.ObjID]Creature{
			22: {Power: 2, Toughness: 2, Controller: 0},
			99: {Power: 4, Toughness: 4, Controller: 0},
		}),
		Cards: TableOf(map[state.ObjID]Card{41: {AttachedTo: 22}}),
	}
	d := abilityPriority(41, "Lightning Greaves: Equip 0")
	in := Decide(b, d, rng(1))
	if err := d.Validate(in); err != nil {
		t.Fatalf("intent %+v failed Validate: %v", in, err)
	}
	if len(in.Choices) != 1 {
		t.Fatalf("priority = %+v, want exactly one choice", in)
	}
	if got := d.Options[in.Choices[0]].Kind; got != "pass" {
		t.Fatalf("already-attached equip = %s (option %d), want pass — an attached equipment is never re-activated, however plausible the other bearer", got, in.Choices[0])
	}
}

// TestA1FirstAttachChosen is A1's negative half: the rule must not disable
// equipping altogether. An UNattached equipment (AttachedTo == 0) with at
// least one own battlefield creature really lands somewhere — the first
// attach changes the board — so its Equip is worth taking and IS chosen.
// Without this, A1's already-attached rule would silently disable equipment
// entirely: every Equip would read as a no-op and the source would sit
// unattached forever. The gate deletes equipNoOp's AttachedTo read (treats
// every equip as a no-op) and this test fails: the policy passes instead of
// equipping.
func TestA1FirstAttachChosen(t *testing.T) {
	b := Board{IsMain: true,
		Creatures: TableOf(map[state.ObjID]Creature{
			22: {Power: 2, Toughness: 2, Controller: 0},
		}),
		Cards: TableOf(map[state.ObjID]Card{41: {AttachedTo: 0}}),
	}
	d := abilityPriority(41, "Lightning Greaves: Equip 0")
	in := Decide(b, d, rng(1))
	if err := d.Validate(in); err != nil {
		t.Fatalf("intent %+v failed Validate: %v", in, err)
	}
	if len(in.Choices) != 1 {
		t.Fatalf("priority = %+v, want exactly one choice", in)
	}
	if got := d.Options[in.Choices[0]].Kind; got != "ability" {
		t.Fatalf("first attach = %s, want the ability chosen (the unattached equipment really lands on the 2/2)", got)
	}
}

// TestA2CheaperAbilityChosen pins rule A2 (ability.go): among two abilities
// worth activating, the cheaper one is chosen — a free ability forgoes
// nothing. Both sources sit unattached (AttachedTo 0, so A1 never fires on
// either) and offer a numeric cost in their label: "Equip 1" versus
// "Equip 4". The gate deletes the cost term from abilityScore's arithmetic
// (scores both abilities identically) and, since they are offered in
// ascending-cost order, the position-first habit would pick the dearer one
// instead and this test fails.
func TestA2CheaperAbilityChosen(t *testing.T) {
	b := Board{IsMain: true,
		Creatures: TableOf(map[state.ObjID]Creature{
			70: {Power: 3, Toughness: 3, Controller: 0},
		}),
		Cards: TableOf(map[state.ObjID]Card{5: {}, 6: {}}), // both unattached: no A1 no-op
	}
	d := &decision.Decision{Seq: 1, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "ability", Obj: 5, Label: "Bonehoard: Equip 1"},
			{Index: 1, Kind: "ability", Obj: 6, Label: "Heavy: Equip 4"},
			{Index: 2, Kind: "pass"},
		}}
	in := Decide(b, d, rng(1))
	if err := d.Validate(in); err != nil {
		t.Fatalf("intent %+v failed Validate: %v", in, err)
	}
	if len(in.Choices) != 1 || in.Choices[0] != 0 {
		t.Fatalf("A2 = %+v, want the cheaper (option 0, Equip 1) chosen over option 1 (Equip 4)", in)
	}
}

// TestA3TieBreaksOnIndex pins rule A3 (ability.go): two abilities of equal
// score (same cost, both worth taking) tie on option index — the lower index
// wins — and the answer is identical over repeated calls. A map range or a
// wall clock here would (a) not be deterministic across the two adapter
// halves and (b) break replay; this test calls the whole Decide twenty times
// and demands the same choice each time.
func TestA3TieBreaksOnIndex(t *testing.T) {
	b := Board{IsMain: true,
		Creatures: TableOf(map[state.ObjID]Creature{
			70: {Power: 3, Toughness: 3, Controller: 0},
		}),
		Cards: TableOf(map[state.ObjID]Card{5: {}, 6: {}}),
	}
	d := &decision.Decision{Seq: 1, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "ability", Obj: 5, Label: "A: Equip 2"},
			{Index: 1, Kind: "ability", Obj: 6, Label: "B: Equip 2"},
			{Index: 2, Kind: "pass"},
		}}
	for i := 0; i < 20; i++ {
		in := Decide(b, d, rng(uint64(i)))
		if err := d.Validate(in); err != nil {
			t.Fatalf("call %d: intent %+v failed Validate: %v", i, in, err)
		}
		if len(in.Choices) != 1 || in.Choices[0] != 0 {
			t.Fatalf("call %d: A3 tie = %+v, want the lower index (option 0) every time", i, in)
		}
	}
}

// TestA4NoWorthtakingFallsThroughToPass pins rule A4 (ability.go): when no
// ability ranks as worth taking, the ability block falls through to the
// explicit "pass" option — the exact thing that ends a turn the old
// position-first code spent re-activating a no-op forever. This is the
// closing-half of A1's board here is the same one-creature + attached
// equipment setup, but the assertion is on the PASS fallthrough and the
// loop ends. The gate deletes the pass fallthrough (the ability block
// returns nothing and clamp tops up into an activation), and the priority
// decision's Min 1 makes that leak: the intent would land on the ability,
// exactly the I-1(b) defect fix rounds 1 and 2 fought.
func TestA4NoWorthtakingFallsThroughToPass(t *testing.T) {
	b := Board{IsMain: true,
		Creatures: TableOf(map[state.ObjID]Creature{22: {Power: 2, Toughness: 2, Controller: 0}}),
		Cards:     TableOf(map[state.ObjID]Card{41: {AttachedTo: 22}}),
	}
	d := abilityPriority(41, "Lightning Greaves: Equip 0")
	in := Decide(b, d, rng(1))
	if err := d.Validate(in); err != nil {
		t.Fatalf("intent %+v failed Validate: %v", in, err)
	}
	if len(in.Choices) != 1 {
		t.Fatalf("priority = %+v, want exactly one choice", in)
	}
	if got := d.Options[in.Choices[0]].Kind; got != "pass" {
		t.Fatalf("A4 = %s (option %d), want the explicit pass when nothing ranks as worth taking", got, in.Choices[0])
	}
}

// TestA1ScopedToAttachAbilities pins X1's promotion into the production
// policy: A1's attachment no-op rule is a fact about an ATTACH only
// (decision.Option.Attach). An attached Aura's or Equipment's own
// non-attach ability (Holy Armor's pump; AttachedTo != 0) and a creatureless
// seat's non-attach ability (Tower of Eons) are activated by Decide and by
// ExploreDecide alike, while a re-equip of an attached Equipment and an
// equip with no creature to land on are still declined by both. The gate
// restores the broad reading (equipNoOp ignoring Attach) and the two
// non-attach halves fail: the policy passes instead of activating.
func TestA1ScopedToAttachAbilities(t *testing.T) {
	policies := []struct {
		name string
		fn   func(Board, *decision.Decision, *rand.Rand) decision.Intent
	}{{"Decide", Decide}, {"ExploreDecide", ExploreDecide}}
	attached := Board{IsMain: true,
		Creatures: TableOf(map[state.ObjID]Creature{22: {Power: 2, Toughness: 2, Controller: 0}}),
		Cards:     TableOf(map[state.ObjID]Card{41: {AttachedTo: 22}}),
	}
	creatureless := Board{IsMain: true,
		Creatures: TableOf(map[state.ObjID]Creature{}),
		Cards:     TableOf(map[state.ObjID]Card{41: {}}),
	}
	cases := []struct {
		name   string
		b      Board
		label  string
		attach bool
		want   string
	}{
		{"attached aura pump", attached, "Holy Armor: Enchanted creature gets +0/+2 until end of turn.", false, "ability"},
		{"creatureless non-attach", creatureless, "Tower of Eons: 8, T: You gain 10 life.", false, "ability"},
		{"attached re-equip", attached, "Lightning Greaves: Equip 0", true, "pass"},
		{"creatureless equip", creatureless, "Lightning Greaves: Equip 0", true, "pass"},
	}
	for _, p := range policies {
		for _, c := range cases {
			d := abilityPriority(41, c.label)
			d.Options[0].Attach = c.attach
			for seed := uint64(0); seed < 8; seed++ {
				in := p.fn(c.b, d, rng(seed))
				if err := d.Validate(in); err != nil {
					t.Fatalf("%s %s seed %d: intent %+v failed Validate: %v", p.name, c.name, seed, in, err)
				}
				if got := d.Options[in.Choices[0]].Kind; got != c.want {
					t.Fatalf("%s %s seed %d = %s, want %s", p.name, c.name, seed, got, c.want)
				}
			}
		}
	}
}
