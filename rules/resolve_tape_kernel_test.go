package rules

// Kernel-era restorations of resolve_kernel_test.go's scenarios. The
// originals were dual runs (legacy resume machinery against the tape kernel,
// logs required identical); the kernel is now the only path, so each runs on
// the kernel alone and keeps its kernel-counter and game-outcome assertions.

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules/resolve"
	"github.com/adams-shaun/gorge/state"
)

// kr8Kernel runs scenario on a fresh kernel fixture, checks a log-only
// replay, and returns the engine and the kernel counter deltas.
func kr8Kernel(t *testing.T, seats int, seed uint64, scenario func(t *testing.T, e *Engine), srcs ...string) (*Engine, resolve.Stats) {
	t.Helper()
	before := resolve.ReadStats()
	e, cfg := kr8Fixture(t, seats, seed, srcs...)
	scenario(t, e)
	st := resolve.ReadStats().Sub(before)
	replayCheck(t, e, cfg)
	return e, st
}

// A single stop-ask: the ask is posed with the tape exhausted, the run
// unwinds, and the answer re-runs the resolution once, served from the tape;
// the rider after it runs.
func TestKr8ChooseColorStopAsk(t *testing.T) {
	var life int32
	e, st := kr8Kernel(t, 2, 9101, func(t *testing.T, e *Engine) {
		life = e.G.Players[0].Life
		tapeCastAndResolve(t, e, "Tape Wash", "U")
	}, tapeChooseColorSrc)
	if st.Served != 1 || st.Posed != 1 || st.Reruns != 1 {
		t.Fatalf("the tape path did not serve the ChooseColor ask: %+v", st)
	}
	if got := e.G.Players[0].Life - life; got != 2 {
		t.Fatalf("life change %d, want 2", got)
	}
}

// A loop with per-iteration asks: k voters cost one first-run pose, then k
// re-runs serving 1+2+...+k answers; the vote's outcome gains life.
func TestKr8VoteLoop(t *testing.T) {
	for _, seats := range []int{2, 4, 8} {
		t.Run(fmt.Sprintf("seats%d", seats), func(t *testing.T) {
			var life int32
			e, st := kr8Kernel(t, seats, 9200+uint64(seats), func(t *testing.T, e *Engine) {
				life = e.G.Players[0].Life
				tapeCastAndResolve(t, e, "Tape Council", "B")
			}, tapeVoteSrc())
			k := int64(seats)
			if st.Posed != 1 || st.Reruns != k || st.Served != k*(k+1)/2 {
				t.Fatalf("vote over %d seats: %+v", seats, st)
			}
			if got := e.G.Players[0].Life - life; got != 1 && got != 3 {
				t.Fatalf("life change %d, want one ballot's 1 or 3", got)
			}
		})
	}
}

// The card ballot and the player ballot are served from the tape the same
// way: one answer per voter, re-run per voter.
func TestKr8CardAndPlayerBallots(t *testing.T) {
	for _, seats := range []int{2, 4} {
		t.Run(fmt.Sprintf("card%d", seats), func(t *testing.T) {
			var bear, angel state.ObjID
			e, st := kr8Kernel(t, seats, 9950+uint64(seats), func(t *testing.T, e *Engine) {
				bear = moveByName(t, e, 0, "ParentLink Bear", state.ZBattlefield)
				angel = moveByName(t, e, 0, "ParentLink Angel", state.ZBattlefield)
				tapeCastAndResolve(t, e, "Tape Judgment", "W")
			}, tapeCardVoteSrc, ptResumeBearSrc, ptResumeAngelSrc)
			if st.Served != int64(seats*(seats+1)/2) {
				t.Fatalf("card ballot: %+v", st)
			}
			if e.G.Obj(bear).Zone != state.ZExile && e.G.Obj(angel).Zone != state.ZExile {
				t.Fatal("the card ballot exiled neither candidate")
			}
		})
		t.Run(fmt.Sprintf("player%d", seats), func(t *testing.T) {
			_, st := kr8Kernel(t, seats, 9960+uint64(seats), func(t *testing.T, e *Engine) {
				tapeCastAndResolve(t, e, "Tape Verdict", "R")
			}, tapePlayerVoteSrc)
			if st.Served != int64(seats*(seats+1)/2) {
				t.Fatalf("player ballot: %+v", st)
			}
		})
	}
}

// The c21c390de scenario: a root target, a targeting link, an intervening
// TgtChoose discard and a ParentTarget reader; ParentTarget reads the right
// link after the served discard.
func TestKr8ParentTargetChain(t *testing.T) {
	var bear, angel state.ObjID
	e, st := kr8Kernel(t, 2, 9301, func(t *testing.T, e *Engine) {
		bear = moveByName(t, e, 0, "ParentLink Bear", state.ZBattlefield)
		angel = moveByName(t, e, 0, "ParentLink Angel", state.ZBattlefield)
		spell := fixtureInHand(t, e, "Parent Link Between")
		addMana(t, e, 0, "R")
		submitChoices(t, e, castOptionFor(t, e, spell).Index)
		driveParentLink(t, e, bear, angel)
	}, parentTargetBetweenScript(), ptResumeBearSrc, ptResumeAngelSrc)
	if !e.HasKeyword(angel, "Flying") || e.HasKeyword(bear, "Flying") {
		t.Fatal("ParentTarget read the wrong link under the tape kernel")
	}
	if st.Served != 1 {
		t.Fatalf("the discard ask was not served from the tape: %+v", st)
	}
}

// Every player discards a card of their choice (TgtChoose over Defined$
// Player): the per-player asks are served and every hand shrinks by one.
func TestKr8EachPlayerDiscardLoop(t *testing.T) {
	e, st := kr8Kernel(t, 4, 9701, func(t *testing.T, e *Engine) {
		tapeCastAndResolve(t, e, "Tape Each Discard", "B")
	}, tapeEachDiscardSrc)
	if st.Served == 0 {
		t.Fatalf("no discard ask was served from the tape: %+v", st)
	}
	var discards [4]int
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.Text == "discarded" {
			discards[e.G.Obj(ev.Obj).Owner]++
		}
	}
	if discards != [4]int{1, 1, 1, 1} {
		t.Fatalf("discards per seat %v, want one each", discards)
	}
}

// A clone of a posed tape resolution shares S0: answering the clone re-runs
// from the same checkpoint and leaves the original untouched, and both end
// identical.
func TestKr8CloneAtPosedDecision(t *testing.T) {
	e, _ := kr8Fixture(t, 4, 9801, tapeVoteSrc())
	addMana(t, e, 0, "B")
	submitChoices(t, e, castOptionFor(t, e, fixtureInHand(t, e, "Tape Council")).Index)
	for e.Pending().Kind == decision.KPriority {
		submitChoices(t, e, tapePassIndex(e.Pending()))
	}
	if !TapePosed(e) {
		t.Fatal("precondition: no tape resolution posed")
	}
	c := e.Clone()
	if !TapePosed(c) {
		t.Fatal("the clone lost the posed checkpoint")
	}
	headBefore, nBefore := e.L.Head(), len(e.L.Events)
	tapeResolveAll(t, c)
	if e.L.Head() != headBefore || len(e.L.Events) != nBefore || !TapePosed(e) {
		t.Fatal("answering the clone changed the original")
	}
	tapeResolveAll(t, e)
	tapeRequireSameLog(t, c, e)
}

// An ask-free resolution skips the checkpoint; one that asks still takes it
// and is served from the tape.
func TestKr8MayAskExemptsAskFreeResolutions(t *testing.T) {
	_, st := kr8Kernel(t, 2, 9201, func(t *testing.T, e *Engine) {
		tapeCastAndResolve(t, e, "Tape Gain", "W")
	}, tapeNoAskSrc)
	if st.Checkpoints != 0 || st.Exempt != 1 || st.Misses != 0 {
		t.Fatalf("the ask-free spell was not exempted: %+v", st)
	}
	_, st = kr8Kernel(t, 2, 9202, func(t *testing.T, e *Engine) {
		tapeCastAndResolve(t, e, "Tape Wash", "U")
	}, tapeChooseColorSrc)
	if st.Checkpoints != 1 || st.Served != 1 {
		t.Fatalf("the asking spell lost its checkpoint: %+v", st)
	}
}

// A predicate miss (forced here by exempting everything): the resolution
// takes no checkpoint, so its ask reaches Engine.Ask with no run serving it.
// It is refused with one replay-visible Note and the asking primitive takes
// its deterministic default (the R-9 degrade), reported to the legacy-ask
// observer; the spell still finishes resolving.
func TestKr8MayAskMissDegradesInPlace(t *testing.T) {
	tapeExemptAll = true
	t.Cleanup(func() { tapeExemptAll = false })
	var classes []string
	prev := SetTapeLegacyObserver(func(class string) { classes = append(classes, class) })
	t.Cleanup(func() { SetTapeLegacyObserver(prev) })
	var life int32
	e, st := kr8Kernel(t, 2, 9203, func(t *testing.T, e *Engine) {
		life = e.G.Players[0].Life
		tapeCastAndResolve(t, e, "Tape Wash", "U")
	}, tapeChooseColorSrc)
	if st.Checkpoints != 0 || st.Exempt != 1 || st.Served != 0 {
		t.Fatalf("the forced exemption did not hold: %+v", st)
	}
	if len(classes) != 1 || !strings.HasPrefix(classes[0], "switch choose/choosecolor") {
		t.Fatalf("legacy-ask classes %q", classes)
	}
	if !hasNote(e, "ask answered with its default: no resolution run serves it (choose/choosecolor)") {
		t.Fatal("the unserved ask left no Note")
	}
	chose := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.Choose && ev.Counter == "color" {
			chose = true
		}
	}
	if !chose {
		t.Fatal("the default colour was not chosen")
	}
	if got := e.G.Players[0].Life - life; got != 2 {
		t.Fatalf("life change %d, want 2 (the rider still runs)", got)
	}
}

// Spec §7.3: a hypothetical world forked at a posed tape resolution
// (CloneHypothetical plus a redeal of the caster's hidden zones) re-runs
// without a prefix divergence, and what it does after the fork reads the
// REDEALT world -- the card drawn after the vote is the redealt library's
// top -- while the real engine draws the true top.
func TestKr8HypotheticalRedealWorld(t *testing.T) {
	e, _ := kr8Fixture(t, 2, 9901, tapeDrawVoteSrc,
		"Name:Tape Filler A\nManaCost:1\nTypes:Artifact\nOracle:x\n",
		"Name:Tape Filler B\nManaCost:2\nTypes:Artifact\nOracle:x\n",
		"Name:Tape Filler C\nManaCost:3\nTypes:Artifact\nOracle:x\n")
	moveByName(t, e, 0, "Tape Filler A", state.ZLibrary)
	moveByName(t, e, 0, "Tape Filler B", state.ZLibrary)
	moveByName(t, e, 0, "Tape Filler C", state.ZLibrary)
	addMana(t, e, 0, "B")
	submitChoices(t, e, castOptionFor(t, e, fixtureInHand(t, e, "Tape Draw Vote")).Index)
	for i := 0; i < 20; i++ {
		d := e.Pending()
		if d.Kind == decision.KChoose && d.Player == 1 {
			break
		}
		if d.Kind == decision.KPriority {
			submitChoices(t, e, tapePassIndex(d))
			continue
		}
		submitChoices(t, e, 0)
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Player != 1 {
		t.Fatalf("precondition: seat 1's vote not posed: %+v", d)
	}
	if !TapePosed(e) {
		t.Fatal("precondition: no tape resolution posed")
	}
	trueHead := e.L.Head()
	w := e.CloneHypothetical(0xfeed)
	tapeTestRedeal(w, 0, 77)
	wantHand := append([]state.ObjID(nil), w.G.Zone(state.ZHand, 0)...)
	wantTop := w.G.Zone(state.ZLibrary, 0)[0]
	trueTop := e.G.Zone(state.ZLibrary, 0)[0]
	if wantTop == trueTop {
		t.Fatal("precondition: the redeal kept the true top card")
	}
	if err := w.SubmitHypothetical(decision.Intent{Seq: d.Seq, Player: 1, Choices: []int{1}}); err != nil {
		t.Fatalf("world submit: %v", err)
	}
	if e.L.Head() != trueHead {
		t.Fatal("the world's re-run touched the real engine")
	}
	gotHand := w.G.Zone(state.ZHand, 0)
	if len(gotHand) != len(wantHand)+1 || gotHand[len(gotHand)-1] != wantTop {
		t.Fatalf("post-fork draw read %v, want the redealt top %d (true top %d)", gotHand, wantTop, trueTop)
	}
	submitChoices(t, e, 1)
	if h := e.G.Zone(state.ZHand, 0); h[len(h)-1] != trueTop {
		t.Fatalf("real engine drew %d, want %d", h[len(h)-1], trueTop)
	}
}

// TestHeads with every resolution checkpointed (the restore and drop paths at
// full rate) must still produce the pinned golden heads. Not parallel: it
// flips a process-wide default.
func TestKr8HeadsCheckpointAll(t *testing.T) {
	if testing.Short() {
		t.Skip("long")
	}
	reg := testutil.CorpusRegistry(t)
	tapeCheckpointAll = true
	defer func() { tapeCheckpointAll = false }()
	before := resolve.ReadStats()
	for _, seats := range AcceptanceSeatCounts() {
		if got, want := acceptanceHead(t, reg, seats), pinnedHead(t, seats); got != want {
			t.Errorf("%d seats: checkpoint-all head %s, golden %s", seats, got, want)
		}
	}
	st := resolve.ReadStats().Sub(before)
	if st.Checkpoints == 0 {
		t.Fatalf("the head games never took a checkpoint: %+v", st)
	}
}

// §7.3 at scale: the clone-fidelity fuzz, and at every posed tape decision a
// hypothetical world (CloneHypothetical plus a redeal of every other seat's
// hand and library) driven a few random intents. A prefix divergence panics.
// GORGE_TAPE_WORLD_GAMES sets the game count (default 6).
func TestKr8WorldsInFuzzGames(t *testing.T) {
	if testing.Short() {
		t.Skip("long")
	}
	reg := testutil.CorpusRegistry(t)
	var worlds, steps int
	cloneFuzzTapeWorldHook = func(t *testing.T, e *Engine, r *rand.Rand) {
		if !TapePosed(e) {
			return
		}
		d := e.Pending()
		w := e.CloneHypothetical(r.Uint64())
		for p := range w.G.Players {
			if state.PlayerID(p) != d.Player {
				tapeTestRedeal(w, state.PlayerID(p), r.Uint64())
			}
		}
		worlds++
		for i := 0; i < 6 && !w.G.Over && w.Pending() != nil; i++ {
			wd := w.Pending()
			for try := 0; try < 8; try++ {
				if err := w.SubmitHypothetical(cloneFuzzRandomIntent(wd, r)); err == nil {
					steps++
					break
				}
			}
		}
	}
	defer func() { cloneFuzzTapeWorldHook = nil }()
	var st cloneFuzzStats
	o := cloneFuzzOpts{every: 4, lockstep: 4, diverge: 4, randomPct: 10}
	games := cloneFuzzEnvInt("GORGE_TAPE_WORLD_GAMES", 6)
	for g := 0; g < games; g++ {
		cfg, label := cloneFuzzConfig(t, reg, g)
		playCloneFuzzGame(t, cfg, label, o, &st)
	}
	if worlds == 0 {
		t.Fatal("no posed tape decision was reached: the world probe measured nothing")
	}
	t.Logf("%d games, %d intents; %d tape worlds, %d world intents", st.games.Load(), st.intents.Load(), worlds, steps)
}
