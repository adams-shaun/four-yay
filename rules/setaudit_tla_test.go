package rules

// Set audit: Avatar: The Last Airbender (tla). One seat's audit of the set's
// named mechanics and its corner cases.
//
// The brief for this file: every finding gets a FAILING unit test, guarded by
// GORGE_SET_AUDIT so the committed suite stays green (a skip is allowed), and a
// follow-up ticket under .ds4/new-tickets/. A test that asserts behaviour the
// engine already gets right stays unguarded: that is regression coverage.
//
// The named mechanics come first (the set's own Airbend/Earthbend/Waterbend/
// Firebending/Exhaust), then evergreen and keyword-action interactions. CR
// citations are the 2026-08-07 Comprehensive Rules text in
// .ds4/MagicCompRules-20260807.txt.

import (
	"os"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// tlaSkip guards a FAILING finding: a skip unless GORGE_SET_AUDIT=1, so the
// committed suite stays green while the defect is open.
func tlaSkip(t *testing.T, reason string) {
	t.Helper()
	if os.Getenv("GORGE_SET_AUDIT") == "" {
		t.Skip("set-audit finding (tla): " + reason)
	}
}

// tlaCorpusCard resolves a real corpus card by name, failing the test if the
// corpus is absent or the card is missing -- a corpus-backed test that skips
// tests nothing (AGENTS.md), so this must be a hard failure.
func tlaCorpusCard(t *testing.T, reg *cards.Registry, name string) *cards.Card {
	t.Helper()
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("corpus fixture: %q missing", name)
	}
	return c
}

// tlaAttackTriggerPump places id on seat 0's battlefield (already able to
// attack), declares it attacking, pushes any attack triggers and resolves the
// top of the stack -- the direct-emission shape other attack-trigger tests
// use.
func tlaAttackTriggerPump(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	e.G.Obj(id).SummonSick = false
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{id}})
	e.putTriggersOnStack()
	if len(e.G.Stack) > 0 {
		e.resolveTop()
	}
}

// tlaCastByName moves a corpus card into seat 0's hand, funds mana, submits
// its cast option and drains the stack. A nil return means no cast option was
// offered (used by the failing finding to report that as the defect).
func tlaCastByName(t *testing.T, e *Engine, name, mana string) (state.ObjID, bool) {
	t.Helper()
	id := searchMoveByName(t, e, name, state.ZHand)
	addMana(t, e, 0, mana)
	idx := -1
	for _, o := range e.Pending().Options {
		if o.Kind == "cast" && o.Obj == id {
			idx = o.Index
		}
	}
	if idx < 0 {
		return id, false
	}
	submitChoices(t, e, idx)
	return id, true
}

// ---------------------------------------------------------------------------
// (a) Firebending -- CR 702.189
// ---------------------------------------------------------------------------

// TestSetAudit_tla_FireSages_FirebendingAddsManaOnAttack pins CR 702.189a:
// "Firebending N" is a triggered ability -- "Whenever this creature attacks,
// add N {R}. Until end of combat, you don't lose this mana as steps and phases
// end." Fire Sages prints K:Firebending:1, so attacking must add one red mana.
func TestSetAudit_tla_FireSages_FirebendingAddsManaOnAttack(t *testing.T) {
	tlaSkip(t, "K:Firebending has no cards/kw_firebending.go expander and no rules handler, so the 21 tla Firebending cards are silent no-ops. Follow-up: tla-firebending-unimplemented")
	reg := testutil.CorpusRegistry(t)
	e, _, _ := newFixtureDeck(t, 17, "Name:Placeholder\nManaCost:0\nTypes:Creature Bear\nPT:1/1\nOracle:x\n")
	fireSages := onBoardCard(t, e, 0, tlaCorpusCard(t, reg, "Fire Sages"))
	tlaAttackTriggerPump(t, e, fireSages)
	if got := e.G.Players[0].Pool[state.MR]; got != 1 {
		t.Fatalf("Firebending 1 added R=%d to the pool, want 1 (CR 702.189a)", got)
	}
}

// TestSetAudit_tla_FirebendingStudent_DynamicPowerAddsMana pins the
// power-derived form used by Firebending Student (K:Firebending:X with
// SVar X:Count$CardPower): a 1/2 student attacking adds one red mana, not a
// fixed amount. CR 702.189a.
func TestSetAudit_tla_FirebendingStudent_DynamicPowerAddsMana(t *testing.T) {
	tlaSkip(t, "K:Firebending has no expander, so the dynamic power count never adds mana. Follow-up: tla-firebending-unimplemented")
	reg := testutil.CorpusRegistry(t)
	e, _, _ := newFixtureDeck(t, 18, "Name:Placeholder\nManaCost:0\nTypes:Creature Bear\nPT:1/1\nOracle:x\n")
	student := onBoardCard(t, e, 0, tlaCorpusCard(t, reg, "Firebending Student"))
	if got := e.Derived(student).Power; got != 1 {
		t.Fatalf("precondition: Firebending Student power = %d, want 1", got)
	}
	tlaAttackTriggerPump(t, e, student)
	if got := e.G.Players[0].Pool[state.MR]; got != 1 {
		t.Fatalf("Firebending X (X=power=1) added R=%d, want 1 (CR 702.189a)", got)
	}
}

// ---------------------------------------------------------------------------
// (a) Airbend -- CR 701.65
// ---------------------------------------------------------------------------

// TestSetAudit_tla_AirbendingLesson_ExilesTarget pins CR 701.65a: airbending a
// permanent exiles it, and for as long as it remains exiled its owner may cast
// it by paying {2} rather than its mana cost. Airbending Lesson is "Airbend
// target nonland permanent. Draw a card." The card compiles (the registry does
// NOT list it unsupported for the keyword action alone in this leaf's shape),
// so the defect is a rules-level no-op, not a parse gap.
func TestSetAudit_tla_AirbendingLesson_ExilesTarget(t *testing.T) {
	tlaSkip(t, "api:Airbend is unregistered, so the 10 tla Airbend cards exile nothing. Follow-up: tla-airbend-unimplemented")
	reg := testutil.CorpusRegistry(t)
	e, _ := searchEngine(t, reg, "Airbending Lesson")
	bear := onBoardCard(t, e, 1, tlaCorpusCard(t, reg, "Grizzly Bears"))
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Grizzly Bears must start on seat 1's battlefield: %+v", o)
	}
	if _, ok := tlaCastByName(t, e, "Airbending Lesson", "WWW"); !ok {
		t.Fatalf("Airbending Lesson not castable for {2}{W}")
	}
	d := e.Pending()
	if d != nil && d.Kind == decision.KTarget {
		picked := false
		for _, o := range d.Options {
			if o.Obj == bear {
				submitChoices(t, e, o.Index)
				picked = true
				break
			}
		}
		if !picked {
			t.Fatalf("airbend target ask did not offer the Bear: %+v", d.Options)
		}
	}
	passUntilStackEmpty(t, e, 40)
	if got := e.G.Obj(bear).Zone; got != state.ZExile {
		t.Fatalf("airbent Bear zone = %v, want ZExile (CR 701.65a)", got)
	}
}

// ---------------------------------------------------------------------------
// (a) Earthbend -- CR 701.66 (regression: expected correct)
// ---------------------------------------------------------------------------

// TestSetAudit_tla_EarthbendingLesson_AnimatesLandWithCounters pins CR 701.66a
// for the set's own Earthbend spelling: "Earthbend 4" makes a target land a
// 0/0 land creature with haste in addition to its other types and puts four
// +1/+1 counters on it. Earthbending Lesson prints Num$ 4. This leaf is
// expected to PASS -- regression coverage for a mechanic 27 tla cards lean on.
func TestSetAudit_tla_EarthbendingLesson_AnimatesLandWithCounters(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e, _ := searchEngine(t, reg, "Earthbending Lesson")
	forest := searchMoveByName(t, e, "Forest", state.ZBattlefield)
	if o := e.G.Obj(forest); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Forest must start on the battlefield: %+v", o)
	}
	if _, ok := tlaCastByName(t, e, "Earthbending Lesson", "GGGG"); !ok {
		t.Fatalf("Earthbending Lesson not castable for {3}{G}")
	}
	earthbendSettleAnswering(t, e, forest, 64)
	o := e.G.Obj(forest)
	if o.Zone != state.ZBattlefield {
		t.Fatalf("earthbent Forest left the battlefield: %v", o.Zone)
	}
	if !e.IsCreature(forest) {
		t.Fatalf("earthbent land is not a creature (CR 701.66a)")
	}
	if got := o.Counter("P1P1"); got != 4 {
		t.Fatalf("earthbent land has %d +1/+1 counters, want 4 (CR 701.66a)", got)
	}
	if !e.HasKeyword(forest, "Haste") {
		t.Fatalf("earthbent land lacks haste (CR 701.66a)")
	}
}

// ---------------------------------------------------------------------------
// (a) Waterbend -- CR 701.67
// ---------------------------------------------------------------------------

// TestSetAudit_tla_GiantKoi_WaterbendAbilityCostTapsCreatures pins CR 701.67a
// for the set's own Waterbend spelling on an ACTIVATED ABILITY (Giant Koi's
// "Waterbend {3}: This creature can't be blocked this turn."). The {3} may be
// paid by tapping untapped artifacts and creatures, so a player with three
// creatures and an empty pool can still activate it. The engine's raise-cost
// reader documents that Waterbend<N> ABILITY costs (the activation flow has no
// contribution announcement) keep their Unknown fallback, so the offer is
// withheld entirely when the pool cannot pay the {3}: that is the finding.
func TestSetAudit_tla_GiantKoi_WaterbendAbilityCostTapsCreatures(t *testing.T) {
	tlaSkip(t, "Waterbend<N> ACTIVATED-ABILITY costs have no contribution announcement; Giant Koi's ability is withheld unless the pool pays {3} in mana, so tapping creatures cannot pay it. Follow-up: tla-waterbend-ability-cost")
	reg := testutil.CorpusRegistry(t)
	e, _ := searchEngine(t, reg, "Giant Koi")
	koiID := searchMoveByName(t, e, "Giant Koi", state.ZBattlefield)
	if o := e.G.Obj(koiID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Giant Koi must be on the battlefield: %+v", o)
	}
	for i := 0; i < 3; i++ {
		onBoardReady(t, e, 0, "Name:Filler\nManaCost:0\nTypes:Creature Fish\nPT:1/1\nOracle:x\n")
	}
	// Re-ask so the freshly placed permanents are in the option snapshot.
	e.priorityRound()
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("precondition: pool must be empty so the ability is payable only by tapping: got %d", got)
	}
	if _, ok := findAbilityOption(e, koiID, 0); !ok {
		t.Fatalf("Giant Koi's Waterbend {3} ability not offered with three creatures to tap and an empty pool (CR 701.67a): %+v", e.Pending().Options)
	}
}

// tlaTapPoolAnswer submits pass on every priority decision and picks a
// waterbend_generic tap wherever one is offered, until the stack drains.
func tlaTapPoolAnswer(t *testing.T, e *Engine, limit int) {
	t.Helper()
	for n := 0; n < limit; n++ {
		d := e.Pending()
		if d == nil {
			return
		}
		if d.Kind != decision.KPriority {
			return
		}
		if len(e.G.Stack) == 0 {
			return
		}
		idx := -1
		for _, o := range d.Options {
			if o.Kind == "waterbend_generic" {
				idx = o.Index
				break
			}
		}
		if idx < 0 {
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
		}
		if idx < 0 {
			t.Fatalf("priority decision with no pass or waterbend option: %+v", d)
		}
		submitChoices(t, e, idx)
	}
}

// TestSetAudit_tla_WaterWhip_WaterbendCastCostTapsCreatures pins the
// CAST-time Waterbend<5> shape (CR 701.67a): with {U}{U} and five creatures,
// Water Whip must be castable by tapping the five creatures for the generic
// {5}. This leaf is expected to PASS -- regression coverage for the Waterbend
// half the engine does implement.
func TestSetAudit_tla_WaterWhip_WaterbendCastCostTapsCreatures(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e, _ := searchEngine(t, reg, "Water Whip")
	waterWhip := searchMoveByName(t, e, "Water Whip", state.ZHand)
	for i := 0; i < 5; i++ {
		onBoardReady(t, e, 0, "Name:Filler\nManaCost:0\nTypes:Creature Fish\nPT:1/1\nOracle:x\n")
	}
	addMana(t, e, 0, "UU")
	e.pending = nil
	e.priorityRound()
	offered := false
	for _, o := range e.Pending().Options {
		if o.Kind == "cast" && o.Obj == waterWhip {
			offered = true
		}
	}
	if !offered {
		t.Fatalf("Water Whip (Waterbend<5>) not offered with {U}{U} and five creatures to tap (CR 701.67a): %+v", e.Pending().Options)
	}
}

// ---------------------------------------------------------------------------
// (a) Exhaust -- CR 702.177 (regression: expected correct)
// ---------------------------------------------------------------------------

// TestSetAudit_tla_HogMonkey_ExhaustOnceEver pins CR 702.177a: an exhaust
// ability ("Activate each exhaust ability only once") may be activated only
// once for the whole game, not once per turn. Hog-Monkey's Exhaust -- {5} puts
// two +1/+1 counters on it. This leaf is expected to PASS -- regression
// coverage for a keyword 8 tla cards carry.
func TestSetAudit_tla_HogMonkey_ExhaustOnceEver(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e, _ := searchEngine(t, reg, "Hog-Monkey")
	monkeyID := searchMoveByName(t, e, "Hog-Monkey", state.ZBattlefield)
	exhaustBattlefield(t, e, monkeyID)
	addMana(t, e, 0, "CCCCCCCCCC") // two {5} payments
	exhaustPriorityAtSeatZero(t, e)
	opt := abilityOption(t, e, monkeyID, 0)
	submitChoices(t, e, opt.Index)
	if _, ok := findAbilityOption(e, monkeyID, 0); ok {
		t.Fatalf("Hog-Monkey exhaust ability offered twice in one turn (CR 702.177a)")
	}
	// And it must stay withheld on a later turn too. Hog-Monkey's
	// BeginCombat menace trigger poses a target ask on the way, so drive with
	// a local answerer that picks the trigger's own first target.
	tlaDriveToMain1Answering(t, e, e.G.Turn+2, 0)
	exhaustBattlefield(t, e, monkeyID)
	addMana(t, e, 0, "CCCCC")
	if _, ok := findAbilityOption(e, monkeyID, 0); ok {
		t.Fatalf("Hog-Monkey exhaust ability re-offered on a later turn (CR 702.177a)")
	}
}

// tlaDriveToMain1Answering drives to the given turn's seat-0 main1, answering
// a KTarget ask with its first legal option (Hog-Monkey's BeginCombat menace
// trigger) and passing priority otherwise.
func tlaDriveToMain1Answering(t *testing.T, e *Engine, turn int32, active state.PlayerID) {
	t.Helper()
	for i := 0; i < 4000; i++ {
		if e.G.Turn == turn && e.G.Active == active && e.G.Step == state.StepMain1 {
			return
		}
		if e.G.Over {
			t.Fatalf("game ended driving to turn %d seat %d main1", turn, active)
		}
		if answerIfDiscard(t, e) {
			continue
		}
		d := e.Pending()
		if d == nil {
			t.Fatalf("no pending decision driving to turn %d seat %d main1", turn, active)
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
				t.Fatalf("priority decision with no pass option: %+v", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pass}}); err != nil {
				t.Fatalf("submit pass: %v", err)
			}
		case decision.KTarget:
			if len(d.Options) == 0 {
				t.Fatalf("target ask with no options: %+v", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
				t.Fatalf("submit target: %v", err)
			}
		default:
			t.Fatalf("decision %v while driving to turn %d seat %d main1: %+v", d.Kind, turn, active, d)
		}
	}
	t.Fatalf("never reached turn %d seat %d main1", turn, active)
}
