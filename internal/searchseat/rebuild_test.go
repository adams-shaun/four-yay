package searchseat_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	gbench "github.com/adams-shaun/gorge/internal/bench"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	ss "github.com/adams-shaun/gorge/internal/searchseat"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/replay"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
)

type rebuildCaptureSeat struct {
	*ss.SearchBot
	feed **ss.Feed
}

func (s *rebuildCaptureSeat) DecideSearch(ctx context.Context, env ss.Env, d decision.Decision) (decision.Intent, error) {
	*s.feed = env.Feed
	return s.SearchBot.DecideSearch(ctx, env, d)
}

func TestRebuildFeedMatchesTheLiveFeed(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	red, err := testutil.LoadRepoDeck(reg, "mono-red-prowess")
	if err != nil {
		t.Fatal(err)
	}
	blue, err := testutil.LoadRepoDeck(reg, "mono-blue-tempo")
	if err != nil {
		t.Fatal(err)
	}
	const seed = uint64(30000042)
	cfg := rules.Config{Seed: seed, Names: []string{"mono-red-prowess", "mono-blue-tempo"}, Decks: [][]*cards.Card{red, blue}, Tokens: reg.Tokens}
	opts := ss.Defaults()
	opts.Worlds, opts.Attempts = 2, 4
	var live *ss.Feed
	seats := []seat.Seat{
		&rebuildCaptureSeat{SearchBot: ss.NewSearchBot(seed^1, opts), feed: &live},
		seat.NewBot(seed ^ 2),
	}
	_, engine, err := gbench.PlayGame(cfg, seats, 200, 80, gbench.Hooks{})
	if err != nil {
		t.Fatalf("bench play: %v", err)
	}
	if live == nil || live.Frames() < 3 {
		t.Fatalf("bench did not produce a useful live search feed: feed=%v", live)
	}
	prefixes := []int{0, len(engine.L.Intents) / 2, len(engine.L.Intents) - 1}
	if prefixes[1] == 0 || prefixes[1] == prefixes[2] {
		t.Fatalf("game produced %d intents; cannot test three distinct prefixes", len(engine.L.Intents))
	}
	for _, n := range prefixes {
		t.Run(fmt.Sprintf("prefix-%d", n), func(t *testing.T) {
			got, err := ss.RebuildFeed(cfg, engine.L, n, state.PlayerID(0))
			if err != nil {
				t.Fatalf("RebuildFeed(%d): %v", n, err)
			}
			want, err := referenceFeed(cfg, engine.L, n, state.PlayerID(0))
			if err != nil {
				t.Fatalf("reference feed: %v", err)
			}
			if !reflect.DeepEqual(got.History(), want.History()) {
				t.Fatalf("prefix %d: rebuilt frames or answers differ", n)
			}
			if !reflect.DeepEqual(got.Collector().Clone(), want.Collector().Clone()) {
				t.Fatalf("prefix %d: rebuilt collector state differs", n)
			}
			livePrefix := searchprobe.History{Actor: live.History().Actor, Frames: live.History().Frames[:n+1], Answers: make(map[int][]searchprobe.Action)}
			for frame, answer := range live.History().Answers {
				if frame < n {
					livePrefix.Answers[frame] = answer
				}
			}
			if !reflect.DeepEqual(got.History(), livePrefix) {
				gh, lh := got.History(), livePrefix
				t.Fatalf("prefix %d: rebuilt/live history differ: actors %d/%d frames %d/%d answers %#v/%#v; first frame board equal=%v events=%d/%d identities=%d/%d decision equal=%v",
					n, gh.Actor, lh.Actor, len(gh.Frames), len(lh.Frames), gh.Answers, lh.Answers,
					reflect.DeepEqual(gh.Frames[0].Board, lh.Frames[0].Board), len(gh.Frames[0].Events), len(lh.Frames[0].Events),
					len(gh.Frames[0].Identities), len(lh.Frames[0].Identities), reflect.DeepEqual(gh.Frames[0].Decision, lh.Frames[0].Decision))
			}
		})
	}
}

func referenceFeed(cfg rules.Config, l *events.Log, n int, actor state.PlayerID) (*ss.Feed, error) {
	f := ss.NewFeed(actor)
	_, err := replay.Walk(l, cfg, n, func(e *rules.Engine, i int) error {
		if _, ok := f.Observe(e); !ok {
			return fmt.Errorf("observe %d: %s", i, f.StopReason())
		}
		if i < n && i < len(l.Intents) {
			if d := e.Pending(); d != nil && d.Player == actor {
				return f.RecordAnswer(d, l.Intents[i])
			}
		}
		return nil
	})
	return f, err
}
