package rules

// vc-static1: a CantSacrifice restriction static's ValidCause$ scoping is
// evaluated (rules/layers_restrict.go SacrificeBlocked's static walk + causeSpecAdmits,
// rules/trigger_cause.go), not skipped by the parameter whitelist.
//
// The Master, Multiplied's reminder text -- "Triggered abilities you control
// can't cause you to sacrifice or exile creature tokens you control." --
// carries `ValidCause$ Triggered.YouCtrl | ForCost$ False` on both its
// CantSacrifice and CantExile lines. Before this fix
// effects.CantRestrictionParamsReadable rejected the whole static (ValidCause
// and ForCost were not whitelisted), so the protection was inert: any
// triggered sacrifice took the tokens anyway.
//
// The cause is evaluated against actionCause() -- the resolving wrapper at
// the top of the stack on the effect-driven path -- through the shared
// state.StackKindTokenOf/StackKindAdmits classifier trigmatch.DiscardCauseAdmits and
// drawCauseAdmits use, so the static path cannot drift from the trigger path.
// ForCost$ False keeps every COST sacrifice payable (the rules cost sites
// pass forCost=true and skip Cause-scoped lines); ForCost$ True lines stay
// skipped whole (cost provenance is not modelled -- angel_of_jubilation,
// yasharn_implacable_earth, recorded in AGENTS.md's combatrestriction1 row).
//
// Every leaf drives the real effSacrifice / Sac-cost paths on real corpus
// cards and ends replay-verified; the fail-closed leaf uses an authored
// fixture (never a committed .txt).

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// masterTestCards looks up the four corpus cards every leaf in this file
// needs and fails fast when the corpus misses one.
func masterTestCards(t *testing.T) (master, alarm, fleshbag, altar *cards.Card) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	for _, name := range []string{"The Master, Multiplied", "Raise the Alarm",
		"Fleshbag Marauder", "Ashnod's Altar"} {
		if _, ok := reg.Lookup(name); !ok {
			t.Fatalf("corpus missing %s", name)
		}
	}
	master, _ = reg.Lookup("The Master, Multiplied")
	alarm, _ = reg.Lookup("Raise the Alarm")
	fleshbag, _ = reg.Lookup("Fleshbag Marauder")
	altar, _ = reg.Lookup("Ashnod's Altar")
	return master, alarm, fleshbag, altar
}

// tokenOptionsFor returns the pending sacrifice decision's options naming
// object id.
func tokenOptionsFor(d *decision.Decision, id state.ObjID) []decision.Option {
	var out []decision.Option
	for _, o := range d.Options {
		if o.Obj == id {
			out = append(out, o)
		}
	}
	return out
}

// chooseSacrificeAnswer answers the pending KChoose sacrifice decision with
// the option naming want (fatal when absent).
func chooseSacrificeAnswer(t *testing.T, e *Engine, d *decision.Decision, want state.ObjID) {
	t.Helper()
	for _, o := range d.Options {
		if o.Obj == want {
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}); err != nil {
				t.Fatalf("submit sacrifice choice: %v", err)
			}
			return
		}
	}
	t.Fatalf("sacrifice decision offers no option for obj %d: %+v", want, d.Options)
}

// makeTokens casts Raise the Alarm (1W) from seat 0's hand and returns the
// two Soldier tokens' object ids.
func makeTokens(t *testing.T, e *Engine, alarm *cards.Card) (state.ObjID, state.ObjID) {
	t.Helper()
	alarmID := findAndMoveToHand(t, e, 0, "Raise the Alarm")
	addMana(t, e, 0, "CWW")
	castFromPriority(t, e, alarmID)
	passUntilStackEmpty(t, e, 60)
	toks := []state.ObjID{}
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.IsToken && o.Face() != nil && o.Face().Name == "Soldier Token" {
			toks = append(toks, id)
		}
	}
	if len(toks) != 2 {
		t.Fatalf("Raise the Alarm made %d tokens, want 2", len(toks))
	}
	return toks[0], toks[1]
}

// TestMasterMultipliedTriggeredSacrificeBlocked is the filing symptom: with
// The Master, Multiplied on the battlefield, a triggered ability its
// controller controls (Fleshbag Marauder's "each player sacrifices a
// creature" ETB trigger, resolving through real effSacrifice) must not take
// seat 0's Soldier tokens. The tokens drop out of the sacrifice ask's
// options entirely; the ask itself is still posed (The Master and the bear
// remain legal candidates) and seat 0's answer is honoured.
func TestMasterMultipliedTriggeredSacrificeBlocked(t *testing.T) {
	t.Parallel()
	master, alarm, fleshbag, _ := masterTestCards(t)
	bear := card(t, staticBearFixture)
	e, cfg := restrictionGame(t, 9301,
		[][]*cards.Card{{alarm, fleshbag}, nil},
		[][]*cards.Card{{master, bear}, nil})
	masterID := bearOnBoard(t, e, 0, master)
	bearID := bearOnBoard(t, e, 0, bear)
	tokA, tokB := makeTokens(t, e, alarm)

	fleshID := findAndMoveToHand(t, e, 0, "Fleshbag Marauder")
	addMana(t, e, 0, "CCB")
	castFromPriority(t, e, fleshID)
	passPriorityUntilAsk(t, e, "Sacrifice")
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "sacrifice" {
		t.Fatalf("expected the ETB sacrifice ask, got %+v", d)
	}
	if len(tokenOptionsFor(d, tokA))+len(tokenOptionsFor(d, tokB)) != 0 {
		t.Fatalf("a creature token offered for sacrifice under The Master's restriction: %+v", d.Options)
	}
	if len(tokenOptionsFor(d, masterID)) != 1 || len(tokenOptionsFor(d, bearID)) != 1 {
		t.Fatalf("non-token creatures must stay eligible: %+v", d.Options)
	}
	// Answer with the first option (a non-token creature) and let the
	// resolution finish.
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
		t.Fatalf("submit sacrifice choice: %v", err)
	}
	passUntilStackEmpty(t, e, 60)
	if z := e.G.Obj(tokA).Zone; z != state.ZBattlefield {
		t.Fatalf("token A %s: a Triggered.YouCtrl cause must not sacrifice it", z)
	}
	if z := e.G.Obj(tokB).Zone; z != state.ZBattlefield {
		t.Fatalf("token B %s: a Triggered.YouCtrl cause must not sacrifice it", z)
	}
	for _, ev := range e.L.Events {
		if events.IsSacrifice(ev) && (ev.Obj == tokA || ev.Obj == tokB) {
			t.Fatalf("a Sacrifice event took the token under The Master's restriction: %+v", ev)
		}
	}
	replayCheck(t, e, cfg)
}

// TestMasterMultipliedSpellCauseSacrificeAllowed proves the ValidCause$ gate
// actually discriminates: the same static's `Triggered.YouCtrl` must NOT
// block a SPELL-caused sacrifice, so Innocent Blood ("each player sacrifices
// a creature", resolving as a spell) still takes the token -- and a
// whitelist-without-evaluation fix (the over-restriction the brief warns
// about) would fail this leaf.
func TestMasterMultipliedSpellCauseSacrificeAllowed(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	if _, ok := reg.Lookup("Innocent Blood"); !ok {
		t.Fatal("corpus missing Innocent Blood")
	}
	master, alarm, _, _ := masterTestCards(t)
	blood, _ := reg.Lookup("Innocent Blood")
	bear := card(t, staticBearFixture)
	e, cfg := restrictionGame(t, 9302,
		[][]*cards.Card{{alarm, blood}, nil},
		[][]*cards.Card{{master, bear}, nil})
	tokA, _ := makeTokens(t, e, alarm)

	bloodID := findAndMoveToHand(t, e, 0, "Innocent Blood")
	addMana(t, e, 0, "B")
	castFromPriority(t, e, bloodID)
	passPriorityUntilAsk(t, e, "Sacrifice")
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "sacrifice" {
		t.Fatalf("expected the sacrifice ask, got %+v", d)
	}
	if len(tokenOptionsFor(d, tokA)) != 1 {
		t.Fatalf("spell-caused sacrifice must still offer the token (ValidCause$ Triggered excludes spells): %+v", d.Options)
	}
	chooseSacrificeAnswer(t, e, d, tokA)
	passUntilStackEmpty(t, e, 60)
	if z := e.G.Obj(tokA).Zone; z == state.ZBattlefield {
		t.Fatal("the spell-caused sacrifice of the token was blocked: ValidCause$ over-restricts")
	}
	replayCheck(t, e, cfg)
}

// TestMasterMultipliedCostSacrificeAllowed is the ForCost$ False half of the
// gate: a COST sacrifice of the same token is still allowed -- the
// restriction must not over-restrict the player's own value plays. The
// vehicle is the authored Feeder's `Sac<1/Creature>` activated-ability cost
// (the sacAsk cost path, TestDoubleSacCostRequiresDistinctCandidates's
// shape), paid through the real cost-site SacrificeBlocked(id, true) calls.
// Ashnod's Altar specifically cannot carry this leaf: its mana-ability offer
// gate (manaSacrifices, rules/mana_activation.go) withholds an ability whose
// Sac cost has more candidates than required, and The Master itself is
// always a second Creature candidate on the same board.
const feederFixture = "Name:Feeder\nManaCost:2\nTypes:Artifact\n" +
	"A:AB$ GainLife | Cost$ Sac<1/Creature> | Defined$ You | LifeAmount$ 2 | SpellDescription$ Sacrifice a creature: you gain 2 life.\nOracle:x\n"

func TestMasterMultipliedCostSacrificeAllowed(t *testing.T) {
	t.Parallel()
	master, alarm, _, _ := masterTestCards(t)
	feeder := card(t, feederFixture)
	e, cfg := restrictionGame(t, 9303,
		[][]*cards.Card{{alarm, feeder}, nil},
		[][]*cards.Card{{master}, nil})
	masterID := bearOnBoard(t, e, 0, master)
	tokA, _ := makeTokens(t, e, alarm)
	feederID := findAndMoveToHand(t, e, 0, "Feeder")
	moveToBattlefield(t, e, feederID)
	addMana(t, e, 0, "")
	opt, ok := findAbilityOption(e, feederID, 0)
	if !ok {
		t.Fatal("Feeder not offered: the ForCost$ False gate over-restricted its Sac cost")
	}
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "sacrifice" {
		t.Fatalf("expected the Sac cost choice, got %+v", d)
	}
	if len(tokenOptionsFor(d, tokA)) != 1 {
		t.Fatalf("the cost path must still offer the token: %+v", d.Options)
	}
	chooseSacrificeAnswer(t, e, d, tokA)
	passUntilStackEmpty(t, e, 60)
	if z := e.G.Obj(tokA).Zone; z == state.ZBattlefield {
		t.Fatal("the token cost sacrifice never happened")
	}
	if z := e.G.Obj(masterID).Zone; z != state.ZBattlefield {
		t.Fatalf("The Master left the battlefield: %s", z)
	}
	if got := e.G.Players[0].Life; got != 22 {
		t.Fatalf("life after the Feeder resolved = %d, want 22 (+2)", got)
	}
	replayCheck(t, e, cfg)
}

// gatekeeperFixture is the authored fail-closed probe (never a committed
// .txt): a CantSacrifice static whose ValidCause$ names a NON-STACK base
// (`Creature`) and one carrying an unmodelled stack qualifier
// (`Triggered.singleTarget`). Neither shape can be evaluated exactly, so
// both must be skipped rather than blanket-blocking the token.
const gatekeeperFixture = "Name:Test Gatekeeper\nManaCost:2 B\nTypes:Creature Zombie\nPT:2/2\n" +
	"S:Mode$ CantSacrifice | ValidCard$ Creature.YouCtrl+token | ValidCause$ Creature | Description$ unevaluable base\n" +
	"S:Mode$ CantSacrifice | ValidCard$ Creature.YouCtrl+token | ValidCause$ Triggered.singleTarget | Description$ unevaluable qualifier\n" +
	"Oracle:x\n"

// TestMasterMultipliedUnknownCauseFailsClosed pins the fail-closed
// direction: a CantSacrifice static whose ValidCause$ the stack-kind
// classifier cannot evaluate exactly (a non-stack base, an unmodelled
// qualifier) must NOT block the sacrifice -- no over-restriction from a
// spec this build cannot read.
func TestMasterMultipliedUnknownCauseFailsClosed(t *testing.T) {
	t.Parallel()
	_, alarm, fleshbag, _ := masterTestCards(t)
	gatekeeper := card(t, gatekeeperFixture)
	bear := card(t, staticBearFixture)
	e, cfg := restrictionGame(t, 9304,
		[][]*cards.Card{{alarm, fleshbag}, nil},
		[][]*cards.Card{{gatekeeper, bear}, nil})
	tokA, _ := makeTokens(t, e, alarm)

	fleshID := findAndMoveToHand(t, e, 0, "Fleshbag Marauder")
	addMana(t, e, 0, "CCB")
	castFromPriority(t, e, fleshID)
	passPriorityUntilAsk(t, e, "Sacrifice")
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "sacrifice" {
		t.Fatalf("expected the ETB sacrifice ask, got %+v", d)
	}
	if len(tokenOptionsFor(d, tokA)) != 1 {
		t.Fatalf("an unevaluable ValidCause$ spec must not block the token (fail closed): %+v", d.Options)
	}
	chooseSacrificeAnswer(t, e, d, tokA)
	passUntilStackEmpty(t, e, 60)
	if z := e.G.Obj(tokA).Zone; z == state.ZBattlefield {
		t.Fatal("the token sacrifice never happened")
	}
	replayCheck(t, e, cfg)
}
