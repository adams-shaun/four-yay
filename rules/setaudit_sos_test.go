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
const sosSpriteSrc = "Name:Test Sprite\nManaCost:1 U\nTypes:Creature Faerie\nPT:1/1\nK:Flying\nOracle:x\n"
const sosElfSrc = "Name:Test Elf\nManaCost:G\nTypes:Creature Elf\nPT:1/1\nOracle:x\n"
const sosOgreSrc = "Name:Test Ogre\nManaCost:2 R\nTypes:Creature Ogre\nPT:3/3\nOracle:x\n"
const sosCubSrc = "Name:Test Cub\nManaCost:0\nTypes:Artifact\nOracle:x\n"
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

// soDamageTaken reports an object's DamageReceivedThisTurn, or -1 if it is
// gone, for dead-or-damaged assertions (Damage is cleared on death).
func soDamageTaken(o *state.Object) int32 {
	if o == nil {
		return -1
	}
	return o.DamageReceivedThisTurn
}
