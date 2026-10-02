package mzbridge

import (
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

func isPass(o *decision.Option) bool { return o.Kind == "pass" }

// An opening board: turn 1, nothing in play, two cards in each hand.
func openingBoard(t testing.TB, fx fixtureCards) (*rules.Engine, *decision.Decision) {
	e, _, d := stage(t, fx, rules.Stage{Turn: 1, Active: 0, Starting: 0, Step: state.StepMain1,
		Players: []rules.StagedPlayer{
			{Life: 20, Library: rep(fx.wood, 5), Hand: []*cards.Card{fx.wood, fx.bear}},
			{Life: 20, Library: rep(fx.peak, 4), Hand: []*cards.Card{fx.peak, fx.zap, fx.zap}},
		}})
	return e, d
}

func TestEncodeOpeningBoard(t *testing.T) {
	fx := fixtures(t)
	e, d := openingBoard(t, fx)
	if d.Kind != decision.KPriority || d.Player != 0 {
		t.Fatalf("decision %s for %d", d.Kind, d.Player)
	}
	golden(t, "opening", names(t, e, 0, d, EncodeOptions{}))
}

// Lands (two untapped, one tapped) and a tapped creature with a +1/+1
// counter and one damage marked.
func TestEncodeLandsAndDamagedCreature(t *testing.T) {
	fx := fixtures(t)
	e, _, d := stage(t, fx, rules.Stage{Turn: 3, Active: 0, Starting: 0, Step: state.StepMain2,
		Players: []rules.StagedPlayer{
			{Life: 18, Library: rep(fx.wood, 3), LandsPlayed: 1},
			{Life: 7, Library: rep(fx.peak, 3)},
		},
		Permanents: []rules.StagedPermanent{
			{Card: fx.wood, Owner: 0, Controller: 0},
			{Card: fx.wood, Owner: 0, Controller: 0},
			{Card: fx.wood, Owner: 0, Controller: 0, Tapped: true},
			{Card: fx.bear, Owner: 0, Controller: 0, Tapped: true, Damage: 1,
				Counters: []rules.StagedCounter{{Kind: "P1P1", N: 1}}},
		}})
	golden(t, "lands_creature", names(t, e, 0, d, EncodeOptions{}))

	// The two untapped lands have the same Permanent.getValue key, so
	// upstream's TreeMap keeps one of them; KeepDuplicatePermanents keeps both.
	const second = "root/Player#1/Battlefield#1/Testwood#3"
	if got := names(t, e, 0, d, EncodeOptions{}); slices.Contains(got, second) {
		t.Errorf("duplicate permanents were not collapsed")
	}
	if got := names(t, e, 0, d, EncodeOptions{KeepDuplicatePermanents: true}); !slices.Contains(got, second) {
		t.Errorf("KeepDuplicatePermanents did not keep the third land")
	}
}

// An attack with a blocker declared: seat 0's bear attacks, seat 1's hawk
// blocks it, and seat 0 has priority in the declare-blockers step.
func TestEncodeAttackWithBlocker(t *testing.T) {
	fx := fixtures(t)
	e, ids, d := stage(t, fx, rules.Stage{Turn: 3, Active: 0, Starting: 0, Step: state.StepDeclareBlockers, Enter: rules.StageBeginStep,
		Players: []rules.StagedPlayer{
			{Life: 20, Library: rep(fx.wood, 3)},
			{Life: 20, Library: rep(fx.peak, 3)},
		},
		Permanents: []rules.StagedPermanent{
			{Card: fx.bear, Owner: 0, Controller: 0},
			{Card: fx.hawk, Owner: 1, Controller: 1},
		},
		Attackers: []rules.StagedAttack{{Attacker: 0, Defender: 1}}})
	if d.Kind != decision.KBlockers || d.Player != 1 {
		t.Fatalf("decision %s for %d, want blockers for 1", d.Kind, d.Player)
	}
	d = pick(t, e, func(o *decision.Option) bool { return o.Obj == ids.Permanents[1] && o.Attacker == ids.Permanents[0] })
	if d.Kind != decision.KPriority || d.Player != 0 || e.Game().Step != state.StepDeclareBlockers {
		t.Fatalf("decision %s for %d in %v", d.Kind, d.Player, e.Game().Step)
	}
	if got := e.Game().Obj(ids.Permanents[0]).BlockedBy; len(got) != 1 {
		t.Fatalf("blockers %v", got)
	}
	golden(t, "attack_block", names(t, e, 0, d, EncodeOptions{}))
}

// A spell on the stack with a target: seat 0 casts the burn spell at seat
// 1's hawk and keeps priority.
func TestEncodeSpellOnStackWithTarget(t *testing.T) {
	fx := fixtures(t)
	e, ids, d := stage(t, fx, rules.Stage{Turn: 3, Active: 0, Starting: 0, Step: state.StepMain1,
		Players: []rules.StagedPlayer{
			{Life: 20, Library: rep(fx.peak, 3), Hand: []*cards.Card{fx.zap}, ManaPool: "R"},
			{Life: 20, Library: rep(fx.peak, 3)},
		},
		Permanents: []rules.StagedPermanent{
			{Card: fx.hawk, Owner: 1, Controller: 1},
		}})
	d = pick(t, e, func(o *decision.Option) bool { return o.Kind == "cast" })
	for n := 0; n < 6 && d.Kind != decision.KPriority; n++ {
		switch d.Kind {
		case decision.KTarget:
			d = pick(t, e, func(o *decision.Option) bool { return o.Obj == ids.Permanents[0] })
		default:
			t.Fatalf("unexpected %s decision while casting: %+v", d.Kind, d.Options)
		}
	}
	g := e.Game()
	if len(g.Stack) != 1 || d.Kind != decision.KPriority {
		t.Fatalf("stack %v, decision %s", g.Stack, d.Kind)
	}
	if tg := g.Obj(g.Stack[0]).Targets; len(tg) != 1 || tg[0].Obj != ids.Permanents[0] {
		t.Fatalf("targets %+v", tg)
	}
	golden(t, "stack_target", names(t, e, d.Player, d, EncodeOptions{}))
}

// A card in exile and a token on the battlefield.
func TestEncodeExiledCard(t *testing.T) {
	fx := fixtures(t)
	e, _, d := stage(t, fx, rules.Stage{Turn: 3, Active: 0, Starting: 0, Step: state.StepMain1,
		Players: []rules.StagedPlayer{
			{Life: 20, Library: rep(fx.wood, 3), LandsPlayed: 1},
			{Life: 20, Library: rep(fx.peak, 3), Exile: []*cards.Card{fx.hawk}},
		}})
	golden(t, "exile", names(t, e, 0, d, EncodeOptions{}))
}

func TestEncodeToken(t *testing.T) {
	fx := fixtures(t)
	e, _, d := stage(t, fx, rules.Stage{Turn: 3, Active: 0, Starting: 0, Step: state.StepMain1,
		Players: []rules.StagedPlayer{
			{Life: 20, Library: rep(fx.wood, 3), LandsPlayed: 1},
			{Life: 20, Library: rep(fx.peak, 3)},
		},
		Permanents: []rules.StagedPermanent{
			{Token: "t_cat", Owner: 1, Controller: 1, Sick: true,
				Counters: []rules.StagedCounter{{Kind: "P1P1", N: 2}}},
		}})
	golden(t, "token", names(t, e, 0, d, EncodeOptions{}))
}

// hiddenPair builds two games that differ only in what seat 1 holds and in
// the order of both libraries. Each seat owns the same multiset of cards in
// both games.
func hiddenPair(t testing.TB, fx fixtureCards) (a, b *rules.Engine, da, db *decision.Decision) {
	mk := func(lib0, hand1, lib1 []*cards.Card) (*rules.Engine, *decision.Decision) {
		e, _, d := stage(t, fx, rules.Stage{Turn: 3, Active: 0, Starting: 0, Step: state.StepMain1,
			Players: []rules.StagedPlayer{
				{Life: 20, Library: lib0, Hand: []*cards.Card{fx.wood, fx.bear}},
				{Life: 20, Library: lib1, Hand: hand1, Graveyard: []*cards.Card{fx.zap}},
			},
			Permanents: []rules.StagedPermanent{
				{Card: fx.wood, Owner: 0, Controller: 0},
				{Card: fx.peak, Owner: 1, Controller: 1},
			}})
		return e, d
	}
	a, da = mk([]*cards.Card{fx.wood, fx.bear, fx.hawk, fx.wood},
		[]*cards.Card{fx.zap, fx.zap}, []*cards.Card{fx.peak, fx.hawk, fx.bear, fx.peak})
	b, db = mk([]*cards.Card{fx.hawk, fx.wood, fx.wood, fx.bear},
		[]*cards.Card{fx.hawk, fx.bear}, []*cards.Card{fx.zap, fx.peak, fx.peak, fx.zap})
	return
}

// With the opponent's hand hidden the id set is a function of what the
// deciding seat may see: two games that differ only in the opponent's hand
// contents and in library order encode identically.
func TestEncodeHidesOpponentHandAndLibraryOrder(t *testing.T) {
	fx := fixtures(t)
	a, b, da, db := hiddenPair(t, fx)
	if da.Player != 0 || db.Player != 0 {
		t.Fatalf("deciders %d %d", da.Player, db.Player)
	}
	ia, ib := ids(t, a, 0, da, EncodeOptions{}), ids(t, b, 0, db, EncodeOptions{})
	if !slices.Equal(ia, ib) {
		t.Errorf("hidden encodings differ:\n%s\n--\n%s", strings.Join(names(t, a, 0, da, EncodeOptions{}), "\n"),
			strings.Join(names(t, b, 0, db, EncodeOptions{}), "\n"))
	}
	// The hand is a count, and no card of it is named.
	na := names(t, a, 0, da, EncodeOptions{})
	if !slices.Contains(na, "root/Opponent#1/CardsInHand@1#1") || slices.Contains(na, "root/Opponent#1/Hand#1") {
		t.Errorf("opponent hand is not a bare count")
	}
	for _, n := range na {
		if strings.Contains(n, "Opponent#1/Hand") {
			t.Errorf("opponent hand is encoded: %s", n)
		}
	}
	// The oracle mode is the control: it does tell the two games apart.
	oa, ob := ids(t, a, 0, da, EncodeOptions{SeeOpponentHand: true}), ids(t, b, 0, db, EncodeOptions{SeeOpponentHand: true})
	if slices.Equal(oa, ob) {
		t.Errorf("perfect-information encodings are equal; the test's two games do not differ")
	}
	if n := names(t, a, 0, da, EncodeOptions{SeeOpponentHand: true}); !slices.Contains(n, "root/Opponent#1/Hand#1/Test Zap#2") {
		t.Errorf("perfect-information encoding does not list the opponent hand")
	}
}

// The Java rule (StateEncoder.java:601) keys the hand on the DECIDING
// player, not on the encoder's owner: when the other seat decides, its hand
// is the one encoded and the owner's becomes a count.
func TestEncodeHandFollowsTheDecider(t *testing.T) {
	fx := fixtures(t)
	e, d := openingBoard(t, fx)
	other := *d
	other.Player = 1
	other.Options = nil
	n := names(t, e, 0, &other, EncodeOptions{})
	if !slices.Contains(n, "root/Opponent#1/Hand#1/Test Zap#2") || !slices.Contains(n, "root/Player#1/CardsInHand@1#1") {
		t.Errorf("hand did not follow the decider:\n%s", strings.Join(n, "\n"))
	}
	if !slices.Contains(n, "root/Opponent#1/IsDecisionPlayer#1") || slices.Contains(n, "root/Player#1/IsDecisionPlayer#1") {
		t.Errorf("IsDecisionPlayer is on the wrong player")
	}
}

// The same state encodes to the same ids every time, from a fresh encoder
// and from a reused one, whatever was encoded in between.
func TestEncodeIsDeterministic(t *testing.T) {
	fx := fixtures(t)
	a, b, da, db := hiddenPair(t, fx)
	want := ids(t, a, 0, da, EncodeOptions{SeeOpponentHand: true})
	enc, fs := NewEncoder(), NewFeatureSet()
	for i := 0; i < 20; i++ {
		if err := enc.Encode(b, 1, db, Priority, "priority", fs, EncodeOptions{SeeOpponentHand: true}); err != nil {
			t.Fatal(err)
		}
		if err := enc.Encode(a, 0, da, Priority, "priority", fs, EncodeOptions{SeeOpponentHand: true}); err != nil {
			t.Fatal(err)
		}
		if got := fs.IDs(); !slices.Equal(got, want) {
			t.Fatalf("round %d: reused encoder gave %d ids, fresh gave %d", i, len(got), len(want))
		}
		if got := ids(t, a, 0, da, EncodeOptions{SeeOpponentHand: true}); !slices.Equal(got, want) {
			t.Fatalf("round %d: fresh encoder is not repeatable", i)
		}
	}
	// Emission ORDER is deterministic too (no map iteration reaches it).
	order := func() []int32 {
		fs := NewFeatureSet()
		var seq []int32
		fs.onEmit = func(id int32, _ *Node, _ string) { seq = append(seq, id) }
		if err := Encode(a, 0, da, Priority, "priority", fs, EncodeOptions{SeeOpponentHand: true}); err != nil {
			t.Fatal(err)
		}
		return seq
	}
	first := order()
	for i := 0; i < 20; i++ {
		if !slices.Equal(order(), first) {
			t.Fatalf("emission order changed on run %d", i)
		}
	}
}

// Swapping the viewer swaps the Player and Opponent subtrees. The decider
// stays the same, so the hand rule does not move; isController and the
// PlayerA/PlayerB entity names are relative to the viewer and are not in
// this board.
func TestEncodePerspectiveSwap(t *testing.T) {
	fx := fixtures(t)
	a, _, da, _ := hiddenPair(t, fx)
	for _, opts := range []EncodeOptions{{}, {SeeOpponentHand: true}} {
		v0 := names(t, a, 0, da, opts)
		v1 := names(t, a, 1, da, opts)
		swap := func(p string) string {
			switch {
			case strings.HasPrefix(p, "root/Player#1"):
				return "root/Opponent#1" + strings.TrimPrefix(p, "root/Player#1")
			case strings.HasPrefix(p, "root/Opponent#1"):
				return "root/Player#1" + strings.TrimPrefix(p, "root/Opponent#1")
			}
			return p
		}
		swapped := make([]string, len(v0))
		for i, p := range v0 {
			swapped[i] = swap(p)
		}
		slices.Sort(swapped)
		if !slices.Equal(swapped, v1) {
			t.Errorf("viewer 1 is not viewer 0 with Player and Opponent swapped (opts %+v):\n%s\n--\n%s",
				opts, strings.Join(swapped, "\n"), strings.Join(v1, "\n"))
		}
		if slices.Equal(v0, v1) {
			t.Errorf("the two perspectives are identical; the board is symmetric and proves nothing")
		}
	}
}

func TestEncodeRejectsWhatItCannotEncode(t *testing.T) {
	fx := fixtures(t)
	e, d := openingBoard(t, fx)
	if err := Encode(e, 2, d, Priority, "priority", NewFeatureSet(), EncodeOptions{}); err == nil {
		t.Errorf("viewer 2 of a two-player game was accepted")
	}
	if err := Encode(nil, 0, d, Priority, "priority", NewFeatureSet(), EncodeOptions{}); err == nil {
		t.Errorf("nil engine was accepted")
	}
}

func TestEncodeNames(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"Urza's", "URZAS"}, {"Power-Plant", "POWER_PLANT"}, {"Time Lord", "TIME_LORD"}, {"Elf", "ELF"},
	} {
		if got := subTypeName(c.in); got != c.want {
			t.Errorf("subTypeName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if got := manaCost("3 W W"); !slices.Equal(got, []string{"{3}", "{W}", "{W}"}) {
		t.Errorf("manaCost = %v", got)
	}
	if got := manaCost("X GW"); !slices.Equal(got, []string{"{X}", "{G/W}"}) {
		t.Errorf("manaCost hybrid = %v", got)
	}
	if got := manaCost("no cost"); got != nil {
		t.Errorf("no cost = %v", got)
	}
	mana, value, other, tap := abilityCost("2 G T Sac<1/CARDNAME>")
	if !slices.Equal(mana, []string{"{2}", "{G}"}) || value != 3 || !slices.Equal(other, []string{"{T}", "Sac<1/CARDNAME>"}) || !tap {
		t.Errorf("abilityCost = %v %d %v %v", mana, value, other, tap)
	}
	if counterName("P1P1") != "+1/+1" || counterName("M1M1") != "-1/-1" || counterName("LOYALTY") != "loyalty" || counterName("Shield") != "" {
		t.Errorf("counterName")
	}
	if keywordRule("Flying") != "flying" || keywordRule("Ward:2") != "Ward:2" || keywordRule("CARDNAME can't block.") != "{this} can't block." {
		t.Errorf("keywordRule")
	}
	if StepName(state.StepMain1) != "PRECOMBAT_MAIN" || StepName(state.StepEnd) != "END_TURN" || StepName(99) != "" {
		t.Errorf("StepName")
	}
	f := fixtureCard(t, "Name:Test Sage\nManaCost:1 U\nTypes:Creature Wizard\nPT:1/1\n"+
		"A:AB$ Draw | Cost$ 2 T | NumCards$ 1 | SpellDescription$ Draw a card.\n"+
		"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigGain | TriggerDescription$ When CARDNAME enters, you gain 1 life.\n"+
		"SVar:TrigGain:DB$ GainLife | LifeAmount$ 1\n"+
		"Oracle:{2}, {T}: Draw a card.\\nWhen Test Sage enters, you gain 1 life. (Reminder.)\n").Faces[0]
	fi := buildFaceInfo(f)
	var rules []string
	for _, a := range fi.abilities {
		rules = append(rules, a.rule)
	}
	want := []string{"", "{2}, {T}: Draw a card.", "When {this} enters, you gain 1 life."}
	if !slices.Equal(rules, want) {
		t.Errorf("ability rules = %q, want %q", rules, want)
	}
}
