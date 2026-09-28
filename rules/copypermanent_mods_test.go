package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// This file pins the characteristic-modification half of DB$ CopyPermanent on
// real corpus cards (task copyp1's follow-up): SetCreatureTypes$, NonLegendary$,
// AddKeywords$, PumpKeywords$ (+PumpDuration$), RemoveKeywords$ and
// RemoveCardTypes$ are applied to the mint as tracked continuous effects, and
// are no longer named by the per-call skip Note. Every test asserts the
// implemented parameter families are absent from any CopyPermanent skip Note,
// the modified characteristics are on the mint, and the whole game replays
// byte-identically from its log.

// hasTypeWord reports whether the derived type list carries w (case-insensitive).
func copyHasTypeWord(types []string, w string) bool {
	for _, t := range types {
		if strings.EqualFold(t, w) {
			return true
		}
	}
	return false
}

// noCopyPermanentModNote fails if any CopyPermanent skip note names one of the
// now-implemented modification families, and if any unimplemented-API note at
// all reached the log.
func noCopyPermanentModNote(t *testing.T, e *Engine, params ...string) {
	t.Helper()
	for _, ev := range e.L.Events {
		if ev.Kind != events.Note {
			continue
		}
		if strings.Contains(ev.Text, "unimplemented API") {
			t.Fatalf("log carries an unimplemented-API note: %q", ev.Text)
		}
		for _, p := range params {
			if strings.Contains(ev.Text, p+"$") {
				t.Fatalf("implemented %s still named by a skip note: %q", p, ev.Text)
			}
		}
	}
}

// answerOptionalYes walks the pending decisions, answering priority with pass,
// mandatory choices with their first option, and any yes/no optional ask with
// the option whose label reads yes (option 0 by the engine's convention) --
// used to accept a "you may" trigger whose body is the copy under test.
func answerOptionalYes(t *testing.T, e *Engine, limit int) {
	t.Helper()
	for i := 0; i < limit && !e.G.Over; i++ {
		if len(e.pendingTriggers) == 0 && len(e.G.Stack) == 0 {
			return
		}
		d := e.Pending()
		if d == nil {
			continue
		}
		if d.Kind == decision.KPriority {
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
			if idx < 0 {
				return
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
				t.Fatalf("submit pass: %v", err)
			}
			continue
		}
		switch d.Kind {
		case decision.KTriggerOrder:
			answerTriggerOrders(t, e)
			continue
		}
		if len(d.Options) == 0 {
			return
		}
		// Option 0 is the "yes"/first-offered answer for every ask this file
		// drives (the optional-trigger KChoose and the hidden pick).
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
			t.Fatalf("submit %s: %v", d.Kind, err)
		}
	}
}

// TestCroakingCounterpartCopyIsAOneOneGreenFrog pins SetCreatureTypes$,
// SetPower$, SetToughness$ and SetColor$ together on the real corpus sorcery:
// the copy is exactly a 1/1 green Frog, so the printed Bear subtype is gone
// (SetCreatureTypes$ REPLACES the creature types, not adds them).
func TestCroakingCounterpartCopyIsAOneOneGreenFrog(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{lookup(t, reg, "Croaking Counterpart"), lookup(t, reg, "Grizzly Bears")},
		[]*cards.Card{})
	moveByName(t, e, 0, "Croaking Counterpart", state.ZHand)
	bear := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)

	addMana(t, e, 0, "CGU") // {1}{G}{U}
	opt := castByName(t, e, 0, "Croaking Counterpart")
	if opt == nil {
		t.Fatalf("Croaking Counterpart not castable: %+v", e.Pending().Options)
	}
	submitChoices(t, e, opt.Index)
	answerKTarget(t, e, bear)
	passUntilStackEmpty(t, e, 20)

	bearCard := e.G.Obj(bear).Card
	cid := findTokenCopyOf(t, e, bearCard, bear)
	d := e.Derived(cid)
	if d.Power != 1 || d.Toughness != 1 {
		t.Fatalf("copy P/T = %d/%d, want 1/1", d.Power, d.Toughness)
	}
	if d.Colors != "G" {
		t.Fatalf("copy colours = %q, want G", d.Colors)
	}
	if !copyHasTypeWord(d.Types, "Frog") {
		t.Fatalf("copy types = %v, want Frog", d.Types)
	}
	if copyHasTypeWord(d.Types, "Bear") {
		t.Fatalf("copy types = %v still carry the printed Bear subtype; SetCreatureTypes$ must replace", d.Types)
	}
	noCopyPermanentModNote(t, e, "SetCreatureTypes", "SetPower", "SetToughness", "SetColor")
	replayCheck(t, e, cfg)
}

// TestKikiJikiCopyHasHasteAndIsSacrificedAtNextEndStep pins AddKeywords$ on its
// real corpus activated ability: the copy gains Haste (a printed keyword it
// does not inherit from the copied Bear) and the AtEOT$ Sacrifice rider is
// still honoured beside it.
func TestKikiJikiCopyHasHasteAndIsSacrificedAtNextEndStep(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{lookup(t, reg, "Kiki-Jiki, Mirror Breaker"), lookup(t, reg, "Grizzly Bears")},
		[]*cards.Card{})
	kk := moveByName(t, e, 0, "Kiki-Jiki, Mirror Breaker", state.ZBattlefield)
	bear := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)

	e.priorityRound()
	opt := abilityOption(t, e, kk, 0)
	submitChoices(t, e, opt.Index)
	answerKTarget(t, e, bear)
	passUntilStackEmpty(t, e, 20)

	bearCard := e.G.Obj(bear).Card
	cid := findTokenCopyOf(t, e, bearCard, bear)
	if !e.HasKeyword(cid, "Haste") {
		t.Fatalf("Kiki-Jiki copy does not have Haste: keywords %v", e.Derived(cid).Keywords)
	}
	if e.HasKeyword(bear, "Haste") {
		t.Fatal("the copied Bear itself gained Haste -- the grant leaked off the copy")
	}
	noCopyPermanentModNote(t, e, "AddKeywords")

	// AtEOT$ Sacrifice: the next end step sacrifices exactly the copy.
	driveToStepAll(t, e, e.G.Turn, e.G.Active, state.StepEnd)
	for e.putTriggersOnStack() {
		answerTriggerOrders(t, e)
		passUntilStackEmpty(t, e, 30)
	}
	passUntilStackEmpty(t, e, 30)
	if z := e.G.Obj(cid).Zone; z == state.ZBattlefield {
		t.Fatalf("Kiki-Jiki copy survived the end-step sacrifice (zone %s)", z)
	}
	if z := e.G.Obj(bear).Zone; z != state.ZBattlefield {
		t.Fatalf("the copied Bear was sacrificed (zone %s)", z)
	}
	noCopyPermanentModNote(t, e, "AddKeywords")
	replayCheck(t, e, cfg)
}

// TestMultiversalRecruitmentCopyIsNotLegendary pins NonLegendary$ True on the
// brief's named carrier: the copy of a legendary creature (Kiki-Jiki) loses the
// Legendary supertype while keeping the rest of its type line -- and, crucially,
// SURVIVES the CR 704.5j legend rule against its original, because the legend
// SBA reads the DERIVED type list (rules/sba.go legendCasualties). Before that
// read, the copy was binned as a duplicate and the card did nothing.
func TestMultiversalRecruitmentCopyIsNotLegendary(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{lookup(t, reg, "Multiversal Recruitment"), lookup(t, reg, "Kiki-Jiki, Mirror Breaker")},
		[]*cards.Card{})
	moveByName(t, e, 0, "Multiversal Recruitment", state.ZHand)
	kk := moveByName(t, e, 0, "Kiki-Jiki, Mirror Breaker", state.ZBattlefield)

	if e.G.Active != 0 {
		driveToTurn(t, e, e.G.Turn+1, 0)
	}
	addMana(t, e, 0, "CCCU") // {3}{U}
	opt := castByName(t, e, 0, "Multiversal Recruitment")
	if opt == nil {
		t.Fatalf("Multiversal Recruitment not castable: %+v", e.Pending().Options)
	}
	submitChoices(t, e, opt.Index)
	answerKTarget(t, e, kk)
	passUntilStackEmpty(t, e, 20)

	kkCard := e.G.Obj(kk).Card
	cid := findTokenCopyOf(t, e, kkCard, kk)
	if z := e.G.Obj(cid).Zone; z != state.ZBattlefield {
		t.Fatalf("the non-legendary copy left the battlefield (zone %s) -- legend rule", z)
	}
	dt := e.Derived(cid)
	if copyHasTypeWord(dt.Types, "Legendary") {
		t.Fatalf("copy types = %v still Legendary; NonLegendary$ must drop the supertype", dt.Types)
	}
	if !copyHasTypeWord(dt.Types, "Creature") || !copyHasTypeWord(dt.Types, "Goblin") {
		t.Fatalf("copy types = %v lost more than the Legendary supertype", dt.Types)
	}
	noCopyPermanentModNote(t, e, "NonLegendary")
	replayCheck(t, e, cfg)
}

// TestEmberIslandProductionCopyIsNotLegendary pins NonLegendary$ True on a
// real corpus carrier whose copy cannot collide with the original: Ember
// Island Production's mode DBCopy2 copies a creature an OPPONENT controls
// under the caster's control, so the two same-named legendaries are controlled
// by different players and the CR 704.5j legend rule does not apply. (This is
// why Multiversal Recruitment cannot pin NonLegendary$ end to end: its copy of
// a creature YOU control shares the original's name, and the legend SBA reads
// the PRINTED face -- o.Face().IsLegendary() -- so the layer-4 supertype strip
// never saves it. Recorded in this round's Issues.)
func TestEmberIslandProductionCopyIsNotLegendary(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{lookup(t, reg, "Ember Island Production")},
		[]*cards.Card{lookup(t, reg, "Kiki-Jiki, Mirror Breaker")})
	moveByName(t, e, 0, "Ember Island Production", state.ZHand)
	kk := moveByName(t, e, 1, "Kiki-Jiki, Mirror Breaker", state.ZBattlefield)

	// A sorcery needs seat 0's own main phase; the genesis toss depends on the
	// shuffle, so drive there when seat 1 started.
	if e.G.Active != 0 {
		driveToTurn(t, e, e.G.Turn+1, 0)
	}

	addMana(t, e, 0, "CCCUU") // {3}{U}{U}
	opt := castByName(t, e, 0, "Ember Island Production")
	if opt == nil {
		t.Fatalf("Ember Island Production not castable: %+v", e.Pending().Options)
	}
	submitChoices(t, e, opt.Index)

	// The Charm mode ask: pick the mode that copies an opponent's creature
	// (its stack description names the 2/2 Coward).
	d := passUntilNonPriority(t, e, 40)
	if d.Kind != decision.KModes && d.Kind != decision.KChoose {
		t.Fatalf("charm mode ask = %+v", d)
	}
	modeIdx := -1
	for _, o := range d.Options {
		if o.Kind == "mode" || strings.Contains(o.Label, "Coward") || strings.Contains(o.Label, "opponent") {
			modeIdx = o.Index
		}
	}
	if modeIdx < 0 {
		t.Fatalf("could not find the opponent-copy mode: %+v", d.Options)
	}
	submitChoices(t, e, modeIdx)
	answerKTarget(t, e, kk)
	passUntilStackEmpty(t, e, 30)

	kkCard := e.G.Obj(kk).Card
	cid := findTokenCopyOf(t, e, kkCard)
	o := e.G.Obj(cid)
	if o.Controller != 0 {
		t.Fatalf("copy controller = %d, want 0 (the caster)", o.Controller)
	}
	dt := e.Derived(cid)
	if copyHasTypeWord(dt.Types, "Legendary") {
		t.Fatalf("copy types = %v still Legendary; NonLegendary$ must drop the supertype", dt.Types)
	}
	if !copyHasTypeWord(dt.Types, "Coward") {
		t.Fatalf("copy types = %v, want Coward", dt.Types)
	}
	if dt.Power != 2 || dt.Toughness != 2 {
		t.Fatalf("copy P/T = %d/%d, want 2/2", dt.Power, dt.Toughness)
	}
	noCopyPermanentModNote(t, e, "NonLegendary")
	replayCheck(t, e, cfg)
}

// driveToCombatAnswering advances the game (answering priority passes, combat
// declarations with "no attackers", trigger-order asks in offered order and
// optional-trigger asks with "yes") until p is the active player at the
// begin-combat step, then returns. It is the drive the begin-combat copy
// triggers need without a whole-turn driver that fatals on their asks.
func driveToCombatAnswering(t *testing.T, e *Engine, p state.PlayerID, limit int) {
	t.Helper()
	for i := 0; i < limit && !e.G.Over; i++ {
		if e.G.Active == p && e.G.Step == state.StepBeginCombat {
			return
		}
		d := e.Pending()
		if d == nil {
			e.Advance()
			continue
		}
		switch d.Kind {
		case decision.KPriority:
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
				t.Fatalf("submit pass: %v", err)
			}
		case decision.KTriggerOrder:
			answerTriggerOrders(t, e)
		case decision.KAttackers, decision.KBlockers:
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: nil}); err != nil {
				t.Fatalf("submit %s: %v", d.Kind, err)
			}
		default:
			if len(d.Options) == 0 {
				t.Fatalf("empty decision %+v while driving to combat", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
				t.Fatalf("submit %s: %v", d.Kind, err)
			}
		}
	}
	t.Fatalf("did not reach seat %d's begin-combat within %d steps", p, limit)
}

// driveToTargetAsk answers priority passes and trigger-order asks (in offered
// order) until a target decision is pending, then returns it -- the drive the
// Saga chapter III copy needs once its earlier chapters have queued their own
// triggers.
func drainToTargetAsk(t *testing.T, e *Engine, limit int) *decision.Decision {
	t.Helper()
	for i := 0; i < limit && !e.G.Over; i++ {
		d := e.Pending()
		if d == nil {
			e.Advance()
			d = e.Pending()
			if d == nil {
				continue
			}
		}
		switch d.Kind {
		case decision.KPriority:
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
				t.Fatalf("submit pass: %v", err)
			}
		case decision.KTriggerOrder:
			answerTriggerOrders(t, e)
		default:
			return d
		}
	}
	t.Fatalf("never reached a target decision within %d steps", limit)
	return nil
}

// TestEleventhHourChapterIIIPrisonerZeroLegendaryAlien pins the reported card
// end to end: chapter III's copy of a targeted creature IS legendary
// (AddTypes$ Legendary) and an Alien (SetCreatureTypes$), and no longer a
// Bear. NewName$ Prisoner Zero is NOT asserted: name machinery is the
// api:Clone ticket agent-20260920T071934Z-aafa7993; extend this assertion when
// that ticket lands.
func TestEleventhHourChapterIIIPrisonerZeroLegendaryAlien(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{lookup(t, reg, "The Eleventh Hour"), lookup(t, reg, "Grizzly Bears")},
		[]*cards.Card{})
	saga := moveByName(t, e, 0, "The Eleventh Hour", state.ZBattlefield)
	bear := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)

	// Chapter I (ETB lore counter) is a library search for a Doctor card; the
	// pool holds none, so it fails to find and shuffles. Chapter II's counter
	// rides in with it and creates the Food and Human tokens; answerQuiet
	// pushes and resolves both chapters.
	e.emit(events.Event{Kind: events.CounterChange, Obj: saga, Counter: "LORE", Amount: 1})
	answerQuiet(t, e, 80)
	// Chapter III: the copy, with its target ask.
	e.emit(events.Event{Kind: events.CounterChange, Obj: saga, Counter: "LORE", Amount: 1})
	d := drainToTargetAsk(t, e, 80)
	if d.Kind != decision.KTarget {
		t.Fatalf("chapter III did not pose its target ask: %+v", d)
	}
	answerKTarget(t, e, bear)
	answerQuiet(t, e, 80)

	bearCard := e.G.Obj(bear).Card
	cid := findTokenCopyOf(t, e, bearCard, bear)
	dt := e.Derived(cid)
	if !copyHasTypeWord(dt.Types, "Legendary") {
		t.Fatalf("chapter III copy types = %v, want Legendary", dt.Types)
	}
	if !copyHasTypeWord(dt.Types, "Alien") {
		t.Fatalf("chapter III copy types = %v, want Alien", dt.Types)
	}
	if copyHasTypeWord(dt.Types, "Bear") {
		t.Fatalf("chapter III copy types = %v still Bear; SetCreatureTypes$ must replace", dt.Types)
	}
	noCopyPermanentModNote(t, e, "SetCreatureTypes", "AddTypes")
	replayCheck(t, e, cfg)
}

// TestMiragePhalanxCopyLosesSoulbondAndGainsHaste pins RemoveKeywords$ beside
// AddKeywords$ on the real corpus granted trigger: while paired, Mirage
// Phalanx grants itself a begin-combat copy that has haste and NOT soulbond,
// so RemoveKeywords$ applies before the same effect's AddKeywords$ (CR 613.1f).
func TestMiragePhalanxCopyLosesSoulbondAndGainsHaste(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{lookup(t, reg, "Mirage Phalanx"), lookup(t, reg, "Grizzly Bears")},
		[]*cards.Card{})
	mp := moveByName(t, e, 0, "Mirage Phalanx", state.ZBattlefield)
	bear := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)

	// CR 702.103: pair the two permanents (the Soulbond pairing the granted
	// trigger's Affected$ spec requires). The Pair event is the engine's own
	// pairing write.
	e.emit(events.Event{Kind: events.Pair, Obj: mp, IDs: []state.ObjID{bear}})

	// At the beginning of MY combat the granted trigger fires (seed 42's
	// starter is seat 1, so seat 0's next begin-combat is turn 2); drive
	// there, then push the queued trigger(s) and answer their asks.
	driveToCombatAnswering(t, e, 0, 400)
	e.putTriggersOnStack()
	answerOptionalYes(t, e, 80)

	mpCard := e.G.Obj(mp).Card
	cid := findTokenCopyOf(t, e, mpCard, mp)
	if !e.HasKeyword(cid, "Haste") {
		t.Fatalf("Mirage Phalanx copy has no Haste: keywords %v", e.Derived(cid).Keywords)
	}
	if e.HasKeyword(cid, "Soulbond") {
		t.Fatalf("Mirage Phalanx copy still has Soulbond: keywords %v", e.Derived(cid).Keywords)
	}
	noCopyPermanentModNote(t, e, "RemoveKeywords", "AddKeywords")
	replayCheck(t, e, cfg)
}

// TestGodPharaohsGiftCopyGainsHasteOnlyUntilEndOfTurn pins PumpKeywords$ +
// PumpDuration$ EOT on the real corpus artifact: its begin-combat copy is a
// 4/4 black Zombie with haste, and the haste is a THIS-TURN grant -- gone by
// the next turn while the copy itself persists.
func TestGodPharaohsGiftCopyGainsHasteOnlyUntilEndOfTurn(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{lookup(t, reg, "God-Pharaoh's Gift"), lookup(t, reg, "Grizzly Bears")},
		[]*cards.Card{})
	moveByName(t, e, 0, "God-Pharaoh's Gift", state.ZBattlefield)
	bear := moveByName(t, e, 0, "Grizzly Bears", state.ZGraveyard)

	// At the beginning of MY combat the optional trigger exiles the graveyard
	// creature and copies it (seed 42's starter is seat 1, so seat 0's next
	// begin-combat is turn 2). Drive there, then push the queued trigger and
	// answer its asks: the optional-yes walk answers the "you may" election
	// (option 0) and the hidden graveyard pick.
	driveToCombatAnswering(t, e, 0, 400)
	e.putTriggersOnStack()
	answerOptionalYes(t, e, 80)

	bearCard := e.G.Obj(bear).Card
	cid := findTokenCopyOf(t, e, bearCard)
	dt := e.Derived(cid)
	if dt.Power != 4 || dt.Toughness != 4 {
		t.Fatalf("GPG copy P/T = %d/%d, want 4/4", dt.Power, dt.Toughness)
	}
	if dt.Colors != "B" {
		t.Fatalf("GPG copy colours = %q, want B", dt.Colors)
	}
	if !copyHasTypeWord(dt.Types, "Zombie") {
		t.Fatalf("GPG copy types = %v, want Zombie", dt.Types)
	}
	if !e.HasKeyword(cid, "Haste") {
		t.Fatalf("GPG copy has no Haste this turn: keywords %v", dt.Keywords)
	}
	noCopyPermanentModNote(t, e, "PumpKeywords", "PumpDuration", "SetCreatureTypes")

	// Drive to the next turn's Main1: EndOfTurnCleanup drops the UntilEOT
	// haste, but the copy is NOT sacrificed (GPG has no AtEOT$), so it is
	// still on the battlefield and simply no longer hasty.
	driveToStepAll(t, e, e.G.Turn+1, 1, state.StepMain1)
	if z := e.G.Obj(cid).Zone; z != state.ZBattlefield {
		t.Fatalf("GPG copy left the battlefield (zone %s) before the expiry could be observed", z)
	}
	if e.HasKeyword(cid, "Haste") {
		t.Fatalf("GPG copy still has Haste after end of turn: keywords %v", e.Derived(cid).Keywords)
	}
	noCopyPermanentModNote(t, e, "PumpKeywords", "PumpDuration")
	replayCheck(t, e, cfg)
}
