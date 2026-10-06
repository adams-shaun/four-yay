package rules

// api:Meld dispatch (ticket cli-20261005T172935Z-a198a472). The primitive's
// own lifecycle is covered in meld_test.go; these tests prove the registered
// api:Meld handler is what a real corpus carrier reaches through the ordinary
// legal walker, i.e. that effects.Register("Meld", effMeld) is doing the work
// and not merely compiling.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestMeldAPIDispatchHanweir activates Hanweir Battlements's real
// "AB$ Meld" ability through the priority decision and asserts the melded
// permanent appears. Reverting the effects.Register call in meld_api.go makes
// the ability's effect a no-op Note, so nothing melds and this fails.
func TestMeldAPIDispatchHanweir(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	const battlements, garrison, township = "Hanweir Battlements", "Hanweir Garrison", "Hanweir, the Writhing Township"

	e, cfg := meldGame(t, reg, []string{battlements, garrison}, nil)
	b, g := meldPut(t, e, battlements, 0), meldPut(t, e, garrison, 0)
	// Precondition: both carriers are on the battlefield under seat 0, and the
	// ability's condition (a controlled Hanweir Garrison) is satisfied.
	if e.G.Obj(b).Zone != state.ZBattlefield || e.G.Obj(g).Zone != state.ZBattlefield ||
		e.G.Obj(b).Controller != 0 || e.G.Obj(g).Controller != 0 {
		t.Fatalf("precondition: battlements zone=%s garrison zone=%s", e.G.Obj(b).Zone, e.G.Obj(g).Zone)
	}
	// Cost is {3}{R}{R} plus {T}: fund the pool and re-ask so the offer
	// reflects the funded pool (the addMana/priorityRound shape).
	addMana(t, e, 0, "CCCRR")

	d := e.Pending()
	if d == nil {
		t.Fatal("no priority decision pending")
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == b && strings.Contains(o.Label, "meld") {
			idx = o.Index
			break
		}
	}
	if idx < 0 {
		t.Fatalf("precondition: Hanweir Battlements's meld ability not offered: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	// The ability resolves on the stack; answer any ask (there is none) and
	// pass until the stack empties.
	for i := 0; i < 20 && len(e.G.Stack) > 0; i++ {
		pd := e.Pending()
		if pd == nil {
			break
		}
		if pd.Kind == "priority" {
			submitChoices(t, e, pickPass(pd))
			continue
		}
		submitChoices(t, e, 0)
	}

	// The melded permanent is on the battlefield, named after the alternate
	// face, and the source permanent has left it.
	var found state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if e.Name(id) == township {
			found = id
		}
	}
	if found == 0 {
		t.Fatalf("api:Meld did not create %q on the battlefield", township)
	}
	// The result card (Hanweir Battlements, the card carrying the alternate
	// face) IS the melded permanent; the partner has left the battlefield.
	if found != b {
		t.Fatalf("melded permanent = %d, want the result card %d", found, b)
	}
	if e.G.Obj(g).Zone == state.ZBattlefield {
		t.Fatalf("partner %d (Hanweir Garrison) is still on the battlefield", g)
	}
	if diff := diffGames(e.G, replayFromLog(t, cfg, e.L.Events)); diff != "" {
		t.Fatalf("log-only replay differs:\n%s", diff)
	}
}
