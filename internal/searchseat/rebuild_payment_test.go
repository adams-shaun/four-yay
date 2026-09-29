package searchseat_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	gbench "github.com/adams-shaun/gorge/internal/bench"
	ss "github.com/adams-shaun/gorge/internal/searchseat"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/replay"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
)

// TestRebuildFeedRecordsPaymentPlanAnswers pins ticket
// agent-20260929T051923Z-27abefaa: a log whose priority answers carry a
// payment-plan witness (or an announce value) must rebuild through
// RebuildFeed. The live driver builds the payment extension before the seat
// answers, and Submit rebuilds it lazily on the way in -- but the replay
// walk's visit fires BEFORE its Submit, so without the matching
// EnsurePaymentActions call in RebuildFeed's own visit, RecordAnswer's
// Validate rejects the planned witness ("payment action ... is not offered")
// and the rebuild errors. RebuildFeed runs on the host's undo path, where a
// rebuild error is a D15 crash by design.
//
// Precondition the assertion depends on: the pinned game actually recorded
// at least one payment/announce priority answer per probed seat. Without
// that, the test would pass vacuously with the fix missing.
func TestRebuildFeedRecordsPaymentPlanAnswers(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	delver, err := testutil.LoadRepoDeck(reg, "ur-delver")
	if err != nil {
		t.Fatal(err)
	}
	control, err := testutil.LoadRepoDeck(reg, "uw-control")
	if err != nil {
		t.Fatal(err)
	}
	const seed = uint64(2)
	cfg := rules.Config{Seed: seed, Names: []string{"ur-delver", "uw-control"}, Decks: [][]*cards.Card{delver, control}, Tokens: reg.Tokens}
	seats := []seat.Seat{
		seat.NewBot(seed ^ 1).EnableAutoPayMana(),
		seat.NewBot(seed ^ 2).EnableAutoPayMana(),
	}
	_, engine, err := gbench.PlayGame(cfg, seats, 60, 400, gbench.Hooks{})
	if err != nil {
		t.Fatalf("bench play: %v", err)
	}

	// Probe walk (a pure replay read: it never builds payment actions and so
	// does not depend on the fix) to find, per seat, the FIRST intent
	// boundary whose recorded answer carries a payment witness or an
	// announce value on a priority decision.
	errAbort := errors.New("probe: both actors found")
	boundary := []int{-1, -1}
	payIntents := 0
	_, err = replay.Walk(engine.L, cfg, len(engine.L.Intents), func(e *rules.Engine, i int) error {
		if i >= len(engine.L.Intents) {
			return nil
		}
		in := engine.L.Intents[i]
		if in.Payment == nil && in.Announce == nil {
			return nil
		}
		payIntents++
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			return nil
		}
		p := int(d.Player)
		if boundary[p] < 0 {
			boundary[p] = i
		}
		if boundary[0] >= 0 && boundary[1] >= 0 {
			return errAbort
		}
		return nil
	})
	if err != nil && !errors.Is(err, errAbort) {
		t.Fatalf("probe walk: %v", err)
	}
	if payIntents == 0 {
		t.Fatalf("precondition failed: the pinned seed-%d game recorded no payment/announce intents at all; the payment-plan rebuild path is not exercised", seed)
	}
	for _, p := range boundary {
		if p >= 0 {
			t.Logf("first payment/announce priority boundary at intent %d", p)
		}
	}

	probed := 0
	for p, b := range boundary {
		if b < 0 {
			continue
		}
		actor := state.PlayerID(p)
		n := b + 1
		t.Run(fmt.Sprintf("actor-%d", p), func(t *testing.T) {
			got, err := ss.RebuildFeed(cfg, engine.L, n, actor)
			if err != nil {
				t.Fatalf("RebuildFeed(%d) of a log with a payment-plan answer at intent %d: %v", n, b, err)
			}
			if got.Frames() == 0 {
				t.Fatalf("actor %d: rebuilt feed captured no frames", actor)
			}
			ans := got.History().Answers
			if len(ans) == 0 {
				t.Fatalf("actor %d: rebuilt feed recorded no answers across %d frames", actor, got.Frames())
			}
			// The boundary answer is the LAST one recorded (the walk records
			// only for i < n): its frame key must exist. The slice itself may
			// legitimately be empty -- this game's payment intents are
			// priority passes carrying a plan and no choices -- so key
			// presence is the assertion: pre-fix, RecordAnswer errored before
			// anything was recorded.
			last := -1
			for k := range ans {
				if k > last {
					last = k
				}
			}
			if _, ok := ans[last]; !ok {
				t.Fatalf("actor %d: boundary answer at frame %d was not recorded", actor, last)
			}
		})
		probed++
	}
	if probed == 0 {
		t.Fatalf("precondition failed: neither seat recorded a payment/announce answer at a priority boundary; the payment-plan rebuild path is not exercised")
	}
}
