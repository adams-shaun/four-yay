package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Behaviour leaves for the remembered/triggered-referents wave: the general
// filter's IsRemembered and greatestPowerControlledByRemembered (gap 1),
// Defined$ TriggeredTarget[LKICopy] (gap 2), the Discard AnyNumber$ and
// RememberDiscarded$ riders (gap 3), the GainControl RememberControlled$ and
// NewController$ Player.withMostLife riders (gap 4), the
// Count$ThisTurnEntered_<Dest>_from_<Origin>_<Valid> count head (gap 5), and
// the choice-replaces-its-kind contract (gap 6). Every card here is a real
// compiled corpus card; the hand-driven resolutions follow the
// choose_control_regression_test pattern, the combat ones the
// combat_damage_trigger_test pattern.

// TestMyrkulsEdictChosenGreatestPowerIsSacrificed drives Myrkul's Edict's
// mode-20 chain on its compiled SVar: RepeatEach per opponent binds the
// opponent as Remembered, DBChooseCard offers ONLY the creatures with the
// greatest power among that player's (greatestPowerControlledByRemembered --
// ties offered, a lesser one never offered), RememberChosen$ binds the
// answer, and DBSacAll's SacrificeAll | ValidCards$ Card.IsRemembered
// sacrifices exactly the chosen card.
func TestMyrkulsEdictChosenGreatestPowerIsSacrificed(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	edict := mustCorpusCard(t, reg, "Myrkul's Edict")
	big := mustCorpusCard(t, reg, "Hill Giant")
	small := mustCorpusCard(t, reg, "Grizzly Bears")
	e := New(Config{Seed: 11, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}})
	src := e.G.AddObject(edict, 0)
	bigObj := e.G.AddObject(big, 1)
	smallObj := e.G.AddObject(small, 1)
	for _, id := range []state.ObjID{src.ID, bigObj.ID, smallObj.ID} {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZBattlefield})
	}
	// The resolving spell sits on the stack: the engine's suspension
	// machinery only resumes a mid-resolution ask whose object is still
	// there (rp.obj's zone gate in resumeResolution).
	e.emit(events.Event{Kind: events.MoveZone, Obj: src.ID, From: state.ZBattlefield, To: state.ZStack})
	// The mode-20 chain head: RepeatEach per opponent binds the opponent as
	// Remembered, poses the DBChooseCard ask per opponent, and chains
	// DBSacAll off the loop's SubAbility$ -- so the whole SVar must run, not
	// DBChooseCard alone (the sacrifice rider lives on SacTopPower, not on
	// DBChooseCard, which carries no SubAbility$).
	sa := cards.ResolveSVar(src.Face().SVars, "SacTopPower")
	if sa == nil {
		t.Fatal("Myrkul's Edict has no SacTopPower SVar")
	}
	ctx := &effects.Ctx{Source: src.ID, Controller: 0, SVars: src.Face().SVars}
	// A hand-driven effects.Resolve must link the enclosing-loop
	// continuation exactly as resolveTop does (rules/stack.go): the ask
	// inside the RepeatEach suspends, and the repeat cursor that re-enters
	// the loop and then chains DBSacAll lives in e.contChain.
	e.contChain = e.contChain[:0]
	e.repeatReported = nil
	effects.Resolve(e, ctx, sa)
	if e.resume != nil {
		e.resume.outer = e.buildContinuationChain(e.contChain, src.ID, nil)
	}

	// The choice ask went to the remembered player, and the pool holds ONLY
	// the greatest-power creature: the 3-power Hill Giant, never the 2-power
	// Bear (a lesser creature is not "the greatest").
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected the ChooseCard ask, got %+v", d)
	}
	if d.Player != 1 {
		t.Fatalf("chooser = seat %d, want the remembered player seat 1", d.Player)
	}
	if len(d.Options) != 1 || d.Options[0].Obj != bigObj.ID {
		t.Fatalf("greatest-power pool = %+v, want only the Hill Giant", d.Options)
	}
	submitChoices(t, e, d.Options[0].Index)
	passUntilStackEmpty(t, e, 10)

	// The follow-up SacrificeAll Card.IsRemembered hit exactly the chosen
	// card: Hill Giant in the graveyard, Bear untouched on the battlefield.
	if z := e.G.Obj(bigObj.ID).Zone; z != state.ZGraveyard {
		t.Fatalf("chosen greatest-power creature zone = %s, want Graveyard", z)
	}
	if z := e.G.Obj(smallObj.ID).Zone; z != state.ZBattlefield {
		t.Fatalf("the lesser creature must be untouched, zone %s", z)
	}
}

// TestMyrkulsEdictGreatestPowerTiesAreAllOffered pins the tie half of Forge's
// greatestPower: every creature at the maximum power matches, so two 3-power
// creatures are BOTH offered and the answer picks one of them.
func TestMyrkulsEdictGreatestPowerTiesAreAllOffered(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	edict := mustCorpusCard(t, reg, "Myrkul's Edict")
	giant := mustCorpusCard(t, reg, "Hill Giant")
	e := New(Config{Seed: 12, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}})
	src := e.G.AddObject(edict, 0)
	first := e.G.AddObject(giant, 1)
	second := e.G.AddObject(giant, 1)
	for _, id := range []state.ObjID{src.ID, first.ID, second.ID} {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZBattlefield})
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: src.ID, From: state.ZBattlefield, To: state.ZStack})
	sa := cards.ResolveSVar(src.Face().SVars, "SacTopPower")
	if sa == nil {
		t.Fatal("Myrkul's Edict has no SacTopPower SVar")
	}
	ctx := &effects.Ctx{Source: src.ID, Controller: 0, SVars: src.Face().SVars}
	e.contChain = e.contChain[:0]
	e.repeatReported = nil
	effects.Resolve(e, ctx, sa)
	if e.resume != nil {
		e.resume.outer = e.buildContinuationChain(e.contChain, src.ID, nil)
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected the ChooseCard ask, got %+v", d)
	}
	if len(d.Options) != 2 {
		t.Fatalf("two tied greatest-power creatures must both be offered, got %+v", d.Options)
	}
	submitChoices(t, e, d.Options[0].Index)
	passUntilStackEmpty(t, e, 10)
	if e.G.Obj(d.Options[0].Obj).Zone != state.ZGraveyard {
		t.Fatal("the answered greatest-power creature must be sacrificed")
	}
	other := d.Options[1].Obj
	if e.G.Obj(other).Zone != state.ZBattlefield {
		t.Fatal("the un-chosen tied creature must survive")
	}
}

// TestKainRememberControlledFeedsTheDrawGate runs Kain, Traitorous Dragoon's
// real combat: when it deals combat damage to a player, that player gains
// control of Kain (NewController$ TriggeredTarget -- the DamageDone event's
// recipient), RememberControlled$ binds the gained permanent, and the chained
// draw's ConditionDefined$ Remembered GE1 gate therefore passes: seat 0 draws
// exactly the damage dealt.
func TestKainRememberControlledFeedsTheDrawGate(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := combatTriggerBoard(t, reg, []string{"Kain, Traitorous Dragoon"},
		[]string{"Name:Ox\nManaCost:1 G\nTypes:Creature Ox\nPT:2/4\nOracle:x\n"}, nil, nil)
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepDeclareAttackers})
	kain := findBattlefield(t, e, 0, "Kain, Traitorous Dragoon", 0)
	ox := findBattlefield(t, e, 0, "Ox", 0)
	_ = ox

	before := countDraws(e, 0)
	e.askAttackers()
	submitAttackers(t, e, kain)
	drainCombatDamagePriority(t, e)
	passUntilStackEmpty(t, e, 30)

	// Kain dealt its 2 power to seat 1: seat 1 gained control, and the
	// remembered-gate draw fired exactly twice.
	if got := e.G.Obj(kain).Controller; got != 1 {
		t.Fatalf("Kain controller = %d, want 1 (the damaged player gained it)", got)
	}
	if drew := countDraws(e, 0) - before; drew != 2 {
		t.Fatalf("Kain's controller drew %d cards, want 2 (RememberControlled$ must feed the gate)", drew)
	}
	replayCheck(t, e, cfg)
}

// TestWildDogsGainsControlOfTheWithMostLifeSeat drives Wild Dogs' real
// NewController$ Player.withMostLife: with seat 1 at more life, the upkeep
// trigger hands the Dogs to seat 1; with the life totals tied, the filter
// matches both holders and the control grant resolves to the first seat in
// turn order (seat 0, already the owner -- a no-op).
func TestWildDogsGainsControlOfTheWithMostLifeSeat(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	dogsCard := mustCorpusCard(t, reg, "Wild Dogs")
	e := New(Config{Seed: 13, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}})
	src := e.G.AddObject(dogsCard, 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: src.ID, From: state.ZLibrary, To: state.ZBattlefield})
	sa := cards.ResolveSVar(src.Face().SVars, "TrigOppControl")
	if sa == nil {
		t.Fatal("Wild Dogs has no TrigOppControl SVar")
	}

	e.G.Players[0].Life, e.G.Players[1].Life = 20, 25
	ctx := &effects.Ctx{Source: src.ID, Controller: 0, SVars: src.Face().SVars}
	effects.Resolve(e, ctx, sa)
	if got := e.G.Obj(src.ID).Controller; got != 1 {
		t.Fatalf("Wild Dogs controller = %d, want 1 (the seat with the most life)", got)
	}

	// Tie: both holders match the filter; the grant resolves to the first
	// seat in turn order.
	e.G.Players[0].Life, e.G.Players[1].Life = 20, 20
	ctx = &effects.Ctx{Source: src.ID, Controller: 0, SVars: src.Face().SVars}
	effects.Resolve(e, ctx, sa)
	if got := e.G.Obj(src.ID).Controller; got != 0 {
		t.Fatalf("tied withMostLife must resolve to the first seat in turn order, got controller %d", got)
	}
}

// TestKashiTribeEliteTapsItsTriggeredTarget runs Kashi-Tribe Elite's real
// combat: when it deals combat damage to a creature, that creature is tapped
// and does not untap next turn -- Defined$ TriggeredTargetLKICopy binds the
// damaged creature (gap 2), including through the per-stack-instance
// TriggerContext.
func TestKashiTribeEliteTapsItsTriggeredTarget(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := combatTriggerBoard(t, reg, []string{"Kashi-Tribe Elite"},
		nil, nil, []string{"Name:Ox\nManaCost:2 G\nTypes:Creature Ox\nPT:2/6\nOracle:x\n"})
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepDeclareAttackers})
	elite := findBattlefield(t, e, 0, "Kashi-Tribe Elite", 0)
	ox := findBattlefield(t, e, 1, "Ox", 0)

	e.askAttackers()
	submitAttackers(t, e, elite)
	submitBlockers(t, e, ox)
	drainCombatDamagePriority(t, e)
	passUntilStackEmpty(t, e, 30)

	if !e.G.Obj(ox).Tapped {
		t.Fatal("the creature Kashi-Tribe Elite dealt combat damage to must be tapped (Defined$ TriggeredTargetLKICopy)")
	}
	replayCheck(t, e, cfg)
}

// TestMindMaggotsAnyNumberDiscardAndRememberedCount runs Mind Maggots' real
// ETB: discard any number of creature cards (the AnyNumber$ ask is real the
// moment one eligible card exists -- Min 0, Max the eligible count), each
// answered card is RememberDiscarded$-remembered, and the chained PutCounter
// reads Count$RememberedSize/Twice: two discarded creatures make four +1/+1
// counters.
func TestMindMaggotsAnyNumberDiscardAndRememberedCount(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	maggots := mustCorpusCard(t, reg, "Mind Maggots")
	e := New(Config{Seed: 14, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{append([]*cards.Card{maggots}, mountainDeck(t, 39)...), mountainDeck(t, 40)}})
	// ObjIDs are 1-based (state.Game.NextID starts at 1), so the deck's
	// first card is object 1, not 0.
	src := e.G.Obj(1)
	if src == nil || src.Card != maggots {
		t.Fatal("Mind Maggots is not object 1 (the deck's first card)")
	}
	// Seat 0's hand: two creatures and a land, in that order. Only the
	// creatures are DiscardValid$-eligible.
	frog := e.G.AddObject(card(t, "Name:Frog\nManaCost:G\nTypes:Creature Frog\nPT:1/1\nOracle:x\n"), 0)
	toad := e.G.AddObject(card(t, "Name:Toad\nManaCost:G\nTypes:Creature Toad\nPT:1/1\nOracle:x\n"), 0)
	moor := e.G.AddObject(card(t, "Name:Moor\nTypes:Land\nOracle:x\n"), 0)
	for _, id := range []state.ObjID{frog.ID, toad.ID, moor.ID} {
		// Logged MoveZone events, never a direct SetZone: the hand must be
		// built through events.Apply or the objects' own Zone field (what the
		// final assertions read) stays ZLibrary.
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZHand})
	}

	// Mind Maggots enters: the ETB trigger fires and its discard is a real
	// AnyNumber choice (one eligible card would already be a choice: discard
	// it or keep it).
	e.emit(events.Event{Kind: events.MoveZone, Obj: src.ID, From: src.Zone, To: state.ZBattlefield})
	// Hand-emitted moves do not run a priority round; the queued ETB trigger
	// reaches the stack (and its ask becomes pending) only through one.
	e.priorityRound()
	var d *decision.Decision
	for i := 0; i < 30; i++ {
		d = e.Pending()
		if d == nil {
			t.Fatal("no decision pending after Mind Maggots entered")
		}
		if d.Kind == decision.KModes && d.ResumeKind == "discard" {
			break
		}
		if d.Kind == decision.KPriority {
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
				t.Fatalf("pass priority: %v", err)
			}
			continue
		}
		t.Fatalf("unexpected decision kind %v (resume %q)", d.Kind, d.ResumeKind)
	}
	if d == nil || d.ResumeKind != "discard" {
		t.Fatal("the AnyNumber discard ask never surfaced")
	}
	if d.Min != 0 {
		t.Fatalf("AnyNumber ask Min = %d, want 0", d.Min)
	}
	if d.Max != 2 {
		t.Fatalf("AnyNumber ask Max = %d, want the eligible count 2", d.Max)
	}
	if len(d.Options) != 2 {
		t.Fatalf("options = %d (%+v), want the two creatures only (the land is ineligible)", len(d.Options), d.Options)
	}
	for _, o := range d.Options {
		if o.Obj != frog.ID && o.Obj != toad.ID {
			t.Fatalf("land offered as a discard choice: %+v", o)
		}
	}
	submitChoices(t, e, d.Options[0].Index, d.Options[1].Index)
	passUntilStackEmpty(t, e, 30)

	if n := e.G.Obj(src.ID).Counter("P1P1"); n != 4 {
		t.Fatalf("Mind Maggots P1P1 counters = %d, want 4 (RememberedSize/Twice over 2 remembered discards)", n)
	}
	if z := e.G.Obj(frog.ID).Zone; z != state.ZGraveyard {
		t.Fatalf("frog zone = %s, want Graveyard", z)
	}
	if z := e.G.Obj(toad.ID).Zone; z != state.ZGraveyard {
		t.Fatalf("toad zone = %s, want Graveyard", z)
	}
	if z := e.G.Obj(moor.ID).Zone; z != state.ZHand {
		t.Fatalf("the land must be untouched, zone %s", z)
	}
}

// TestGravelighterBranchCountsCreaturesThatDiedThisTurn runs Gravelighter's
// real ETB both ways: a creature that entered the graveyard from the
// battlefield THIS TURN makes the Count$ThisTurnEntered_Graveyard_from_Battlefield_Creature
// branch true and seat 0 draws a card; with no such entry the false arm runs
// and each player sacrifices a creature instead (the deterministic first-match
// stand-in takes seat 0's Bear).
func TestGravelighterBranchCountsCreaturesThatDiedThisTurn(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	grave := mustCorpusCard(t, reg, "Gravelighter")
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	mk := func(seed uint64) *Engine {
		e := New(Config{Seed: seed, Names: []string{"a", "b"},
			Decks: [][]*cards.Card{append([]*cards.Card{grave, bear}, mountainDeck(t, 38)...), mountainDeck(t, 40)}})
		return e
	}
	place := func(e *Engine, c *cards.Card) state.ObjID {
		for i := range e.G.Objs {
			o := &e.G.Objs[i]
			if o.Card == c && o.Zone != state.ZBattlefield {
				e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: o.Zone, To: state.ZBattlefield})
				return o.ID
			}
		}
		t.Fatal("card not found in a non-battlefield zone")
		return 0
	}

	// True arm: the Bear died this turn, so Gravelighter's draw fires.
	e := mk(15)
	dead := place(e, bear)
	e.emit(events.Event{Kind: events.MoveZone, Obj: dead, From: state.ZBattlefield, To: state.ZGraveyard})
	draws := countDraws(e, 0)
	enter := place(e, grave)
	if enter == 0 {
		t.Fatal("Gravelighter never placed")
	}
	e.priorityRound()
	passUntilStackEmpty(t, e, 30)
	if got := countDraws(e, 0) - draws; got != 1 {
		t.Fatalf("draws after Gravelighter entered with a creature dead this turn = %d, want 1", got)
	}

	// False arm: nothing died, so each player sacrifices a creature. Since
	// this ticket's sacrifice-asks-its-player fix (CR 701.21a), the lone
	// eligible player answers a real KChoose instead of the old deterministic
	// stand-in silently taking the Bear.
	e2 := mk(16)
	alive := place(e2, bear)
	draws2 := countDraws(e2, 0)
	place(e2, grave)
	e2.priorityRound()
	for i := 0; i < 30 && !e2.G.Over && len(e2.G.Stack) > 0; i++ {
		d := e2.Pending()
		if d == nil {
			break
		}
		if d.Kind == decision.KChoose && len(d.Options) > 0 && d.Options[0].Kind == "sacrifice" {
			answerSacrifice(t, e2, "Grizzly Bears")
			continue
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("non-priority decision %+v while draining the stack", d)
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
		if err := e2.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
			t.Fatalf("submit: %v", err)
		}
	}
	if z := e2.G.Obj(alive).Zone; z != state.ZGraveyard {
		t.Fatalf("the false arm's sacrifice stand-in must take the Bear, zone %s", z)
	}
	if got := countDraws(e2, 0) - draws2; got != 0 {
		t.Fatalf("the false arm must not draw, drew %d", got)
	}
}

// TestChooseReplacesItsKindNotAccumulate pins the choice contract Forge
// implements (ChooseCardEffect's terminal host.setChosenCards(allChosen) and
// ChoosePlayerEffect's per-chooser host.setChosenPlayer): each choice SA
// replaces ITS OWN KIND and leaves the other kind alone, so a follow-up that
// reads Defined$ ChosenCard sees only the LATEST card choice. With the old
// accumulate-everything behaviour the mover below would bounce BOTH the Bear
// and the Forest; Equipoise's PhasesLand/PhasesArtifact chain is the corpus
// shape this stands for (its phase-out is idempotent, so the divergence there
// is silent -- the mover here makes it observable).
func TestChooseReplacesItsKindNotAccumulate(t *testing.T) {
	t.Parallel()
	src := card(t, "Name:Chainer\nManaCost:1 U\nTypes:Sorcery\n"+
		"A:SP$ ChooseCard | Choices$ Permanent | Mandatory$ True | SubAbility$ ChooseLand\n"+
		"SVar:ChooseLand:DB$ ChooseCard | Choices$ Land | Mandatory$ True | SubAbility$ DBMove\n"+
		"SVar:DBMove:DB$ ChangeZone | Defined$ ChosenCard | Origin$ Battlefield | Destination$ Hand\nOracle:x\n")
	e := New(Config{Seed: 17, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{append([]*cards.Card{src}, mountainDeck(t, 39)...), mountainDeck(t, 40)}})
	// The hand-built engine parks wherever genesis left it; Advance drives
	// the step machinery until the first decision is actually pending.
	e.Advance()
	bearer := e.G.Obj(1)
	if bearer == nil || bearer.Card != src {
		t.Fatal("Chainer is not object 1 (the deck's first card)")
	}
	// The genesis deal shuffled the library, so move the Chainer into the
	// hand with a logged MoveZone before driving to main1 to cast it.
	e.emit(events.Event{Kind: events.MoveZone, Obj: bearer.ID, From: state.ZLibrary, To: state.ZHand})
	bear := e.G.AddObject(card(t, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"), 0)
	forest := e.G.AddObject(card(t, "Name:Forest\nTypes:Land\nOracle:x\n"), 0)
	for _, o := range []*state.Object{bear, forest} {
		e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: o.Zone, To: state.ZBattlefield})
	}
	addMana(t, e, 0, "UU")
	// Cast it for real (resolveTop owns the suspension), then let the
	// resolution reach its first ChooseCard ask.
	d0 := e.Pending()
	castIdx := -1
	for _, o := range d0.Options {
		if o.Kind == "cast" && o.Obj == bearer.ID {
			castIdx = o.Index
		}
	}
	if castIdx < 0 {
		t.Fatalf("no cast option for the Chainer: %+v", d0.Options)
	}
	submitChoices(t, e, castIdx)
	var d *decision.Decision
	for {
		d = e.Pending()
		if d == nil {
			t.Fatal("no decision pending after the cast")
		}
		if d.Kind == decision.KChoose && d.ResumeKind == "choice" {
			break
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("unexpected decision while waiting for the choice ask: %+v", d)
		}
		idx := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				idx = o.Index
			}
		}
		submitChoices(t, e, idx)
	}

	// First ask: any permanent. Answer with the Bear.
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected the first ChooseCard ask, got %+v", d)
	}
	bearIdx := -1
	for _, o := range d.Options {
		if o.Obj == bear.ID {
			bearIdx = o.Index
		}
	}
	submitChoices(t, e, bearIdx)

	// Second ask: lands only. Answer with the Forest.
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected the second ChooseCard ask, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Obj == bear.ID {
			t.Fatal("the second choice's pool must be lands only (the Bear is stale)")
		}
	}
	forestIdx := -1
	for _, o := range d.Options {
		if o.Obj == forest.ID {
			forestIdx = o.Index
		}
	}
	if forestIdx < 0 {
		t.Fatalf("Forest not offered: %+v", d.Options)
	}
	submitChoices(t, e, forestIdx)
	passUntilStackEmpty(t, e, 20)

	// The mover read Defined$ ChosenCard: ONLY the latest choice moved.
	if z := e.G.Obj(forest.ID).Zone; z != state.ZHand {
		t.Fatalf("Forest zone = %s, want Hand (the latest choice moved)", z)
	}
	if z := e.G.Obj(bear.ID).Zone; z != state.ZBattlefield {
		t.Fatalf("Bear zone = %s, want Battlefield (the earlier choice must not ride along)", z)
	}
}
