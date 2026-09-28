package rules

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func chanceConfig(t *testing.T) Config {
	t.Helper()
	return Config{Seed: 73, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{mountainDeck(t, 12), mountainDeck(t, 12)}}
}

// Losing the opt-in boundary must not silently change ordinary replay.
func TestHypotheticalUnconditionedMatchesNormal(t *testing.T) {
	t.Parallel()
	cfg := chanceConfig(t)
	normal := New(cfg)
	hyp, err := NewHypothetical(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if hyp.L.Head() != normal.L.Head() || hyp.RNGDraws() != normal.RNGDraws() {
		t.Fatal("unconditioned hypothetical changed normal genesis")
	}
	if got := normal.ChanceTranscript(); got != nil {
		t.Fatal("normal game recorded hypothetical transcript")
	}
	if got := hyp.ChanceTranscript(); len(got) != 23 || got[0].Bound != 2 || got[1].Bound != 12 {
		t.Fatalf("missing toss or genesis shuffle draws: %v", got)
	}
}

// A forced shuffle must happen during genesis, not via fabricated later moves.
func TestHypotheticalForcesGenesisAndOwnsPrefix(t *testing.T) {
	t.Parallel()
	cfg := chanceConfig(t)
	prefix := []ChanceDraw{{Bound: 2, Value: 1}}
	for p := 0; p < 2; p++ {
		for n := 12; n > 1; n-- {
			prefix = append(prefix, ChanceDraw{Bound: n, Value: n - 1})
		}
	}
	e, err := NewHypothetical(cfg, prefix)
	if err != nil {
		t.Fatal(err)
	}
	if e.G.StartingPlayer != 1 {
		t.Fatalf("starter = %d", e.G.StartingPlayer)
	}
	// Identity permutations deal the first seven physical cards to each seat.
	if got := e.G.Zone(state.ZHand, 0); !reflect.DeepEqual(got, []state.ObjID{1, 2, 3, 4, 5, 6, 7}) {
		t.Fatalf("hand = %v", got)
	}
	prefix[0].Value = 0
	got := e.ChanceTranscript()
	if got[0].Value != 1 {
		t.Fatal("caller prefix aliases the transcript")
	}
	got[0].Value = 0
	if e.ChanceTranscript()[0].Value != 1 {
		t.Fatal("returned transcript aliases the engine")
	}
}

func TestHypotheticalPlannerControlsGenesisAndReplaysFromTranscript(t *testing.T) {
	t.Parallel()
	cfg := chanceConfig(t)
	calls := 0
	e, err := NewHypotheticalPlanned(cfg, []ChanceDraw{{Bound: 2, Value: 0}}, func(ctx ShuffleContext) ([]state.ObjID, error) {
		if ctx.Player != 0 {
			return nil, nil
		}
		if ctx.Ordinal != 0 || len(ctx.Hand) != 0 || len(ctx.Library) != 12 {
			return nil, fmt.Errorf("unexpected context: %+v", ctx)
		}
		calls++
		out := make([]state.ObjID, len(ctx.Library))
		for i, card := range ctx.Library {
			out[len(out)-1-i] = card.ID
		}
		return out, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("planner calls=%d want 1", calls)
	}
	if got, want := e.G.Zone(state.ZHand, 0), []state.ObjID{12, 11, 10, 9, 8, 7, 6}; !reflect.DeepEqual(got, want) {
		t.Fatalf("planned hand=%v want %v", got, want)
	}
	replay, err := NewHypothetical(cfg, e.ChanceTranscript())
	if err != nil {
		t.Fatal(err)
	}
	if replay.L.Head() != e.L.Head() || replay.RNGDraws() != e.RNGDraws() {
		t.Fatal("planned shuffle did not replay from chance transcript")
	}
	for n := 2; n < 42; n++ {
		if got, want := replay.Rand(n), e.Rand(n); got != want {
			t.Fatalf("forced shuffle did not advance PCG at bound %d: replay=%d engine=%d", n, got, want)
		}
	}
}

func TestHypotheticalPlannerRejectsNonPermutation(t *testing.T) {
	t.Parallel()
	cfg := chanceConfig(t)
	e, err := NewHypotheticalPlanned(cfg, []ChanceDraw{{Bound: 2, Value: 0}}, func(ctx ShuffleContext) ([]state.ObjID, error) {
		out := make([]state.ObjID, len(ctx.Library))
		return out, nil
	})
	if err == nil || e != nil || !strings.Contains(err.Error(), "permutation") {
		t.Fatalf("engine=%v error=%v", e != nil, err)
	}
}

func TestHypotheticalPlannerControlsEffectsShuffleAndCloneDropsPlanner(t *testing.T) {
	t.Parallel()
	cfg := chanceConfig(t)
	spell := card(t, "Name:Planned Shuffle\nManaCost:0\nTypes:Sorcery\nA:SP$ Shuffle | Defined$ You\nOracle:Test shuffle.\n")
	for i := range cfg.Decks[0] {
		cfg.Decks[0][i] = spell
	}
	var desired []state.ObjID
	laterCalls := 0
	e, err := NewHypotheticalPlanned(cfg, []ChanceDraw{{Bound: 2, Value: 0}}, func(ctx ShuffleContext) ([]state.ObjID, error) {
		if ctx.Player != 0 || ctx.Ordinal == 0 {
			return nil, nil
		}
		if ctx.Ordinal != 1 || len(ctx.Hand) != 6 || len(ctx.Library) != 5 {
			return nil, fmt.Errorf("later shuffle context: %+v", ctx)
		}
		for _, card := range append(append([]ShuffleCard(nil), ctx.Library...), ctx.Hand...) {
			if card.Name != "Planned Shuffle" {
				return nil, fmt.Errorf("unexpected card %q", card.Name)
			}
		}
		laterCalls++
		desired = make([]state.ObjID, len(ctx.Library))
		for i, card := range ctx.Library {
			desired[len(desired)-1-i] = card.ID
		}
		return desired, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.AdvanceHypothetical(); err != nil {
		t.Fatal(err)
	}
	clone := e.Clone()
	castFirstAndResolve(t, e)
	if laterCalls != 1 {
		t.Fatalf("later planner calls=%d want 1", laterCalls)
	}
	if got := lastShuffle(e.L.Events, 0); !reflect.DeepEqual(got, desired) {
		t.Fatalf("shuffle=%v want %v", got, desired)
	}
	castFirstAndResolve(t, clone)
	if laterCalls != 1 {
		t.Fatal("hypothetical clone retained proposal callback")
	}
	replay, err := NewHypothetical(cfg, e.ChanceTranscript())
	if err != nil {
		t.Fatal(err)
	}
	if err := replay.AdvanceHypothetical(); err != nil {
		t.Fatal(err)
	}
	for _, in := range e.L.Intents {
		if err := replay.SubmitHypothetical(in); err != nil {
			t.Fatal(err)
		}
	}
	if replay.L.Head() != e.L.Head() || replay.RNGDraws() != e.RNGDraws() {
		t.Fatal("later planned shuffle did not replay")
	}
}

func TestHypotheticalPlannerControlsMulliganShuffle(t *testing.T) {
	t.Parallel()
	cfg := chanceConfig(t)
	cfg.Mulligans = 1
	var desired []state.ObjID
	e, err := NewHypotheticalPlanned(cfg, []ChanceDraw{{Bound: 2, Value: 0}}, func(ctx ShuffleContext) ([]state.ObjID, error) {
		if ctx.Ordinal == 0 {
			return nil, nil
		}
		if len(ctx.Hand) != 0 || len(ctx.Library) != 12 {
			return nil, fmt.Errorf("mulligan shuffle context: %+v", ctx)
		}
		desired = make([]state.ObjID, len(ctx.Library))
		for i, card := range ctx.Library {
			desired[len(desired)-1-i] = card.ID
		}
		return desired, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.AdvanceHypothetical(); err != nil {
		t.Fatal(err)
	}
	d := e.Pending()
	choice := -1
	for i, option := range d.Options {
		if option.Kind == "mulligan" {
			choice = i
			break
		}
	}
	if choice < 0 {
		t.Fatalf("mulligan option missing: %+v", d)
	}
	mulliganer := d.Player
	if err := e.SubmitHypothetical(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{choice}}); err != nil {
		t.Fatal(err)
	}
	// The mulligan REDRAW is resolved at the pass boundary, not on the
	// declaring seat's own submit (CR 103.4/103.5: all mulligans in a
	// declaration pass happen simultaneously -- see resolveMulliganRedraws).
	// Keep every remaining seat so the pass completes and the deferred
	// redraw runs; then the planner's Ordinal-1 callback has produced the
	// shuffle this test asserts on.
	for desired == nil {
		if err := submitPregameKeep(t, e); err != nil {
			t.Fatal(err)
		}
	}
	if got := lastShuffle(e.L.Events, mulliganer); !reflect.DeepEqual(got, desired) {
		t.Fatalf("mulligan shuffle=%v want %v", got, desired)
	}
}

func TestHypotheticalPlannerEffectErrorPoisonsSubmit(t *testing.T) {
	t.Parallel()
	cfg := chanceConfig(t)
	spell := card(t, "Name:Error Shuffle\nManaCost:0\nTypes:Sorcery\nA:SP$ Shuffle | Defined$ You\nOracle:Test shuffle.\n")
	for i := range cfg.Decks[0] {
		cfg.Decks[0][i] = spell
	}
	want := errors.New("planner rejected effect shuffle")
	e, err := NewHypotheticalPlanned(cfg, []ChanceDraw{{Bound: 2, Value: 0}}, func(ctx ShuffleContext) ([]state.ObjID, error) {
		if ctx.Player == 0 && ctx.Ordinal == 1 {
			return nil, want
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.AdvanceHypothetical(); err != nil {
		t.Fatal(err)
	}
	cast := false
	for i := 0; i < 100; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("missing priority before effect shuffle")
		}
		choice := -1
		for _, option := range d.Options {
			if option.Kind == "pass" {
				choice = option.Index
			}
			if !cast && option.Kind == "cast" {
				choice = option.Index
				cast = true
				break
			}
		}
		if choice < 0 {
			t.Fatalf("missing cast/pass: %+v", d)
		}
		in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{choice}}
		err = e.SubmitHypothetical(in)
		if err == nil {
			continue
		}
		if !errors.Is(err, want) {
			t.Fatalf("SubmitHypothetical error=%v, want wrapped %v", err, want)
		}
		head := e.L.Head()
		if again := e.SubmitHypothetical(in); !errors.Is(again, want) {
			t.Fatalf("persisted chance error=%v, want wrapped %v", again, want)
		}
		if e.L.Head() != head {
			t.Fatal("poisoned effect-shuffle branch kept mutating")
		}
		return
	}
	t.Fatal("effect shuffle did not surface planner error")
}

func TestClearHypotheticalPlannerRestoresOrdinaryShuffleContinuation(t *testing.T) {
	t.Parallel()
	cfg := chanceConfig(t)
	spell := card(t, "Name:Cleared Shuffle\nManaCost:0\nTypes:Sorcery\nA:SP$ Shuffle | Defined$ You\nOracle:Test shuffle.\n")
	for i := range cfg.Decks[0] {
		cfg.Decks[0][i] = spell
	}
	calls := 0
	e, err := NewHypotheticalPlanned(cfg, []ChanceDraw{{Bound: 2, Value: 0}}, func(ShuffleContext) ([]state.ObjID, error) {
		calls++
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	genesis := e.ChanceTranscript()
	baseline, err := NewHypothetical(cfg, genesis)
	if err != nil {
		t.Fatal(err)
	}
	e.ClearHypotheticalPlanner()
	if err := e.AdvanceHypothetical(); err != nil {
		t.Fatal(err)
	}
	if err := baseline.AdvanceHypothetical(); err != nil {
		t.Fatal(err)
	}
	castFirstAndResolve(t, e)
	castFirstAndResolve(t, baseline)
	if calls != 2 {
		t.Fatalf("planner calls=%d want genesis-only 2", calls)
	}
	if e.L.Head() != baseline.L.Head() || e.RNGDraws() != baseline.RNGDraws() ||
		!reflect.DeepEqual(e.ChanceTranscript(), baseline.ChanceTranscript()) {
		t.Fatal("clearing planner changed ordinary shuffle continuation")
	}
}

func castFirstAndResolve(t *testing.T, e *Engine) {
	t.Helper()
	cast := false
	shuffles := shuffleCount(e.L.Events, 0)
	for i := 0; i < 100; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("missing priority")
		}
		choice := -1
		for j, option := range d.Options {
			if option.Kind == "pass" {
				choice = j
			}
			if !cast && option.Kind == "cast" {
				choice = j
				cast = true
				break
			}
		}
		if choice < 0 {
			t.Fatal("missing cast/pass")
		}
		if err := e.SubmitHypothetical(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{choice}}); err != nil {
			t.Fatal(err)
		}
		if cast && shuffleCount(e.L.Events, 0) > shuffles {
			return
		}
	}
	t.Fatal("shuffle spell did not resolve")
}

func shuffleCount(log []events.Event, player state.PlayerID) int {
	n := 0
	for _, ev := range log {
		if ev.Kind == events.Shuffle && ev.Player == player {
			n++
		}
	}
	return n
}

func lastShuffle(log []events.Event, player state.PlayerID) []state.ObjID {
	for i := len(log) - 1; i >= 0; i-- {
		if log[i].Kind == events.Shuffle && log[i].Player == player {
			return log[i].IDs
		}
	}
	return nil
}

// submitPregameKeep answers the pending pregame decision with its "keep"
// option. A mulligan's REDRAW is resolved at the declaration-pass boundary
// (CR 103.4/103.5; resolveMulliganRedraws), not on the declaring seat's own
// submit, so a test that observes the redraw -- its shuffle, its chance
// consumption, or a chance failure inside it -- must drive the rest of the
// pass to completion by keeping every remaining declarer.
func submitPregameKeep(t *testing.T, e *Engine) error {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("pregame ended before a keep was available")
	}
	keep := -1
	for i, option := range d.Options {
		if option.Kind == "keep" {
			keep = i
			break
		}
	}
	if keep < 0 {
		t.Fatalf("keep option missing: %+v", d)
	}
	return e.SubmitHypothetical(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{keep}})
}

func TestHypotheticalRejectsMalformedChance(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		draw ChanceDraw
		want string
	}{
		{"zero bound", ChanceDraw{Bound: 0, Value: 0}, "bound"},
		{"negative value", ChanceDraw{Bound: 2, Value: -1}, "value"},
		{"large value", ChanceDraw{Bound: 2, Value: 2}, "value"},
		{"wrong site", ChanceDraw{Bound: 3, Value: 1}, "bound"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, err := NewHypothetical(chanceConfig(t), []ChanceDraw{tc.draw})
			if err == nil || !strings.Contains(err.Error(), tc.want) || e != nil {
				t.Fatalf("got engine=%v error=%v", e != nil, err)
			}
		})
	}
}

func TestHypotheticalReplayAndCloneOwnChanceState(t *testing.T) {
	t.Parallel()
	cfg := chanceConfig(t)
	cfg.Mulligans = 1
	e, err := NewHypothetical(cfg, []ChanceDraw{{Bound: 2, Value: 0}})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.AdvanceHypothetical(); err != nil {
		t.Fatal(err)
	}
	clone := e.Clone()
	d := clone.Pending()
	choice := -1
	for i, o := range d.Options {
		if o.Kind == "mulligan" {
			choice = i
		}
	}
	if choice < 0 {
		t.Fatalf("no mulligan: %+v", d)
	}
	intent := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{choice}}
	head, draws := e.L.Head(), e.RNGDraws()
	if err := clone.SubmitHypothetical(intent); err != nil {
		t.Fatal(err)
	}
	// The mulligan redraw is deferred to the pass boundary (CR 103.4/103.5),
	// so its chance use lands when the rest of the pass keeps, not on the
	// mulligan submit. e must still be untouched after the clone's submit.
	if e.L.Head() != head || e.RNGDraws() != draws || len(e.ChanceTranscript()) != int(draws) {
		t.Fatal("clone changed source log or chance state")
	}
	if err := submitPregameKeep(t, clone); err != nil {
		t.Fatal(err)
	}
	if clone.RNGDraws() <= draws {
		t.Fatal("mulligan did not consume chance")
	}
	if err := e.SubmitHypothetical(intent); err != nil {
		t.Fatal(err)
	}
	if err := submitPregameKeep(t, e); err != nil {
		t.Fatal(err)
	}
	if e.L.Head() != clone.L.Head() || !reflect.DeepEqual(e.ChanceTranscript(), clone.ChanceTranscript()) {
		t.Fatal("clone lost chance position")
	}
	replay, err := NewHypothetical(cfg, e.ChanceTranscript())
	if err != nil {
		t.Fatal(err)
	}
	if err := replay.AdvanceHypothetical(); err != nil {
		t.Fatal(err)
	}
	if err := replay.SubmitHypothetical(intent); err != nil {
		t.Fatal(err)
	}
	if err := submitPregameKeep(t, replay); err != nil {
		t.Fatal(err)
	}
	if replay.L.Head() != e.L.Head() || replay.RNGDraws() != e.RNGDraws() {
		t.Fatal("chance+intent replay diverged")
	}
}

func TestHypotheticalSubmitFailurePoisonsOnlyThatBranch(t *testing.T) {
	t.Parallel()
	cfg := chanceConfig(t)
	cfg.Mulligans = 1
	base, err := NewHypothetical(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := base.AdvanceHypothetical(); err != nil {
		t.Fatal(err)
	}
	prefix := append(base.ChanceTranscript(), ChanceDraw{Bound: 999, Value: 0})
	e, err := NewHypothetical(cfg, prefix)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.AdvanceHypothetical(); err != nil {
		t.Fatal(err)
	}
	d := e.Pending()
	choice := -1
	for i, o := range d.Options {
		if o.Kind == "mulligan" {
			choice = i
		}
	}
	if choice < 0 {
		t.Fatal("missing mulligan")
	}
	in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{choice}}
	// The REDRAW that consumes the poisoned draw is deferred to the pass
	// boundary (CR 103.4/103.5), so the mulligan submit itself succeeds and
	// the chance failure surfaces when the pass's other declarer keeps --
	// that submit runs resolveMulliganRedraws, whose shuffle consumes the
	// invalid draw.
	if err := e.SubmitHypothetical(in); err != nil {
		t.Fatal(err)
	}
	if err := submitPregameKeep(t, e); err == nil || !strings.Contains(err.Error(), "bound") {
		t.Fatalf("error = %v", err)
	}
	head := e.L.Head()
	if err := e.SubmitHypothetical(in); err == nil {
		t.Fatal("accepted input after chance failure")
	}
	if head != e.L.Head() {
		t.Fatal("failed branch kept mutating")
	}
	if err := base.SubmitHypothetical(in); err != nil {
		t.Fatal(err)
	}
	if err := submitPregameKeep(t, base); err != nil {
		t.Fatal(err)
	}
	if err := New(cfg).SubmitHypothetical(in); err == nil {
		t.Fatal("hypothetical entry accepted a normal engine")
	}
}

// A full tape must restore not just the recorded prefix but the same seeded
// continuation. Skipping the PCG advance at forced draws breaks that property.
func TestHypotheticalFullTapePreservesRandomContinuation(t *testing.T) {
	t.Parallel()
	cfg := chanceConfig(t)
	original, err := NewHypothetical(cfg, []ChanceDraw{{Bound: 2, Value: 1}})
	if err != nil {
		t.Fatal(err)
	}
	replay, err := NewHypothetical(cfg, original.ChanceTranscript())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 40; i++ {
		if a, b := original.Rand(1000+i), replay.Rand(1000+i); a != b {
			t.Fatalf("continuation %d: %d != %d", i, a, b)
		}
	}
}

// Exercise Host.Rand inside a real resolving Shuffle spell, not only the
// engine's separate genesis/mulligan Fisher-Yates loops.
func TestHypotheticalEffectsShuffleReplaysAndRejectsWrongBound(t *testing.T) {
	t.Parallel()
	cfg := chanceConfig(t)
	spell := card(t, "Name:Hypothesis Shuffle\nManaCost:0\nTypes:Sorcery\nA:SP$ Shuffle | Defined$ You\nOracle:Test shuffle.\n")
	for i := range cfg.Decks[0] {
		cfg.Decks[0][i] = spell
	}
	e, err := NewHypothetical(cfg, []ChanceDraw{{Bound: 2, Value: 0}})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.AdvanceHypothetical(); err != nil {
		t.Fatal(err)
	}
	genesisDraws := e.RNGDraws()
	cast := false
	for i := 0; i < 100 && e.RNGDraws() == genesisDraws; i++ {
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			t.Fatalf("unexpected decision %+v", d)
		}
		choice := -1
		for j, o := range d.Options {
			if o.Kind == "pass" {
				choice = j
			}
		}
		if !cast {
			for j, o := range d.Options {
				if o.Kind == "cast" {
					choice = j
					cast = true
					break
				}
			}
		}
		if choice < 0 {
			t.Fatal("missing cast/pass")
		}
		if err := e.SubmitHypothetical(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{choice}}); err != nil {
			t.Fatal(err)
		}
	}
	if !cast || e.RNGDraws() != genesisDraws+4 {
		t.Fatalf("effect shuffle draws=%d, genesis=%d, cast=%v", e.RNGDraws(), genesisDraws, cast)
	}
	tape := e.ChanceTranscript()
	replay, err := NewHypothetical(cfg, tape)
	if err != nil {
		t.Fatal(err)
	}
	if err := replay.AdvanceHypothetical(); err != nil {
		t.Fatal(err)
	}
	for _, in := range e.L.Intents {
		if err := replay.SubmitHypothetical(in); err != nil {
			t.Fatal(err)
		}
	}
	if replay.L.Head() != e.L.Head() || replay.RNGDraws() != e.RNGDraws() {
		t.Fatal("effects chance replay diverged")
	}
	tape[genesisDraws].Bound++
	bad, err := NewHypothetical(cfg, tape)
	if err != nil {
		t.Fatal(err)
	}
	if err := bad.AdvanceHypothetical(); err != nil {
		t.Fatal(err)
	}
	for _, in := range e.L.Intents {
		if err = bad.SubmitHypothetical(in); err != nil {
			break
		}
	}
	if err == nil || !strings.Contains(err.Error(), "bound") {
		t.Fatalf("effects error=%v", err)
	}
}

// CloneHypothetical copies an actual engine into a hypothetical one with its
// own future chance: the source is untouched, the copy accepts
// SubmitHypothetical, and its generator is the seed's, not the source's.
func TestCloneHypotheticalReseedsWithoutTouchingSource(t *testing.T) {
	t.Parallel()
	e := New(chanceConfig(t))
	e.Advance()
	head, n, draws := e.L.Head(), len(e.L.Events), e.RNGDraws()
	c := e.CloneHypothetical(99)
	if c.L.Head() != head || len(c.ChanceTranscript()) != 0 {
		t.Fatal("hypothetical clone does not start at the source boundary with an empty transcript")
	}
	d := c.Pending()
	if d == nil {
		t.Fatal("no pending decision")
	}
	if err := c.SubmitHypothetical(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
		t.Fatal(err)
	}
	if e.L.Head() != head || len(e.L.Events) != n || e.RNGDraws() != draws {
		t.Fatal("submitting to the hypothetical clone changed the source")
	}
	a, b := e.CloneHypothetical(1), e.CloneHypothetical(1)
	if a.rng.IntN(1<<30) != b.rng.IntN(1<<30) {
		t.Fatal("same seed, different generator")
	}
	src := e.Clone()
	if e.CloneHypothetical(2).rng.IntN(1<<30) == src.rng.IntN(1<<30) && e.CloneHypothetical(3).rng.IntN(1<<30) == e.Clone().rng.IntN(1<<30) {
		t.Fatal("hypothetical clone inherited the source generator")
	}
}
