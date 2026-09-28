package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// Acceptance for the offer/ask target-feasibility agreement on the real corpus
// cards the autopay-fuzz audit named. The shared rule is
// rules/legal.go's targetChoiceFeasible; these tests pin the per-card family
// outcome (CR 601.2c: an offered cast must be able to announce a legal
// target).

// corpusTargetFeasibleCard seeds a real corpus card into seat 0's hand and
// returns the engine and the card id. It asserts the preconditions every test
// here depends on: the card is face-up in hand and its real spell ability
// carries the expected ValidTgts.
func corpusTargetFeasibleCard(t *testing.T, seed uint64, rel, wantTgts string) (*Engine, state.ObjID) {
	t.Helper()
	e, _, id := newFixtureDeck(t, seed, corpusCardText(t, rel))
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZHand || o.Face() == nil {
		t.Fatalf("precondition: %s is not a face-up hand card: %+v", rel, o)
	}
	sa := o.Face().SpellAbility()
	if sa == nil || sa.Params["ValidTgts"] != wantTgts {
		t.Fatalf("precondition: %s spell ability = %+v, want ValidTgts %q", rel, sa, wantTgts)
	}
	return e, id
}

// TestIncriminateSameControllerSplitIsWithheld pins Incriminate
// ("Choose two target creatures controlled by the same player"):
// TargetMin$ 2 | TargetMax$ 2 | TargetsWithSameController$ True. With two
// legal creatures split across controllers no legal pair exists, so the cast
// must not be offered. Before the shared rule the count-only census offered it
// and the ask reversed it with "cast aborted: no legal target".
func TestIncriminateSameControllerSplitIsWithheld(t *testing.T) {
	t.Parallel()
	e, spell := corpusTargetFeasibleCard(t, 7201, "i/incriminate.txt", "Creature")
	bearA, bearB := bearPermanent(t, e, 0), bearPermanent(t, e, 1)
	a, b := e.G.Obj(bearA), e.G.Obj(bearB)
	// Precondition: exactly two legal creatures, under DISTINCT controllers,
	// and the real SA carries the same-controller constraint with a mandatory
	// minimum of two.
	if a == nil || b == nil || a.Zone != state.ZBattlefield || b.Zone != state.ZBattlefield || a.Controller == b.Controller {
		t.Fatalf("precondition: bears are not battlefield permanents under distinct controllers: %+v %+v", a, b)
	}
	sa := e.G.Obj(spell).Face().SpellAbility()
	if sa.Params["TargetsWithSameController"] != "True" || sa.Params["TargetMin"] != "2" || sa.Params["TargetMax"] != "2" {
		t.Fatalf("precondition: Incriminate lost its mandatory same-controller pair: %+v", sa)
	}
	candidates := e.legalTargetCandidates(0, spell, spell, sa)
	_, _, capacity, constrained := e.sameControllerTargetBounds(sa, candidates, 2, 2)
	if len(candidates) != 2 || !constrained || capacity != 1 {
		t.Fatalf("precondition: candidates %d, capacity %d (constrained %v), want 2 candidates with capacity 1", len(candidates), capacity, constrained)
	}
	addMana(t, e, 0, "CB")
	if castOffered(e, spell) {
		t.Fatal("Incriminate offered with two creatures split across controllers: no legal same-controller pair exists (CR 601.2c)")
	}
	if hasNote(e, "cast aborted: no legal target") {
		t.Fatal("Incriminate reached the target ask and aborted; the offer must withhold it")
	}
}

// TestCannibalizeSameControllerSplitIsWithheld is the sibling card family with
// the identical target declaration shape ("Choose two target creatures
// controlled by the same player. Exile one ..."), so the fix covers it too.
func TestCannibalizeSameControllerSplitIsWithheld(t *testing.T) {
	t.Parallel()
	e, spell := corpusTargetFeasibleCard(t, 7202, "c/cannibalize.txt", "Creature")
	bearA, bearB := bearPermanent(t, e, 0), bearPermanent(t, e, 1)
	a, b := e.G.Obj(bearA), e.G.Obj(bearB)
	if a == nil || b == nil || a.Zone != state.ZBattlefield || b.Zone != state.ZBattlefield || a.Controller == b.Controller {
		t.Fatalf("precondition: bears are not battlefield permanents under distinct controllers: %+v %+v", a, b)
	}
	sa := e.G.Obj(spell).Face().SpellAbility()
	if sa.Params["TargetsWithSameController"] != "True" || sa.Params["TargetMin"] != "2" || sa.Params["TargetMax"] != "2" {
		t.Fatalf("precondition: Cannibalize lost its mandatory same-controller pair: %+v", sa)
	}
	addMana(t, e, 0, "CB")
	if castOffered(e, spell) {
		t.Fatal("Cannibalize offered with two creatures split across controllers: no legal same-controller pair exists (CR 601.2c)")
	}
}

// TestIncriminateSameControllerPairIsOffered is the positive half: with BOTH
// legal creatures under one controller the same-controller pair exists, so the
// cast must stay offered and pose an answerable two-target ask.
func TestIncriminateSameControllerPairIsOffered(t *testing.T) {
	t.Parallel()
	e, spell := corpusTargetFeasibleCard(t, 7203, "i/incriminate.txt", "Creature")
	bearA, bearB := bearPermanent(t, e, 1), bearPermanent(t, e, 1)
	if a, b := e.G.Obj(bearA), e.G.Obj(bearB); a == nil || b == nil || a.Controller != 1 || b.Controller != 1 {
		t.Fatalf("precondition: both bears must be battlefield permanents under one controller: %+v %+v", a, b)
	}
	addMana(t, e, 0, "CB")
	if !castOffered(e, spell) {
		t.Fatal("Incriminate withheld although two same-controller creatures make a legal pair")
	}
	var cast *decision.Option
	for _, o := range castOptions(t, e) {
		if o.Obj == spell {
			c := o
			cast = &c
		}
	}
	if cast == nil {
		t.Fatal("precondition: the cast option disappeared before submission")
	}
	submitChoices(t, e, cast.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget || d.Min != 2 || d.Max != 2 {
		t.Fatalf("pending = %+v, want a mandatory two-target ask", d)
	}
}

// TestIntoTheFloodMawGiftRequiresAFeasibleTarget pins the Gift-dependent
// mandatory target branch with both own and opponent creatures present.
func TestIntoTheFloodMawGiftRequiresAFeasibleTarget(t *testing.T) {
	t.Parallel()
	e, spell := corpusTargetFeasibleCard(t, 7205, "i/into_the_flood_maw.txt", "Creature.OppCtrl")
	ownCreature := bearPermanent(t, e, 0)
	opponentCreature := bearPermanent(t, e, 1)
	if o := e.G.Obj(ownCreature); o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 {
		t.Fatalf("precondition: own creature is not a battlefield permanent: %+v", o)
	}
	if o := e.G.Obj(opponentCreature); o == nil || o.Zone != state.ZBattlefield || o.Controller != 1 {
		t.Fatalf("precondition: opponent creature is not a battlefield permanent: %+v", o)
	}
	addMana(t, e, 0, "U")
	if !castOffered(e, spell) {
		t.Fatal("Into the Flood Maw was withheld despite an opponent creature target")
	}
	var cast *decision.Option
	for _, opt := range castOptions(t, e) {
		if opt.Obj == spell {
			c := opt
			cast = &c
		}
	}
	if cast == nil {
		t.Fatal("precondition: cast offer disappeared")
	}
	submitChoices(t, e, cast.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("gift election pending = %+v, want KChoose", d)
	}
	answerGift(t, e, false)
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("after declining gift target decision = %+v", d)
	}
	if targetOptionIndex(d, ownCreature) >= 0 {
		t.Fatal("own creature was offered as target for Into the Flood Maw")
	}
	idx := targetOptionIndex(d, opponentCreature)
	if idx < 0 {
		t.Fatalf("opponent creature missing from legal target options: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	if hasNote(e, "cast aborted: no legal target") {
		t.Fatal("the cast reversed despite announcing its legal opponent creature")
	}
	if e.G.Obj(spell).Zone == state.ZHand {
		t.Fatal("castable target selection did not advance the spell from hand")
	}
}

// The flash permission is only satisfied by a target the grant covers. The
// offered target list must already be restricted, so choosing an opponent's
// otherwise-legal permanent cannot produce a CR 601.2e reversal.
func TestFlashPhotographyAnnouncementUsesCoveredTarget(t *testing.T) {
	t.Parallel()
	e, spell := offTurnFlashEngine(t, "Flash Photography", 2, 2)
	theirBear := battlePerm(t, e, 1, "Name:Their Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	myBear := battlePerm(t, e, 0, "Name:My Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	if !hasCastOption(e.legalActions(0), spell) {
		t.Fatal("precondition: Flash Photography not offered despite a qualifying target")
	}
	e.beginCast(0, decision.Option{Kind: "cast", Obj: spell})
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("target decision = %+v", d)
	}
	if istTargetOptionFor(d, theirBear) >= 0 {
		t.Fatalf("permission-uncovered opponent target was offered: %+v", d.Options)
	}
	if idx := istTargetOptionFor(d, myBear); idx < 0 {
		t.Fatalf("qualifying target missing from announcement: %+v", d.Options)
	} else {
		submitChoices(t, e, idx)
	}
	if hasNote(e, "flash permission's target requirement unmet") {
		t.Fatal("cast was reversed despite announcing a permission-covered target")
	}
}

// TestDisruptionAuraAttachAILogicDoesNotRestrictTargets pins the brief's aura
// family: Disruption Aura (Enchant artifact) carries SVar:AttachAITgts and
// SVar:AttachAILogic:Curse, which are Forge AI hints that must NOT restrict the
// legal targets. With only the caster's own artifact in play the K:Enchant
// expansion's ValidTgts$ Artifact still admits it, so the cast is offered and
// announces that artifact -- offer and announcement agree.
func TestDisruptionAuraAttachAILogicDoesNotRestrictTargets(t *testing.T) {
	t.Parallel()
	e, spell := corpusTargetFeasibleCard(t, 7204, "d/disruption_aura.txt", "Artifact")
	o := e.G.Obj(spell)
	// Precondition: the AI hints really are on the card (so this test would
	// catch a future reader that started honouring them).
	src := corpusCardText(t, "d/disruption_aura.txt")
	if !strings.Contains(src, "AttachAILogic:Curse") || !strings.Contains(src, "AttachAITgts:") {
		t.Fatalf("precondition: Disruption Aura lost its AttachAI hints")
	}
	if !strings.Contains(src, "K:Enchant:Artifact") {
		t.Fatalf("precondition: Disruption Aura lost its Enchant keyword")
	}
	_ = o
	gizmo := onBoardCard(t, e, 0, corpusCard(t, "Galvanic Key"))
	if g := e.G.Obj(gizmo); g == nil || g.Zone != state.ZBattlefield || g.Controller != 0 {
		t.Fatalf("precondition: the caster's own artifact is not on the battlefield: %+v", g)
	}
	// 2U for {2}{U}.
	addMana(t, e, 0, "UUC")
	if !castOffered(e, spell) {
		t.Fatal("Disruption Aura withheld although the caster's own artifact is a legal Enchant:Artifact target; the AttachAI hints must not restrict legal targets")
	}
	var cast *decision.Option
	for _, opt := range castOptions(t, e) {
		if opt.Obj == spell {
			c := opt
			cast = &c
		}
	}
	if cast == nil {
		t.Fatal("precondition: the cast option disappeared before submission")
	}
	submitChoices(t, e, cast.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("pending = %+v, want the aura's target ask", d)
	}
	idx := -1
	for _, opt := range d.Options {
		if opt.Obj == gizmo {
			idx = opt.Index
		}
	}
	if idx < 0 {
		t.Fatalf("the caster's own artifact is not among the aura's target options: %+v", d.Options)
	}
	// The offer/ask agreement must be a COMPLETING cast, not merely an
	// askable one: submit the artifact and assert the spell left the hand
	// without the CR 733.1 reversal (r2 review MINOR).
	submitChoices(t, e, idx)
	if hasNote(e, "cast aborted: no legal target") {
		t.Fatal("the aura's cast aborted with a legal artifact target available")
	}
	// The spell is pushed before the target ask (CR 601.2c before payment),
	// so a completing announcement leaves the card ON THE STACK, not back in
	// hand (the reversal zone) and not gone.
	if z := e.G.Obj(spell).Zone; z != state.ZStack {
		t.Fatalf("the aura's announcement left the spell in %s, want stack (a completing cast)", z)
	}
}
