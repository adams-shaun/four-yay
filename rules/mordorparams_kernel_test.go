package rules

// Kernel-era restorations of the mordorparams_test.go behaviour tests the W3
// legacy removal deleted: the same scenarios, driven through the resolution
// kernel (rules/resolve) instead of the suspend/resume protocol.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestArcaneDenialSlowtripDrawsUpToTwo drives Arcane Denial's real compiled
// DrawTwo SVar ("Its controller may draw up to two cards") against a delayed
// trigger whose remembered set names the countered spell's CONTROLLER — seat
// 1 here, never the resolving controller — the way the Gríma test drives
// DBRestRandomOrder. Two reads are pinned at once:
//
//   - `Defined$ DelayTriggerRemembered` hands the remembered PLAYER through,
//     so the ask and the draws belong to seat 1 (definedSpec's own case; an
//     objects-only read, or a case that falls out of the switch, retargets
//     the whole draw at the resolving source's controller);
//   - `Upto$ True` poses a real Min 0 / Max 2 KChoose over the TARGET's own
//     library top, and both answers are asserted — answering both draws 2,
//     answering none draws 0, since a single-value answer can pass by
//     coincidence. A library with one card caps the ask at 1.
//
// The full Arcane Denial chain cannot deliver the remembered controller
// today: the SP$ Counter's `RememberTargets$ True` is the census-tracked
// `param:api:Counter.RememberTargets` gap, so the registration remembers
// nobody. That gap is this test's reason for driving the SVar directly —
// it is NOT worked around by pinning the wrong-seat fallback.
func TestArcaneDenialSlowtripDrawsUpToTwo(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	denial := searchCorpusCard(t, reg, "Arcane Denial")
	drawTwo := cards.ResolveSVar(denial.Faces[0].SVars, "DrawTwo")
	if drawTwo == nil {
		t.Fatal("Arcane Denial's DrawTwo SVar did not resolve")
	}

	// setup builds a two-seat game, parks a source permanent on seat 0's
	// battlefield and returns the delayed trigger's resolution context: the
	// remembered set is seat 1, the countered spell's controller.
	setup := func(t *testing.T, seed uint64) (*Engine, Config, *effects.Ctx) {
		t.Helper()
		e, cfg := mordorEngine(t, reg, seed, "Grizzly Bears")
		src := moveToBattlefieldByName(t, e, 0, "Grizzly Bears")
		e.priorityRound()
		ctx := &effects.Ctx{Source: src, Controller: 0, ResolvingObj: src,
			Remembered: []state.Target{{Player: 1, IsPlayer: true}}}
		return e, cfg, ctx
	}
	// uptoAsk resolves DrawTwo and returns the posed ask, asserting the
	// shape every subtest shares.
	uptoAsk := func(t *testing.T, e *Engine, ctx *effects.Ctx, wantMax int) *decision.Decision {
		t.Helper()
		kr6Probe(e, func() { c := *ctx; effects.Resolve(e, &c, drawTwo) })
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose || d.Min != 0 || d.Max != wantMax {
			t.Fatalf("upto ask = %+v, want KChoose Min 0 Max %d", d, wantMax)
		}
		if d.Player != 1 {
			t.Fatalf("upto ask player = %d, want the remembered seat 1 "+
				"(Defined$ DelayTriggerRemembered dropped the player)", d.Player)
		}
		return d
	}

	t.Run("answer two draws two", func(t *testing.T) {
		e, cfg, ctx := setup(t, 4103)
		lib := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 1)...)
		before := countDrawFor(e, 1)
		d := uptoAsk(t, e, ctx, 2)
		if len(d.Options) != 2 || d.Options[0].Kind != "card" {
			t.Fatalf("upto ask shape = %+v, want two card options", d.Options)
		}
		// The options are the TARGET's own library top, in library order.
		for i, o := range d.Options {
			if o.Obj != lib[i] {
				t.Fatalf("option %d = %d, want seat 1's library top card %d", i, o.Obj, lib[i])
			}
		}
		submitChoices(t, e, d.Options[0].Index, d.Options[1].Index)
		if got := countDrawFor(e, 1) - before; got != 2 {
			t.Fatalf("answering both drew %d for seat 1, want 2", got)
		}
		replayCheck(t, e, cfg)
	})

	t.Run("answer none draws none", func(t *testing.T) {
		e, cfg, ctx := setup(t, 4104)
		before := countDrawFor(e, 1)
		beforeSeat0 := countDrawFor(e, 0)
		uptoAsk(t, e, ctx, 2)
		submitChoices(t, e) // the empty answer, legal at Min 0
		if got := countDrawFor(e, 1) - before; got != 0 {
			t.Fatalf("answering none drew %d for seat 1, want 0", got)
		}
		if got := countDrawFor(e, 0) - beforeSeat0; got != 0 {
			t.Fatalf("the declined upto draw drew %d for the resolving controller", got)
		}
		replayCheck(t, e, cfg)
	})

	t.Run("one card left caps the ask at one", func(t *testing.T) {
		e, cfg, ctx := setup(t, 4105)
		// Shrink the ASKED seat's library to one card through logged moves,
		// so the ask caps at Min 0 / Max 1 with one option.
		for len(e.G.Zone(state.ZLibrary, 1)) > 1 {
			id := e.G.Zone(state.ZLibrary, 1)[0]
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZGraveyard})
		}
		before := countDrawFor(e, 1)
		d := uptoAsk(t, e, ctx, 1)
		if len(d.Options) != 1 {
			t.Fatalf("capped upto ask = %+v, want one option", d.Options)
		}
		submitChoices(t, e, d.Options[0].Index)
		if got := countDrawFor(e, 1) - before; got != 1 {
			t.Fatalf("the capped answer drew %d, want 1", got)
		}
		replayCheck(t, e, cfg)
	})
}
