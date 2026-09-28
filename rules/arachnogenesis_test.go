package rules

// arachnogenesis_test.go pins the reported deck-gap end to end on the real
// corpus card: Arachnogenesis' SVar:X:Count$Valid Creature.attackingYou
// ("create X 1/2 green Spider creature tokens with reach, where X is the
// number of creatures attacking you"). The reported defect was that the card
// created ZERO tokens — the unknown attackingYou predicate matched nobody, so
// X degraded to 0 and only the curse replacement registered. This file casts
// the compiled corpus SA in a real three-seat combat with two attackers
// declared at the caster and one at another defender, and asserts the two
// Spiders actually enter.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// arachCorpusCard asserts the audited script shape the count lives in: the
// spell body is SP$ Token with TokenAmount$ X -> the g_1_2_spider_reach
// script, and SVar:X is the Count$Valid Creature.attackingYou idiom.
func arachCorpusCard(t *testing.T, reg *cards.Registry) *cards.Card {
	t.Helper()
	c, ok := reg.Lookup("Arachnogenesis")
	if !ok {
		t.Fatal("corpus has no Arachnogenesis -- the card this defect is about is untestable")
	}
	f := c.Faces[0]
	var tokenSA *cards.SA
	for _, a := range f.Abilities {
		if a.Kind == "SP" && a.API == "Token" {
			tokenSA = a
		}
	}
	if tokenSA == nil {
		t.Fatalf("corpus Arachnogenesis carries no SP$ Token ability: %+v", f.Abilities)
	}
	if tokenSA.Params["TokenAmount"] != "X" || tokenSA.Params["TokenScript"] != "g_1_2_spider_reach" {
		t.Fatalf("corpus Arachnogenesis token params = %v, want TokenAmount$ X -> g_1_2_spider_reach", tokenSA.Params)
	}
	if got := f.SVars["X"]; got != "Count$Valid Creature.attackingYou" {
		t.Fatalf("corpus Arachnogenesis SVar:X = %q, want Count$Valid Creature.attackingYou", got)
	}
	if reg.Tokens["g_1_2_spider_reach"] == nil {
		t.Fatal("corpus token registry has no g_1_2_spider_reach for Arachnogenesis")
	}
	return c
}

// countSpiders counts seat p's battlefield tokens named "Spider Token".
func countSpiders(t *testing.T, e *Engine, p state.PlayerID) int {
	t.Helper()
	n := 0
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		if f := e.G.Obj(id).Face(); f != nil && f.Name == "Spider Token" {
			n++
		}
	}
	return n
}

// arachFixture builds the three-seat game the pin drives: the corpus
// Arachnogenesis plus Plains filler in seat 0's deck, three distinct raiders
// plus Mountains in seat 1's (so the attackers enter through LOGGED MoveZone
// events, not the eventless onBoard fixture, and the replay check stays
// honest), Mountains in seat 2's. Returns the engine, the config, seat 0's
// Arachnogenesis id, and the three raiders' ids.
func arachFixture(t *testing.T, reg *cards.Registry) (e *Engine, cfg Config, id state.ObjID, raiders [3]state.ObjID) {
	t.Helper()
	ara := arachCorpusCard(t, reg)
	plains, ok := reg.Lookup("Plains")
	if !ok {
		t.Fatal("corpus has no Plains to pad the deck")
	}
	mountain, ok := reg.Lookup("Mountain")
	if !ok {
		t.Fatal("corpus has no Mountain to pad the raider deck")
	}
	deck := []*cards.Card{ara}
	for len(deck) < 40 {
		deck = append(deck, plains)
	}
	rdeck := []*cards.Card{
		card(t, "Name:Raider One\nManaCost:2 R\nTypes:Creature Goblin\nPT:2/2\nOracle:x\n"),
		card(t, "Name:Raider Two\nManaCost:2 R\nTypes:Creature Goblin\nPT:2/2\nOracle:x\n"),
		card(t, "Name:Raider Three\nManaCost:2 R\nTypes:Creature Goblin\nPT:2/2\nOracle:x\n"),
	}
	for len(rdeck) < 40 {
		rdeck = append(rdeck, mountain)
	}
	cfg = Config{Seed: 447, Names: []string{"caster", "raider", "bystander"},
		Decks:  [][]*cards.Card{deck, rdeck, mountainDeck(t, 40)},
		Tokens: reg.Tokens}
	e = New(cfg)
	e.Advance()

	// Arachnogenesis into seat 0's hand.
	for _, cand := range e.G.Zone(state.ZLibrary, 0) {
		if e.G.Obj(cand).Face().Name == "Arachnogenesis" {
			id = cand
		}
	}
	if id == 0 {
		t.Fatal("Arachnogenesis absent from seat 0's library")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZHand})

	// The raiders onto seat 1's battlefield through logged moves from
	// wherever genesis dealt them. SummonSick is real machinery's problem:
	// the entry sets it (CR 302.6's control-since-turn-start read), and the
	// intervening turn starts clear it exactly as a real battlefield entry
	// would -- which is what keeps the replay byte-identical.
	found := 0
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, cand := range e.G.Zone(z, 1) {
			o := e.G.Obj(cand)
			if o.Card == nil || o.IsToken {
				continue
			}
			n := o.Face().Name
			idx := -1
			switch n {
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
			found++
		}
	}
	if found != 3 {
		t.Fatalf("placed %d of 3 raiders from seat 1's hand/library", found)
	}
	return e, cfg, id, raiders
}

// driveToAttackers passes priority (answering any multi-card cleanup discard
// with its full Min..Max -- passToKind's single-choice non-priority answer
// cannot cross one) until seat 1's declare-attackers decision is pending.
func driveToAttackers(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 120; i++ {
		d := e.Pending()
		if d == nil {
			continue
		}
		if d.Kind == decision.KAttackers {
			return
		}
		if d.Kind != decision.KPriority {
			cs := make([]int, 0, d.Max)
			for j := 0; j < d.Max && j < len(d.Options); j++ {
				cs = append(cs, d.Options[j].Index)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: cs}); err != nil {
				t.Fatalf("answer %s while driving to combat: %v", d.Kind, err)
			}
			continue
		}
		passIdx := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				passIdx = o.Index
			}
		}
		if passIdx < 0 {
			t.Fatalf("priority decision with no pass option: %+v", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{passIdx}}); err != nil {
			t.Fatalf("pass priority: %v", err)
		}
	}
	t.Fatalf("no attackers decision within the pass budget (turn %d seat %d step %s)", e.G.Turn, e.G.Active, e.G.Step)
}

// TestArachnogenesisCreatesTokensForAttackersAtYou drives seat 1's turn: two
// of its creatures attack seat 0 (the caster), one attacks seat 2, and seat 0
// casts the corpus Arachnogenesis in the declare-attackers priority window.
// The resolution must create exactly the two Spiders -- not zero (the
// reported defect) and not three (a seat-blind IsAttacking read).
func TestArachnogenesisCreatesTokensForAttackersAtYou(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, id, raiders := arachFixture(t, reg)

	driveToAttackers(t, e)
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected an attackers decision, got %+v", d)
	}
	// Raiders One and Two attack seat 0, Raider Three attacks seat 2: one
	// option per (attacker, defender) pair (CR 506.2), the pair's defender
	// on Option.Player.
	var choices []int
	for _, o := range d.Options {
		if (o.Obj == raiders[0] || o.Obj == raiders[1]) && o.Player == 0 {
			choices = append(choices, o.Index)
		}
		if o.Obj == raiders[2] && o.Player == 2 {
			choices = append(choices, o.Index)
		}
	}
	if len(choices) != 3 {
		t.Fatalf("expected 3 attack pairs (two at seat 0, one at seat 2), got %d from %+v", len(choices), d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
		t.Fatalf("submit attackers: %v", err)
	}

	// Precondition the count rests on: exactly two declared attackers at the
	// caster, one at seat 2 -- and the raiders are attackable at all.
	nAt0, nAt2 := 0, 0
	for _, oid := range e.G.Zone(state.ZBattlefield, 1) {
		o := e.G.Obj(oid)
		if o.Attacking == 0 {
			nAt0++
		}
		if o.Attacking == 2 {
			nAt2++
		}
	}
	if nAt0 != 2 || nAt2 != 1 {
		t.Fatalf("declared attackers broken: %d at seat 0, %d at seat 2", nAt0, nAt2)
	}

	// {2}{G} for the cast, floated mid-step (the pool drains only at the
	// step boundary, and the spell resolves inside this window).
	for _, r := range "CCG" {
		e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: string(r), Amount: 1})
	}

	// Cross the CR 508.2 priority window, casting from seat 0's seat.
	cast := false
	for i := 0; i < 64 && !cast; i++ {
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			t.Fatalf("non-priority decision %+v in the declare-attackers window", d)
		}
		if d.Player != 0 {
			for _, o := range d.Options {
				if o.Kind == "pass" {
					if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}); err != nil {
						t.Fatalf("pass priority: %v", err)
					}
					break
				}
			}
			continue
		}
		idx := -1
		for _, o := range d.Options {
			if o.Kind == "cast" && o.Obj == id {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("no cast option for Arachnogenesis at seat 0's priority: %+v", d.Options)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
			t.Fatalf("submit cast: %v", err)
		}
		cast = true
	}
	if !cast {
		t.Fatal("never reached seat 0's priority in the declare-attackers window")
	}
	passUntilStackEmpty(t, e, 20)

	// The fix's whole point: the two Spiders exist, 1/2, the g_1_2 token.
	if got := countSpiders(t, e, 0); got != 2 {
		t.Fatalf("Spider tokens on seat 0's battlefield = %d, want 2 (the reported defect made 0; a seat-blind IsAttacking read would make 3)", got)
	}
	if e.G.Obj(id).Zone != state.ZGraveyard {
		t.Fatalf("Arachnogenesis zone %s after resolving", e.G.Obj(id).Zone)
	}
	// The curse rider resolved with the spell (the reported "only registers
	// its curse replacement" state was the pre-fix half-behaviour; the fix
	// keeps the registration AND adds the tokens). The bodyless Prevent$
	// True DamageDone form is the complete-replacement idiom the Effect
	// registration keeps (effects/misc.go's bodyless-prevent arm).
	curse := false
	for _, ce := range e.continuous {
		if ce.Source == id && ce.ReplacementEvent == "DamageDone" &&
			ce.ReplacementParams["Prevent"] == "True" &&
			ce.ReplacementParams["ValidSource"] == "Creature.nonSpider" {
			curse = true
		}
	}
	if !curse {
		t.Fatal("Arachnogenesis' non-Spider combat-damage prevention never registered")
	}
	replayCheck(t, e, cfg)
}
