package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// TestDargoSacChoicePreservesLaterArtifactPart pins the cast-side half of the
// sequential-sacrifice feasibility rule: a broad first Sac part followed by a
// narrower second part must not offer the first part a candidate that strands
// the second. With one artifact and one creature and X=1, Dargo's
// Sac<X/Artifact.orCreature> must offer ONLY the creature; the artifact is
// the sole candidate the Sac<1/Artifact> part can pay with.
func TestDargoSacChoicePreservesLaterArtifactPart(t *testing.T) {
	t.Parallel()
	e, spell, ids := dargoEngine(t, []string{
		"Name:Anvil\nTypes:Artifact\nOracle:x\n",
		"Name:Crab\nTypes:Creature\nPT:1/1\nOracle:x\n",
	}, "RRRRR")
	var artifact, creature state.ObjID
	for _, id := range ids {
		o := e.G.Obj(id)
		if o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: candidate %d in %s, want battlefield", id, o.Zone)
		}
		if strings.Contains(strings.Join(o.Face().Types, " "), "Artifact") {
			artifact = id
		}
		if strings.Contains(strings.Join(o.Face().Types, " "), "Creature") {
			creature = id
		}
	}
	if artifact == 0 || creature == 0 || artifact == creature {
		t.Fatalf("precondition: distinct artifact and creature required: artifact=%d creature=%d", artifact, creature)
	}
	base := pay.WithSpellAbilityExtras(e.G.Obj(spell).Face(), e.castOfferBase(0, spell))
	extra := ParseCost("Sac<X/Artifact>")
	if len(base.Sac) != 1 || len(extra.Sac) != 1 || base.Sac[0].Spec == extra.Sac[0].Spec {
		t.Fatalf("precondition: expected different broad and artifact-only Sac parts: base=%+v extra=%+v", base.Sac, extra.Sac)
	}
	pc := &pendingCast{player: 0, card: spell, from: state.ZHand, ability: -1, cost: base.Plus(extra), x: 1, xDone: true}
	e.cast = pc
	if !e.sacAsk() {
		t.Fatal("sacrifice choice did not suspend")
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Min != 1 || d.Max != 1 {
		t.Fatalf("pending = %+v, want one-sacrifice choice", d)
	}
	if len(d.Options) != 1 || d.Options[0].Obj != creature {
		t.Fatalf("first sacrifice options = %+v, want only creature %d (artifact %d must remain for next part)", d.Options, creature, artifact)
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
		t.Fatalf("the feasible sacrifice answer does not validate: %v", err)
	}
	submitChoices(t, e, d.Options[0].Index)
	next := e.Pending()
	if next == nil || next.Kind != decision.KChoose || len(next.Options) != 1 || next.Options[0].Obj != artifact {
		t.Fatalf("second cast sacrifice choice = %+v, want the reserved artifact %d", next, artifact)
	}
	submitChoices(t, e, next.Options[0].Index)
	if e.G.Obj(artifact).Zone != state.ZGraveyard || e.G.Obj(creature).Zone != state.ZGraveyard || e.G.Obj(spell).Zone != state.ZStack {
		t.Fatalf("cast did not complete after feasible answers: spell=%s artifact=%s creature=%s", e.G.Obj(spell).Zone, e.G.Obj(artifact).Zone, e.G.Obj(creature).Zone)
	}
}

// TestDargoSacContinuationBotAnswerValidatesAndCompletes runs the
// deterministic bot's own answer through the SAME binding board (the
// attack_restrict_limit bot-repair pattern): at the filtered first ask the
// bot's Decide answer and its Clamp repair both pass Decision.Validate, and
// submitting the answer progresses the cast to the artifact ask instead of
// re-posing the identical question (the livelock class the one-home rule
// exists for). Two creatures give the first part a real choice; the artifact
// is still excluded because it is the later part's only candidate.
func TestDargoSacContinuationBotAnswerValidatesAndCompletes(t *testing.T) {
	t.Parallel()
	e, spell, ids := dargoEngine(t, []string{
		"Name:Anvil\nTypes:Artifact\nOracle:x\n",
		"Name:Crab\nTypes:Creature\nPT:1/1\nOracle:x\n",
		"Name:Crab2\nTypes:Creature\nPT:1/1\nOracle:x\n",
	}, "RRRRR")
	var artifact state.ObjID
	var creatures []state.ObjID
	for _, id := range ids {
		o := e.G.Obj(id)
		if o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: candidate %d in %s, want battlefield", id, o.Zone)
		}
		types := strings.Join(o.Face().Types, " ")
		switch {
		case strings.Contains(types, "Artifact") && !strings.Contains(types, "Creature"):
			artifact = id
		case strings.Contains(types, "Creature"):
			creatures = append(creatures, id)
		}
	}
	if artifact == 0 || len(creatures) != 2 {
		t.Fatalf("precondition: want one artifact and two creatures, got artifact=%d creatures=%v", artifact, creatures)
	}
	// The synthetic second part rides the Dargo base cost, as the offer-gate
	// regressions build it; the pending cast is entered by hand (same shape
	// as TestDargoSacChoicePreservesLaterArtifactPart) because the REAL cast
	// carries only Dargo's own Sac part.
	base := pay.WithSpellAbilityExtras(e.G.Obj(spell).Face(), e.castOfferBase(0, spell))
	extra := ParseCost("Sac<X/Artifact>")
	if len(base.Sac) != 1 || !base.Sac[0].Announced || len(extra.Sac) != 1 {
		t.Fatalf("precondition: want Dargo's announced Sac part plus a synthetic Sac<X/Artifact>: base=%+v extra=%+v", base.Sac, extra.Sac)
	}
	pc := &pendingCast{player: 0, card: spell, from: state.ZHand, ability: -1, cost: base.Plus(extra), x: 1, xDone: true}
	e.cast = pc
	if !e.sacAsk() {
		t.Fatal("sacrifice choice did not suspend")
	}
	ds := e.Pending()
	if ds == nil || ds.Kind != decision.KChoose || ds.Min != 1 || ds.Max != 1 {
		t.Fatalf("first sacrifice ask = %+v, want a one-of choice", ds)
	}
	if len(ds.Options) != 2 {
		t.Fatalf("first sacrifice options = %+v, want exactly the two creatures", ds.Options)
	}
	for _, o := range ds.Options {
		if o.Obj == artifact {
			t.Fatalf("artifact %d offered at the first ask although the later Sac<1/Artifact> part needs it: %+v", artifact, ds.Options)
		}
	}
	// The policy's own answer must clear Validate and Clamp on the real
	// offered decision, and its pick must be one of the creatures.
	in := newTestBot(1).answer(e, ds)
	if err := ds.Validate(in); err != nil {
		t.Fatalf("bot answer failed Decision.Validate: choices %v, err %v", in.Choices, err)
	}
	if clamped := botpolicy.Clamp(ds, in); ds.Validate(clamped) != nil {
		t.Fatalf("Clamp left an answer Validate rejects: choices %v, err %v", clamped.Choices, ds.Validate(clamped))
	}
	if len(in.Choices) != 1 {
		t.Fatalf("bot choices = %v, want exactly one sacrifice", in.Choices)
	}
	picked := ds.Options[in.Choices[0]].Obj
	if picked != creatures[0] && picked != creatures[1] {
		t.Fatalf("bot picked %d, want one of the creatures %v", picked, creatures)
	}
	submitChoices(t, e, in.Choices...)
	// The cast progressed: the next ask is the reserved artifact, not a
	// re-posed identical first ask.
	next := e.Pending()
	if next == nil || next.Kind != decision.KChoose || len(next.Options) != 1 || next.Options[0].Obj != artifact {
		t.Fatalf("second sacrifice ask = %+v, want exactly the reserved artifact %d", next, artifact)
	}
	submitChoices(t, e, next.Options[0].Index)
	other := creatures[0]
	if picked == creatures[0] {
		other = creatures[1]
	}
	if e.G.Obj(spell).Zone != state.ZStack {
		t.Fatalf("spell zone = %s, want stack (cast completed after both feasible sacrifices)", e.G.Obj(spell).Zone)
	}
	if e.G.Obj(artifact).Zone != state.ZGraveyard || e.G.Obj(picked).Zone != state.ZGraveyard {
		t.Fatalf("artifact=%s picked=%s, want both sacrificed", e.G.Obj(artifact).Zone, e.G.Obj(picked).Zone)
	}
	if e.G.Obj(other).Zone != state.ZBattlefield {
		t.Fatalf("unpicked creature zone = %s, want battlefield (only one creature was paid)", e.G.Obj(other).Zone)
	}
}

// manaActivateOption finds the offered mana-activation option for id in the
// pending priority decision: a costed AB$ Mana ability is posed as an
// "activate" option; the non-fatal form returns ok=false when absent.
func manaActivateOption(e *Engine, id state.ObjID) (decision.Option, bool) {
	d := e.Pending()
	if d == nil {
		return decision.Option{}, false
	}
	for _, o := range d.Options {
		if (o.Kind == "activate" || o.Kind == "ability") && o.Obj == id {
			return o, true
		}
	}
	return decision.Option{}, false
}

// TestManaAbilitySacChoicePreservesLaterArtifactPart pins the mana-ability
// half of the sequential-sacrifice feasibility rule THROUGH THE REAL
// PRIORITY-WINDOW ACTIVATION: the overlapping-parts AB$ Mana ability is
// offered exactly because a distinct assignment exists, its first sacrifice
// ask excludes the object the later Sac<1/Artifact> part needs, the
// deterministic bot's answer passes Decision.Validate and Clamp, and the
// ability settles both parts and produces its mana.
func TestManaAbilitySacChoicePreservesLaterArtifactPart(t *testing.T) {
	t.Parallel()
	outlet := "Name:Outlet\nTypes:Enchantment\n" +
		"A:AB$ Mana | Cost$ Sac<1/Artifact;Creature/artifact or creature> Sac<1/Artifact> | Produced$ R | SpellDescription$ Add {R}.\nOracle:x\n"
	artifactCreature := "Name:Artifact Crab\nTypes:Artifact Creature\nPT:1/1\nOracle:x\n"
	creatureScript := "Name:Crab\nTypes:Creature\nPT:1/1\nOracle:x\n"
	e, _, _ := newFixtureDeck(t, 82, outlet, artifactCreature, creatureScript, creatureScript)
	source := moveSeeded(t, e, 0, outlet, state.ZBattlefield)
	artifact := putCreature(t, e, 0, artifactCreature)
	creatureA := putCreature(t, e, 0, creatureScript)
	creatureB := putCreature(t, e, 0, creatureScript)
	// Re-pose priority AFTER the moves: the original pending decision was
	// posed before the outlet was on the battlefield, so its option list
	// cannot carry the activation yet.
	e.pending = nil
	e.priorityRound()
	for _, id := range []state.ObjID{source, artifact, creatureA, creatureB} {
		if e.G.Obj(id).Zone != state.ZBattlefield {
			t.Fatalf("precondition: object %d is in %s, want battlefield", id, e.G.Obj(id).Zone)
		}
	}
	for _, id := range []state.ObjID{creatureA, creatureB} {
		if id == artifact || !strings.Contains(strings.Join(e.G.Obj(id).Face().Types, " "), "Creature") || strings.Contains(strings.Join(e.G.Obj(id).Face().Types, " "), "Artifact") {
			t.Fatalf("precondition: ordinary creature %d has unexpected types %v", id, e.G.Obj(id).Face().Types)
		}
	}
	if !strings.Contains(strings.Join(e.G.Obj(artifact).Face().Types, " "), "Artifact") || !strings.Contains(strings.Join(e.G.Obj(artifact).Face().Types, " "), "Creature") {
		t.Fatalf("precondition: narrow-filter candidate %d must be an artifact creature", artifact)
	}
	if pool := e.G.Players[0].Pool; pool[state.MR] != 0 {
		t.Fatalf("precondition: pool %+v already holds red, want an empty pool so the produced mana is attributable", pool)
	}
	face := e.G.Obj(source).Face()
	if len(face.Abilities) == 0 {
		t.Fatalf("precondition: fixture did not compile mana ability: face=%+v", face)
	}
	ma := face.Abilities[0]
	cost := e.parseCost(ma.Params["Cost"])
	if len(cost.Sac) != 2 || cost.Sac[0].Spec == cost.Sac[1].Spec {
		t.Fatalf("precondition: mana ability must compile overlapping Sac parts: %+v", cost.Sac)
	}
	if _, ok := pay.ManaSacrifices(asPayer(e), 0, source, cost); !ok {
		t.Fatal("precondition: mana ability has no distinct sacrifice assignment")
	}
	if _, ok := pay.ManaSacrifices(asPayer(e), 0, source, ParseCost("Sac<1/Artifact> Sac<1/Artifact>")); ok {
		t.Fatal("mana cost with two artifact parts was payable using the sole artifact candidate")
	}
	// The offer gate: a distinct assignment exists, so the activation is
	// offered at priority.
	opt, ok := manaActivateOption(e, source)
	if !ok {
		t.Fatalf("mana ability with a distinct assignment was withheld at priority: %+v", e.Pending().Options)
	}
	submitChoices(t, e, opt.Index)
	first := e.Pending()
	if first == nil || first.Kind != decision.KChoose || first.Min != 1 || first.Max != 1 || len(first.Options) != 2 {
		t.Fatalf("mana activation first choice = %+v, want a one-of-two ask over the two ordinary creatures", first)
	}
	chosen := -1
	for _, option := range first.Options {
		if option.Obj == artifact {
			t.Fatalf("artifact %d is offered despite being required for the later part: %+v", artifact, first.Options)
		}
		if option.Obj == creatureA || option.Obj == creatureB {
			chosen = option.Index
		}
	}
	if chosen < 0 {
		t.Fatalf("no ordinary creature offered: %+v", first.Options)
	}
	// The bot's own answer must agree with Validate and Clamp on the real
	// offered ask, and must not name the reserved artifact.
	in := newTestBot(1).answer(e, first)
	if err := first.Validate(in); err != nil {
		t.Fatalf("bot answer failed Decision.Validate: choices %v, err %v", in.Choices, err)
	}
	if clamped := botpolicy.Clamp(first, in); first.Validate(clamped) != nil {
		t.Fatalf("Clamp left an answer Validate rejects: choices %v, err %v", clamped.Choices, first.Validate(clamped))
	}
	for _, c := range in.Choices {
		if first.Options[c].Obj == artifact {
			t.Fatalf("bot answered the reserved artifact %d: choices %v", artifact, in.Choices)
		}
	}
	submitChoices(t, e, in.Choices...)
	// Both parts settled and the mana was produced; the unused creature and
	// the outlet itself stay on the battlefield.
	if pool := e.G.Players[0].Pool; pool[state.MR] != 1 {
		t.Fatalf("pool after activation = %+v, want one red (Produced$ R resolved)", pool)
	}
	if e.G.Obj(artifact).Zone != state.ZGraveyard {
		t.Fatalf("artifact zone = %s, want graveyard (paid by the later part)", e.G.Obj(artifact).Zone)
	}
	if e.G.Obj(creatureA).Zone != state.ZGraveyard && e.G.Obj(creatureB).Zone != state.ZGraveyard {
		t.Fatalf("neither ordinary creature was sacrificed: creatureA=%s creatureB=%s", e.G.Obj(creatureA).Zone, e.G.Obj(creatureB).Zone)
	}
	if e.G.Obj(source).Zone != state.ZBattlefield {
		t.Fatalf("outlet zone = %s, want battlefield (the ability resolved)", e.G.Obj(source).Zone)
	}
}

// TestManaAbilityOverlappingSacPartsNotOfferedWithoutAssignment is the offer
// gate's negative half: with ONLY the artifact creature on the battlefield,
// both Sac parts can only be paid by the same object, so the activation must
// not be offered at all.
func TestManaAbilityOverlappingSacPartsNotOfferedWithoutAssignment(t *testing.T) {
	t.Parallel()
	outlet := "Name:Outlet\nTypes:Enchantment\n" +
		"A:AB$ Mana | Cost$ Sac<1/Artifact;Creature/artifact or creature> Sac<1/Artifact> | Produced$ R | SpellDescription$ Add {R}.\nOracle:x\n"
	artifactCreature := "Name:Artifact Crab\nTypes:Artifact Creature\nPT:1/1\nOracle:x\n"
	e, _, _ := newFixtureDeck(t, 83, outlet, artifactCreature)
	source := moveSeeded(t, e, 0, outlet, state.ZBattlefield)
	artifact := putCreature(t, e, 0, artifactCreature)
	e.pending = nil
	e.priorityRound()
	if e.G.Obj(source).Zone != state.ZBattlefield || e.G.Obj(artifact).Zone != state.ZBattlefield {
		t.Fatalf("precondition: outlet=%s candidate=%s, want both on the battlefield",
			e.G.Obj(source).Zone, e.G.Obj(artifact).Zone)
	}
	if battlefield := e.G.Zone(state.ZBattlefield, 0); len(battlefield) != 2 {
		t.Fatalf("precondition: %d permanents on the battlefield, want exactly the outlet and its candidate", len(battlefield))
	}
	ma := e.G.Obj(source).Face().Abilities[0]
	cost := e.parseCost(ma.Params["Cost"])
	if len(cost.Sac) != 2 {
		t.Fatalf("precondition: mana ability must compile two Sac parts: %+v", cost.Sac)
	}
	if _, ok := pay.ManaSacrifices(asPayer(e), 0, source, cost); ok {
		t.Fatal("precondition: a distinct assignment exists -- this board must force both parts onto the one artifact creature")
	}
	if _, ok := manaActivateOption(e, source); ok {
		t.Fatalf("the overlapping-part mana ability was offered with no distinct assignment: %+v", e.Pending().Options)
	}
}

// TestCastMultiUnitSacPartLeavesLaterPartItsOwnCandidate pins the split
// shape's nontrivial case: a multi-unit CURRENT part beside a later part is
// paid one unit per decision, and each unit's options keep a complete
// distinct assignment for the remaining units AND the later part -- the
// three artifacts are offered three, then two, then the later part takes
// the last one alone.
func TestCastMultiUnitSacPartLeavesLaterPartItsOwnCandidate(t *testing.T) {
	t.Parallel()
	e, spell, ids := dargoEngine(t, []string{
		"Name:Anvil\nTypes:Artifact\nOracle:x\n",
		"Name:Anvil2\nTypes:Artifact\nOracle:x\n",
		"Name:Anvil3\nTypes:Artifact\nOracle:x\n",
	}, "RRRRR")
	for i, id := range ids {
		if e.G.Obj(id).Zone != state.ZBattlefield {
			t.Fatalf("precondition: artifact %d in %s, want battlefield", i, e.G.Obj(id).Zone)
		}
	}
	if a, b, c := e.G.Obj(ids[0]), e.G.Obj(ids[1]), e.G.Obj(ids[2]); a.ID == b.ID || b.ID == c.ID || a.ID == c.ID {
		t.Fatal("precondition: the three artifacts must be distinct objects")
	}
	cost := ParseCost("Sac<2/Artifact> Sac<1/Artifact>")
	if len(cost.Sac) != 2 || cost.Sac[0].N != 2 || cost.Sac[1].N != 1 {
		t.Fatalf("precondition: cost did not compile two Sac parts of 2 and 1: %+v", cost.Sac)
	}
	pc := &pendingCast{player: 0, card: spell, from: state.ZHand, ability: -1, cost: cost}
	e.cast = pc
	if !e.sacAsk() {
		t.Fatal("sacrifice choice did not suspend")
	}
	paid := map[state.ObjID]bool{}
	for want := 3; want >= 1; want-- {
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose || d.Min != 1 || d.Max != 1 || len(d.Options) != want {
			t.Fatalf("unit ask %d: %+v, want a one-of ask over exactly %d candidates", 4-want, d, want)
		}
		for _, o := range d.Options {
			if paid[o.Obj] {
				t.Fatalf("unit ask %d re-offered already-paid object %d: %+v", 4-want, o.Obj, d.Options)
			}
		}
		// After the first two units the sole remaining artifact is offered to
		// the LAST part alone, never consumed early.
		pick := d.Options[0]
		paid[pick.Obj] = true
		submitChoices(t, e, pick.Index)
	}
	if e.G.Obj(spell).Zone != state.ZStack {
		t.Fatalf("spell zone = %s, want stack (cast completed after the three-unit cost)", e.G.Obj(spell).Zone)
	}
	for i, id := range ids {
		if e.G.Obj(id).Zone != state.ZGraveyard {
			t.Fatalf("artifact %d zone = %s, want graveyard (all three sacrificed)", i, e.G.Obj(id).Zone)
		}
	}
}
