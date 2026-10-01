package searchseat

import (
	"fmt"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/replay"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// Feed is one search actor's observation stream: the searchprobe.Collector
// and searchprobe.History the sampler needs, maintained by whoever drives the
// engine across that actor's whole game.
//
// It exists because Choose needs a History and a seat cannot build one
// (see this package's doc): searchprobe.Collector.Capture takes a
// *rules.Engine and the raw event burst since the previous capture, and must
// run at EVERY decision of EVERY player, while a seat is invoked only at its
// own. So the driver owns one Feed per search seat, calls Observe at every
// decision of every player, and the seat reads the accumulated History back
// through HistoryRef at its own decisions. cmd/searchteacher's game loop and
// internal/bench.PlayGame (the seat-driving bench) both go through this one
// type, so the two capture paths cannot drift -- and because the extraction
// is behaviour-identical to the loop the teacher always ran, the label corpus
// the generator emits is byte-for-byte what it emitted before the Feed
// existed (pinned by the reference label hash the task keeps).
//
// A Feed is single-goroutine: one game's driver, one seat. Nothing here
// mutates engine state -- Capture is a read-only projection -- so observing
// a game never moves an event, a chain head or a replay.
type Feed struct {
	collector *searchprobe.Collector
	h         searchprobe.History
	// pos is the event-log offset the next Capture bursts from; it advances
	// on every successful capture, exactly as the teacher's loop always
	// advanced it.
	pos int
	// stopped records the first capture error, if any. Once stopped the feed
	// never observes again (the teacher's loop set observing=false and played
	// the bot for the rest of the game) -- a dead observation stream must not
	// wedge a game that can still play legally.
	stopped bool
	stopErr string
	// known is the incremental known-card projection over this feed's frames:
	// the fold semantics are KnownTracker.Update's (the answer to frame i is
	// folded before frame i+1). It is derived state, like the rest of the feed;
	// a fresh Feed (NewFeed, and so RebuildFeed) starts it empty.
	known KnownTracker
}

// RebuildFeed reconstructs an actor's observation stream from the first n
// intents of a log. It captures each intent boundary and records the logged
// answer when that boundary belongs to actor, matching the live driver.
func RebuildFeed(cfg rules.Config, l *events.Log, n int, actor state.PlayerID) (*Feed, error) {
	f := NewFeed(actor)
	_, err := replay.Walk(l, cfg, n, func(e *rules.Engine, i int) error {
		if _, ok := f.Observe(e); !ok {
			return fmt.Errorf("searchseat: observe at intent %d: %s", i, f.StopReason())
		}
		if i < len(l.Intents) && i < n {
			d := e.Pending()
			if d != nil && d.Player == actor {
				// A recorded priority answer may carry a planned payment or an
				// announcement. The live driver builds the payment extension
				// (EnsurePaymentActions) before the seat answers; Submit does
				// the same lazily. The walk's visit fires BEFORE its Submit,
				// so at this boundary the pending decision has no offered
				// payment actions yet and RecordAnswer's Validate would
				// reject the planned witness ("payment action ... is not
				// offered"). Build it here, exactly as Submit does: the build
				// is a pure derived read (no event, no RNG), so the replay
				// comparison cannot diverge, and it only runs where the
				// recorded intent proves it is needed.
				if in := l.Intents[i]; in.Payment != nil || in.Announce != nil {
					e.EnsurePaymentActions()
				}
				if err := f.RecordAnswer(d, l.Intents[i]); err != nil {
					return fmt.Errorf("searchseat: record answer at intent %d: %w", i, err)
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return f, nil
}

// NewFeed starts the observation stream for one actor.
func NewFeed(actor state.PlayerID) *Feed {
	return &Feed{
		collector: searchprobe.NewCollector(actor),
		h:         searchprobe.History{Actor: actor, Answers: map[int][]searchprobe.Action{}},
	}
}

// Unchain stops the feed's captures from extending the history digest chain
// (searchprobe.Collector.Unchain): for a seat that never samples
// (DigestFree), every real decision skips the chain's JSON encoding and
// hash. Call it before the first Observe; Sample refuses the resulting
// history.
func (f *Feed) Unchain() { f.collector.Unchain() }

// DigestFree is a search seat that never roots a sampler in its feed's
// history digest (searchprobe.Sample): a driver may Unchain its feed.
type DigestFree interface {
	DigestFree() bool
}

// Observe captures one frame at the driver's current decision -- at EVERY
// decision of every player, not only the actor's, because the sampler's epoch
// constraints need the whole burst stream. The returned Frame is the one just
// appended (also readable back as LastFrame).
//
// ok is false only when the feed has stopped (a capture error happened
// earlier, or this capture failed). The caller's contract on a failed capture
// is the teacher's own: stop observing and play on -- never wedge.
func (f *Feed) Observe(e *rules.Engine) (searchprobe.Frame, bool) {
	if f.stopped {
		return searchprobe.Frame{}, false
	}
	fh, err := f.collector.Capture(e, e.L.Events[f.pos:])
	if err != nil {
		f.stopped = true
		f.stopErr = err.Error()
		return searchprobe.Frame{}, false
	}
	f.h.Frames = append(f.h.Frames, fh)
	f.pos = len(e.L.Events)
	return fh, true
}

// RecordAnswer records the intent the actor actually played at its most
// recent frame -- the teacher's loop recorded the FINAL intent (post-teach),
// so the sampler's Answers describe what really happened. A translation error
// is returned, not swallowed: an unrecorded answer would silently corrupt
// every later Sample drawn from this history.
func (f *Feed) RecordAnswer(d *decision.Decision, in decision.Intent) error {
	a, err := f.collector.Actions(d, in)
	if err != nil {
		return err
	}
	f.h.Answers[len(f.h.Frames)-1] = a
	return nil
}

// Live reports whether the feed is still observing.
func (f *Feed) Live() bool { return !f.stopped }

// StopReason is the first capture error's message, when the feed stopped.
func (f *Feed) StopReason() string { return f.stopErr }

// HistoryRef is the live history pointer (the teacher's own &h shape -- its
// game loop passes &h down to teach, which reads h.Frames and copies *h into
// Choose). Callers must treat it as read-only except through Observe and
// RecordAnswer.
func (f *Feed) HistoryRef() *searchprobe.History { return &f.h }

// History is the history by value, the shape Choose takes.
func (f *Feed) History() searchprobe.History { return f.h }

// Known is the known-card projection over this feed's frames as of the last
// captured frame, folded incrementally and cached in the feed. It is the one
// owner of the tracker: every seat that needs the projection (azmcts's redeal
// source, sbsearch's redealer) reads it here, so there is no per-seat copy
// that could drift or survive a rebuild. An error is sticky and the feed's
// projection is dead for the rest of the game, exactly as KnownTracker.Update.
func (f *Feed) Known() (searchprobe.KnownCards, error) {
	return f.known.Update(f.h)
}

// Collector is the feed's actor-scoped collector (Choose takes it as an
// argument; it is never nil).
func (f *Feed) Collector() *searchprobe.Collector { return f.collector }

// LastFrame is the frame the most recent successful Observe appended -- the
// frame Choose scores the current decision against. It is valid only while
// the feed is live and has at least one frame; a caller guards with Live and
// Frames >= 1 (the driver only invokes a seat's search path on a live feed it
// just captured through).
func (f *Feed) LastFrame() searchprobe.Frame { return f.h.Frames[len(f.h.Frames)-1] }

// Frames is the frame count so far.
func (f *Feed) Frames() int { return len(f.h.Frames) }
