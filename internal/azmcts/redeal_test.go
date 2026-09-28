package azmcts

import (
	"reflect"
	"sort"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/internal/searchseat"
	"github.com/adams-shaun/gorge/internal/spellbench"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"

	gbench "github.com/adams-shaun/gorge/internal/bench"
)

// fedPosition is a real bot-vs-bot game stopped at a searchable seat-0
// decision, with seat 0's observation feed maintained exactly as
// internal/bench.PlayGame maintains it (a capture at every decision of every
// player, seat 0's answers recorded back).
type fedPosition struct {
	e     *rules.Engine
	d     *decision.Decision
	bot   decision.Intent
	feed  *searchseat.Feed
	setup searchprobe.PublicGame
}

// feedPosition plays until seat 0 faces a searchable decision at turn >=
// minTurn while seat 1 holds at least minOppHand cards. check, when non-nil,
// runs at every seat-0 decision on the way (after the capture).
func feedPosition(t *testing.T, cfg rules.Config, minTurn int32, minOppHand int, check func(*searchseat.Feed)) fedPosition {
	t.Helper()
	e := rules.New(cfg)
	e.Advance()
	feed := searchseat.NewFeed(0)
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
			if check != nil {
				check(feed)
			}
			if e.G.Turn >= minTurn && len(e.G.Zone(state.ZHand, 1)) >= minOppHand {
				if _, _, ok := enumerate(searchprobe.NewCollector(0), e, d, in, AllKinds(), DefaultOptions().Limit); ok {
					return fedPosition{e: e, d: d, bot: in, feed: feed, setup: setup}
				}
			}
			if err := feed.RecordAnswer(d, in); err != nil {
				t.Fatal(err)
			}
		}
		if err := e.Submit(in); err != nil {
			t.Fatalf("step %d submit: %v", steps, err)
		}
	}
	t.Fatalf("seed %d: no searchable seat-0 decision at turn >= %d with %d opponent cards in hand", cfg.Seed, minTurn, minOppHand)
	return fedPosition{}
}

func (p fedPosition) input(t *testing.T, base *rules.Engine) RedealInput {
	t.Helper()
	h := p.feed.History()
	known, err := searchprobe.ProjectKnownCards(h)
	if err != nil {
		t.Fatal(err)
	}
	return RedealInput{Setup: p.setup, History: h, Known: known, Base: searchprobe.RedealBase{Engine: base, Observer: p.feed.Collector()}}
}

func names(e *rules.Engine, ids []state.ObjID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, e.G.Obj(id).Card.Faces[0].Name)
	}
	return out
}

func sortedNames(e *rules.Engine, ids ...[]state.ObjID) []string {
	var out []string
	for _, list := range ids {
		out = append(out, names(e, list)...)
	}
	sort.Strings(out)
	return out
}

// derivablePool is the opponent's deck list minus every one of its cards
// the seat sees outside the hidden zones: what an honest redeal may deal it.
func derivablePool(t *testing.T, e *rules.Engine, setup searchprobe.PublicGame, p state.PlayerID) []string {
	t.Helper()
	pool := make(map[string]int)
	for _, c := range setup.Decks[p] {
		pool[c.Faces[0].Name]++
	}
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		public := o.Zone == state.ZBattlefield || o.Zone == state.ZGraveyard || o.Zone == state.ZExile || o.Zone == state.ZStack || o.Zone == state.ZCommand
		if o.Owner != p || o.Card == nil || len(o.Card.Faces) == 0 || !public {
			continue
		}
		if _, ok := pool[o.Card.Faces[0].Name]; ok {
			pool[o.Card.Faces[0].Name]--
		}
	}
	var out []string
	for name, n := range pool {
		for i := 0; i < n; i++ {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// Every redealt world keeps what the searching seat can see -- the pending
// decision, every public zone object for object, the seat's own hand, every
// zone size -- deals each player's hidden cards from the pool the seat can
// derive, and varies what it cannot see: the opponent's hand and both
// libraries' order. The real engine is never touched.
func TestRedealWorldsKeepWhatTheSeatSees(t *testing.T) {
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	p := feedPosition(t, cfg, 4, 3, nil)
	head, nev := p.e.L.Head(), len(p.e.L.Events)
	src, err := NewRedeal(p.input(t, p.e), searchprobe.NewCollector(0), 99, 0)
	if err != nil {
		t.Fatal(err)
	}
	if r := src.Refused(); r != "" {
		t.Fatalf("redeal refused at a plain position: %s", r)
	}
	realOpp := sortedNames(p.e, p.e.G.Zone(state.ZHand, 1))
	oppPool := derivablePool(t, p.e, p.setup, 1)
	sameOppHand, sameOwnLib := 0, 0
	const n = 40
	for sim := 0; sim < n; sim++ {
		w, err := src.World(sim)
		if err != nil {
			t.Fatal(err)
		}
		if !w.Hypothetical {
			t.Fatal("a redealt world must be walked hypothetically")
		}
		we := w.Engine
		if pd := we.Pending(); pd == nil || pd.Seq != p.d.Seq || pd.Kind != p.d.Kind || pd.Player != 0 {
			t.Fatal("world is not at the root decision")
		}
		for pl := state.PlayerID(0); pl < 2; pl++ {
			for _, z := range []state.Zone{state.ZBattlefield, state.ZGraveyard, state.ZExile} {
				if !reflect.DeepEqual(we.G.Zone(z, pl), p.e.G.Zone(z, pl)) {
					t.Fatalf("player %d public zone %v changed", pl, z)
				}
			}
			for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
				if len(we.G.Zone(z, pl)) != len(p.e.G.Zone(z, pl)) {
					t.Fatalf("player %d zone %v size changed", pl, z)
				}
			}
		}
		if !reflect.DeepEqual(sortedNames(we, we.G.Zone(state.ZHand, 0)), sortedNames(p.e, p.e.G.Zone(state.ZHand, 0))) {
			t.Fatal("the seat's own hand was redealt")
		}
		if !reflect.DeepEqual(sortedNames(we, we.G.Zone(state.ZHand, 0), we.G.Zone(state.ZLibrary, 0)), sortedNames(p.e, p.e.G.Zone(state.ZHand, 0), p.e.G.Zone(state.ZLibrary, 0))) {
			t.Fatal("the seat's own hidden multiset changed")
		}
		if got := sortedNames(we, we.G.Zone(state.ZHand, 1), we.G.Zone(state.ZLibrary, 1)); !reflect.DeepEqual(got, oppPool) {
			t.Fatalf("opponent hidden cards are not its derivable pool:\n got %v\nwant %v", got, oppPool)
		}
		if reflect.DeepEqual(sortedNames(we, we.G.Zone(state.ZHand, 1)), realOpp) {
			sameOppHand++
		}
		if reflect.DeepEqual(names(we, we.G.Zone(state.ZLibrary, 0)), names(p.e, p.e.G.Zone(state.ZLibrary, 0))) {
			sameOwnLib++
		}
	}
	t.Logf("opponent hand %v (%d cards); %d/%d worlds dealt that hand, %d/%d kept the seat's own library order", realOpp, len(realOpp), sameOppHand, n, sameOwnLib, n)
	if sameOppHand > n/4 {
		t.Fatalf("%d/%d worlds dealt the opponent its real hand", sameOppHand, n)
	}
	if sameOwnLib > 0 {
		t.Fatalf("%d worlds kept the seat's real library order", sameOwnLib)
	}
	if p.e.L.Head() != head || len(p.e.L.Events) != nev {
		t.Fatal("redealing changed the real engine")
	}
}

// The leak test (AZ spec §4, the pn21 pattern): two real games that differ
// only in the opponent's never-revealed hidden cards -- which cards are in
// its hand, and the order of both libraries -- give byte-identical worlds for
// every seed. What a world deals is a function of the seat's observation and
// the seed alone, so the sampled opponent hand is independent of the real one.
func TestRedealWorldsIgnoreTheRealHiddenCards(t *testing.T) {
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	p := feedPosition(t, cfg, 4, 3, nil)
	alt := p.e.Clone()
	hand, lib := alt.G.Zone(state.ZHand, 1), alt.G.Zone(state.ZLibrary, 1)
	known := p.input(t, p.e).Known
	pinned := make(map[string]bool)
	for _, kh := range known.Hands {
		if kh.Player == 1 {
			for _, c := range kh.Cards {
				pinned[c.Name] = true
			}
		}
	}
	// Swap every unpinned opponent hand card for a library card of another
	// name, secretly, then reverse both libraries.
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
	for pl := state.PlayerID(0); pl < 2; pl++ {
		cur := alt.G.Zone(state.ZLibrary, pl)
		rev := make([]state.ObjID, len(cur))
		for i, id := range cur {
			rev[len(cur)-1-i] = id
		}
		events.Emit(alt.G, alt.L, events.Event{Kind: events.LibraryOrder, Player: pl, IDs: rev, Secret: true})
	}
	if reflect.DeepEqual(sortedNames(alt, alt.G.Zone(state.ZHand, 1)), sortedNames(p.e, p.e.G.Zone(state.ZHand, 1))) {
		t.Fatal("fixture: the two games hold the same opponent hand")
	}
	a, err := NewRedeal(p.input(t, p.e), searchprobe.NewCollector(0), 7, 0)
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewRedeal(p.input(t, alt), searchprobe.NewCollector(0), 7, 0)
	if err != nil {
		t.Fatal(err)
	}
	if a.Refused() != "" || b.Refused() != "" {
		t.Fatalf("refused: %q / %q", a.Refused(), b.Refused())
	}
	for sim := 0; sim < 25; sim++ {
		wa, err := a.World(sim)
		if err != nil {
			t.Fatal(err)
		}
		wb, err := b.World(sim)
		if err != nil {
			t.Fatal(err)
		}
		for pl := state.PlayerID(0); pl < 2; pl++ {
			for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
				na, nb := names(wa.Engine, wa.Engine.G.Zone(z, pl)), names(wb.Engine, wb.Engine.G.Zone(z, pl))
				if !reflect.DeepEqual(na, nb) {
					t.Fatalf("sim %d player %d %v depends on the real hidden cards:\n%v\n%v", sim, pl, z, na, nb)
				}
			}
		}
	}
}

// The incremental projection a seat keeps equals the whole-history fold at
// every one of its decisions.
func TestKnownTrackerMatchesProjection(t *testing.T) {
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	var kt KnownTracker
	checks := 0
	feedPosition(t, cfg, 6, 0, func(f *searchseat.Feed) {
		h := f.History()
		got, err := kt.Update(h)
		if err != nil {
			t.Fatal(err)
		}
		want, err := searchprobe.ProjectKnownCards(h)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("frame %d: incremental projection %+v, whole fold %+v", len(h.Frames), got, want)
		}
		checks++
	})
	if checks < 10 {
		t.Fatalf("only %d checks", checks)
	}
}

// Search runs on redealt worlds, one per simulation and K round-robin, and
// is deterministic in its seed.
func TestSearchOnRedealtWorlds(t *testing.T) {
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	p := feedPosition(t, cfg, 4, 3, nil)
	for _, k := range []int{0, 3} {
		var prev Result
		for rep := 0; rep < 2; rep++ {
			obs := searchprobe.NewCollector(0)
			src, err := NewRedeal(p.input(t, p.e), obs, 11, k)
			if err != nil {
				t.Fatal(err)
			}
			opts := DefaultOptions()
			opts.Sims, opts.Seed = 12, 11
			res, err := Search(Root{Engine: p.e, Decision: p.d, Bot: p.bot, Observer: obs}, src, nil, opts)
			if err != nil {
				t.Fatal(err)
			}
			if res.Stats.Searched != 1 || res.Stats.Completed == 0 || res.Stats.NoWorld != 0 {
				t.Fatalf("K=%d: %+v", k, res.Stats)
			}
			if rep == 1 && (!reflect.DeepEqual(res.Visits, prev.Visits) || res.Choice != prev.Choice) {
				t.Fatalf("K=%d: not deterministic: %v vs %v", k, res.Visits, prev.Visits)
			}
			prev = res
		}
		t.Logf("K=%d: visits %v, completed %d", k, prev.Visits, prev.Stats.Completed)
	}
}

// An az seat on the redeal source plays whole games without the
// clairvoyant gate open, searches, and replays exactly.
func TestSeatOnRedealPlaysAndReplays(t *testing.T) {
	prevGate := clairvoyantAllowed.Load()
	clairvoyantAllowed.Store(false)
	t.Cleanup(func() { clairvoyantAllowed.Store(prevGate) })
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	sc := DefaultSeatConfig()
	sc.Search.Sims, sc.World = 4, WorldRedeal
	var diags []Diag
	prev := Watch
	Watch = func(d Diag) { diags = append(diags, d) }
	t.Cleanup(func() { Watch = prev })
	heads := make([]string, 2)
	for i := range heads {
		az, err := NewSeat(cfg.Seed^1, nil, sc)
		if err != nil {
			t.Fatal(err)
		}
		if heads[i], err = playAZ(t, cfg, az, 300); err != nil {
			t.Fatal(err)
		}
	}
	if heads[0] != heads[1] {
		t.Fatalf("replay diverged: %s vs %s", heads[0], heads[1])
	}
	var total Stats
	searched := 0
	for _, d := range diags {
		total.Add(d.Stats)
		if d.Searched {
			searched++
		}
		if d.Refused != "" {
			t.Logf("refused: %s", d.Refused)
		}
	}
	t.Logf("%d searched; completed %d/%d, no-world %d, refused %d, feed-stopped %d", searched, total.Completed, total.Simulations, total.NoWorld, total.RedealRefused, total.FeedStopped)
	if searched == 0 || total.Completed == 0 {
		t.Fatal("the redeal seat never searched")
	}
	if total.RedealRefused*4 > searched {
		t.Fatalf("redeal refused at %d of %d searched decisions", total.RedealRefused, searched)
	}
}

func TestNewSeatRejectsUnknownWorld(t *testing.T) {
	sc := DefaultSeatConfig()
	sc.World = "sampled"
	if _, err := NewSeat(1, nil, sc); err == nil {
		t.Fatal("unknown world source accepted")
	}
}

// Regression (M1 diagnosis): on the CawGates mirror an embalmed Sacred Cat's
// token bears a deck card's name; counting it as a seen deck card made the
// seat's derivable pool one short and refused the redeal. Tokens and copies
// are not deck cards, so the seat never refuses (and never fails a deal).
func TestRedealAccountsForNamedTokens(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	cg, err := spellbench.Deck(reg, spellbench.PauperKernel, "CawGates")
	if err != nil {
		t.Fatal(err)
	}
	sc := DefaultSeatConfig()
	sc.Search.Sims, sc.World = 2, WorldRedeal
	var diags []Diag
	prev := Watch
	Watch = func(d Diag) { diags = append(diags, d) }
	t.Cleanup(func() { Watch = prev })
	embalmed := 0
	for s := uint64(0); s < 6 && embalmed == 0; s++ {
		cfg := rules.Config{Seed: 7700 + s, Names: []string{"CawGates", "CawGates"}, Decks: [][]*cards.Card{cg, cg}, Tokens: reg.Tokens}
		az, err := NewSeat(cfg.Seed^1, nil, sc)
		if err != nil {
			t.Fatal(err)
		}
		_, e, err := gbench.PlayGame(cfg, []seat.Seat{az, seat.NewBot(cfg.Seed ^ 2)}, 200, 6000, gbench.Hooks{})
		if err != nil {
			t.Fatal(err)
		}
		for i := range e.G.Objs {
			if o := &e.G.Objs[i]; o.IsToken && o.Card != nil && len(o.Card.Faces) > 0 && o.Card.Faces[0].Name == "Sacred Cat" {
				embalmed++
			}
		}
	}
	var total Stats
	for _, d := range diags {
		total.Add(d.Stats)
		if d.Refused != "" || d.DealFailed != "" {
			t.Errorf("refused %q, deal failed %q", d.Refused, d.DealFailed)
		}
	}
	t.Logf("%d embalmed Sacred Cat tokens; %d searched, no-world %d", embalmed, total.Searched, total.NoWorld)
	if embalmed == 0 {
		t.Skip("no seed embalmed a Sacred Cat; the regression was not exercised")
	}
}
