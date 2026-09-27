// altcast_test.go — one proof test per alternative-cost keyword, each driven
// by a real card script from the ticket's table (the corpus .cards/cardsfolder
// entry, never a name-sharing fixture): Nulldrifter and Fury for evoke (mana
// and ExileFromHand costs), Ragavan for dash, Impulsive Pilferer for encore,
// Cyclonic Rift for overload, Timeline Culler for warp, Emrakul, the World
// Anew (discarded by Mind Rot) for madness, and Redirect Lightning for the
// AlternateAdditionalCost either-or.
//
// The fixtures that are NOT the card under test (the red card Fury's evoke
// exiles, the Bear Cyclonic Rift's overloaded form bounces, the Mind Rot
// discard fuel) are freely authored fixtures, never corpus .txt, per the
// licensing rule.
//
// The encore test compiles its card FRESH from cardsfolder (cards.OpenCorpus
// on a temp copy) rather than reading the shared ir.gob.gz cache: the cache's
// staleness rule is keyed on cards.lock, which a Go-side keyword-expansion
// change (cards/keywords.go) does not touch, so a cached registry decodes
// Impulsive Pilferer without the K:Encore expansion the parser now applies.
package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

const (
	altRedSrc = "Name:Ember\nManaCost:R\nTypes:Instant\n" +
		"A:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1 | SpellDescription$ Deal 1 damage.\n"
	altBearSrc          = "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	altProtectedBearSrc = "Name:Protected Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nK:Protection from Blue\nOracle:x\n"
	altElfSrc           = "Name:Test Elf\nManaCost:G\nTypes:Creature Elf\nPT:1/1\nOracle:x\n"
	altDragonSrc        = "Name:Test Dragon\nManaCost:R\nTypes:Creature Dragon\nPT:1/1\nOracle:x\n"
	altArtifactSrc      = "Name:Test Relic\nManaCost:1\nTypes:Artifact\nOracle:x\n"
)

// altCostEngine builds a two-seat engine: seat 0's deck is the named CORPUS
// cards over Mountains, seat 1's is the named fixture sources over Mountains,
// and the CR 103.1 toss is driven to start seat 0 (seatZeroStart) so the test
// can address seats and turns by index. reg is the shared compiled corpus;
// cfg carries reg.Tokens so replayCheck's genesis reproduces it.
func altCostEngine(t *testing.T, seed uint64, heroCorpus []string, heroFixtures, foeFixtures []string) (*Engine, Config, *cards.Registry) {
	t.Helper()
	return altCostEngineReg(t, seed, testutil.CorpusRegistry(t), heroCorpus, heroFixtures, foeFixtures)
}

// altCostEngineReg is altCostEngine over a caller-supplied registry (the
// fresh-compile variant uses it).
func altCostEngineReg(t *testing.T, seed uint64, reg *cards.Registry, heroCorpus []string, heroFixtures, foeFixtures []string) (*Engine, Config, *cards.Registry) {
	t.Helper()
	var hero, foe []*cards.Card
	for _, name := range heroCorpus {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("registry lacks %q", name)
		}
		hero = append(hero, c)
	}
	for _, src := range heroFixtures {
		hero = append(hero, card(t, src))
	}
	for _, src := range foeFixtures {
		foe = append(foe, card(t, src))
	}
	build := func(s uint64) Config {
		hd := append(append([]*cards.Card(nil), hero...), mountainDeck(t, 40-len(hero))...)
		fd := append(append([]*cards.Card(nil), foe...), mountainDeck(t, 40-len(foe))...)
		return Config{Seed: s, Names: []string{"a", "b"},
			Decks:  [][]*cards.Card{hd, fd},
			Tokens: reg.Tokens}
	}
	cfg := seatZeroStart(build(seed))
	e := New(cfg)
	e.Advance()
	return e, cfg, reg
}

// freshEncoreRegistry compiles a minimal corpus fresh (no ir.gob.gz cache):
// the real Impulsive Pilferer script and the Treasure token its dies trigger
// mints, copied out of the worktree's .cards corpus into a temp dir. This is
// the only way a test can see the K:Encore expansion before the shared cache
// is recompiled (see the file header).
func freshEncoreRegistry(t *testing.T) *cards.Registry {
	t.Helper()
	dir := t.TempDir()
	cf := filepath.Join(dir, "cardsfolder")
	tk := filepath.Join(dir, "tokenscripts")
	if err := os.MkdirAll(cf, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(tk, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{
		{"../.cards/cardsfolder/i/impulsive_pilferer.txt", filepath.Join(cf, "impulsive_pilferer.txt")},
		{"../.cards/tokenscripts/c_a_treasure_sac.txt", filepath.Join(tk, "c_a_treasure_sac.txt")},
	} {
		b, err := os.ReadFile(pair[0])
		if err != nil {
			t.Fatalf("corpus copy: %v", err)
		}
		if err := os.WriteFile(pair[1], b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	reg, _, err := cards.CompileDir(cf)
	if err != nil {
		t.Fatalf("fresh compile: %v", err)
	}
	return reg
}

// findCardObj locates the seeded card by face name in seat p's zones, moving
// it to `to` with a logged MoveZone when genesis shuffled it elsewhere, and
// returning its id. It drives to seat 0's Main1 first (the move happens with
// the game parked at a real priority point, exactly as moveSeeded does) and
// re-runs priorityRound after any move, so the caller's next helper sees a
// fresh decision over the moved card.
func findCardObj(t *testing.T, e *Engine, p state.PlayerID, name string, to state.Zone) state.ObjID {
	t.Helper()
	if e.G.Active == 0 {
		toMain1(t, e)
	} else {
		driveToStep(t, e, e.G.Turn+1, 0, state.StepMain1)
	}
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary, state.ZBattlefield, state.ZGraveyard} {
		for _, id := range e.G.Zone(z, p) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
				if o.Zone != to {
					e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: to})
					// The emit invalidated the parked priority decision; a
					// fresh round re-derives it so the caller's next helper
					// (addMana, the Pending() readers) sees a real decision.
					e.pending = nil
					e.priorityRound()
				}
				return id
			}
		}
	}
	t.Fatalf("card %q not found in seat %d's zones", name, p)
	return 0
}

// castModeOption returns the index of the pending priority decision's cast
// option for id with the given mode, failing the test when absent.
func castModeOption(t *testing.T, e *Engine, id state.ObjID, mode string) int {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision pending: %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id && o.Mode == mode {
			return o.Index
		}
	}
	t.Fatalf("no (%s) cast option for %s in %+v", mode, e.G.Obj(id).Face().Name, d.Options)
	return -1
}

// passOnce answers one pending priority decision with its pass option.
func passOnceP(t *testing.T, e *Engine) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("passOnce: non-priority decision %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind == "pass" {
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}); err != nil {
				t.Fatalf("submit pass: %v", err)
			}
			return
		}
	}
	t.Fatalf("priority decision with no pass option: %+v", d.Options)
}

func TestEvokeCastPaysTheEvokeCostAndSacrifices(t *testing.T) {
	e, cfg, _ := altCostEngine(t, 911, []string{"Nulldrifter"}, nil, nil)
	id := findCardObj(t, e, 0, "Nulldrifter", state.ZHand)
	// The pool added here is exactly the EVOKE cost {2}{U}, not the printed
	// {7}: the (evoked) option exists only because beginCast's "evoked" mode
	// charged the keyword cost in place of the mana cost.
	addMana(t, e, 0, "CCU")
	submitChoices(t, e, castModeOption(t, e, id, "evoked"))
	passUntilStackEmpty(t, e, 40)
	// CR 702.79a: the entry queues a real mandatory triggered ability. It is
	// respondable on the stack, then sacrifices the creature when it resolves.
	if o := e.G.Obj(id); o.Zone != state.ZGraveyard {
		t.Fatalf("evoked Nulldrifter in %s, want graveyard (CR 702.79a sacrifices it)", o.Zone)
	}
	if e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("pool after evoke %d, want 0", e.G.Players[0].Pool.Total())
	}
	sacrificed := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.Obj == id && ev.From == state.ZBattlefield &&
			ev.To == state.ZGraveyard && ev.Text == "sacrificed" {
			sacrificed = true
		}
	}
	if !sacrificed {
		t.Fatal("no evoke sacrifice MoveZone in the log")
	}
	replayCheck(t, e, cfg)
}

func TestEvokeExileCostExilesTheChosenCard(t *testing.T) {
	e, cfg, _ := altCostEngine(t, 912, []string{"Fury"}, []string{altRedSrc}, nil)
	id := findCardObj(t, e, 0, "Fury", state.ZHand)
	ember := findCardObj(t, e, 0, "Ember", state.ZHand)
	addMana(t, e, 0, "") // refresh the offer list with both cards in hand
	submitChoices(t, e, castModeOption(t, e, id, "evoked"))
	// The evoked cost is ExileFromHand<1/Card.Red+Other>: an exile-cost ask
	// whose only candidate is the fixture Ember (Fury itself is excluded by
	// the +Other self-reference, and the Mountains are not red).
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Min != 1 || d.Max != 1 ||
		len(d.Options) != 1 || d.Options[0].Kind != "exilecost" || d.Options[0].Obj != ember {
		t.Fatalf("exile-cost ask: %+v", d)
	}
	submitChoices(t, e, 0)
	passOnceP(t, e) // seat 0 passes
	passOnceP(t, e) // seat 1 passes; the evoked spell resolves onto the battlefield
	// Fury's own ETB (4 damage divided, no legal targets) and the evoke
	// sacrifice follow-up queue from the same entry: one same-controller
	// ordering ask.
	d = e.Pending()
	if d == nil || d.Kind != decision.KTriggerOrder {
		t.Fatalf("ordering ask: %+v", d)
	}
	submitChoices(t, e, 0, 1)
	// The ordered-first ETB trigger resolves into a division ask with no
	// legal candidates (TargetMin$ 0): an empty answer divides nothing.
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget || d.Min != 0 {
		t.Fatalf("Fury ETB division ask: %+v", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}); err != nil {
		t.Fatalf("empty division answer: %v", err)
	}
	passUntilStackEmpty(t, e, 40)
	// CR 702.79a: the evoked creature is sacrificed -- unconditionally.
	if o := e.G.Obj(id); o.Zone != state.ZGraveyard {
		t.Fatalf("evoked Fury in %s, want graveyard", o.Zone)
	}
	if o := e.G.Obj(ember); o.Zone != state.ZExile {
		t.Fatalf("Ember zone %s, want exile", o.Zone)
	}
	replayCheck(t, e, cfg)
}

func TestDashCommanderPaysTaxFromTheCommandZone(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	ragavan, ok := reg.Lookup("Ragavan, Nimble Pilferer")
	if !ok {
		t.Fatal("registry lacks Ragavan")
	}
	cfg := Config{Seed: 913, Names: []string{"a", "b"}, Format: FormatCommander,
		Decks: [][]*cards.Card{
			append([]*cards.Card{ragavan}, mountainDeck(t, 39)...),
			mountainDeck(t, 40),
		}, Commanders: [][]int{{0}, nil}, Tokens: reg.Tokens}
	cfg = seatZeroStart(cfg)
	e := New(cfg)
	e.Advance()
	driveToStep(t, e, 1, 0, state.StepMain1)
	id := e.G.Players[0].Commanders[0]
	// Cast once normally, then return the same commander so the second cast
	// owes {2}. This uses the real command-zone accounting event path.
	addMana(t, e, 0, "R")
	submitChoices(t, e, castModeOption(t, e, id, ""))
	passUntilStackEmpty(t, e, 40)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZCommand})
	e.pending = nil
	e.priorityRound()
	addMana(t, e, 0, "CCCR") // dash {1}{R} plus commander tax {2}
	submitChoices(t, e, castModeOption(t, e, id, "dashed"))
	passUntilStackEmpty(t, e, 40)
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("dashed command-zone Ragavan left %d mana; commander tax was not paid", got)
	}
	if o := e.G.Obj(id); o.Zone != state.ZBattlefield || o.CastFlags&state.FlagDashed == 0 {
		t.Fatalf("command-zone dash: zone=%s flags=%d", o.Zone, o.CastFlags)
	}
	replayCheck(t, e, cfg)
}

func TestDashCastsGainHasteAndReturnAtTheEndStep(t *testing.T) {
	e, cfg, _ := altCostEngine(t, 914, []string{"Ragavan, Nimble Pilferer"}, nil, nil)
	id := findCardObj(t, e, 0, "Ragavan, Nimble Pilferer", state.ZHand)
	addMana(t, e, 0, "CR")
	submitChoices(t, e, castModeOption(t, e, id, "dashed"))
	passUntilStackEmpty(t, e, 40)
	o := e.G.Obj(id)
	if o.Zone != state.ZBattlefield || o.CastFlags&state.FlagDashed == 0 {
		t.Fatalf("Ragavan zone %s flags %d", o.Zone, o.CastFlags)
	}
	if !e.HasKeyword(id, "Haste") {
		t.Fatal("dashed Ragavan has no haste")
	}
	// The end step's delayed trigger returns it to hand.
	driveToStepAll(t, e, e.G.Turn, 0, state.StepEnd)
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(id); o.Zone != state.ZHand {
		t.Fatalf("Ragavan after the end step: %s, want hand", o.Zone)
	}
	if len(e.G.Delayed) != 0 {
		t.Fatalf("dash registration not consumed: %d", len(e.G.Delayed))
	}
	replayCheck(t, e, cfg)
}

func TestEncoreActivatesFromTheGraveyardIntoHastedTokenCopies(t *testing.T) {
	reg := freshEncoreRegistry(t)
	e, cfg, _ := altCostEngineReg(t, 915, reg, []string{"Impulsive Pilferer"}, nil, nil)
	id := findCardObj(t, e, 0, "Impulsive Pilferer", state.ZGraveyard)
	// The encore cost is {3}{R} plus exiling the card itself: four mana.
	addMana(t, e, 0, "CCCR")
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority: %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == id {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no encore ability option: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 40)
	// One opponent, one hasted token copy; the card itself is exiled.
	if o := e.G.Obj(id); o.Zone != state.ZExile {
		t.Fatalf("Impulsive Pilferer zone %s, want exile", o.Zone)
	}
	tokens := 0
	var tokID state.ObjID
	for _, tid := range e.G.Zone(state.ZBattlefield, 0) {
		o := e.G.Obj(tid)
		if o.IsToken && o.Face() != nil && o.Face().Name == "Impulsive Pilferer" {
			tokens++
			tokID = tid
		}
	}
	if tokens != 1 {
		t.Fatalf("encore tokens %d, want 1", tokens)
	}
	if !e.HasKeyword(tokID, "Haste") {
		t.Fatal("encore token has no haste")
	}
	// The token must attack its corresponding opponent this turn if able.
	driveToStepAll(t, e, e.G.Turn, 0, state.StepDeclareAttackers)
	d = e.Pending()
	if d == nil || d.Kind != decision.KAttackers || len(d.Options) != 1 ||
		d.Options[0].Obj != tokID || d.Options[0].Player != 1 {
		t.Fatalf("encore attack requirement: %+v", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	// Sacrificed at the beginning of the next end step; a token that leaves
	// the battlefield ceases to exist (CR 704.5d), so the object's tombstone
	// zone is ZCeased, not the graveyard.
	driveToStepAll(t, e, e.G.Turn, 0, state.StepEnd)
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(tokID); o.Zone != state.ZCeased {
		t.Fatalf("encore token after the end step: %s, want ceased", o.Zone)
	}
	replayCheck(t, e, cfg)
}

func TestEncoreCreatesOneDelayedTriggerForAllOpponentTokens(t *testing.T) {
	reg := freshEncoreRegistry(t)
	pilferer, ok := reg.Lookup("Impulsive Pilferer")
	if !ok {
		t.Fatal("fresh registry lacks Impulsive Pilferer")
	}
	stifle, ok := testutil.CorpusRegistry(t).Lookup("Stifle")
	if !ok {
		t.Fatal("registry lacks Stifle")
	}
	decks := [][]*cards.Card{
		append([]*cards.Card{pilferer, stifle}, mountainDeck(t, 38)...),
		mountainDeck(t, 40), mountainDeck(t, 40), mountainDeck(t, 40),
	}
	cfg := seatZeroStart(Config{Seed: 936, Names: []string{"a", "b", "c", "d"},
		Decks: decks, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	id := findCardObj(t, e, 0, "Impulsive Pilferer", state.ZGraveyard)
	stifleID := findCardObj(t, e, 0, "Stifle", state.ZHand)
	addMana(t, e, 0, "CCCR")
	d := e.Pending()
	encore := -1
	for _, opt := range d.Options {
		if opt.Kind == "ability" && opt.Obj == id {
			encore = opt.Index
		}
	}
	submitChoices(t, e, encore)
	passUntilStackEmpty(t, e, 80)
	var tokens []state.ObjID
	for _, tid := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(tid); o.IsToken && o.Face() != nil && o.Face().Name == "Impulsive Pilferer" {
			tokens = append(tokens, tid)
		}
	}
	if len(tokens) != 3 || len(e.G.Delayed) != 1 || len(e.G.Delayed[0].Remembered) != 3 {
		t.Fatalf("encore group: tokens=%v delayed=%+v", tokens, e.G.Delayed)
	}
	e.pending = nil
	e.setStep(state.StepEnd)
	e.priorityRound()
	// The one group trigger is now on the stack. Fund and cast the real
	// Stifle at it; countering it must save all three tokens.
	if len(e.G.Stack) != 1 {
		t.Fatalf("encore delayed stack=%v, want one trigger", e.G.Stack)
	}
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "U", Amount: 1})
	e.pending = nil
	e.priorityRound()
	submitChoices(t, e, castModeOption(t, e, stifleID, ""))
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget || len(d.Options) != 1 {
		t.Fatalf("Stifle target over encore group: %+v", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	passUntilStackEmpty(t, e, 80)
	for _, tid := range tokens {
		if o := e.G.Obj(tid); o.Zone != state.ZBattlefield {
			t.Fatalf("Stifling the one encore trigger failed to save token %d: %s", tid, o.Zone)
		}
	}
	pushes := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.DelayedPush && ev.Counter == "__kwEncoreSacrificeGroup" {
			pushes++
		}
	}
	if pushes != 1 {
		t.Fatalf("encore emitted %d group delayed triggers, want 1", pushes)
	}
	replayCheck(t, e, cfg)
}

func TestEncoreCleanupSurvivesSourceChangingIncarnation(t *testing.T) {
	reg := freshEncoreRegistry(t)
	pilferer, ok := reg.Lookup("Impulsive Pilferer")
	if !ok {
		t.Fatal("fresh registry lacks Impulsive Pilferer")
	}
	cfg := seatZeroStart(Config{Seed: 938, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{append([]*cards.Card{pilferer}, mountainDeck(t, 39)...), mountainDeck(t, 40)}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	source := findCardObj(t, e, 0, "Impulsive Pilferer", state.ZGraveyard)
	addMana(t, e, 0, "CCCR")
	ability := -1
	for _, opt := range e.Pending().Options {
		if opt.Kind == "ability" && opt.Obj == source {
			ability = opt.Index
		}
	}
	if ability < 0 {
		t.Fatalf("Encore was not offered: %+v", e.Pending().Options)
	}
	submitChoices(t, e, ability)
	passUntilStackEmpty(t, e, 80)
	var token state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o.IsToken && o.Face() != nil && o.Face().Name == "Impulsive Pilferer" {
			token = id
		}
	}
	if token == 0 || e.G.Obj(source).Zone != state.ZExile || len(e.G.Delayed) != 1 {
		t.Fatalf("encore setup: source=%s token=%d delayed=%+v", e.G.Obj(source).Zone, token, e.G.Delayed)
	}
	// A delayed trigger exists independently of its source (CR 603.7). Make
	// the exiled source a new battlefield incarnation before the cleanup;
	// the remembered token must still be sacrificed.
	e.emit(events.Event{Kind: events.MoveZone, Obj: source, From: state.ZExile, To: state.ZBattlefield})
	// The still-live Encore token must attack before this turn can reach its
	// end step, so satisfy the independent attack requirement first.
	driveToStepAll(t, e, e.G.Turn, 0, state.StepDeclareAttackers)
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers || len(d.Options) != 1 || d.Options[0].Obj != token {
		t.Fatalf("encore attack requirement after source move: %+v", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	driveToStepAll(t, e, e.G.Turn, 0, state.StepEnd)
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(token); o.Zone != state.ZCeased {
		t.Fatalf("encore token survived after source changed incarnation: %s", o.Zone)
	}
	replayCheck(t, e, cfg)
}

func TestOverloadedCastTargetsEachNotOne(t *testing.T) {
	e, cfg, _ := altCostEngine(t, 916, []string{"Cyclonic Rift"}, nil, []string{altBearSrc, altProtectedBearSrc})
	id := findCardObj(t, e, 0, "Cyclonic Rift", state.ZHand)
	b1 := findCardObj(t, e, 1, "Bear", state.ZBattlefield)
	b2 := findCardObj(t, e, 1, "Protected Bear", state.ZBattlefield)
	if b1 == b2 {
		t.Fatal("the two Bears resolved to one object")
	}
	addMana(t, e, 0, "CCCCCCCU")
	submitChoices(t, e, castModeOption(t, e, id, "overloaded"))
	// No target decision or TargetsChosen event: overload says "each", so
	// protection from blue does not exclude the protected permanent.
	for _, ev := range e.L.Events {
		if ev.Kind == events.TargetsChosen && ev.Obj == id {
			t.Fatalf("overload emitted a targeting event: %+v", ev)
		}
	}
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(id); o.Zone != state.ZGraveyard {
		t.Fatalf("Cyclonic Rift zone %s", o.Zone)
	}
	for _, bear := range []state.ObjID{b1, b2} {
		if o := e.G.Obj(bear); o.Zone != state.ZHand {
			t.Fatalf("Bear %d in %s, want bounced to hand", bear, o.Zone)
		}
	}
	if got := len(e.G.Zone(state.ZBattlefield, 0)); got != 0 {
		t.Fatalf("seat 0 lost %d permanents to its own overloaded Rift", got)
	}
	replayCheck(t, e, cfg)
}

func TestOverloadIsCastableWhenOnlyProtectedObjectsMatch(t *testing.T) {
	e, cfg, _ := altCostEngine(t, 928, []string{"Cyclonic Rift"}, nil, []string{altProtectedBearSrc})
	id := findCardObj(t, e, 0, "Cyclonic Rift", state.ZHand)
	bear := findCardObj(t, e, 1, "Protected Bear", state.ZBattlefield)
	addMana(t, e, 0, "CCCCCCCU")
	submitChoices(t, e, castModeOption(t, e, id, "overloaded"))
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(bear); o.Zone != state.ZHand {
		t.Fatalf("protected Bear in %s, overload does not target and must bounce it", o.Zone)
	}
	replayCheck(t, e, cfg)
}

func TestWarpCastsFromTheGraveyardExileAtEndStepAndRecastFromExile(t *testing.T) {
	e, cfg, _ := altCostEngine(t, 917, []string{"Timeline Culler"}, nil, nil)
	id := findCardObj(t, e, 0, "Timeline Culler", state.ZGraveyard)
	addMana(t, e, 0, "B")
	submitChoices(t, e, castModeOption(t, e, id, "warped"))
	passUntilStackEmpty(t, e, 40)
	o := e.G.Obj(id)
	if o.Zone != state.ZBattlefield || o.CastFlags&state.FlagWarped == 0 {
		t.Fatalf("Timeline Culler zone %s flags %d", o.Zone, o.CastFlags)
	}
	// The warp cost's two life.
	if life := e.G.Players[0].Life; life != 18 {
		t.Fatalf("life after warp %d, want 18", life)
	}
	// Exiled at the beginning of the next end step.
	driveToStepAll(t, e, e.G.Turn, 0, state.StepEnd)
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(id); o.Zone != state.ZExile {
		t.Fatalf("Timeline Culler after the end step: %s, want exile", o.Zone)
	}
	// On a later turn, the recast from exile uses the NORMAL {B}{B} cost and
	// is not warped again, so it receives no second end-step exile trigger.
	driveToStepAll(t, e, e.G.Turn+2, 0, state.StepMain1)
	addMana(t, e, 0, "BB")
	submitChoices(t, e, castModeOption(t, e, id, "warp_recast"))
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(id); o.Zone != state.ZBattlefield || o.CastFlags&state.FlagWarped != 0 {
		t.Fatalf("Timeline Culler after normal recast: zone=%s flags=%d", o.Zone, o.CastFlags)
	}
	driveToStepAll(t, e, e.G.Turn, 0, state.StepEnd)
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(id); o.Zone != state.ZBattlefield {
		t.Fatalf("Timeline Culler after the later end step: %s, want battlefield", o.Zone)
	}
	// An unrelated later exile must not reuse the historical warp trigger.
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield,
		To: state.ZExile, Text: "exiled by unrelated removal"})
	driveToStepAll(t, e, e.G.Turn+2, 0, state.StepMain1)
	addMana(t, e, 0, "BB")
	for _, opt := range e.Pending().Options {
		if opt.Obj == id && opt.Mode == "warp_recast" {
			t.Fatalf("unrelated re-exile reused old warp provenance: %+v", opt)
		}
	}
	replayCheck(t, e, cfg)
}

func TestMadnessEmrakulUsesOptionalReplacementAndRespondableTrigger(t *testing.T) {
	e, cfg, _ := altCostEngine(t, 918, []string{"Emrakul, the World Anew", "Mind Rot"}, []string{altRedSrc}, nil)
	emrakul := findCardObj(t, e, 0, "Emrakul, the World Anew", state.ZHand)
	ember := findCardObj(t, e, 0, "Ember", state.ZHand)
	// {2}{B} for Mind Rot followed by Emrakul's six-{C} madness cost.
	addMana(t, e, 0, "CCCCCCCCB")
	mr := findCardObj(t, e, 0, "Mind Rot", state.ZHand)
	submitChoices(t, e, castModeOption(t, e, mr, ""))
	d := e.Pending()
	seat0 := -1
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == 0 {
			seat0 = o.Index
		}
	}
	submitChoices(t, e, seat0)
	passOnceP(t, e)
	passOnceP(t, e)
	d = e.Pending()
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("Mind Rot discard choice: %+v", d)
	}
	emrakulIdx, emberIdx := -1, -1
	for _, o := range d.Options {
		if o.Obj == emrakul {
			emrakulIdx = o.Index
		}
		if o.Obj == ember {
			emberIdx = o.Index
		}
	}
	submitChoices(t, e, emrakulIdx, emberIdx)
	// CR 702.35a: Madness first offers an optional replacement. Nothing has
	// moved until the owner accepts it.
	d = e.Pending()
	if d == nil || d.Kind != decision.KReplacement || e.G.Obj(emrakul).Zone != state.ZHand {
		t.Fatalf("madness replacement: pending=%+v zone=%s", d, e.G.Obj(emrakul).Zone)
	}
	submitChoices(t, e, 0) // exile instead of putting it in the graveyard
	if e.G.Obj(emrakul).Zone != state.ZExile || e.G.Obj(ember).Zone != state.ZGraveyard {
		t.Fatalf("discard destinations: Emrakul=%s Ember=%s", e.G.Obj(emrakul).Zone, e.G.Obj(ember).Zone)
	}
	// CR 702.35b: accepting the replacement creates one real triggered
	// ability on the stack. Priority exists before its cast choice.
	d = e.Pending()
	if d == nil || d.Kind != decision.KPriority || len(e.G.Stack) != 1 {
		t.Fatalf("priority over madness trigger: pending=%+v stack=%v", d, e.G.Stack)
	}
	ability := e.G.Obj(e.G.Stack[0])
	if ability == nil || ability.Ability == nil || ability.Ability.API != "MadnessCast" || ability.Source != emrakul {
		t.Fatalf("madness stack ability: %+v", ability)
	}
	passOnceP(t, e)
	passOnceP(t, e)
	d = e.Pending()
	if d == nil || d.Kind != decision.KTriggerOptional || d.Source != emrakul {
		t.Fatalf("madness resolution choice: %+v", d)
	}
	yes := -1
	for _, o := range d.Options {
		if o.Kind == "yes" {
			yes = o.Index
		}
	}
	if yes < 0 {
		t.Fatalf("funded six-C madness cast not offered: %+v", d.Options)
	}
	submitChoices(t, e, yes)
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool after Mind Rot and six-C madness = %d, want 0", got)
	}
	if o := e.G.Obj(emrakul); o.Zone != state.ZStack {
		t.Fatalf("six-C madness cast left Emrakul in %s, want stack", o.Zone)
	}
	replayCheck(t, e, cfg)
}

func TestMadnessTriggerCanBeStifled(t *testing.T) {
	e, cfg, _ := altCostEngine(t, 937, []string{"Emrakul, the World Anew", "Stifle"}, nil, nil)
	emrakul := findCardObj(t, e, 0, "Emrakul, the World Anew", state.ZHand)
	stifle := findCardObj(t, e, 0, "Stifle", state.ZHand)
	addMana(t, e, 0, "U")
	e.pending = nil
	e.emit(events.Discard(emrakul, 0))
	submitChoices(t, e, 0) // accept the exile replacement
	if len(e.G.Stack) != 1 || e.G.Obj(e.G.Stack[0]).Ability.API != "MadnessCast" {
		t.Fatalf("madness did not create one stack trigger: %v", e.G.Stack)
	}
	trigger := e.G.Stack[0]
	submitChoices(t, e, castModeOption(t, e, stifle, ""))
	d := e.Pending()
	target := -1
	for _, opt := range d.Options {
		if opt.Obj == trigger {
			target = opt.Index
		}
	}
	if target < 0 {
		t.Fatalf("Stifle cannot target madness trigger: %+v", d)
	}
	submitChoices(t, e, target)
	passUntilStackEmpty(t, e, 60)
	if o := e.G.Obj(emrakul); o.Zone != state.ZExile {
		t.Fatalf("Stifled madness trigger moved Emrakul to %s", o.Zone)
	}
	replayCheck(t, e, cfg)
}

func TestMadnessReplacementMayBeDeclined(t *testing.T) {
	e, cfg, _ := altCostEngine(t, 934, []string{"Emrakul, the World Anew"}, nil, nil)
	id := findCardObj(t, e, 0, "Emrakul, the World Anew", state.ZHand)
	// This is the event every discard implementation proposes; the real
	// Emrakul keyword supplies the replacement being tested.
	e.pending = nil
	e.emit(events.Discard(id, 0))
	d := e.Pending()
	if d == nil || d.Kind != decision.KReplacement || e.G.Obj(id).Zone != state.ZHand {
		t.Fatalf("optional madness replacement: pending=%+v zone=%s", d, e.G.Obj(id).Zone)
	}
	submitChoices(t, e, 1) // decline exile
	if o := e.G.Obj(id); o.Zone != state.ZGraveyard {
		t.Fatalf("declined madness replacement moved Emrakul to %s", o.Zone)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.KeywordTriggerPush && ev.Obj == id {
			t.Fatal("declined madness replacement created a trigger")
		}
	}
	replayCheck(t, e, cfg)
}

func TestMadnessExileByNonDiscardDoesNotOfferTheCast(t *testing.T) {
	// CR 702.35a opens the cast window on a DISCARD only. Evoking Fury exiles
	// a red card as a COST -- here the real madness card Fiery Temper -- and
	// that exile must not offer the Temper's madness cast.
	e, cfg, _ := altCostEngine(t, 922, []string{"Fury", "Fiery Temper"}, nil, nil)
	id := findCardObj(t, e, 0, "Fury", state.ZHand)
	temper := findCardObj(t, e, 0, "Fiery Temper", state.ZHand)
	// Mind Rot is in the deck to keep Fury from being the only eligible red
	// card: the exile-cost ask's options must include the Temper.
	addMana(t, e, 0, "")
	submitChoices(t, e, castModeOption(t, e, id, "evoked"))
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Min != 1 || d.Max != 1 {
		t.Fatalf("exile-cost ask: %+v", d)
	}
	temperIdx := -1
	for _, o := range d.Options {
		if o.Obj == temper {
			temperIdx = o.Index
		}
	}
	if temperIdx < 0 {
		t.Fatalf("exile options missing the Temper: %+v", d.Options)
	}
	submitChoices(t, e, temperIdx) // exile the Temper as the evoke cost
	passOnceP(t, e)
	passOnceP(t, e) // the evoked Fury resolves onto the battlefield
	// The evoke sacrifice and Fury's ETB share one ordering ask; both become
	// real triggered abilities on the stack.
	d = e.Pending()
	if d == nil || d.Kind != decision.KTriggerOrder {
		t.Fatalf("ordering ask: %+v", d)
	}
	submitChoices(t, e, 0, 1)
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget || d.Min != 0 {
		t.Fatalf("Fury ETB division ask: %+v", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}); err != nil {
		t.Fatalf("empty division answer: %v", err)
	}
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(id); o.Zone != state.ZGraveyard {
		t.Fatalf("evoked Fury in %s, want graveyard", o.Zone)
	}
	if o := e.G.Obj(temper); o.Zone != state.ZExile {
		t.Fatalf("Temper zone %s, want exile", o.Zone)
	}
	// And no madness cast offer was ever posed: the only asks after the
	// cost exile are the ordering and division asks and priority.
	for _, ev := range e.L.Events {
		if ev.Kind == events.DecisionAsk && ev.Text == string(decision.KTriggerOptional) {
			t.Fatalf("bogus madness cast offer in the log: %+v", ev)
		}
	}
	replayCheck(t, e, cfg)
}

func TestEvokeSacrificeIsARespondableTriggeredAbility(t *testing.T) {
	e, cfg, _ := altCostEngine(t, 923, []string{"Nulldrifter", "Stifle"}, nil, nil)
	null := findCardObj(t, e, 0, "Nulldrifter", state.ZHand)
	stifle := findCardObj(t, e, 0, "Stifle", state.ZHand)
	addMana(t, e, 0, "CCCUU")
	submitChoices(t, e, castModeOption(t, e, null, "evoked"))
	// Pass only until the evoked creature enters and its keyword trigger is
	// on the stack. Other merged trigger work may add a priority handoff, so
	// do not bake a particular number of passes into this proof.
	var evokeAbility state.ObjID
	for i := 0; i < 8; i++ {
		for _, stackID := range e.G.Stack {
			o := e.G.Obj(stackID)
			if o != nil && o.Ability != nil && o.Source == null && o.Ability.API == "Sacrifice" {
				evokeAbility = stackID
				break
			}
		}
		if evokeAbility != 0 {
			break
		}
		passOnceP(t, e)
	}
	if evokeAbility == 0 {
		t.Fatalf("no evoke trigger after entry: stack=%v", e.G.Stack)
	}
	if o := e.G.Obj(evokeAbility); o == nil || o.Ability == nil || o.Source != null {
		t.Fatalf("evoke stack object: %+v", o)
	}
	// Cast the real Stifle and target the evoke trigger.
	d := e.Pending()
	idx := -1
	for _, opt := range d.Options {
		if opt.Kind == "cast" && opt.Obj == stifle {
			idx = opt.Index
		}
	}
	if idx < 0 {
		t.Fatalf("Stifle not offered: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	d = e.Pending()
	target := -1
	for _, opt := range d.Options {
		if opt.Obj == evokeAbility {
			target = opt.Index
		}
	}
	if target < 0 {
		t.Fatalf("Stifle cannot target evoke trigger: %+v", d)
	}
	submitChoices(t, e, target)
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(null); o.Zone != state.ZBattlefield {
		t.Fatalf("Stifled evoke moved Nulldrifter to %s", o.Zone)
	}
	replayCheck(t, e, cfg)
}

func TestDashDelayedReturnDoesNotFollowABlinkedPermanent(t *testing.T) {
	e, cfg, _ := altCostEngine(t, 924, []string{"Ragavan, Nimble Pilferer"}, nil, nil)
	id := findCardObj(t, e, 0, "Ragavan, Nimble Pilferer", state.ZHand)
	addMana(t, e, 0, "CR")
	submitChoices(t, e, castModeOption(t, e, id, "dashed"))
	passUntilStackEmpty(t, e, 40)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZExile})
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZExile, To: state.ZBattlefield})
	driveToStepAll(t, e, e.G.Turn, 0, state.StepEnd)
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(id); o.Zone != state.ZBattlefield {
		t.Fatalf("dash trigger followed the blinked object to %s", o.Zone)
	}
	replayCheck(t, e, cfg)
}

func TestWarpDoesNotGrantEveryWarpCardGraveyardPermission(t *testing.T) {
	e, _, _ := altCostEngine(t, 925, []string{"Network Marauder"}, nil, nil)
	id := findCardObj(t, e, 0, "Network Marauder", state.ZGraveyard)
	addMana(t, e, 0, "CU")
	for _, opt := range e.Pending().Options {
		if opt.Kind == "cast" && opt.Obj == id && opt.Mode == "warped" {
			t.Fatalf("ordinary Warp card offered from graveyard: %+v", opt)
		}
	}
}

func TestSpiritGuideActivatesFromHandAndPaysItsExileCost(t *testing.T) {
	e, cfg, _ := altCostEngine(t, 935, []string{"Elvish Spirit Guide"}, []string{altElfSrc}, nil)
	guide := findCardObj(t, e, 0, "Elvish Spirit Guide", state.ZHand)
	elf := findCardObj(t, e, 0, "Test Elf", state.ZHand)
	d := e.Pending()
	activate := -1
	for _, opt := range d.Options {
		if opt.Kind == "activate" && opt.Obj == guide {
			activate = opt.Index
		}
	}
	if activate < 0 {
		t.Fatalf("hand-zone Spirit Guide not offered: %+v", d.Options)
	}
	submitChoices(t, e, activate)
	// CARDNAME leaves exactly one legal cost object, so the synchronous mana
	// path pays it without a meaningless singleton choice.
	if e.G.Obj(guide).Zone != state.ZExile || e.G.Players[0].Pool[state.MG] != 1 {
		t.Fatalf("Spirit Guide payment: zone=%s pool=%+v", e.G.Obj(guide).Zone, e.G.Players[0].Pool)
	}
	// Spend the produced mana through the real cast payment path.
	submitChoices(t, e, castModeOption(t, e, elf, ""))
	if e.G.Players[0].Pool.Total() != 0 || e.G.Obj(elf).Zone != state.ZStack {
		t.Fatalf("Spirit Guide mana not spent: pool=%+v elf=%s", e.G.Players[0].Pool, e.G.Obj(elf).Zone)
	}
	replayCheck(t, e, cfg)
}

func TestManaAbilityExileCostIsPaid(t *testing.T) {
	e, cfg, _ := altCostEngine(t, 926, []string{"Cadaverous Bloom"}, []string{altRedSrc}, nil)
	bloom := findCardObj(t, e, 0, "Cadaverous Bloom", state.ZBattlefield)
	if got := e.AvailableMana(0).Total(); got != 0 {
		t.Fatalf("exile-cost Bloom advertised as %d free available mana", got)
	}
	ember := findCardObj(t, e, 0, "Ember", state.ZHand)
	d := e.Pending()
	activate := -1
	for _, opt := range d.Options {
		if opt.Kind == "activate" && opt.Obj == bloom {
			activate = opt.Index
		}
	}
	if activate < 0 {
		t.Fatalf("Cadaverous Bloom mana ability not offered: %+v", d.Options)
	}
	submitChoices(t, e, activate)
	// Choose its black-mana ability, then the real hand card to exile.
	d = e.Pending()
	black := -1
	for _, opt := range d.Options {
		if opt.Label == "Exile a card from your hand: Add BB" {
			black = opt.Index
		}
	}
	submitChoices(t, e, black)
	d = e.Pending()
	exile := -1
	for _, opt := range d.Options {
		if opt.Obj == ember {
			exile = opt.Index
		}
	}
	if exile < 0 {
		t.Fatalf("mana exile ask omits Ember: %+v", d)
	}
	submitChoices(t, e, exile)
	if o := e.G.Obj(ember); o.Zone != state.ZExile {
		t.Fatalf("mana cost left Ember in %s", o.Zone)
	}
	if got := e.G.Players[0].Pool[state.MB]; got != 2 {
		t.Fatalf("Bloom added %d black mana, want 2", got)
	}
	replayCheck(t, e, cfg)
}

func TestAlternateAdditionalCostGrammarIsNotGenericMana(t *testing.T) {
	tests := []struct {
		raw string
		ok  func(Cost) bool
	}{
		{"Reveal<1/Elf>", func(c Cost) bool { return len(c.Reveal) == 1 }},
		{"Behold<1/Dragon>", func(c Cost) bool { return len(c.Behold) == 1 }},
		{"Blight<2>", func(c Cost) bool { return len(c.Blight) == 1 && c.Blight[0].N == 2 }},
		{"Forage", func(c Cost) bool { return c.Forage }},
		{"tapXType<1/Artifact>", func(c Cost) bool { return len(c.TapPermanent) == 1 }},
	}
	for _, tc := range tests {
		c := ParseCost(tc.raw)
		if c.Generic != 0 || !tc.ok(c) {
			t.Errorf("ParseCost(%q) = %+v", tc.raw, c)
		}
	}
}

func TestAlternateAdditionalCostSpecialPayments(t *testing.T) {
	t.Parallel()
	chooseKind := func(t *testing.T, e *Engine, kind string) {
		t.Helper()
		d := e.Pending()
		for _, opt := range d.Options {
			if opt.Kind == kind {
				submitChoices(t, e, opt.Index)
				return
			}
		}
		t.Fatalf("no %s option in %+v", kind, d)
	}
	chooseObj := func(t *testing.T, e *Engine, id state.ObjID) {
		t.Helper()
		d := e.Pending()
		for _, opt := range d.Options {
			if opt.Obj == id {
				submitChoices(t, e, opt.Index)
				return
			}
		}
		t.Fatalf("no object %d option in %+v", id, d)
	}

	t.Run("behold", func(t *testing.T) {
		e, _, _ := altCostEngine(t, 930, []string{"Caustic Exhale"}, []string{altDragonSrc}, []string{altBearSrc})
		spell := findCardObj(t, e, 0, "Caustic Exhale", state.ZHand)
		dragon := findCardObj(t, e, 0, "Test Dragon", state.ZHand)
		bear := findCardObj(t, e, 1, "Bear", state.ZBattlefield)
		addMana(t, e, 0, "B")
		submitChoices(t, e, castModeOption(t, e, spell, ""))
		chooseKind(t, e, "altaddcost") // behold is the only payable option
		chooseObj(t, e, bear)
		if e.G.Obj(dragon).Zone != state.ZHand {
			t.Fatal("behold moved the revealed Dragon")
		}
	})

	t.Run("blight", func(t *testing.T) {
		e, _, _ := altCostEngine(t, 931, []string{"Bogslither's Embrace"}, []string{altBearSrc}, []string{altBearSrc})
		spell := findCardObj(t, e, 0, "Bogslither's Embrace", state.ZHand)
		mine := findCardObj(t, e, 0, "Bear", state.ZBattlefield)
		theirs := findCardObj(t, e, 1, "Bear", state.ZBattlefield)
		addMana(t, e, 0, "CB")
		submitChoices(t, e, castModeOption(t, e, spell, ""))
		chooseKind(t, e, "altaddcost")
		chooseObj(t, e, theirs)
		if got := e.G.Obj(mine).Counter("M1M1"); got != 1 {
			t.Fatalf("blight counters = %d, want 1", got)
		}
	})

	t.Run("forage", func(t *testing.T) {
		e, _, _ := altCostEngine(t, 932, []string{"Feed the Cycle"}, []string{altRedSrc, altElfSrc, altDragonSrc}, []string{altBearSrc})
		spell := findCardObj(t, e, 0, "Feed the Cycle", state.ZHand)
		var fuel []state.ObjID
		for _, name := range []string{"Ember", "Test Elf", "Test Dragon"} {
			fuel = append(fuel, findCardObj(t, e, 0, name, state.ZGraveyard))
		}
		bear := findCardObj(t, e, 1, "Bear", state.ZBattlefield)
		addMana(t, e, 0, "CB")
		submitChoices(t, e, castModeOption(t, e, spell, ""))
		chooseKind(t, e, "altaddcost")
		chooseKind(t, e, "forage_exile")
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose || d.Min != 3 {
			t.Fatalf("forage exile ask: %+v", d)
		}
		submitChoices(t, e, d.Options[0].Index, d.Options[1].Index, d.Options[2].Index)
		chooseObj(t, e, bear)
		for _, id := range fuel {
			if e.G.Obj(id).Zone != state.ZExile {
				t.Fatalf("forage fuel %d not exiled", id)
			}
		}
	})

	t.Run("tap artifact", func(t *testing.T) {
		e, _, _ := altCostEngine(t, 933, []string{"Disruption Protocol"}, []string{altArtifactSrc}, []string{altRedSrc})
		spell := findCardObj(t, e, 0, "Disruption Protocol", state.ZHand)
		relic := findCardObj(t, e, 0, "Test Relic", state.ZBattlefield)
		shock := findCardObj(t, e, 1, "Ember", state.ZHand)
		e.emit(events.Event{Kind: events.PutOnStack, Obj: shock, Player: 1, From: state.ZHand, To: state.ZStack})
		e.pending = nil
		e.priorityRound()
		addMana(t, e, 0, "UU")
		submitChoices(t, e, castModeOption(t, e, spell, ""))
		chooseKind(t, e, "altaddcost")
		chooseObj(t, e, shock)
		if !e.G.Obj(relic).Tapped {
			t.Fatal("tap-artifact additional cost did not tap the relic")
		}
	})
}

func TestAlternateAdditionalCostRevealPaysWithARealCard(t *testing.T) {
	e, cfg, _ := altCostEngine(t, 929, []string{"Wren's Run Vanquisher"}, []string{altElfSrc}, nil)
	vanquisher := findCardObj(t, e, 0, "Wren's Run Vanquisher", state.ZHand)
	elf := findCardObj(t, e, 0, "Test Elf", state.ZHand)
	addMana(t, e, 0, "CG")
	submitChoices(t, e, castModeOption(t, e, vanquisher, ""))
	// Choose the Reveal branch rather than {3}; its sole matching Elf is a
	// forced selection and is publicly named by the payment event.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "altaddcost" {
		t.Fatalf("reveal-or-pay ask: %+v", d)
	}
	reveal := -1
	for _, opt := range d.Options {
		if opt.Kind == "altaddcost" && strings.Contains(opt.Label, "Reveal") {
			reveal = opt.Index
		}
		if strings.ContainsAny(opt.Label, "<>") {
			t.Fatalf("altaddcost option %q leaks raw cost syntax", opt.Label)
		}
	}
	submitChoices(t, e, reveal)
	if o := e.G.Obj(elf); o.Zone != state.ZHand {
		t.Fatalf("revealed Elf moved to %s", o.Zone)
	}
	found := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && ev.Text == "revealed Test Elf as a cost" {
			found = true
		}
	}
	if !found {
		t.Fatal("reveal payment was not publicly recorded")
	}
	replayCheck(t, e, cfg)
}

func TestAlternateAdditionalCostSinglePartIsMandatory(t *testing.T) {
	e, cfg, _ := altCostEngine(t, 927, []string{"Dusk Rose Reliquary"}, []string{altBearSrc}, nil)
	reliquary := findCardObj(t, e, 0, "Dusk Rose Reliquary", state.ZHand)
	bear := findCardObj(t, e, 0, "Bear", state.ZBattlefield)
	addMana(t, e, 0, "W")
	submitChoices(t, e, castModeOption(t, e, reliquary, ""))
	// There is no either-or ask for this one-part form; the ordinary
	// sacrifice-cost chooser is mandatory.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 1 || d.Options[0].Obj != bear {
		t.Fatalf("single-part sacrifice ask: %+v", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	if o := e.G.Obj(bear); o.Zone != state.ZGraveyard {
		t.Fatalf("single-part sacrifice left Bear in %s", o.Zone)
	}
	if o := e.G.Obj(reliquary); o.Zone != state.ZStack {
		t.Fatalf("Reliquary after payment in %s, want stack", o.Zone)
	}
	replayCheck(t, e, cfg)
}

func TestAlternateAdditionalCostAsksWhichAlternative(t *testing.T) {
	e, cfg, _ := altCostEngine(t, 919, []string{"Redirect Lightning", "Shock"}, nil, []string{altBearSrc})
	rl := findCardObj(t, e, 0, "Redirect Lightning", state.ZHand)
	shock := findCardObj(t, e, 0, "Shock", state.ZHand)
	bear := findCardObj(t, e, 1, "Bear", state.ZBattlefield)
	addMana(t, e, 0, "RRRR")
	d := e.Pending()
	shockIdx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == shock && o.Mode == "" {
			shockIdx = o.Index
		}
	}
	if shockIdx < 0 {
		t.Fatalf("no Shock cast option: %+v", d.Options)
	}
	submitChoices(t, e, shockIdx)
	// Shock's target ask: the Bear.
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("Shock target ask: %+v", d)
	}
	bearIdx := -1
	for _, o := range d.Options {
		if o.Obj == bear {
			bearIdx = o.Index
		}
	}
	submitChoices(t, e, bearIdx)
	// Seat 0 kept priority with Shock still on the stack: cast Redirect
	// Lightning. Its either-or additional cost asks which one to pay.
	d = e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority with Shock on the stack: %+v", d)
	}
	rlIdx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == rl && o.Mode == "" {
			rlIdx = o.Index
		}
	}
	if rlIdx < 0 {
		t.Fatalf("no Redirect Lightning cast option: %+v", d.Options)
	}
	submitChoices(t, e, rlIdx)
	// The either-or ask: both alternatives payable, script order (5 life
	// first), then the {2} alternative.
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 2 ||
		d.Options[0].Kind != "altaddcost" || d.Options[0].Label != "Pay 5 life" ||
		d.Options[1].Label != "Pay 2" {
		t.Fatalf("alternate-additional-cost ask: %+v", d)
	}
	submitChoices(t, e, 1) // pay {2}
	// Then Redirect Lightning's own target: the Shock on the stack.
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("Redirect Lightning target ask: %+v", d)
	}
	stackIdx := -1
	for _, o := range d.Options {
		if o.Obj == shock {
			stackIdx = o.Index
		}
	}
	if stackIdx < 0 {
		t.Fatalf("Redirect Lightning options missing the Shock on the stack: %+v", d.Options)
	}
	submitChoices(t, e, stackIdx)
	// ChangeTargets is now implemented independently on main, so resolve its
	// newly exposed target ask rather than treating it as this cost test's
	// outcome. The alternative-cost proof owns the payment assertions below.
	for i := 0; i < 40 && len(e.G.Stack) != 0; i++ {
		d = e.Pending()
		if d != nil && d.Kind == decision.KChoose && d.Prompt == "Choose new target" {
			keep := -1
			for _, opt := range d.Options {
				if opt.Obj == bear {
					keep = opt.Index
				}
			}
			if keep < 0 {
				t.Fatalf("redirect options omit original Bear target: %+v", d.Options)
			}
			submitChoices(t, e, keep)
			continue
		}
		passOnceP(t, e)
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack did not drain: %v", e.G.Stack)
	}
	if pool := e.G.Players[0].Pool.Total(); pool != 0 {
		t.Fatalf("pool after the {2} alternative %d, want 0", pool)
	}
	if life := e.G.Players[0].Life; life != 20 {
		t.Fatalf("life %d, want 20 (the life alternative was not chosen)", life)
	}
	replayCheck(t, e, cfg)
}
