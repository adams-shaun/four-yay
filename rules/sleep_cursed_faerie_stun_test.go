package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Sleep-Cursed Faerie's corpus script enters it tapped with three stun
// counters (ReplaceWith$ ETBTapped -> DB$ Tap | ETB$ True -> SubAbility$
// DBAddCounter -> DB$ PutCounter | ETB$ True | CounterType$ STUN |
// CounterNum$ 3). These tests pin the two checkpoints a reader can confuse:
// the ENTRY carries all three counters (TestSleepCursedFaerieStunEntersWithThree),
// while the SETUP checkpoint in the oracle driver reads two because the
// setup drive runs through turn 1's untap step, where CR 122.1d removes one
// stun counter instead of untapping the creature. That -1 is the printed
// rule, not a lost placement: XMage's frozen verdicts agree
// (Sleep-Cursed Faerie/activate#0.0/v1 shows STUN=2 at setup; cast-resolve/v1
// shows STUN=3 at resolve). A future change that makes the checkpoint read
// three would move the engine away from XMage's recorded agreement.

// TestSleepCursedFaerieStunEntersWithThree pins the entry placement on the
// real corpus card: hand -> battlefield entry places exactly three stun
// counters on a tapped permanent, with one CounterChange STUN +3 event and
// one entering Tap event on the log.
func TestSleepCursedFaerieStunEntersWithThree(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)

	faerie := enterFromHand(t, e, reg, 0, "Sleep-Cursed Faerie")
	o := e.G.Obj(faerie)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("fixture: Sleep-Cursed Faerie zone = %v, want battlefield", o.Zone)
	}
	if !o.Tapped || o.Counter("STUN") != 3 {
		t.Fatalf("entry: tapped/stun = %v/%d, want tapped with three stun counters", o.Tapped, o.Counter("STUN"))
	}

	taps, plus3 := 0, 0
	for _, ev := range e.L.Events {
		if ev.Obj != faerie {
			continue
		}
		switch {
		case ev.Kind == events.Tap:
			taps++
		case ev.Kind == events.CounterChange && ev.Counter == "STUN" && ev.Amount == 3:
			plus3++
		}
	}
	if taps != 1 {
		t.Fatalf("entering Tap events on the faerie = %d, want 1", taps)
	}
	if plus3 != 1 {
		t.Fatalf("CounterChange STUN +3 events on the faerie = %d, want 1", plus3)
	}
}

// TestSleepCursedFaerieStunSetupCheckpointIsTwoAfterUntapStep pins the setup
// checkpoint of the oracle driver: the scenario places the Faerie on the
// battlefield at setup, and the setup drive runs from genesis through turn
// 1's untap step, where CR 122.1d removes one stun counter instead of
// untapping. The checkpoint therefore reads Tapped=true, STUN=2, with one +3
// placement and one -1 UntapReplacedByStunNotice substitution on the log.
// This is the test that fails if anyone later "fixes" the engine to keep
// three stun counters at the checkpoint.
func TestSleepCursedFaerieStunSetupCheckpointIsTwoAfterUntapStep(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	const scenario = `{"name":"sleep-cursed-faerie-setup","setup":{"p0":{"battlefield":["Sleep-Cursed Faerie"]}},"steps":[]}`
	sc, err := decodeOracleScenario([]byte(scenario))
	if err != nil {
		t.Fatal(err)
	}
	sc.xmageFixture = true // a generated compliance scenario (RunOracleScenarioJSON)
	fails, transcript, run := runOracleScenario(reg, sc)
	if len(fails) != 0 {
		t.Fatalf("scenario failed: %v\n%s", fails, strings.Join(transcript, "\n"))
	}
	if len(run.snaps) < 1 {
		t.Fatalf("snapshots: %d, want at least the setup snapshot", len(run.snaps))
	}
	p, ok := snapPerm(run.snaps[0], "p0:Sleep-Cursed Faerie")
	if !ok {
		t.Fatalf("Sleep-Cursed Faerie is not on the battlefield at the setup snapshot\n%s", strings.Join(transcript, "\n"))
	}
	if !p.Tapped || p.Counters["STUN"] != 2 {
		t.Fatalf("setup checkpoint: tapped/stun = %v/%d, want tapped with STUN=2 (3 placed, 1 removed by turn 1's untap step per CR 122.1d)\n%s",
			p.Tapped, p.Counters["STUN"], strings.Join(transcript, "\n"))
	}

	faerie, ok := run.refs["p0:Sleep-Cursed Faerie"]
	if !ok {
		t.Fatal("fixture: setup ref p0:Sleep-Cursed Faerie was not bound to an object")
	}
	taps, plus3, minus1 := 0, 0, 0
	for _, ev := range run.e.L.Events {
		if ev.Obj != faerie {
			continue
		}
		switch {
		case ev.Kind == events.Tap:
			taps++
		case ev.Kind == events.CounterChange && ev.Counter == "STUN" && ev.Amount == 3:
			plus3++
		case ev.Kind == events.CounterChange && ev.Counter == "STUN" && ev.Amount == -1 &&
			ev.Text == events.UntapReplacedByStunNotice:
			minus1++
		}
	}
	if taps != 1 {
		t.Fatalf("Tap events on the faerie = %d, want 1 (the ETB$ True entering tap)", taps)
	}
	if plus3 != 1 {
		t.Fatalf("CounterChange STUN +3 events on the faerie = %d, want 1", plus3)
	}
	if minus1 != 1 {
		t.Fatalf("CounterChange STUN -1 UntapReplacedByStunNotice events on the faerie = %d, want 1 (the turn-1 untap step)",
			minus1)
	}
}
