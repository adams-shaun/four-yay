package effects

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestPutCounterRememberPutRemembersPlayerRecipient pins Synth Eradicator's
// DBEnergy body: paying the optional election puts two energy counters on its
// controller and RememberPut$ makes that player visible to the chained gate.
func TestPutCounterRememberPutRemembersPlayerRecipient(t *testing.T) {
	sa, sv := corpusPutCounterSA(t, "Synth Eradicator")
	if !strings.EqualFold(strings.TrimSpace(sa.Params["RememberPut"]), "True") {
		t.Fatalf("precondition: Synth Eradicator DBEnergy RememberPut$ = %q, want True", sa.Params["RememberPut"])
	}
	// Isolate this API body from DBPlay/DBCleanup so the remembered recipient
	// can be asserted before the card's own cleanup clears the set.
	body := *sa
	body.Sub = nil
	sa = &body
	h := &askHost{}
	h.g = state.NewGame(names(2))
	src := putCounterObject(t, &h.fakeHost)
	c := &Ctx{Source: src, Controller: 0, SVars: sv,
		Targets: []state.Target{{Player: 0, IsPlayer: true}}, PutOpt: "yes"}

	Resolve(h, c, sa)
	if got := h.g.Players[0].Counter("ENERGY"); got != 2 {
		t.Fatalf("controller energy = %d, want 2", got)
	}
	if len(c.Remembered) != 1 || !c.Remembered[0].IsPlayer || c.Remembered[0].Player != 0 {
		t.Fatalf("Remembered = %+v, want exactly player 0 (the recipient of the counters)", c.Remembered)
	}
	for _, ev := range h.log {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unimplemented API") {
			t.Fatalf("PutCounter handler did not run: %q", ev.Text)
		}
	}
}
