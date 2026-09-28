package rules

// Set audit: Secrets of Strixhaven (sos) — 271 cards (.ds4/brief.md).
//
// Every card named here is a real corpus card. The audit examines:
//
//   (a) oracle keyword support — does the engine implement the set's
//       keywords/ability words with CR-correct behaviour, not merely
//       "the card compiles"? A counted-supported card whose keyword is
//       a no-op, a Note, or an approximation is a finding.
//   (b) corner cases — modal/MDFC faces, alternative costs, "up to one"
//       targets, zone changes mid-resolution.
//   (c) zone interactions — mill, counters from zones, exile.
//
// A test that PASSES is regression coverage. A test that FAILS is a
// finding: it asserts the CR-correct behaviour, cites the rule, and is
// guarded with GORGE_SET_AUDIT so the ordinary suite stays green until
// the follow-up ticket lands.

import (
	"os"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func sosCard(t *testing.T, name string) *cards.Card {
	t.Helper()
	c, ok := testutil.CorpusRegistry(t).Lookup(name)
	if !ok {
		t.Fatalf("corpus missing %q -- the sos set cannot be audited", name)
	}
	return c
}

const sosBearSrc = "Name:Test Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
const sosBoltSrc = "Name:Test Bolt\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1\nOracle:x\n"
const sosHealSrc = "Name:Test Heal\nManaCost:G\nTypes:Instant\nA:SP$ GainLife | Defined$ You | LifeAmount$ 1\nOracle:x\n"
const sosBombSrc = "Name:Test Bomb\nManaCost:5\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1\nOracle:x\n"
const sosInsightSrc = "Name:Test Insight\nManaCost:2\nTypes:Instant\nA:SP$ GainLife | Defined$ You | LifeAmount$ 1\nOracle:x\n"
const sosRelicSrc = "Name:Test Relic\nManaCost:2\nTypes:Artifact\nOracle:x\n"

// sosDrain passes priority (and answers any KTarget ask on `target`, or a
// KChoose/KModes ask with its first option) until the stack is empty. It
// fails loudly on anything else, so an unexpected ask in an audit flow is
// visible instead of silently eaten. A target of 0 rejects any KTarget ask.
func sosDrain(t *testing.T, e *Engine, target state.ObjID, limit int) {
	t.Helper()
	for i := 0; i < limit; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no pending decision while draining (stack depth %d)", len(e.G.Stack))
		}
		switch d.Kind {
		case decision.KTarget:
			idx := -1
			for _, o := range d.Options {
				if target != 0 && o.Obj == target {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("drain: unexpected target ask (wanted %d): %+v", target, d.Options)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
				t.Fatalf("submit drain target: %v", err)
			}
		case decision.KPriority:
			if len(e.G.Stack) == 0 {
				return
			}
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("drain: priority decision with no pass option: %+v", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
				t.Fatalf("submit drain pass: %v", err)
			}
		case decision.KChoose, decision.KModes:
			if len(d.Options) == 0 {
				t.Fatalf("drain: ask with no options: %+v", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
				t.Fatalf("submit drain first option: %v", err)
			}
		default:
			t.Fatalf("drain: unexpected decision %v: %+v", d.Kind, d)
		}
	}
	t.Fatalf("drain did not empty the stack within %d steps", limit)
}

// sosAnswerPlayerTarget answers the pending KTarget ask by selecting the
// option naming player p.
func sosAnswerPlayerTarget(t *testing.T, e *Engine, p state.PlayerID) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a target decision, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == p {
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}); err != nil {
				t.Fatalf("submit player target: %v", err)
			}
			return
		}
	}
	t.Fatalf("no player-%d target option in %+v", p, d.Options)
}

// sosWardAsk passes priority (whoever holds it) until the ward unless-pay
// ask appears, then returns it.
func sosWardAsk(t *testing.T, e *Engine) *decision.Decision {
	t.Helper()
	for i := 0; i < 10; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no pending decision while waiting for the ward ask")
		}
		if d.Kind == decision.KModes && d.ResumeKind == "unless_pay" {
			return d
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("unexpected decision while waiting for the ward ask: %+v", d)
		}
		sosPassPriority(t, e)
	}
	t.Fatal("the ward unless-pay ask never appeared")
	return nil
}

// sosPassPriority submits the pass option for the currently pending priority
// decision.
func sosPassPriority(t *testing.T, e *Engine) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("expected a priority decision, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind == "pass" {
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}); err != nil {
				t.Fatalf("submit pass: %v", err)
			}
			return
		}
	}
	t.Fatalf("no pass option in %+v", d.Options)
}

// ---------------------------------------------------------------------------
// (a) Converge: "This creature enters with a +1/+1 counter on it for each
// color of mana spent to cast it." Rancorous Archaic (2/2) — nine converge
// cards in the set.
func TestSetAudit_sos_RancorousArchaic_ConvergeCounters(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 901, []string{"Rancorous Archaic"}, nil, nil)
	archaic := findAndMoveToHand(t, e, 0, "Rancorous Archaic")
	if archaic == 0 {
		t.Fatal("Rancorous Archaic not found")
	}
	addMana(t, e, 0, "WWUUB") // {5} paid with exactly 3 colours (W,U,B)
	submitChoices(t, e, castOptionFor(t, e, archaic).Index)
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(archaic); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Rancorous Archaic did not enter the battlefield")
	}
	// Three colours of mana spent → exactly three +1/+1 counters (2/2 → 5/5).
	if got := e.G.Obj(archaic).Counter("P1P1"); got != 3 {
		t.Errorf("converge: +1/+1 counters = %d, want 3 (three colours of mana spent)", got)
	}
	if got, want := e.Derived(archaic).Power, int32(5); got != want {
		t.Errorf("converge: power = %d, want %d", got, want)
	}
	replayCheck(t, e, cfg)
}

// ---------------------------------------------------------------------------
// (a) Infusion — "If you gained life this turn, ..." (12 cards). The
// condition must gate the rider: no life gained → main effect only; life
// gained → rider applies.
func TestSetAudit_sos_FoolishFate_InfusionGatesRider(t *testing.T) {
	t.Run("no life gained this turn", func(t *testing.T) {
		t.Parallel()
		e, cfg, _ := altCostEngine(t, 902, []string{"Foolish Fate"}, nil, []string{sosBearSrc})
		bear := putCreature(t, e, 1, sosBearSrc)
		fate := findAndMoveToHand(t, e, 0, "Foolish Fate")
		addMana(t, e, 0, "BBB") // {2}{B}
		submitChoices(t, e, castOptionFor(t, e, fate).Index)
		answerTargetAsk(t, e, []state.ObjID{bear})
		passUntilStackEmpty(t, e, 20)
		if o := e.G.Obj(bear); o == nil || o.Zone != state.ZGraveyard {
			t.Fatal("precondition: Test Bear was not destroyed by Foolish Fate")
		}
		if life := e.G.Players[1].Life; life != 20 {
			t.Errorf("infusion off-branch: foe life = %d, want 20 (no life gained this turn, so the rider must not run)", life)
		}
		replayCheck(t, e, cfg)
	})
	t.Run("life gained this turn", func(t *testing.T) {
		t.Parallel()
		e, cfg, _ := altCostEngine(t, 903, []string{"Foolish Fate"}, []string{sosHealSrc}, []string{sosBearSrc})
		bear := putCreature(t, e, 1, sosBearSrc)
		heal := findAndMoveToHand(t, e, 0, "Test Heal")
		addMana(t, e, 0, "G")
		submitChoices(t, e, castOptionFor(t, e, heal).Index)
		passUntilStackEmpty(t, e, 20)
		if life := e.G.Players[0].Life; life != 21 {
			t.Fatalf("precondition: seat 0 life = %d, want 21 after Test Heal (life gained this turn)", life)
		}
		fate := findAndMoveToHand(t, e, 0, "Foolish Fate")
		addMana(t, e, 0, "BBB") // {2}{B}
		submitChoices(t, e, castOptionFor(t, e, fate).Index)
		answerTargetAsk(t, e, []state.ObjID{bear})
		passUntilStackEmpty(t, e, 20)
		if o := e.G.Obj(bear); o == nil || o.Zone != state.ZGraveyard {
			t.Fatal("precondition: Test Bear was not destroyed by Foolish Fate")
		}
		// Infusion rider: the creature's controller loses 3 life (20 → 17).
		if life := e.G.Players[1].Life; life != 17 {
			t.Errorf("infusion on-branch: foe life = %d, want 17 (destroy + lose 3 because life was gained this turn)", life)
		}
		replayCheck(t, e, cfg)
	})
}

// ---------------------------------------------------------------------------
// (a) Repartee — "Whenever you cast an instant or sorcery spell that targets
// a creature, ..." (12 cards). Fires only when YOUR spell targets a CREATURE
// (the corpus trigger filters ValidActivatingPlayer$ You plus
// TargetsValid$ Creature.inZoneBattlefield).
func TestSetAudit_sos_GraduationDay_ReparteeGatesOnCreatureTarget(t *testing.T) {
	t.Run("fires: instant targeting a creature", func(t *testing.T) {
		t.Parallel()
		e, cfg, _ := altCostEngine(t, 904, []string{"Graduation Day"}, []string{sosBearSrc, sosBoltSrc}, nil)
		grad := findAndMoveToHand(t, e, 0, "Graduation Day")
		addMana(t, e, 0, "W")
		submitChoices(t, e, castOptionFor(t, e, grad).Index)
		passUntilStackEmpty(t, e, 20)
		if o := e.G.Obj(grad); o == nil || o.Zone != state.ZBattlefield {
			t.Fatal("precondition: Graduation Day not on battlefield")
		}
		bear := putCreature(t, e, 0, sosBearSrc)
		bolt := findAndMoveToHand(t, e, 0, "Test Bolt")
		addMana(t, e, 0, "R")
		submitChoices(t, e, castOptionFor(t, e, bolt).Index)
		// Answers the bolt's target ask AND the Repartee placement ask (both
		// offer the bear).
		sosDrain(t, e, bear, 30)
		if got := e.G.Obj(bear).Counter("P1P1"); got != 1 {
			t.Errorf("repartee: bear +1/+1 counters = %d, want 1", got)
		}
		replayCheck(t, e, cfg)
	})
	t.Run("does not fire: instant targeting a player", func(t *testing.T) {
		t.Parallel()
		e, cfg, _ := altCostEngine(t, 905, []string{"Graduation Day"}, []string{sosBearSrc, sosBoltSrc}, nil)
		grad := findAndMoveToHand(t, e, 0, "Graduation Day")
		addMana(t, e, 0, "W")
		submitChoices(t, e, castOptionFor(t, e, grad).Index)
		passUntilStackEmpty(t, e, 20)
		bear := putCreature(t, e, 0, sosBearSrc)
		bolt := findAndMoveToHand(t, e, 0, "Test Bolt")
		addMana(t, e, 0, "R")
		submitChoices(t, e, castOptionFor(t, e, bolt).Index)
		sosAnswerPlayerTarget(t, e, 1)
		// No further target ask may appear: the spell targeted a player, so
		// Repartee must not trigger (a drain target of 0 rejects any KTarget).
		sosDrain(t, e, 0, 30)
		if got := e.G.Obj(bear).Counter("P1P1"); got != 0 {
			t.Errorf("repartee: bear +1/+1 counters = %d, want 0 (the spell targeted a player, not a creature)", got)
		}
		replayCheck(t, e, cfg)
	})
}

// ---------------------------------------------------------------------------
// (a) Opus — "Whenever you cast an instant or sorcery spell, ... If five or
// more mana was spent to cast that spell, ... instead." (10 cards), plus the
// stun-counter ETB (CR 122.1d).
func TestSetAudit_sos_DelugeVirtuoso_OpusManaSpentThreshold(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 906, []string{"Deluge Virtuoso"}, []string{sosHealSrc, sosBombSrc}, []string{sosBearSrc})
	bear := putCreature(t, e, 1, sosBearSrc)
	virtuoso := findAndMoveToHand(t, e, 0, "Deluge Virtuoso")
	addMana(t, e, 0, "UUU") // {2}{U}
	submitChoices(t, e, castOptionFor(t, e, virtuoso).Index)
	sosDrain(t, e, bear, 30) // ETB ask: tap + stun the bear
	if o := e.G.Obj(virtuoso); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Deluge Virtuoso not on battlefield")
	}
	if !e.G.Obj(bear).Tapped {
		t.Errorf("opus ETB: bear should be tapped")
	}
	if got := e.G.Obj(bear).Counter("STUN"); got != 1 {
		t.Errorf("opus ETB: stun counters on bear = %d, want 1 (CR 122.1d)", got)
	}
	if got := e.Derived(virtuoso).Power; got != 2 || e.Derived(virtuoso).Toughness != 2 {
		t.Fatalf("precondition: virtuoso PT = %d/%d, want 2/2 before any Opus pump",
			e.Derived(virtuoso).Power, e.Derived(virtuoso).Toughness)
	}
	// A one-mana instant: spent < 5 → +1/+1.
	heal := findAndMoveToHand(t, e, 0, "Test Heal")
	addMana(t, e, 0, "G")
	submitChoices(t, e, castOptionFor(t, e, heal).Index)
	passUntilStackEmpty(t, e, 20)
	if got := e.Derived(virtuoso).Power; got != 3 {
		t.Errorf("opus cheap cast: virtuoso power = %d, want 3 (+1/+1 for <5 mana spent)", got)
	}
	// A five-mana spell: spent 5 ≥ 5 → +2/+2 instead.
	bomb := findAndMoveToHand(t, e, 0, "Test Bomb")
	addMana(t, e, 0, "RRRRR")
	submitChoices(t, e, castOptionFor(t, e, bomb).Index)
	sosAnswerPlayerTarget(t, e, 1)
	passUntilStackEmpty(t, e, 20)
	if got := e.Derived(virtuoso).Power; got != 5 {
		t.Errorf("opus threshold cast: virtuoso power = %d, want 5 (+2/+2 for ≥5 mana spent)", got)
	}
	replayCheck(t, e, cfg)
}

// ---------------------------------------------------------------------------
// (a) Ward (CR 702.36) — Inkshape Demonstrator: Ward {2}. The targeting
// opponent may pay {2} or let the spell be countered.
func TestSetAudit_sos_InkshapeDemonstrator_WardPayOrCounter(t *testing.T) {
	// castWardFlow gets the Demonstrator onto the battlefield and the foe's
	// Test Bolt cast onto the stack targeting it, leaving the ward pay ask
	// pending.
	castWardFlow := func(t *testing.T, seed uint64) (*Engine, Config, state.ObjID) {
		t.Helper()
		e, cfg, _ := altCostEngine(t, seed, []string{"Inkshape Demonstrator"}, nil, []string{sosBoltSrc})
		demo := findAndMoveToHand(t, e, 0, "Inkshape Demonstrator")
		addMana(t, e, 0, "WWWW") // {3}{W}
		submitChoices(t, e, castOptionFor(t, e, demo).Index)
		passUntilStackEmpty(t, e, 20)
		if o := e.G.Obj(demo); o == nil || o.Zone != state.ZBattlefield {
			t.Fatal("precondition: Inkshape Demonstrator not on battlefield")
		}
		foeBolt := findAndMoveToHand(t, e, 1, "Test Bolt")
		addMana(t, e, 1, "RRR")
		sosPassPriority(t, e) // the foe gains priority and casts the bolt
		submitChoices(t, e, castOptionFor(t, e, foeBolt).Index)
		answerTargetAsk(t, e, []state.ObjID{demo})
		return e, cfg, demo
	}
	t.Run("paying lets the spell resolve", func(t *testing.T) {
		t.Parallel()
		e, cfg, demo := castWardFlow(t, 907)
		d := sosWardAsk(t, e)
		submitChoices(t, e, d.Options[0].Index) // "Pay {2}"
		passUntilStackEmpty(t, e, 20)
		if o := e.G.Obj(demo); o == nil || o.Zone != state.ZBattlefield {
			t.Fatal("ward pay: the Demonstrator should be on the battlefield")
		}
		replayCheck(t, e, cfg)
	})
	t.Run("declining counters the spell", func(t *testing.T) {
		t.Parallel()
		e, cfg, demo := castWardFlow(t, 908)
		d := sosWardAsk(t, e)
		submitChoices(t, e, d.Options[len(d.Options)-1].Index) // "Don't pay"
		passUntilStackEmpty(t, e, 20)
		if o := e.G.Obj(demo); o == nil || o.Zone != state.ZBattlefield {
			t.Fatal("ward decline: the Demonstrator should still be on the battlefield")
		}
		// CR 702.36: the spell was countered without resolving, so the warding
		// permanent is undamaged.
		if life := e.G.Players[0].Life; life != 20 {
			t.Errorf("ward decline: seat 0 life = %d, want 20", life)
		}
		replayCheck(t, e, cfg)
	})
}

// ---------------------------------------------------------------------------
// FINDING (a) Prepared (26 cards). "This creature enters prepared. (While
// it's prepared, you may cast a copy of its spell. Doing so unprepares it.)"
// The engine models only Suspected in AlterAttribute (effects/misc.go
// effAlterAttribute); Attributes$ Prepared emits a "not modelled" note and
// the cast-a-copy rider has no implementation at all.
func TestSetAudit_sos_EliteInterceptor_PreparedAttribute(t *testing.T) {
	if os.Getenv("GORGE_SET_AUDIT") == "" {
		t.Skip("set-audit finding (sos): AlterAttribute 'Prepared' is unmodelled and the cast-a-copy-of-its-spell rider does not exist. Follow-up: implement the Prepared mechanic")
	}
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 909, []string{"Elite Interceptor"}, nil, nil)
	intl := findAndMoveToHand(t, e, 0, "Elite Interceptor")
	if intl == 0 {
		t.Fatal("Elite Interceptor not found")
	}
	addMana(t, e, 0, "W")
	submitChoices(t, e, castOptionFor(t, e, intl).Index)
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(intl); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Elite Interceptor not on battlefield")
	}
	// CR-correct: the creature IS prepared — an AlterAttribute event naming
	// Prepared was emitted, and no "not modelled" note.
	sawPrepared, sawNote := false, false
	for _, ev := range e.L.Events {
		if ev.Kind == events.AlterAttribute && ev.Text == "Prepared" {
			sawPrepared = true
		}
		if ev.Kind == events.Note && strings.Contains(ev.Text, "not modelled") {
			sawNote = true
		}
	}
	if !sawPrepared {
		t.Error("prepared: no AlterAttribute(Prepared) event was emitted on ETB")
	}
	if sawNote {
		t.Error("prepared: engine emitted an unmodelled-attribute note")
	}
	replayCheck(t, e, cfg)
}

// ---------------------------------------------------------------------------
// FINDING (a) Increment (9 cards). "Whenever you cast a spell, if the amount
// of mana you spent is greater than this creature's power or toughness, put
// a +1/+1 counter on this creature." kw:Increment is unsupported
// (cards.Registry.Unsupported: kw:Increment).
func TestSetAudit_sos_PensiveProfessor_IncrementOnExpensiveCast(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 910, []string{"Pensive Professor"}, []string{sosInsightSrc}, nil)
	prof := findAndMoveToHand(t, e, 0, "Pensive Professor")
	addMana(t, e, 0, "UUU") // {1}{U}{U}
	submitChoices(t, e, castOptionFor(t, e, prof).Index)
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(prof); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Pensive Professor not on battlefield")
	}
	if got, want := e.Derived(prof).Power, int32(0); got != want {
		t.Fatalf("precondition: professor power = %d, want %d before the cast", got, want)
	}
	insight := findAndMoveToHand(t, e, 0, "Test Insight")
	libBefore := len(e.G.Zone(state.ZLibrary, 0))
	addMana(t, e, 0, "GG") // {2} — 2 mana spent > power 0
	submitChoices(t, e, castOptionFor(t, e, insight).Index)
	passUntilStackEmpty(t, e, 20)
	// CR-correct: 2 mana spent > power 0 → a +1/+1 counter (0/2 → 1/2), and
	// the "one or more +1/+1 counters are put on this creature" rider draws.
	if got := e.G.Obj(prof).Counter("P1P1"); got != 1 {
		t.Errorf("increment: +1/+1 counters = %d, want 1 (spent 2 > power 0)", got)
	}
	if got, want := len(e.G.Zone(state.ZLibrary, 0)), libBefore-1; got != want {
		t.Errorf("increment rider: library = %d, want %d (the counters-added trigger should have drawn one card)", got, want)
	}
	replayCheck(t, e, cfg)
}

// ---------------------------------------------------------------------------
// FINDING (a) Paradigm (5 cards). "Then exile this spell. After you first
// resolve a spell with this name, you may cast a copy of it from exile
// without paying its mana cost at the beginning of each of your first main
// phases." kw:Paradigm is unsupported (cards.Registry.Unsupported:
// kw:Paradigm).
func TestSetAudit_sos_RestorationSeminar_ParadigmExileAndCopy(t *testing.T) {
	if os.Getenv("GORGE_SET_AUDIT") == "" {
		t.Skip("set-audit finding (sos): kw:Paradigm is unimplemented -- no exile-after-resolve and no copy castable from exile. Follow-up: implement the Paradigm mechanic")
	}
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 911, []string{"Restoration Seminar"}, []string{sosRelicSrc}, nil)
	relic := addToGraveyard(t, e, 0, sosRelicSrc)
	seminar := findAndMoveToHand(t, e, 0, "Restoration Seminar")
	addMana(t, e, 0, "WWWWWWW") // {5}{W}{W}
	submitChoices(t, e, castOptionFor(t, e, seminar).Index)
	answerTargetAsk(t, e, []state.ObjID{relic})
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(relic); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: the relic was not returned to the battlefield")
	}
	// Paradigm: the spell exiles itself after resolving (currently it goes to
	// the graveyard), and a copy would then be castable free from exile at
	// each of the player's first main phases — the same root cause.
	if o := e.G.Obj(seminar); o == nil || o.Zone != state.ZExile {
		t.Errorf("paradigm: seminar zone = %v, want exile", o)
	}
	replayCheck(t, e, cfg)
}

// ---------------------------------------------------------------------------
// (b) MDFC corner case: casting the BACK face of a modal double-faced card.
// Tam, Observant Sequencer // Deep Sight: the back face is a sorcery
// ("You draw a card and gain 1 life"), so a hand cast must resolve the back
// face and never put a permanent onto the battlefield (CR 712.3a).
func TestSetAudit_sos_TamObservantSequencer_CastBackFace(t *testing.T) {
	if os.Getenv("GORGE_SET_AUDIT") == "" {
		t.Skip("set-audit finding (sos): the back face of AlternateMode:Prepare modal cards is never offered for casting (only 'Cast Tam, Observant Sequencer'). Follow-up: implement AlternateMode:Prepare back-face casting")
	}
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 912, []string{"Tam, Observant Sequencer // Deep Sight"}, nil, nil)
	tam := findAndMoveToHand(t, e, 0, "Tam, Observant Sequencer")
	if tam == 0 {
		t.Fatal("Tam, Observant Sequencer not in hand")
	}
	libBefore := len(e.G.Zone(state.ZLibrary, 0))
	addMana(t, e, 0, "GGUU") // enough for BOTH faces: front {2}{G}{U}, back {G}{U}
	// Pick the BACK-face cast option explicitly; if only the front face is
	// offered, that is itself the finding.
	backIdx := -1
	var offered []decision.Option
	for _, o := range castOptions(t, e) {
		if o.Obj != tam {
			continue
		}
		offered = append(offered, o)
		if strings.Contains(o.Mode, "Deep Sight") {
			backIdx = o.Index
		}
	}
	if backIdx < 0 {
		t.Fatalf("mdfc: no back-face (Deep Sight) cast option offered; options: %+v", offered)
	}
	submitChoices(t, e, backIdx)
	passUntilStackEmpty(t, e, 20)
	if life := e.G.Players[0].Life; life != 21 {
		t.Errorf("mdfc back face: life = %d, want 21 (Deep Sight gained 1 life)", life)
	}
	if got, want := len(e.G.Zone(state.ZLibrary, 0)), libBefore-1; got != want {
		t.Errorf("mdfc back face: library = %d, want %d (Deep Sight drew one card)", got, want)
	}
	replayCheck(t, e, cfg)
}

// ---------------------------------------------------------------------------
// (b) "then double the number of +1/+1 counters on that creature" (Growth
// Curve). Put 1, then double: 0 → 1 → 2 counters, never 1 or 3.
func TestSetAudit_sos_GrowthCurve_DoublesCounters(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 913, []string{"Growth Curve"}, []string{sosBearSrc}, nil)
	bear := putCreature(t, e, 0, sosBearSrc)
	if got := e.G.Obj(bear).Counter("P1P1"); got != 0 {
		t.Fatalf("precondition: bear starts with %d counters, want 0", got)
	}
	curve := findAndMoveToHand(t, e, 0, "Growth Curve")
	addMana(t, e, 0, "GU")
	submitChoices(t, e, castOptionFor(t, e, curve).Index)
	answerTargetAsk(t, e, []state.ObjID{bear})
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(bear).Counter("P1P1"); got != 2 {
		t.Errorf("growth curve: +1/+1 counters = %d, want 2 (1 added, then doubled)", got)
	}
	replayCheck(t, e, cfg)
}

// ---------------------------------------------------------------------------
// (c) Flashback (CR 702.78) — Dig Site Inventory: cast from the graveyard for
// its flashback cost, then exile (nine flashback cards in the set).
func TestSetAudit_sos_DigSiteInventory_FlashbackFromGraveyard(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 914, []string{"Dig Site Inventory"}, []string{sosBearSrc}, nil)
	bear := putCreature(t, e, 0, sosBearSrc)
	inv := findAndMoveToHand(t, e, 0, "Dig Site Inventory")
	addMana(t, e, 0, "W")
	submitChoices(t, e, castOptionFor(t, e, inv).Index)
	answerTargetAsk(t, e, []state.ObjID{bear})
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(inv); o == nil || o.Zone != state.ZGraveyard {
		t.Fatal("precondition: Dig Site Inventory not in the graveyard after the first cast")
	}
	if got := e.G.Obj(bear).Counter("P1P1"); got != 1 {
		t.Fatalf("precondition: bear +1/+1 counters = %d, want 1 after the first cast", got)
	}
	// Flashback {W}: castable from the graveyard...
	addMana(t, e, 0, "W")
	if !castOffered(e, inv) {
		t.Fatal("flashback: Dig Site Inventory not castable from the graveyard")
	}
	submitChoices(t, e, castOptionFor(t, e, inv).Index)
	answerTargetAsk(t, e, []state.ObjID{bear})
	passUntilStackEmpty(t, e, 20)
	// ...and CR 702.78b: exile it afterwards (never a second trip to the
	// graveyard).
	if o := e.G.Obj(inv); o == nil || o.Zone != state.ZExile {
		t.Errorf("flashback: inventory zone = %v, want exile", o)
	}
	if got := e.G.Obj(bear).Counter("P1P1"); got != 2 {
		t.Errorf("flashback: bear +1/+1 counters = %d, want 2 after the flashback cast", got)
	}
	replayCheck(t, e, cfg)
}

// ---------------------------------------------------------------------------
// (c) Surveil (CR 701.21) — Imperious Inkmage's ETB "surveil 2" must pose the
// arrange ask (Ruling J0) over the top two cards, and a chosen card goes to
// the graveyard.
func TestSetAudit_sos_ImperiousInkmage_SurveilArrangeAsk(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 915, []string{"Imperious Inkmage"}, nil, nil)
	ink := findAndMoveToHand(t, e, 0, "Imperious Inkmage")
	addMana(t, e, 0, "WWB") // {1}{W}{B}
	submitChoices(t, e, castOptionFor(t, e, ink).Index)
	// Wait for the ETB surveil arrange ask.
	d := e.Pending()
	for i := 0; i < 10 && (d == nil || d.Kind != decision.KArrange); i++ {
		sosPassPriority(t, e)
		d = e.Pending()
	}
	if d == nil || d.Kind != decision.KArrange {
		t.Fatalf("surveil: no KArrange ask posed, got %+v", d)
	}
	if len(d.Options) != 2 {
		t.Fatalf("surveil 2: options = %d, want 2 (the top two cards)", len(d.Options))
	}
	graveBefore := len(e.G.Zone(state.ZGraveyard, 0))
	// Keep the first looked-at card (into the graveyard), keep the second on
	// top of the library.
	submitChoices(t, e, d.Options[0].Index)
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(ink); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Imperious Inkmage not on battlefield")
	}
	if got, want := len(e.G.Zone(state.ZGraveyard, 0)), graveBefore+1; got != want {
		t.Errorf("surveil: graveyard = %d, want %d (the chosen card should have been put there)", got, want)
	}
	sawSurveil := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.Surveil {
			sawSurveil = true
		}
	}
	if !sawSurveil {
		t.Error("surveil: no Surveil event was emitted")
	}
	replayCheck(t, e, cfg)
}

// ---------------------------------------------------------------------------
// FINDING (census) — the cards cards.Registry.Unsupported still names. Each
// entry here is a root-cause gap: kw:Increment (9 sos cards), kw:Paradigm
// (5 sos cards), api:SkipTurn (Ral Zarek's [-7] "target opponent skips their
// next X turns"), stat:CantBeCopied (Choreographed Sparks), and
// count:PlayerCountRemembered$Valid (Pox Plague).
func TestSetAudit_sos_CensusLevelGaps(t *testing.T) {
	if os.Getenv("GORGE_SET_AUDIT") == "" {
		t.Skip("set-audit finding (sos): 17 sos cards still name missing primitives (kw:Increment x9, kw:Paradigm x5, api:SkipTurn, stat:CantBeCopied, count:PlayerCountRemembered$Valid). Follow-up: close the sos census gaps")
	}
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	supported := effects.Supported()
	names := []string{
		// kw:Increment
		"Pensive Professor", "Tester of the Tangential", "Textbook Tabulator",
		"Ambitious Augmenter", "Hungry Graffalon", "Topiary Lecturer",
		"Berta, Wise Extrapolator", "Cuboid Colony", "Fractal Tender",
		// kw:Paradigm
		"Restoration Seminar", "Echocasting Symposium", "Decorum Dissertation",
		"Improvisation Capstone", "Germination Practicum",
		// single-card gaps
		"Ral Zarek, Guest Lecturer", "Choreographed Sparks", "Pox Plague",
	}
	for _, name := range names {
		c := sosCard(t, name)
		if m := reg.Unsupported(c, supported); len(m) > 0 {
			t.Errorf("%s still needs %v", name, m)
		}
	}
}
