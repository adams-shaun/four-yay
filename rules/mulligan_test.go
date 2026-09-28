package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// twoSeatConfig builds a two-seat, one-deck-each Config over mountainDeck,
// with Mulligans permitted mulligans per seat. The mulligan round only runs
// when Mulligans > 0 (Ruling R-8.4: the zero value skips the round entirely).
func twoSeatConfig(t *testing.T, deckSize, mulligans int) Config {
	t.Helper()
	return Config{
		Seed: 42, Mulligans: mulligans,
		Names: []string{"a", "b"},
		Decks: [][]*cards.Card{mountainDeck(t, deckSize), mountainDeck(t, deckSize)},
	}
}

func fourSeatConfig(t *testing.T, deckSize, mulligans int) Config {
	t.Helper()
	return Config{
		Seed: 42, Mulligans: mulligans,
		Names: []string{"a", "b", "c", "d"},
		Decks: [][]*cards.Card{
			mountainDeck(t, deckSize), mountainDeck(t, deckSize),
			mountainDeck(t, deckSize), mountainDeck(t, deckSize),
		},
	}
}

// playPregame drives a New-built engine through its pre-game round: Advance
// once (which issues the first keep/mulligan ask), then answer every pending
// decision with decide until the round hands to turn 1 (Turn >= 1) or the
// game ends. Returns the engine.
func playPregame(t *testing.T, e *Engine, decide func(d *decision.Decision) decision.Intent) *Engine {
	t.Helper()
	e.Advance()
	for e.G.Turn == 0 && e.Pending() != nil && !e.G.Over {
		d := e.Pending()
		if err := e.Submit(decide(d)); err != nil {
			t.Fatalf("pregame intent: %v", err)
		}
	}
	return e
}

// keepEverywhere is a decide policy that keeps any keep/mulligan ask (both
// seats, every round) -- no mulligan, so no bottoming ever runs.
func keepEverywhere(d *decision.Decision) decision.Intent {
	return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}
}

// TestMulliganWithKeepsGoesStraightToTurnOne: a round in which every seat
// keeps must reach turn 1 with every alive seat still holding a full 7-card
// hand, and -- because no one mulliganed -- must emit no Shuffle at all. The
// mulligan round changes nothing when nobody takes one (R-Martin zero == today).
func TestMulliganWithKeepsGoesStraightToTurnOne(t *testing.T) {
	t.Parallel()
	cfg := twoSeatConfig(t, 41, 1)
	e := New(cfg)
	playPregame(t, e, keepEverywhere)

	if e.G.Turn != 1 {
		t.Fatalf("round did not reach turn 1, turn %d", e.G.Turn)
	}
	for _, p := range e.G.AliveFrom(0) {
		if got := len(e.G.Zone(state.ZHand, p)); got != 7 {
			t.Errorf("seat %d hand %d, want 7 (an untouched keep)", p, got)
		}
	}
	// Genesis emits one Shuffle per seat (two seats -> two); the round must
	// add none, because nobody mulliganed.
	shuffles := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Shuffle {
			shuffles++
		}
	}
	if shuffles != 2 {
		t.Errorf("no one mulliganed, but the log holds %d Shuffles (want the 2 genesis deals only)", shuffles)
	}
}

// TestABotThatMulligansKeepsSevenAndBottomsOne drives a round where seat 0
// mulligans once -- CR 103.4: it shuffles back in and draws a FULL new seven,
// so at keep time it holds 7 -- then bottoms its lowest-indexed card; seat 1
// keeps untouched. After the round seat 0 holds 6 (the bottom removes one of
// its seven), its library gained exactly the bottomed card at its bottom, and
// the mulligan's re-shuffle is in the log (so the whole round is replayable).
func TestABotThatMulligansKeepsSevenAndBottomsOne(t *testing.T) {
	t.Parallel()
	cfg := twoSeatConfig(t, 41, 1)
	e := New(cfg)
	var (
		bottomed    state.ObjID
		sawMulligan bool
	)
	decide := func(d *decision.Decision) decision.Intent {
		// Bottoming phase: bottom the lowest-indexed card and record it.
		if d.Kind == decision.KMulligan && len(d.Options) > 0 && d.Options[0].Kind == "bottom" {
			bottomed = d.Options[0].Obj
			return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}
		}
		// Seat 0 mulligans exactly once; everyone else keeps.
		if d.Player == 0 && !sawMulligan {
			for _, o := range d.Options {
				if o.Kind == "mulligan" {
					sawMulligan = true
					return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}
				}
			}
		}
		return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}} // keep
	}
	playPregame(t, e, decide)

	if e.G.Turn != 1 {
		t.Fatalf("round did not reach turn 1, turn %d", e.G.Turn)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != 6 {
		t.Errorf("seat 0 hand %d, want 6 after one mulligan (full seven) and one bottom", got)
	}
	if got := len(e.G.Zone(state.ZHand, 1)); got != 7 {
		t.Errorf("seat 1 hand %d, want 7 (kept untouched)", got)
	}
	lib := e.G.Zone(state.ZLibrary, 0)
	if len(lib) == 0 || lib[len(lib)-1] != bottomed {
		t.Errorf("seat 0 library bottom %v, want the bottomed card %v", lib, bottomed)
	}
	found := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.Shuffle && ev.Player == 0 {
			found = true
		}
	}
	if !found {
		t.Errorf("no Shuffle event recorded for seat 0's mulligan re-shuffle")
	}
}

// TestMultiplayerSecondMulliganBottomsOne proves CR 103.5b exempts only the
// first mulligan's bottoming penalty in a game with three or more players.
func TestMultiplayerSecondMulliganBottomsOne(t *testing.T) {
	t.Parallel()
	e := New(fourSeatConfig(t, 60, 2))
	taken := 0
	var (
		bottomAsk      *decision.Decision
		freeKeepPrompt string
	)
	decide := func(d *decision.Decision) decision.Intent {
		if len(d.Options) > 0 && d.Options[0].Kind == "bottom" {
			copy := *d
			bottomAsk = &copy
			return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}
		}
		if d.Player == 0 && taken < 2 {
			if taken == 1 {
				freeKeepPrompt = d.Prompt
			}
			for _, o := range d.Options {
				if o.Kind == "mulligan" {
					taken++
					return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}
				}
			}
		}
		return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}
	}
	playPregame(t, e, decide)

	if want := "a plays first. Keep your hand (keep all seven cards) or take a mulligan?"; freeKeepPrompt != want {
		t.Errorf("first multiplayer mulligan keep prompt = %q, want %q", freeKeepPrompt, want)
	}
	if bottomAsk == nil {
		t.Fatal("seat 0 received no bottoming ask after its second multiplayer mulligan")
	}
	if bottomAsk.Player != 0 || bottomAsk.Min != 1 || bottomAsk.Max != 1 {
		t.Errorf("second multiplayer mulligan bottoming ask = player %d, %d..%d; want player 0, 1..1",
			bottomAsk.Player, bottomAsk.Min, bottomAsk.Max)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != 6 {
		t.Errorf("seat 0 hand %d, want 6 after two multiplayer mulligans and one bottom", got)
	}
}

// TestTwoPlayerFirstMulliganStillBottomsOne proves the multiplayer exemption
// does not leak into an ordinary two-player London mulligan round.
func TestTwoPlayerFirstMulliganStillBottomsOne(t *testing.T) {
	t.Parallel()
	e := New(twoSeatConfig(t, 41, 1))
	took := false
	var bottomAsk *decision.Decision
	decide := func(d *decision.Decision) decision.Intent {
		if len(d.Options) > 0 && d.Options[0].Kind == "bottom" {
			copy := *d
			bottomAsk = &copy
			return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}
		}
		if d.Player == 0 && !took {
			for _, o := range d.Options {
				if o.Kind == "mulligan" {
					took = true
					return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}
				}
			}
		}
		return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}
	}
	playPregame(t, e, decide)

	if bottomAsk == nil {
		t.Fatal("seat 0 received no bottoming ask after its first two-player mulligan")
	}
	if bottomAsk.Player != 0 || bottomAsk.Min != 1 || bottomAsk.Max != 1 {
		t.Errorf("first two-player mulligan bottoming ask = player %d, %d..%d; want player 0, 1..1",
			bottomAsk.Player, bottomAsk.Min, bottomAsk.Max)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != 6 {
		t.Errorf("seat 0 hand %d, want 6 after its first two-player mulligan and one bottom", got)
	}
}

// TestMulliganBottomIsDistinctIndices pins London bottoming's reuse of the
// distinct-index shape Validate already enforces for KTriggerOrder (Ruling
// U2): a Min == Max == taken decision over per-card "bottom" options must
// reject a duplicated index, because Validate's duplicate answer is not a
// valid permutation of taken hand indices.
func TestMulliganBottomIsDistinctIndices(t *testing.T) {
	t.Parallel()
	d := &decision.Decision{Seq: 1, Player: 0, Kind: decision.KMulligan, Min: 2, Max: 2,
		Options: []decision.Option{{Index: 0, Kind: "bottom"}, {Index: 1, Kind: "bottom"}, {Index: 2, Kind: "bottom"}}}
	if d.Validate(decision.Intent{Seq: 1, Player: 0, Choices: []int{0, 0}}) == nil {
		t.Fatal("duplicate bottom index accepted")
	}
	if d.Validate(decision.Intent{Seq: 1, Player: 0, Choices: []int{0, 1}}) != nil {
		t.Fatal("valid distinct bottom rejected")
	}
	if d.Validate(decision.Intent{Seq: 1, Player: 0, Choices: []int{0, 1, 2}}) == nil {
		t.Fatal("too many bottom choices accepted")
	}
}

// TestTwoMulligansPenaltyIsLinear proves the London penalty is ONE bottomed
// card per mulligan (7 - k after k mulligans), not a smaller redraw stacked
// on top of the bottoming (which would end at 7 - 2k) -- the whole bug this
// fix round corrects (Finding 1). Seat 0 takes both free mulligans
// (Mulligans: 2), keeps, and bottoms two: the round must end at 7 - 2 = 5.
// The double-penalizing implementation this fix replaces drew 7 - taken each
// time and would end at 7 - 2 - 2 = 3, so the 5 is the linearity proof.
func TestTwoMulligansPenaltyIsLinear(t *testing.T) {
	t.Parallel()
	cfg := twoSeatConfig(t, 60, 2)
	e := New(cfg)
	mulligans := 0
	decide := func(d *decision.Decision) decision.Intent {
		// Bottoming phase: bottom the d.Min lowest-indexed cards.
		if d.Kind == decision.KMulligan && len(d.Options) > 0 && d.Options[0].Kind == "bottom" {
			ch := make([]int, 0, d.Min)
			for j := 0; j < len(d.Options) && j < d.Min; j++ {
				ch = append(ch, d.Options[j].Index)
			}
			return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: ch}
		}
		// Seat 0 mulligans while a mulligan option remains (exactly the two
		// free ones), then keeps; everyone else keeps.
		if d.Player == 0 && mulligans < 2 {
			for _, o := range d.Options {
				if o.Kind == "mulligan" {
					mulligans++
					return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}
				}
			}
		}
		return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}} // keep
	}
	playPregame(t, e, decide)

	if e.G.Turn != 1 {
		t.Fatalf("round did not reach turn 1, turn %d", e.G.Turn)
	}
	if mulligans != 2 {
		t.Fatalf("seat 0 mulliganed %d times, want the two free mulligans", mulligans)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != 5 {
		t.Errorf("seat 0 hand %d, want 5 (7 - 2 mulligans) after two mulligans and two bottoms", got)
	}
	if got := len(e.G.Zone(state.ZHand, 1)); got != 7 {
		t.Errorf("seat 1 hand %d, want 7 (kept untouched)", got)
	}
}

// TestLastMulliganThenOnlyKeepOffered wraps the free-mulligan gate (Finding
// 2, fix round 1): with Mulligans: 1, seat 0's first ask offers keep AND
// mulligan; after it takes the last allowed mulligan, the very next ask for
// that seat must offer only "keep" -- one option, Kind == "keep", and no
// option with Kind == "mulligan" -- because the allowance is spent (R-M4:
// London offers no mulligan past the free count). It is driven through
// New/Pending/Submit so it proves a real game state reaches the gate, not
// that an if works; the round then still completes to turn 1.
func TestLastMulliganThenOnlyKeepOffered(t *testing.T) {
	t.Parallel()
	cfg := twoSeatConfig(t, 41, 1)
	e := New(cfg)
	e.Advance() // first keep/mulligan ask

	count := func(d *decision.Decision) map[string]int {
		kinds := map[string]int{}
		for _, o := range d.Options {
			kinds[o.Kind]++
		}
		return kinds
	}

	first := e.Pending()
	if first.Player != 0 {
		t.Fatalf("first ask is for seat %d, want seat 0", first.Player)
	}
	if k := count(first); k["keep"] != 1 || k["mulligan"] != 1 {
		t.Fatalf("first ask offers keep=%d mulligan=%d, want 1 and 1 while a free mulligan remains", k["keep"], k["mulligan"])
	}
	mullIndex := -1
	for _, o := range first.Options {
		if o.Kind == "mulligan" {
			mullIndex = o.Index
		}
	}
	if err := e.Submit(decision.Intent{Seq: first.Seq, Player: first.Player, Choices: []int{mullIndex}}); err != nil {
		t.Fatalf("taking the last allowed mulligan: %v", err)
	}

	next := e.Pending()
	if next == nil {
		t.Fatal("no ask issued after the last allowed mulligan")
	}
	// CR 103.5 completes the current declaration pass before returning to
	// the mulliganing seat. Seat 1 therefore gets its first declaration now;
	// only the next pass reaches seat 0's forced keep.
	if next.Player != 1 {
		t.Fatalf("post-mulligan ask is for seat %d, want the other seat 1 in the same pass", next.Player)
	}
	if err := e.Submit(decision.Intent{Seq: next.Seq, Player: next.Player, Choices: []int{0}}); err != nil {
		t.Fatalf("seat 1 keep: %v", err)
	}
	next = e.Pending()
	if next == nil || next.Player != 0 {
		t.Fatalf("next-pass ask = %+v, want the mulliganing seat 0", next)
	}
	if len(next.Options) != 1 {
		t.Errorf("post-mulligan ask has %d options, want 1 (only keep)", len(next.Options))
	}
	if k := count(next); k["keep"] != 1 || k["mulligan"] != 0 {
		t.Errorf("post-mulligan ask offers keep=%d mulligan=%d, want only one keep", k["keep"], k["mulligan"])
	}

	// The gate reached from a real game state, its answer still plays: the
	// seat keeps, then seat 0 bottoms one, and the round completes.
	playPregame(t, e, keepEverywhere)
	if e.G.Turn != 1 {
		t.Fatalf("round did not reach turn 1 after the last allowable mulligan, turn %d", e.G.Turn)
	}
}

// TestKeptPregameRoundReplaysExactly proves the mulligan round is event-
// driven: a round that mixed a mulligan (seat 0, once) and a keep (seat 1)
// replayed from its own recorded (Config, Log) -- the same value replay is
// handed -- reaches the same chain head as the live run. This is Ruling
// R-8.4's determinism invariant: Config.Mulligans must travel with the
// Config replay reads.
func TestKeptPregameRoundReplaysExactly(t *testing.T) {
	t.Parallel()
	cfg := twoSeatConfig(t, 42, 1)
	e := New(cfg)
	var sawMulligan bool
	decide := func(d *decision.Decision) decision.Intent {
		if d.Kind == decision.KMulligan && len(d.Options) > 0 && d.Options[0].Kind == "bottom" {
			ch := make([]int, 0, d.Min)
			for j := 0; j < len(d.Options) && j < d.Min; j++ {
				ch = append(ch, d.Options[j].Index)
			}
			return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: ch}
		}
		if d.Player == 0 && !sawMulligan {
			for _, o := range d.Options {
				if o.Kind == "mulligan" {
					sawMulligan = true
					return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}
				}
			}
		}
		return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}
	}
	playPregame(t, e, decide)

	re, err := replayFor(cfg, e.L)
	if err != nil {
		t.Fatal(err)
	}
	if re.L.Head() != e.L.Head() {
		t.Fatalf("replay head %s != live %s", re.L.Head(), e.L.Head())
	}
}

// capturePrompts drives a pre-game round in which seat 0 mulligans `take`
// times before keeping (so it will bottom `take`), seat 1 keeps untouched, and
// returns the keep/mulligan prompt the round first showed seat 0 and the
// bottoming prompt it shows the seat that must bottom -- the two human-visible
// sentences of the mulligan round.
func capturePrompts(t *testing.T, mulligans, take int) (keepPrompt, bottomPrompt string) {
	t.Helper()
	cfg := twoSeatConfig(t, 50, mulligans)
	e := New(cfg)
	took := 0
	decide := func(d *decision.Decision) decision.Intent {
		if d.Kind == decision.KMulligan && len(d.Options) > 0 && d.Options[0].Kind == "bottom" {
			if bottomPrompt == "" {
				bottomPrompt = d.Prompt
			}
			ch := make([]int, 0, d.Min)
			for j := 0; j < len(d.Options) && j < d.Min; j++ {
				ch = append(ch, d.Options[j].Index)
			}
			return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: ch}
		}
		if d.Player == 0 && took < take {
			for _, o := range d.Options {
				if o.Kind == "mulligan" {
					if keepPrompt == "" {
						keepPrompt = d.Prompt
					}
					took++
					return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}
				}
			}
		}
		if keepPrompt == "" {
			keepPrompt = d.Prompt
		}
		return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}} // keep
	}
	playPregame(t, e, decide)
	return keepPrompt, bottomPrompt
}

// TestBottomingPromptIsRealEnglish pins finding bh's singular and plural
// forms: the bottoming ask's Prompt is the last real sentence a player reads
// in the mulligan round, and it must be written for a human -- "Put 1 card on
// the bottom of your library" for a one-card bottom, "Put 2 cards on the
// bottom of your library" for two -- never the engine-speak "bottoms 2
// card(s)" it replaced.
func TestBottomingPromptIsRealEnglish(t *testing.T) {
	t.Parallel()
	if got, want := bottomingPrompt(0), "Keep all seven cards"; got != want {
		t.Errorf("zero-card bottom prompt = %q, want %q", got, want)
	}
	_, singular := capturePrompts(t, 1, 1)
	if want := "Put 1 card on the bottom of your library"; singular != want {
		t.Errorf("one-card bottom prompt = %q, want %q", singular, want)
	}
	_, plural := capturePrompts(t, 2, 2)
	if want := "Put 2 cards on the bottom of your library"; plural != want {
		t.Errorf("two-card bottom prompt = %q, want %q", plural, want)
	}
}

// TestKeepMulliganPromptHasNoEngineSpeak pins finding bh's sweep through the
// round's other prompt: the keep/mulligan ask, the FIRST sentence a player
// reads in a game, had the same tell as the bottoming one -- "keeps 7 and
// bottoms 1, or mulligans" -- and must no longer use "bottoms" as a verb or
// the "(s)" shorthand.
func TestKeepMulliganPromptHasNoEngineSpeak(t *testing.T) {
	t.Parallel()
	keep, _ := capturePrompts(t, 2, 1)
	for _, bad := range []string{"bottoms", "card(s)", "London mulligan:", "mulligans"} {
		if strings.Contains(keep, bad) {
			t.Errorf("keep/mulligan prompt %q still carries engine-speak %q", keep, bad)
		}
	}
	if !strings.Contains(keep, "mulligan") {
		t.Errorf("keep/mulligan prompt %q no longer names the mulligan choice", keep)
	}
}

// TestPregameSkippedWhenMulligansZero pins R-8.4's inert default: a Config
// that never sets Mulligans (the zero value) skips the round entirely and
// behaves exactly as before -- the engine begins the first turn immediately.
func TestPregameSkippedWhenMulligansZero(t *testing.T) {
	t.Parallel()
	cfg := twoSeatConfig(t, 40, 0)
	e := New(cfg)
	e.Advance()
	if e.G.Turn != 1 {
		t.Fatalf("Mulligans 0 must skip the round and begin turn 1, turn %d", e.G.Turn)
	}
	if e.pregame {
		t.Fatal("e.pregame set with Mulligans 0")
	}
}
