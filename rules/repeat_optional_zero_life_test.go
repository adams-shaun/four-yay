package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestAdNauseamRepeatOverEmptyLibraryEndsTheLoop is the paymirror resolve-
// horizon livelock (paymirror -seed 1000 -seats 2 -formats constructed:
// seed=1014 tron,the-epic-storm, 16 routes, every one Ad Nauseam): a
// passer that always answers the RepeatOptional$ election "Repeat" drains a
// library and then repeats a body that reveals nothing, loses 0 life (a
// zero-amount LifeChange) and clears an empty Remembered set (a Note). That
// iteration changed nothing -- a 0 LifeChange folds into no state -- so
// CR 732.2a's shortcut must end the do/while there instead of re-offering an
// election whose yes can only loop until the livelock watcher aborts.
func TestAdNauseamRepeatOverEmptyLibraryEndsTheLoop(t *testing.T) {
	e, cfg, id, caster := corpusCardConfig(t, 6104, "Ad Nauseam")
	addMana(t, e, caster, "BBBCC")
	lib := len(e.G.Zone(state.ZLibrary, caster))
	if lib == 0 {
		t.Fatal("precondition: empty library")
	}

	d := castAndReachElection(t, e, id, -1)
	elections := 0
	for d != nil && d.ResumeKind == "repeat_optional" {
		elections++
		if elections > lib { // one election per card taken, none after the no-op
			t.Fatalf("%d repeat elections offered over a %d-card library: a no-progress iteration re-offered the election", elections, lib)
		}
		yes := -1
		for _, o := range d.Options {
			if o.Kind == "yes" {
				yes = o.Index
			}
		}
		if yes < 0 {
			t.Fatalf("election has no repeat option: %+v", d.Options)
		}
		submitChoices(t, e, yes)
		d = e.Pending()
	}
	if got := len(e.G.Zone(state.ZLibrary, caster)); got != 0 {
		t.Fatalf("the loop ended with %d cards left; want the whole library taken", got)
	}
	stopped := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && ev.Obj == id && ev.Text == "the repeated process changed nothing; it is not offered again" {
			stopped = true
		}
	}
	if !stopped {
		t.Fatal("the loop did not end on the no-progress shortcut")
	}
	passUntilStackEmpty(t, e, 30)
	if o := e.G.Obj(id); o != nil && o.Zone == state.ZStack {
		t.Fatal("Ad Nauseam is still on the stack")
	}
	replayCheck(t, e, cfg)
}
