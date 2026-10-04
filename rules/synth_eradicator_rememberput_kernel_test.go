package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Synth Eradicator attack trigger end to end under the kernel: the EQ0 gate
// over the PutCounter-recipient memory decides whether the exiled card gets
// the MayPlay-in-exile grant (decline -> playable, accept -> 2 energy, no
// grant).
func TestSynthEradicatorRememberPutGatesTheExiledCardKernel(t *testing.T) {
	t.Parallel()
	run := func(seed uint64, accept bool) (*Engine, Config, state.ObjID) {
		e, cfg, synth := gateFixture(t, seed, "Synth Eradicator")
		synth = gateMoveFromLibrary(t, e, "Synth Eradicator", state.ZBattlefield)
		if e.G.Obj(synth).Zone != state.ZBattlefield {
			t.Fatal("precondition: Synth Eradicator is not on the battlefield")
		}
		e.emit(events.Event{Kind: events.TriggerPush, Obj: synth, Player: 0})
		kr9ResolveTop(e)
		d := e.Pending()
		if d == nil || d.ResumeKind != "put_optional" {
			t.Fatalf("attack trigger did not reach the optional energy election: %+v", d)
		}
		if len(d.Options) != 2 || d.Options[0].Kind != "yes" || d.Options[1].Kind != "no" {
			t.Fatalf("energy election options = %+v, want yes then no (0 = yes)", d.Options)
		}
		answer := 1 // no: decline the energy
		if accept {
			answer = 0 // yes: get {E}{E}
		}
		answerPutOptional(t, e, answer)
		if d := e.Pending(); d != nil && d.Kind != decision.KPriority {
			t.Fatalf("unexpected pending decision after resolving attack trigger: %+v", d)
		}
		// The exiled card is the only object in seat 0's exile that is not the
		// resolving trigger's own ability object (the source permanent is on
		// the battlefield).
		var exiled state.ObjID
		for _, id := range e.G.Zone(state.ZExile, 0) {
			if o := e.G.Obj(id); o != nil && o.Card != nil && id != synth {
				exiled = id
				break
			}
		}
		if exiled == 0 {
			t.Fatalf("precondition: attack trigger did not exile a card: %v", e.G.Zone(state.ZExile, 0))
		}
		if e.G.Obj(exiled).Zone != state.ZExile {
			t.Fatalf("precondition: the imprinted card is in %v, want exile", e.G.Obj(exiled).Zone)
		}
		// The reward rider must be registered: an unregistered DB Effect would
		// emit an `unimplemented API Effect` Note and make the negative half of
		// this test vacuous.
		for _, ev := range e.L.Events {
			if ev.Kind == events.Note && strings.Contains(ev.Text, "unimplemented API Effect") {
				t.Fatalf("DBPlay handler did not run: %q", ev.Text)
			}
		}
		return e, cfg, exiled
	}

	declined, declinedCfg, declinedCard := run(917, false)
	grant := mayPlayGrantOn(declined, declinedCard)
	if grant == nil {
		t.Fatalf("declining the energy election did not register a may-play grant for exiled card %d: %+v",
			declinedCard, declined.continuous)
	}
	if grant.AffectedZone != "Exile" {
		t.Fatalf("grant AffectedZone = %q, want Exile", grant.AffectedZone)
	}
	if !synthEradicatorOffersPlay(declined, declinedCard) {
		t.Fatalf("declining the energy election did not make exiled card %d playable", declinedCard)
	}
	if got := declined.G.Players[0].Counter("ENERGY"); got != 0 {
		t.Fatalf("declined energy = %d, want 0", got)
	}
	replayCheck(t, declined, declinedCfg)

	accepted, acceptedCfg, acceptedCard := run(918, true)
	if got := accepted.G.Players[0].Counter("ENERGY"); got != 2 {
		t.Fatalf("accepted energy = %d, want 2", got)
	}
	if g := mayPlayGrantOn(accepted, acceptedCard); g != nil {
		t.Fatalf("accepted energy election still registered a play grant for exiled card %d: %+v",
			acceptedCard, g)
	}
	if synthEradicatorOffersPlay(accepted, acceptedCard) {
		t.Fatalf("accepted energy election still offers exiled card %d for play", acceptedCard)
	}
	replayCheck(t, accepted, acceptedCfg)
}
