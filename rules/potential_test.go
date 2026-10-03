package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// This file pins rules.PotentialActions and rules.PotentialMana — the
// server-side projection the auto-pass stop decision reads (view's
// potential_actions, rv2c): the engine's own legal-offer walk priced against
// the hypothetical pool the seat's untapped sources could produce. Every case
// here is one of the eight client-side pricing defects the projection
// replaces, so the client never re-derives castability again. The fixture
// board of the first test is the Jitte feedback snapshot's capture point
// (feedback/20260914T145022Z-64a8422c, seq 482): seat 0, main1, pool {C}{W},
// both lands tapped, Thalia on the battlefield raising the Jitte to {3}.

// jitteHandSrcs are the capture-point hand cards except Serra Avenger (whose
// cast carries a turn-4 Condition static the projection need not resolve to
// answer the Jitte question; the pool {C}{W} cannot pay her {W}{W} either).
var jitteHandSrcs = []string{
	"Name:Batterskull\nManaCost:5\nTypes:Artifact Equipment\nOracle:Living weapon\n",
	"Name:Palace Jailer\nManaCost:2 W W\nTypes:Creature Human Soldier\nPT:2/2\nOracle:When this enters, become the monarch.\n",
	"Name:Umezawa's Jitte\nManaCost:2\nTypes:Legendary Artifact Equipment\nK:Equip:2\nOracle:Whenever equipped creature deals combat damage, put two charge counters on CARDNAME.\n",
	"Name:Sanctum Prelate\nManaCost:1 W W\nTypes:Creature Human Cleric\nPT:2/2\nOracle:x\n",
	"Name:Recruiter of the Guard\nManaCost:2 W\nTypes:Creature Human Soldier\nPT:1/1\nOracle:x\n",
}

const thaliaSrc = "Name:Thalia, Guardian of Thraben\nManaCost:1 W\nTypes:Legendary Creature Human Soldier\nPT:2/1\nK:First Strike\n" +
	"S:Mode$ RaiseCost | ValidCard$ Card.nonCreature | Type$ Spell | Amount$ 1 | Description$ Noncreature spells cost {1} more to cast.\n" +
	"Oracle:First strike. Noncreature spells cost {1} more to cast.\n"

const jitteSnapshotPlains = "Name:Plains\nTypes:Basic Land Plains\nOracle:x\n"

const jitteSnapshotWasteland = "Name:Wasteland\nTypes:Land Wasteland\n" +
	"A:AB$ Mana | Cost$ T | Produced$ C | SpellDescription$ Add {C}.\n" +
	"Oracle:{T}: Add {C}.\n"

// jitteSnapshotBoard builds the capture-point board: the two lands already
// tapped (the reporter tapped them for the {C}{W} in the pool), Thalia
// untapped, and seat 0 in main1 with the snapshot pool {C:1, W:1}. The
// opening hand's Mountains are cleared (the reporter's hand held no lands)
// and the land drop is spent, so a play_land can never mask the cast answer.
func jitteSnapshotBoard(t *testing.T, withThalia bool) (*Engine, state.ObjID) {
	t.Helper()
	e := layerEngine(t)
	e.G.Step = state.StepMain1
	e.G.Players[0].LandsPlayed = 1
	e.G.SetZone(state.ZHand, 0, nil)
	pool := e.G.Players[0].Pool
	pool[state.MC] = 1
	pool[state.MW] = 1
	e.G.Players[0].Pool = pool
	plains := onBoard(t, e, 0, jitteSnapshotPlains)
	e.G.Obj(plains).SummonSick = false
	e.G.Obj(plains).Tapped = true
	wasteland := onBoard(t, e, 0, jitteSnapshotWasteland)
	e.G.Obj(wasteland).SummonSick = false
	e.G.Obj(wasteland).Tapped = true
	if withThalia {
		onBoard(t, e, 0, thaliaSrc)
	}
	var jitte state.ObjID
	for _, src := range jitteHandSrcs {
		id := onHand(t, e, 0, src)
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Umezawa's Jitte" {
			jitte = id
		}
	}
	return e, jitte
}

// restrictedBearBoard seats the rv2c-review drift pin: a plain {2} Bear in
// hand with a floating batch of two colourless RESTRICTED to the given
// RestrictValid$ class, both lands tapped, main1, land drop spent. The class
// is a parameter so the same fixture pins both sides of the boundary -- a
// non-matching class must be invisible to the ordinary offer gate (exactly
// the pool the payment will charge) while a matching one must be visible.
func restrictedBearBoard(t *testing.T, valid string) (*Engine, state.ObjID) {
	t.Helper()
	e := layerEngine(t)
	e.G.Step = state.StepMain1
	e.G.Players[0].LandsPlayed = 1
	e.G.SetZone(state.ZHand, 0, nil)
	card := onHand(t, e, 0, "Name:ProbeBear\nManaCost:2\nTypes:Creature Bear\nPT:1/1\nOracle:x\n")
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "C", Amount: 2,
		Text: events.ManaRestrictionText(valid, 0)})
	return e, card
}

// TestCastableExcludesNonMatchingRestrictedMana pins the ordinary offer gate's
// pool semantics against the raw floating pool (rv2c review, the drift the
// castable/castablePriced split introduced and this fixes): manaAvailableFor
// excludes a restricted batch a payment does not admit, so a {2} creature
// cannot be cast off {C:2 Spell.Artifact} -- the offer gate and the payment
// path must answer identically, or the engine offers a cast whose payment
// later aborts. The matching class ({Spell.Creature}) is the contrast half:
// the same batch pays, so the gate did not overcorrect. The potential walk's
// hypothetical pool deliberately keeps the RAW units (a mana over-bound: a
// wrongly withheld pass costs one idle stop, a wrongly eaten window loses the
// action), so the projection may still carry the cast here -- only the
// ordinary offer is pinned.
func TestCastableExcludesNonMatchingRestrictedMana(t *testing.T) {
	t.Parallel()
	e, bear := restrictedBearBoard(t, "Spell.Artifact")
	if pay.CostPayable(asPayer(e), 0, bear, false, ParseCost("2")) {
		t.Fatal("costPayable admitted a restricted batch the payment would refuse")
	}
	if e.castable(0, bear, ParseCost("2"), false) {
		t.Fatal("castable priced the offer against the RAW pool: a non-matching restricted batch counted toward a creature cast")
	}
	for _, o := range e.legalActions(0) {
		if o.Kind == "cast" && o.Obj == bear {
			t.Errorf("offer walk offered the Bear cast off non-matching restricted mana; the payment (payManaFor) would refuse it")
		}
	}
	// The deliberate over-bound, documented not accidental: the hypothetical
	// pool keeps the raw units, so the PROJECTION (never a payment) may still
	// carry the cast.
	projected := false
	for _, a := range e.PotentialActions(0) {
		if a.Kind == "cast" && a.Obj == bear {
			projected = true
		}
	}
	if !projected {
		t.Error("the potential walk's hyp pool must stay a raw over-bound: the projection should still carry the cast")
	}

	// Contrast: the matching class admits the batch at every gate.
	e2, bear2 := restrictedBearBoard(t, "Spell.Creature")
	if !pay.CostPayable(asPayer(e2), 0, bear2, false, ParseCost("2")) {
		t.Fatal("matching restricted batch must pay a creature cast")
	}
	if !e2.castable(0, bear2, ParseCost("2"), false) {
		t.Fatal("castable must admit mana whose restriction matches the payment")
	}
	found := false
	for _, o := range e2.legalActions(0) {
		if o.Kind == "cast" && o.Obj == bear2 {
			found = true
		}
	}
	if !found {
		t.Fatal("offer walk must offer the Bear cast off matching restricted mana")
	}
}

// TestPotentialActionsJitteThaliaSnapshotIsNotCastable pins the Jitte report's
// real defect on the projection: at the capture point the engine rightly
// refused the cast (Thalia raises the {2} Jitte to {3}, the pool is {C}{W}
// and BOTH lands are already tapped), so potential_actions carries no cast at
// all — the new client reads that and SKIPS the window the old client stopped
// for. The contrast half proves the RaiseCost is what the projection carries:
// without Thalia the same board makes the Jitte cast potential, because the
// pool CAN pay the printed {2}.
func TestPotentialActionsJitteThaliaSnapshotIsNotCastable(t *testing.T) {
	t.Parallel()
	e, jitte := jitteSnapshotBoard(t, true)
	acts := e.PotentialActions(0)
	for _, a := range acts {
		if a.Kind == "cast" {
			t.Errorf("Jitte snapshot: seat 0 potential actions carry a cast %q (obj %d); want none — Thalia raises Jitte to {3} and the pool is {C}{W}", a.Label, a.Obj)
		}
	}
	for _, a := range acts {
		if a.Kind == "activate" || a.Kind == "pass" || a.Kind == "concede" {
			t.Errorf("potential_actions must never carry kind %q", a.Kind)
		}
	}

	// Contrast: without Thalia, the same pool pays the printed {2}, so the
	// cast IS potential — the projection carries the engine's live cost
	// rules, not the printed number.
	e2, jitte2 := jitteSnapshotBoard(t, false)
	found := false
	for _, a := range e2.PotentialActions(0) {
		if a.Kind == "cast" && a.Obj == jitte2 {
			found = true
		}
	}
	if !found {
		t.Fatal("without Thalia the Jitte cast must be a potential action (pool {C}{W} pays {2})")
	}
	if jitte == 0 || jitte2 == 0 {
		t.Fatal("fixture lost the Jitte object")
	}
}

// TestPotentialActionsReduceCostOffered pins the Medallion side: a live
// ReduceCost static on the battlefield composes into the offer cost, so the
// {1}{W} spell is castable from one untapped Plains plus the floating pool —
// even though the floating pool alone (empty) offers no cast. The old client
// priced the printed cost and passed the window; the projection stops it.
func TestPotentialActionsReduceCostOffered(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	e.G.Step = state.StepMain1
	e.G.SetZone(state.ZHand, 0, nil)
	e.G.Players[0].LandsPlayed = 1
	onBoard(t, e, 0, jitteSnapshotPlains) // untapped: PotentialMana W:1
	onBoard(t, e, 0, "Name:Pearl Medallion\nManaCost:2\nTypes:Artifact\n"+
		"S:Mode$ ReduceCost | ValidCard$ Card.White | Type$ Spell | Activator$ You | Amount$ 1 | Description$ White spells you cast cost {1} less to cast.\n"+
		"Oracle:White spells you cast cost {1} less to cast.\n")
	spell := onHand(t, e, 0, "Name:Divine Verdict\nManaCost:1 W\nTypes:Instant\nOracle:W {1}\n")

	// The REAL offer (floating pool empty) carries no cast: the
	// float-then-cast model is why the projection exists.
	for _, o := range e.legalActions(0) {
		if o.Kind == "cast" && o.Obj == spell {
			t.Fatal("empty floating pool must not offer the cast directly")
		}
	}
	found := false
	for _, a := range e.PotentialActions(0) {
		if a.Kind == "cast" && a.Obj == spell {
			found = true
		}
	}
	if !found {
		t.Fatal("Pearl Medallion reduction: the {1}{W} spell must be a potential cast from one untapped Plains")
	}
}

// TestPotentialActionsEquipAbility pins the activate-effects half: an Equip
// {2} (a SorcerySpeed$ Attach ability) on an untapped permanent with two
// untapped Plains behind it is a potential ABILITY, though the floating pool
// is empty. The old client skipped every activation without {T} plus floated
// mana and read the window as empty.
func TestPotentialActionsEquipAbility(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	e.G.Step = state.StepMain1
	e.G.SetZone(state.ZHand, 0, nil)
	e.G.Players[0].LandsPlayed = 1
	onBoard(t, e, 0, jitteSnapshotPlains)
	onBoard(t, e, 0, jitteSnapshotPlains)
	onBoard(t, e, 0, "Name:Grizzly Bears\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	bf := onHand(t, e, 0, "Name:Bonehoard Dracosaur\nManaCost:0\nTypes:Artifact Equipment\nK:Equip:2\nOracle:Equip {2}\n")
	e.G.Obj(bf).Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), bf))
	found := false
	for _, a := range e.PotentialActions(0) {
		if a.Kind == "ability" && a.Obj == bf {
			found = true
		}
	}
	if !found {
		t.Fatal("Equip {2} with two untapped Plains must be a potential ability")
	}
}

// TestPotentialActionsCommandZoneCast pins the command-zone half: in a
// Commander game the commander sitting in the command zone is a potential
// cast once the seat's untapped sources cover its (taxed) cost, though the
// floating pool is empty. The old client scanned only the hand.
func TestPotentialActionsCommandZoneCast(t *testing.T) {
	t.Parallel()
	e, _ := commanderGame(t, commanderDamageSeed, FormatCommander, 0, [][]string{
		{"Name:Giada, Font of Hope\nManaCost:1 W\nTypes:Legendary Creature Angel\nPT:2/2\nOracle:x\n"},
		{"Name:Beatstick\nManaCost:0\nTypes:Creature Zombie\nPT:1/1\nOracle:x\n"},
	})
	e.G.Step = state.StepMain1
	e.G.SetZone(state.ZHand, 0, nil)
	e.G.Players[0].LandsPlayed = 1
	onBoard(t, e, 0, jitteSnapshotPlains)
	onBoard(t, e, 0, jitteSnapshotPlains)
	cmd := e.G.Zone(state.ZCommand, 0)[0]
	found := false
	for _, a := range e.PotentialActions(0) {
		if a.Kind == "cast" && a.Obj == cmd {
			found = true
		}
	}
	if !found {
		t.Fatal("Giada in the command zone with two untapped Plains must be a potential cast")
	}
}

// TestPotentialActionsFlashbackCast pins the graveyard half: a Lingering
// Souls in the graveyard is a potential cast for its flashback cost {1}{B}
// once the seat's untapped sources cover it. The old client scanned only the
// hand.
func TestPotentialActionsFlashbackCast(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	e.G.Step = state.StepMain1
	e.G.SetZone(state.ZHand, 0, nil)
	e.G.Players[0].LandsPlayed = 1
	onBoard(t, e, 0, "Name:Swamp\nTypes:Basic Land Swamp\nOracle:x\n")
	onBoard(t, e, 0, jitteSnapshotPlains)
	souls := onHand(t, e, 0, "Name:Lingering Souls\nManaCost:2 W\nTypes:Sorcery\nK:Flashback:1 B\n"+
		"Oracle:Create two 1/1 white Spirit creature tokens with flying.\n")
	o := e.G.Obj(souls)
	o.Zone = state.ZGraveyard
	e.G.SetZone(state.ZGraveyard, 0, append(e.G.Zone(state.ZGraveyard, 0), souls))
	e.G.SetZone(state.ZHand, 0, nil)
	found := false
	for _, a := range e.PotentialActions(0) {
		if a.Kind == "cast" && a.Obj == souls && a.Mode == "flashback" {
			found = true
		}
	}
	if !found {
		t.Fatal("Lingering Souls flashback {1}{B} from the graveyard must be a potential cast")
	}
}

// TestPotentialActionsIndeterminateSource pins the Tron half: three untapped
// Urza lands (Amount$ UrzaAmount — an indeterminate amount the static pricing
// resolves to zero) make the {7} creature a potential cast, though the
// floating pool is empty and the fixed-colour bound would see nothing.
func TestPotentialActionsIndeterminateSource(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	e.G.Step = state.StepMain1
	e.G.SetZone(state.ZHand, 0, nil)
	e.G.Players[0].LandsPlayed = 1
	for _, src := range []string{
		"Name:Urza's Mine\nTypes:Land Urza's Mine\nA:AB$ Mana | Cost$ T | Produced$ C | Amount$ UrzaAmount | SpellDescription$ Add {C}.\nOracle:x\n",
		"Name:Urza's Power-Plant\nTypes:Land Urza's Power-Plant\nA:AB$ Mana | Cost$ T | Produced$ C | Amount$ UrzaAmount | SpellDescription$ Add {C}.\nOracle:x\n",
		"Name:Urza's Tower\nTypes:Land Urza's Tower\nA:AB$ Mana | Cost$ T | Produced$ C | Amount$ UrzaAmount | SpellDescription$ Add {C}.\nOracle:x\n",
	} {
		onBoard(t, e, 0, src)
	}
	big := onHand(t, e, 0, "Name:Wandering Archon\nManaCost:7\nTypes:Creature Archon\nPT:1/1\nOracle:x\n")
	found := false
	for _, a := range e.PotentialActions(0) {
		if a.Kind == "cast" && a.Obj == big {
			found = true
		}
	}
	if !found {
		t.Fatal("three indeterminate Urza lands must make the {7} creature a potential cast")
	}
}

// TestPotentialActionsIndeterminateSourceIsUnbounded pins the upper boundary
// of the indeterminate-source contract. A source that actually counts 100
// creatures must keep a {100} spell visible; an arbitrary 99-mana sentinel
// would eat that action. The potential representation saturates every slot at
// math.MaxInt32, and the affordability total saturates too, so it cannot wrap
// negative while checking the generic cost.
func TestPotentialActionsIndeterminateSourceIsUnbounded(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	e.G.Step = state.StepMain1
	e.G.SetZone(state.ZHand, 0, nil)
	e.G.Players[0].LandsPlayed = 1
	onBoard(t, e, 0, "Name:Countland\nTypes:Land\n"+
		"A:AB$ Mana | Cost$ T | Produced$ G | Amount$ Count$Valid Creature.YouCtrl | SpellDescription$ Add G for each creature you control.\nOracle:x\n")
	for range 100 {
		onBoard(t, e, 0, "Name:Counter Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:1/1\nOracle:x\n")
	}
	big := onHand(t, e, 0, "Name:Century Beast\nManaCost:100\nTypes:Creature Beast\nPT:100/100\nOracle:x\n")
	if got := e.PotentialMana(0).Total(); got != potentialUnbounded {
		t.Fatalf("100-creature indeterminate potential total = %d, want saturated %d", got, potentialUnbounded)
	}
	for _, a := range e.PotentialActions(0) {
		if a.Kind == "cast" && a.Obj == big {
			return
		}
	}
	t.Fatal("a source that can make 100 mana must keep a {100} spell as a potential action")
}

// TestPotentialActionsBlazeAtXZero pins the X half: an X spell is a potential
// cast at X=0 from whatever its fixed pips need — CR 107.3b — the shape the
// old client's variable-pip refusal ate (the pin autopilot.test.ts used to
// carry). One untapped Mountain behind a floating {R} is enough.
func TestPotentialActionsBlazeAtXZero(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	e.G.Step = state.StepMain1
	e.G.SetZone(state.ZHand, 0, nil)
	e.G.Players[0].LandsPlayed = 1
	onBoard(t, e, 0, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:x\n")
	pool := e.G.Players[0].Pool
	pool[state.MR] = 1
	e.G.Players[0].Pool = pool
	blaze := onHand(t, e, 0, "Name:Blaze\nManaCost:X R\nTypes:Sorcery\nOracle:Blaze deals X damage to any target.\n")
	found := false
	for _, a := range e.PotentialActions(0) {
		if a.Kind == "cast" && a.Obj == blaze {
			found = true
		}
	}
	if !found {
		t.Fatal("Blaze must be a potential cast (X priced at 0, CR 107.3b)")
	}
}

// TestPotentialActionsRishadanPortRespondable pins the respondable half: an
// instant-speed activation needing floated mana (Rishadan Port's "11, T:
// tap target land", cost {1} + T) is a potential ABILITY with no floating
// mana — the fact an opponent-spell window reads to decide whether the seat
// could respond if it stopped. The old client ignored mana-only activations
// entirely, so the window passed.
func TestPotentialActionsRishadanPortRespondable(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	e.G.Step = state.StepMain1
	e.G.SetZone(state.ZHand, 0, nil)
	e.G.Players[0].LandsPlayed = 1
	port := onBoard(t, e, 0, "Name:Rishadan Port\nManaCost:3\nTypes:Land\n"+
		"A:AB$ Mana | Cost$ T | Produced$ 1 | SpellDescription$ Add {1}.\n"+
		"A:AB$ Tap | Cost$ 1 T | ValidTgts$ Land | TgtPrompt$ Select target land | SpellDescription$ Tap target land.\n"+
		"Oracle:{T}: Add {1}.\n{1}, {T}: Tap target land.\n")
	found := false
	for _, a := range e.PotentialActions(0) {
		if a.Kind == "ability" && a.Obj == port {
			found = true
		}
	}
	if !found {
		t.Fatal("Rishadan Port's tap-a-land activation must be a potential ability")
	}
	// The same shape with the Port TAPPED is not: the tap gate is real.
	e.G.Obj(port).Tapped = true
	for _, a := range e.PotentialActions(0) {
		if a.Kind == "ability" && a.Obj == port {
			t.Fatal("a tapped Port must not be a potential ability")
		}
	}
}

// TestPotentialActionsKarakasNoTargetDoesNotStop pins the Karakas half: an
// activation whose cost is a bare {T} and whose target does not exist is NOT
// a potential action (the engine's own target gate withholds the offer), so
// it must not make a window stop-worthy. The old client read every bare-T
// cost as payable and stopped every main phase.
func TestPotentialActionsKarakasNoTargetDoesNotStop(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	e.G.Step = state.StepMain1
	e.G.SetZone(state.ZHand, 0, nil)
	e.G.Players[0].LandsPlayed = 1
	onBoard(t, e, 0, "Name:Karakas\nManaCost:no cost\nTypes:Legendary Land\n"+
		"A:AB$ MoveZone | Cost$ T | ValidTgts$ Creature.Legendary | Origin$ Battlefield | DestinationZone$ Hand | SpellDescription$ Return target legendary creature to its owner's hand.\n"+
		"Oracle:{T}: Return target legendary creature to its owner's hand.\n")
	// No legendary creature anywhere (the decks are Mountains): nothing to
	// bounce. The battlefield walk must not have handed the seat a reason to
	// stop every main phase.
	if acts := e.PotentialActions(0); len(acts) != 0 {
		t.Fatalf("Karakas with no legal target must leave no potential actions, got %v", acts)
	}
	// With a legendary creature on the board the activation IS potential —
	// the gate is the target, not the bare-T cost.
	leg := onBoard(t, e, 0, "Name:Saffi Eriksdotter\nManaCost:G W\nTypes:Legendary Creature Human\nPT:2/2\nOracle:x\n")
	e.G.Obj(leg).SummonSick = false
	found := false
	for _, a := range e.PotentialActions(0) {
		if a.Kind == "ability" && a.Obj == e.G.Zone(state.ZBattlefield, 0)[0] {
			found = true
		}
	}
	if !found {
		t.Fatal("Karakas with a legendary creature on the battlefield must be a potential ability")
	}
}

// TestPotentialManaOverbound pins PotentialMana's folding rules directly:
// a fixed single-face source, a fixed multi-face source, an open (Any)
// production and an indeterminate amount.
func TestPotentialManaOverbound(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	e.G.SetZone(state.ZHand, 0, nil)
	onBoard(t, e, 0, jitteSnapshotPlains) // W:1
	if m := e.PotentialMana(0); m[state.MW] != 1 || m.Total() != 1 {
		t.Fatalf("one Plains potential = %v, want exactly {W:1}", m)
	}

	e2 := layerEngine(t)
	e2.G.SetZone(state.ZHand, 0, nil)
	onBoard(t, e2, 0, "Name:Volcanic Island\nTypes:Land Island Mountain\nOracle:x\n") // U or R: both folded
	onBoard(t, e2, 0, "Name:Falling Timbaland\nTypes:Land\nA:AB$ Mana | Cost$ T | Produced$ Any | SpellDescription$ Add one mana of any colour.\nOracle:x\n")
	onBoard(t, e2, 0, "Name:Cradle Stand-in\nTypes:Land\nA:AB$ Mana | Cost$ T | Produced$ G | Amount$ Count$Valid Creature.YouCtrl | SpellDescription$ Add G per creature.\nOracle:x\n")
	m := e2.PotentialMana(0)
	for _, i := range []int{state.MW, state.MU, state.MB, state.MR, state.MG, state.MC} {
		if m[i] < potentialUnbounded {
			t.Errorf("potential slot %d = %d, want >= %d (an Any source and an indeterminate amount price unbounded)", i, m[i], potentialUnbounded)
		}
	}
}

// TestPotentialActionsIsAPureRead pins the projection's side-effect contract:
// it emits no event, mutates no pool, and leaves the real offer walk (and
// therefore the engine's pending decision surface) exactly as it found it.
func TestPotentialActionsIsAPureRead(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	e.G.Step = state.StepMain1
	e.G.SetZone(state.ZHand, 0, nil)
	e.G.Players[0].LandsPlayed = 1
	before := len(e.L.Events)
	poolBefore := e.G.Players[0].Pool
	_ = e.PotentialActions(0)
	if len(e.L.Events) != before {
		t.Fatalf("PotentialActions emitted %d events", len(e.L.Events)-before)
	}
	if e.G.Players[0].Pool != poolBefore {
		t.Fatalf("PotentialActions mutated the pool: %v -> %v", poolBefore, e.G.Players[0].Pool)
	}
	// The real (floating-pool) walk is unchanged by the refactor: with the
	// hand cleared the only options are the mana taps and pass/concede.
	for _, o := range e.legalActions(0) {
		if o.Kind == "cast" || o.Kind == "ability" || o.Kind == "play_land" {
			t.Fatalf("an empty hand must not offer %q directly", o.Kind)
		}
	}
}

// TestPotentialManaPaidManaAbilitySequenced pins the paid-mana-ability
// ordering half of the second-round finding: the potential pool must admit a
// source whose mana ability carries a PAID activation cost even though the
// real floating pool does not already cover it. A Plains plus an untapped
// "{1}, {T}: add {C}{C}" source must produce the {2} the {C}{C} pays for,
// because the seat taps the Plains for {W}, pays the {1} to activate the
// source, and casts from what it produced. The old gate priced the paid
// source against the REAL pool (empty) and dropped it, so only the {W}
// projected and the {2} spell was silently uncastable.
func TestPotentialManaPaidManaAbilitySequenced(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	e.G.SetZone(state.ZHand, 0, nil)
	onBoard(t, e, 0, jitteSnapshotPlains) // untapped free {W}
	onBoard(t, e, 0, "Name:PaidCave\nTypes:Land\n"+
		"A:AB$ Mana | Cost$ 1 T | Produced$ C | Amount$ 2 | SpellDescription$ {1}, {T}: Add {C}{C}.\n"+
		"Oracle:{1}, {T}: Add {C}{C}.\n")
	m := e.PotentialMana(0)
	if m.Total() < 3 || m[state.MC] < 2 {
		t.Fatalf("Plains + paid {1},T->{C}{C} source potential = %v, want >= {C:2} plus the Plains' {W}", m)
	}
	// Contrast: the paid source ALONE cannot pay its own activation, so it
	// contributes nothing -- the fix is order-accounted, not a blank
	// over-bound with no source to pay the cost.
	e2 := layerEngine(t)
	e2.G.SetZone(state.ZHand, 0, nil)
	onBoard(t, e2, 0, "Name:PaidCave\nTypes:Land\n"+
		"A:AB$ Mana | Cost$ 1 T | Produced$ C | Amount$ 2 | SpellDescription$ {1}, {T}: Add {C}{C}.\n"+
		"Oracle:{1}, {T}: Add {C}{C}.\n")
	if m2 := e2.PotentialMana(0); m2.Total() != 0 {
		t.Fatalf("a paid source with nothing to pay {1} must contribute nothing, got %v", m2)
	}
}

// TestPotentialActionsPaidManaAbilitySequenced is the action-level pin of the
// same defect: with the Plains and the paid source behind it, the {2} creature
// in hand IS a potential cast (the projection prices against the sequenced
// pool), though the floating pool is empty. This is the exact shape the second
// round's break attempt reproduced (pool=[C:1], {2} omitted).
func TestPotentialActionsPaidManaAbilitySequenced(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	e.G.Step = state.StepMain1
	e.G.SetZone(state.ZHand, 0, nil)
	e.G.Players[0].LandsPlayed = 1
	onBoard(t, e, 0, jitteSnapshotPlains) // untapped free {W}
	onBoard(t, e, 0, "Name:PaidCave\nTypes:Land\n"+
		"A:AB$ Mana | Cost$ 1 T | Produced$ C | Amount$ 2 | SpellDescription$ {1}, {T}: Add {C}{C}.\n"+
		"Oracle:{1}, {T}: Add {C}{C}.\n")
	creature := onHand(t, e, 0, "Name:Colourless Bear\nManaCost:2\nTypes:Creature Bear\nPT:1/1\nOracle:x\n")

	// The REAL offer (empty floating pool) rightly has no cast; the projection
	// exists precisely to catch the float-then-cast sequence.
	for _, o := range e.legalActions(0) {
		if o.Kind == "cast" && o.Obj == creature {
			t.Fatal("empty floating pool must not offer the cast directly")
		}
	}
	found := false
	for _, a := range e.PotentialActions(0) {
		if a.Kind == "cast" && a.Obj == creature {
			found = true
		}
	}
	if !found {
		t.Fatal("Plains + paid {1},T->{C}{C} source must make the {2} creature a potential cast")
	}
}

// TestPotentialManaBlankProducedIsColourless pins the blank-Produced$ half of
// the second-round finding: a mana ability with NO Produced$ is the executor's
// own colourless default (one {C}), NOT an alternative production. The old
// addPotentialMana tested producedOpen("") BEFORE the blank->"C" fallback, so
// producedOpen("") returned true and the source granted potentialUnbounded in
// every colour slot -- turning an unpayable {W} spell into a potential cast.
func TestPotentialManaBlankProducedIsColourless(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	e.G.SetZone(state.ZHand, 0, nil)
	onBoard(t, e, 0, "Name:BlankManaLand\nTypes:Land\n"+
		"A:AB$ Mana | Cost$ T | SpellDescription$ Add {C}.\nOracle:x\n")
	m := e.PotentialMana(0)
	if m[state.MC] != 1 {
		t.Fatalf("blank Produced$ must be one colourless, got C=%d (pool %v)", m[state.MC], m)
	}
	for _, i := range []int{state.MW, state.MU, state.MB, state.MR, state.MG} {
		if m[i] != 0 {
			t.Fatalf("a colourless source must not contribute colour slot %d, got %d (pool %v)", i, m[i], m)
		}
	}
}

// TestPotentialActionsBlankProducedNotColoured pins the action-level
// consequence: a colourless-only source must NOT make a {W} spell a potential
// cast. This is the precise failure the finding's blank-production break
// attempt hit (PotentialMana was [99 99 99 99 99 99] and projected the white
// cast).
func TestPotentialActionsBlankProducedNotColoured(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	e.G.Step = state.StepMain1
	e.G.SetZone(state.ZHand, 0, nil)
	e.G.Players[0].LandsPlayed = 1
	onBoard(t, e, 0, "Name:BlankManaLand\nTypes:Land\n"+
		"A:AB$ Mana | Cost$ T | SpellDescription$ Add {C}.\nOracle:x\n")
	white := onHand(t, e, 0, "Name:White Bear\nManaCost:W\nTypes:Creature Bear\nPT:1/1\nOracle:x\n")
	for _, a := range e.PotentialActions(0) {
		if a.Kind == "cast" && a.Obj == white {
			t.Fatal("a colourless source must not make a {W} spell a potential cast")
		}
	}
	// The same source plus a colourless {1} spell IS castable -- one {C} pays
	// {1} generic, the shape the old unbounded blank wrongly generalised.
	if m := e.PotentialMana(0); m[state.MC] != 1 {
		t.Fatalf("blank source potential must be {C:1}, got %v", m)
	}
}

// TestPotentialActionsJitteThaliaCorpusScriptIsNotCastable is the
// real-script form of the substitute proof the controller's 2026-09-17
// ruling accepts for the (waived, evidence-gone) cmd/repro replay: the REAL
// corpus Thalia, Guardian of Thraben and Umezawa's Jitte, the capture point's
// board (both lands tapped, pool {C}{W}, land drop spent, main1). The engine
// must project no cast — Thalia's RaiseCost raises the printed {2} Jitte to
// {3} — and without Thalia the same board must project the cast, because the
// pool {C}{W} pays the printed {2}. The projection is what the auto-pass
// stop decision reads, so the window the Jitte report described is skipped.
func TestPotentialActionsJitteThaliaCorpusScriptIsNotCastable(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	jitte, ok := reg.Lookup("Umezawa's Jitte")
	if !ok {
		t.Fatal("corpus fixture: Umezawa's Jitte missing")
	}
	thalia, ok := reg.Lookup("Thalia, Guardian of Thraben")
	if !ok {
		t.Fatal("corpus fixture: Thalia, Guardian of Thraben missing")
	}
	build := func(withThalia bool) (*Engine, state.ObjID) {
		t.Helper()
		e := New(Config{Seed: 42, Names: []string{"a", "b"},
			Decks: [][]*cards.Card{append([]*cards.Card{jitte}, mountainDeck(t, 39)...), mountainDeck(t, 40)}})
		e.Advance()
		toMain1(t, e)
		jitteID := moveByName(t, e, 0, "Umezawa's Jitte", state.ZHand)
		if withThalia {
			onBoardCard(t, e, 0, thalia)
		}
		// The capture point: the reporter tapped BOTH lands for {C}{W} and
		// spent the land drop, so nothing untapped remains and no land drop
		// can mask the cast answer.
		e.G.Players[0].LandsPlayed = 1
		for _, id := range e.G.Zone(state.ZBattlefield, 0) {
			o := e.G.Obj(id)
			if o.Face() != nil && o.Face().IsLand() {
				o.Tapped = true
			}
		}
		pool := e.G.Players[0].Pool
		pool[state.MC] = 1
		pool[state.MW] = 1
		e.G.Players[0].Pool = pool
		return e, jitteID
	}

	e, _ := build(true)
	for _, a := range e.PotentialActions(0) {
		if a.Kind == "cast" {
			t.Errorf("real-script Jitte board with Thalia: potential actions carry cast %q; want none — RaiseCost raises {2} to {3} over pool {C}{W}", a.Label)
		}
	}
	// The engine's own REAL offer agrees (the projection mirrors it): no cast
	// is offered now either.
	for _, o := range e.legalActions(0) {
		if o.Kind == "cast" {
			t.Errorf("real-script Jitte board with Thalia: the offer walk itself carries cast %q", o.Label)
		}
	}

	e2, jitte2 := build(false)
	found := false
	for _, a := range e2.PotentialActions(0) {
		if a.Kind == "cast" && a.Obj == jitte2 {
			found = true
		}
	}
	if !found {
		t.Fatal("real-script Jitte board without Thalia: the cast must be a potential action (pool {C}{W} pays {2})")
	}
}

// TestPotentialActionsMountDoomDamageAbility pins the engine half of
// fb-20260923T033148Z-877b8f8f ("mount doom -- can only play tap for mana").
// Measured: with Mount Doom untapped beside enough other mana, the empty-pool
// priority window offers Mount Doom ONLY its mana activation (the
// float-then-cast model, pinned by TestLanderTokenAbilityOfferedOnlyWhenPool
// Funded), while the potential-action projection carries its
// "{1}{B}{R}, {T}: deal 1 damage" ability. The client's card affordance reads
// that projection so the player is not left with a one-click tap that spends
// the {T} the damage ability needs; this test holds the engine contract the
// client relies on.
func TestPotentialActionsMountDoomDamageAbility(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	e.G.Step = state.StepMain1
	e.G.SetZone(state.ZHand, 0, nil)
	e.G.Players[0].LandsPlayed = 1
	doom := onBoard(t, e, 0, "Name:Mount Doom\nTypes:Legendary Land\n"+
		"A:AB$ Mana | Cost$ T PayLife<1> | Produced$ Combo B R | SpellDescription$ Add {B} or {R}.\n"+
		"A:AB$ DealDamage | Cost$ 1 B R T | Defined$ Opponent | NumDmg$ 1 | SpellDescription$ CARDNAME deals 1 damage to each opponent.\n"+
		"Oracle:x\n")
	onBoard(t, e, 0, "Name:Badlands\nTypes:Land Swamp Mountain\n"+
		"A:AB$ Mana | Cost$ T | Produced$ B | SpellDescription$ Add {B}.\n"+
		"A:AB$ Mana | Cost$ T | Produced$ R | SpellDescription$ Add {R}.\nOracle:x\n")
	onBoard(t, e, 0, jitteSnapshotPlains)
	onBoard(t, e, 0, "Name:Swamp\nTypes:Basic Land Swamp\nA:AB$ Mana | Cost$ T | Produced$ B | SpellDescription$ Add {B}.\nOracle:x\n")
	onBoard(t, e, 0, "Name:Mountain\nTypes:Basic Land Mountain\nA:AB$ Mana | Cost$ T | Produced$ R | SpellDescription$ Add {R}.\nOracle:x\n")
	if o := e.G.Obj(doom); o == nil || o.Tapped || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Mount Doom must be an untapped permanent: %+v", o)
	}
	if e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("precondition: empty floating pool, got %v", e.G.Players[0].Pool)
	}
	var live []string
	for _, o := range e.legalActions(0) {
		if o.Obj == doom {
			live = append(live, o.Kind)
		}
	}
	if len(live) != 1 || live[0] != "activate" {
		t.Fatalf("empty-pool offers on Mount Doom = %v, want only its mana activation", live)
	}
	found := false
	for _, a := range e.PotentialActions(0) {
		if a.Kind == "ability" && a.Obj == doom && a.Ability == 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("Mount Doom's {1}{B}{R},{T} damage ability missing from the potential actions: %+v", e.PotentialActions(0))
	}
}
