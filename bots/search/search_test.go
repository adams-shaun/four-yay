package search

import (
	"context"
	"reflect"
	"testing"

	// the caretaker every search entry names (spec §3.1); Validate below
	// refuses an entry whose caretaker is not a registered non-Env policy,
	// so the caretaker must be linked into this test binary.
	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/bots"
	_ "github.com/adams-shaun/gorge/bots/bot"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/internal/searchseat"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// delegate is a recording stand-in for *searchseat.SearchBot: it notes which
// half the adapter called and remembers what it was handed, so the routing
// tests below can assert the forwarding without paying a real search.
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

// castPriority is a priority decision offering two distinct castable objects,
// the shape Eligible's cast arm covers.
func castPriority() *decision.Decision {
	return &decision.Decision{Kind: decision.KPriority, Options: []decision.Option{
		{Index: 0, Kind: "cast", Obj: 101}, {Index: 1, Kind: "cast", Obj: 202},
	}}
}

func TestHostedSearchEntryIsRegistered(t *testing.T) {
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
		t.Errorf("Formats = %v, want [constructed]: every search measurement is 1v1 constructed", e.Formats)
	}
	if e.MaxSeats != 2 {
		t.Errorf("MaxSeats = %d, want 2 (spec §3.1)", e.MaxSeats)
	}
	if len(e.Strength) == 0 || e.Strength[0].Claim == "" || e.Strength[0].Source == "" {
		t.Errorf("Strength = %+v, want one measured claim with a source", e.Strength)
	}
	if e.Cost.MeanMS == 0 || e.Cost.Scope != "per searched decision" {
		t.Errorf("Cost = %+v, want the measured 664 ms mean per searched decision", e.Cost)
	}
	if err := bots.Validate(); err != nil {
		t.Errorf("bots.Validate: %v", err)
	}

	s, err := bots.New(Policy, bots.Options{Seed: 5, SearchParallelism: 2})
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

// TestHostedSearchConfigIsHonest pins the hosted config (spec §4.3, §11): the
// registry builds searchseat.Defaults() — the measured teacher's knobs — with
// only Parallelism overlaid, and sets none of the measurement switches that
// would change what the seat may read or how it samples. A config that turned
// Redeal/Clairvoyant on, or carried a Prior or an oracle value head, would be
// a cheating or off-measurement seat; a config that dropped a knob from
// Defaults would silently drift off the measured setting.
func TestHostedSearchConfigIsHonest(t *testing.T) {
	base := searchseat.Defaults()

	// Precondition: Defaults actually configures the searched kinds, so the
	// honesty assertions below are about a live config and not a zero value.
	if len(base.Kinds) == 0 || !base.Kinds["attackers"] || !base.Kinds["cast"] {
		t.Fatalf("Defaults().Kinds = %v, want attackers+cast on: the honesty check would be vacuous", base.Kinds)
	}

	cfg := Hosted()
	if !reflect.DeepEqual(cfg, base) {
		t.Errorf("Hosted() = %+v, want exactly searchseat.Defaults() %+v", cfg, base)
	}
	for name, zero := range map[string]interface{}{
		"Clairvoyant":             cfg.Clairvoyant,
		"Redeal":                  cfg.Redeal,
		"NoLandExclusion":         cfg.NoLandExclusion,
		"ComparePotentialActions": cfg.ComparePotentialActions,
	} {
		if zero != false {
			t.Errorf("Hosted().%s = %v, want false: a playing seat leaves the measurement switches off", name, zero)
		}
	}
	if cfg.Prior != nil {
		t.Error("Hosted().Prior is set: the hosted seat plays the hand-ordered candidate list, not a checkpoint")
	}
	if cfg.OracleValue != nil {
		t.Error("Hosted().OracleValue is set: the hosted seat has no value head")
	}

	// The registry factory overlays Parallelism and nothing else.
	o := bots.Options{Seed: 9, SearchParallelism: 3}
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
	want := Hosted()
	want.Parallelism = 3
	if !reflect.DeepEqual(hs.opts, want) {
		t.Errorf("factory config = %+v, want Hosted() with Parallelism %d: %+v", hs.opts, 3, want)
	}
	if hs.opts.SampleSeed != base.SampleSeed || !reflect.DeepEqual(hs.opts.Kinds, base.Kinds) {
		t.Errorf("factory config drifted off the measured knobs: seed %d kinds %v", hs.opts.SampleSeed, hs.opts.Kinds)
	}
}

// TestHostedSearchWantsEnvIsEligible pins WantsEnv to searchseat.Eligible —
// the host gates its redeal on it, so any drift either pays a root for a
// decision the teacher delegates (wasted redeals) or starves a decision the
// teacher covers. The sample must contain both an eligible and an ineligible
// decision, or the equality would hold vacuously.
func TestHostedSearchWantsEnvIsEligible(t *testing.T) {
	cfg := Hosted()
	s := &hostedSeat{bot: &delegate{}, opts: cfg}
	ds := []*decision.Decision{
		{Kind: decision.KAttackers},
		{Kind: decision.KBlockers},
		castPriority(),
		{Kind: decision.KPriority, Options: []decision.Option{{Index: 0, Kind: "cast", Obj: 7}}},
		{Kind: decision.KMulligan},
		{Kind: decision.KChoose, Options: []decision.Option{{Index: 0, Kind: "done"}}},
	}
	eligible := 0
	for _, d := range ds {
		want := searchseat.Eligible(d, cfg)
		if got := s.WantsEnv(d); got != want {
			t.Errorf("WantsEnv(%s) = %v, want Eligible %v", d.Kind, got, want)
		}
		if want {
			eligible++
		}
	}
	if eligible == 0 || eligible == len(ds) {
		t.Fatalf("sample has %d/%d eligible decisions; it must contain both kinds or the pin is vacuous", eligible, len(ds))
	}
}

// TestHostedSearchSeatRoutesTheEnv pins the spec §5.1 routing: the Env is
// forwarded to the wrapped DecideSearch exactly when the honest root built and
// the feed is live, and every other shape plays the wrapped bot on env.Board
// (the bench's fallback). A root that reached DecideBoard, or a fallback that
// reached DecideSearch, would either leak the live engine through the wrong
// half or search without an observation feed.
func TestHostedSearchSeatRoutesTheEnv(t *testing.T) {
	feed := searchseat.NewFeed(state.PlayerID(0))
	if !feed.Live() {
		t.Fatal("a fresh feed is not live; the healthy case below would be the fallback")
	}
	root := &rules.Engine{}
	// board carries one marker fact; the identity of the board a delegate
	// receives is checked through it, because Board holds maps (not
	// comparable) and its value is copied through the Env.
	board := botpolicy.NewBoard(2)
	board.Life[0] = 42
	setup := searchprobePublicGame()
	d := decision.Decision{Kind: decision.KAttackers, Seq: 3}
	intent := decision.Intent{}

	build := func(f *delegate) *hostedSeat { return &hostedSeat{bot: f, opts: Hosted()} }

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
		if f.env == nil || f.env.Engine != root || f.env.Feed != feed || f.env.Board.Life[0] != 42 || !reflect.DeepEqual(f.env.Setup, setup) {
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
		if f.board.Life[0] != 42 {
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
		if f.board.Life[0] != 42 {
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

func searchprobePublicGame() searchprobe.PublicGame {
	return searchprobe.PublicGame{Names: []string{"a", "b"}, StartingLife: 20}
}
