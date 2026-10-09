package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// The Earth King (TLA #172, task tek1): "Whenever one or more creatures you
// control with power 4 or greater attack, search your library for up to that
// many basic land cards, put them onto the battlefield tapped, then shuffle."
// The count ref TriggerObjectsAttackers$Amount must read the MATCHED
// attackers (the ones the line's powerGE4 filter admitted), not the whole
// declared batch. Two cases with different attacker counts, so a silent zero
// cannot pass by coincidence: with both creatures attacking the trigger must
// search exactly one (Nessian Asp 4/5; The Earth King 2/2 does not qualify),
// and with only the 2/2 attacking the trigger must not fire at all.
const earthKingFetchScenario = `{"name":"earth-king-fetch","cr":["701.23","603.3"],"why":"The Earth King attack trigger searches for the count of MATCHED attackers","setup":{"p0":{"battlefield":["The Earth King","Nessian Asp"],"library_top":["Forest"]},"p1":{"battlefield":["Grizzly Bears"]}},"steps":[{"op":"attack","seat":0,"defender":"p1","attackers":["p0:The Earth King","p0:Nessian Asp"]},{"op":"resolve","answers":[{"kind":"choose","pick":["p0:Forest"]}]}]}`

const earthKingNoMatchScenario = `{"name":"earth-king-nomatch","cr":["701.23","603.3"],"why":"The Earth King attack trigger stays silent when no attacker reaches power 4","setup":{"p0":{"battlefield":["The Earth King","Nessian Asp"],"library_top":["Forest"]},"p1":{"battlefield":["Grizzly Bears"]}},"steps":[{"op":"attack","seat":0,"defender":"p1","attackers":["p0:The Earth King"]},{"op":"resolve"}]}`

func TestTheEarthKingAttackTriggerFetchesMatchedCount(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	res, err := RunOracleScenarioJSON(reg, []byte(earthKingFetchScenario))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("scenario failed: %v\n%s", res.Fails, strings.Join(res.Transcript, "\n"))
	}
	if got := len(res.Snapshots); got != 3 {
		t.Fatalf("snapshots: %d, want setup, attack, resolve", got)
	}
	setup := res.Snapshots[0]
	// Preconditions: the qualified attacker really is on the battlefield with
	// power >= 4, the unqualified one below it, and the basic land really is
	// in the library to be found.
	if asp, ok := snapPerm(setup, "p0:Nessian Asp"); !ok || asp.PT != "4/5" {
		t.Fatalf("setup p0:Nessian Asp = %+v, %v; want the 4/5 on the battlefield", asp, ok)
	}
	if king, ok := snapPerm(setup, "p0:The Earth King"); !ok || king.PT != "2/2" {
		t.Fatalf("setup p0:The Earth King = %+v, %v; want the 2/2 on the battlefield", king, ok)
	}
	if setup.Players[0].LibraryCount != 38 || len(setup.Players[0].LibraryTop) == 0 ||
		setup.Players[0].LibraryTop[0] != "Forest" {
		t.Fatalf("setup p0 library_count %d library_top %v, want 38 with Forest on top",
			setup.Players[0].LibraryCount, setup.Players[0].LibraryTop)
	}
	// The trigger fired: its ability sat on the stack after the declaration.
	if atk := res.Snapshots[1]; len(atk.Stack) == 0 || atk.Stack[0].Source != "p0:The Earth King" {
		t.Fatalf("attack snapshot stack = %+v, want the Earth King's trigger ability", atk.Stack)
	}
	final := res.Snapshots[2]
	// XMage's frozen row: the search found the one qualifying attacker's
	// worth of land -- library 38 -> 37 and a tapped Forest on the battlefield.
	if got := final.Players[0].LibraryCount; got != 37 {
		t.Fatalf("final p0.library_count = %d, want 37 (XMage parity: one Forest fetched)", got)
	}
	forest, ok := snapPerm(final, "p0:Forest")
	if !ok {
		t.Fatalf("final permanents lack p0:Forest: %+v", final.Permanents)
	}
	if !forest.Tapped || forest.Controller != 0 {
		t.Fatalf("final p0:Forest = %+v, want tapped and controlled by p0", forest)
	}
}

func TestTheEarthKingAttackTriggerStaysSilentWithoutAMatch(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	res, err := RunOracleScenarioJSON(reg, []byte(earthKingNoMatchScenario))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("scenario failed: %v\n%s", res.Fails, strings.Join(res.Transcript, "\n"))
	}
	if got := len(res.Snapshots); got != 3 {
		t.Fatalf("snapshots: %d, want setup, attack, resolve", got)
	}
	setup := res.Snapshots[0]
	if king, ok := snapPerm(setup, "p0:The Earth King"); !ok || king.PT != "2/2" {
		t.Fatalf("setup p0:The Earth King = %+v, %v; want the 2/2 on the battlefield", king, ok)
	}
	if asp, ok := snapPerm(setup, "p0:Nessian Asp"); !ok || asp.PT != "4/5" {
		t.Fatalf("setup p0:Nessian Asp = %+v, %v; want the 4/5 held back (power 4 beats the 2/2 case apart)", asp, ok)
	}
	final := res.Snapshots[2]
	king, ok := snapPerm(final, "p0:The Earth King")
	if !ok || !king.Attacking {
		t.Fatalf("final p0:The Earth King = %+v, %v; want it attacking so the case is not vacuous", king, ok)
	}
	// No attacker reaches power 4, so the trigger never fires: nothing is
	// searched, no land moves, the library is untouched.
	if got := final.Players[0].LibraryCount; got != 38 {
		t.Fatalf("final p0.library_count = %d, want 38 (no trigger, no fetch)", got)
	}
	if _, ok := snapPerm(final, "p0:Forest"); ok {
		t.Fatal("final permanents include p0:Forest; the unqualified attack must fetch nothing")
	}
	if len(final.Stack) != 0 {
		t.Fatalf("final stack = %+v, want empty (the trigger must not have queued)", final.Stack)
	}
}
