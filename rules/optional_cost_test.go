// optional_cost_test.go — stat:OptionalCost (the optional self additional
// cost, `S:Mode$ OptionalCost | EffectZone$ All | ValidCard$ Card.Self |
// ValidSA$ Spell`) pinned end to end on REAL corpus carriers:
//
//   - Burning Curiosity's `Blight<1>`: the paid offer exists beside the
//     plain cast, paying it stamps the pay-time CastInfo's optionalcostpaid
//     flag, and the spell's own `SVar:X:Count$OptionalGenericCostPaid.3.2`
//     makes its Dig exile 3 rather than 2; the plain/declined cast exiles 2
//     and stamps nothing.
//   - Voltage Surge's `Sac<1/Artifact>`: a NON-Blight optional part is paid
//     through the ordinary staged cost machinery (the shared sacAsk), and
//     the spell's `SVar:X:Count$OptionalGenericCostPaid.4.2` deals 4 rather
//     than 2 — proving no bespoke optional-cost payment path exists.
//   - Graven Archfiend's ETB gate `CastSA>Count$OptionalGenericCostPaid.1.0`
//     (the count head's CastSA> provenance route): the trigger fires only
//     when the cast paid.
//
// The decks are built from compiled corpus cards plus freely authored
// fixtures (never committed Forge text). None of the three cards is in a
// legacy golden deck, so no chain head depends on this work.

package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// optionalCostCastInfo reports whether the log carries a pay-time CastInfo
// for obj whose flag list names optionalcostpaid.
func optionalCostCastInfo(e *Engine, obj state.ObjID) bool {
	for _, ev := range e.L.Events {
		if ev.Kind != events.CastInfo || ev.Obj != obj {
			continue
		}
		for _, part := range splitCSV(ev.Counter) {
			if part == "optionalcostpaid" {
				return true
			}
		}
	}
	return false
}

// optionalCostNotes returns the Note events naming text.
func optionalCostNotes(e *Engine, substr string) []events.Event {
	var out []events.Event
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, substr) {
			out = append(out, ev)
		}
	}
	return out
}

// optionalCostCountExile asserts the top n cards of seat 0's library were
// exiled by the resolving Dig: seat 0's exile holds exactly n cards and the
// spell's Remembered list (carried on the object) names n objects.
func optionalCostCountExile(t *testing.T, e *Engine, spell state.ObjID, n int) {
	t.Helper()
	if got := len(e.G.Zone(state.ZExile, 0)); got != n {
		t.Fatalf("seat 0 exile holds %d cards, want %d (the Dig's X branch)", got, n)
	}
	if o := e.G.Obj(spell); o != nil && len(o.Remembered) != 0 && len(o.Remembered) != n {
		t.Fatalf("Dig remembered %d objects, want %d", len(o.Remembered), n)
	}
}

// choosePendingObject answers the pending KChoose by the option naming id.
func choosePendingObject(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("no KChoose pending: %+v", d)
	}
	for _, o := range d.Options {
		if o.Obj == id {
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("no option for object %d in %+v", id, d.Options)
}

// TestOptionalCostBurningCuriosityPaidBlightExilesThree pins the PAID path:
// the optional-cost offer exists beside the plain cast, paying the Blight<1>
// part puts the -1/-1 counter on the chosen creature through the ordinary
// blight stage, the pay-time CastInfo stamps optionalcostpaid, and the Dig's
// SVar:X (Count$OptionalGenericCostPaid.3.2) resolves to 3.
func TestOptionalCostBurningCuriosityPaidBlightExilesThree(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 610, []string{"Burning Curiosity"},
		[]string{altBearSrc, altBearSrc}, []string{altBearSrc})
	spell := findCardObj(t, e, 0, "Burning Curiosity", state.ZHand)
	mine1 := findCardObj(t, e, 0, "Bear", state.ZBattlefield)
	mine2 := findCardObj(t, e, 0, "Bear", state.ZBattlefield)
	// PRECONDITION: the corpus card really carries the OptionalCost static —
	// without it every offer assertion below is vacuous.
	if len(e.optionalCostViews(e.collectCostStatics(), 0, spell)) != 1 {
		t.Fatalf("precondition: Burning Curiosity carries no applicable OptionalCost static")
	}
	addMana(t, e, 0, "CCRR")
	// The plain cast must remain available beside the paid option.
	castModeOption(t, e, spell, "")
	paid := castModeOption(t, e, spell, "optionalcost")
	submitChoices(t, e, paid)
	// The ordinary Blight stage asks which creature to blight.
	choosePendingObject(t, e, mine2)
	passUntilStackEmpty(t, e, 40)

	if got := e.G.Obj(mine2).Counter("M1M1"); got != 1 {
		t.Fatalf("blight counters on chosen creature = %d, want 1", got)
	}
	if got := e.G.Obj(mine1).Counter("M1M1"); got != 0 {
		t.Fatalf("the unchosen creature was blighted too (%d counters)", got)
	}
	if !optionalCostCastInfo(e, spell) {
		t.Fatal("no pay-time CastInfo carrying optionalcostpaid")
	}
	optionalCostCountExile(t, e, spell, 3)
	replayCheck(t, e, cfg)
}

// TestOptionalCostBurningCuriosityPlainExilesTwo pins the decline path: the
// plain cast is unaffected — no Blight ask, no optionalcostpaid provenance —
// and the Dig's unpaid branch exiles 2.
func TestOptionalCostBurningCuriosityPlainExilesTwo(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 611, []string{"Burning Curiosity"},
		[]string{altBearSrc, altBearSrc}, []string{altBearSrc})
	spell := findCardObj(t, e, 0, "Burning Curiosity", state.ZHand)
	mine1 := findCardObj(t, e, 0, "Bear", state.ZBattlefield)
	mine2 := findCardObj(t, e, 0, "Bear", state.ZBattlefield)
	addMana(t, e, 0, "CCRR")
	// The paid offer must exist for this to be a genuine DECLINE rather than
	// a feature that was never offered (the fix-reverted proof depends on
	// this assertion failing).
	castModeOption(t, e, spell, "optionalcost")
	plain := castModeOption(t, e, spell, "")
	submitChoices(t, e, plain)
	passUntilStackEmpty(t, e, 40)

	if got := e.G.Obj(mine1).Counter("M1M1") + e.G.Obj(mine2).Counter("M1M1"); got != 0 {
		t.Fatalf("plain cast blighted %d counters, want 0", got)
	}
	if optionalCostCastInfo(e, spell) {
		t.Fatal("plain cast stamped optionalcostpaid provenance")
	}
	optionalCostCountExile(t, e, spell, 2)
	replayCheck(t, e, cfg)
}

// TestOptionalCostVoltageSurgeSacrificeDealsFour pins a NON-Blight optional
// part through the ORDINARY payment path: Voltage Surge's Sac<1/Artifact>
// is settled by the shared sacrifice stage, and the spell's
// Count$OptionalGenericCostPaid.4.2 deals 4 to the target rather than 2 —
// the count head read through the real resolution, not a unit stub.
func TestOptionalCostVoltageSurgeSacrificeDealsFour(t *testing.T) {
	t.Parallel()
	giant := "Name:Giant\nManaCost:4 G\nTypes:Creature Giant\nPT:5/9\nOracle:x\n"
	e, cfg, _ := altCostEngine(t, 612, []string{"Voltage Surge"},
		[]string{altArtifactSrc}, []string{giant})
	spell := findCardObj(t, e, 0, "Voltage Surge", state.ZHand)
	relic := findCardObj(t, e, 0, "Test Relic", state.ZBattlefield)
	g := findCardObj(t, e, 1, "Giant", state.ZBattlefield)
	addMana(t, e, 0, "R")
	paid := castModeOption(t, e, spell, "optionalcost")
	submitChoices(t, e, paid)
	// The ordinary sacrifice stage asks for the artifact.
	choosePendingObject(t, e, relic)
	// CR 601.2c: the target ask for the damage.
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("no target ask pending: %+v", d)
	}
	pick := -1
	for _, o := range d.Options {
		if o.Obj == g {
			pick = o.Index
		}
	}
	if pick < 0 {
		t.Fatalf("target ask did not offer the Giant: %+v", d.Options)
	}
	submitChoices(t, e, pick)
	passUntilStackEmpty(t, e, 40)

	if z := e.G.Obj(relic).Zone; z != state.ZGraveyard {
		t.Fatalf("sacrificed artifact in %s, want graveyard", z)
	}
	if got := e.G.Obj(g).Damage; got != 4 {
		t.Fatalf("Giant took %d damage, want 4 (the paid branch)", got)
	}
	if !optionalCostCastInfo(e, spell) {
		t.Fatal("no pay-time CastInfo carrying optionalcostpaid")
	}
	replayCheck(t, e, cfg)
}

// TestOptionalCostVoltageSurgePlainDealsTwo pins the unpaid branch of the
// same carrier: declining the optional sacrifice deals 2.
func TestOptionalCostVoltageSurgePlainDealsTwo(t *testing.T) {
	t.Parallel()
	giant := "Name:Giant\nManaCost:4 G\nTypes:Creature Giant\nPT:5/9\nOracle:x\n"
	e, _, _ := altCostEngine(t, 613, []string{"Voltage Surge"},
		[]string{altArtifactSrc}, []string{giant})
	spell := findCardObj(t, e, 0, "Voltage Surge", state.ZHand)
	relic := findCardObj(t, e, 0, "Test Relic", state.ZBattlefield)
	g := findCardObj(t, e, 1, "Giant", state.ZBattlefield)
	addMana(t, e, 0, "R")
	plain := castModeOption(t, e, spell, "")
	submitChoices(t, e, plain)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("no target ask pending: %+v", d)
	}
	pick := -1
	for _, o := range d.Options {
		if o.Obj == g {
			pick = o.Index
		}
	}
	if pick < 0 {
		t.Fatalf("target ask did not offer the Giant: %+v", d.Options)
	}
	submitChoices(t, e, pick)
	passUntilStackEmpty(t, e, 40)

	if z := e.G.Obj(relic).Zone; z != state.ZBattlefield {
		t.Fatalf("plain cast moved the artifact to %s", z)
	}
	if got := e.G.Obj(g).Damage; got != 2 {
		t.Fatalf("Giant took %d damage, want 2 (the unpaid branch)", got)
	}
	if optionalCostCastInfo(e, spell) {
		t.Fatal("plain cast stamped optionalcostpaid provenance")
	}
}

// TestOptionalCostGravenArchfiendGateReadsTheCast pins the count head's
// CastSA> provenance route on the real carrier: the ETB trigger's
// CheckSVar$ "CastSA>Count$OptionalGenericCostPaid.1.0" holds only on the
// paid cast. The observable is the trigger's body running: DB$ MakeCard
// Conjure is unregistered in this build (a listed defect), so a fired body
// lands the registry's "unimplemented API MakeCard" Note — present on the
// paid cast, absent on the plain one, which is exactly the gate's verdict
// either way.
func TestOptionalCostGravenArchfiendGateReadsTheCast(t *testing.T) {
	t.Parallel()
	t.Run("paid", func(t *testing.T) {
		e, cfg, _ := altCostEngine(t, 614, []string{"Graven Archfiend"},
			[]string{altBearSrc}, nil)
		spell := findCardObj(t, e, 0, "Graven Archfiend", state.ZHand)
		bear := findCardObj(t, e, 0, "Bear", state.ZBattlefield)
		// PRECONDITION: the sacrifice candidate is a non-Demon creature, the
		// cost's own filter; without it the paid offer could not exist.
		if len(e.optionalCostViews(e.collectCostStatics(), 0, spell)) != 1 {
			t.Fatalf("precondition: Graven Archfiend carries no applicable OptionalCost static")
		}
		addMana(t, e, 0, "CCCBB")
		submitChoices(t, e, castModeOption(t, e, spell, "optionalcost"))
		choosePendingObject(t, e, bear)
		passUntilStackEmpty(t, e, 40)

		if z := e.G.Obj(bear).Zone; z != state.ZGraveyard {
			t.Fatalf("sacrificed creature in %s, want graveyard", z)
		}
		perm := e.G.Obj(spell)
		if perm == nil || perm.Zone != state.ZBattlefield {
			t.Fatalf("precondition: Archfiend must be on the battlefield, got %v", perm)
		}
		if !perm.OptionalCostPaid {
			t.Fatal("the permanent did not fold the paid provenance")
		}
		if len(optionalCostNotes(e, "MakeCard")) == 0 {
			t.Fatal("the ETB trigger never ran its body: the CastSA> gate read the cast as unpaid")
		}
		replayCheck(t, e, cfg)
	})
	t.Run("plain", func(t *testing.T) {
		e, _, _ := altCostEngine(t, 615, []string{"Graven Archfiend"},
			[]string{altBearSrc}, nil)
		spell := findCardObj(t, e, 0, "Graven Archfiend", state.ZHand)
		findCardObj(t, e, 0, "Bear", state.ZBattlefield)
		addMana(t, e, 0, "CCCBB")
		submitChoices(t, e, castModeOption(t, e, spell, ""))
		passUntilStackEmpty(t, e, 40)

		perm := e.G.Obj(spell)
		if perm == nil || perm.Zone != state.ZBattlefield {
			t.Fatalf("precondition: Archfiend must be on the battlefield, got %v", perm)
		}
		if perm.OptionalCostPaid {
			t.Fatal("plain cast carries the paid provenance")
		}
		if n := optionalCostNotes(e, "MakeCard"); len(n) != 0 {
			t.Fatalf("the gate failed to hold the trigger back on the plain cast: %v", n)
		}
	})
}

// TestOptionalCostChargesTheSameCompositionTheOfferPriced pins the
// offer/charge agreement when a carrier has BOTH an OptionalCost static and a
// spell-ability additional cost (the SpellAbility `Cost$` that
// withSpellAbilityExtras folds). The hand offer (legal.go) prices
// withSpellAbilityExtras(f, convokeBase).Plus(optional); beginCast must fold
// the same extras before the optional part, or the paid cast silently
// undercharges by the mandatory additional cost. Zero corpus OptionalCost
// carriers pair the two today, so this is a synthetic card -- but the
// disagreement it guards is the exact livelock class (an offered option the
// charge does not cover) the shared fold exists to prevent.
func TestOptionalCostChargesTheSameCompositionTheOfferPriced(t *testing.T) {
	t.Parallel()
	envoy := "Name:Test Envoy\nManaCost:R\nTypes:Instant\n" +
		"S:Mode$ OptionalCost | EffectZone$ All | ValidCard$ Card.Self | ValidSA$ Spell | Cost$ Blight<1> | Description$ x\n" +
		"A:SP$ DealDamage | NumDmg$ 1 | ValidTgts$ Creature | Cost$ Sac<1/Artifact> | SpellDescription$ x\n" +
		"Oracle:x\n"
	e, cfg, _ := altCostEngine(t, 616, nil,
		[]string{envoy, altArtifactSrc, altBearSrc}, []string{altBearSrc})
	spell := findCardObj(t, e, 0, "Test Envoy", state.ZHand)
	relic := findCardObj(t, e, 0, "Test Relic", state.ZBattlefield)
	blighted := findCardObj(t, e, 0, "Bear", state.ZBattlefield)
	victim := findCardObj(t, e, 1, "Bear", state.ZBattlefield)
	// PRECONDITION: the card really carries the SpellAbility Cost$ the fold
	// reads -- without it every assertion below is vacuous.
	if sa := e.G.Obj(spell).Face().SpellAbility(); sa == nil || sa.Params["Cost"] == "" {
		t.Fatalf("precondition: Test Envoy has no SpellAbility Cost$ to fold")
	}
	addMana(t, e, 0, "R")
	submitChoices(t, e, castModeOption(t, e, spell, "optionalcost"))
	// Drive the staged asks: the sac choice, the blight choice, the target.
	// The spell sits in hand through the pre-push cost asks and on the stack
	// from pushCast to resolution, so the loop ends once it has left both.
	for i := 0; i < 40; i++ {
		if e.G.Over {
			break
		}
		if z := e.G.Obj(spell).Zone; z != state.ZHand && z != state.ZStack {
			break
		}
		d := e.Pending()
		if d == nil {
			break
		}
		switch d.Kind {
		case decision.KPriority:
			pass := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					pass = o.Index
				}
			}
			if pass < 0 {
				t.Fatalf("priority decision with no pass option: %+v", d.Options)
			}
			submitChoices(t, e, pass)
		case decision.KChoose:
			pick := -1
			for _, o := range d.Options {
				if o.Obj == relic || o.Obj == blighted {
					pick = o.Index
					break
				}
			}
			if pick < 0 {
				t.Fatalf("KChoose offered neither the artifact nor the blight candidate: %+v", d.Options)
			}
			submitChoices(t, e, pick)
		case decision.KTarget:
			pick := -1
			for _, o := range d.Options {
				if o.Obj == victim {
					pick = o.Index
				}
			}
			if pick < 0 {
				t.Fatalf("target ask did not offer the victim: %+v", d.Options)
			}
			submitChoices(t, e, pick)
		default:
			t.Fatalf("unexpected decision kind %v", d.Kind)
		}
	}
	passUntilStackEmpty(t, e, 40)

	// The SpellAbility Sac<1/Artifact> is the extra the offer priced: if the
	// paid cast did not fold it, the artifact is still on the battlefield.
	if z := e.G.Obj(relic).Zone; z != state.ZGraveyard {
		t.Fatalf("the spell-ability Sac<1/Artifact> extra was not charged (artifact in %s); the paid cast omitted the extras the offer priced", z)
	}
	if got := e.G.Obj(blighted).Counter("M1M1"); got != 1 {
		t.Fatalf("optional Blight<1> counters = %d, want 1", got)
	}
	if got := e.G.Obj(victim).Damage; got != 1 {
		t.Fatalf("victim took %d damage, want 1", got)
	}
	if !optionalCostCastInfo(e, spell) {
		t.Fatal("no pay-time CastInfo carrying optionalcostpaid")
	}
	replayCheck(t, e, cfg)
}
