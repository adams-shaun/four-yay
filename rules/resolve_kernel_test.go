package rules

// The resolution kernel's dual-run tests (W3 steps 0-1; lasagna spec §7.7):
// every scenario is driven twice by the same deterministic driver, once on
// the legacy resume machinery and once on the tape kernel (rules/resolve),
// and the two logs must be identical event for event and intent for intent,
// with the same head and RNG draw count. The kernel counters prove the tape
// path ran.

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules/resolve"
	"github.com/adams-shaun/gorge/state"
)

// tapeFixture is newFixtureDeck generalised to n seats: seat 0's library
// holds the fixture cards (moved to hand), every other card is a Mountain.
func tapeFixture(t *testing.T, seats int, seed uint64, tape bool, srcs ...string) (*Engine, Config) {
	t.Helper()
	var fixtures []*cards.Card
	for _, s := range srcs {
		fixtures = append(fixtures, card(t, s))
	}
	names := make([]string, seats)
	decks := make([][]*cards.Card, seats)
	for i := range names {
		names[i] = fmt.Sprintf("p%d", i)
		decks[i] = mountainDeck(t, 40)
	}
	decks[0] = append(append([]*cards.Card(nil), fixtures...), mountainDeck(t, 40-len(fixtures))...)
	cfg := seatZeroStart(Config{Seed: seed, Names: names, Decks: decks, Tokens: map[string]*cards.Card{}})
	cfg.LegacyResume = !tape
	// The legacy arm stays legacy even under GORGE_TAPE_KERNEL=1.
	prev := tapeKernelEnv
	tapeKernelEnv = tapeKernelEnv && tape
	e := New(cfg)
	tapeKernelEnv = prev
	e.Advance()
	for _, f := range fixtures {
		name := f.Faces[0].Name
		inHand := false
		for _, id := range e.G.Zone(state.ZHand, 0) {
			if o := e.G.Obj(id); o.Face().Name == name {
				inHand = true
			}
		}
		if !inHand {
			moveByName(t, e, 0, name, state.ZHand)
		}
	}
	return e, cfg
}

// tapePick is the deterministic answer policy: every non-priority decision
// takes options chosen from its sequence number, so the two runs answer
// identically exactly when they are posed identically.
func tapePick(d *decision.Decision) []int {
	if len(d.Options) == 0 {
		return nil
	}
	n := d.Max
	if n < 1 {
		n = 1
	}
	if n > len(d.Options) {
		n = len(d.Options)
	}
	if d.Min > n {
		n = d.Min
	}
	start := int(d.Seq) % len(d.Options)
	out := make([]int, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, (start+i)%len(d.Options))
	}
	return out
}

func tapePassIndex(d *decision.Decision) int {
	for _, o := range d.Options {
		if o.Kind == "pass" {
			return o.Index
		}
	}
	return -1
}

// tapeResolveAll passes priority and answers every other decision with
// tapePick until the stack is empty at a priority decision.
func tapeResolveAll(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 400; i++ {
		d := e.Pending()
		if d == nil || e.G.Over {
			return
		}
		if d.Kind == decision.KPriority {
			if len(e.G.Stack) == 0 {
				return
			}
			submitChoices(t, e, tapePassIndex(d))
			continue
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: tapePick(d)}); err != nil {
			t.Fatalf("submit %s %v: %v", d.Kind, tapePick(d), err)
		}
	}
	t.Fatal("stack never drained")
}

// tapeCastAndResolve funds and casts the named fixture, answers its cast
// asks with tapePick, then resolves everything.
func tapeCastAndResolve(t *testing.T, e *Engine, name, mana string) {
	t.Helper()
	addMana(t, e, 0, mana)
	id := fixtureInHand(t, e, name)
	submitChoices(t, e, castOptionFor(t, e, id).Index)
	for i := 0; i < 20; i++ {
		d := e.Pending()
		if d == nil || d.Kind == decision.KPriority {
			break
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: tapePick(d)}); err != nil {
			t.Fatalf("cast-time %s: %v", d.Kind, err)
		}
	}
	tapeResolveAll(t, e)
}

// tapeDual runs scenario on a legacy and a tape engine and requires the two
// logs to be identical; it returns the tape engine and the counter deltas.
func tapeDual(t *testing.T, seats int, seed uint64, scenario func(t *testing.T, e *Engine), srcs ...string) (*Engine, resolve.Stats) {
	t.Helper()
	legacy, _ := tapeFixture(t, seats, seed, false, srcs...)
	scenario(t, legacy)
	before := resolve.ReadStats()
	tape, cfg := tapeFixture(t, seats, seed, true, srcs...)
	scenario(t, tape)
	st := resolve.ReadStats().Sub(before)
	tapeRequireSameLog(t, legacy, tape)
	replayCheck(t, tape, cfg)
	t.Logf("kernel stats: %+v", st)
	return tape, st
}

func tapeRequireSameLog(t *testing.T, a, b *Engine) {
	t.Helper()
	n := min(len(a.L.Events), len(b.L.Events))
	var x, y []byte
	for i := 0; i < n; i++ {
		x = a.L.Events[i].Append(x[:0])
		y = b.L.Events[i].Append(y[:0])
		if string(x) != string(y) {
			t.Fatalf("event %d differs:\n legacy %s\n tape   %s", i, tapeEventString(a.L.Events[i]), tapeEventString(b.L.Events[i]))
		}
	}
	if len(a.L.Events) != len(b.L.Events) {
		t.Fatalf("event counts differ: legacy %d, tape %d", len(a.L.Events), len(b.L.Events))
	}
	if len(a.L.Intents) != len(b.L.Intents) {
		t.Fatalf("intent counts differ: legacy %d, tape %d", len(a.L.Intents), len(b.L.Intents))
	}
	if a.L.Head() != b.L.Head() || a.RNGDraws() != b.RNGDraws() {
		t.Fatalf("heads differ: legacy %s/%d, tape %s/%d", a.L.Head(), a.RNGDraws(), b.L.Head(), b.RNGDraws())
	}
}

func tapeEventString(ev events.Event) string {
	return fmt.Sprintf("{%v obj=%d p=%d amt=%d text=%q ctr=%q}", ev.Kind, ev.Obj, ev.Player, ev.Amount, ev.Text, ev.Counter)
}

const tapeChooseColorSrc = "Name:Tape Wash\nManaCost:U\nTypes:Sorcery\n" +
	"A:SP$ ChooseColor | Defined$ You | SubAbility$ DBGain\n" +
	"SVar:DBGain:DB$ GainLife | LifeAmount$ 2\nOracle:x\n"

const tapeNoAskSrc = "Name:Tape Gain\nManaCost:W\nTypes:Sorcery\nA:SP$ GainLife | LifeAmount$ 2\nOracle:x\n"

// A single stop-ask: the ask is posed with the tape exhausted, the run
// unwinds, and the answer re-runs the resolution once, served from the tape.
func TestTapeChooseColorStopAsk(t *testing.T) {
	_, st := tapeDual(t, 2, 9101, func(t *testing.T, e *Engine) {
		tapeCastAndResolve(t, e, "Tape Wash", "U")
	}, tapeChooseColorSrc)
	if st.Served != 1 || st.Posed != 1 || st.Reruns != 1 {
		t.Fatalf("the tape path did not serve the ChooseColor ask: %+v", st)
	}
}

func tapeVoteSrc() string {
	return "Name:Tape Council\nManaCost:B\nTypes:Sorcery\n" +
		"A:SP$ Vote | Defined$ Player | Choices$ DBGainA,DBGainB | VoteTiedAbility$ DBGainB\n" +
		"SVar:DBGainA:DB$ GainLife | LifeAmount$ 1 | Defined$ You\n" +
		"SVar:DBGainB:DB$ GainLife | LifeAmount$ 3 | Defined$ You\nOracle:x\n"
}

// A loop with per-iteration asks: k voters cost one first-run pose, then k
// re-runs serving 1+2+...+k answers (O(k^2) re-execution).
func TestTapeVoteLoop(t *testing.T) {
	for _, seats := range []int{2, 4, 8} {
		t.Run(fmt.Sprintf("seats%d", seats), func(t *testing.T) {
			_, st := tapeDual(t, seats, 9200+uint64(seats), func(t *testing.T, e *Engine) {
				tapeCastAndResolve(t, e, "Tape Council", "B")
			}, tapeVoteSrc())
			k := int64(seats)
			if st.Posed != 1 || st.Reruns != k || st.Served != k*(k+1)/2 {
				t.Fatalf("vote over %d seats: %+v", seats, st)
			}
		})
	}
}

const tapeCardVoteSrc = "Name:Tape Judgment\nManaCost:W\nTypes:Sorcery\n" +
	"A:SP$ Vote | Defined$ Player | VoteSubAbility$ DBExile | VoteCard$ Permanent.nonLand\n" +
	"SVar:DBExile:DB$ ChangeZone | Defined$ Remembered | Origin$ Battlefield | Destination$ Exile\nOracle:x\n"

const tapePlayerVoteSrc = "Name:Tape Verdict\nManaCost:R\nTypes:Sorcery\n" +
	"A:SP$ Vote | Defined$ Player | VotePlayer$ Other | StoreVoteNum$ True | SubAbility$ DBRepeat\n" +
	"SVar:DBRepeat:DB$ RepeatEach | RepeatPlayers$ Player | RepeatSubAbility$ DBDraw | AmountFromVotes$ True\n" +
	"SVar:DBDraw:DB$ Draw | Defined$ Remembered | NumCards$ Votes\nOracle:x\n"

// The two other ballot shapes (card ballot, player ballot) share the vote
// cursor; with all three converted the cursor is dead under the kernel.
func TestTapeCardAndPlayerBallots(t *testing.T) {
	for _, seats := range []int{2, 4} {
		t.Run(fmt.Sprintf("card%d", seats), func(t *testing.T) {
			_, st := tapeDual(t, seats, 9950+uint64(seats), func(t *testing.T, e *Engine) {
				moveByName(t, e, 0, "ParentLink Bear", state.ZBattlefield)
				moveByName(t, e, 0, "ParentLink Angel", state.ZBattlefield)
				tapeCastAndResolve(t, e, "Tape Judgment", "W")
			}, tapeCardVoteSrc, ptResumeBearSrc, ptResumeAngelSrc)
			if st.Served != int64(seats*(seats+1)/2) {
				t.Fatalf("card ballot: %+v", st)
			}
		})
		t.Run(fmt.Sprintf("player%d", seats), func(t *testing.T) {
			_, st := tapeDual(t, seats, 9960+uint64(seats), func(t *testing.T, e *Engine) {
				tapeCastAndResolve(t, e, "Tape Verdict", "R")
			}, tapePlayerVoteSrc)
			if st.Served != int64(seats*(seats+1)/2) {
				t.Fatalf("player ballot: %+v", st)
			}
		})
	}
}

// The c21c390de scenario: a root target, a targeting link, an intervening
// TgtChoose discard (a converted KModes stop-ask, whose answer carries the
// ModeChosen marker) and a ParentTarget reader.
func TestTapeParentTargetChain(t *testing.T) {
	var bear, angel state.ObjID
	tape, st := tapeDual(t, 2, 9301, func(t *testing.T, e *Engine) {
		bear = moveByName(t, e, 0, "ParentLink Bear", state.ZBattlefield)
		angel = moveByName(t, e, 0, "ParentLink Angel", state.ZBattlefield)
		spell := fixtureInHand(t, e, "Parent Link Between")
		addMana(t, e, 0, "R")
		submitChoices(t, e, castOptionFor(t, e, spell).Index)
		driveParentLink(t, e, bear, angel)
	}, parentTargetBetweenScript(), ptResumeBearSrc, ptResumeAngelSrc)
	if !tape.HasKeyword(angel, "Flying") || tape.HasKeyword(bear, "Flying") {
		t.Fatal("ParentTarget read the wrong link under the tape kernel")
	}
	if st.Served != 1 {
		t.Fatalf("the discard ask was not served from the tape: %+v", st)
	}
}

// A loop with per-iteration asks: every player discards a card of their
// choice (TgtChoose over Defined$ Player).
const tapeEachDiscardSrc = "Name:Tape Each Discard\nManaCost:B\nTypes:Sorcery\n" +
	"A:SP$ Discard | Defined$ Player | Mode$ TgtChoose | NumCards$ 1 | SubAbility$ DBGain\n" +
	"SVar:DBGain:DB$ GainLife | LifeAmount$ 2\nOracle:x\n"

func TestTapeEachPlayerDiscardLoop(t *testing.T) {
	_, st := tapeDual(t, 4, 9701, func(t *testing.T, e *Engine) {
		tapeCastAndResolve(t, e, "Tape Each Discard", "B")
	}, tapeEachDiscardSrc)
	if st.Served == 0 {
		t.Fatalf("no discard ask was served from the tape: %+v", st)
	}
}

// A converted ask then a legacy one (Scry's KArrange, held legacy): the re-run aborts and
// the legacy path replays the tape (§7.7 run-time fallback).
const tapeMixedSrc = "Name:Tape Mixed\nManaCost:U\nTypes:Sorcery\n" +
	"A:SP$ Discard | Defined$ You | Mode$ TgtChoose | NumCards$ 1 | SubAbility$ DBScry\n" +
	"SVar:DBScry:DB$ Scry | ScryNum$ 2\nOracle:x\n"

// tapeLegacyArrange keeps Scry's KArrange on the legacy path for a test (a
// converted site declines the tape through tapeForceLegacy).
func tapeLegacyArrange(t *testing.T) {
	tapeForceLegacy = func(d *decision.Decision) bool { return d.Kind == decision.KArrange }
	t.Cleanup(func() { tapeForceLegacy = nil })
}

func TestTapeAbortToLegacy(t *testing.T) {
	tapeLegacyArrange(t)
	_, st := tapeDual(t, 2, 9501, func(t *testing.T, e *Engine) {
		tapeCastAndResolve(t, e, "Tape Mixed", "U")
	}, tapeMixedSrc)
	if st.Aborts != 1 {
		t.Fatalf("expected one legacy-replay abort: %+v", st)
	}
}

// A legacy ask first: the kernel switches to legacy in place.
const tapeLegacyFirstSrc = "Name:Tape Legacy First\nManaCost:U\nTypes:Sorcery\n" +
	"A:SP$ Scry | ScryNum$ 2 | SubAbility$ DBColor\n" +
	"SVar:DBColor:DB$ ChooseColor | Defined$ You\nOracle:x\n"

func TestTapeLegacyFirstSwitch(t *testing.T) {
	tapeLegacyArrange(t)
	_, st := tapeDual(t, 2, 9601, func(t *testing.T, e *Engine) {
		tapeCastAndResolve(t, e, "Tape Legacy First", "U")
	}, tapeLegacyFirstSrc)
	if st.LegacySwitch != 1 || st.Served != 0 {
		t.Fatalf("expected an in-place legacy switch: %+v", st)
	}
}

// A clone of a posed tape resolution shares S0: answering the clone re-runs
// from the same checkpoint and leaves the original untouched, and both end
// identical.
func TestTapeCloneAtPosedDecision(t *testing.T) {
	e, _ := tapeFixture(t, 4, 9801, true, tapeVoteSrc())
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
// and is served from the tape; both stay byte-identical to legacy.
func TestTapeMayAskExemptsAskFreeResolutions(t *testing.T) {
	_, st := tapeDual(t, 2, 9201, func(t *testing.T, e *Engine) {
		tapeCastAndResolve(t, e, "Tape Gain", "W")
	}, tapeNoAskSrc)
	if st.Checkpoints != 0 || st.Exempt != 1 || st.Misses != 0 {
		t.Fatalf("the ask-free spell was not exempted: %+v", st)
	}
	_, st = tapeDual(t, 2, 9202, func(t *testing.T, e *Engine) {
		tapeCastAndResolve(t, e, "Tape Wash", "U")
	}, tapeChooseColorSrc)
	if st.Checkpoints != 1 || st.Served != 1 {
		t.Fatalf("the asking spell lost its checkpoint: %+v", st)
	}
}

// A predicate miss (an engine-posed as-enters or replacement-order choice
// the text walk cannot see is the canonical shape; here forced by exempting
// everything) is caught at Engine.ask, falls back to the legacy path in
// place, reaches the miss observer, and stays byte-identical.
func TestTapeMayAskMissFallsBackInPlace(t *testing.T) {
	tapeExemptAll = true
	t.Cleanup(func() { tapeExemptAll = false })
	var classes []string
	prev := SetTapeMissObserver(func(class string) { classes = append(classes, class) })
	t.Cleanup(func() { SetTapeMissObserver(prev) })
	_, st := tapeDual(t, 2, 9203, func(t *testing.T, e *Engine) {
		tapeCastAndResolve(t, e, "Tape Wash", "U")
	}, tapeChooseColorSrc)
	if st.Checkpoints != 0 || st.Misses != 1 {
		t.Fatalf("the forced miss was not caught: %+v", st)
	}
	if len(classes) != 1 || classes[0] != "spell:ChooseColor>GainLife -> choose/choosecolor" {
		t.Fatalf("miss classes %q", classes)
	}
}

// tapeTestRedeal re-deals player p's hand and library in a hypothetical
// world exactly the way searchprobe's redealPlayer does: Secret MoveZone and
// LibraryOrder events on the world's own log (events.Emit), here with every
// hidden card unknown.
func tapeTestRedeal(w *Engine, p state.PlayerID, seed uint64) {
	hand := append([]state.ObjID(nil), w.G.Zone(state.ZHand, p)...)
	lib := append([]state.ObjID(nil), w.G.Zone(state.ZLibrary, p)...)
	pool := append(append([]state.ObjID(nil), hand...), lib...)
	r := newRNG(seed)
	r.Shuffle(pool)
	for _, id := range hand {
		events.Emit(w.G, w.L, events.Event{Kind: events.MoveZone, Player: p, Obj: id, From: state.ZHand, To: state.ZLibrary, Secret: true})
	}
	for _, id := range pool[:len(hand)] {
		events.Emit(w.G, w.L, events.Event{Kind: events.MoveZone, Player: p, Obj: id, From: state.ZLibrary, To: state.ZHand, Secret: true})
	}
	events.Emit(w.G, w.L, events.Event{Kind: events.LibraryOrder, Player: p, IDs: pool[len(hand):], Secret: true})
}

const tapeDrawVoteSrc = "Name:Tape Draw Vote\nManaCost:B\nTypes:Sorcery\n" +
	"A:SP$ Draw | NumCards$ 2 | SubAbility$ DBVote\n" +
	"SVar:DBVote:DB$ Vote | Defined$ Player | Choices$ DBGainA,DBGainB | VoteTiedAbility$ DBGainB | SubAbility$ DBDraw\n" +
	"SVar:DBGainA:DB$ GainLife | LifeAmount$ 1 | Defined$ You\n" +
	"SVar:DBGainB:DB$ GainLife | LifeAmount$ 3 | Defined$ You\n" +
	"SVar:DBDraw:DB$ Draw | NumCards$ 1 | SubAbility$ DBShuffle\n" +
	"SVar:DBShuffle:DB$ Shuffle | Defined$ You\nOracle:x\n"

// Spec §7.3: a hypothetical world forked at a posed tape resolution
// (CloneHypothetical plus a redeal of the caster's hidden zones, from the
// voting opponent's point of view) re-runs without a prefix divergence, and
// what it does after the fork reads the REDEALT world -- the card the
// resolution draws after the vote is the redealt library's top, and the
// post-fork shuffle uses the world's generator, not S0's.
func TestTapeHypotheticalRedealWorld(t *testing.T) {
	for _, tape := range []bool{false, true} {
		t.Run(fmt.Sprintf("tape=%v", tape), func(t *testing.T) {
			e, _ := tapeFixture(t, 2, 9901, tape, tapeDrawVoteSrc,
				"Name:Tape Filler A\nManaCost:1\nTypes:Artifact\nOracle:x\n",
				"Name:Tape Filler B\nManaCost:2\nTypes:Artifact\nOracle:x\n",
				"Name:Tape Filler C\nManaCost:3\nTypes:Artifact\nOracle:x\n")
			moveByName(t, e, 0, "Tape Filler A", state.ZLibrary)
			moveByName(t, e, 0, "Tape Filler B", state.ZLibrary)
			moveByName(t, e, 0, "Tape Filler C", state.ZLibrary)
			addMana(t, e, 0, "B")
			submitChoices(t, e, castOptionFor(t, e, fixtureInHand(t, e, "Tape Draw Vote")).Index)
			// Pass until seat 1's vote is posed (seat 0 votes option 0).
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
			if tape && !TapePosed(e) {
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
			// The same answer on the real engine draws the TRUE top: the two
			// differ, so the world above did not read it.
			submitChoices(t, e, 1)
			if h := e.G.Zone(state.ZHand, 0); h[len(h)-1] != trueTop {
				t.Fatalf("real engine drew %d, want %d", h[len(h)-1], trueTop)
			}
		})
	}
}

// TestTapeHeads is the dual run's TestHeads leg: the pinned golden heads of
// the acceptance round-robin, played with every engine on the tape kernel,
// once with the ask-free predicate and once with every resolution
// checkpointed (the restore and drop paths at full rate). Not parallel: it
// flips process-wide defaults.
func TestTapeHeads(t *testing.T) {
	if testing.Short() {
		t.Skip("long")
	}
	reg := testutil.CorpusRegistry(t)
	prev := tapeKernelEnv
	tapeKernelEnv = true
	defer func() { tapeKernelEnv = prev }()
	for _, all := range []bool{false, true} {
		t.Run(fmt.Sprintf("checkpointAll=%v", all), func(t *testing.T) {
			tapeCheckpointAll = all
			defer func() { tapeCheckpointAll = false }()
			before := resolve.ReadStats()
			for _, seats := range AcceptanceSeatCounts() {
				if got, want := acceptanceHead(t, reg, seats), pinnedHead(t, seats); got != want {
					t.Errorf("%d seats: tape-kernel head %s, golden %s", seats, got, want)
				}
			}
			st := resolve.ReadStats().Sub(before)
			t.Logf("kernel stats over the head games: %+v", st)
			if st.Checkpoints+st.Exempt == 0 {
				t.Fatal("the head games never reached the kernel")
			}
		})
	}
}

// TestTapeWorldsInFuzzGames is §7.3 at scale: the clone-fidelity fuzz with
// every engine on the tape kernel, and at every posed tape decision a
// hypothetical world (CloneHypothetical plus a redeal of every other seat's
// hand and library) is driven a few random intents. A prefix divergence
// panics. GORGE_TAPE_WORLD_GAMES sets the game count (default 6).
func TestTapeWorldsInFuzzGames(t *testing.T) {
	if testing.Short() {
		t.Skip("long")
	}
	reg := testutil.CorpusRegistry(t)
	prev := tapeKernelEnv
	tapeKernelEnv = true
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
	defer func() { tapeKernelEnv = prev; cloneFuzzTapeWorldHook = nil }()
	before := resolve.ReadStats()
	var st cloneFuzzStats
	o := cloneFuzzOpts{every: 4, lockstep: 4, diverge: 4, randomPct: 10}
	games := cloneFuzzEnvInt("GORGE_TAPE_WORLD_GAMES", 6)
	for g := 0; g < games; g++ {
		cfg, label := cloneFuzzConfig(t, reg, g)
		playCloneFuzzGame(t, cfg, label, o, &st)
	}
	ks := resolve.ReadStats().Sub(before)
	t.Logf("%d games, %d intents, %d clones; %d tape worlds, %d world intents; kernel %+v",
		st.games.Load(), st.intents.Load(), st.clones.Load(), worlds, steps, ks)
}

// tapeLegacyOnly runs the calling test on the legacy resume path (opted out
// of the default kernel, as Config.LegacyResume does): the test pins that
// path's own machinery -- its suspension records, resume frames and park
// shapes -- which the kernel replaces. The process-wide default is switched
// for the test's duration, so the caller must not be parallel (top-level
// parallel tests run only after every sequential one has finished).
func tapeLegacyOnly(t *testing.T) {
	t.Helper()
	prev := tapeKernelEnv
	tapeKernelEnv = false
	t.Cleanup(func() { tapeKernelEnv = prev })
}
