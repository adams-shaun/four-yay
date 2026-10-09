package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// Shriekwood Devourer (task greatestcardpower): "Whenever you attack with one
// or more creatures, untap up to X lands, where X is the greatest power among
// those creatures." Its SVar:X is TriggerObjectsAttackers$GreatestCardPower,
// the property arm evalRefProperty previously had no case for, so X degraded
// to a silent 0 and no land untapped. Two cases with different attacker
// counts: with Shriekwood (7/5) and a 2/2 attacking X must be 7 (the max, not
// the summed 9, and the "up to" cap must bind at 7 over the 8 tapped lands),
// and with only the 2/2 attacking X must be 2 -- so a silent zero cannot pass
// by coincidence.
//
// The tapped lands belong to p1: setup taps happen before turn 1, and turn 1's
// untap step untaps the active seat's (p0's) permanents, so a p0 fixture could
// never reach the attack still tapped. p1's permanents do not untap on p0's
// turn (CR 502.1), and the card says "up to X lands" with no controller
// restriction, which is the candidate set untapTypeCandidates already reads.
const shriekwoodGreatestPowerSevenScenario = `{"name":"shriekwood-greatest-power-seven","cr":["603.3","701.23"],"why":"Shriekwood Devourer untaps up to X lands, X = the greatest power among the attackers (7)","setup":{"p0":{"battlefield":["Shriekwood Devourer","Grizzly Bears"]},"p1":{"battlefield":["Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest"],"tapped":["Forest"]}},"steps":[{"op":"attack","seat":0,"defender":"p1","attackers":["p0:Shriekwood Devourer","p0:Grizzly Bears"]},{"op":"resolve","answers":[{"kind":"choose","pick":["p1:Forest","p1:Forest#2","p1:Forest#3","p1:Forest#4","p1:Forest#5","p1:Forest#6","p1:Forest#7"]}]}]}`

const shriekwoodGreatestPowerTwoScenario = `{"name":"shriekwood-greatest-power-two","cr":["603.3","701.23"],"why":"Shriekwood Devourer untaps up to X lands, X = the greatest power among the attackers (2 when only the 2/2 attacks)","setup":{"p0":{"battlefield":["Shriekwood Devourer","Grizzly Bears"]},"p1":{"battlefield":["Forest","Forest","Forest","Forest","Forest","Forest","Forest","Forest"],"tapped":["Forest"]}},"steps":[{"op":"attack","seat":0,"defender":"p1","attackers":["p0:Grizzly Bears"]},{"op":"resolve","answers":[{"kind":"choose","pick":["p1:Forest","p1:Forest#2"]}]}]}`

// forestTaps counts the final battlefield's tapped and untapped Forests.
func forestTaps(s OracleSnapshot) (tapped, untapped int) {
	for _, p := range s.Permanents {
		if p.Name != "Forest" {
			continue
		}
		if p.Tapped {
			tapped++
		} else {
			untapped++
		}
	}
	return tapped, untapped
}

// untapDecision returns the recorded untap chooser, if the scenario posed one.
func untapDecision(ds []OracleDecision) (OracleDecision, bool) {
	for _, d := range ds {
		if d.Resume == "untap" {
			return d, true
		}
	}
	return OracleDecision{}, false
}

func TestShriekwoodDevourerUntapsGreatestPowerLands(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	res, err := RunOracleScenarioJSON(reg, []byte(shriekwoodGreatestPowerSevenScenario))
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
	// Preconditions: the compared powers really differ (7/5 vs 2/2) and all
	// 8 lands really start tapped, so the untap is observable.
	if sh, ok := snapPerm(setup, "p0:Shriekwood Devourer"); !ok || sh.PT != "7/5" {
		t.Fatalf("setup p0:Shriekwood Devourer = %+v, %v; want the 7/5 on the battlefield", sh, ok)
	}
	if bear, ok := snapPerm(setup, "p0:Grizzly Bears"); !ok || bear.PT != "2/2" {
		t.Fatalf("setup p0:Grizzly Bears = %+v, %v; want the 2/2 on the battlefield", bear, ok)
	}
	if tapped, untapped := forestTaps(setup); tapped != 8 || untapped != 0 {
		t.Fatalf("setup forests tapped/untapped = %d/%d, want 8/0", tapped, untapped)
	}
	// The trigger fired: its ability sat on the stack after the declaration.
	if atk := res.Snapshots[1]; len(atk.Stack) == 0 || atk.Stack[0].Source != "p0:Shriekwood Devourer" {
		t.Fatalf("attack snapshot stack = %+v, want Shriekwood Devourer's trigger ability", atk.Stack)
	}
	// The chooser's own bounds carry X: Max 7 (the greatest power) over all
	// 8 tapped lands, Min 0 (up to). An over-wide X (8) or the summed 9
	// would publish a different Max; X = 0 would pose no chooser at all and
	// leave the queued answer unconsumed, which res.Fails already rejects.
	d, ok := untapDecision(res.Decisions)
	if !ok {
		t.Fatalf("no untap chooser was recorded: %+v", res.Decisions)
	}
	if d.Min != 0 || d.Max != 7 || d.Options != 8 || len(d.Picks) != 7 {
		t.Fatalf("untap chooser min/max/options/picks = %d/%d/%d/%d, want 0/7/8/7", d.Min, d.Max, d.Options, len(d.Picks))
	}
	final := res.Snapshots[2]
	if tapped, untapped := forestTaps(final); tapped != 1 || untapped != 7 {
		t.Fatalf("final forests tapped/untapped = %d/%d, want 1/7 (exactly X = 7 untapped)", tapped, untapped)
	}
}

func TestShriekwoodDevourerGreatestPowerTracksTheAttackers(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	res, err := RunOracleScenarioJSON(reg, []byte(shriekwoodGreatestPowerTwoScenario))
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
	if sh, ok := snapPerm(setup, "p0:Shriekwood Devourer"); !ok || sh.PT != "7/5" {
		t.Fatalf("setup p0:Shriekwood Devourer = %+v, %v; want the 7/5 held back (its 7 would beat the 2)", sh, ok)
	}
	if tapped, untapped := forestTaps(setup); tapped != 8 || untapped != 0 {
		t.Fatalf("setup forests tapped/untapped = %d/%d, want 8/0", tapped, untapped)
	}
	// Only the 2/2 attacked, so the referent set is that one attacker: X = 2.
	d, ok := untapDecision(res.Decisions)
	if !ok {
		t.Fatalf("no untap chooser was recorded: %+v", res.Decisions)
	}
	if d.Min != 0 || d.Max != 2 || d.Options != 8 || len(d.Picks) != 2 {
		t.Fatalf("untap chooser min/max/options/picks = %d/%d/%d/%d, want 0/2/8/2", d.Min, d.Max, d.Options, len(d.Picks))
	}
	final := res.Snapshots[2]
	if tapped, untapped := forestTaps(final); tapped != 6 || untapped != 2 {
		t.Fatalf("final forests tapped/untapped = %d/%d, want 6/2 (exactly X = 2 untapped)", tapped, untapped)
	}
}
