package azredeal

import (
	"context"
	"reflect"
	"testing"

	// the caretaker the entry names (spec §3.1); Validate below refuses an
	// entry whose caretaker is not a registered non-Env policy, so the
	// caretaker must be linked into this test binary.
	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/bots"
	_ "github.com/adams-shaun/gorge/bots/bot"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/azmcts"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/internal/searchseat"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// delegate is a recording stand-in for *azmcts.Seat: it notes which half the
// adapter called and remembers what it was handed, so the routing tests below
// can assert the forwarding without paying a real 100-sim search.
type delegate struct {
	calls []string
	env   *searchseat.Env
	d     *decision.Decision
	board botpolicy.Board
	in    decision.Intent
}

func (f *delegate) Decide(ctx context.Context, v view.View, d decision.Decision) (decision.Intent, error) {
	f.calls = append(f.calls, "Decide")
	return f.in, nil
}

func (f *delegate) DecideBoard(ctx context.Context, b botpolicy.Board, d decision.Decision) (decision.Intent, error) {
	f.calls = append(f.calls, "DecideBoard")
	f.board = b
	return f.in, nil
}

func (f *delegate) DecideSearch(ctx context.Context, env searchseat.Env, d decision.Decision) (decision.Intent, error) {
	f.calls = append(f.calls, "DecideSearch")
	e := env
	f.env = &e
	f.d = &d
	return f.in, nil
}

func TestHostedAZEntryIsRegistered(t *testing.T) {
	e, ok := bots.Lookup(Policy)
	if !ok {
		t.Fatalf("bots.Lookup(%q): not registered (known: %v)", Policy, bots.Names())
	}
	if e.Tier != bots.Experimental {
		t.Errorf("Tier = %q, want experimental (only bot is production, spec §3.1)", e.Tier)
	}
	if !e.Env || !e.Search {
		t.Errorf("Env/Search = %v/%v, want true/true: the host must build Envs and charge search slots", e.Env, e.Search)
	}
	if e.Caretaker != "bot" {
		t.Errorf("Caretaker = %q, want bot (spec §3.1)", e.Caretaker)
	}
	if !reflect.DeepEqual(e.Formats, []string{"constructed"}) {
		t.Errorf("Formats = %v, want [constructed]: every az measurement is 1v1 constructed", e.Formats)
	}
	if e.MaxSeats != 2 {
		t.Errorf("MaxSeats = %d, want 2 (spec §3.1)", e.MaxSeats)
	}
	if len(e.Strength) == 0 || e.Strength[0].Claim == "" || e.Strength[0].Source == "" {
		t.Errorf("Strength = %+v, want measured claims with a source", e.Strength)
	}
	if e.Cost.MeanMS == 0 || e.Cost.Scope != "per searched decision" {
		t.Errorf("Cost = %+v, want the measured ~250 ms mean per searched decision", e.Cost)
	}
	if err := bots.Validate(); err != nil {
		t.Errorf("bots.Validate: %v", err)
	}

	s, err := bots.New(Policy, bots.Options{Seed: 5})
	if err != nil {
		t.Fatalf("bots.New: %v", err)
	}
	if _, ok := s.(bots.EnvSeat); !ok {
		t.Error("the built seat does not implement bots.EnvSeat: the host would never build an Env for it")
	}
	if _, ok := s.(seat.BoardSeat); !ok {
		t.Error("the built seat does not implement seat.BoardSeat: non-Env decisions would pay a View projection")
	}
}

// TestHostedAZIsRedeal pins the hosted config (spec §4.3, §5.4, §11): the
// registry builds azmcts.DefaultSeatConfig() — the 100-sim eval knobs the
// +20.5pp measurement was taken with — in the honest REDEAL world with zero K,
// and never arms the switches that would make it a different or dishonest
// bot: Source stays nil (NewSeat refuses a clairvoyant world without one, so
// a hosted seat can never clone the real engine), the net is nil (generation
// 0: heuristic leaf, uniform prior), and Explore is false (argmax, no noise).
// A Hosted() that dropped a knob from DefaultSeatConfig, or that armed the
// clairvoyant world, would silently drift off the measured setting or leak.
func TestHostedAZIsRedeal(t *testing.T) {
	base := azmcts.DefaultSeatConfig()

	// Precondition: the eval default leaves the world unset (the clairvoyant
	// stage-1 default) — so the World assertion below is about the hosted
	// override and not a tautology — and configures a live search budget.
	if base.World != "" {
		t.Fatalf("DefaultSeatConfig().World = %q, want the empty clairvoyant default: the honest-world pin below would be vacuous", base.World)
	}
	if base.Search.Sims <= 0 {
		t.Fatalf("DefaultSeatConfig().Search.Sims = %d, want a live budget", base.Search.Sims)
	}

	cfg := Hosted()
	if cfg.World != azmcts.WorldRedeal {
		t.Errorf("Hosted().World = %q, want %q: the hosted seat plays the honest redeal world, never clairvoyant", cfg.World, azmcts.WorldRedeal)
	}
	if cfg.Worlds != 0 {
		t.Errorf("Hosted().Worlds = %d, want 0: a fresh deal per simulation is the measured gen-0 shape", cfg.Worlds)
	}
	if cfg.Source != nil {
		t.Error("Hosted().Source is set: a hosted entry must never inject the clairvoyant world source")
	}
	if cfg.Explore {
		t.Error("Hosted().Explore is set: the hosted seat plays argmax with no noise, not generation mode")
	}
	if cfg.PriorOnly {
		t.Error("Hosted().PriorOnly is set: the hosted seat searches, it does not play a network prior")
	}
	if cfg.Search.Sims != base.Search.Sims || cfg.Search.Limit != base.Search.Limit ||
		cfg.Search.MaxSteps != base.Search.MaxSteps || cfg.Search.Kinds != base.Search.Kinds {
		t.Errorf("Hosted().Search = %+v, want the eval knobs unchanged: %+v", cfg.Search, base.Search)
	}

	// The built seat carries exactly this config, a nil net and no Source:
	// the invariant lives in the wrapped az seat, not just in Hosted().
	o := bots.Options{Seed: 9}
	entry, ok := bots.Lookup(Policy)
	if !ok {
		t.Fatal("entry vanished")
	}
	s, err := entry.New(o)
	if err != nil {
		t.Fatalf("entry.New: %v", err)
	}
	hs, ok := s.(*hostedSeat)
	if !ok {
		t.Fatalf("entry.New built %T, want *hostedSeat", s)
	}
	if !reflect.DeepEqual(hs.cfg, Hosted()) {
		t.Errorf("factory config = %+v, want Hosted() unchanged", hs.cfg)
	}
	az, ok := hs.bot.(*azmcts.Seat)
	if !ok {
		t.Fatalf("the adapter wraps %T, want *azmcts.Seat", hs.bot)
	}
	// reflect reads unexported fields read-only, which is exactly what this
	// pin needs: the wrapper's promise ("never set Source", "nil net") is a
	// fact about the az seat's private state.
	azv := reflect.ValueOf(az).Elem()
	if net := azv.FieldByName("net"); !net.IsNil() {
		t.Error("the wrapped az seat carries a network: the hosted entry is generation 0 (heuristic leaf, uniform prior)")
	}
	if seed := azv.FieldByName("seed"); seed.Uint() != o.Seed {
		t.Errorf("the wrapped az seat's seed is %d, want the Options seed %d", seed.Uint(), o.Seed)
	}
	azCfg := azv.FieldByName("cfg")
	if got := azCfg.FieldByName("World").String(); got != azmcts.WorldRedeal {
		t.Errorf("the wrapped seat's World is %q, want %q", got, azmcts.WorldRedeal)
	}
	if got := azCfg.FieldByName("Source"); !got.IsNil() {
		t.Error("the wrapped seat's Source is set: a hosted entry must never inject the clairvoyant world source")
	}
	if got := azCfg.FieldByName("Worlds").Int(); got != 0 {
		t.Errorf("the wrapped seat's Worlds is %d, want 0", got)
	}
}

// TestHostedAZWantsEnvIsTheSearchedKinds pins WantsEnv to the az seat's
// searched kinds under its own kind set (spec §5.1: priority, attackers,
// blockers, target) — the host gates its redeal on it, so any drift either
// pays a root for a decision the az seat would delegate (wasted redeals) or
// starves a decision the az seat would search. The sample must contain both
// a searched and an unsearched kind, or the pin would hold vacuously.
func TestHostedAZWantsEnvIsTheSearchedKinds(t *testing.T) {
	s := &hostedSeat{bot: &delegate{}, cfg: Hosted()}
	ds := []*decision.Decision{
		{Kind: decision.KPriority},
		{Kind: decision.KAttackers},
		{Kind: decision.KBlockers},
		{Kind: decision.KTarget},
		{Kind: decision.KMulligan},
		{Kind: decision.KChoose, Options: []decision.Option{{Index: 0, Kind: "done"}}},
	}
	searched, unsearched := ds[:4], ds[4:]
	for _, d := range searched {
		if got := s.WantsEnv(d); !got {
			t.Errorf("WantsEnv(%s) = false, want true: every searched kind earns an Env", d.Kind)
		}
	}
	// Precondition: the sample really contains unsearched kinds, so the
	// negative assertions below are not empty.
	if len(unsearched) == 0 {
		t.Fatal("the sample holds only searched kinds; it must contain both or the pin is vacuous")
	}
	for _, d := range unsearched {
		if s.WantsEnv(d) {
			t.Errorf("WantsEnv(%s) = true, want false: only the searched kinds earn an Env", d.Kind)
		}
	}

	// A config that narrows Kinds must narrow WantsEnv with it: the host
	// pays a redeal for every Env the seat asks for.
	narrow := Hosted()
	narrow.Search.Kinds = azmcts.Kinds{Attackers: true}
	ns := &hostedSeat{bot: &delegate{}, cfg: narrow}
	if !ns.WantsEnv(&decision.Decision{Kind: decision.KAttackers}) {
		t.Error("WantsEnv(attackers) = false under an attackers-only config")
	}
	if ns.WantsEnv(&decision.Decision{Kind: decision.KPriority}) {
		t.Error("WantsEnv(priority) = true under an attackers-only config: the host would pay a redeal the seat never uses")
	}
}

// TestHostedAZSeatRoutesTheEnv pins the spec §5.1 routing: the Env is
// forwarded to the wrapped DecideSearch exactly when the honest root built and
// the feed is live, and every other shape plays the wrapped bot on env.Board
// (the bench's fallback). A root that reached DecideBoard, or a fallback that
// reached DecideSearch, would either leak the live engine through the wrong
// half or search without an observation feed — the az seat's redeal source is
// built from the feed's history, so the latter is not merely wasteful, it
// fails the decision loudly.
func TestHostedAZSeatRoutesTheEnv(t *testing.T) {
	feed := searchseat.NewFeed(state.PlayerID(0))
	if !feed.Live() {
		t.Fatal("a fresh feed is not live; the healthy case below would be the fallback")
	}
	root := &rules.Engine{}
	// board carries one marker fact; the identity of the board a delegate
	// receives is checked through it, because Board holds maps (not
	// comparable) and its value is copied through the Env.
	board := botpolicy.NewBoard(2)
	board.Life.Set(0, 42)
	setup := searchprobe.PublicGame{Names: []string{"a", "b"}, StartingLife: 20}
	d := decision.Decision{Kind: decision.KPriority, Seq: 3}
	intent := decision.Intent{}

	build := func(f *delegate) *hostedSeat { return &hostedSeat{bot: f, cfg: Hosted()} }

	t.Run("healthy forwards the whole env to DecideSearch", func(t *testing.T) {
		f := &delegate{in: intent}
		env := bots.Env{
			Search: searchseat.Env{Setup: setup, Engine: root, Board: board, Feed: feed},
		}
		in, err := build(f).DecideEnv(context.Background(), env, d)
		if err != nil {
			t.Fatalf("DecideEnv: %v", err)
		}
		if len(f.calls) != 1 || f.calls[0] != "DecideSearch" {
			t.Fatalf("calls = %v, want exactly one DecideSearch", f.calls)
		}
		if f.env == nil || f.env.Engine != root || f.env.Feed != feed || f.env.Board.Life.Get(0) != 42 || !reflect.DeepEqual(f.env.Setup, setup) {
			t.Fatalf("DecideSearch got env %+v, want the forwarded env.Search unchanged", f.env)
		}
		if f.d == nil || f.d.Seq != d.Seq || f.d.Kind != d.Kind {
			t.Fatalf("DecideSearch got decision %+v, want %v", f.d, d)
		}
		if !reflect.DeepEqual(in, intent) {
			t.Fatalf("intent %v, want the delegate's %v", in, intent)
		}
	})

	t.Run("refused root plays the board fallback", func(t *testing.T) {
		f := &delegate{in: intent}
		env := bots.Env{Board: board, RootRefused: "no live observation feed",
			Search: searchseat.Env{Setup: setup, Engine: nil, Board: board, Feed: feed}}
		in, err := build(f).DecideEnv(context.Background(), env, d)
		if err != nil {
			t.Fatalf("DecideEnv: %v", err)
		}
		if len(f.calls) != 1 || f.calls[0] != "DecideBoard" {
			t.Fatalf("calls = %v, want exactly one DecideBoard", f.calls)
		}
		if f.board.Life.Get(0) != 42 {
			t.Fatalf("DecideBoard got %v, want env.Board (the marker fact is missing)", f.board)
		}
		if !reflect.DeepEqual(in, intent) {
			t.Fatalf("intent %v, want the delegate's %v", in, intent)
		}
	})

	t.Run("nil feed plays the board fallback", func(t *testing.T) {
		f := &delegate{in: intent}
		env := bots.Env{Board: board,
			Search: searchseat.Env{Setup: setup, Engine: root, Board: board, Feed: nil}}
		if _, err := build(f).DecideEnv(context.Background(), env, d); err != nil {
			t.Fatalf("DecideEnv: %v", err)
		}
		if len(f.calls) != 1 || f.calls[0] != "DecideBoard" {
			t.Fatalf("calls = %v, want exactly one DecideBoard (a nil feed is a stopped feed)", f.calls)
		}
		if f.board.Life.Get(0) != 42 {
			t.Fatalf("DecideBoard got %v, want env.Board (the marker fact is missing)", f.board)
		}
	})

	t.Run("non-env decisions never touch the search half", func(t *testing.T) {
		f := &delegate{in: intent}
		if _, err := build(f).DecideBoard(context.Background(), board, d); err != nil {
			t.Fatalf("DecideBoard: %v", err)
		}
		if _, err := build(f).Decide(context.Background(), view.View{}, d); err != nil {
			t.Fatalf("Decide: %v", err)
		}
		if len(f.calls) != 2 || f.calls[0] != "DecideBoard" || f.calls[1] != "Decide" {
			t.Fatalf("calls = %v, want DecideBoard then Decide and no search", f.calls)
		}
	})
}
