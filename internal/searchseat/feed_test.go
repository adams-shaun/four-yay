package searchseat

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// driveTeacherStyle runs the pre-Feed teacher loop shape -- a raw Collector,
// a hand-built History, the pos cursor -- over cfg's game, capturing at every
// decision of every player and recording the actor's answers, and hands back
// the history and the finished engine. It is the exact inline code the Feed
// replaced in cmd/searchteacher's playGame, kept here as the oracle the Feed
// must reproduce.
func driveTeacherStyle(t *testing.T, cfg rules.Config, actor state.PlayerID, maxIntents int) (searchprobe.History, *rules.Engine) {
	t.Helper()
	e := rules.New(cfg)
	e.Advance()
	rngs := searchprobe.BotRandoms(cfg.Seed, len(cfg.Names))
	board := botpolicy.NewBoard(len(cfg.Names))
	collector := searchprobe.NewCollector(actor)
	h := searchprobe.History{Actor: actor, Answers: map[int][]searchprobe.Action{}}
	pos := 0
	for steps := 0; !e.G.Over && steps < maxIntents; steps++ {
		d := e.Pending()
		if d == nil {
			break
		}
		b := botpolicy.BoardFromGameInto(e.G, e, d.Player, &board)
		in := botpolicy.Decide(b, d, rngs[d.Player])
		f, err := collector.Capture(e, e.L.Events[pos:])
		if err != nil {
			t.Fatalf("oracle capture failed: %v", err)
		}
		h.Frames = append(h.Frames, f)
		if d.Player == actor {
			a, err := collector.Actions(d, in)
			if err != nil {
				t.Fatalf("oracle answer record: %v", err)
			}
			h.Answers[len(h.Frames)-1] = a
		}
		pos = len(e.L.Events)
		if err := e.Submit(in); err != nil {
			t.Fatalf("submit: %v", err)
		}
	}
	return h, e
}

// TestFeedReproducesTheTeacherLoop pins the Feed against the inline loop it
// replaced (driveTeacherStyle): frame count, every frame's observed events
// and board bytes, and the recorded answers must be identical, because a
// capture path that drifted from the teacher's would invalidate both the
// corpus the generator emits and the worlds the playing seat samples.
func TestFeedReproducesTheTeacherLoop(t *testing.T) {
	names, decks := testutil.SampleDecks(t, 2)
	cfg := rules.Config{Seed: 7, Names: names, Decks: decks}
	actor := state.PlayerID(0)

	oracle, oeng := driveTeacherStyle(t, cfg, actor, 80)

	e := rules.New(cfg)
	e.Advance()
	rngs := searchprobe.BotRandoms(cfg.Seed, len(cfg.Names))
	board := botpolicy.NewBoard(len(cfg.Names))
	feed := NewFeed(actor)
	for steps := 0; !e.G.Over && steps < 80; steps++ {
		d := e.Pending()
		if d == nil {
			break
		}
		b := botpolicy.BoardFromGameInto(e.G, e, d.Player, &board)
		in := botpolicy.Decide(b, d, rngs[d.Player])
		if _, ok := feed.Observe(e); !ok {
			t.Fatalf("feed capture failed: %s", feed.StopReason())
		}
		if d.Player == actor {
			if err := feed.RecordAnswer(d, in); err != nil {
				t.Fatalf("feed answer record: %v", err)
			}
		}
		if err := e.Submit(in); err != nil {
			t.Fatalf("submit: %v", err)
		}
	}

	got := feed.History()
	if feed.Frames() != len(oracle.Frames) {
		t.Fatalf("feed frames = %d, teacher loop = %d", feed.Frames(), len(oracle.Frames))
	}
	for i := range oracle.Frames {
		of, gf := oracle.Frames[i], got.Frames[i]
		if len(of.Events) != len(gf.Events) {
			t.Fatalf("frame %d: feed events = %d, teacher loop = %d", i, len(gf.Events), len(of.Events))
		}
		for j := range of.Events {
			if !reflect.DeepEqual(of.Events[j], gf.Events[j]) {
				t.Fatalf("frame %d event %d: feed %+v, teacher loop %+v", i, j, gf.Events[j], of.Events[j])
			}
		}
		if !reflect.DeepEqual(of.Board, gf.Board) {
			t.Fatalf("frame %d: board differs from the teacher loop's", i)
		}
	}
	if !reflect.DeepEqual(got.Answers, oracle.Answers) {
		t.Fatalf("feed answers differ from the teacher loop's recorded answers")
	}
	// And the whole serialized history -- what Sample digests for its seeds.
	dg, err := searchprobe.HistoryDigest(got)
	if err != nil {
		t.Fatal(err)
	}
	do, err := searchprobe.HistoryDigest(oracle)
	if err != nil {
		t.Fatal(err)
	}
	if dg != do {
		t.Fatalf("history digest differs from the teacher loop's")
	}
	ag, _ := json.Marshal(got)
	ao, _ := json.Marshal(oracle)
	if !bytes.Equal(ag, ao) {
		t.Fatalf("serialized history differs from the teacher loop's")
	}
	_ = oeng
}

// TestFeedCapturesAtEveryPlayerDecision pins the "every decision of every
// player" contract directly: one frame per Pending, answers only at the
// actor's.
func TestFeedCapturesAtEveryPlayerDecision(t *testing.T) {
	names, decks := testutil.SampleDecks(t, 2)
	cfg := rules.Config{Seed: 11, Names: names, Decks: decks}
	e := rules.New(cfg)
	e.Advance()
	feed := NewFeed(state.PlayerID(0))
	board := botpolicy.NewBoard(2)
	rngs := searchprobe.BotRandoms(cfg.Seed, len(cfg.Names))
	decisions, actorDecisions := 0, 0
	for steps := 0; !e.G.Over && steps < 60; steps++ {
		d := e.Pending()
		if d == nil {
			break
		}
		decisions++
		b := botpolicy.BoardFromGameInto(e.G, e, d.Player, &board)
		in := botpolicy.Decide(b, d, rngs[d.Player])
		if _, ok := feed.Observe(e); !ok {
			t.Fatalf("capture failed at decision %d: %s", decisions, feed.StopReason())
		}
		if d.Player == state.PlayerID(0) {
			actorDecisions++
			if err := feed.RecordAnswer(d, in); err != nil {
				t.Fatalf("record: %v", err)
			}
		}
		if err := e.Submit(in); err != nil {
			t.Fatalf("submit: %v", err)
		}
	}
	if feed.Frames() != decisions {
		t.Fatalf("frames = %d, decisions = %d (a capture was skipped or duplicated)", feed.Frames(), decisions)
	}
	if len(feed.History().Answers) != actorDecisions {
		t.Fatalf("answers = %d, actor decisions = %d", len(feed.History().Answers), actorDecisions)
	}
	if !feed.Live() {
		t.Fatalf("feed stopped unexpectedly: %s", feed.StopReason())
	}
}
