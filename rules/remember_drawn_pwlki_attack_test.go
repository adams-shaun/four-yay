package rules

// Corpus-backed engine proofs for the two readers this ticket fixed:
//
//   - effects/cardflow.go effDraw's RememberDrawn$ read. The old read treated
//     the parameter as a boolean True/False, so the corpus's other spelling
//     AllReplaced (13 files) silently remembered nothing and every downstream
//     `Remembered$Amount` / `Defined$ RememberedController` consumer saw an
//     empty list. Kwain, Itinerant Meddler and Communal Brewing are the
//     reported carriers. The read also strips the resolution's fire-time
//     event capture from Ctx.Remembered before recording the draws: a
//     triggered ability's facing object (for Brewing, the entering Brewing
//     itself) is not a card drawn this way and must not add a phantom card to
//     the `for each card drawn this way` count.
//   - effects/filter.go's attackingYouOrYourPWLKI predicate. Without it the
//     Count$ValidAll Creature.attackingYouOrYourPWLKI SVar evaluated to 0, so
//     Mangara, the Diplomat's (and Tomik, Wielder of Law's)
//     `CheckSVar$ X | SVarCompare$ GE2` intervening-if never held and the
//     AttackersDeclared trigger never fired.
//
// Every card is a real corpus card via testutil.CorpusRegistry; the raiders,
// the planeswalker and the padding lands are inline synthetic fixtures (never
// corpus .txt, per the licensing rule).

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// pwlkiCorpusGame builds a three-seat game whose seat 0 is seeded with seat0
// plus Mountain padding, seats 1 and 2 with Mountains only, under the CR
// 103.1 toss starting seat 0. It is the shared shape every test in this file
// drives (a second opponent makes Communal Brewing's "any number of target
// opponents" a real multi-draw, and makes Kwain's Defined$ Player three real
// drawers).
func pwlkiCorpusGame(t *testing.T, seed uint64, seat0 []*cards.Card) (*Engine, Config) {
	t.Helper()
	pad := func(base []*cards.Card) []*cards.Card {
		out := append([]*cards.Card(nil), base...)
		return append(out, mountainDeck(t, 40-len(base))...)
	}
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"a", "b", "c"},
		Decks:  [][]*cards.Card{pad(seat0), mountainDeck(t, 40), mountainDeck(t, 40)},
		Tokens: map[string]*cards.Card{}})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}

// driveUnsick drives to a Main1 on or after turn `turn` for seat `active`
// where `id`'s SummonSick is clear, then re-asks priority. Direct
// SummonSick=false mutation would be an unlogged state write that replayCheck
// rejects, so the turn rotation is the only honest way to clear a tap
// ability's CR 302.6 gate.
func driveUnsick(t *testing.T, e *Engine, id state.ObjID, turn int32, active state.PlayerID) {
	t.Helper()
	for i := 0; i < 300; i++ {
		if e.G.Turn >= turn && e.G.Active == active && e.G.Step == state.StepMain1 && !e.G.Obj(id).SummonSick {
			return
		}
		passAll(t, e, 1)
	}
	t.Fatalf("could not reach a Main1 on/after turn %d seat %d with %d unsick (turn %d seat %d step %s sick=%v)",
		turn, active, id, e.G.Turn, e.G.Active, e.G.Step, e.G.Obj(id).SummonSick)
}

// ingredientCount reads Communal Brewing's INGREDIENT counters.
func ingredientCount(o *state.Object) int {
	if o == nil {
		return -1
	}
	for i := range o.Counters {
		if o.Counters[i].Kind == "INGREDIENT" {
			return int(o.Counters[i].N)
		}
	}
	return 0
}

// resolveBrewingETB moves the corpus Communal Brewing to seat 0's battlefield,
// answers its "any number of target opponents each draw a card" ask with
// exactly `targets` seat 1 and (when 2) seat 2, and drains the resolution.
// Returns the Brewing object's id and the opponents actually offered.
func resolveBrewingETB(t *testing.T, e *Engine, wantSeats []state.PlayerID) state.ObjID {
	t.Helper()
	id := moveByName(t, e, 0, "Communal Brewing", state.ZBattlefield)
	if id == 0 {
		t.Fatal("corpus Communal Brewing was not seeded into seat 0's deck")
	}
	// The ETB trigger fires on the MoveZone and goes on the stack when
	// priority is passed; the first non-priority ask is its target choice.
	d := passPriorityUntilNonPriority(t, e)
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("Communal Brewing ETB = %+v, want its KTarget ask", d)
	}
	if d.Min != 0 {
		t.Fatalf("precondition: Brewing ask Min = %d, want 0 (any number)", d.Min)
	}
	var choices []int
	for _, want := range wantSeats {
		found := -1
		for _, o := range d.Options {
			if o.Kind == "player" && o.Player == want {
				found = o.Index
			}
		}
		if found < 0 {
			t.Fatalf("precondition: opponent seat %d was not offered as a Brewing target: %+v", want, d.Options)
		}
		choices = append(choices, found)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
		t.Fatalf("submit Brewing targets: %v", err)
	}
	passUntilStackEmpty(t, e, 30)
	return id
}

// TestCommunalBrewingIngredientCountersPerCardDrawn is the reported defect:
// "any number of target opponents each draw a card. Put an ingredient counter
// on CARDNAME, then put an ingredient counter on it for each card drawn this
// way." The pre-fix RememberDrawn$ read treated AllReplaced as false, so
// Remembered stayed at just the trigger's fire-time capture (the entering
// Brewing), SVar:X:Remembered$Amount read 1, and the card put only 1 base + 1
// phantom = 2 counters no matter how many opponents drew. The correct totals
// are 1 base + the number of cards actually drawn.
func TestCommunalBrewingIngredientCountersPerCardDrawn(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	brew := mustCorpusCard(t, reg, "Communal Brewing")

	// Two opponents each draw: 1 base + 2 drawn = 3 ingredient counters.
	e, cfg := pwlkiCorpusGame(t, 811, []*cards.Card{brew})
	id := resolveBrewingETB(t, e, []state.PlayerID{1, 2})
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Brewing not on the battlefield: %+v", o)
	}
	if got := ingredientCount(o); got != 3 {
		t.Fatalf("Brewing ingredient counters = %d, want 3 (1 base + 2 opponents drew; the pre-fix read counted only the trigger's own capture)", got)
	}
	replayCheck(t, e, cfg)

	// One opponent draws: 1 base + 1 drawn = 2 counters -- a DIFFERENT count,
	// so the assertion is not a constant.
	e2, cfg2 := pwlkiCorpusGame(t, 811, []*cards.Card{brew})
	id2 := resolveBrewingETB(t, e2, []state.PlayerID{1})
	o2 := e2.G.Obj(id2)
	if o2 == nil || o2.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Brewing not on the battlefield: %+v", o2)
	}
	if got := ingredientCount(o2); got != 2 {
		t.Fatalf("Brewing ingredient counters = %d, want 2 (1 base + 1 opponent drew)", got)
	}
	replayCheck(t, e2, cfg2)
}

// TestKwainAllReplacedDrawGainsLifeForEachDrawer is the reported defect:
// Kwain's "{T}: Each player may draw a card, then each player who drew a card
// this way gains 1 life." RememberDrawn$ AllReplaced populates the
// resolution's Remembered, and DBGainLife's Defined$ RememberedController
// resolves to the controllers of those remembered cards. The pre-fix read
// left Remembered empty, so no player gained life at all.
func TestKwainAllReplacedDrawGainsLifeForEachDrawer(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	kwain := mustCorpusCard(t, reg, "Kwain, Itinerant Meddler")

	e, cfg := pwlkiCorpusGame(t, 812, []*cards.Card{kwain})
	kw := moveByName(t, e, 0, "Kwain, Itinerant Meddler", state.ZBattlefield)
	if kw == 0 {
		t.Fatal("corpus Kwain was not seeded into seat 0's deck")
	}
	driveUnsick(t, e, kw, 2, 0)

	// Precondition: three living seats, all at the starting 20 so a +1 is
	// unmistakable, and the {T} ability is actually offered.
	for p := state.PlayerID(0); p < 3; p++ {
		if got := e.G.Players[p].Life; got != 20 {
			t.Fatalf("precondition: seat %d life = %d, want 20", p, got)
		}
	}
	if o := e.G.Obj(kw); o == nil || o.Zone != state.ZBattlefield || o.Tapped || o.SummonSick {
		t.Fatalf("precondition: Kwain not an untapped, unsick battlefield permanent: %+v", o)
	}
	opt := abilityOption(t, e, kw, 0)
	submitChoices(t, e, opt.Index)

	// ACCEPT: each player draws, and every drawer's controller gains 1.
	ask := passPriorityUntil(t, e, decision.KChoose)
	if ask == nil || ask.ResumeKind != "draw_optional" || ask.Player != 0 {
		t.Fatalf("Kwain draw ask = %+v, want the controller's draw_optional", ask)
	}
	if ask.Options[0].Kind != "yes" {
		t.Fatalf("Kwain draw ask option 0 = %+v, want yes", ask.Options[0])
	}
	submitChoices(t, e, ask.Options[0].Index)
	passUntilStackEmpty(t, e, 30)
	for p := state.PlayerID(0); p < 3; p++ {
		if got := e.G.Players[p].Life; got != 21 {
			t.Fatalf("seat %d life = %d, want 21 (drew off AllReplaced RememberDrawn)", p, got)
		}
	}
	replayCheck(t, e, cfg)

	// DECLINE: the same ability's no arm draws nothing and grants no life.
	e2, cfg2 := pwlkiCorpusGame(t, 812, []*cards.Card{kwain})
	kw2 := moveByName(t, e2, 0, "Kwain, Itinerant Meddler", state.ZBattlefield)
	driveUnsick(t, e2, kw2, 2, 0)
	opt2 := abilityOption(t, e2, kw2, 0)
	submitChoices(t, e2, opt2.Index)
	ask2 := passPriorityUntil(t, e2, decision.KChoose)
	if ask2 == nil || ask2.ResumeKind != "draw_optional" {
		t.Fatalf("Kwain decline ask = %+v, want the draw_optional", ask2)
	}
	submitChoices(t, e2, ask2.Options[1].Index) // no
	passUntilStackEmpty(t, e2, 30)
	for p := state.PlayerID(0); p < 3; p++ {
		if got := e2.G.Players[p].Life; got != 20 {
			t.Fatalf("seat %d life = %d after a declined draw, want 20", p, got)
		}
	}
	replayCheck(t, e2, cfg2)
}

// mangaraGame builds a three-seat game for the attackingYouOrYourPWLKI pins:
// seat 0 has Mangara, the Diplomat plus an optional inline planeswalker and
// Plains padding, seat 1 has the three raiders plus Mountain padding, seat 2
// is Mountains. Mangara and the raiders are placed on the battlefield through
// LOGGED moves, and the engine is driven to seat 1's declare-attackers step
// (their summoning sickness is clear because they entered during seat 0's
// turn). Returns the engine, the config, Mangara's id, the planeswalker's id
// (0 when none) and the raider ids.
func mangaraGame(t *testing.T, reg *cards.Registry, wantPW bool) (*Engine, Config, state.ObjID, state.ObjID, [3]state.ObjID) {
	t.Helper()
	mangara := mustCorpusCard(t, reg, "Mangara, the Diplomat")
	plains := mustCorpusCard(t, reg, "Plains")
	mountain := mustCorpusCard(t, reg, "Mountain")

	seat0 := []*cards.Card{mangara}
	if wantPW {
		seat0 = append(seat0, card(t, "Name:Test Walker\nManaCost:3 W\nTypes:Planeswalker Testy\nLoyalty:3\nOracle:x\n"))
	}
	for len(seat0) < 40 {
		seat0 = append(seat0, plains)
	}
	raiderSrcs := [3]string{
		"Name:Raider One\nManaCost:2 R\nTypes:Creature Goblin\nPT:2/2\nOracle:x\n",
		"Name:Raider Two\nManaCost:2 R\nTypes:Creature Goblin\nPT:2/2\nOracle:x\n",
		"Name:Raider Three\nManaCost:2 R\nTypes:Creature Goblin\nPT:2/2\nOracle:x\n",
	}
	seat1 := make([]*cards.Card, 0, 40)
	for _, s := range raiderSrcs {
		seat1 = append(seat1, card(t, s))
	}
	for len(seat1) < 40 {
		seat1 = append(seat1, mountain)
	}
	cfg := seatZeroStart(Config{Seed: 813, Names: []string{"m", "att", "third"},
		Decks:  [][]*cards.Card{seat0, seat1, mountainDeck(t, 40)},
		Tokens: map[string]*cards.Card{}})
	e := New(cfg)
	e.Advance()

	mID := moveByName(t, e, 0, "Mangara, the Diplomat", state.ZBattlefield)
	if mID == 0 {
		t.Fatal("corpus Mangara was not seeded into seat 0's deck")
	}
	var pwID state.ObjID
	if wantPW {
		pwID = moveByName(t, e, 0, "Test Walker", state.ZBattlefield)
		if pwID == 0 {
			t.Fatal("inline planeswalker was not seeded into seat 0's deck")
		}
	}
	var raiders [3]state.ObjID
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, cand := range e.G.Zone(z, 1) {
			o := e.G.Obj(cand)
			if o == nil || o.Card == nil || o.IsToken || o.Face() == nil {
				continue
			}
			idx := -1
			switch o.Face().Name {
			case "Raider One":
				idx = 0
			case "Raider Two":
				idx = 1
			case "Raider Three":
				idx = 2
			}
			if idx < 0 || raiders[idx] != 0 {
				continue
			}
			e.emit(events.Event{Kind: events.MoveZone, Obj: cand, From: z, To: state.ZBattlefield})
			raiders[idx] = cand
		}
	}
	for i := range raiders {
		if raiders[i] == 0 {
			t.Fatalf("precondition: raider %d was not placed", i)
		}
	}
	driveToAttackers(t, e)
	return e, cfg, mID, pwID, raiders
}

// declareAttacks submits the given set of attack options and resolves the
// resulting trigger stack, returning seat 0's hand size before and after.
func declareAttacks(t *testing.T, e *Engine, picks []int) (int, int) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected a KAttackers decision, got %+v", d)
	}
	before := len(e.G.Zone(state.ZHand, 0))
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: picks}); err != nil {
		t.Fatalf("submit attackers: %v", err)
	}
	passUntilStackEmpty(t, e, 30)
	return before, len(e.G.Zone(state.ZHand, 0))
}

// TestMangaraDiplomatAttackTrigger draws when two or more creatures attack its
// controller or that controller's planeswalkers. The pre-fix
// attackingYouOrYourPWLKI predicate did not exist, so the SVar count was 0 and
// the GE2 gate never held.
func TestMangaraDiplomatAttackTrigger(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)

	// FIRES: one raider at seat 0 and one raider at seat 0's planeswalker --
	// the mixed case the OR token exists for (2 qualifying attackers, one of
	// each kind).
	e, cfg, mID, pwID, raiders := mangaraGame(t, reg, true)
	if o := e.G.Obj(mID); o == nil || o.Zone != state.ZBattlefield || o.Face().Name != "Mangara, the Diplomat" {
		t.Fatalf("precondition: Mangara trigger source not on the battlefield: %+v", o)
	}
	if o := e.G.Obj(pwID); o == nil || o.Zone != state.ZBattlefield || !o.Face().IsPlaneswalker() {
		t.Fatalf("precondition: planeswalker defender not on the battlefield: %+v", o)
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected the attackers decision, got %+v", d)
	}
	atPlayer, atPW := -1, -1
	for _, o := range d.Options {
		if o.Obj == raiders[0] && o.Battle == 0 && o.Player == 0 {
			atPlayer = o.Index
		}
		if o.Obj == raiders[1] && o.Battle == pwID && o.Player == 0 {
			atPW = o.Index
		}
	}
	if atPlayer < 0 || atPW < 0 {
		t.Fatalf("precondition: mixed attack pairs not offered (atPlayer=%d atPW=%d): %+v", atPlayer, atPW, d.Options)
	}
	before, after := declareAttacks(t, e, []int{atPlayer, atPW})
	// The two attackers really are attacking seat 0 / its walker.
	nAt0 := 0
	for _, oid := range e.G.Zone(state.ZBattlefield, 1) {
		if o := e.G.Obj(oid); o != nil && o.IsAttacking && o.Attacking == 0 {
			nAt0++
		}
	}
	if nAt0 != 2 {
		t.Fatalf("precondition: %d attackers recorded at seat 0, want 2", nAt0)
	}
	if after != before+1 {
		t.Fatalf("Mangara mix trigger hand %d -> %d, want +1 (two qualifying attackers)", before, after)
	}
	replayCheck(t, e, cfg)

	// DOES NOT FIRE with a single qualifying attacker (one at seat 0, the
	// other at the unrelated seat 2).
	e2, cfg2, m2, _, raiders2 := mangaraGame(t, reg, false)
	if o := e2.G.Obj(m2); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Mangara not on the battlefield: %+v", o)
	}
	d2 := e2.Pending()
	if d2 == nil || d2.Kind != decision.KAttackers {
		t.Fatalf("expected the attackers decision, got %+v", d2)
	}
	oneAt0, oneAt2 := -1, -1
	for _, o := range d2.Options {
		if o.Obj == raiders2[0] && o.Battle == 0 && o.Player == 0 {
			oneAt0 = o.Index
		}
		if o.Obj == raiders2[1] && o.Battle == 0 && o.Player == 2 {
			oneAt2 = o.Index
		}
	}
	if oneAt0 < 0 || oneAt2 < 0 {
		t.Fatalf("precondition: the one-at-you / one-at-third pairs not offered: %+v", d2.Options)
	}
	before2, after2 := declareAttacks(t, e2, []int{oneAt0, oneAt2})
	if after2 != before2 {
		t.Fatalf("Mangara single-qualifying trigger hand %d -> %d, want no draw", before2, after2)
	}
	replayCheck(t, e2, cfg2)
}
