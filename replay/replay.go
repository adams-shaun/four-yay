// Package replay re-executes a match from its recorded event log and
// verifies the rebuilt event stream against the one on file, naming the
// first sequence number where they part ways.
//
// The contract is (Config, Log) together, never the Log alone (Rulings P5,
// T11-d). Genesis -- state.NewGame and the per-deck AddObject calls in
// rules.New -- runs before the log exists and legitimately bypasses events
// (see rules/engine.go's own package doc), so the log can never recover
// deck contents or player names on its own: a faithful replay needs the
// original Config's Names and Decks (the same *cards.Card pointers, in the
// same order) supplied again alongside the Log. Replay and ReplayTo both
// overwrite cfg.Seed with l.Seed before running -- the log's seed is the
// one that produced it, and a caller passing a mismatched cfg.Seed would
// only be replaying a different match by accident.
//
// Everything from the first logged event onward -- turn structure, zone
// moves, life, damage, priority, and every Intent a client ever answered
// with -- is re-derived by running the real rules.Engine against the
// recorded Intents in order, not by reapplying logged events directly (that
// is rules/trigger_test.go's replayFromLog, a rules-internal fidelity
// harness this package does not use or duplicate). Because every state
// change already goes through one path (events.Emit), an engine that is
// unchanged since the log was recorded reproduces it byte for byte; an
// engine that has changed either rejects a recorded Intent outright (a
// plain error -- see run below) or produces an event stream that diverges
// from the recording, reported as a *Divergence naming the first differing
// event.
package replay

import (
	"fmt"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules"
)

// Divergence is returned by Replay (and can be returned by ReplayTo) when
// the rebuilt event stream does not match the recorded one. It is the
// specific diagnosis Ruling P3 asks for: "the engine changed under this
// log" as a one-line answer, not an investigation.
type Divergence struct {
	// Seq is the index of the first event that differs. Every stored event's
	// own Seq equals its slice index (events.Log.Append assigns it that way),
	// so this doubles as an index into both event slices when Missing is
	// false.
	Seq uint64
	// Want and Got are the recorded and replayed events at Seq. They are the
	// zero events.Event (Kind GameStart) when Missing is true; use Missing,
	// never a zero-value check on Want, to tell the two cases apart.
	Want, Got events.Event
	// Missing is true when the replay produced an event at Seq but the
	// recorded log has nothing there at all -- the replay ran further than
	// the recording did. Want is meaningless in that case; Error says so in
	// words instead of printing a zero events.Event (whose Kind reads
	// "game_start" and would mislead) as if it were a real recorded event.
	Missing bool
	// Short is Missing's mirror image (M12, final whole-branch review): true
	// when every event the replay DID produce matched the recording byte for
	// byte, but the recorded log has more events at the end that the replay,
	// having run out of recorded Intents to submit, never reached. Got is
	// meaningless in that case, for the same reason Want is meaningless when
	// Missing is true -- there is nothing the replay actually produced at
	// Seq to name.
	Short bool
}

// Error names the first differing event. Ruling P3: a missing counterpart in
// the recorded log gets its own wording, not a misleading zero-value event.
func (d *Divergence) Error() string {
	if d.Missing {
		return fmt.Sprintf("replay diverged at event %d: recorded log ends at event %d; replay produced %s",
			d.Seq, d.Seq, d.Got.Kind)
	}
	if d.Short {
		return fmt.Sprintf("replay diverged at event %d: replay ended there; the recorded log continues with %s",
			d.Seq, d.Want.Kind)
	}
	return fmt.Sprintf("replay diverged at event %d: recorded %s, replayed %s",
		d.Seq, d.Want.Kind, d.Got.Kind)
}

// Replay rebuilds the whole match named by (cfg, l) and verifies it against
// l: every recorded Intent is submitted, in order, to a freshly-created
// engine, and the rebuilt event stream is compared against l.Events as it
// is produced (Ruling P3) rather than only once at the end. On success the
// returned engine's log has the same events, in the same order, as l, and a
// chain Head equal to l.HeadAt(len(l.Events)) -- l.Head() itself only if l
// accumulated that chain live in this process, see Ruling T24-a below; the
// returned error is nil.
//
// A *Divergence names the first event where the two streams part ways,
// including the case where the replay produced more events than were ever
// recorded (Missing true). An Intent the engine would not have asked for --
// a Seq the current pending decision does not match, an out-of-range or
// duplicate choice, the wrong player, and so on -- is refused by
// decision.Decision.Validate before it can do anything; that surfaces as a
// plain wrapped error, not a Divergence, since nothing was compared (Ruling
// P2). A returned engine is never nil once a log was supplied; a nil log is
// a programming error and returns (nil, error) instead (Ruling T24-b, fix
// round 1: this sentence used to promise a non-nil engine unconditionally,
// which the l == nil guard below directly contradicted). Otherwise the
// engine reflects however far the replay got before it stopped; a caller
// may inspect its Game and Log freely, but should not expect further Submit
// calls against it to mean anything once an error has been returned.
func Replay(l *events.Log, cfg rules.Config) (*rules.Engine, error) {
	if l == nil {
		return nil, fmt.Errorf("replay: nil log")
	}
	e, err := run(l, cfg, len(l.Intents))
	if err != nil {
		return e, err
	}
	// Every event so far matched (run's own incremental compare already
	// guarantees that); the only way to still be wrong here is for the
	// recorded log to have MORE events than the replay ever produced -- the
	// mirror image of the Missing case above, and not something the
	// incremental per-Submit compare can see, since it only ever looks at
	// events the replay HAS produced.
	//
	// M12 (final whole-branch review): this used to return a plain
	// fmt.Errorf carrying neither a Seq nor a *Divergence, unlike compare's
	// Missing:true case just above it -- so a caller doing the ordinary
	// errors.As(err, &divergence) dance (cmd/mtgsim's printReplayOutcome is
	// exactly this) fell through to a bare "replay error: ..." instead of a
	// located divergence. Returning a *Divergence with Short:true (Missing's
	// mirror image: see the field's own doc) and Seq set to where the
	// replay stopped fixes that.
	if len(e.L.Events) != len(l.Events) {
		return e, &Divergence{Seq: uint64(len(e.L.Events)), Short: true, Want: l.Events[len(e.L.Events)]}
	}
	// Ruling T24-a (fix round 1, Important #1): l.Head() is wrong for any l
	// that did not accumulate its own chain live in this process. l.chain is
	// unexported with no json tag (events/log.go), while Seed, Events and
	// Intents all have one -- so a Log that came off disk or the wire has a
	// zero chain, and Head() reports the seedless zero hash even when every
	// event matches the replay byte for byte. l.HeadAt(len(l.Events))
	// recomputes the whole chain from Seed and Events alone, which DO
	// round-trip; HeadAt's own doc calls this "what makes 'playback to N'
	// verifiable against a full log", and events/log_test.go already pins
	// Head() == HeadAt(len(Events)) for an in-memory log, so nothing changes
	// for a log built and replayed in the same process.
	//
	// A NoHash log tracks no chain at all: HeadAt short-circuits to "" for
	// one, and so does Head() (events/log.go), so there is nothing coherent
	// to compare -- e.L is always a live, hashing log regardless of l.NoHash
	// (rules.Config carries no NoHash field for New to propagate), so
	// comparing e.L.Head() against l's forced-empty HeadAt would fail every
	// NoHash log for a reason that has nothing to do with whether it
	// replayed correctly. Every event already matched byte for byte via the
	// incremental compare in run, and the count check just above already
	// covers the one failure mode a chain comparison could otherwise catch,
	// so skip it rather than fail it against an empty string that proves
	// nothing.
	if !l.NoHash {
		if got, want := e.L.Head(), l.HeadAt(len(l.Events)); got != want {
			return e, fmt.Errorf("replay: chain %s, recorded %s", got, want)
		}
	}
	return e, nil
}

// ReplayTo rebuilds state as of the first n recorded Intents and returns the
// engine positioned at whatever comes next -- the next pending decision, or
// a finished game if n's Intents ended it. "Playback to" and "resume from"
// are the same operation with a different n: the returned engine can be
// driven onward exactly like one built fresh, by calling Submit with a new
// Intent for its Pending() decision.
//
// n is clamped rather than rejected (Ruling P4's totality table): n < 0
// behaves as 0 (genesis and the events it logs, but no Intents applied),
// and n > len(l.Intents) behaves as len(l.Intents) (every recorded Intent,
// the same prefix Replay itself would run). Either way the result is a
// valid engine, never a panic.
//
// Like Replay, cfg.Seed is overwritten with l.Seed, and the incremental
// compare against l.Events (Ruling P3) still runs after every Advance and
// Submit, so a *Divergence can still surface here -- ReplayTo is Replay
// with an early stop, not a separate, unchecked code path.
func ReplayTo(l *events.Log, cfg rules.Config, n int) (*rules.Engine, error) {
	if l == nil {
		return nil, fmt.Errorf("replay: nil log")
	}
	return run(l, cfg, n)
}

// run drives a fresh engine through cfg's genesis and then n of l's recorded
// Intents (clamped to [0, len(l.Intents)]), comparing the engine's own
// growing event log against l's recorded events after every step that can
// add to it: the first Advance past genesis, and every subsequent Submit.
// Comparing incrementally, rather than once at the end, is Ruling P3: an
// intent altered early enough can make a LATER recorded intent invalid
// against the now-different game (Submit would then return a plain
// rejection, not a Divergence), so the only way to guarantee the FIRST
// actual divergence is reported is to check after every step, before
// feeding the next one.
func run(l *events.Log, cfg rules.Config, n int) (*rules.Engine, error) {
	if l == nil {
		return nil, fmt.Errorf("replay: nil log")
	}
	if n < 0 {
		n = 0
	} else if n > len(l.Intents) {
		n = len(l.Intents)
	}
	cfg.Seed = l.Seed // Ruling P5: the log's seed wins over the caller's.

	// CR 103.1's winner-chooses ask is posed only by a harness that can
	// answer it, so a log that recorded it carries a DecisionAsk event for
	// the kind. A log that asked is reconstructed through the choice
	// constructor (rules.NewStartingPlayerChoice), which defers the pregame
	// rounds the original run deferred, and the ask is re-posed here so the
	// recorded choice Intent lands on the decision it answered. A log that
	// never asked is reconstructed through plain rules.New -- byte-identical
	// to the run that produced it -- and advances through the R-9 fallback
	// exactly as the original run did.
	var e *rules.Engine
	asked := startingPlayerAsked(l)
	if asked {
		e = rules.NewStartingPlayerChoice(cfg)
	} else {
		e = rules.New(cfg)
	}
	if asked {
		// Re-pose the recorded ask, before Advance's R-9 fallback would
		// default it, so the recorded choice Intent lands on the decision it
		// answered. The re-pose emits the same DecisionAsk event at the same
		// log position the original run recorded.
		if e.AskStartingPlayer() == nil {
			return e, fmt.Errorf("replay: log recorded a starting_player ask but none is available")
		}
	}
	// Recorded payment selectors are replay-only consumers of the dormant
	// planner offers.  Normal engines continue to leave the extension empty
	// until the host publication ticket lands.
	e.EnablePaymentPlanReplay()
	e.Advance()
	checked, err := compare(e, l, 0)
	if err != nil {
		return e, err
	}

	for i := 0; i < n; i++ {
		if e.G.Over {
			break
		}
		if e.Pending() == nil {
			return e, fmt.Errorf("replay: no decision pending at intent %d", i)
		}
		// Ruling P2: submit the recorded Intent exactly as logged. Rewriting
		// its Seq to whatever the engine currently asks for (as the brief's
		// own reference implementation did) would silently repair the very
		// mismatch that means the engine changed underneath this log --
		// test 5's whole point is that such an Intent must be refused, not
		// quietly patched up and applied anyway.
		if err := e.Submit(l.Intents[i]); err != nil {
			return e, fmt.Errorf("replay: intent %d rejected: %w", i, err)
		}
		checked, err = compare(e, l, checked)
		if err != nil {
			return e, err
		}
	}
	return e, nil
}

// startingPlayerAsked reports whether l recorded the CR 103.1 winner-chooses
// ask (a DecisionAsk whose Text is the kind). Replay must pose exactly the
// decisions the original game posed, and this one is conditional, so the log
// is the only source of truth for whether it happened.
func startingPlayerAsked(l *events.Log) bool {
	if l == nil {
		return false
	}
	for _, ev := range l.Events {
		if ev.Kind == events.DecisionAsk && ev.Text == "starting_player" {
			return true
		}
	}
	return false
}

// compare checks e's own event log, from index checked onward, against l's
// recorded events at the same indices, byte for byte via Event.Append
// (Ruling P3) -- the same comparison events.Log's own Head chains, so two
// events that compare equal here always chain to the same Head, and two
// that do not can never accidentally chain to one. It returns how many
// events have now been verified (so the caller can resume from there next
// time) and the first *Divergence found, or a nil error if the whole of
// e.L.Events checked out.
func compare(e *rules.Engine, l *events.Log, checked int) (int, error) {
	got := e.L.Events
	for i := checked; i < len(got); i++ {
		if i >= len(l.Events) {
			return i, &Divergence{Seq: uint64(i), Missing: true, Got: got[i]}
		}
		if string(got[i].Append(nil)) != string(l.Events[i].Append(nil)) {
			return i, &Divergence{Seq: uint64(i), Want: l.Events[i], Got: got[i]}
		}
	}
	return len(got), nil
}
