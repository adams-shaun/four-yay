package searchseat_test

// The honest root (BP-04, spec §5.1/§5.3): one redealer deal of the live
// position, taken from the seat's own feed, which is the only engine a hosted
// bot ever touches. These tests pin the three claims the host depends on --
// the root observes exactly what the seat last saw, the root's hidden cards
// are independent of the real ones, and a nested redeal from a redealt root
// prepares -- and that a root seeds deterministically.

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/azmcts"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	ss "github.com/adams-shaun/gorge/internal/searchseat"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// rootPosition is a real bot-vs-bot game stopped at a seat-0 decision with
// seat 0's observation feed maintained exactly as internal/bench does (a
// capture at every decision of every player, seat 0's answers recorded back).
// The feed is live and holds every frame up to and including this decision.
type rootPosition struct {
	e     *rules.Engine
	d     *decision.Decision
	feed  *ss.Feed
	setup searchprobe.PublicGame
	cfg   rules.Config
}

func newRootPosition(t *testing.T, cfg rules.Config) rootPosition {
	t.Helper()
	e := rules.New(cfg)
	e.Advance()
	feed := ss.NewFeed(0)
	rngs := searchprobe.BotRandoms(cfg.Seed, 2)
	board := botpolicy.NewBoard(2)
	setup := searchprobe.PublicGame{Names: cfg.Names, Decks: cfg.Decks, Tokens: cfg.Tokens, StartingLife: cfg.StartingLife}
	for steps := 0; steps < 20000 && !e.G.Over; steps++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no pending decision at step %d", steps)
		}
		if _, ok := feed.Observe(e); !ok {
			t.Fatalf("feed stopped: %s", feed.StopReason())
		}
		in := botpolicy.Decide(botpolicy.BoardFromGameInto(e.G, e, d.Player, &board), d, rngs[d.Player])
		if d.Player == 0 {
			// Stop at a boundary where the redealer has something to do: a
			// real opponent hand and both libraries.
			if e.G.Turn >= 4 && len(e.G.Zone(state.ZHand, 1)) >= 3 && len(e.G.Zone(state.ZLibrary, 0)) >= 5 {
				return rootPosition{e: e, d: d, feed: feed, setup: setup, cfg: cfg}
			}
			if err := feed.RecordAnswer(d, in); err != nil {
				t.Fatal(err)
			}
		}
		if err := e.Submit(in); err != nil {
			t.Fatalf("step %d submit: %v", steps, err)
		}
	}
	t.Fatalf("seed %d: no suitable seat-0 decision", cfg.Seed)
	return rootPosition{}
}

func rootTestConfig(t *testing.T, a, b string) rules.Config {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	da, err := testutil.LoadRepoDeck(reg, a)
	if err != nil {
		t.Fatal(err)
	}
	db, err := testutil.LoadRepoDeck(reg, b)
	if err != nil {
		t.Fatal(err)
	}
	return rules.Config{Seed: 9001, Names: []string{a, b}, Decks: [][]*cards.Card{da, db}, Tokens: reg.Tokens}
}

func rootNames(t *testing.T, e *rules.Engine, ids []state.ObjID) []string {
	t.Helper()
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		o := e.G.Obj(id)
		if o == nil || o.Card == nil || len(o.Card.Faces) == 0 {
			t.Fatalf("object %d has no name", id)
		}
		out = append(out, o.Card.Faces[0].Name)
	}
	return out
}

// TestHonestRootKeepsTheObservation: the root engine, captured with a clone of
// the seat's own collector, projects to exactly the feed's last frame -- the
// board and the decision the seat was asked. A root that moved the decision
// would make every search answer a different question than the one asked.
func TestHonestRootKeepsTheObservation(t *testing.T) {
	p := newRootPosition(t, rootTestConfig(t, "mono-red-prowess", "mono-blue-tempo"))
	last := p.feed.LastFrame()
	if last.Decision == nil {
		t.Fatal("fixture: the feed's last frame carries no decision")
	}
	root, reason := ss.HonestRoot(p.setup, p.feed, p.e, [2]uint64{7, 11})
	if reason != "" {
		t.Fatalf("HonestRoot refused: %s", reason)
	}
	if root == nil {
		t.Fatal("HonestRoot returned a nil engine with no reason")
	}
	if root == p.e {
		t.Fatal("HonestRoot returned the live engine itself")
	}
	got, err := p.feed.Collector().Clone().Capture(root, nil)
	if err != nil {
		t.Fatalf("capture the root: %v", err)
	}
	if string(got.Board) != string(last.Board) {
		t.Fatalf("the root's board differs from the feed's last frame")
	}
	if !reflect.DeepEqual(got.Decision, last.Decision) {
		t.Fatalf("the root's decision differs from the feed's last frame")
	}
	// Precondition: the root really did redeal something (an untouched clone
	// would trivially satisfy the two comparisons above).
	if sameHiddenWorld(t, root, p.e) {
		t.Fatal("the root kept every real hidden card; the observation check is vacuous")
	}
}

// sameHiddenWorld reports whether two engines hold the same card in every
// hand and library, position by position.
func sameHiddenWorld(t *testing.T, a, b *rules.Engine) bool {
	t.Helper()
	for pl := state.PlayerID(0); int(pl) < len(a.G.Players); pl++ {
		for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
			if !reflect.DeepEqual(rootNames(t, a, a.G.Zone(z, pl)), rootNames(t, b, b.G.Zone(z, pl))) {
				return false
			}
		}
	}
	return true
}

// TestHonestRootIgnoresTheRealHiddenCards is the leak test at the root: two
// live positions that differ only in the opponent's never-revealed hidden
// cards and both libraries' order -- the swap fixture of
// TestRedealWorldsIgnoreTheRealHiddenCards -- give name-identical roots for
// the same seed and the same feed. What a root deals is a function of the
// seat's observation and the seed alone.
func TestHonestRootIgnoresTheRealHiddenCards(t *testing.T) {
	p := newRootPosition(t, rootTestConfig(t, "mono-red-prowess", "mono-blue-tempo"))
	alt := p.e.Clone()
	// Swap every unpinned opponent hand card for a same-named-different library
	// card, secretly, then reverse both libraries -- exactly the azmcts
	// fixture, so the two engines differ ONLY in hidden facts.
	known, err := p.feed.Known()
	if err != nil {
		t.Fatal(err)
	}
	pinned := make(map[string]bool)
	for _, kh := range known.Hands {
		if kh.Player == 1 {
			for _, c := range kh.Cards {
				pinned[c.Name] = true
			}
		}
	}
	hand, lib := alt.G.Zone(state.ZHand, 1), alt.G.Zone(state.ZLibrary, 1)
	swapped := 0
	used := make(map[state.ObjID]bool)
	for _, h := range append([]state.ObjID(nil), hand...) {
		hn := alt.G.Obj(h).Card.Faces[0].Name
		if pinned[hn] {
			continue
		}
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
	for pl := state.PlayerID(0); int(pl) < len(alt.G.Players); pl++ {
		cur := alt.G.Zone(state.ZLibrary, pl)
		rev := make([]state.ObjID, len(cur))
		for i, id := range cur {
			rev[len(cur)-1-i] = id
		}
		events.Emit(alt.G, alt.L, events.Event{Kind: events.LibraryOrder, Player: pl, IDs: rev, Secret: true})
	}
	if sameHiddenWorld(t, alt, p.e) {
		t.Fatal("fixture: the two positions hold the same hidden cards")
	}
	seed := [2]uint64{123, 456}
	a, reason := ss.HonestRoot(p.setup, p.feed, p.e, seed)
	if reason != "" {
		t.Fatalf("HonestRoot(real) refused: %s", reason)
	}
	b, reason := ss.HonestRoot(p.setup, p.feed, alt, seed)
	if reason != "" {
		t.Fatalf("HonestRoot(alt) refused: %s", reason)
	}
	if !sameHiddenWorld(t, a, b) {
		for pl := state.PlayerID(0); int(pl) < len(a.G.Players); pl++ {
			for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
				t.Logf("player %d zone %v: real %v alt %v", pl, z, rootNames(t, a, a.G.Zone(z, pl)), rootNames(t, b, b.G.Zone(z, pl)))
			}
		}
		t.Fatal("two roots for the same seed/feed depend on the real hidden cards")
	}
}

// TestHonestRootIsDeterministic: the root is a pure function of the feed and
// the seed, so two calls with the same seed deal the same hidden cards.
func TestHonestRootIsDeterministic(t *testing.T) {
	p := newRootPosition(t, rootTestConfig(t, "mono-red-prowess", "mono-blue-tempo"))
	seed := [2]uint64{99, 1}
	first, reason := ss.HonestRoot(p.setup, p.feed, p.e, seed)
	if reason != "" {
		t.Fatalf("first root: %s", reason)
	}
	second, reason := ss.HonestRoot(p.setup, p.feed, p.e, seed)
	if reason != "" {
		t.Fatalf("second root: %s", reason)
	}
	if !sameHiddenWorld(t, first, second) {
		t.Fatal("two roots with the same seed differ")
	}
}

// TestRedealFromAnHonestRootPrepares: a nested honest redeal whose base is a
// redealt root prepares rather than refusing. This is the INFERRED claim of
// spec §5.3 that BP-05 depends on; a refusal here would mean azmcts and
// sbsearch cannot run a hosted search from an honest root at all.
func TestRedealFromAnHonestRootPrepares(t *testing.T) {
	p := newRootPosition(t, rootTestConfig(t, "mono-red-prowess", "mono-blue-tempo"))
	root, reason := ss.HonestRoot(p.setup, p.feed, p.e, [2]uint64{3, 4})
	if reason != "" {
		t.Fatalf("HonestRoot refused: %s", reason)
	}
	known, err := p.feed.Known()
	if err != nil {
		t.Fatal(err)
	}
	src, err := azmcts.NewRedeal(azmcts.RedealInput{
		Setup:   p.setup,
		History: p.feed.History(),
		Known:   known,
		Base:    searchprobe.RedealBase{Engine: root, Observer: p.feed.Collector()},
	}, p.feed.Collector().Clone(), 5, 0)
	if err != nil {
		t.Fatalf("NewRedeal: %v", err)
	}
	if refused := src.Refused(); refused != "" {
		t.Fatalf("a redeal from an honest root refused: %s", refused)
	}
	if _, err := src.World(0); err != nil {
		t.Fatalf("a world from an honest root failed: %v", err)
	}
}

// TestHonestRootRefusesWithoutAFeed: no live feed is a named refusal, never a
// nil engine with an empty reason and never a clairvoyant fallback.
func TestHonestRootRefusesWithoutAFeed(t *testing.T) {
	if e, reason := ss.HonestRoot(searchprobe.PublicGame{}, nil, nil, [2]uint64{}); e != nil || reason == "" {
		t.Fatalf("nil feed: got (%v, %q), want (nil, non-empty)", e, reason)
	}
	dead := ss.NewFeed(0)
	// A feed that never observed has no frames: still a refusal.
	if e, reason := ss.HonestRoot(searchprobe.PublicGame{}, dead, nil, [2]uint64{}); e != nil || reason == "" {
		t.Fatalf("frame-less feed: got (%v, %q), want (nil, non-empty)", e, reason)
	}
}
