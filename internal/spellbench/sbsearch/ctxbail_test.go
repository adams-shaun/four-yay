package sbsearch

import (
	"context"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// TestArmedContextBailsBetweenWorlds pins the hosted seats' wall-clock
// bail-out (the caller's context, armed by the host with a per-decision
// budget): a ctx that is done stops the world loop BETWEEN worlds -- before
// the first one, when it is already done at the call -- the partial means
// are discarded, and the decision is refused, so search plays sb-tactical's
// pick, the usual no-valid-world fallback.
//
// The cheapest deterministic way to test a bail-out: pass an
// already-cancelled context and assert no world was ever dealt, rather than
// racing a real wall clock. The precondition is asserted first: with a live
// context the SAME evaluate reaches the dealer -- so the zero deals below is
// the bail-out and not a seat that never deals at all.
func TestArmedContextBailsBetweenWorlds(t *testing.T) {
	g := newTestGame(t)
	s := New(g.tactical(7), testSeed, quick())
	d := decision.Decision{Kind: decision.KPriority, Seq: 1, Player: 0}
	roots := []root{
		{in: decision.Intent{Seq: 1, Player: 0}},
		{in: decision.Intent{Seq: 1, Player: 0, Choices: []int{1}}},
	}
	e := &rules.Engine{G: &state.Game{}} // rootTurn only; a refused deal never rolls out
	deals := 0
	mk := func() (dealer, string) {
		return funcDealer(func(w int, seed [2]uint64) (*rules.Engine, string) {
			deals++
			return nil, "test refusal" // a refused deal: dropped, never rolled out
		}), ""
	}

	liveMeans, liveValid, liveFailed, _, liveRefused := s.evaluate(context.Background(), e, mk, d, roots, false, nil)
	if deals == 0 {
		t.Fatal("the live evaluate never reached the dealer; the bail-out assertion below would be vacuous")
	}
	if deals != 2 {
		t.Fatalf("the live evaluate dealt %d worlds, want the quick config's 2", deals)
	}
	if liveValid != 0 || liveFailed != 2 || liveRefused != "deal: test refusal" || len(liveMeans) != 0 {
		t.Fatalf("the live evaluate mis-counted a refused deal: valid %d failed %d refused %q means %v",
			liveValid, liveFailed, liveRefused, liveMeans)
	}

	deals = 0
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	means, valid, _, rollouts, refused := s.evaluate(ctx, e, mk, d, roots, false, nil)
	if deals != 0 {
		t.Fatalf("the armed evaluate dealt %d worlds; the bail-out did not fire at the loop top", deals)
	}
	if valid != 0 || rollouts != 0 || len(means) != 0 {
		t.Fatalf("the armed evaluate still produced work: valid %d rollouts %d means %v", valid, rollouts, means)
	}
	if !strings.HasPrefix(refused, "context: ") {
		t.Fatalf("refused = %q, want the context bail-out's \"context: <err>\"", refused)
	}
}
