package templates_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/compliance/oraclegen/templates"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// The two served rows of Petrified Hamlet (ticket levelb-static-named-enters):
// a land that names a card while its ETB trigger resolves. Both scenarios
// play the land, resolve the ETB trigger -- whose NameCard ask gorge's legacy
// no-universe runner answers with p0's library top, which the setup pins to
// the probe land -- and assert at p0's next priority. The control drops the
// source from p0's hand and the play/resolve steps, so the same checkpoint
// measures the board as if the land were never played.

const (
	namedEntersCard     = "Petrified Hamlet"
	namedEntersSub      = "static.cant-be-activated-named-enters"
	namedEntersProbe    = "Karakas"
	namedEntersTarget   = "Thalia, Guardian of Thraben"
	namedEntersSuppress = "return target legendary creature"
	namedEntersGrant    = "add"
	namedEntersXChoice  = "Karakas"
	namedEntersNameStep = 1 // the resolve step poses the NameCard ask
)

// namedEntersItem generates the level-B item for one of the card's
// requirements and asserts the classification.
func namedEntersItem(t *testing.T, key, sub string) oraclegen.Item {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup(namedEntersCard)
	if !ok {
		t.Fatalf("precondition: %s absent from corpus", namedEntersCard)
	}
	for _, r := range levelb.Requirements(c) {
		if r.Key != key {
			continue
		}
		if r.Sub != sub {
			t.Fatalf("precondition: %s %s classified %s, want %s", namedEntersCard, key, r.Sub, sub)
		}
		it, skip := templates.GenerateB(reg, namedEntersCard, r)
		if skip != nil {
			t.Fatalf("%s %s: %s", namedEntersCard, key, skip.Reason)
		}
		return it
	}
	t.Fatalf("precondition: %s carries no requirement %s", namedEntersCard, key)
	return oraclegen.Item{}
}

// namedEntersPreconditions asserts the fixture the observation reads is
// really there: the probe land and its legendary target on p0's battlefield,
// the probe's name on top of p0's library, and the source in p0's hand.
func namedEntersPreconditions(t *testing.T, it oraclegen.Item) {
	t.Helper()
	if it.Card != namedEntersCard {
		t.Fatalf("precondition: item card %q, want %q", it.Card, namedEntersCard)
	}
	p0 := it.Setup["p0"]
	for _, want := range []string{namedEntersProbe, namedEntersTarget} {
		if !slices.Contains(p0.Battlefield, want) {
			t.Fatalf("precondition: %q not on p0's battlefield %v", want, p0.Battlefield)
		}
	}
	if len(p0.LibraryTop) == 0 || p0.LibraryTop[0] != namedEntersProbe {
		t.Fatalf("precondition: p0 library top %v, want %q first (the name the legacy ask offers)", p0.LibraryTop, namedEntersProbe)
	}
	if !slices.Contains(p0.Hand, namedEntersCard) {
		t.Fatalf("precondition: source not in p0's hand %v", p0.Hand)
	}
}

// namedEntersSteps checks the play/resolve/checkpoint shape of the item and
// returns the checkpoint step.
func namedEntersSteps(t *testing.T, it oraclegen.Item) oraclegen.Step {
	t.Helper()
	if len(it.Steps) != 3 || it.Steps[0].Op != "play" || it.Steps[1].Op != "resolve" {
		t.Fatalf("precondition: steps %+v, want play/resolve/checkpoint", it.Steps)
	}
	return it.Steps[2]
}

// namedEntersControl builds the source-removed control: the same checkpoint,
// the source out of p0's hand, the play and resolve steps gone, every want
// flipped to true.
func namedEntersControl(it oraclegen.Item) oraclegen.Item {
	ctl := it
	ctl.Setup = map[string]oraclegen.Seat{}
	for k, v := range it.Setup {
		ctl.Setup[k] = v
	}
	p0 := ctl.Setup["p0"]
	p0.Hand = slices.DeleteFunc(slices.Clone(p0.Hand), func(n string) bool { return n == namedEntersCard })
	ctl.Setup["p0"] = p0
	ctl.Steps = append([]oraclegen.Step(nil), it.Steps[2])
	ctl.Steps[0].Expect = append([]oraclegen.Expect(nil), ctl.Steps[0].Expect...)
	want := true
	for i := range ctl.Steps[0].Expect {
		ctl.Steps[0].Expect[i].Want = &want
	}
	return ctl
}

// namedEntersScriptsName asserts the item scripts XMage's card-name answer
// (the chosen name) at the resolve step that poses the ask.
func namedEntersScriptsName(t *testing.T, it oraclegen.Item) {
	t.Helper()
	if namedEntersNameStep >= len(it.XAnswers) {
		t.Fatalf("precondition: no XAnswers for step %d in %+v", namedEntersNameStep, it.XAnswers)
	}
	want := oraclegen.XAnswer{Seat: 0, Kind: "choice", Value: namedEntersXChoice}
	found := false
	for _, a := range it.XAnswers[namedEntersNameStep] {
		if a == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("no %q name answer at step %d in %+v", namedEntersXChoice, namedEntersNameStep, it.XAnswers)
	}
}

// TestCantBeActivatedNamedEntersPetrifiedHamlet locks static#0.0: the named
// probe land's non-mana activated ability is withheld at p0's next priority
// while the control offers it.
func TestCantBeActivatedNamedEntersPetrifiedHamlet(t *testing.T) {
	it := namedEntersItem(t, "static#0.0", namedEntersSub)
	if want := namedEntersCard + "/static#0.0/v1"; it.ID != want {
		t.Fatalf("identity = %q, want %q", it.ID, want)
	}
	namedEntersPreconditions(t, it)
	last := namedEntersSteps(t, it)
	if len(last.Expect) != 1 || last.Expect[0].Offered == nil || last.Expect[0].Want == nil || *last.Expect[0].Want {
		t.Fatalf("checkpoint %+v lacks a want=false offered assertion", last.Expect)
	}
	o := last.Expect[0].Offered
	if o.Kind != "activate" || o.Card != "p0:"+namedEntersProbe || !strings.Contains(strings.ToLower(o.Label), namedEntersSuppress) {
		t.Fatalf("asserted option %+v, want the probe's return ability", o)
	}
	if res := runZone(t, it); len(res.Fails) != 0 {
		t.Fatalf("with the source: %v", res.Fails)
	}
	namedEntersScriptsName(t, it)
	if res := runZone(t, namedEntersControl(it)); len(res.Fails) != 0 {
		t.Fatalf("control does not offer the return ability: %v", res.Fails)
	}
}

// TestGrantedManaNamedCardPetrifiedHamlet locks static#0.1: the named probe
// land is offered the granted "{T}: Add {C}." activation while the control,
// without the source, is not.
func TestGrantedManaNamedCardPetrifiedHamlet(t *testing.T) {
	it := namedEntersItem(t, "static#0.1", "static.continuous")
	if want := namedEntersCard + "/static#0.1/v1"; it.ID != want {
		t.Fatalf("identity = %q, want %q", it.ID, want)
	}
	namedEntersPreconditions(t, it)
	last := namedEntersSteps(t, it)
	if len(last.Expect) != 1 || last.Expect[0].Offered == nil || last.Expect[0].Want == nil || !*last.Expect[0].Want {
		t.Fatalf("checkpoint %+v lacks a want=true offered assertion", last.Expect)
	}
	o := last.Expect[0].Offered
	if o.Kind != "activate" || o.Card != "p0:"+namedEntersProbe || !strings.Contains(strings.ToLower(o.Label), namedEntersGrant) {
		t.Fatalf("asserted option %+v, want the granted Add {C} activation", o)
	}
	if res := runZone(t, it); len(res.Fails) != 0 {
		t.Fatalf("with the source: %v", res.Fails)
	}
	namedEntersScriptsName(t, it)
	if res := runZone(t, namedEntersControl(it)); len(res.Fails) == 0 {
		t.Fatalf("the granted activation is still offered without the source: the assertion is not the static's")
	}
}
