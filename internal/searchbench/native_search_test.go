package searchbench

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/azmcts"
	"github.com/adams-shaun/gorge/internal/azmcts/clairvoyant"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// armPosition is a bot-vs-bot game (60-card repo decks) stopped at a seat-0
// main-phase priority at turn >= minTurn, with at least one planned cast
// and three cards in the opponent's hand, plus eight belief worlds
// re-dealt from it over the deck lists.
type armPosition struct {
	real   *rules.Engine
	worlds []*rules.Engine
	decks  [][]*cards.Card
}

func newArmPosition(t testing.TB, seed uint64, minTurn int32) armPosition {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	a, err := testutil.LoadRepoDeck(reg, "mono-red-prowess")
	if err != nil {
		t.Fatal(err)
	}
	b, err := testutil.LoadRepoDeck(reg, "mono-blue-tempo")
	if err != nil {
		t.Fatal(err)
	}
	cfg := rules.Config{Seed: seed, Names: []string{"mono-red-prowess", "mono-blue-tempo"}, Decks: [][]*cards.Card{a, b}, Tokens: reg.Tokens}
	e := rules.New(cfg)
	e.Advance()
	rngs := searchprobe.BotRandoms(seed, 2)
	board := botpolicy.NewBoard(2)
	for steps := 0; steps < 20000 && !e.G.Over; steps++ {
		d := e.Pending()
		if d.Player == 0 && d.Kind == decision.KPriority && e.G.Turn >= minTurn && e.G.Step.IsMain() && e.G.Active == 0 &&
			len(e.G.Zone(state.ZHand, 1)) >= 3 && len(e.G.Stack) == 0 && len(e.EnsurePaymentActions()) > 0 && castOrPass(e) {
			worlds, err := RedealWorlds(e, cfg.Decks, WorldCount, seed^0x77)
			if err != nil {
				t.Fatal(err)
			}
			return armPosition{real: e, worlds: worlds, decks: cfg.Decks}
		}
		in := botpolicy.Decide(botpolicy.BoardFromGameInto(e.G, e, d.Player, &board), d, rngs[d.Player])
		if err := e.Submit(in); err != nil {
			t.Fatalf("step %d: %v", steps, err)
		}
	}
	t.Fatalf("seed %d: no seat-0 main-phase priority with a planned cast at turn >= %d", seed, minTurn)
	return armPosition{}
}

// castOrPass reports whether the auto-pay bot's answer at e is a cast, an
// ability or a pass -- not a land play: the benchmark root comes after the
// land drop (upstream's preLand).
func castOrPass(e *rules.Engine) bool {
	in := botAnswer(e, 1)
	if in.Payment != nil {
		return true
	}
	d := e.Pending()
	if len(in.Choices) != 1 {
		return false
	}
	switch d.Options[in.Choices[0]].Kind {
	case "cast", "ability", "pass":
		return true
	}
	return false
}

func (p armPosition) input(arm SearchArm, sims int, seed uint64) ArmInput {
	return ArmInput{Arm: arm, Options: BenchOptions(sims), Seed: seed, Real: p.real, Worlds: p.worlds}
}

const armSeed = 30000011

// Runs first in the package (source order): the gate is process-wide, and
// every later test opens it.
func TestClairvoyantArmNeedsTheGate(t *testing.T) {
	p := newArmPosition(t, armSeed, 4)
	_, err := RunArm(context.Background(), p.input(ArmClairvoyant, 4, 1))
	if !errors.Is(err, clairvoyant.ErrClairvoyantRefused) {
		t.Fatalf("clairvoyant arm without AllowClairvoyant: %v", err)
	}
}

// The same input and seed give an identical ArmResult for every arm, and
// every search arm searches this position.
func TestArmsAreDeterministic(t *testing.T) {
	clairvoyant.AllowClairvoyant()
	p := newArmPosition(t, armSeed, 4)
	for _, arm := range Arms {
		a, err := RunArm(context.Background(), p.input(arm, 24, 5))
		if err != nil {
			t.Fatalf("%s: %v", arm, err)
		}
		b, err := RunArm(context.Background(), p.input(arm, 24, 5))
		if err != nil {
			t.Fatalf("%s: %v", arm, err)
		}
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("%s: not deterministic:\n%+v\n%+v", arm, a, b)
		}
		if arm == ArmNoSearch {
			if a.Searched || a.Table != nil || !reflect.DeepEqual(a.Intent, a.Bot) {
				t.Fatalf("no-search: %+v", a)
			}
			continue
		}
		st := a.Stats
		if !a.Searched || st.Completed == 0 || st.Simulations != 24 || st.RootTruncated != 0 {
			t.Fatalf("%s: searched %v, stats %+v", arm, a.Searched, st)
		}
		if err := p.real.Pending().Validate(a.Intent); err != nil {
			t.Fatalf("%s: the answer is not legal on the real engine: %v", arm, err)
		}
		visits := 0
		for _, row := range a.Table {
			visits += row.Visits
		}
		if visits != st.Completed {
			t.Fatalf("%s: root visits %d, completed %d", arm, visits, st.Completed)
		}
		t.Logf("%s: %s (%d/%d completed, %.1f plies, %.2f edges, %.2f turns per simulation; env steps %d)", arm, a.Label, st.Completed, st.Simulations, st.MeanLeafPlies(), st.MeanLeafEdges(), st.MeanLeafTurns(), st.EnvSteps)
		for _, row := range a.Table {
			t.Logf("  %-40s visits %3d avail %3d Q %.3f prior %.3f", row.Label, row.Visits, row.Avail, row.Q, row.Prior)
		}
	}
	// A different seed is a different search, not the same answer by accident.
	a, _ := RunArm(context.Background(), p.input(ArmISMCTS, 24, 5))
	b, _ := RunArm(context.Background(), p.input(ArmISMCTS, 24, 6))
	if reflect.DeepEqual(a.WorldPicks, b.WorldPicks) {
		t.Fatalf("is-mcts picked worlds identically under two seeds: %v", a.WorldPicks)
	}
}

// PIMC-4's merge is the union of the trees' root keys: visits and
// availability summed, Q visit-weighted, the prior averaged over the trees
// that hold the key.
func TestMergeRootTablesIsAKeyUnion(t *testing.T) {
	tables := [][]azmcts.RootRow{
		{{Key: "pass", Label: "Pass", Visits: 3, Q: 0.4, Avail: 5, Prior: 0.5}, {Key: "bolt", Label: "Bolt", Visits: 2, Q: 0.7, Avail: 5, Prior: 0.5}},
		{{Key: "bolt", Label: "Bolt", Visits: 4, Q: 0.4, Avail: 5, Prior: 1.0 / 3}, {Key: "pass", Label: "Pass", Visits: 1, Q: 0.2, Avail: 5, Prior: 1.0 / 3}, {Key: "shock", Label: "Shock", Visits: 0, Avail: 5, Prior: 1.0 / 3}},
		{},
		{{Key: "shock", Label: "Shock", Visits: 5, Q: 0.9, Avail: 5, Prior: 1}},
	}
	got := MergeRootTables(tables)
	want := []azmcts.RootRow{
		{Key: "pass", Label: "Pass", Visits: 4, Q: (3*0.4 + 0.2) / 4, Avail: 10, Prior: (0.5 + 1.0/3) / 2},
		{Key: "bolt", Label: "Bolt", Visits: 6, Q: (2*0.7 + 4*0.4) / 6, Avail: 10, Prior: (0.5 + 1.0/3) / 2},
		{Key: "shock", Label: "Shock", Visits: 5, Q: 0.9, Avail: 10, Prior: (1.0/3 + 1) / 2},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.Key != w.Key || g.Label != w.Label || g.Visits != w.Visits || g.Avail != w.Avail || !near(g.Q, w.Q) || !near(g.Prior, w.Prior) {
			t.Fatalf("row %d: %+v, want %+v", i, g, w)
		}
	}
}

func near(a, b float64) bool { d := a - b; return d < 1e-9 && d > -1e-9 }

// PIMC-4 on a real position: the budget splits 3/3/2/2, the merged table
// is the per-world tables' union, and the choice is the most visited key.
func TestPIMC4SplitsAndMerges(t *testing.T) {
	p := newArmPosition(t, armSeed, 4)
	r, err := RunArm(context.Background(), p.input(ArmPIMC4, 10, 9))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.PerWorld) != PIMCWorlds {
		t.Fatalf("%d per-world tables", len(r.PerWorld))
	}
	for i, want := range []int{3, 3, 2, 2} {
		n := 0
		for _, row := range r.PerWorld[i] {
			n += row.Visits
		}
		if n != want {
			t.Fatalf("world %d: %d visits, want %d", i, n, want)
		}
	}
	if !reflect.DeepEqual(r.Table, MergeRootTables(r.PerWorld)) {
		t.Fatal("merged table is not the per-world union")
	}
	for _, row := range r.Table {
		if row.Visits > r.Table[r.Choice].Visits {
			t.Fatalf("choice %d is not the most visited", r.Choice)
		}
	}
	if r.Stats.Simulations != 10 {
		t.Fatalf("%d simulations", r.Stats.Simulations)
	}
}

// IS-MCTS touches every world, re-deals every simulation, and its re-deals
// vary the opponent's hand.
func TestISMCTSTouchesAllWorldsAndRedeals(t *testing.T) {
	p := newArmPosition(t, armSeed, 4)
	r, err := RunArm(context.Background(), p.input(ArmISMCTS, 200, 3))
	if err != nil {
		t.Fatal(err)
	}
	for i, n := range r.WorldPicks {
		if n == 0 {
			t.Fatalf("world %d never picked: %v", i, r.WorldPicks)
		}
	}
	if r.Redeals != r.Stats.Simulations || r.RedealFailures != 0 || r.Stats.NoWorld != 0 {
		t.Fatalf("redeals %d failures %d over %d simulations (no-world %d)", r.Redeals, r.RedealFailures, r.Stats.Simulations, r.Stats.NoWorld)
	}
	src, err := newISSource(p.worlds, 0, 17)
	if err != nil {
		t.Fatal(err)
	}
	hands := map[string]bool{}
	for sim := 0; sim < 40; sim++ {
		w, err := src.World(sim)
		if err != nil {
			t.Fatal(err)
		}
		hands[handKey(w.Engine, 1)] = true
		if got, want := len(w.Engine.G.Zone(state.ZHand, 1)), len(p.real.G.Zone(state.ZHand, 1)); got != want {
			t.Fatalf("re-dealt opponent hand has %d cards, want %d", got, want)
		}
		if handKey(w.Engine, 0) != handKey(p.real, 0) {
			t.Fatal("the searching seat's own hand was re-dealt")
		}
	}
	if len(hands) < 10 {
		t.Fatalf("40 re-deals dealt only %d distinct opponent hands", len(hands))
	}
	t.Logf("picks %v; 40 re-deals, %d distinct opponent hands", r.WorldPicks, len(hands))
}

func handKey(e *rules.Engine, p state.PlayerID) string {
	var names []string
	for _, id := range e.G.Zone(state.ZHand, p) {
		names = append(names, e.G.Obj(id).Card.Faces[0].Name)
	}
	sort.Strings(names)
	return strings.Join(names, "\x00")
}

// hideDifferently returns a clone of e whose opponent holds different
// hidden cards -- every opponent hand card swapped, secretly, for a library
// card of another name -- and whose libraries are both reversed: the same
// observation, different hidden cards.
func hideDifferently(t *testing.T, e *rules.Engine) *rules.Engine {
	t.Helper()
	alt := e.Clone()
	hand, lib := alt.G.Zone(state.ZHand, 1), alt.G.Zone(state.ZLibrary, 1)
	used := map[state.ObjID]bool{}
	swapped := 0
	for _, h := range append([]state.ObjID(nil), hand...) {
		hn := alt.G.Obj(h).Card.Faces[0].Name
		for _, l := range lib {
			if used[l] || alt.G.Obj(l).Card.Faces[0].Name == hn {
				continue
			}
			used[l] = true
			events.Emit(alt.G, alt.L, events.Event{Kind: events.MoveZone, Player: 1, Obj: h, From: state.ZHand, To: state.ZLibrary, Secret: true})
			events.Emit(alt.G, alt.L, events.Event{Kind: events.MoveZone, Player: 1, Obj: l, From: state.ZLibrary, To: state.ZHand, Secret: true})
			swapped++
			break
		}
	}
	if swapped == 0 {
		t.Fatal("fixture: no opponent hand card could be swapped")
	}
	for pl := state.PlayerID(0); pl < 2; pl++ {
		cur := alt.G.Zone(state.ZLibrary, pl)
		rev := make([]state.ObjID, len(cur))
		for i, id := range cur {
			rev[len(cur)-1-i] = id
		}
		events.Emit(alt.G, alt.L, events.Event{Kind: events.LibraryOrder, Player: pl, IDs: rev, Secret: true})
	}
	if handKey(alt, 1) == handKey(e, 1) {
		t.Fatal("fixture: the opponent's hand did not change")
	}
	return alt
}

// The fairness proof (the TestRedealWorldsIgnoreTheRealHiddenCards
// pattern): IS-MCTS's answer and root table are a function of the
// observation, the worlds' unseen-card POOLS and the seed alone. Two runs
// whose real engine holds different hidden cards, and whose every world
// holds its pool in a different arrangement (other cards in the
// opponent's hand, both libraries reversed), give identical results.
func TestISMCTSIgnoresTheHiddenCards(t *testing.T) {
	p := newArmPosition(t, armSeed, 4)
	altReal := hideDifferently(t, p.real)
	altWorlds := make([]*rules.Engine, len(p.worlds))
	for i, w := range p.worlds {
		altWorlds[i] = hideDifferently(t, w)
	}
	base := p.input(ArmISMCTS, 64, 21)
	a, err := RunArm(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	for _, alt := range []struct {
		name string
		in   ArmInput
	}{
		{"real hidden cards", ArmInput{Arm: ArmISMCTS, Options: base.Options, Seed: base.Seed, Real: altReal, Worlds: p.worlds}},
		{"worlds' arrangement", ArmInput{Arm: ArmISMCTS, Options: base.Options, Seed: base.Seed, Real: p.real, Worlds: altWorlds}},
	} {
		b, err := RunArm(context.Background(), alt.in)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(a.Table, b.Table) || a.Key != b.Key || !reflect.DeepEqual(a.WorldPicks, b.WorldPicks) {
			t.Fatalf("%s changed the search:\n%+v\n%+v", alt.name, a.Table, b.Table)
		}
	}
	// PIMC-1 does read its world's arrangement (a determinization): the
	// control that the fixture's hidden change is visible to a search.
	c, err := RunArm(context.Background(), p.input(ArmPIMC1, 64, 21))
	if err != nil {
		t.Fatal(err)
	}
	d, err := RunArm(context.Background(), ArmInput{Arm: ArmPIMC1, Options: base.Options, Seed: base.Seed, Real: p.real, Worlds: altWorlds})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("pimc-1 root table differs under the rearranged world: %v", !reflect.DeepEqual(c.Table, d.Table))
}

// Worlds that disagree with the observation are refused.
func TestRunArmRefusesInconsistentWorlds(t *testing.T) {
	p := newArmPosition(t, armSeed, 4)
	other := newArmPosition(t, armSeed+1, 4)
	in := p.input(ArmISMCTS, 4, 1)
	in.Worlds = append([]*rules.Engine{p.worlds[0]}, other.worlds[1:]...)
	if _, err := RunArm(context.Background(), in); err == nil {
		t.Fatal("worlds from another game were accepted")
	}
}
