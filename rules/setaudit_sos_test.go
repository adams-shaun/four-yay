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
	"reflect"
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
const sosSpriteSrc = "Name:Test Sprite\nManaCost:1 U\nTypes:Creature Faerie\nPT:1/1\nK:Flying\nOracle:x\n"
const sosElfSrc = "Name:Test Elf\nManaCost:G\nTypes:Creature Elf\nPT:1/1\nOracle:x\n"
const sosOgreSrc = "Name:Test Ogre\nManaCost:2 R\nTypes:Creature Ogre\nPT:3/3\nOracle:x\n"
const sosCubSrc = "Name:Test Cub\nManaCost:0\nTypes:Artifact\nOracle:x\n"
const sosBoltSrc = "Name:Test Bolt\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1\nOracle:x\n"
const sosHeavyBoltSrc = "Name:Test Heavy Bolt\nManaCost:2 R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 2\nOracle:x\n"
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
		case decision.KTriggerOptional:
			// Accept the optional trigger (its "yes" election) so a "you may
			// search" ETB resolves inside the drain.
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "yes" {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("drain: trigger_optional ask with no yes option: %+v", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
				t.Fatalf("submit drain trigger_optional yes: %v", err)
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
// (a) Ward (CR 702.21a) — Inkshape Demonstrator: Ward {2}. The targeting
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
		} else if o.Damage != 1 {
			t.Errorf("ward pay: demonstrator damage = %d, want 1 (paid Bolt must resolve)", o.Damage)
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
		// CR 702.21a: the spell was countered without resolving, so the
		// targeted creature, not just its controller, must be undamaged.
		if got := e.G.Obj(demo).Damage; got != 0 {
			t.Errorf("ward decline: demonstrator damage = %d, want 0 (unpaid Bolt countered)", got)
		}
		replayCheck(t, e, cfg)
	})
}

// ---------------------------------------------------------------------------
// FINDING (a) Prepared (26 cards). "This creature enters prepared. (While
// it's prepared, you may cast a copy of its spell. Doing so unprepares it.)"
// The engine models only Suspected in AlterAttribute (effects/misc.go
// effAlterAttribute); Attributes$ Prepared emits a "not modelled" note and
// the cast-a-copy rider has no implementation at all. CR 722.3a-c.
func TestSetAudit_sos_EliteInterceptor_PreparedAttribute(t *testing.T) {
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

// CR 722.3c: preparation exiles a copy with ONLY the inset spell's
// characteristics. Its controller can cast it; the creature becomes
// unprepared at cast time, not when the spell resolves. This tests the
// defining rider independently of the ETB attribute event above.
func TestSetAudit_sos_EliteInterceptor_PreparedSpellCopyUnprepares(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 916, []string{"Elite Interceptor // Rejoinder"}, []string{sosBearSrc}, nil)
	intl := findAndMoveToHand(t, e, 0, "Elite Interceptor")
	addMana(t, e, 0, "W")
	submitChoices(t, e, castOptionFor(t, e, intl).Index)
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(intl); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Interceptor must enter the battlefield, got %+v", o)
	}
	bear := putCreature(t, e, 0, sosBearSrc)
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: copy has no legal creature target")
	}
	// putCreature's setup MoveZone clears pending; resume priority before
	// inspecting the cast options for the exiled copy.
	e.priorityRound()
	// At sorcery timing a prepared Interceptor must have an exiled Rejoinder
	// copy available for casting; a bare ability Note cannot satisfy this.
	var copyID state.ObjID
	for _, id := range e.G.Zone(state.ZExile, 0) {
		if o := e.G.Obj(id); o != nil && o.IsCopy && o.Face() != nil && o.Face().Name == "Rejoinder" {
			copyID = id
		}
	}
	if copyID == 0 {
		t.Fatal("prepared copy: no Rejoinder spell copy in exile after Interceptor enters (CR 722.3c)")
	}
	// The copy is cast, not cast free: Rejoinder's {1}{W} is paid (CR 601.2f).
	addMana(t, e, 0, "WW")
	idx := -1
	for _, opt := range castOptions(t, e) {
		if opt.Obj == copyID {
			idx = opt.Index
		}
	}
	if idx < 0 {
		t.Fatalf("prepared copy: exiled Rejoinder copy %d has no cast option", copyID)
	}
	libBefore := len(e.G.Zone(state.ZLibrary, 0))
	submitChoices(t, e, idx)
	// Casting has put the copy on the stack, but priority has not passed and
	// the spell has not resolved. CR 601.2i / 722.3c require unpreparing as
	// part of the cast, not as a later resolution or zone-change consequence.
	if o := e.G.Obj(copyID); o == nil || o.Zone != state.ZStack {
		t.Fatalf("prepared copy: after cast choice, copy zone = %v, want stack before resolution", zoneOf(o))
	}
	if o := e.G.Obj(intl); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("prepared copy: Interceptor left the battlefield during casting")
	}
	unpreparedAtCast := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.AlterAttribute && ev.Obj == intl && ev.Text == "Prepared" && ev.Amount < 0 {
			unpreparedAtCast = true
		}
	}
	if !unpreparedAtCast {
		t.Fatal("prepared copy: no removal of Interceptor's Prepared designation immediately after casting, before resolution")
	}
	sosDrain(t, e, bear, 30)
	if o := e.G.Obj(intl); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("prepared copy: Interceptor left the battlefield unexpectedly")
	}
	if got := len(e.G.Zone(state.ZLibrary, 0)); got != libBefore-1 {
		t.Errorf("prepared copy: library has %d cards, want %d (Rejoinder must draw)", got, libBefore-1)
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
	// each of the player's first main phases — the same root cause (CR 702.192a).
	if o := e.G.Obj(seminar); o == nil || o.Zone != state.ZExile {
		t.Errorf("paradigm: seminar zone = %v, want exile", o)
	}
	replayCheck(t, e, cfg)
}

// CR 702.192a: after the first successful resolution, EACH precombat main
// phase creates a copy of the exiled Lesson which may be cast for free.
// This separate test keeps the delayed-copy half from being masked by the
// missing exile-on-resolution assertion in the test above.
func TestSetAudit_sos_RestorationSeminar_ParadigmNextMainCopyCast(t *testing.T) {
	if os.Getenv("GORGE_SET_AUDIT") == "" {
		t.Skip("set-audit finding (sos): Paradigm never creates a free copy at the next first main phase. Follow-up: implement the Paradigm mechanic")
	}
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 917, []string{"Restoration Seminar"}, []string{sosRelicSrc, sosRelicSrc}, nil)
	first := addToGraveyard(t, e, 0, sosRelicSrc)
	seminar := findAndMoveToHand(t, e, 0, "Restoration Seminar")
	addMana(t, e, 0, "WWWWWWW")
	submitChoices(t, e, castOptionFor(t, e, seminar).Index)
	answerTargetAsk(t, e, []state.ObjID{first})
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(first); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: original Seminar did not resolve, relic zone = %+v", o)
	}
	if o := e.G.Obj(seminar); o == nil || o.Zone != state.ZExile {
		t.Errorf("paradigm prerequisite: resolved seminar not exiled (zone %v, CR 702.192a)", zoneOf(o))
	}
	// Leave a second legal graveyard target for the later copy. No mana is
	// added for the copy: it must be cast without paying its mana cost.
	second := addToGraveyard(t, e, 0, sosRelicSrc)
	if second == first || e.G.Obj(second).Zone != state.ZGraveyard {
		t.Fatalf("precondition: second relic must be in graveyard, ids %d/%d", first, second)
	}
	// moveSeeded clears pending after its setup MoveZone. Resume the ordinary
	// priority loop before advancing time; otherwise driveToStep sees nil.
	e.priorityRound()
	// A two-player game starts with seat 0 on turn 1; its next precombat
	// main is turn 3. Stop at entry before passing away the optional ask.
	driveToStep(t, e, 3, 0, state.StepMain1)
	if e.G.Active != 0 || e.G.Step != state.StepMain1 {
		t.Fatal("precondition: did not arrive at seat 0's next first main phase")
	}
	var copyID state.ObjID
	for i := 0; i < 12 && copyID == 0 && e.G.Step == state.StepMain1; i++ {
		for _, id := range e.G.Zone(state.ZExile, 0) {
			if o := e.G.Obj(id); o != nil && o.IsCopy && o.Face() != nil && o.Face().Name == "Restoration Seminar" {
				copyID = id
			}
		}
		if copyID == 0 {
			sosPassPriority(t, e)
		}
	}
	if copyID == 0 {
		t.Fatal("paradigm next main: no free Restoration Seminar copy created in exile (CR 702.192a)")
	}
	d := e.Pending()
	if d == nil {
		t.Fatal("paradigm next main: no pending decision to cast the copy")
	}
	castIdx := -1
	for _, opt := range d.Options {
		if opt.Obj == copyID && (opt.Kind == "cast" || strings.Contains(strings.ToLower(opt.Label), "cast")) {
			castIdx = opt.Index
		}
	}
	if castIdx < 0 {
		t.Fatalf("paradigm next main: copy %d not offered for free cast: %+v", copyID, d)
	}
	submitChoices(t, e, castIdx)
	answerTargetAsk(t, e, []state.ObjID{second})
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(second); o == nil || o.Zone != state.ZBattlefield {
		t.Errorf("paradigm next main: copied Seminar did not return second relic; got %+v", o)
	}
	replayCheck(t, e, cfg)
}

// ---------------------------------------------------------------------------
// (b) Preparation-card corner case (CR 722.3): the inset prepare spell is
// NOT a castable MDFC back face. It can only be cast as a copy made in exile
// by the prepared permanent (tested above).
func TestSetAudit_sos_TamObservantSequencer_PrepareSpellNotCastableFromHand(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 912, []string{"Tam, Observant Sequencer // Deep Sight"}, nil, nil)
	tam := findAndMoveToHand(t, e, 0, "Tam, Observant Sequencer")
	if tam == 0 {
		t.Fatal("Tam, Observant Sequencer not in hand")
	}
	addMana(t, e, 0, "GGUU") // enough for both printed mana costs
	frontOffered := false
	for _, o := range castOptions(t, e) {
		if o.Obj != tam {
			continue
		}
		if strings.Contains(o.Mode, "Deep Sight") || strings.Contains(o.Label, "Deep Sight") {
			t.Errorf("prepare spell: Deep Sight offered from hand: %+v (CR 722.3)", o)
		}
		frontOffered = true
	}
	if !frontOffered {
		t.Fatal("precondition: Tam's ordinary front-face cast was not offered")
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
// Census ONLY (not a behavioural test): cards.Registry.Unsupported still
// names these missing primitives. Each entry here is a root-cause gap:
// count:PlayerCountRemembered$Valid (Pox Plague).
//
// stat:CantBeCopied (Choreographed Sparks) used to be a row here; it is
// implemented now (effects/copy.go enforces it on stack copies) and its
// behaviour lives in rules/cantbecopied_test.go.
//
// kw:Increment (Pensive Professor, Tester of the Tangential, Textbook
// Tabulator, Ambitious Augmenter, Hungry Graffalon, Topiary Lecturer, Berta
// Wise Extrapolator, Cuboid Colony, Fractal Tender) used to be a row here; it
// is implemented now (cards/kw_increment.go, a spellcast counter trigger) and
// its behaviour lives in rules/increment_test.go.
//
// kw:Paradigm (Restoration Seminar, Echocasting Symposium, Decorum
// Dissertation, Improvisation Capstone, Germination Practicum) used to be a
// row here; it is implemented now (rules/paradigm.go) and its census lives in
// rules/paradigm_test.go's TestParadigmCensus.
func TestSetAudit_sos_CensusLevelGaps(t *testing.T) {
	if os.Getenv("GORGE_SET_AUDIT") == "" {
		t.Skip("set-audit finding (sos): 1 sos card still names a missing primitive (count:PlayerCountRemembered$Valid). Follow-up: close the sos census gaps")
	}
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	supported := effects.Supported()
	names := []string{
		// single-card gap
		"Pox Plague",
	}
	for _, name := range names {
		c := sosCard(t, name)
		if m := reg.Unsupported(c, supported); len(m) > 0 {
			t.Errorf("%s still needs %v", name, m)
		}
	}
}

// ---------------------------------------------------------------------------
// ===== Part 2: the 20 keywords part 1 did not touch, and passes B/C. =====
//
// Combat keywords below use the combat harness (combat_test.go helpers): the
// sos carrier is placed on the battlefield as a real corpus card and the
// assertion is the combat outcome (who is blockable, where damage lands, what
// life moves), never a HasKeyword lookup. Eventless onBoardCard placement is
// the file's fixture convention (onBoard's eventless note), so these tests do
// not replayCheck — exactly like every test in combat_test.go itself.

// sosBlockOptions returns the pending KBlockers options naming `want`.
func sosBlockOptions(t *testing.T, e *Engine, want state.ObjID) []decision.Option {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KBlockers {
		t.Fatalf("expected a blockers decision, got %+v", d)
	}
	var out []decision.Option
	for _, o := range d.Options {
		if o.Obj == want {
			out = append(out, o)
		}
	}
	return out
}

// sosAttackOption reports whether the pending KAttackers decision offers an
// attack option for the given attacker.
func sosAttackOption(t *testing.T, e *Engine, id state.ObjID) *decision.Option {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected an attackers decision, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Obj == id {
			return &o
		}
	}
	return nil
}

// (a) Deathtouch (CR 702.2b): "any amount of damage ... is enough to destroy
// it." Noxious Newt (1/2 deathtouch) vs a 3/3: the ONE damage must kill the
// 3/3, which would survive ordinary 1 damage. Also asserts deathtouch
// damage follows the ordinary damage step (the ogre's 3 kills the 1/2 newt).
func TestSetAudit_sos_NoxiousNewt_DeathtouchLethalDamage(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	newt := onBoardReadyCard(t, e, 0, sosCard(t, "Noxious Newt"))
	ogre := onBoard(t, e, 1, sosOgreSrc)
	if o := e.G.Obj(ogre); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Test Ogre not on the battlefield")
	}
	e.askAttackers()
	submitAttackersOnly(t, e, newt)
	drainCombatPriority(t, e)
	submitBlockersOnly(t, e, ogre)
	drainCombatPriority(t, e)
	if o := e.G.Obj(ogre); o == nil || o.Zone != state.ZGraveyard {
		t.Errorf("deathtouch: blocker zone = %+v, want the graveyard (1 deathtouch damage kills a 3/3, CR 702.2b)", o)
	}
	if o := e.G.Obj(newt); o == nil || o.Zone != state.ZGraveyard {
		t.Errorf("deathtouch: the 1/2 Newt should have died to 3 damage, got %+v", o)
	}
}

// (a) First strike (CR 702.7b): Honorbound Page (3/3 first strike) vs a 3/3.
// Without first strike both die to simultaneous damage; with it, the ogre
// dies in the first-strike step and never deals its 3.
func TestSetAudit_sos_HonorboundPage_FirstStrikeDamageOrder(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	page := onBoardReadyCard(t, e, 0, sosCard(t, "Honorbound Page"))
	ogre := onBoard(t, e, 1, sosOgreSrc)
	e.askAttackers()
	submitAttackersOnly(t, e, page)
	drainCombatPriority(t, e)
	submitBlockersOnly(t, e, ogre)
	drainCombatPriority(t, e)
	drainCombatDamagePriority(t, e)
	if o := e.G.Obj(ogre); o == nil || o.Zone != state.ZGraveyard {
		t.Errorf("first strike: blocker zone = %+v, want the graveyard", o)
	}
	nd := e.G.Obj(page)
	if nd == nil || nd.Zone != state.ZBattlefield || nd.Damage != 0 {
		t.Errorf("first strike: page zone/damage = %+v/%d, want battlefield/0 (the 3/3 must die in the first-strike step before its regular damage, CR 702.7b)", nd, pageDamage(nd))
	}
}

// (a) Double strike (CR 702.4b): Quill-Blade Laureate (1/1 double strike) vs
// a 2/2. The first-strike pass deals 1 (bear alive, damaged); the regular
// pass deals the second 1 (bear dead). The 1/1 laureate necessarily dies to
// the bear's regular-step 2, so the split is asserted on the blocker.
func TestSetAudit_sos_QuillBladeLaureate_DoubleStrikeTwoSteps(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	laureate := onBoardReadyCard(t, e, 0, sosCard(t, "Quill-Blade Laureate"))
	bear := onBoard(t, e, 1, sosBearSrc)
	e.askAttackers()
	submitAttackersOnly(t, e, laureate)
	drainCombatPriority(t, e)
	submitBlockersOnly(t, e, bear)
	drainCombatPriority(t, e)
	// Mid-boundary, at the CR 510.4 between-passes window: the first-strike
	// pass has dealt 1 and the regular pass has not run yet.
	if e.G.Step != state.StepCombatDamage || e.Pending() == nil || e.Pending().Kind != decision.KPriority {
		t.Fatalf("expected the between-passes priority window in the combat damage step, got step %s pending %+v", e.G.Step, e.Pending())
	}
	if bo := e.G.Obj(bear); bo == nil || bo.Zone != state.ZBattlefield || bo.Damage != 1 {
		t.Errorf("double strike: at the between-passes window the bear should be on the battlefield with 1 damage, got zone %+v damage %+v", bo, pageDamage(bo))
	}
	drainCombatDamagePriority(t, e)
	if bo := e.G.Obj(bear); bo == nil || bo.Zone != state.ZGraveyard {
		t.Errorf("double strike: the bear should die to the second (regular) strike, got %+v", bo)
	}
	if lo := e.G.Obj(laureate); lo == nil || lo.Zone != state.ZGraveyard {
		t.Errorf("double strike: the 1/1 laureate should have died to the bear's regular-step 2, got %+v", lo)
	}
}

// (a) Flying (CR 702.9b): Owlin Historian attacks. A ground creature is NOT
// offered as a blocker; a flying creature IS.
func TestSetAudit_sos_OwlinHistorian_FlyingBlocksGroundUnblockable(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	historian := onBoardReadyCard(t, e, 0, sosCard(t, "Owlin Historian"))
	bear := onBoard(t, e, 1, sosBearSrc)     // no flying, no reach
	sprite := onBoard(t, e, 1, sosSpriteSrc) // flying
	e.askAttackers()
	submitAttackersOnly(t, e, historian)
	drainCombatPriority(t, e)
	if opts := sosBlockOptions(t, e, bear); len(opts) != 0 {
		t.Fatalf("flying: a ground creature was offered against a flying attacker: %+v (CR 702.9b)", opts)
	}
	if opts := sosBlockOptions(t, e, sprite); len(opts) == 0 {
		t.Fatalf("flying: the flying blocker was not offered against the flying attacker: %+v", e.Pending().Options)
	}
	submitBlockersOnly(t, e, sprite)
	drainCombatPriority(t, e)
	if o := e.G.Obj(sprite); o == nil || o.Zone != state.ZGraveyard {
		t.Errorf("flying: the 1/1 flying blocker should die to the 2/3 attacker, got %+v", o)
	}
	if o := e.G.Obj(historian); o == nil || o.Zone != state.ZBattlefield || o.Damage != 1 {
		t.Errorf("flying: the historian should have taken the sprite's 1 damage on the battlefield, got %+v", o)
	}
}

// (a) Reach (CR 702.11b): the reach creature's outcome lives on the BLOCK
// side, so the flying Sprite attacks on seat 1's turn and seat 0 defends:
// the Rearing Embermare (4/5 reach) blocks the flying attacker, the ground
// bear's declaration is rejected.
func TestSetAudit_sos_RearingEmbermare_ReachBlocksFlying(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	mare := onBoardCard(t, e, 0, sosCard(t, "Rearing Embermare"))
	bear := onBoard(t, e, 0, sosBearSrc)
	sprite := onBoard(t, e, 1, sosSpriteSrc)
	driveToStepAll(t, e, 2, 1, state.StepDeclareAttackers)
	e.askAttackers()
	submitAttackersOnly(t, e, sprite)
	drainCombatPriority(t, e)
	// The blockers ask itself enforces CR 702.11b: the ground bear is NOT
	// offered against the flying attacker; the reach embermare IS.
	if opts := sosBlockOptions(t, e, bear); len(opts) != 0 {
		t.Fatalf("reach: a ground creature was offered against a flying attacker (CR 702.11b)")
	}
	submitBlockersOnly(t, e, mare)
	drainCombatPriority(t, e)
	if o := e.G.Obj(sprite); o == nil || o.Zone != state.ZGraveyard {
		t.Errorf("reach: the 1/1 flying attacker should die to the 4/5 reach blocker, got %+v", o)
	}
	if o := e.G.Obj(mare); o == nil || o.Zone != state.ZBattlefield || o.Damage != 1 {
		t.Errorf("reach: the embermare should have taken the sprite's 1 damage, got %+v", o)
	}
}

// (a) Trample (CR 702.19c): Quandrix, the Proof (6/6 trample, flying) blocked
// by a 1/1 flying Sprite: 1 lethal damage to the blocker, the remaining 5 to
// the defender.
func TestSetAudit_sos_QuandrixTheProof_TrampleOverkillToPlayer(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	proof := onBoardReadyCard(t, e, 0, sosCard(t, "Quandrix, the Proof"))
	// The Proof is also FLYING, so the trample-over blocker is the 1/1
	// flying Sprite (the only legal blocker the foe owns here).
	flyer := onBoard(t, e, 1, sosSpriteSrc)
	e.askAttackers()
	submitAttackersOnly(t, e, proof)
	drainCombatPriority(t, e)
	submitBlockersOnly(t, e, flyer)
	drainCombatPriority(t, e)
	if o := e.G.Obj(flyer); o == nil || o.Zone != state.ZGraveyard {
		t.Errorf("trample: the 1/1 blocker should die, got %+v", o)
	}
	if life := e.G.Players[1].Life; life != 15 {
		t.Errorf("trample: defender life = %d, want 15 (5 of 6 damage assigned past the 1/1 blocker, CR 702.19c)", life)
	}
	if o := e.G.Obj(proof); o == nil || o.Zone != state.ZBattlefield || o.Damage != 1 {
		t.Errorf("trample: attacker should survive with 1 damage, got %+v", o)
	}
}

// (a) Haste (CR 302.6 / 702.10a): Charging Strifeknight attacks the turn it
// enters despite summoning sickness; a sick non-haste creature does not.
func TestSetAudit_sos_ChargingStrifeknight_HasteAttacksWhileSick(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	knight := onBoardCard(t, e, 0, sosCard(t, "Charging Strifeknight"))
	elf := onBoard(t, e, 0, sosElfSrc)
	// The sickness preconditions are the point: both are freshly placed and
	// summoning sick, and only the haste creature is offered an attack.
	if !e.G.Obj(knight).SummonSick || !e.G.Obj(elf).SummonSick {
		t.Fatal("precondition: both creatures must be summoning sick for this comparison")
	}
	e.askAttackers()
	if sosAttackOption(t, e, knight) == nil {
		t.Errorf("haste: the summoning-sick Strifeknight was not offered an attack (CR 702.10a)")
	}
	if sosAttackOption(t, e, elf) != nil {
		t.Errorf("control: the summoning-sick non-haste elf WAS offered an attack (CR 302.6)")
	}
}

// (a) Lifelink (CR 702.15a): Shattered Acolyte (2/2 lifelink) blocked by a
// 1/1: the controller gains life equal to the damage dealt.
func TestSetAudit_sos_ShatteredAcolyte_LifelinkGainsDamageDealt(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	acolyte := onBoardReadyCard(t, e, 0, sosCard(t, "Shattered Acolyte"))
	elf := onBoard(t, e, 1, sosElfSrc)
	if life := e.G.Players[0].Life; life != 20 {
		t.Fatalf("precondition: seat 0 life = %d, want 20", life)
	}
	e.askAttackers()
	submitAttackersOnly(t, e, acolyte)
	drainCombatPriority(t, e)
	submitBlockersOnly(t, e, elf)
	drainCombatPriority(t, e)
	if o := e.G.Obj(elf); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("precondition: the 1/1 blocker should have died to the 2/2, got %+v", o)
	}
	if life := e.G.Players[0].Life; life != 22 {
		t.Errorf("lifelink: seat 0 life = %d, want 22 (gains life equal to the 2 damage dealt, CR 702.15a)", life)
	}
}

// (a) Menace (CR 702.31b): Ulna Alley Shopkeep (2/3 menace) cannot be blocked
// by one creature; two can. Blocked by two 2/2s, the shopkeep deals 1+1 to
// the bears and dies to their 4; a single-blocker declaration is rejected.
func TestSetAudit_sos_UlnaAlleyShopkeep_MenaceNeedsTwoBlockers(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	shopkeep := onBoardReadyCard(t, e, 0, sosCard(t, "Ulna Alley Shopkeep"))
	bear1 := onBoard(t, e, 1, sosBearSrc)
	bear2 := onBoard(t, e, 1, sosBearSrc)
	e.askAttackers()
	submitAttackersOnly(t, e, shopkeep)
	drainCombatPriority(t, e)
	// The single-blocker candidate is still listed (its option carries
	// MinBlockers 2), so the CR-correct outcome is enforced at validation:
	// the one-creature declaration is rejected, the two-creature one accepted.
	if d := e.Pending(); d == nil || d.Kind != decision.KBlockers {
		t.Fatalf("expected a blockers decision, got %+v", d)
	}
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Obj == bear1 {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("precondition: single-blocker option missing; options %+v", d.Options)
	}
	for _, o := range d.Options {
		if o.Obj == bear1 && o.MinBlockers != 2 {
			t.Errorf("menace: the single-blocker option does not carry the CR 702.31b two-blocker requirement (MinBlockers=%d)", o.MinBlockers)
		}
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err == nil {
		t.Errorf("menace: a one-creature block of a menace attacker was ACCEPTED (CR 702.31b: cannot be blocked except by two or more)")
	}
	// The rejected whole-declaration sends the engine back to the CR 509.2
	// priority window; pass it so the blockers ask is re-posed.
	drainCombatPriority(t, e)
	submitBlockersOnly(t, e, bear1, bear2)
	drainCombatPriority(t, e)
	// Two live blockers pose the attacker's controller a CR 510.1c
	// damage-division decision; the only legal split here is 1+1.
	submitDivision(t, e, []int32{1, 1})
	drainCombatDamagePriority(t, e)
	// The damage pass's SBA tail is deferred for the end-of-combat priority
	// round; pass it so the lethal damage on the 1/1s resolves (the helper
	// below is the declare-window drain, so settle by hand here).
	sosPassPendingPriority(t, e)
	so := e.G.Obj(shopkeep)
	// events.Move clears Damage the instant something dies, so the lethal
	// total is read off DamageReceivedThisTurn (the combat_test.go idiom).
	if so == nil || so.Zone != state.ZGraveyard || so.DamageReceivedThisTurn != 4 {
		t.Errorf("menace: shopkeep zone/total damage = %+v/%d, want graveyard/4 (the two 2/2s deal 4 to the 2/3)", so, soDamageTaken(so))
	}
	for _, id := range []state.ObjID{bear1, bear2} {
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZBattlefield || o.Damage != 1 {
			t.Errorf("menace: bear %d should have taken the shopkeep's 1 of its 2 damage on the battlefield, got %+v", id, o)
		}
	}
}

// (a) Vigilance (CR 702.3b): Transcendent Archaic attacks WITHOUT tapping; a
// plain creature still taps when it attacks.
func TestSetAudit_sos_TranscendentArchaic_VigilanceDoesNotTap(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	archaic := onBoardReadyCard(t, e, 0, sosCard(t, "Transcendent Archaic"))
	bear := onBoardReady(t, e, 0, sosBearSrc)
	elf := onBoard(t, e, 1, sosElfSrc)
	e.askAttackers()
	submitAttackersOnly(t, e, archaic, bear)
	drainCombatPriority(t, e)
	submitBlockersOnly(t, e, elf)
	drainCombatPriority(t, e)
	if ao := e.G.Obj(archaic); ao == nil || ao.Tapped {
		t.Errorf("vigilance: the archaic zone/tapped = %+v/%v, want tapped false — it must attack without tapping (CR 702.3b)", ao, ao != nil && ao.Tapped)
	}
	if o := e.G.Obj(bear); o == nil || !o.Tapped {
		t.Errorf("control: the plain bear must tap when it attacks (CR 702.3b contrast)")
	}
}

// (a) Double ("then double the number of +1/+1 counters", CR-correct
// arithmetic): a bear that ALREADY has 2 counters goes to 6 (put 1 → 3, then
// double), not to 4 (double-then-put) or 5 (put only). Part 1 pinned 0→2;
// this pins the doubling against a non-zero base.
func TestSetAudit_sos_GrowthCurve_DoublesOnExistingCounters(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 920, []string{"Growth Curve", "Growth Curve"}, []string{sosBearSrc}, nil)
	bear := putCreature(t, e, 0, sosBearSrc)
	curve := findAndMoveToHand(t, e, 0, "Growth Curve")
	// First cast: 0 → 1 → 2 (the part-1 shape).
	addMana(t, e, 0, "GU")
	submitChoices(t, e, castOptionFor(t, e, curve).Index)
	answerTargetAsk(t, e, []state.ObjID{bear})
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(bear).Counter("P1P1"); got != 2 {
		t.Fatalf("precondition: after the first cast the bear has %d counters, want 2", got)
	}
	// Second cast: 2 → 3 (put) → 6 (double).
	curve = findAndMoveToHand(t, e, 0, "Growth Curve")
	addMana(t, e, 0, "GU")
	submitChoices(t, e, castOptionFor(t, e, curve).Index)
	answerTargetAsk(t, e, []state.ObjID{bear})
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(bear).Counter("P1P1"); got != 6 {
		t.Errorf("double: counters = %d, want 6 (2 existing, put 1 to 3, then doubled)", got)
	}
	replayCheck(t, e, cfg)
}

// pageDamage reports the damage field of an object or -1 if it is gone, for
// zone-or-damage assertions that must not dereference nil.
func pageDamage(o *state.Object) int32 {
	if o == nil {
		return -1
	}
	return o.Damage
}

// soDamageTaken reports an object's DamageReceivedThisTurn, or -1 if it is
// gone, for dead-or-damaged assertions (Damage is cleared on death).
func soDamageTaken(o *state.Object) int32 {
	if o == nil {
		return -1
	}
	return o.DamageReceivedThisTurn
}

// sosPassPendingPriority submits "pass" for whatever priority decision is
// pending, wherever it is (end-of-combat SBA-tail windows live outside the
// declare steps drainCombatPriority covers).
func sosPassPendingPriority(t *testing.T, e *Engine) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		return
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "pass" {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("priority decision with no pass option: %+v", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("pass pending priority: %v", err)
	}
}

// sosPreparedCopy finds the exile-resident prepared copy named name (CR
// 722.3c: the inset spell is copied into exile when the creature enters).
func sosPreparedCopy(e *Engine, name string) state.ObjID {
	for _, id := range e.G.Zone(state.ZExile, 0) {
		if o := e.G.Obj(id); o != nil && o.IsCopy && o.Face() != nil && o.Face().Name == name {
			return id
		}
	}
	return 0
}

// sosPreparedCastOption returns the pending decision's cast option for the
// exile-resident prepared copy (CR 722.3c), matched by object, and whether
// it is offered at all.
func sosPreparedCastOption(e *Engine, copy state.ObjID) (decision.Option, bool) {
	d := e.Pending()
	if d == nil {
		return decision.Option{}, false
	}
	for _, o := range d.Options {
		if o.Obj == copy {
			return o, true
		}
	}
	return decision.Option{}, false
}

// sosMilledIntoGraveyard returns the non-token, non-source card the tested
// mill left in seat 0's graveyard (the library was Mountains otherwise).
func sosMilledIntoGraveyard(e *Engine, excluding state.ObjID) state.ObjID {
	for _, id := range e.G.Zone(state.ZGraveyard, 0) {
		if o := e.G.Obj(id); o != nil && id != excluding && !o.IsToken {
			return id
		}
	}
	return 0
}

// (a) Affinity (CR 702.41a), Witherbloom, the Balancer. Two halves: the
// dragon's own printed affinity reduces its cast by the creatures you
// (a) Affinity, SELF half (CR 702.41a), Witherbloom, the Balancer: the
// dragon's own printed K:Affinity:Creature reduces its cast by one per
// creature you control, and a plain bolt is not discounted while the grant
// source is not yet on the battlefield. Cost proof via the engine's own
// costModifiers oracle (reduceOf, the cost_modifier_parity_test oracle).
func TestSetAudit_sos_WitherbloomBalancer_AffinitySelf(t *testing.T) {
	t.Parallel()
	e, _, _ := altCostEngine(t, 930, []string{"Witherbloom, the Balancer"}, []string{sosElfSrc, sosElfSrc, sosBoltSrc}, nil)
	toMain1(t, e)
	balancer := findAndMoveToHand(t, e, 0, "Witherbloom, the Balancer")
	bolt := findAndMoveToHand(t, e, 0, "Test Bolt")
	if balancer == 0 || bolt == 0 {
		t.Fatalf("precondition: balancer %d bolt %d must open in hand", balancer, bolt)
	}
	putCreature(t, e, 0, sosElfSrc)
	putCreature(t, e, 0, sosElfSrc)
	e.priorityRound()
	// Self-affinity: two creatures on the battlefield reduce the dragon's
	// {6} by 2. The dragon itself is not yet on the battlefield and must
	// not count for its own cast.
	if got := reduceOf(t, e, 0, balancer); got != 2 {
		t.Errorf("affinity: the balancer's own reduction = %d, want 2 (two creatures you control, CR 702.41a)", got)
	}
	// Baseline: the plain bolt is not discounted.
	if got := reduceOf(t, e, 0, bolt); got != 0 {
		t.Errorf("affinity: the bolt was reduced %d without the balancer on the battlefield, want 0", got)
	}
}

// (a) Affinity, GRANT half (CR 702.41a), Witherbloom, the Balancer: its
// continuous static (Affected$ Instant.wasCastByYou,Sorcery.wasCastByYou |
// AffectedZone$ Stack | AddKeyword$ Affinity:Creature) must give a bolt on
// the stack the affinity reduction (the dragon's own printed K:Affinity
// prices its own casts through the face static kwAffinity mints; this is
// the GRANTED half, delivered by the layer-6 AddKeyword$ walk). Fixed in
// rules/statics.go's appendEffectCostStatics, which synthesizes the
// ReduceCost static the printed expander would have minted and binds it to
// the grant's hosts. Pinned on the parity oracle (reduceOf) AND on the real
// offer flow: {2}{R} is castable for {R} alone.
func TestSetAudit_sos_WitherbloomBalancer_AffinityGrantStack(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 942, []string{"Witherbloom, the Balancer"}, []string{sosElfSrc, sosBoltSrc, sosHeavyBoltSrc}, nil)
	toMain1(t, e)
	bolt := findAndMoveToHand(t, e, 0, "Test Bolt")
	if bolt == 0 {
		t.Fatalf("precondition: bolt %d must open in hand", bolt)
	}
	heavy := findAndMoveToHand(t, e, 0, "Test Heavy Bolt")
	if heavy == 0 {
		t.Fatal("precondition: the heavy bolt must open in hand")
	}
	putCreature(t, e, 0, sosElfSrc)
	if balID := searchMoveByName(t, e, "Witherbloom, the Balancer", state.ZBattlefield); balID == 0 {
		t.Fatal("precondition: the balancer was not found in the library")
	} else if o := e.G.Obj(balID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: the balancer is not on the battlefield")
	}
	e.priorityRound()
	if sosHandCardByName(e, 0, "Witherbloom, the Balancer") != 0 {
		t.Fatal("precondition: the balancer is still in hand")
	}
	if got := reduceOf(t, e, 0, bolt); got != 2 {
		t.Errorf("affinity grant: the bolt's reduction = %d, want 2 (elf + the dragon itself, CR 702.41a via the granted keyword)", got)
	}
	// The reduction is real money on the offer: {2}{R} castable for {R}
	// alone (two creatures you control take {2} off), and no other mana in
	// the pool.
	addMana(t, e, 0, "R")
	opt := castOptionFor(t, e, heavy)
	submitChoices(t, e, opt.Index)
	sosAnswerPlayerTarget(t, e, 1)
	if o := e.G.Obj(heavy); o == nil || o.Zone != state.ZStack {
		t.Fatalf("the heavy bolt is not on the stack after the cast")
	}
	if e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("pool after the cast = %d, want 0 (the granted affinity took {2} off {2}{R})", e.G.Players[0].Pool.Total())
	}
	replayCheck(t, e, cfg)
}

// (a) Cascade granted on the stack (CR 702.85), Quandrix, the Proof. The
// dragon's static grants Cascade to instants/sorceries cast from hand; the
// bolt (MV 1) digs, finds Memnite (MV 0), and its free-cast election is
// accepted: the Memnite resolves BEFORE the bolt that granted it.
func TestSetAudit_sos_QuandrixTheProof_GrantedCascadeFreeCast(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 931, []string{"Quandrix, the Proof", "Memnite"}, []string{sosBoltSrc}, nil)
	toMain1(t, e)
	dragon := findAndMoveToHand(t, e, 0, "Quandrix, the Proof")
	bolt := findAndMoveToHand(t, e, 0, "Test Bolt")
	mem := findAndMoveToHand(t, e, 0, "Memnite")
	if dragon == 0 || bolt == 0 || mem == 0 {
		t.Fatalf("precondition: dragon %d bolt %d memnite %d all needed in hand", dragon, bolt, mem)
	}
	searchMoveByName(t, e, "Quandrix, the Proof", state.ZBattlefield)
	e.priorityRound()
	if o := e.G.Obj(dragon); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: dragon not on the battlefield")
	}
	cascadeArrangeWindow(t, e, []string{"Memnite"})
	addMana(t, e, 0, "R")
	submitChoices(t, e, castOptionFor(t, e, bolt).Index)
	sosAnswerPlayerTarget(t, e, 1)
	// The cascade trigger is placed as players receive priority and its
	// resolution poses the free-cast election; pass until it appears.
	for i := 0; i < 6; i++ {
		if d := e.Pending(); d != nil && d.Kind == decision.KModes && d.ResumeKind == "play" {
			break
		}
		sosPassPriority(t, e)
	}
	// The cascade free-cast election (KModes, ResumeKind "play") is for the
	// Memnite; take it, then let everything resolve.
	idx := cascadeElection(t, e, "Memnite")
	if err := e.Submit(decision.Intent{Seq: e.Pending().Seq, Player: e.Pending().Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit cascade election: %v", err)
	}
	passUntilStackEmpty(t, e, 30)
	if n := movedTo(t, e, mem, state.ZLibrary, state.ZExile); n == 0 {
		t.Errorf("cascade: the granted cascade never exiled the Memnite from the library (CR 702.85a via the stack grant)")
	}
	if o := e.G.Obj(mem); o == nil || o.Zone != state.ZBattlefield {
		t.Errorf("cascade: the free-cast Memnite should have resolved to the battlefield, got %+v", o)
	}
	if life := e.G.Players[1].Life; life != 19 {
		t.Errorf("cascade: the bolt did not resolve after the cascade (defender life %d, want 19)", life)
	}
	replayCheck(t, e, cfg)
}

// (a) Crew (CR 702.122), Strixhaven Skycoach. The vehicle's own Crew 2 taps
// a 2-power creature and turns the vehicle into an artifact creature; its
// ETB search (ShuffleNonMandatory$ True) finds a basic land and shuffles
// (deterministic — replayCheck closes the test).
func TestSetAudit_sos_StrixhavenSkycoach_ETBSearchAndCrew(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 932, []string{"Strixhaven Skycoach"}, []string{sosBearSrc}, nil)
	toMain1(t, e)
	coach := findAndMoveToHand(t, e, 0, "Strixhaven Skycoach")
	addMana(t, e, 0, "CCC")
	submitChoices(t, e, castOptionFor(t, e, coach).Index)
	// The ETB search is an optional election; the drain takes its "yes".
	sosDrain(t, e, 0, 12)
	if o := e.G.Obj(coach); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: skycoach not on the battlefield")
	}
	if sosHandCardByName(e, 0, "Mountain") == 0 {
		t.Errorf("crew: the ETB search never put a basic land into hand (ChangeZone Library→Hand, ShuffleNonMandatory$ True)")
	}
	crewer := putCreature(t, e, 0, sosBearSrc)
	e.priorityRound()
	crewVehicle(t, e, coach, crewer)
	// CR 702.122a: crew taps the CREWING creatures; the vehicle itself is
	// not tapped by its own crew cost (only its later attacks tap it).
	if o := e.G.Obj(coach); o == nil || o.Tapped {
		t.Errorf("crew: the skycoach is tapped; CR 702.122a taps the crewing creatures, never the vehicle")
	}
	if o := e.G.Obj(crewer); o == nil || !o.Tapped {
		t.Errorf("crew: the crewer is not tapped (CR 702.122a taps any number of creatures with total power ≥ 2)")
	}
	if !e.IsCreature(coach) {
		t.Errorf("crew: the crewed vehicle is not a creature until end of turn (CR 702.122a)")
	}
	replayCheck(t, e, cfg)
}

// (a) Fight (CR 701.12), Chelonian Tackle: the pump (+0/+10) applies BEFORE
// the fight sub-resolves, so the pumped 2/2 takes the ogre's 3 and survives;
// the fight itself is the SubAbility's "up to one" target. Declared variant.
func TestSetAudit_sos_ChelonianTackle_FightDeclared(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 933, []string{"Chelonian Tackle"}, []string{sosBearSrc}, []string{sosOgreSrc})
	toMain1(t, e)
	tackle := findAndMoveToHand(t, e, 0, "Chelonian Tackle")
	bear := putCreature(t, e, 0, sosBearSrc)
	ogre := putCreature(t, e, 1, sosOgreSrc)
	e.priorityRound()
	addMana(t, e, 0, "GGG")
	submitChoices(t, e, castOptionFor(t, e, tackle).Index)
	answerTargetAsk(t, e, []state.ObjID{bear})
	// Both players pass so the tackle resolves; its resolution poses the
	// fight's "up to one" ask (TargetMin$ 0).
	sosPassPriority(t, e)
	sosPassPriority(t, e)
	// The fight's "up to one" ask is a KChoose tgts election (Min 0, Max 1).
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "tgts" {
		t.Fatalf("expected the fight's up-to-one tgts ask, got kind %v resume %q: %+v", d.Kind, d.ResumeKind, d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Obj == ogre {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("precondition: the ogre is not offered as the fight target: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit fight target: %v", err)
	}
	passUntilStackEmpty(t, e, 20)
	if so := e.G.Obj(bear); so == nil || so.Zone != state.ZBattlefield || so.Damage != 3 {
		t.Errorf("fight: the pumped bear should survive on the battlefield with the ogre's 3 damage, got zone/damage %+v/%d", so, pageDamage(so))
	}
	if oo := e.G.Obj(ogre); oo == nil || oo.Zone != state.ZBattlefield || oo.Damage != 2 {
		t.Errorf("fight: the ogre should have taken the bear's power (2) and survived, got zone/damage %+v/%d", oo, pageDamage(oo))
	}
	if got := e.Derived(bear).Toughness; got != 12 {
		t.Errorf("fight: the +0/+10 pump did not apply before the fight's return damage (toughness %d, want 12, CR 608.2 ordering)", got)
	}
	replayCheck(t, e, cfg)
}

// (a) Fight, Chelonian Tackle, the ZERO-target path (TargetMin$ 0, CR
// 701.12a "up to one target"): declining the fight deals no damage but the
// pump still lands.
func TestSetAudit_sos_ChelonianTackle_FightDeclined(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 934, []string{"Chelonian Tackle"}, []string{sosBearSrc}, []string{sosOgreSrc})
	toMain1(t, e)
	tackle := findAndMoveToHand(t, e, 0, "Chelonian Tackle")
	bear := putCreature(t, e, 0, sosBearSrc)
	ogre := putCreature(t, e, 1, sosOgreSrc)
	e.priorityRound()
	addMana(t, e, 0, "GGG")
	submitChoices(t, e, castOptionFor(t, e, tackle).Index)
	answerTargetAsk(t, e, []state.ObjID{bear})
	sosPassPriority(t, e)
	sosPassPriority(t, e)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "tgts" {
		t.Fatalf("expected the fight's up-to-one tgts ask, got kind %v resume %q: %+v", d.Kind, d.ResumeKind, d)
	}
	// "Up to one" legal decline: no targets at all.
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: nil}); err != nil {
		t.Fatalf("declining the up-to-one fight target was rejected: %v (CR 701.12a TargetMin$ 0)", err)
	}
	passUntilStackEmpty(t, e, 20)
	if so := e.G.Obj(bear); so == nil || so.Damage != 0 {
		t.Errorf("fight declined: the bear took %v damage, want 0 (no fight target)", soDamageTaken(so))
	}
	if oo := e.G.Obj(ogre); oo == nil || oo.Damage != 0 {
		t.Errorf("fight declined: the ogre took %v damage, want 0", soDamageTaken(oo))
	}
	if got := e.Derived(bear).Toughness; got != 12 {
		t.Errorf("fight declined: the +0/+10 pump still applies (toughness %d, want 12)", got)
	}
	replayCheck(t, e, cfg)
}

// (a) Landfall (CR 702.27 / the printed ability word), Tam, Observant
// Sequencer // Deep Sight. The front-face trigger fires on my land drop,
// PREPARES Tam, and the prepared inset spell (Deep Sight) is then castable
// as the CR 722.3c copy — which unprepares Tam.
func TestSetAudit_sos_TamObservantSequencer_LandfallPreparesAndCopyCasts(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 935, []string{"Tam, Observant Sequencer // Deep Sight"}, nil, nil)
	toMain1(t, e)
	tam := searchMoveByName(t, e, "Tam, Observant Sequencer", state.ZBattlefield)
	if o := e.G.Obj(tam); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Tam not on the battlefield")
	}
	mtn := searchMoveByName(t, e, "Mountain", state.ZHand)
	if !playOneLand(t, e, 0, mtn) {
		t.Fatalf("precondition: the land drop was not offered")
	}
	passUntilStackEmpty(t, e, 12)
	if o := e.G.Obj(tam); o == nil || !o.Prepared {
		t.Errorf("landfall: Tam is not prepared after a land entered under its controller's control (the printed ability word)")
	}
	// The prepared copy of the inset spell exists in exile.
	e.priorityRound()
	copyID := sosPreparedCopy(e, "Deep Sight")
	if copyID == 0 {
		t.Fatalf("landfall: no Deep Sight prepared copy in exile (CR 722.3c)")
	}
	// The copy is cast, not cast free: Deep Sight's {G}{U} is paid (CR 601.2f).
	addMana(t, e, 0, "GU")
	opt, ok := sosPreparedCastOption(e, copyID)
	if !ok {
		t.Fatalf("landfall: no prepared_copy cast option for the Deep Sight copy: %+v", e.Pending().Options)
	}
	handBefore := len(e.G.Zone(state.ZHand, 0))
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 12)
	if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore+1 {
		t.Errorf("landfall: the Deep Sight copy did not draw (hand %d, want %d)", got, handBefore+1)
	}
	if life := e.G.Players[0].Life; life != 21 {
		t.Errorf("landfall: the Deep Sight copy did not gain 1 life (%d, want 21)", life)
	}
	if o := e.G.Obj(tam); o == nil || o.Prepared {
		t.Errorf("landfall: casting the copy did not unprepare Tam (CR 722.3c \"Doing so unprepares it\")")
	}
	replayCheck(t, e, cfg)
}

// (a) Treasure (CR 111.10), Goblin Glasswright // Craft with Pride: the
// creature enters PREPARED, the prepared copy of the inset spell (Craft with
// Pride — never offered from hand, CR 722.3) resolves and mints a Treasure,
// and the Treasure's {T}, sacrifice: add one mana of any color pays out.
func TestSetAudit_sos_GoblinGlasswright_PreparedCopyMintsTreasure(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 936, []string{"Goblin Glasswright // Craft with Pride"}, nil, nil)
	toMain1(t, e)
	gw := findAndMoveToHand(t, e, 0, "Goblin Glasswright")
	addMana(t, e, 0, "RR")
	submitChoices(t, e, castOptionFor(t, e, gw).Index)
	passUntilStackEmpty(t, e, 12)
	if o := e.G.Obj(gw); o == nil || o.Zone != state.ZBattlefield || !o.Prepared {
		t.Fatalf("precondition: Glasswright should enter prepared (Prepared bit true), got %+v", o)
	}
	// Back face from hand must NOT be offered (CR 722.3).
	e.priorityRound()
	if o := sosHandCardByName(e, 0, "Craft with Pride"); o != 0 {
		t.Errorf("treasure: the inset prepare spell was offered from hand (CR 722.3 forbids it)")
	}
	copyID := sosPreparedCopy(e, "Craft with Pride")
	if copyID == 0 {
		t.Fatalf("treasure: no Craft with Pride prepared copy in exile")
	}
	// The copy is cast, not cast free: Craft with Pride's {R} is paid (CR 601.2f).
	addMana(t, e, 0, "R")
	opt, ok := sosPreparedCastOption(e, copyID)
	if !ok {
		t.Fatalf("treasure: no prepared_copy cast option: %+v", e.Pending().Options)
	}
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 12)
	treasure := sosFindTokenBy(t, e, "Treasure Token")
	if treasure == 0 {
		t.Fatalf("treasure: no Treasure token was minted (CR 111.10)")
	}
	// Sacrifice it for mana: {T}, Sacrifice this token: Add one mana of any
	// color. The color election takes its first option.
	poolBefore := e.G.Players[0].Pool.Total()
	// The Treasure's sacrifice-for-mana ability is a mana ability: the offer
	// walks expose it as an "activate" option (rules/cast.go's mana-ability
	// path), not the "ability" kind battlefield permanents take.
	var abOpt *decision.Option
	for i := range e.Pending().Options {
		if o := e.Pending().Options[i]; o.Kind == "activate" && o.Obj == treasure {
			abOpt = &e.Pending().Options[i]
			break
		}
	}
	if abOpt == nil {
		t.Fatalf("treasure: the Treasure's sacrifice ability was not offered: %+v", e.Pending())
	}
	submitChoices(t, e, abOpt.Index)
	sosDrain(t, e, 0, 8)
	if got := e.G.Players[0].Pool.Total(); got != poolBefore+1 {
		t.Errorf("treasure: sacrificing the Treasure added %d mana, want 1 (pool %d → %d)", got-poolBefore, poolBefore, got)
	}
	if o := e.G.Obj(treasure); o != nil && o.Zone == state.ZBattlefield {
		t.Errorf("treasure: the sacrificed token is still on the battlefield")
	}
	replayCheck(t, e, cfg)
}

// (a) Treasure, Sanar, Unfinished Genius's gated {T} ability: no treasure
// until you've cast an instant or sorcery this turn (CheckSVar gate); after
// one, it mints.
func TestSetAudit_sos_SanarUnfinishedGenius_TreasureGate(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 937, []string{"Sanar, Unfinished Genius"}, []string{sosHealSrc}, nil)
	toMain1(t, e)
	sanar := searchMoveByName(t, e, "Sanar, Unfinished Genius", state.ZBattlefield)
	if o := e.G.Obj(sanar); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Sanar not on the battlefield")
	}
	e.priorityRound()
	// Gate closed: no instant/sorcery cast yet this turn.
	if abOpt := abilityFor(t, e, 0, sanar); abOpt != nil {
		t.Errorf("treasure: Sanar's treasure ability was offered before any instant/sorcery was cast (CheckSVar$ gate)")
	}
	heal := findAndMoveToHand(t, e, 0, "Test Heal")
	if heal == 0 {
		t.Fatal("precondition: the heal fixture is not in hand")
	}
	// CR 302.6: Sanar is summoning sick on turn 1, so drive to seat 0's NEXT
	// turn (seat 1 holds turn 2) and cast the instant THERE, then activate on
	// the same turn — the gate reads Count$ThisTurnCast, which resets at every
	// turn change, so casting on turn 1 and activating on turn 3 would fail.
	driveToStepAll(t, e, 3, 0, state.StepMain1)
	addMana(t, e, 0, "G")
	submitChoices(t, e, castOptionFor(t, e, heal).Index)
	e.priorityRound()
	abOpt := abilityFor(t, e, 0, sanar)
	if abOpt == nil {
		t.Fatalf("treasure: after casting an instant Sanar's treasure ability is still not offered (CheckSVar$ Count$ThisTurnCast gate)")
	}
	submitChoices(t, e, abOpt.Index)
	sosDrain(t, e, 0, 8)
	if sosFindTokenBy(t, e, "Treasure Token") == 0 {
		t.Errorf("treasure: no Treasure token after the gated activation")
	}
	replayCheck(t, e, cfg)
}

// (c) MayPlay exile duration (Tablet of Discovery, pass C): its ETB mills a
// card and grants "you may play that card this turn". Positive: while the
// milled land sits in the GRAVEYARD a play is offered. Duration: the
// permission is GONE on the opponent's next turn — "this turn" (an
// end-of-your-next-turn duration would still be live there).
func TestSetAudit_sos_TabletOfDiscovery_MayPlayThisTurnOnly(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 938, []string{"Tablet of Discovery"}, nil, nil)
	toMain1(t, e)
	tablet := findAndMoveToHand(t, e, 0, "Tablet of Discovery")
	addMana(t, e, 0, "RRR")
	submitChoices(t, e, castOptionFor(t, e, tablet).Index)
	sosDrain(t, e, 0, 12)
	if o := e.G.Obj(tablet); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Tablet not on the battlefield")
	}
	milled := sosMilledIntoGraveyard(e, tablet)
	if milled == 0 {
		t.Fatalf("precondition: the mill moved no card to the graveyard")
	}
	if mo := e.G.Obj(milled); mo == nil || mo.Zone != state.ZGraveyard {
		t.Fatalf("precondition: the milled card is in %+v, want the graveyard", mo)
	}
	// Positive: the play permission is live while the card is in the
	// graveyard (AffectedZone$ Graveyard).
	if n := countPlayLand(e, milled); n != 1 {
		t.Errorf("mayplay: the milled card's play is not offered while it is in the graveyard (n=%d)", n)
	}
	playOneLand(t, e, 0, milled)
	if o := e.G.Obj(milled); o == nil || o.Zone != state.ZBattlefield {
		t.Errorf("mayplay: playing the milled card did not commit it to the battlefield, got %+v", o)
	}
	// Duration boundary: gone by the opponent's turn.
	driveToStepAll(t, e, 2, 1, state.StepUpkeep)
	if n := countPlayLand(e, milled); n != 0 {
		t.Errorf("mayplay: the play permission outlived its 'this turn' duration (n=%d on turn 2)", n)
	}
	replayCheck(t, e, cfg)
}

// (c) Graveyard-leave trigger (CR 603.6c, Ark of Hunger): a card LEAVING the
// graveyard fires "deals 1 damage to each opponent and you gain 1 life"
// exactly once, and its mill's MayPlay permission drives the leave.
func TestSetAudit_sos_ArkOfHunger_GraveyardLeaveFiresOnce(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 939, []string{"Ark of Hunger"}, nil, nil)
	toMain1(t, e)
	ark := findAndMoveToHand(t, e, 0, "Ark of Hunger")
	addMana(t, e, 0, "RRRW")
	submitChoices(t, e, castOptionFor(t, e, ark).Index)
	passUntilStackEmpty(t, e, 12)
	if o := e.G.Obj(ark); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Ark not on the battlefield")
	}
	e.priorityRound()
	abOpt := abilityFor(t, e, 0, ark)
	if abOpt == nil {
		t.Fatalf("precondition: the Ark's mill ability was not offered: %+v", e.Pending())
	}
	submitChoices(t, e, abOpt.Index)
	sosDrain(t, e, 0, 12)
	milled := sosMilledIntoGraveyard(e, ark)
	if milled == 0 {
		t.Fatalf("precondition: the mill moved no card to the graveyard")
	}
	if n := countPlayLand(e, milled); n != 1 {
		t.Fatalf("precondition: the milled card's MayPlay permission is not live (n=%d)", n)
	}
	// Leave: play the milled land from the graveyard. The Ark's trigger
	// fires ONCE for that one-card departure.
	if !playOneLand(t, e, 0, milled) {
		t.Fatalf("precondition: playing the milled card from the graveyard was not offered")
	}
	passUntilStackEmpty(t, e, 12)
	if life := e.G.Players[1].Life; life != 19 {
		t.Errorf("graveyard-leave: opponent life %d, want 19 (exactly one 1-damage trigger, CR 603.6c)", life)
	}
	if life := e.G.Players[0].Life; life != 21 {
		t.Errorf("graveyard-leave: my life %d, want 21 (exactly one 1-life gain)", life)
	}
	replayCheck(t, e, cfg)
}

// (a) Flash (CR 702.8), Cuboid Colony: at the OPPONENT's main phase the
// flash creature's cast is offered and a plain creature's is not. (The set's
// other flash carrier, Skycoach Conductor // All Aboard, parses the same
// K:Flash; this one isolates the timing gate.)
func TestSetAudit_sos_CuboidColony_FlashCastOnOpponentsTurn(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 940, []string{"Cuboid Colony"}, []string{sosBearSrc}, nil)
	driveToStepAll(t, e, 2, 1, state.StepMain1)
	cuboid := findAndMoveToHand(t, e, 0, "Cuboid Colony")
	bear := findAndMoveToHand(t, e, 0, "Test Bear")
	if cuboid == 0 || bear == 0 {
		t.Fatalf("precondition: Cuboid %d Bear %d must be in hand", cuboid, bear)
	}
	// Float the {G}{U} in seat 0's pool WITHOUT driving (addMana would drive
	// to my own main phase): the ManaAdd events are logged and the pool
	// survives inside the step.
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "G", Amount: 1})
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "U", Amount: 1})
	e.priorityRound()
	// Seat 1 holds priority first (it is the active seat); pass it so the
	// pending ask is seat 0's own offer walk.
	sosPassPriority(t, e)
	if !castOffered(e, cuboid) {
		t.Errorf("flash: the Cuboid Colony's cast is not offered on the opponent's main phase (CR 702.8a)")
	}
	if castOffered(e, bear) {
		t.Errorf("control: the non-flash bear's cast WAS offered on the opponent's turn (CR 702.8 contrast)")
	}
	replayCheck(t, e, cfg)
}

// (c/a) Grandeur (CR 702.36), Page, Loose Leaf. The DigUntil activation's
// cost is Discard<1/Card.namedPage, Loose Leaf>: the discard-cost ask offers
// ONLY the hand's other Page, Loose Leaf copy (the Card.named predicate
// routes the cost selector); the dig reveals until an instant or sorcery,
// puts it in hand, and the revealed rest goes to the library BOTTOM in a
// random order (RevealedLibraryPosition$ -1, RevealRandomOrder$ True — the
// random order itself cannot be pinned by a seeded assertion, so the bottom
// K cards' multiset and "no instant/sorcery among them" are asserted).
func TestSetAudit_sos_PageLooseLeaf_GrandeurCostAndDig(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 941, []string{"Page, Loose Leaf", "Page, Loose Leaf", "Grizzly Bears"}, []string{sosInsightSrc}, nil)
	toMain1(t, e)
	page := searchMoveByName(t, e, "Page, Loose Leaf", state.ZBattlefield)
	page2 := findAndMoveToHand(t, e, 0, "Page, Loose Leaf")
	if o := e.G.Obj(page2); o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: the other Page copy is not in hand")
	}
	// CR 302.6: both the Page's abilities cost {T}, so the grandeur test
	// runs on the Page's SECOND turn (summoning sickness over).
	driveToStepAll(t, e, 3, 0, state.StepUpkeep)
	// Insight's depth from the top of the library before the dig: the dig
	// reveals everything above it and sends that set to the bottom.
	lib := e.G.Zone(state.ZLibrary, 0)
	depth := -1
	for i, id := range lib {
		if o := e.G.Obj(id); o.Face().Name == "Test Insight" {
			depth = i
		}
	}
	if depth < 0 {
		t.Fatal("precondition: the Test Insight is not in the library")
	}
	above := map[string]int{}
	for _, id := range lib[:depth] {
		above[e.G.Obj(id).Face().Name]++
	}
	e.priorityRound()
	d := e.Pending()
	abIdx := -1
	for _, o := range d.Options {
		if o.Obj == page && o.Kind == "ability" && strings.Contains(o.Label, "Reveal cards") {
			abIdx = o.Index
		}
	}
	if abIdx < 0 {
		t.Fatalf("grandeur: the DigUntil activation was not offered: %+v", d.Options)
	}
	submitChoices(t, e, abIdx)
	// The cost's discard ask: ONLY the other Page, Loose Leaf copy is legal.
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "discard" {
		t.Fatalf("grandeur: expected the discard-cost ask, got %+v", d)
	}
	discardable := map[state.ObjID]bool{}
	for _, o := range d.Options {
		discardable[o.Obj] = true
	}
	if len(discardable) != 1 || !discardable[page2] {
		t.Fatalf("grandeur: the discard cost offered %v, want exactly the other Page copy %d (Card.named selector)", discardable, page2)
	}
	if oo := e.G.Obj(page2); oo == nil || oo.Zone != state.ZHand || oo.Face().Name != "Page, Loose Leaf" {
		t.Fatalf("grandeur: the offered discard candidate is not the hand's other Page copy")
	}
	submitChoices(t, e, d.Options[0].Index)
	// The dig resolves.
	sosDrain(t, e, 0, 12)
	if oo := e.G.Obj(page2); oo == nil || oo.Zone != state.ZGraveyard {
		t.Errorf("grandeur: the discarded cost card is in %+v, want the graveyard (CR 701.20b)", oo)
	}
	if po := e.G.Obj(page); po == nil || po.Zone != state.ZBattlefield || po.Tapped {
		t.Errorf("grandeur: the grandeur activation does not tap the Page (its Discard cost has no tap symbol), got tapped=%v", po != nil && po.Tapped)
	}
	insight := state.ObjID(0)
	for _, id := range e.G.Zone(state.ZHand, 0) {
		if o := e.G.Obj(id); o.Face().Name == "Test Insight" {
			insight = id
		}
	}
	if insight == 0 {
		t.Errorf("grandeur: the dig did not put the instant/sorcery into hand (FoundDestination$ Hand)")
	}
	lib = e.G.Zone(state.ZLibrary, 0)
	if len(lib) == 0 {
		t.Fatal("precondition: the library is empty")
	}
	bottom := lib[len(lib)-depth:]
	bottomNames := map[string]int{}
	for _, id := range bottom {
		bottomNames[e.G.Obj(id).Face().Name]++
	}
	if !reflect.DeepEqual(above, bottomNames) {
		t.Errorf("grandeur: the revealed rest at the library bottom is %v, want exactly the pre-dig set above the Insight %v (RevealedLibraryPosition$ -1)", bottomNames, above)
	}
	for _, id := range bottom {
		if o := e.G.Obj(id); o.Face().IsInstant() || o.Face().IsSorcery() {
			t.Errorf("grandeur: an instant/sorcery (%q) was left at the library bottom; the dig stops AT it", o.Face().Name)
		}
	}
	replayCheck(t, e, cfg)
}

// sosFindTokenBy returns the battlefield object whose face name is name and
// that is a token, or 0.
func sosFindTokenBy(t *testing.T, e *Engine, name string) state.ObjID {
	t.Helper()
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.IsToken && o.Face() != nil && o.Face().Name == name {
			return id
		}
	}
	return 0
}

// sosHandCardByName returns the hand object named name, or 0.
func sosHandCardByName(e *Engine, p state.PlayerID, name string) state.ObjID {
	for _, id := range e.G.Zone(state.ZHand, p) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
			return id
		}
	}
	return 0
}

// ---------------------------------------------------------------- Pass B --

// (b) X cost read on resolution (CR 601.2b, CR 107.3f), Molten Note: the
// damage is "the amount of mana spent to cast this spell"
// (SVar:Y:Count$CastTotalManaSpent), NOT the announced X — X=2 plus {R}{W}
// spent is 4 damage, which is what kills the 2/2. The discriminating
// assertion: if the engine read Count$xPaid instead, the bear survives.
func TestSetAudit_sos_MoltenNote_XTotalManaSpentDamage(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 943, []string{"Molten Note"}, []string{sosBearSrc}, nil)
	toMain1(t, e)
	note := findAndMoveToHand(t, e, 0, "Molten Note")
	bear := putCreature(t, e, 0, sosBearSrc)
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: the bear is not on the battlefield")
	}
	e.priorityRound()
	addMana(t, e, 0, "RRWW")
	submitChoices(t, e, castOptionFor(t, e, note).Index)
	// The CR 601.2b X announcement: pick X=2.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "x" {
		t.Fatalf("want the announced-X ask, got %+v", d)
	}
	xIdx := -1
	for _, o := range d.Options {
		if o.Amount == 2 {
			xIdx = o.Index
		}
	}
	if xIdx < 0 {
		t.Fatalf("precondition: X=2 not offered: %+v", d.Options)
	}
	submitChoices(t, e, xIdx)
	answerTargetAsk(t, e, []state.ObjID{bear})
	sosPassPriority(t, e)
	sosPassPriority(t, e)
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZGraveyard {
		t.Errorf("x cost: the bear was not dealt the 4 mana-spent damage (zone %v, want the graveyard)", zoneName(o))
	}
	if life := e.G.Players[1].Life; life != 20 {
		t.Errorf("x cost: opponent life %d, want 20 (the Note targeted a creature, CR 601.2b)", life)
	}
	replayCheck(t, e, cfg)
}

// (b) X into counters (CR 601.2b / CR 122.1d), Procrastinate: "Put twice X
// stun counters on it" (SVar:Y:SVar$X/Twice) — X=1 puts TWO stun counters
// and taps the creature.
func TestSetAudit_sos_Procrastinate_TwiceXStunCounters(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 946, []string{"Procrastinate"}, []string{sosBearSrc}, nil)
	toMain1(t, e)
	proc := findAndMoveToHand(t, e, 0, "Procrastinate")
	bear := putCreature(t, e, 0, sosBearSrc)
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield || o.Tapped {
		t.Fatalf("precondition: the bear must be an untapped battlefield permanent, got %v/%v", zoneName(o), o.Tapped)
	}
	e.priorityRound()
	addMana(t, e, 0, "UU")
	submitChoices(t, e, castOptionFor(t, e, proc).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "x" {
		t.Fatalf("want the announced-X ask, got %+v", d)
	}
	xIdx := -1
	for _, o := range d.Options {
		if o.Amount == 1 {
			xIdx = o.Index
		}
	}
	if xIdx < 0 {
		t.Fatalf("precondition: X=1 not offered: %+v", d.Options)
	}
	submitChoices(t, e, xIdx)
	answerTargetAsk(t, e, []state.ObjID{bear})
	sosPassPriority(t, e)
	sosPassPriority(t, e)
	passUntilStackEmpty(t, e, 20)
	o := e.G.Obj(bear)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: the bear left the battlefield: %v", zoneName(o))
	}
	if !o.Tapped {
		t.Errorf("x cost: the target was not tapped (A:SP$ Tap, CR 601.2 resolution)")
	}
	if got := o.Counter("STUN"); got != 2 {
		t.Errorf("x cost: stun counters = %d, want 2 (\"twice X\" with X=1, SVar Y: SVar$X/Twice)", got)
	}
	replayCheck(t, e, cfg)
}

// (b) Multiplayer (CR 800.4), three seats: a real sos carrier casts and
// resolves in a 3-seat game and the whole game replays byte-identically
// (replayCheck). Seize the Spoils is the carrier: its additional-cost
// discard ask and its Treasure token exercise the full cast round beyond
// the two-seat shape every other test here uses.
func TestSetAudit_sos_ThreeSeatSeizeTheSpoilsReplay(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	spoils, ok := reg.Lookup("Seize the Spoils")
	if !ok {
		t.Fatal("corpus missing \"Seize the Spoils\"")
	}
	fill := func(extras []*cards.Card) []*cards.Card {
		deck := append([]*cards.Card{}, extras...)
		for len(deck) < 40 {
			deck = append(deck, mountainDeck(t, 1)[0])
		}
		return deck
	}
	cfg := seatZeroStart(Config{Seed: 947, Names: []string{"a", "b", "c"},
		Decks:  [][]*cards.Card{fill([]*cards.Card{spoils}), fill(nil), fill(nil)},
		Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	spoilsID := findAndMoveToHand(t, e, 0, "Seize the Spoils")
	if spoilsID == 0 {
		t.Fatal("precondition: Seize the Spoils not in hand")
	}
	handBefore := len(e.G.Zone(state.ZHand, 0))
	addMana(t, e, 0, "RRR")
	submitChoices(t, e, castOptionFor(t, e, spoilsID).Index)
	// The additional-cost discard ask (Cost$ 2 R Discard<1/Card/card>) takes
	// its first option; then the round drains.
	sosDrain(t, e, 0, 20)
	if sosFindTokenBy(t, e, "Treasure Token") == 0 {
		t.Errorf("multiplayer: no Treasure token minted in the 3-seat game (CR 111.10)")
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore {
		t.Errorf("multiplayer: hand size %d, want %d (the cast card and one discard leave the hand; two cards are drawn)", got, handBefore)
	}
	replayCheck(t, e, cfg)
}

// sosKillSrc is a local kill fixture for the response test: a plain
// 5-damage instant (sosBoltSrc/sosBombSrc's NumDmg$ 1 would leave the 2/2
// bear alive).
const sosKillSrc = "Name:Test Killer\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 5\nOracle:x\n"

// (b) Illegal target mid-stack (CR 608.2b): Chelonian Tackle targets my
// bear; the OPPONENT's killer spell kills the bear in response; the tackle's
// target is illegal when it would resolve, so it does NOTHING — no pump, no
// fight, no damage — and is put into its owner's graveyard. The
// distinguishing assertion: the ogre (the fight's available target) takes
// zero damage and no fight tgts ask is ever posed.
func TestSetAudit_sos_ChelonianTackle_TargetDiesInResponse(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 948, []string{"Chelonian Tackle"}, []string{sosBearSrc}, []string{sosKillSrc, sosOgreSrc})
	toMain1(t, e)
	tackle := findAndMoveToHand(t, e, 0, "Chelonian Tackle")
	bear := putCreature(t, e, 0, sosBearSrc)
	ogre := putCreature(t, e, 1, sosOgreSrc)
	// The opponent's responder must actually be in its opening hand (the
	// deck is shuffled; a fixture deep in the library is not a legal cast).
	foeBolt := findAndMoveToHand(t, e, 1, "Test Killer")
	if o := e.G.Obj(foeBolt); o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: the opponent's killer spell is not in hand (%v)", zoneName(o))
	}
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield || e.Derived(bear).Toughness != 2 {
		t.Fatalf("precondition: bear zone/toughness %v/%d, want battlefield/2", zoneName(o), e.Derived(bear).Toughness)
	}
	e.priorityRound()
	addMana(t, e, 0, "GGG")
	addMana(t, e, 1, "R")
	submitChoices(t, e, castOptionFor(t, e, tackle).Index)
	answerTargetAsk(t, e, []state.ObjID{bear})
	// I hold priority after my cast; pass so the opponent can respond.
	sosPassPriority(t, e)
	d := e.Pending()
	if d == nil || d.Player != 1 || d.Kind != decision.KPriority {
		t.Fatalf("precondition: expected the opponent's priority ask, got %+v", d)
	}
	foeBoltIdx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" {
			if o.Obj != foeBolt {
				t.Fatalf("precondition: unexpected cast option %+v", o)
			}
			foeBoltIdx = o.Index
		}
	}
	if foeBoltIdx < 0 {
		t.Fatalf("precondition: the opponent's bolt cast was not offered (pool %d): %+v",
			e.G.Players[1].Pool.Total(), d.Options)
	}
	submitChoices(t, e, foeBoltIdx)
	answerTargetAsk(t, e, []state.ObjID{bear})
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("precondition: the bear did not die to the bolt (zone %v)", zoneName(o))
	}
	if o := e.G.Obj(tackle); o == nil || o.Zone != state.ZGraveyard {
		t.Errorf("illegal target: the fizzled tackle is not in the graveyard (zone %v, CR 608.2b)", zoneName(o))
	}
	if o := e.G.Obj(ogre); o == nil || o.Damage != 0 || o.Zone != state.ZBattlefield {
		t.Errorf("illegal target: the spell did something (ogre %v/%d) — a fizzled spell must do nothing (CR 608.2b)",
			zoneName(o), pageDamage(o))
	}
	if got := e.Derived(ogre).Toughness; got != 3 {
		t.Errorf("illegal target: the ogre was pumped, want the raw 3/3 (got toughness %d)", got)
	}
	replayCheck(t, e, cfg)
}

// (b) Control change (CR 800.4a), inline fixture — the sos set has NO
// gain-control carrier (checked against .superpowers/set-audit/sos.json's
// 271 oracles): a sorcery `A:SP$ GainControl | ValidTgts$ Creature` takes
// an sos-flavoured creature. The object KEEPS ITS IDENTITY: same state.ObjID,
// same card, same zone, only the controller moved — a control change never
// re-enters the battlefield (CR 800.4a/304.2 contrast with CR 613.6).
const sosSeizeSrc = "Name:Test Seize\nManaCost:2 R\nTypes:Sorcery\nA:SP$ GainControl | ValidTgts$ Creature | SpellDescription$ Gain control of target creature.\nOracle:x\n"

func TestSetAudit_sos_ControlChangeKeepsIdentity(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 949, nil, []string{sosSeizeSrc}, []string{sosBearSrc})
	toMain1(t, e)
	seize := findAndMoveToHand(t, e, 0, "Test Seize")
	bear := putCreature(t, e, 1, sosBearSrc)
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield || o.Controller != 1 {
		t.Fatalf("precondition: the bear must be seat 1's battlefield permanent, got %v/%d", zoneName(o), o.Controller)
	}
	e.priorityRound()
	addMana(t, e, 0, "RRR")
	submitChoices(t, e, castOptionFor(t, e, seize).Index)
	answerTargetAsk(t, e, []state.ObjID{bear})
	sosPassPriority(t, e)
	sosPassPriority(t, e)
	passUntilStackEmpty(t, e, 20)
	o := e.G.Obj(bear)
	if o == nil {
		t.Fatal("identity: the control change destroyed the object (a new object id would violate CR 800.4a)")
	}
	if o.Zone != state.ZBattlefield || o.Controller != 0 {
		t.Errorf("control change: the bear is %s under controller %d, want battlefield/0", zoneName(o), o.Controller)
	}
	if o.Face().Name != "Test Bear" {
		t.Errorf("control change: the object's card identity moved (face %q, want \"Test Bear\")", o.Face().Name)
	}
	replayCheck(t, e, cfg)
}

// ---------------------------------------------------------------- Pass C --

// (c) Face-down exile (CR 708.4 / the FaceDown$ exile encoding the
// Hideaway family uses): an exiled card's state.Object.FaceDown bit is
// folded by events, so a later "look at it" projection has something to
// redact. The sos set has NO face-down-exile carrier (checked against
// sos.json — no Hideaway, no Foretell); the inline fixture is the same
// encoding effects/zone.go's applyFaceDownMarker writes for the real
// carriers. The seat-view redaction half of this boundary is pinned by
// view/look_redaction_test.go and view/visibility_pin_test.go (view
// imports rules, so a rules-package test cannot also drive the projection).
const sosFDExileSrc = "Name:Test FD Exile\nManaCost:1 U\nTypes:Instant\nA:SP$ ChangeZone | ValidTgts$ Creature | Origin$ Battlefield | Destination$ Exile | FaceDown$ True\nOracle:x\n"

func TestSetAudit_sos_FaceDownExileFoldsTheBit(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 950, nil, []string{sosFDExileSrc}, []string{sosBearSrc})
	toMain1(t, e)
	fd := findAndMoveToHand(t, e, 0, "Test FD Exile")
	bear := putCreature(t, e, 1, sosBearSrc)
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield || o.FaceDown {
		t.Fatalf("precondition: the bear must be face-up on the battlefield, got %v/%v", zoneName(o), o.FaceDown)
	}
	e.priorityRound()
	addMana(t, e, 0, "UU")
	submitChoices(t, e, castOptionFor(t, e, fd).Index)
	answerTargetAsk(t, e, []state.ObjID{bear})
	sosPassPriority(t, e)
	sosPassPriority(t, e)
	passUntilStackEmpty(t, e, 20)
	o := e.G.Obj(bear)
	if o == nil || o.Zone != state.ZExile {
		t.Fatalf("precondition: the bear was not exiled (zone %v)", zoneName(o))
	}
	if !o.FaceDown {
		t.Errorf("face-down exile: the exiled bear is face up; CR 708.4's face_down encoding never folded Object.FaceDown")
	}
	if o.Face().Name != "Test Bear" {
		t.Errorf("face-down exile: the card identity moved (face %q, want \"Test Bear\")", o.Face().Name)
	}
	replayCheck(t, e, cfg)
}
