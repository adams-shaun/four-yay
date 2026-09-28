package rules

// The TargetMax$-aware MaxTotalTargetPower$ prune (task
// cli-20260922T225142Z-0ab0cb60, closing the AGENTS.md "Known approximations"
// row "(maxpower1)").
//
// totalPowerCappedCandidates' negative-offset argument originally drew on
// EVERY negative candidate in the census, i.e. it assumed a selection could
// take unlimited negatives. CR 601.2c caps a selection at TargetMax$, so a
// selection containing an over-cap candidate can take at most TargetMax$-1
// other things and can therefore claw back only the most negative
// TargetMax$-1 of them. With the default TargetMax$ 1 there is no offset at
// all.
//
// askCrossModeCharmTargets (the cross-mode TargetUnique$ Charm ask) built its
// census from legalTargetCandidates and never consulted the cap helper at
// all: it neither pruned nor attached the running budget. Neither corpus
// MaxTotalTargetPower$ carrier (Reunion of the House, Nethroi) is a Charm, so
// both pins below are synthetic SAs of the named shape.
//
// Two authored CDA fixtures (scourge_of_the_skyclaves.txt's own
// characteristic-defining "20 minus the highest life total" shape, which
// applies in EVERY zone per CR 208.2) give the census two distinct
// -1-power candidates at a 21-life opponent. Two is the minimum that tells
// the bounded offset from the unbounded one: with a single negative and a
// single offset slot the old and new sums agree.

import (
	"strconv"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// negOffsetScript authors a CDA creature whose derived power is 20 minus the
// highest life total among players -- -1 with an opponent at 21 -- in every
// zone, so it is a negative-power TARGET CANDIDATE in the graveyard. name
// keeps two instances distinct (the deck helper keys by name).
func negOffsetScript(name string) string {
	return "Name:" + name + "\nManaCost:1 B\nTypes:Creature Demon\nPT:*/*\n" +
		"S:Mode$ Continuous | CharacteristicDefining$ True | SetPower$ X | SetToughness$ X | Description$ CARDNAME's power and toughness are each equal to 20 minus the highest life total among players.\n" +
		"SVar:Y:PlayerCountPlayers$HighestLifeTotal\n" +
		"SVar:X:SVar$Y/NMinus.20\n" +
		"Oracle:x\n"
}

// maxPowerAbilityFixture returns graveyard creatures to hand under a literal
// MaxTotalTargetPower$ cap and a literal TargetMax$ (the row's own "TargetMax$
// bounding how many negatives one selection may take"). Hand, not
// battlefield: the -1/-1 CDA fixtures would die to the toughness state-based
// action the instant they entered (CR 704.5f), hiding the resolution.
func maxPowerAbilityFixture(capPower, maxTargets int) string {
	return "Name:PowerReclaimerBounded\nManaCost:2\nTypes:Creature\n" +
		"A:AB$ ChangeZone | Cost$ 0 | Origin$ Graveyard | Destination$ Hand | " +
		"TargetMin$ 0 | TargetMax$ " + strconv.Itoa(maxTargets) + " | ValidTgts$ Creature.YouOwn | " +
		"MaxTotalTargetPower$ " + strconv.Itoa(capPower) + "\n" +
		"Oracle:x\n"
}

// moveNamedToGrave finds the named card in seat 0's hand or library and
// bridges it to the graveyard with a logged move (the newFixtureDeck
// convention). It fails loudly if the card is absent -- the precondition the
// prune assertions depend on.
func moveNamedToGrave(t *testing.T, e *Engine, name string) state.ObjID {
	t.Helper()
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, 0) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: state.ZGraveyard})
				return id
			}
		}
	}
	t.Fatalf("card %q absent from seat 0's hand/library (precondition)", name)
	return 0
}

// TestTotalPowerCapOffsetRespectsTargetMax is the pin for the TargetMax$-
// bounded offset: with TargetMax$ 2 and a cap of 0, the 2-power Bear can take
// only ONE other target, and the best such target is a single -1, so 2-1 = 1
// busts every selection containing it and it must be pruned. The pre-fix
// offset summed BOTH -1s (2-2 = 0) and wrongly kept it. The two negatives
// themselves stay offered (each can pair with the other), and the real bot's
// answer must Validate.
func TestTotalPowerCapOffsetRespectsTargetMax(t *testing.T) {
	t.Parallel()
	const victim = "BigTwo"
	victimScript := "Name:" + victim + "\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	e, cfg, src := newFixtureDeck(t, 9701, maxPowerAbilityFixture(0, 2),
		negOffsetScript("NegA"), negOffsetScript("NegB"), victimScript)
	toMain1(t, e)
	// Precondition: the two CDA fixtures are on the board reading -1 (the
	// value the offset arithmetic depends on), which needs the opponent at 21
	// life -- set BEFORE the power is read.
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: 21 - e.G.Players[1].Life})
	negA := moveNamedToGrave(t, e, "NegA")
	negB := moveNamedToGrave(t, e, "NegB")
	bear := moveNamedToGrave(t, e, victim)
	if o := e.G.Obj(src); o == nil || o.Zone != state.ZBattlefield {
		e.emit(events.Event{Kind: events.MoveZone, Obj: src, From: o.Zone, To: state.ZBattlefield})
	}
	e.pending = nil
	e.priorityRound()
	for id, want := range map[state.ObjID]int32{negA: -1, negB: -1, bear: 2} {
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZGraveyard || e.Power(id) != want {
			t.Fatalf("%s zone %v power %d, want graveyard at %d (precondition)",
				o.Face().Name, o.Zone, e.Power(id), want)
		}
	}

	submitChoices(t, e, abilityOption(t, e, src, 0).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("pending = %+v, want the ability-path KTarget ask", d)
	}
	if !d.HasBudget() || d.MaxSum != 0 {
		t.Fatalf("MaxSum = %d Budgeted = %v, want a present budget of 0 (precondition)", d.MaxSum, d.Budgeted)
	}
	offerSet := map[state.ObjID]decision.Option{}
	for _, o := range d.Options {
		offerSet[o.Obj] = o
	}
	if _, ok := offerSet[bear]; ok {
		t.Fatalf("the 2-power Bear was offered under TargetMax$ 2 / cap 0 though its only offset is a single -1: options=%+v", d.Options)
	}
	for _, id := range []state.ObjID{negA, negB} {
		if _, ok := offerSet[id]; !ok {
			t.Fatalf("the -1-power %s was pruned though it can pair with the other -1 under the cap: options=%+v",
				e.G.Obj(id).Face().Name, d.Options)
		}
	}
	if in := newTestBot(1).answer(e, d); d.Validate(in) != nil {
		t.Fatalf("bot answer %v failed Validate: %v", in.Choices, d.Validate(in))
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player,
		Choices: []int{offerSet[negA].Index, offerSet[negB].Index}}); err != nil {
		t.Fatalf("the compensated -1 + (-1) answer was rejected: %v", err)
	}
	passUntilStackEmpty(t, e, 20)
	for _, id := range []state.ObjID{negA, negB} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZHand {
			t.Fatalf("%s = %+v, want in hand (the ability returned it)", e.G.Obj(id).Face().Name, o)
		}
	}
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("the Bear = %+v, want still in the graveyard", o)
	}
	replayCheck(t, e, cfg)
}

// TestTotalPowerCapOffsetUnboundedWhenTargetMaxIsEveryCandidate is the
// companion precondition pin: the two corpus carriers' TargetMax$ is X (every
// candidate), where TargetMax$-1 exceeds the negative count and the offset is
// the whole negative sum -- the pre-fix behaviour, which must be preserved.
// Under TargetMax$ X and a cap of 0 the 2-power Bear sits beside two -1s
// (2-1-1 = 0) and MUST be offered; a fix that bounded the offset too tightly
// would wrongly prune it.
func TestTotalPowerCapOffsetUnboundedWhenTargetMaxIsEveryCandidate(t *testing.T) {
	t.Parallel()
	const victim = "BigTwo"
	victimScript := "Name:" + victim + "\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	e, cfg, src := newFixtureDeck(t, 9702,
		maxPowerAbilityFixtureWhereTargetMaxIsX(0),
		negOffsetScript("NegA"), negOffsetScript("NegB"), victimScript)
	toMain1(t, e)
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: 21 - e.G.Players[1].Life})
	negA := moveNamedToGrave(t, e, "NegA")
	negB := moveNamedToGrave(t, e, "NegB")
	bear := moveNamedToGrave(t, e, victim)
	if o := e.G.Obj(src); o == nil || o.Zone != state.ZBattlefield {
		e.emit(events.Event{Kind: events.MoveZone, Obj: src, From: o.Zone, To: state.ZBattlefield})
	}
	e.pending = nil
	e.priorityRound()
	if e.Power(negA) != -1 || e.Power(negB) != -1 || e.Power(bear) != 2 {
		t.Fatalf("powers = %d/%d/%d, want -1/-1/2 (precondition)",
			e.Power(negA), e.Power(negB), e.Power(bear))
	}
	submitChoices(t, e, abilityOption(t, e, src, 0).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("pending = %+v, want the ability-path KTarget ask", d)
	}
	offerSet := map[state.ObjID]decision.Option{}
	for _, o := range d.Options {
		offerSet[o.Obj] = o
	}
	if _, ok := offerSet[bear]; !ok {
		t.Fatalf("the 2-power Bear was pruned though TargetMax$ X lets it take both -1s (2-1-1 = 0): options=%+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player,
		Choices: []int{offerSet[bear].Index, offerSet[negA].Index, offerSet[negB].Index}}); err != nil {
		t.Fatalf("the compensated 2 + (-1) + (-1) answer was rejected: %v", err)
	}
	passUntilStackEmpty(t, e, 20)
	replayCheck(t, e, cfg)
}

// maxPowerAbilityFixtureWhereTargetMaxIsX is the corpus carriers' own shape:
// TargetMax$ X resolved by an SVar to the candidate count, so every candidate
// is selectable.
func maxPowerAbilityFixtureWhereTargetMaxIsX(capPower int) string {
	return "Name:PowerReclaimerBounded\nManaCost:2\nTypes:Creature\n" +
		"A:AB$ ChangeZone | Cost$ 0 | Origin$ Graveyard | Destination$ Hand | " +
		"TargetMin$ 0 | TargetMax$ X | ValidTgts$ Creature.YouOwn | " +
		"MaxTotalTargetPower$ " + strconv.Itoa(capPower) + "\n" +
		"SVar:X:Count$ValidGraveyard Creature.YouOwn\n" +
		"Oracle:x\n"
}

// charmMaxPowerScript is a synthetic cross-mode TargetUnique$ Charm (the
// family effect.CharmCrossModeShape classifies CharmUniqueSupported) whose
// first target-bearing mode carries MaxTotalTargetPower$ 10. No corpus Charm
// carries the parameter, so the ask's cap read is only reachable here.
func charmMaxPowerScript() string {
	return "Name:MaxCharm\nManaCost:B\nTypes:Instant\n" +
		"A:SP$ Charm | CharmNum$ 2 | Choices$ MA,MB\n" +
		"SVar:MA:DB$ LoseLife | ValidTgts$ Player | TargetUnique$ True | MaxTotalTargetPower$ 10 | LifeAmount$ 2 | SpellDescription$ Target player loses 2 life.\n" +
		"SVar:MB:DB$ Draw | ValidTgts$ Player | TargetUnique$ True | MaxTotalTargetPower$ 10 | NumCards$ 1 | SpellDescription$ Target player draws a card.\n" +
		"Oracle:x\n"
}

// TestCrossModeCharmAskReadsThePowerCap pins the second half of the row:
// askCrossModeCharmTargets ran legalTargetCandidates straight into the
// decision, reading no MaxTotalTargetPower$ at all, so a cap a caller put on
// its first target-bearing mode never rode the wire. With the shared cap read
// the combined ask carries the present budget (MaxSum 10, Budgeted) exactly
// as askTarget and cast.go's targetAsk do; before the fix both were zero.
func TestCrossModeCharmAskReadsThePowerCap(t *testing.T) {
	t.Parallel()
	e, cfg, id := newFixtureDeck(t, 9703, charmMaxPowerScript())
	addMana(t, e, 0, "B")
	d := castFixture(t, e, id, -1)
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("pending = %+v, want the cast modes ask (precondition)", d)
	}
	submitChoices(t, e, 0, 1) // both target-bearing modes
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget || d.Min != 2 || d.Max != 2 {
		t.Fatalf("pending = %+v, want the combined Min==Max==2 cross-mode KTarget ask", d)
	}
	if !d.HasBudget() || d.MaxSum != 10 {
		t.Fatalf("combined charm ask MaxSum = %d Budgeted = %v, want a present budget of 10 (the first mode's MaxTotalTargetPower$)",
			d.MaxSum, d.Budgeted)
	}
	// The players' Value is 0 and the cap never prunes a player, so a
	// different-player answer still validates and resolves.
	if in := newTestBot(1).answer(e, d); d.Validate(in) != nil {
		t.Fatalf("bot answer %v failed Validate: %v", in.Choices, d.Validate(in))
	}
	submitChoices(t, e, d.Options[0].Index, d.Options[len(d.Options)-1].Index)
	passUntilStackEmpty(t, e, 20)
	replayCheck(t, e, cfg)
}
