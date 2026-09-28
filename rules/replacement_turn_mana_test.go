// The four turn/mana replacement events the engine brief names:
// repl:Untap (the "doesn't untap during its controller's untap step" class),
// repl:BeginPhase (the "skip your draw step" class), repl:Transform (the
// "as this transforms" class) and repl:ProduceMana (the "produces three
// times as much" class, whose ReplaceWith$ body is api:ReplaceMana).
//
// Every primitive test below drives a REAL corpus card pulled from the
// compiled registry -- Basalt Monolith, Necropotence, Sephiroth Fabled
// SOLDIER and Virtue of Strength -- never a hand-authored stand-in for the
// primitive itself. The one supplementary chained-skip test authors its own
// R: lines in the corpus shape (the Stasis / Eon Hub forms, re-typed, never
// copied). Licensing rule: no Forge .txt text is committed here.

package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// realCardEngine builds a two-seat engine whose seat-0 deck leads with the
// named corpus cards (real deck cards, so replayCheck's genesis
// reconstruction sees them), padded with authored basic Mountains, and
// returns it at a fresh turn-1 priority ask after the named cards have been
// moved onto the battlefield with logged MoveZones.
func realCardEngine(t *testing.T, reg *cards.Registry, seed uint64, names ...string) (*Engine, Config, []state.ObjID) {
	t.Helper()
	var deck []*cards.Card
	for _, name := range names {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("corpus fixture: %s missing", name)
		}
		deck = append(deck, c)
	}
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{append(deck, mountainDeck(t, 40-len(deck))...), mountainDeck(t, 40)}})
	e := New(cfg)
	e.Advance()
	ids := make([]state.ObjID, 0, len(names))
	for _, name := range names {
		ids = append(ids, moveByName(t, e, 0, name, state.ZBattlefield))
	}
	e.pending = nil
	e.priorityRound()
	return e, cfg, ids
}

// passPriority submits one pass against the current pending priority
// decision (re-asking first if the last submit consumed it).
func passPriority(t *testing.T, e *Engine) {
	t.Helper()
	d := e.Pending()
	if d == nil {
		e.priorityRound()
		d = e.Pending()
		if d == nil {
			t.Fatal("no pending priority after priorityRound")
		}
	}
	if d.Kind != decision.KPriority {
		t.Fatalf("want KPriority, got %+v", d)
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
	submitChoices(t, e, idx)
}

// TestBasaltMonolithStaysTappedThroughItsUntapStep is the repl:Untap leaf:
// Basalt Monolith's real R:Event$ Untap | ValidCard$ Card.Self |
// ValidStepTurnToController$ You | Layer$ CantHappen line keeps it tapped
// through its controller's untap step while every other permanent untaps
// normally.
func TestBasaltMonolithStaysTappedThroughItsUntapStep(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, ids := realCardEngine(t, reg, 11, "Basalt Monolith")
	basalt := ids[0]
	mountain := moveByName(t, e, 0, "Mountain", state.ZBattlefield)
	// Both enter tapped via the raw Tap setup emit; the replacement gates on
	// the untap STEP, which is what turn 2's beginTurn below exercises.
	e.emit(events.Event{Kind: events.Tap, Obj: basalt})
	e.emit(events.Event{Kind: events.Tap, Obj: mountain})
	e.pending = nil
	e.priorityRound()

	driveToStep(t, e, 3, 0, state.StepMain1)
	if !e.G.Obj(basalt).Tapped {
		t.Fatal("Basalt Monolith untapped during its controller's untap step")
	}
	if e.G.Obj(mountain).Tapped {
		t.Fatal("the plain Mountain untapped too: the replacement must be Card.Self-scoped")
	}
	replayCheck(t, e, cfg)
}

// TestNecropotenceSkipsItsControllersDrawStep is the repl:BeginPhase leaf:
// Necropotence's real R:Event$ BeginPhase | ValidPlayer$ You | Phase$ Draw |
// Skip$ True line skips ITS CONTROLLER's draw step -- seat 0 reaches first
// main with no draw -- while seat 1's own draw step later in the same round
// still draws (the ValidPlayer$ You gate).
func TestNecropotenceSkipsItsControllersDrawStep(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, _ := realCardEngine(t, reg, 23, "Necropotence")

	// Turn 2 (seat 1's turn) draws normally -- the gate is ValidPlayer$ You,
	// so seat 1 is unaffected -- and turn 3 (seat 0's turn) is where the skip
	// fires on the draw entry that follows the upkeep's priority pass.
	lib1 := len(e.G.Zone(state.ZLibrary, 1))
	driveToStep(t, e, 2, 1, state.StepMain1)
	if got := len(e.G.Zone(state.ZLibrary, 1)); got != lib1-1 {
		t.Fatalf("seat 1's library went %d -> %d across its normal draw step, want one draw", lib1, got)
	}
	lib0 := len(e.G.Zone(state.ZLibrary, 0))
	driveToStep(t, e, 3, 0, state.StepMain1)
	if e.G.Step != state.StepMain1 {
		t.Fatalf("after the skip the turn is in %s, want main1 (the draw step is skipped, not stalled)", e.G.Step)
	}
	if got := len(e.G.Zone(state.ZLibrary, 0)); got != lib0 {
		t.Fatalf("seat 0's library went %d -> %d across a skipped draw step", lib0, got)
	}
	replayCheck(t, e, cfg)
}

// TestWildWastelandSkipsItsControllersDrawStep pins the brief's named deck
// carrier (Hail, Caesar pip census 2026-09-18): Wild Wasteland's real
// R:Event$ BeginPhase | ActiveZones$ Battlefield | ValidPlayer$ You |
// Phase$ Draw | Skip$ True line -- character-for-character the Necropotence
// shape -- skips ITS CONTROLLER's draw step. Wild Wasteland is a distinct
// card from a distinct set whose upkeep half is a real DB$ Dig, so the
// library count alone is not enough (the Dig exiles two cards on the same
// turn): the assertion is the DRAW EVENT COUNT. Seat 0 (the controller)
// draws zero cards in its turn 3 draw step while seat 1 draws its normal one
// in turn 2, and a control game with no Wild Wasteland draws seat 0 on turn
// 3 -- so the skip is the card's doing, not a stuck engine.
func TestWildWastelandSkipsItsControllersDrawStep(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, _ := realCardEngine(t, reg, 7, "Wild Wasteland")

	// The skip is ValidPlayer$ You: seat 1's turn-2 draw step still draws.
	lib1 := len(e.G.Zone(state.ZLibrary, 1))
	driveToStep(t, e, 2, 1, state.StepMain1)
	if got := len(e.G.Zone(state.ZLibrary, 1)); got != lib1-1 {
		t.Fatalf("seat 1's library went %d -> %d across its normal draw step, want one draw", lib1, got)
	}

	// Seat 0 reaches turn-3 main1 with no draw: the draw step is skipped, so
	// driveToStep straight to main1 succeeds while the skipped step never
	// enters the log.
	driveToStep(t, e, 3, 0, state.StepMain1)
	if e.G.Step != state.StepMain1 {
		t.Fatalf("after the skip the turn is in %s, want main1 (the draw step is skipped, not stalled)", e.G.Step)
	}
	if got := wildWastelandDrawsInTurn(e, 0, 3); got != 0 {
		t.Fatalf("seat 0 drew %d card(s) in its turn-3 draw step despite Wild Wasteland's Skip$ True", got)
	}

	// Control: the same seat-0 turn draws once with no Wild Wasteland, so the
	// zero above is the replacement and not a broken driver.
	e2, _, _ := realCardEngine(t, reg, 8)
	driveToStep(t, e2, 3, 0, state.StepMain1)
	if got := wildWastelandDrawsInTurn(e2, 0, 3); got != 1 {
		t.Fatalf("control game drew seat 0 %d card(s) on turn 3, want exactly one", got)
	}
	replayCheck(t, e, cfg)
}

// wildWastelandDrawsInTurn counts Draw events for p after the TurnChange that
// opened `turn` -- the one unambiguous probe of a draw step whose StepChange
// was skipped out of the log entirely.
func wildWastelandDrawsInTurn(e *Engine, p state.PlayerID, turn int32) int {
	inTurn := false
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.TurnChange && ev.Amount == turn {
			inTurn = true
			continue
		}
		if inTurn && ev.Kind == events.Draw && ev.Player == p {
			n++
		}
	}
	return n
}

// TestChainedUntapAndUpkeepSkips exercises the chain no single corpus card
// shows: two authored BeginPhase lines in the Stasis / Eon Hub shape (one
// skipping untap, one upkeep), so turn 2 begins with NO untap of a tapped
// permanent, lands directly on the draw step, and the draw step's own
// turn-based action still runs (CR 504.1: skipping upkeep never skips the
// draw). The skipped steps emit no StepChange of their own after turn 2
// begins -- the landing step's entry is the only one in the log.
func TestChainedUntapAndUpkeepSkips(t *testing.T) {
	t.Parallel()
	const skipUntapSrc = "Name:Stasis Well\nManaCost:2 U\nTypes:Enchantment\n" +
		"R:Event$ BeginPhase | ActiveZones$ Battlefield | Phase$ Untap | Skip$ True | Description$ Players skip their untap step.\nOracle:x\n"
	const skipUpkeepSrc = "Name:Quiet Dawn\nManaCost:1 W\nTypes:Enchantment\n" +
		"R:Event$ BeginPhase | ActiveZones$ Battlefield | Phase$ Upkeep | Skip$ True | Description$ Players skip their upkeep steps.\nOracle:x\n"
	e, cfg, _ := newFixtureDeck(t, 31, skipUntapSrc, skipUpkeepSrc)
	moveByName(t, e, 0, "Stasis Well", state.ZBattlefield)
	moveByName(t, e, 0, "Quiet Dawn", state.ZBattlefield)
	mountain := moveByName(t, e, 0, "Mountain", state.ZBattlefield)
	e.emit(events.Event{Kind: events.Tap, Obj: mountain})
	e.pending = nil
	e.priorityRound()
	lib0 := len(e.G.Zone(state.ZLibrary, 0))

	driveToStep(t, e, 3, 0, state.StepMain1)
	if !e.G.Obj(mountain).Tapped {
		t.Fatal("the Mountain untapped although the untap step was skipped")
	}
	if got := len(e.G.Zone(state.ZLibrary, 0)); got != lib0-1 {
		t.Fatalf("seat 0's library went %d -> %d; the skipped upkeep must still hand over to a drawing draw step", lib0, got)
	}
	sawTurn3 := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.TurnChange && ev.Amount == 3 {
			sawTurn3 = true
			continue
		}
		if sawTurn3 && ev.Kind == events.StepChange {
			if ev.Step == state.StepUntap || ev.Step == state.StepUpkeep {
				t.Fatalf("skipped step %s still emitted a StepChange", ev.Step)
			}
			if ev.Step == state.StepDraw {
				break
			}
		}
	}
	if !sawTurn3 {
		t.Fatal("turn 3 never began")
	}
	replayCheck(t, e, cfg)
}

// TestSephirothTransformRunsTheDestinationFaceReplacement is the
// repl:Transform leaf: the real corpus Sephiroth Fabled SOLDIER's
// "as this transforms" replacement is written on the DESTINATION face, and
// flipping it resolves that face's ReplaceWith$ body (the Super Nova emblem
// effect) while the FlipFace itself still happens -- an augmentation, not a
// cancellation. The transform is driven through the card's own real
// DBTransform SVar (DB$ SetState | Mode$ Transform) resolved out of the
// stack, exactly resolveManaAbility resolves a mana ability.
func TestSephirothTransformRunsTheDestinationFaceReplacement(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, ids := realCardEngine(t, reg, 47, "Sephiroth, Fabled SOLDIER")
	seph := ids[0]
	o := e.G.Obj(seph)
	sa := cards.ResolveSVar(o.Face().SVars, "DBTransform")
	if sa == nil {
		t.Fatal("corpus Sephiroth carries no DBTransform SVar")
	}
	// DBTransform's ConditionCheckSVar$ X reads Count$ResolvedThisTurn, so
	// this leaf is reached only on the FOURTH resolution of the turn. Present
	// that context -- the tally a fourth real resolution would have folded
	// (events.Apply's Resolve case) -- rather than zero, which the gate now
	// correctly reads as "not yet" and skips the flip.
	e.G.ResolvedThisTurn = map[string]int32{events.ResolvedAbilityKey(seph, sa): 4}
	e.resolveAbility(seph, 0, nil, sa, o.Face().SVars)

	if e.G.Obj(seph).FaceIdx != 1 {
		t.Fatalf("Sephiroth face index %d, want 1 -- the replacement augments the flip, never cancels it", e.G.Obj(seph).FaceIdx)
	}
	// The destination face's ReplaceWith$ DBEffect arms a permanent-lifetime
	// trigger; it must not silently degrade to the old rejection Note.
	if len(e.G.Delayed) == 0 || e.G.Delayed[len(e.G.Delayed)-1].EffectDuration != "permanent" ||
		hasNote(e, "unmodelled Effect trigger lifetime (Duration$ Permanent") {
		t.Fatalf("Super Nova emblem not registered: %+v", e.G.Delayed)
	}
	if !hasEvent(e, events.FlipFace, seph) {
		t.Fatal("no FlipFace event logged")
	}
	replayCheck(t, e, cfg)
}

// TestVirtueOfStrengthTriplesBasicLandMana is the repl:ProduceMana +
// api:ReplaceMana leaf: Virtue of Strength's real R:Event$ ProduceMana |
// ValidActivator$ You | ValidCard$ Land.Basic line triples what its
// controller's BASIC lands produce, leaves a non-basic land's production and
// an opponent's production untouched, and the rewritten ManaAdd is what the
// log (and therefore replay) carries.
func TestVirtueOfStrengthTriplesBasicLandMana(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, ids := realCardEngine(t, reg, 5, "Virtue of Strength", "Volcanic Island")
	virtue, volcano := ids[0], ids[1]
	_ = virtue
	mtn0 := moveByName(t, e, 0, "Mountain", state.ZBattlefield)
	mtn1 := moveByName(t, e, 1, "Mountain", state.ZBattlefield)
	e.pending = nil
	e.priorityRound()

	// Seat 0's basic Mountain: its intrinsic production is red; tripled once.
	opt := activateOption(t, e, mtn0)
	submitChoices(t, e, opt)
	pool := e.G.Players[0].Pool
	if pool[state.MR] != 3 || pool.Total() != 3 || !e.G.Obj(mtn0).Tapped {
		t.Fatalf("basic land mana = %+v, want three red (tripled once, not twice)", pool)
	}
	// The producer is synchronous replacement context, not a durable ManaAdd
	// field. This preserves the pre-ProduceMana event encoding for unrelated
	// games while the rewritten amount remains replayable.
	for _, ev := range e.L.Events {
		if ev.Kind == events.ManaAdd && ev.Player == 0 && ev.Counter == "R" && ev.Amount == 3 && ev.Obj != 0 {
			t.Fatalf("rewritten ManaAdd has producer Obj %d; producer context must not alter the event stream", ev.Obj)
		}
	}

	// Seat 0's NON-basic Volcanic Island: untouched by the Land.Basic gate.
	e.priorityRound()
	opt = activateOption(t, e, volcano)
	submitChoices(t, e, opt)
	d := e.Pending()
	idx := manaOption(t, d, "R")
	submitChoices(t, e, idx)
	pool = e.G.Players[0].Pool
	if pool[state.MR] != 4 || pool.Total() != 4 {
		t.Fatalf("non-basic land mana = %+v, want exactly one red added to the tripled pool", pool)
	}

	// Seat 1's basic Mountain: ValidActivator$ You excludes the opponent.
	e.priorityRound()
	passPriority(t, e)
	opt = activateOption(t, e, mtn1)
	submitChoices(t, e, opt)
	if p1 := e.G.Players[1].Pool; p1[state.MR] != 1 || p1.Total() != 1 {
		t.Fatalf("opponent's basic land mana = %+v, want one untripled red", p1)
	}
	replayCheck(t, e, cfg)
}

// TestDampingSphereReplacesTypeAndAmount drives the real Damping Sphere and
// Ancient Tomb scripts. ReplaceMana$ (as distinct from ReplaceType$) means
// one mana of the named type instead of every type AND amount, so Tomb's
// two-colorless event becomes exactly one colorless event.
func TestDampingSphereReplacesTypeAndAmount(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, ids := realCardEngine(t, reg, 71, "Damping Sphere", "Ancient Tomb")
	tomb := ids[1]
	submitChoices(t, e, activateOption(t, e, tomb))
	if got := e.G.Players[0].Pool; got.Total() != 1 || got[state.MC] != 1 {
		t.Fatalf("Ancient Tomb through Damping Sphere produced %+v, want exactly one colorless", got)
	}
	replayCheck(t, e, cfg)
}

// TestNyxbloomDoesNotMultiplySacrificeOnlyMana proves ProduceMana's
// tap-for-mana provenance with two real scripts. Krark-Clan Ironworks pays a
// sacrifice-only cost, so Nyxbloom Ancient's "tap a permanent for mana"
// replacement does not apply even though synchronous replacement context
// identifies KCI as the producer.
func TestNyxbloomDoesNotMultiplySacrificeOnlyMana(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, ids := realCardEngine(t, reg, 73, "Nyxbloom Ancient", "Krark-Clan Ironworks")
	kci := ids[1]
	submitChoices(t, e, activateOption(t, e, kci))
	if got := e.G.Players[0].Pool; got.Total() != 2 || got[state.MC] != 2 {
		t.Fatalf("KCI sacrifice-only production through Nyxbloom = %+v, want two colorless", got)
	}
	if e.G.Obj(kci).Zone != state.ZGraveyard {
		t.Fatalf("KCI zone = %s, want graveyard after paying its real sacrifice cost", e.G.Obj(kci).Zone)
	}
	replayCheck(t, e, cfg)
}

// TestManaReplacementApplicabilityIsRechecked uses two real cards to prove
// each rewrite is followed by a fresh applicability pass. Damping Sphere does
// not match a Mountain's initial one-mana event; Nyxbloom first triples it,
// which makes Damping's ManaAmount$ GE2 gate newly true, and Damping then
// replaces the result with exactly one colorless mana.
func TestManaReplacementApplicabilityIsRechecked(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, _ := realCardEngine(t, reg, 77, "Nyxbloom Ancient", "Damping Sphere")
	mountain := moveByName(t, e, 0, "Mountain", state.ZBattlefield)
	e.pending = nil
	e.priorityRound()
	submitChoices(t, e, activateOption(t, e, mountain))
	if got := e.G.Players[0].Pool; got.Total() != 1 || got[state.MC] != 1 {
		t.Fatalf("Nyxbloom then newly-applicable Damping Sphere produced %+v, want one colorless", got)
	}
	replayCheck(t, e, cfg)
}

// TestCompetingManaReplacementsUsePlayerOrder drives the real Naked
// Singularity and Reality Twist scripts. Both replace a Mountain's red mana,
// and both remain applicable after the first rewrite, so CR 616.1 asks the
// affected player which applies first; the unchosen replacement then applies
// automatically and determines the final colour.
func TestCompetingManaReplacementsUsePlayerOrder(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		first string
		want  int
	}{
		{first: "Naked Singularity", want: state.MW},
		{first: "Reality Twist", want: state.MU},
	} {
		t.Run(tc.first+" first", func(t *testing.T) {
			e, cfg, _ := realCardEngine(t, reg, 79, "Naked Singularity", "Reality Twist")
			mountain := moveByName(t, e, 0, "Mountain", state.ZBattlefield)
			e.pending = nil
			e.priorityRound()
			submitChoices(t, e, activateOption(t, e, mountain))
			d := e.Pending()
			if d == nil || d.Kind != decision.KReplacement || len(d.Options) != 2 {
				t.Fatalf("competing mana replacement decision = %+v, want two KReplacement options", d)
			}
			pick := -1
			for _, opt := range d.Options {
				if o := e.G.Obj(opt.Obj); o != nil && o.Face() != nil && o.Face().Name == tc.first {
					pick = opt.Index
				}
			}
			if pick < 0 {
				t.Fatalf("no option for %s in %+v", tc.first, d.Options)
			}
			submitChoices(t, e, pick)
			pool := e.G.Players[0].Pool
			if pool.Total() != 1 || pool[tc.want] != 1 {
				t.Fatalf("after choosing %s first, pool = %+v, want one mana in slot %d", tc.first, pool, tc.want)
			}
			replayCheck(t, e, cfg)
		})
	}
}

// TestFastingAsksWhetherToSkipTheDrawStep drives Fasting's real Optional$
// BeginPhase replacement through both answers. Accepting skips the draw and
// resolves its gain-life body; declining logs the original draw-step entry
// and performs the turn-based draw exactly once.
func TestFastingAsksWhetherToSkipTheDrawStep(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	for _, apply := range []bool{true, false} {
		name := "decline"
		if apply {
			name = "apply"
		}
		t.Run(name, func(t *testing.T) {
			e, cfg, _ := realCardEngine(t, reg, 83, "Fasting")
			// Put a replayable turn/step boundary immediately before the event
			// under test. This isolates Fasting's replacement from its separate
			// upkeep-counter and drawn-card triggers without mutating Game.
			e.pending = nil
			e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: e.G.Turn + 1})
			e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
			life := e.G.Players[0].Life
			library := len(e.G.Zone(state.ZLibrary, 0))
			e.setStep(state.StepDraw)
			d := e.Pending()
			if d == nil || d.Kind != decision.KReplacement || len(d.Options) != 2 ||
				d.Options[0].Kind != "apply" || d.Options[1].Kind != "decline" {
				t.Fatalf("Fasting choice = %+v, want apply/decline KReplacement", d)
			}
			pick := 1
			if apply {
				pick = 0
			}
			submitChoices(t, e, pick)
			if apply {
				if got := e.G.Players[0].Life; got != life+2 {
					t.Fatalf("life after applying Fasting = %d, want %d", got, life+2)
				}
				if got := len(e.G.Zone(state.ZLibrary, 0)); got != library {
					t.Fatalf("library after applying Fasting = %d, want unchanged %d", got, library)
				}
				if e.G.Step != state.StepMain1 {
					t.Fatalf("step after applying Fasting = %s, want main1", e.G.Step)
				}
			} else {
				if got := e.G.Players[0].Life; got != life {
					t.Fatalf("life after declining Fasting = %d, want %d", got, life)
				}
				if got := len(e.G.Zone(state.ZLibrary, 0)); got != library-1 {
					t.Fatalf("library after declining Fasting = %d, want one draw (%d)", got, library-1)
				}
				if e.G.Step != state.StepDraw {
					t.Fatalf("step after declining Fasting = %s, want draw", e.G.Step)
				}
			}
			replayCheck(t, e, cfg)
		})
	}
}

// TestDecliningOptionalPhaseReplacementContinuesToMandatoryReplacement is the
// CR 616 interaction between real Fasting and Necropotence. The affected
// player chooses Fasting first, declines it, and still has Necropotence's
// mandatory draw-step skip applied; declining one effect cannot bypass the
// other applicable replacement.
func TestDecliningOptionalPhaseReplacementContinuesToMandatoryReplacement(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, _ := realCardEngine(t, reg, 89, "Fasting", "Necropotence")
	e.pending = nil
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: e.G.Turn + 1})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	library := len(e.G.Zone(state.ZLibrary, 0))
	e.setStep(state.StepDraw)

	d := e.Pending()
	if d == nil || d.Kind != decision.KReplacement || len(d.Options) != 2 {
		t.Fatalf("phase replacement order = %+v, want Fasting/Necropotence choice", d)
	}
	fasting := -1
	for _, opt := range d.Options {
		if o := e.G.Obj(opt.Obj); o != nil && o.Face() != nil && o.Face().Name == "Fasting" {
			fasting = opt.Index
		}
	}
	if fasting < 0 {
		t.Fatalf("phase replacement options = %+v, no Fasting", d.Options)
	}
	submitChoices(t, e, fasting)
	d = e.Pending()
	if d == nil || len(d.Options) != 2 || d.Options[0].Kind != "apply" || d.Options[1].Kind != "decline" {
		t.Fatalf("Fasting apply/decline = %+v", d)
	}
	submitChoices(t, e, 1)
	if got := len(e.G.Zone(state.ZLibrary, 0)); got != library {
		t.Fatalf("library after declining Fasting with Necropotence active = %d, want %d", got, library)
	}
	if e.G.Step != state.StepMain1 {
		t.Fatalf("step after remaining mandatory skip = %s, want main1", e.G.Step)
	}
	replayCheck(t, e, cfg)
}

// TestPulseOfLlanowarAsksForReplacementManaColor drives the real choice-
// valued ReplaceType$ Any body. The original red ManaAdd is parked until the
// affected player chooses, and the answered blue production is what enters
// both the pool and replayable log.
func TestPulseOfLlanowarAsksForReplacementManaColor(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, _ := realCardEngine(t, reg, 97, "Pulse of Llanowar")
	mountain := moveByName(t, e, 0, "Mountain", state.ZBattlefield)
	e.pending = nil
	e.priorityRound()
	submitChoices(t, e, activateOption(t, e, mountain))
	d := e.Pending()
	if d == nil || d.Kind != decision.KReplacement || len(d.Options) != 5 || d.Options[1].Label != "Add U" {
		t.Fatalf("Pulse replacement colour decision = %+v, want five WUBRG options", d)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool before replacement colour answer = %d, want 0 (ManaAdd must remain parked)", got)
	}
	clone := e.Clone()
	submitChoices(t, clone, 1)
	if got := clone.G.Players[0].Pool; got.Total() != 1 || got[state.MU] != 1 {
		t.Fatalf("clone lost parked Pulse replacement: pool = %+v, want one blue", got)
	}
	submitChoices(t, e, 1)
	if got := e.G.Players[0].Pool; got.Total() != 1 || got[state.MU] != 1 {
		t.Fatalf("Pulse replacement mana = %+v, want one blue", got)
	}
	replayCheck(t, e, cfg)
}

// TestReplacementManaColorResumesCastPayment proves the replacement-time
// colour ask can interrupt CR 601.2g. A Mountain pays for real Sol Ring only
// after Pulse's parked answer; the cast then commits and spends that mana.
func TestReplacementManaColorResumesCastPayment(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _, ids := realCardEngine(t, reg, 99, "Pulse of Llanowar", "Sol Ring")
	ring := ids[1]
	e.emit(events.Event{Kind: events.MoveZone, Obj: ring, From: state.ZBattlefield, To: state.ZHand})
	mountain := moveByName(t, e, 0, "Mountain", state.ZBattlefield)
	e.pending = nil
	e.beginCast(0, decision.Option{Kind: "cast", Obj: ring})
	e.Advance()
	d := e.Pending()
	activate := -1
	for _, opt := range d.Options {
		if opt.Kind == "activate" && opt.Obj == mountain {
			activate = opt.Index
		}
	}
	if activate < 0 {
		t.Fatalf("cast mana window = %+v, no Mountain activation", d)
	}
	submitChoices(t, e, activate)
	if d = e.Pending(); d == nil || d.Kind != decision.KReplacement || len(d.Options) != 5 {
		t.Fatalf("cast-time Pulse colour choice = %+v", d)
	}
	submitChoices(t, e, 2)
	if e.G.Obj(ring).Zone != state.ZStack || len(e.G.Stack) != 1 || e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("after cast-time replacement answer: ring=%s stack=%v pool=%+v; want paid spell on stack",
			e.G.Obj(ring).Zone, e.G.Stack, e.G.Players[0].Pool)
	}
}

// TestCompetingUntapReplacementsUseAffectedPlayerOrder drives the real
// battlefield Intruder Alarm and command-zone Edge of Malacol scripts against
// one tapped Memnite. Both prevent its untap, but Edge's ReplaceWith$ adds two
// counters, so the affected permanent's controller must get the CR 616.1
// choice rather than whichever replacement appears first in the zone scan.
func TestCompetingUntapReplacementsUseAffectedPlayerOrder(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, ids := realCardEngine(t, reg, 100, "Edge of Malacol", "Intruder Alarm", "Memnite")
	edge, memnite := ids[0], ids[2]
	e.emit(events.Event{Kind: events.MoveZone, Obj: edge, From: state.ZBattlefield, To: state.ZCommand})
	e.emit(events.Event{Kind: events.Tap, Obj: memnite})
	e.pending = nil
	// beginTurn's actual untap loop must park here. The answer must resume that
	// loop and advance to upkeep only after the selected replacement completes.
	e.beginTurn(0)

	d := e.Pending()
	if d == nil || d.Kind != decision.KReplacement || len(d.Options) != 2 {
		t.Fatalf("untap replacement decision = %+v, want two KReplacement options", d)
	}
	edgeChoice := -1
	for _, opt := range d.Options {
		if o := e.G.Obj(opt.Obj); o != nil && o.Face() != nil && o.Face().Name == "Edge of Malacol" {
			edgeChoice = opt.Index
		}
	}
	if edgeChoice < 0 {
		t.Fatalf("untap replacement options = %+v, no Edge of Malacol", d.Options)
	}
	// The parked turn-based continuation is part of the replacement choice, so
	// answering a clone must resume its own untap scan too.
	clone := e.Clone()
	submitChoices(t, clone, edgeChoice)
	if o := clone.G.Obj(memnite); !o.Tapped || o.Counter("P1P1") != 2 ||
		clone.G.Step != state.StepUpkeep || clone.Pending() == nil || clone.Pending().Kind != decision.KPriority {
		t.Fatalf("clone after untap answer: Memnite=%+v step=%s pending=%+v, want tapped two-counter Memnite and upkeep priority",
			o, clone.G.Step, clone.Pending())
	}
	submitChoices(t, e, edgeChoice)
	if o := e.G.Obj(memnite); !o.Tapped || o.Counter("P1P1") != 2 {
		t.Fatalf("choosing Edge replacement left Memnite tapped=%v counters=%+v, want tapped with two +1/+1 counters",
			o.Tapped, o.Counters)
	}
	if e.G.Step != state.StepUpkeep || e.Pending() == nil || e.Pending().Kind != decision.KPriority {
		t.Fatalf("after untap answer: step=%s pending=%+v, want upkeep priority", e.G.Step, e.Pending())
	}
	replayCheck(t, e, cfg)
}

// TestCommandZoneReplacementSourcesAreDiscovered pins the replacement-only
// command-zone scan on one real source for each ticket event that exists
// there. Trigger discovery remains on the ordinary object walk.
func TestCommandZoneReplacementSourcesAreDiscovered(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	t.Run("Untap", func(t *testing.T) {
		e, cfg, ids := realCardEngine(t, reg, 101, "Edge of Malacol", "Memnite")
		edge, creature := ids[0], ids[1]
		e.emit(events.Event{Kind: events.MoveZone, Obj: edge, From: state.ZBattlefield, To: state.ZCommand})
		e.emit(events.Event{Kind: events.Tap, Obj: creature})
		e.pending = nil
		e.priorityRound()
		driveToStep(t, e, 3, 0, state.StepMain1)
		if !e.G.Obj(creature).Tapped || e.G.Obj(creature).Counter("P1P1") != 2 {
			t.Fatalf("Edge command-zone untap replacement: tapped=%v counters=%v, want tapped with two +1/+1 counters",
				e.G.Obj(creature).Tapped, e.G.Obj(creature).Counters)
		}
		replayCheck(t, e, cfg)
	})

	t.Run("BeginPhase", func(t *testing.T) {
		e, cfg, ids := realCardEngine(t, reg, 103, "Necropotence Avatar")
		avatar := ids[0]
		e.emit(events.Event{Kind: events.MoveZone, Obj: avatar, From: state.ZBattlefield, To: state.ZCommand})
		e.pending = nil
		e.priorityRound()
		library := len(e.G.Zone(state.ZLibrary, 0))
		driveToStep(t, e, 3, 0, state.StepMain1)
		if got := len(e.G.Zone(state.ZLibrary, 0)); got != library {
			t.Fatalf("Necropotence Avatar command-zone draw skip: library %d -> %d", library, got)
		}
		replayCheck(t, e, cfg)
	})

	t.Run("ProduceMana", func(t *testing.T) {
		e, cfg, ids := realCardEngine(t, reg, 107, "Mirri")
		mirri := ids[0]
		e.emit(events.Event{Kind: events.MoveZone, Obj: mirri, From: state.ZBattlefield, To: state.ZCommand})
		mountain := moveByName(t, e, 0, "Mountain", state.ZBattlefield)
		e.pending = nil
		e.priorityRound()
		submitChoices(t, e, activateOption(t, e, mountain))
		d := e.Pending()
		if d == nil || d.Kind != decision.KReplacement || len(d.Options) != 5 {
			t.Fatalf("Mirri command-zone mana choice = %+v, want WUBRG replacement choice", d)
		}
		submitChoices(t, e, 4)
		if got := e.G.Players[0].Pool; got.Total() != 1 || got[state.MG] != 1 {
			t.Fatalf("Mirri command-zone replacement mana = %+v, want one green", got)
		}
		replayCheck(t, e, cfg)
	})
}
