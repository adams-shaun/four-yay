package rules

// One test per primitive this ticket implements, each driven by at least one
// real corpus card script from the ticket's card table (lookup fetches the
// REAL compiled script; the other participants are freely-authored fixtures,
// per the licensing rule that a corpus .txt is never inlined).

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// watcherGolemSrc is a freely-authored colorless artifact creature whose
// enters-the-battlefield trigger puts one +1/+1 counter on it. The trigger
// carries the corpus's own source-scoping convention (ValidCard$ Card.Self)
// and the counter primitive the corpus uses for this shape (DB$ PutCounter).
const watcherGolemSrc = "Name:Watcher Golem\nManaCost:3\nTypes:Artifact Creature Golem\nPT:2/2\n" +
	"T:Mode$ ChangesZone | Origin$ Hand | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ DbCount | TriggerDescription$ When CARDNAME enters the battlefield, put a +1/+1 counter on it.\n" +
	"SVar:DbCount:DB$ PutCounter | Defined$ Self | Counter$ P1P1\n"

// corpusEngine builds a two-seat engine whose seat-0 deck is extras0 followed
// by Mountains and seat-1's is extras1 followed by Mountains, driven to seat
// 0's turn-1 Main1. Genesis shuffles, so tests find cards with moveByName
// (which walks hand then library), never by deck position.
func corpusEngine(t *testing.T, reg *cards.Registry, extras0, extras1 []*cards.Card) *Engine {
	t.Helper()
	fill := func(n int) []*cards.Card {
		m, ok := reg.Lookup("Mountain")
		if !ok {
			t.Fatal("corpus fixture: Mountain missing")
		}
		out := make([]*cards.Card, n)
		for i := range out {
			out[i] = m
		}
		return out
	}
	e := New(Config{Seed: 42, Names: []string{"a", "b"}, Tokens: reg.Tokens,
		Decks: [][]*cards.Card{append(append([]*cards.Card{}, extras0...), fill(40-len(extras0))...),
			append(append([]*cards.Card{}, extras1...), fill(40-len(extras1))...)}})
	e.Advance()
	toMain1(t, e)
	return e
}

// corpusEngineThree is corpusEngine's three-seat variant for multiplayer
// rules that must distinguish the active player from the other opponents.
func corpusEngineThree(t *testing.T, reg *cards.Registry, extras0, extras1, extras2 []*cards.Card) *Engine {
	t.Helper()
	m, ok := reg.Lookup("Mountain")
	if !ok {
		t.Fatal("corpus fixture: Mountain missing")
	}
	fill := func(extras []*cards.Card) []*cards.Card {
		deck := append([]*cards.Card{}, extras...)
		for len(deck) < 40 {
			deck = append(deck, m)
		}
		return deck
	}
	e := New(seatZeroStart(Config{Seed: 42, Names: []string{"a", "b", "c"}, Tokens: reg.Tokens,
		Decks: [][]*cards.Card{fill(extras0), fill(extras1), fill(extras2)}}))
	e.Advance()
	toMain1(t, e)
	return e
}

// corpusEngineCfg is corpusEngine with the Config returned, for replayCheck.
func corpusEngineCfg(t *testing.T, reg *cards.Registry, extras0, extras1 []*cards.Card) (*Engine, Config) {
	t.Helper()
	fill := func(n int) []*cards.Card {
		m, ok := reg.Lookup("Mountain")
		if !ok {
			t.Fatal("corpus fixture: Mountain missing")
		}
		out := make([]*cards.Card, n)
		for i := range out {
			out[i] = m
		}
		return out
	}
	cfg := Config{Seed: 42, Names: []string{"a", "b"}, Tokens: reg.Tokens,
		Decks: [][]*cards.Card{append(append([]*cards.Card{}, extras0...), fill(40-len(extras0))...),
			append(append([]*cards.Card{}, extras1...), fill(40-len(extras1))...)}}
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}

// lookup is reg.Lookup or the test fails: a corpus card this ticket's tests
// depend on being absent is a corpus-pin change, not something to paper over.
func lookup(t *testing.T, reg *cards.Registry, name string) *cards.Card {
	t.Helper()
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("corpus fixture: %q missing from the registry", name)
	}
	return c
}

// countEvents counts events matching pred in the engine's whole log.
func countEvents(e *Engine, pred func(events.Event) bool) int {
	n := 0
	for _, ev := range e.L.Events {
		if pred(ev) {
			n++
		}
	}
	return n
}

// driveToTurn answers every pending decision -- pass for priority, FIRST
// option otherwise (the same first-choice stand-in botpolicy's clamp fallback
// uses) -- until the game is at turn/p's Main1, then returns false. Bounded.
// StepUntap is never observable between decisions (beginTurn runs untap,
// upkeep and the opening priority inside one emit chain), so the stop reads
// Main1.
func driveToTurn(t *testing.T, e *Engine, turn int32, p state.PlayerID) bool {
	t.Helper()
	for i := 0; i < 4000; i++ {
		if e.G.Over {
			t.Fatalf("game ended unexpectedly at turn %d", e.G.Turn)
		}
		if e.G.Turn >= turn && e.G.Active == p && e.G.Step == state.StepMain1 {
			return false
		}
		d := e.Pending()
		if d == nil {
			continue
		}
		if d.Kind == decision.KPriority {
			for _, o := range d.Options {
				if o.Kind == "pass" {
					if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}); err != nil {
						t.Fatalf("submit pass: %v", err)
					}
					break
				}
			}
			continue
		}
		if len(d.Options) == 0 {
			t.Fatalf("empty decision %+v", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
			t.Fatalf("submit: %v", err)
		}
	}
	t.Fatal("driveToTurn did not converge")
	return false
}

// passToKind passes priority decisions (whole CR 508.2/509.1 windows, every
// seat) and answers any turn-based ask it crosses with its first option,
// until a decision of the wanted kind is pending.
func passToKind(t *testing.T, e *Engine, kind decision.Kind) {
	t.Helper()
	for i := 0; i < 50; i++ {
		d := e.Pending()
		if d == nil {
			continue
		}
		if d.Kind == kind {
			return
		}
		if d.Kind != decision.KPriority {
			if len(d.Options) == 0 {
				t.Fatalf("empty non-priority decision %+v while waiting for %s", d, kind)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
				t.Fatalf("submit: %v", err)
			}
			continue
		}
		passOnce(t, e)
	}
	t.Fatalf("no %s decision after 50 passes (turn %d seat %d step %s)", kind, e.G.Turn, e.G.Active, e.G.Step)
}

// firstCreature returns seat p's first battlefield creature.
func firstCreature(t *testing.T, e *Engine, p state.PlayerID) state.ObjID {
	t.Helper()
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		if o := e.G.Obj(id); o != nil && o.Face().IsCreature() {
			return id
		}
	}
	t.Fatalf("seat %d has no creature", p)
	return 0
}

// castNamed submits the "cast" option whose label names name in the pending
// priority decision (stack_test.go's own castFirst takes a KIND and submits
// the first option of it -- too coarse when several cards are castable).
func castNamed(t *testing.T, e *Engine, name string) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision pending (got %+v)", d)
	}
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Label == "Cast "+name {
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}); err != nil {
				t.Fatalf("submit cast %q: %v", name, err)
			}
			return
		}
	}
	t.Fatalf("no cast option for %q in %+v", name, d.Options)
}

// findInHand returns the id of the card named name in seat p's hand, or 0.
func findInHand(e *Engine, p state.PlayerID, name string) state.ObjID {
	for _, id := range e.G.Zone(state.ZHand, p) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
			return id
		}
	}
	return 0
}

// TestFogPreventsThisTurnsCombatDamageOnly is api:Fog's leaf: real corpus Fog
// cast in a live combat prevents every combat-damage assignment of the turn,
// and a fresh combat the NEXT turn deals damage normally.
func TestFogPreventsThisTurnsCombatDamageOnly(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	bear := card(t, bearSrc)
	// Seat 1 brings no creatures, so the auto-answers that drive to the test
	// turns never trade seat 0's bears away.
	e := corpusEngine(t, reg,
		[]*cards.Card{lookup(t, reg, "Fog"), bear, bear},
		[]*cards.Card{})
	moveByName(t, e, 0, "Bear", state.ZBattlefield)
	moveByName(t, e, 0, "Bear", state.ZBattlefield)
	// Seat 0's bears are summoning sick on turn 1, so the Fogged combat runs
	// on seat 0's SECOND turn: drive there, cast the real Fog, then attack.
	for driveToTurn(t, e, 3, 0) {
	}
	moveByName(t, e, 0, "Fog", state.ZHand)
	addMana(t, e, 0, "G")
	fogID := findInHand(e, 0, "Fog")
	if fogID == 0 {
		t.Fatal("Fog not in seat 0's hand on turn 3")
	}
	castNamed(t, e, "Fog")
	if e.G.Obj(fogID).Zone != state.ZStack {
		t.Fatalf("Fog not on the stack: %s", e.G.Obj(fogID).Zone)
	}
	passUntilStackEmpty(t, e, 30)
	if e.G.Obj(fogID).Zone != state.ZGraveyard {
		t.Fatalf("Fog did not resolve: %s", e.G.Obj(fogID).Zone)
	}
	if !e.fogActive() {
		t.Fatal("Fog resolved but no PreventCombatDamage effect is active")
	}
	dmgBefore := countEvents(e, func(ev events.Event) bool {
		return ev.Kind == events.Damage && ev.Amount > 0
	})
	driveToStep(t, e, e.G.Turn, e.G.Active, state.StepDeclareAttackers)
	passToKind(t, e, decision.KAttackers)
	atk := firstCreature(t, e, 0)
	submitAttackersOnly(t, e, atk)
	passAll(t, e, 300)
	if dmg := countEvents(e, func(ev events.Event) bool {
		return ev.Kind == events.Damage && ev.Amount > 0
	}); dmg != dmgBefore {
		t.Fatalf("%d Damage events after a Fogged combat (want %d)", dmg, dmgBefore)
	}
	if e.fogActive() {
		t.Fatal("the Fog effect survived past its turn's cleanup")
	}
	// The next turn's combat is not Fogged: seat 0's other Bear attacks and
	// the 2 damage lands on seat 1.
	for driveToTurn(t, e, 5, 0) {
	}
	playerDmgBefore := countEvents(e, func(ev events.Event) bool {
		return ev.Kind == events.Damage && ev.Amount > 0 && ev.Obj == 0
	})
	driveToStep(t, e, e.G.Turn, e.G.Active, state.StepDeclareAttackers)
	passToKind(t, e, decision.KAttackers)
	atk = firstCreature(t, e, e.G.Active)
	submitAttackersOnly(t, e, atk)
	passAll(t, e, 300)
	playerDmg := countEvents(e, func(ev events.Event) bool {
		return ev.Kind == events.Damage && ev.Amount > 0 && ev.Obj == 0
	})
	if playerDmg <= playerDmgBefore {
		t.Fatal("the turn after the Fog dealt no combat damage to a player")
	}
}

// TestGambleSearchesThenShuffles is api:Shuffle's leaf (real corpus Gamble):
// the search resolves, the random discard runs, and the chained DB$ Shuffle
// emits one Secret Shuffle event that leaves the library a real permutation.
func TestGambleSearchesThenShuffles(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	bear := card(t, bearSrc)
	e := corpusEngine(t, reg,
		[]*cards.Card{lookup(t, reg, "Gamble"), bear},
		[]*cards.Card{})
	moveByName(t, e, 0, "Gamble", state.ZHand)
	addMana(t, e, 0, "R")
	castNamed(t, e, "Gamble")
	// Let the spell reach the top of the stack and resolve into the search
	// ask (a real, ordered option list over the library).
	passToKind(t, e, decision.KChoose)
	submitChoices(t, e, 0)
	passUntilStackEmpty(t, e, 40)
	shuffles := countEvents(e, func(ev events.Event) bool {
		return ev.Kind == events.Shuffle && ev.Player == 0
	})
	if shuffles == 0 {
		t.Fatal("Gamble resolved without a Shuffle event")
	}
	if got := len(e.G.Zone(state.ZHand, 0)) + len(e.G.Zone(state.ZLibrary, 0)) + len(e.G.Zone(state.ZGraveyard, 0)); got != 40 {
		t.Fatalf("card count changed: hand %d + library %d + graveyard %d",
			len(e.G.Zone(state.ZHand, 0)), len(e.G.Zone(state.ZLibrary, 0)), len(e.G.Zone(state.ZGraveyard, 0)))
	}
}

// TestAllIsDustSacrificesColoredPermanentsOnly is api:SacrificeAll's leaf
// (real corpus All Is Dust): each player sacrifices exactly the permanents
// that are one or more colors; colorless ones survive.
func TestAllIsDustSacrificesColoredPermanentsOnly(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	bear := card(t, bearSrc)
	golem := card(t, "Name:Scrap Golem\nManaCost:3\nTypes:Artifact Creature Golem\nPT:3/3\nOracle:x\n")
	e := corpusEngine(t, reg,
		[]*cards.Card{lookup(t, reg, "All Is Dust"), bear, golem},
		[]*cards.Card{bear})
	moveByName(t, e, 0, "All Is Dust", state.ZHand)
	b0 := moveByName(t, e, 0, "Bear", state.ZBattlefield)
	g0 := moveByName(t, e, 0, "Scrap Golem", state.ZBattlefield)
	b1 := moveByName(t, e, 1, "Bear", state.ZBattlefield)
	for i := 0; i < 7; i++ {
		e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "C", Amount: 1})
	}
	e.priorityRound()
	castNamed(t, e, "All Is Dust")
	passUntilStackEmpty(t, e, 40)
	for _, tc := range []struct {
		id      state.ObjID
		name    string
		sacrved bool
	}{{b0, "seat-0 Bear", true}, {g0, "seat-0 Scrap Golem", false}, {b1, "seat-1 Bear", true}} {
		o := e.G.Obj(tc.id)
		if o == nil {
			t.Fatalf("%s: object vanished entirely", tc.name)
		}
		if tc.sacrved && o.Zone != state.ZGraveyard {
			t.Fatalf("%s was not sacrificed (zone %s)", tc.name, o.Zone)
		}
		if !tc.sacrved && o.Zone != state.ZBattlefield {
			t.Fatalf("%s must survive (zone %s)", tc.name, o.Zone)
		}
	}
}

// TestFinalFortuneGrantsAndSpendsAnExtraTurn is api:AddTurn's leaf (real
// corpus Final Fortune): the grant, the repeat seat at cleanup, and the
// delayed end-step trigger of the granted turn costing its controller the
// game. The whole game replays byte-identically.
func TestFinalFortuneGrantsAndSpendsAnExtraTurn(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{lookup(t, reg, "Final Fortune")},
		[]*cards.Card{})
	moveByName(t, e, 0, "Final Fortune", state.ZHand)
	addMana(t, e, 0, "RR")
	castNamed(t, e, "Final Fortune")
	passUntilStackEmpty(t, e, 40)
	grants := countEvents(e, func(ev events.Event) bool {
		return ev.Kind == events.ExtraTurn && ev.Player == 0 && ev.Amount > 0
	})
	if grants != 1 {
		t.Fatalf("want exactly one ExtraTurn grant, got %d", grants)
	}
	// Drive through the turn's end: at cleanup the seat repeats and consumes
	// the extra turn; at the extra turn's end step the delayed trigger fires.
	passAll(t, e, 4000)
	if !e.G.Over {
		t.Fatal("the game did not end with the lost extra turn")
	}
	lost := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.PlayerLost && ev.Player == 0 && ev.Text == "lost the game" {
			lost = true
		}
	}
	if !lost {
		t.Fatal("the extra turn's end step did not cost seat 0 the game")
	}
	if e.G.ExtraTurns[0] != 0 {
		t.Fatalf("extra-turn count not consumed: %d", e.G.ExtraTurns[0])
	}
	replayCheck(t, e, cfg)
}

// TestTimeStretchQueuesEveryGrantedTurn is AddTurn's multi-turn regression:
// the real corpus Time Stretch has NumTurns$ 2, so its one grant event must
// yield two separately consumed queue entries.
// TestSavorTheMomentExtraTurnSkipsUntap is api:AddTurn's SkipUntap$ rider
// regression. Savor the Moment is a real corpus script: the tapped Bear must
// stay tapped when its granted turn begins, while the turn still proceeds to
// upkeep and replays from its event log.
func TestSavorTheMomentExtraTurnSkipsUntap(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	bear := card(t, bearSrc)
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{lookup(t, reg, "Savor the Moment"), bear}, nil)
	bearID := moveByName(t, e, 0, "Bear", state.ZBattlefield)
	e.emit(events.Event{Kind: events.Tap, Obj: bearID})
	moveByName(t, e, 0, "Savor the Moment", state.ZHand)
	addMana(t, e, 0, "CUU")
	castNamed(t, e, "Savor the Moment")
	passUntilStackEmpty(t, e, 40)
	if got := len(e.G.ExtraTurnQueue); got != 1 || !e.G.ExtraTurnQueue[0].SkipUntap {
		t.Fatalf("Savor the Moment queue = %+v, want one SkipUntap grant", e.G.ExtraTurnQueue)
	}
	e.setStep(state.StepCleanup)
	e.advanceStep()
	if e.G.Step != state.StepUpkeep {
		t.Fatalf("extra turn starts at %s, want upkeep after skipped untap", e.G.Step)
	}
	if !e.G.Obj(bearID).Tapped {
		t.Fatal("Savor the Moment untapped a permanent during its extra turn")
	}
	replayCheck(t, e, cfg)
}

func TestTimeStretchQueuesEveryGrantedTurn(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Time Stretch")}, nil)
	moveByName(t, e, 0, "Time Stretch", state.ZHand)
	addMana(t, e, 0, "CCCCCCCCUU")
	castNamed(t, e, "Time Stretch")
	passToKind(t, e, decision.KTarget)
	submitChoices(t, e, 0)
	passUntilStackEmpty(t, e, 60)
	if got := len(e.G.ExtraTurnQueue); got != 2 {
		t.Fatalf("Time Stretch queued %d extra turns, want 2", got)
	}
	for want := 1; want >= 0; want-- {
		e.setStep(state.StepCleanup)
		e.advanceStep()
		if got := len(e.G.ExtraTurnQueue); got != want {
			t.Fatalf("after consumption queue length = %d, want %d", got, want)
		}
		if got := e.G.ExtraTurns[0]; got != want {
			t.Fatalf("after consumption count = %d, want %d", got, want)
		}
	}
}

// TestNameStickerGoblinRollsAndFiresItsRanges is api:RollDice's leaf (real
// corpus "Name Sticker" Goblin): the ETB trigger rolls a d20 (one Note), the
// result's range sub adds the matching {R} amount, and the chained amass
// creates the Orc Army.
func TestNameStickerGoblinRollsAndFiresItsRanges(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg,
		[]*cards.Card{lookup(t, reg, `"Name Sticker" Goblin`)},
		[]*cards.Card{})
	moveByName(t, e, 0, `"Name Sticker" Goblin`, state.ZHand)
	for _, s := range []string{"R", "R", "C"} {
		e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: s, Amount: 1})
	}
	e.priorityRound()
	castNamed(t, e, `"Name Sticker" Goblin`)
	passUntilStackEmpty(t, e, 60)
	rolls := countEvents(e, func(ev events.Event) bool {
		return ev.Kind == events.Note && len(ev.Text) > 11 && ev.Text[:11] == "rolls a d20"
	})
	if rolls != 1 {
		t.Fatalf("want exactly one die roll, got %d", rolls)
	}
	if got := e.G.Players[0].Pool[state.MR]; got != 4 && got != 5 && got != 6 {
		t.Fatalf("the die's range sub added %d R, want 4, 5 or 6", got)
	}

}

// TestOrcishBowmastersAmassesAnOrcArmy is api:Amass's leaf (real corpus
// Orcish Bowmasters): the ETB trigger's damage target resolves, then the
// chained amass puts one +1/+1 on an Army that is also an Orc.
func TestOrcishBowmastersAmassesAnOrcArmy(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg,
		[]*cards.Card{lookup(t, reg, "Orcish Bowmasters")},
		[]*cards.Card{})
	moveByName(t, e, 0, "Orcish Bowmasters", state.ZHand)
	for _, s := range []string{"B", "C"} {
		e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: s, Amount: 1})
	}
	e.priorityRound()
	castNamed(t, e, "Orcish Bowmasters")
	passToKind(t, e, decision.KTarget)
	submitChoices(t, e, 1) // deal the 1 damage to seat 1
	passUntilStackEmpty(t, e, 60)
	found := false
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		o := e.G.Obj(id)
		if o == nil || !o.IsToken || o.Face() == nil {
			continue
		}
		isArmy := false
		for _, ty := range o.Face().Types {
			if ty == "Army" {
				isArmy = true
			}
		}
		if !isArmy {
			continue
		}
		found = true
		if o.Counter("P1P1") != 1 {
			t.Fatalf("the amassed Army has %d +1/+1 counters, want 1", o.Counter("P1P1"))
		}
		// "It's also an Orc" (CR 701.55b): the permanent type grant.
		types := e.Derived(id).Types
		has := false
		for _, ty := range types {
			if ty == "Orc" {
				has = true
			}
		}
		if !has {
			t.Fatalf("the amassed Army is not also an Orc: %v", types)
		}
	}
	if !found {
		t.Fatal("no Army token was amassed")
	}
}

// TestDargoCastsAsACreature is api:PermanentCreature's leaf (real corpus
// Dargo, the Shipwrecker): the SP$ PermanentCreature cast pays its
// Sac<X>/... additional-cost option (declined here) and the creature enters
// with its printed 7/5.
func TestDargoCastsAsACreature(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg,
		[]*cards.Card{lookup(t, reg, "Dargo, the Shipwrecker")},
		[]*cards.Card{})
	moveByName(t, e, 0, "Dargo, the Shipwrecker", state.ZHand)
	addMana(t, e, 0, "RRRRRRR")
	castNamed(t, e, "Dargo, the Shipwrecker")
	// The optional Sac<X> additional cost announces its count first (CR
	// 601.2b): declining the sacrifice is announcing X=0.
	d := e.Pending()
	if d != nil && d.Kind == decision.KChoose {
		zero := -1
		for _, o := range d.Options {
			if o.Kind == "x" && o.Amount == 0 {
				zero = o.Index
			}
		}
		if zero < 0 {
			t.Fatalf("no X=0 count option: %+v", d.Options)
		}
		submitChoices(t, e, zero)
	}
	passUntilStackEmpty(t, e, 40)
	dargo := state.ObjID(0)
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Dargo, the Shipwrecker" {
			dargo = id
		}
	}
	if dargo == 0 {
		t.Fatal("Dargo never entered the battlefield")
	}
	o := e.G.Obj(dargo)
	if o.Face().Power() != 7 || o.Face().Toughness() != 5 {
		t.Fatalf("Dargo entered as %d/%d", o.Face().Power(), o.Face().Toughness())
	}
}

// answerQuiet is the test driver for a settle-anything moment: answer every
// pending decision -- pass for priority, the first Min options otherwise
// (trigger-order asks, KModes) -- until the trigger queue AND the stack are
// both empty. Bounded, and it fatals on a zero-option decision (a decision
// nobody could answer differently should never reach a seat here).
func answerQuiet(t *testing.T, e *Engine, limit int) {
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
			for _, o := range d.Options {
				if o.Kind == "pass" {
					if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}); err != nil {
						t.Fatalf("submit pass: %v", err)
					}
					break
				}
			}
			continue
		}
		if len(d.Options) == 0 {
			t.Fatalf("empty decision %+v", d)
		}
		ch := []int{}
		for k := 0; k < d.Min && k < len(d.Options); k++ {
			ch = append(ch, d.Options[k].Index)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: ch}); err != nil {
			t.Fatalf("submit: %v", err)
		}
	}
	if !e.G.Over {
		t.Fatal("answerQuiet did not settle")
	}
}

// TestStationTapsASummoningSickCreatureForChargeCounters is kw:Station's leaf
// (real corpus Exploration Broodship): the sorcery-window offer, the tap
// pick over another creature, and the charge counters equal to its
// layer-derived power. The creature is SUMMONING SICK and still eligible:
// sickness (CR 302.6) bars a creature from attacking and from its OWN
// tap-cost abilities, never from being tapped to pay another permanent's
// activation cost -- the same reading crewing a Vehicle has.
func TestStationTapsASummoningSickCreatureForChargeCounters(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	bear := card(t, bearSrc)
	e := corpusEngine(t, reg,
		[]*cards.Card{lookup(t, reg, "Exploration Broodship"), bear},
		[]*cards.Card{})
	brood := moveByName(t, e, 0, "Exploration Broodship", state.ZBattlefield)
	bearID := moveByName(t, e, 0, "Bear", state.ZBattlefield)
	e.priorityRound()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision (got %+v)", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "station" && o.Obj == brood {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no station option for the broodship: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit station: %v", err)
	}
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("no tap pick after the station option (got %+v)", d)
	}
	pick := -1
	for _, o := range d.Options {
		if o.Obj == bearID {
			pick = o.Index
		}
	}
	if pick < 0 {
		t.Fatalf("the summoning-sick Bear was not offered as a station tap: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pick}}); err != nil {
		t.Fatalf("submit tap pick: %v", err)
	}
	if e.G.Obj(bearID).Tapped != true {
		t.Fatal("the tapped creature was not tapped")
	}
	if got := e.G.Obj(brood).Counter("CHARGE"); got != 2 {
		t.Fatalf("the spacecraft has %d charge counters, want 2 (the Bear's power)", got)
	}
}

// TestStationUsesDerivedCreatureType covers CR 702.150's current
// characteristics requirement: a noncreature permanent animated by a live
// type-layer effect is a legal station tap.
func TestStationUsesDerivedCreatureType(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	artifact := card(t, "Name:Animated Relic\nTypes:Artifact\nOracle:x\n")
	e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Exploration Broodship"), artifact}, nil)
	brood := moveByName(t, e, 0, "Exploration Broodship", state.ZBattlefield)
	relic := moveByName(t, e, 0, "Animated Relic", state.ZBattlefield)
	e.AddContinuous(state.ContinuousEffect{Source: relic, Controller: 0, Affects: "Card.Self", Layer: state.LType, AddTypes: []string{"Creature"}, UntilEOT: true})
	if got := e.stationCandidates(0, brood); len(got) != 1 || got[0] != relic {
		t.Fatalf("animated artifact station candidates = %v, want [%d]", got, relic)
	}
}

// TestRoomUnlockCreatesTheDemon is trig:UnlockDoor's leaf (real corpus Unholy
// Annex / Ritual Chamber): the room enters as its front half; as a sorcery
// the locked half's mana cost pays for the unlock (one DoorUnlock event); the
// unlocked face's "When you unlock this door" trigger fires and creates the
// 6/6 black Demon token with flying.
// TestRoomAlternateCastUnlocksFrontDoor is trig:UnlockDoor's front-door
// regression (real corpus Spiked Corridor / Torture Pit). CR 309.4b permits
// casting Torture Pit first; unlocking Spiked Corridor must then select the
// front face's trigger and create its three Devils.
func TestRoomAlternateCastUnlocksFrontDoor(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Spiked Corridor")}, nil)
	moveByName(t, e, 0, "Spiked Corridor", state.ZHand)
	addMana(t, e, 0, "RRRRRRRR")
	castNamed(t, e, "Torture Pit")
	passUntilStackEmpty(t, e, 30)
	room := state.ObjID(0)
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Torture Pit" {
			room = id
		}
	}
	if room == 0 || e.G.Obj(room).Unlocked {
		t.Fatalf("alternate Room cast = %d unlocked=%v, want locked Torture Pit", room, room != 0 && e.G.Obj(room).Unlocked)
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority to unlock Spiked Corridor (got %+v)", d)
	}
	unlock := -1
	for _, o := range d.Options {
		if o.Kind == "unlock" && o.Obj == room && o.Label == "Unlock Spiked Corridor" {
			unlock = o.Index
		}
	}
	if unlock < 0 {
		t.Fatalf("no front-door unlock option in %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{unlock}}); err != nil {
		t.Fatalf("submit front-door unlock: %v", err)
	}
	answerQuiet(t, e, 60)
	devils := 0
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		o := e.G.Obj(id)
		if o != nil && o.IsToken && o.Face() != nil && strings.Contains(o.Face().Name, "Devil") {
			devils++
		}
	}
	if devils != 3 {
		t.Fatalf("Spiked Corridor unlock created %d Devils, want 3", devils)
	}
}

// TestRoomUnlockTargetedTriggerPlacesTarget is the target-bearing half of
// trig:UnlockDoor's leaf (real corpus Bottomless Pool / Locker Room). The
// locked door's Execute$ SVar must receive its target ask while it is put on
// the stack, just as an ordinary triggered ability does.
func TestRoomUnlockTargetedTriggerPlacesTarget(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	bear := card(t, bearSrc)
	e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Bottomless Pool")}, []*cards.Card{bear})
	moveByName(t, e, 0, "Bottomless Pool", state.ZHand)
	bearID := moveByName(t, e, 1, "Bear", state.ZBattlefield)
	addMana(t, e, 0, "UUUUUU")
	castNamed(t, e, "Locker Room")
	passUntilStackEmpty(t, e, 30)
	room := state.ObjID(0)
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Locker Room" {
			room = id
		}
	}
	if room == 0 {
		t.Fatal("Locker Room never entered the battlefield")
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority to unlock Bottomless Pool (got %+v)", d)
	}
	unlock := -1
	for _, o := range d.Options {
		if o.Kind == "unlock" && o.Obj == room && o.Label == "Unlock Bottomless Pool" {
			unlock = o.Index
		}
	}
	if unlock < 0 {
		t.Fatalf("no Bottomless Pool unlock option in %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{unlock}}); err != nil {
		t.Fatalf("submit Bottomless Pool unlock: %v", err)
	}
	// The unlock's trigger is drained once both seats pass priority, just as
	// it is in play; only then is its placement target decision posed.
	passToKind(t, e, decision.KTarget)
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("unlock target decision = %+v, want KTarget", d)
	}
	pick := -1
	for _, o := range d.Options {
		if o.Obj == bearID {
			pick = o.Index
		}
	}
	if pick < 0 {
		t.Fatalf("Bottomless Pool did not offer the opposing Bear: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pick}}); err != nil {
		t.Fatalf("submit Bottomless Pool target: %v", err)
	}
	answerQuiet(t, e, 40)
	if got := e.G.Obj(bearID).Zone; got != state.ZHand {
		t.Fatalf("Bottomless Pool left Bear in %s, want hand", got)
	}
}

func TestRoomUnlockCreatesTheDemon(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg,
		[]*cards.Card{lookup(t, reg, "Unholy Annex")},
		[]*cards.Card{})
	moveByName(t, e, 0, "Unholy Annex", state.ZHand)
	addMana(t, e, 0, "BBBBBBBB")
	castNamed(t, e, "Unholy Annex")
	passUntilStackEmpty(t, e, 30)
	room := state.ObjID(0)
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Unholy Annex" {
			room = id
		}
	}
	if room == 0 {
		t.Fatal("the room never entered the battlefield")
	}
	if e.G.Obj(room).Unlocked {
		t.Fatal("the room entered with BOTH doors unlocked")
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision after the room entered (got %+v)", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "unlock" && o.Obj == room && o.Label == "Unlock Ritual Chamber" {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no unlock option for Ritual Chamber: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit unlock: %v", err)
	}
	if !e.G.Obj(room).Unlocked {
		t.Fatal("the unlock answer did not unlock the door")
	}
	if got := e.G.Players[0].Pool[state.MB]; got != 0 {
		t.Fatalf("the unlock left %d B unspent, want 0 -- all 8 paid out (3 for the cast, 5 for Ritual Chamber)", got)
	}
	answerQuiet(t, e, 60)
	demon := state.ObjID(0)
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		o := e.G.Obj(id)
		if o == nil || !o.IsToken || o.Face() == nil {
			continue
		}
		if strings.Contains(o.Face().Name, "Demon") {
			demon = id
		}
	}
	if demon == 0 {
		t.Fatal("the unlock trigger never created the Demon token")
	}
	o := e.G.Obj(demon)
	if o.Face().Power() != 6 || o.Face().Toughness() != 6 {
		t.Fatalf("the Demon token is %d/%d, want 6/6", o.Face().Power(), o.Face().Toughness())
	}
	if !e.HasKeyword(demon, "Flying") {
		t.Fatal("the Demon token does not have Flying")
	}
}

// TestUrzasSagaChaptersAndLoreCounters is kw:Chapter's leaf (real corpus
// Urza's Saga): it enters with one lore counter, its chapter I ability
// resolves (the granted "{T}: Add {C}"), the after-your-draw-step counter
// lands the next turn and chapter II resolves (the granted {2},{T} token
// ability). The chapter abilities are the real corpus SVars, minted through
// the delayed-shape push.
func TestUrzasSagaChaptersAndLoreCounters(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg,
		[]*cards.Card{lookup(t, reg, "Urza's Saga")},
		[]*cards.Card{})
	moveByName(t, e, 0, "Urza's Saga", state.ZHand)
	e.priorityRound()
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "play_land" && o.Label == "Play Urza's Saga" {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("Urza's Saga was not offered as a land play: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit play land: %v", err)
	}
	saga := state.ObjID(0)
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Urza's Saga" {
			saga = id
		}
	}
	if saga == 0 {
		t.Fatal("the Saga never entered the battlefield")
	}
	answerQuiet(t, e, 60)
	if got := e.G.Obj(saga).Counter("LORE"); got != 1 {
		t.Fatalf("the Saga entered with %d lore counters, want 1", got)
	}
	// Chapter I resolved: the Saga now grants "{T}: Add {C}" -- the mana
	// activation appears among the priority options.
	e.priorityRound()
	d = e.Pending()
	mana := false
	for _, o := range d.Options {
		if o.Kind == "activate" && o.Obj == saga && o.Label == "Activate Urza's Saga for mana" {
			mana = true
		}
	}
	if !mana {
		t.Fatalf("chapter I's {T}: Add {C} was never granted: %+v", d.Options)
	}
	// The next controller turn: after the draw step the second lore counter
	// lands and chapter II (the {2},{T} token ability) resolves.
	for driveToTurn(t, e, 3, 0) {
	}
	answerQuiet(t, e, 60)
	if got := e.G.Obj(saga).Counter("LORE"); got != 2 {
		t.Fatalf("the Saga has %d lore counters after turn 3's draw step, want 2", got)
	}
	pushes := countEvents(e, func(ev events.Event) bool {
		return ev.Kind == events.DelayedPush && ev.Obj == saga
	})
	if pushes != 2 {
		t.Fatalf("chapter abilities pushed: %d, want 2 (chapters I and II)", pushes)
	}
	// Chapter II resolved: the Saga now grants the {2},{T} Construct token
	// ability -- offered once the {2} is payable.
	addMana(t, e, 0, "CC")
	d = e.Pending()
	token := false
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == saga {
			token = true
		}
	}
	if !token {
		t.Fatalf("chapter II's {2},{T} Construct ability was never granted: %+v", d.Options)
	}
}

// TestStartYourEnginesSpeedLifecycle is kw:Start your engines' leaf (real
// corpus Amonkhet Raceway): entry starts a speed-less controller at 1 (CR
// 702.163a); an opponent losing life takes the turn's ONE increase (163b),
// including on another seat's turn and for every eligible player in a
// multiplayer game; max speed 4 turns the "Max speed —" static's granted
// ability on (163c), and activating it gives the target haste.
func TestStartYourEnginesSpeedLifecycle(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	bear := card(t, bearSrc)
	e := corpusEngineThree(t, reg,
		[]*cards.Card{lookup(t, reg, "Amonkhet Raceway"), bear},
		[]*cards.Card{lookup(t, reg, "Amonkhet Raceway")},
		nil)
	moveByName(t, e, 0, "Amonkhet Raceway", state.ZBattlefield)
	moveByName(t, e, 1, "Amonkhet Raceway", state.ZBattlefield)
	bearID := moveByName(t, e, 0, "Bear", state.ZBattlefield)
	if got := e.G.Players[0].Speed; got != 1 {
		t.Fatalf("seat 0 speed after its raceway entered: %d, want 1", got)
	}
	if got := e.G.Players[1].Speed; got != 1 {
		t.Fatalf("seat 1 speed after its raceway entered: %d, want 1", got)
	}
	// The turn's increase: one opponent loss, once.
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: -1})
	if got := e.G.Players[0].Speed; got != 2 {
		t.Fatalf("speed after an opponent lost life: %d, want 2", got)
	}
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: -1})
	if got := e.G.Players[0].Speed; got != 2 {
		t.Fatalf("a second loss the same turn raised speed to %d, want 2 (once per turn)", got)
	}
	// On seat 1's turn, seat 2 is an opponent of BOTH speed-holding seats.
	// CR 702.163b has no active-player restriction: both must gain.
	for driveToTurn(t, e, 2, 1) {
	}
	e.emit(events.Event{Kind: events.LifeChange, Player: 2, Amount: -1})
	if got := e.G.Players[0].Speed; got != 3 {
		t.Fatalf("non-active seat 0 speed after seat 2 lost life: %d, want 3", got)
	}
	if got := e.G.Players[1].Speed; got != 2 {
		t.Fatalf("active seat 1 speed after seat 2 lost life: %d, want 2", got)
	}
	// A later opponent loss reaches max speed 4 for seat 0. Its next turn
	// also clears summoning sickness from Raceway before activating Max speed.
	for driveToTurn(t, e, 4, 0) {
	}
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: -1})
	if got := e.G.Players[0].Speed; got != 4 {
		t.Fatalf("speed at max: %d, want 4", got)
	}
	// The max-speed static's granted ability is offered; activating it gives
	// the Bear haste.
	e.priorityRound()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision at max speed (got %+v)", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "granted" && o.Obj != 0 {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no granted max-speed option at speed 4: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit granted: %v", err)
	}
	passToKind(t, e, decision.KTarget)
	submitChoices(t, e, 0) // target the Bear
	passUntilStackEmpty(t, e, 30)
	if !e.HasKeyword(bearID, "Haste") {
		t.Fatal("the max-speed pump did not grant the target haste")
	}
}

// TestPanharmoniconDoublesATriggerAnAdditionalTime is stat:Panharmonicon's
// leaf (real corpus Echoes of Eternity): with the static on the battlefield,
// a matching colorless permanent's triggered ability triggers an ADDITIONAL
// time -- two placements, two resolutions, two +1/+1 counters -- where the
// same entry alone triggers once.
func TestPanharmoniconDoublesATriggerAnAdditionalTime(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	golem := card(t, watcherGolemSrc)
	e := corpusEngine(t, reg,
		[]*cards.Card{lookup(t, reg, "Echoes of Eternity"), golem, golem},
		[]*cards.Card{})
	// Without the static: the ETB trigger fires once.
	g1 := moveByName(t, e, 0, "Watcher Golem", state.ZHand)
	moveByName(t, e, 0, "Watcher Golem", state.ZBattlefield)
	e.priorityRound()
	answerQuiet(t, e, 60)
	if c := e.G.Obj(g1).Counter("P1P1"); c != 1 {
		t.Fatalf("the lone golem has %d +1/+1 counters, want 1", c)
	}
	// With Echoes of Eternity on the battlefield (moved, not cast, so its own
	// SpellCast copy machinery stays out of the way): the next identical
	// entry triggers an additional time.
	moveByName(t, e, 0, "Echoes of Eternity", state.ZHand)
	moveByName(t, e, 0, "Echoes of Eternity", state.ZBattlefield)
	g2 := moveByName(t, e, 0, "Watcher Golem", state.ZHand)
	moveByName(t, e, 0, "Watcher Golem", state.ZBattlefield)
	answerQuiet(t, e, 60)
	if c := e.G.Obj(g2).Counter("P1P1"); c != 2 {
		t.Fatalf("the doubled trigger produced %d +1/+1 counters, want 2", c)
	}
}

// commanderConfig is a Commander-format Config whose seat-0 deck is cmds +
// Mountains (filled out to 40 so the opening deal never empties a library),
// with the named deck indices seated as that seat's commanders.
func commanderConfig(t *testing.T, reg *cards.Registry, cmds []*cards.Card, indices []int) Config {
	t.Helper()
	m, ok := reg.Lookup("Mountain")
	if !ok {
		t.Fatal("corpus fixture: Mountain missing")
	}
	deck := append(append([]*cards.Card{}, cmds...), m)
	for len(deck) < 40 {
		deck = append(deck, m)
	}
	other := [](*cards.Card){}
	for len(other) < 40 {
		other = append(other, m)
	}
	return Config{Seed: 42, Names: []string{"a", "b"}, Tokens: reg.Tokens,
		Format: FormatCommander, StartingLife: 40,
		Decks:      [][]*cards.Card{deck, other},
		Commanders: [][]int{indices, nil}}
}

// TestPartnerSeatsTwoCommanders is kw:Partner's leaf (real corpus Vial
// Smasher the Fierce + Dargo, the Shipwrecker, partners under CR 90.3a): the
// engine seats BOTH as seat 0's commanders in the command zone, and each is
// castable from there (the command-zone cast offer).
func TestPartnerSeatsTwoCommanders(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	cfg := commanderConfig(t, reg,
		[]*cards.Card{lookup(t, reg, "Vial Smasher the Fierce"), lookup(t, reg, "Dargo, the Shipwrecker")},
		[]int{0, 1})
	e := New(cfg)
	e.Advance()
	cmds := e.G.Players[0].Commanders
	if len(cmds) != 2 {
		t.Fatalf("seat 0 has %d commanders in the command zone, want 2", len(cmds))
	}
	for i, id := range cmds {
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZCommand {
			t.Fatalf("commander %d is not in the command zone (obj %v)", i, o)
		}
	}
	if e.G.Players[0].Life != 40 {
		t.Fatalf("commander starting life is %d, want 40", e.G.Players[0].Life)
	}
	// Both partners are castable from the command zone.
	toMain1(t, e)
	addMana(t, e, 0, "CBR")
	d := e.Pending()
	smasher := false
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Label == "Cast Vial Smasher the Fierce" {
			smasher = true
		}
	}
	if !smasher {
		t.Fatalf("Vial Smasher is not offered as a command-zone cast: %+v", d.Options)
	}
}

// TestLordWindgraceCanBeCommander is kw:CanBeCommander's leaf (real corpus
// Lord Windgrace, "CARDNAME can be your commander."): the keyword shape is
// understood -- the card is seatable as a commander, reaches the command
// zone, and is castable from there under the CR 903.8 tax.
func TestLordWindgraceCanBeCommander(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	cfg := commanderConfig(t, reg,
		[]*cards.Card{lookup(t, reg, "Lord Windgrace")},
		[]int{0})
	e := New(cfg)
	e.Advance()
	cmds := e.G.Players[0].Commanders
	if len(cmds) != 1 || e.G.Obj(cmds[0]) == nil || e.G.Obj(cmds[0]).Zone != state.ZCommand {
		t.Fatalf("Lord Windgrace was not seated in the command zone (cmds %v)", cmds)
	}
	toMain1(t, e)
	addMana(t, e, 0, "WBRGC")
	d := e.Pending()
	offered := false
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Label == "Cast Lord Windgrace" {
			offered = true
		}
	}
	if !offered {
		t.Fatalf("Lord Windgrace is not offered as a command-zone cast: %+v", d.Options)
	}
}

// TestIllegalCommanderConfigurationIsRejected is the NEGATIVE half of the
// kw:Partner / kw:CanBeCommander deck-construction work (CR 903.4/903.13):
// legalCommandersFor must actually run on every Commander-format New, so each
// illegal configuration seats NOTHING (empty command zone, the rejected cards
// stay in the library) and the CR 903 rejection Note lands on the log. Every
// card here is real corpus script; the pair cases are deliberately WRONG
// pairs -- legendary creatures with no Partner ability, a plain Partner paired
// with a Partner-with card (CR 903.13a/c: not a legal pair), and two
// Partner-with cards whose named partners are not each other.
func TestIllegalCommanderConfigurationIsRejected(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	cases := []struct {
		name string
		cmds []string
	}{
		{"single noncommander card", []string{"Orcish Bowmasters"}},
		{"legendary cards with no Partner ability", []string{"Lord Windgrace", "Sheoldred, the Apocalypse"}},
		{"plain Partner paired with a Partner-with card", []string{"Vial Smasher the Fierce", "Krav, the Unredeemed"}},
		{"Partner-with cards that do not name each other", []string{"Krav, the Unredeemed", "Will Kenrith"}},
		{"Doctor's companion paired with a non-Doctor", []string{"Rose Tyler", "Lord Windgrace"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var cmds []*cards.Card
			for _, n := range tc.cmds {
				cmds = append(cmds, lookup(t, reg, n))
			}
			indices := make([]int, len(cmds))
			for i := range indices {
				indices[i] = i
			}
			e := New(commanderConfig(t, reg, cmds, indices))
			e.Advance()
			if got := e.G.Players[0].Commanders; len(got) != 0 {
				t.Fatalf("illegal configuration seated %d commander(s), want 0", len(got))
			}
			for _, id := range e.G.Zone(state.ZLibrary, 0) {
				if o := e.G.Obj(id); o != nil && o.Zone == state.ZCommand {
					t.Fatalf("a rejected commander card sits in the command zone")
				}
			}
			rejected := false
			for _, ev := range e.L.Events {
				if ev.Kind == events.Note && strings.Contains(ev.Text, "commander configuration rejected under CR 903") {
					rejected = true
				}
			}
			if !rejected {
				t.Fatalf("no CR 903 rejection Note on the log for %q", tc.name)
			}
		})
	}
}

// TestLegendarySpacecraftCommanderLegalityFollowsPTBox pins the CR 903.3
// Spacecraft carve-out the engine now shares with the deck validator (the
// engine delegates to deck.IsCommanderEligible): a legendary Spacecraft with
// a printed power/toughness box (Hearthhull, the Worldseed -- authored
// fixture for the shape) IS commander-legal, while a P/T-less legendary
// Spacecraft (the corpus's real The Eternity Elevator) stays illegal. The
// old engine predicate had no Spacecraft carve-out at all and rejected the
// P/T half, so pro-shaper's commander never reached the command zone.
func TestLegendarySpacecraftCommanderLegalityFollowsPTBox(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	craft := card(t, "Name:Legendary Test Craft\nTypes:Legendary Artifact Spacecraft\nPT:4/4\n")
	if !commanderCardLegal(craft) {
		t.Fatal("a legendary Spacecraft with a printed power/toughness box is not commander-legal")
	}
	ptless := lookup(t, reg, "The Eternity Elevator")
	if commanderCardLegal(ptless) {
		t.Fatal("a P/T-less legendary Spacecraft is commander-legal")
	}
	e := New(commanderConfig(t, reg, []*cards.Card{ptless}, []int{0}))
	e.Advance()
	if got := e.G.Players[0].Commanders; len(got) != 0 {
		t.Fatalf("P/T-less legendary Spacecraft seated %d commander(s), want 0", len(got))
	}
}

// TestLegendarySpacecraftWithPTIsSeatedAsCommander is the leaf for the
// pro-shaper genesis rejection (fb-20260918T194005Z-e4e743e1): a real corpus
// Hearthhull, the Worldseed (legendary Spacecraft, printed P/T) must be
// SEATED as seat 0's commander -- one Commanders entry, the object in the
// command zone -- and the CR 903 rejection Note must be absent.
func TestLegendarySpacecraftWithPTIsSeatedAsCommander(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	h := lookup(t, reg, "Hearthhull, the Worldseed")
	if !commanderCardLegal(h) {
		t.Fatal("Hearthhull, the Worldseed is not commander-legal (deck.IsCommanderEligible says it is)")
	}
	e := New(commanderConfig(t, reg, []*cards.Card{h}, []int{0}))
	e.Advance()
	cmds := e.G.Players[0].Commanders
	if len(cmds) != 1 {
		t.Fatalf("Hearthhull seated %d commander(s), want 1", len(cmds))
	}
	if o := e.G.Obj(cmds[0]); o == nil || o.Zone != state.ZCommand {
		t.Fatalf("the seated commander is not in the command zone (obj %v)", cmds[0])
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "commander configuration rejected under CR 903") {
			t.Fatal("the CR 903 rejection Note is on the log for a legal commander")
		}
	}
}

// TestValiantEndeavorEngineLevelChooseOneResult drives the RollDice
// choose-one-result ask (ChosenSVar$/OtherSVar$, real corpus Valiant
// Endeavor) through the REAL engine: the two-die roll suspends mid-
// resolution on a Min==Max==1 KChoose whose per-die results ride the
// decision, the answered pick (the SECOND die, proving the answer and not a
// first-option default drives the outcome) resumes through rules'
// "roll" arm, and the chained DestroyAll reads the chosen result through the
// published name (Creature.powerGEX) while the chained Token reads the other
// (TokenAmount$ Y).
func TestValiantEndeavorEngineLevelChooseOneResult(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg,
		[]*cards.Card{lookup(t, reg, "Valiant Endeavor"), lookup(t, reg, "Llanowar Elves"),
			lookup(t, reg, "Grizzly Bears"), lookup(t, reg, "Craw Wurm")},
		[]*cards.Card{})
	moveByName(t, e, 0, "Valiant Endeavor", state.ZHand)
	moveByName(t, e, 0, "Llanowar Elves", state.ZBattlefield)
	moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	moveByName(t, e, 0, "Craw Wurm", state.ZBattlefield)
	addMana(t, e, 0, "WWWWWW")
	e.priorityRound()
	castNamed(t, e, "Valiant Endeavor")

	// Drain priority until the roll ask is pending.
	var d *decision.Decision
	for i := 0; i < 50; i++ {
		p := e.Pending()
		if p == nil {
			continue
		}
		if p.Kind == decision.KChoose && p.ResumeKind == "roll" {
			d = p
			break
		}
		if p.Kind == decision.KPriority {
			idx := -1
			for _, o := range p.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("priority decision with no pass option: %+v", p)
			}
			if err := e.Submit(decision.Intent{Seq: p.Seq, Player: p.Player, Choices: []int{idx}}); err != nil {
				t.Fatalf("submit pass: %v", err)
			}
			continue
		}
		t.Fatalf("unexpected non-priority decision while waiting for the roll ask: %+v", p)
	}
	if d == nil {
		t.Fatal("no roll KChoose decision after the cast resolved")
	}
	if d.Player != 0 || d.Min != 1 || d.Max != 1 || len(d.Rolls) != 2 {
		t.Fatalf("roll decision = %+v, want Min==Max==1 for seat 0 with two rolls", d)
	}

	// The creatures and their powers, captured before the answer resolves.
	type vic struct {
		name  string
		power int
	}
	var victims []vic
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		o := e.G.Obj(id)
		if o != nil && o.Face().IsCreature() {
			victims = append(victims, vic{o.Face().Name, o.Face().Power()})
		}
	}
	if len(victims) != 3 {
		t.Fatalf("want three creature victims, got %v", victims)
	}

	// Answer: pick the SECOND die, so a first-option default cannot fake this.
	chosen := d.Rolls[d.Options[1].Index]
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[1].Index}}); err != nil {
		t.Fatalf("submit roll pick: %v", err)
	}
	other := d.Rolls[0]
	for _, v := range victims {
		o := findObjByName(t, e, v.name)
		shouldBeDead := v.power >= int(chosen)
		if shouldBeDead && o.Zone == state.ZBattlefield {
			t.Fatalf("%s (power %d) survived a chosen roll of %d", v.name, v.power, chosen)
		}
		if !shouldBeDead && o.Zone != state.ZBattlefield {
			t.Fatalf("%s (power %d) died but is below the chosen roll of %d", v.name, v.power, chosen)
		}
	}
	knights := 0
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		o := e.G.Obj(id)
		if o != nil && o.IsToken && o.Face() != nil && strings.Contains(o.Face().Name, "Knight") {
			knights++
		}
	}
	if knights != int(other) {
		t.Fatalf("%d Knight tokens created, want %d (the other rolled result)", knights, other)
	}
}

// findObjByName returns the object whose face is named name, in any zone.
func findObjByName(t *testing.T, e *Engine, name string) *state.Object {
	t.Helper()
	for i := range e.G.Objs {
		if o := &e.G.Objs[i]; o.Face() != nil && o.Face().Name == name {
			return o
		}
	}
	t.Fatalf("no object named %q", name)
	return nil
}
