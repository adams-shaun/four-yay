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

// ---------------------------------------------------------------------------
// (b) OptionalCost with a Waterbend<N> cost -- CR 601.2f / 701.67
// ---------------------------------------------------------------------------

// TestSetAudit_tla_RuinousWaterbending_OptionalCostOffered pins the optional
// additional cost shape `S:Mode$ OptionalCost | Cost$ Waterbend<4>`: "As an
// additional cost to cast this spell, you may waterbend {4}." With four
// untapped creatures and enough mana, the engine must offer BOTH the plain
// cast and the "(optional cost)" cast, because CR 601.2f makes the payer
// choose whether to pay it. optionalCostViews drops any static whose Cost$
// ParseCost cannot model, and ParseCost does not model Waterbend<N>, so the
// paid branch is unreachable for the 3 tla cards that carry this shape.
func TestSetAudit_tla_RuinousWaterbending_OptionalCostOffered(t *testing.T) {
	tlaSkip(t, "S:Mode$ OptionalCost with Cost$ Waterbend<N> is dropped by optionalCostViews (ParseCost.Unknown != 0), so Ruinous Waterbending/Spirit Water Revival/Secret of Bloodbending never offer their paid branch. Follow-up: tla-waterbend-ability-cost")
	reg := testutil.CorpusRegistry(t)
	e, _ := searchEngine(t, reg, "Ruinous Waterbending")
	rb := searchMoveByName(t, e, "Ruinous Waterbending", state.ZHand)
	for i := 0; i < 4; i++ {
		onBoard(t, e, 0, "Name:Tap Fodder\nManaCost:0\nTypes:Creature Goat\nPT:1/1\nOracle:x\n")
	}
	addMana(t, e, 0, "BBC")
	e.pending = nil
	e.priorityRound()
	optional := false
	for _, o := range e.Pending().Options {
		if o.Kind == "cast" && o.Obj == rb && o.Mode == "optionalcost" {
			optional = true
		}
	}
	if !optional {
		t.Fatalf("Ruinous Waterbending (optional waterbend {4}) offered no optional-cost cast with four creatures to tap (CR 601.2f): %+v", e.Pending().Options)
	}
}

// ---------------------------------------------------------------------------
// (a)/(b) Diligent Zookeeper -- Count$ValidSelf
// ---------------------------------------------------------------------------

// TestSetAudit_tla_DiligentZookeeper_CountsCreatureTypes pins the continuous
// bonus "Each non-Human creature you control gets +1/+1 for each of its
// creature types, to a maximum of 10." A Cat Soldier is two creature types, so
// it must be +2/+2; a Human creature gets nothing. Count$ValidSelf
// (Card$CreatureType/LimitMax.10) is unread, so the static is a no-op and the
// card is counted supported while its whole rules text does nothing.
func TestSetAudit_tla_DiligentZookeeper_CountsCreatureTypes(t *testing.T) {
	tlaSkip(t, "Count$ValidSelf (Card$CreatureType/LimitMax.10) is unread, so Diligent Zookeeper's +1/+1-per-type bonus is a no-op. Follow-up: tla-diligent-zookeeper-validself-count")
	reg := testutil.CorpusRegistry(t)
	e := combatEngine(t)
	onBoardCard(t, e, 0, tlaCorpusCard(t, reg, "Diligent Zookeeper"))
	cat := onBoard(t, e, 0, "Name:Feline Warrior\nManaCost:0\nTypes:Creature Cat Soldier\nPT:2/2\nOracle:x\n")
	human := onBoard(t, e, 0, "Name:Village Guard\nManaCost:0\nTypes:Creature Human Soldier\nPT:2/2\nOracle:x\n")
	dc := e.Derived(cat)
	if dc.Power != 4 || dc.Toughness != 4 {
		t.Fatalf("Cat Soldier P/T = %d/%d, want 4/4 (2 creature types, +2/+2)", dc.Power, dc.Toughness)
	}
	dh := e.Derived(human)
	if dh.Power != 2 || dh.Toughness != 2 {
		t.Fatalf("Human Soldier P/T = %d/%d, want 2/2 (non-Human filter excludes it)", dh.Power, dh.Toughness)
	}
}

// ---------------------------------------------------------------------------
// (c) Graveyard census + target-player mill -- Master Pakku (regression)
// ---------------------------------------------------------------------------

// TestSetAudit_tla_MasterPakku_MillsForLessonsInGraveyard pins CR 701.13a for a
// graveyard-reading "for each" count: Master Pakku's "Whenever CARDNAME
// becomes tapped, target player mills X cards, where X is the number of Lesson
// cards in your graveyard" (Count$ValidGraveyard Lesson.YouOwn). With two
// Lesson cards in the controller's graveyard, tapping Pakku must mill the
// targeted player exactly two cards. This leaf is expected to PASS --
// regression coverage for the graveyard-count + mill family the set leans on
// (8 Mill cards, Master Pakku among them).
func TestSetAudit_tla_MasterPakku_MillsForLessonsInGraveyard(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e, _ := searchEngine(t, reg, "Master Pakku", "Airbending Lesson", "Sokka's Haiku")
	pakku := searchMoveByName(t, e, "Master Pakku", state.ZBattlefield)
	if o := e.G.Obj(pakku); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Master Pakku must be on the battlefield: %+v", o)
	}
	for _, name := range []string{"Airbending Lesson", "Sokka's Haiku"} {
		id := searchMoveByName(t, e, name, state.ZGraveyard)
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZGraveyard {
			t.Fatalf("precondition: %s must be in the graveyard: %+v", name, o)
		}
	}
	before := len(e.G.Zone(state.ZGraveyard, 1))
	e.emit(events.Event{Kind: events.Tap, Obj: pakku, Player: 0})
	tlaAnswerTargetsAndPass(t, e, 1, 64)
	after := len(e.G.Zone(state.ZGraveyard, 1))
	if after-before != 2 {
		t.Fatalf("targeted player milled %d cards, want 2 (one per Lesson card in the graveyard)", after-before)
	}
}

// tlaAnswerTargetsAndPass answers every KTarget ask naming player p (or, if no
// option names p, the first option) and passes every priority decision until
// the stack and trigger queue drain.
func tlaAnswerTargetsAndPass(t *testing.T, e *Engine, p state.PlayerID, limit int) {
	t.Helper()
	for n := 0; n < limit; n++ {
		d := e.Pending()
		if d == nil {
			return
		}
		switch d.Kind {
		case decision.KTarget:
			idx := d.Options[0].Index
			for _, o := range d.Options {
				if o.Player == p {
					idx = o.Index
				}
			}
			submitChoices(t, e, idx)
		case decision.KPriority:
			if len(e.G.Stack) == 0 && len(e.pendingTriggers) == 0 {
				return
			}
			pass := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					pass = o.Index
				}
			}
			if pass < 0 {
				t.Fatalf("priority decision with no pass option: %+v", d)
			}
			submitChoices(t, e, pass)
		default:
			t.Fatalf("unexpected decision %v while settling: %+v", d.Kind, d)
		}
	}
	t.Fatalf("engine never settled within %d answers", limit)
}

// ---------------------------------------------------------------------------
// (c) Scry trigger on a library look -- Planetarium of Wan Shi Tong (finding)
// ---------------------------------------------------------------------------

// tlaTriggerPushes counts the trigger pushes whose source is id: the direct
// "did this permanent's triggered ability reach the stack" observable, the
// same one anchorTriggerPushes (temporal_anchor_test.go) uses. A bare Scry
// never emits a TriggerPush, so unlike a Note this cannot be satisfied by the
// scry primitive itself.
func tlaTriggerPushes(e *Engine, id state.ObjID) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.TriggerPush && ev.Obj == id {
			n++
		}
	}
	return n
}

// TestSetAudit_tla_Planetarium_ScryTriggerLooksAtTop pins the library half of
// "Whenever you scry or surveil, look at the top card of your library. You may
// cast that card without paying its mana cost. Do this only once each turn."
// Planetarium of Wan Shi Tong carries the trigger as `T:Mode$ Scry` conditioned
// on `PresentDefined$ Remembered | IsPresent$ Card | PresentCompare$ EQ0`;
// activating its `{1},{T}: Scry 2` ability must fire it. The observable is the
// trigger's own TriggerPush -- a bare scry (effLookAndArrange) emits only its
// own "looks at the top of the library" Note and no TriggerPush, so asserting
// that Note (the earlier revision of this leaf) made the test vacuous. The
// engine's trigger present-clause reader accepts only `PresentDefined$ Self`
// and fails every other defined group closed (rules/trigger_condition.go
// presentClauseHolds), so `PresentDefined$ Remembered` suppresses the trigger
// and the whole card is a no-op: a finding.
func TestSetAudit_tla_Planetarium_ScryTriggerLooksAtTop(t *testing.T) {
	tlaSkip(t, "a trigger present clause with PresentDefined$ Remembered fails closed, so Planetarium's Mode$ Scry trigger never fires. Follow-up: tla-trigger-present-defined-remembered")
	reg := testutil.CorpusRegistry(t)
	e, _ := searchEngine(t, reg, "Planetarium of Wan Shi Tong")
	p := searchMoveByName(t, e, "Planetarium of Wan Shi Tong", state.ZBattlefield)
	o := e.G.Obj(p)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Planetarium must be on the battlefield: %+v", o)
	}
	// Precondition: the card really carries the Mode$ Scry trigger with the
	// Remembered present clause this leaf blames.
	sawScry := false
	for _, tr := range o.Face().Triggers {
		if tr.Mode == "Scry" && tr.Params["PresentDefined"] == "Remembered" {
			sawScry = true
		}
	}
	if !sawScry {
		t.Fatalf("precondition: Planetarium lost its Mode$ Scry/PresentDefined$ Remembered trigger: %+v", o.Face().Triggers)
	}
	addMana(t, e, 0, "CC")
	opt := abilityOption(t, e, p, 0)
	submitChoices(t, e, opt.Index)
	// The scry ask: keep every card on top, in the order offered.
	sawArrange := false
	for n := 0; n < 40; n++ {
		d := e.Pending()
		if d == nil {
			break
		}
		if d.Kind == decision.KArrange {
			sawArrange = true
			submitChoices(t, e, d.Options[0].Index)
			continue
		}
		if d.Kind == decision.KPriority {
			if len(e.G.Stack) == 0 && len(e.pendingTriggers) == 0 {
				break
			}
			pass := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					pass = o.Index
				}
			}
			submitChoices(t, e, pass)
			continue
		}
		break
	}
	if !sawArrange {
		t.Fatal("precondition: Scry 2 must pose a KArrange decision")
	}
	// Precondition: the scry completed and emitted its own marker, so the
	// Mode$ Scry match had a real event to match against.
	if n := len(scryMarkers(e)); n == 0 {
		t.Fatal("precondition: the scry completed without emitting an events.Scry marker")
	}
	// The trigger-only observable: the trigger must reach the stack. A bare
	// scry never emits a TriggerPush for the activating permanent.
	if n := tlaTriggerPushes(e, p); n == 0 {
		t.Fatalf("Planetarium's Mode$ Scry trigger never fired after a real scry (TriggerPush count = 0; the scry's own Note is not proof): its PresentDefined$ Remembered condition fails closed (CR 603.4)")
	}
}
