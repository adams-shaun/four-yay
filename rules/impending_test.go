package rules

// kw:Impending (CR 702.176) proof tests.
//
// Impending N—[cost] is an alternative cast: a permanent cast for its
// impending cost enters with N time counters and is not a creature until the
// last is removed; at the beginning of your end step remove a time counter
// from it. The corpus prints the whole mechanic on one K:Impending:N:cost
// line (rules/impending.go owns the readers, the entry rider and the end-step
// trigger; state/object.go's ImpendingDormant is the shared predicate and
// rules/chars/types.go's impendingTypeSwitch is the layer-4 answer).
//
// The behavioural tests use a hand-authored fixture (never a corpus .txt,
// per the licensing rule) so the board is a plain Golem with no enter/attack
// trigger to answer; TestImpendingCensusPinsCorpusCarriers pins the real
// corpus carriers in both directions.
//
// The fixture below is authored inline:
//
//	Name:Impending Test Golem
//	ManaCost:3 G
//	Types:Creature Golem
//	PT:4/4
//	K:Impending:2:1 G

import (
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

const impendingGolemSrc = "Name:Impending Test Golem\nManaCost:3 G\nTypes:Creature Golem\nPT:4/4\n" +
	"K:Impending:2:1 G\nOracle:Impending 2—{1}{G}\n"

// impendingEngine is altCostEngine with the fixture Golem in seat 0's deck.
func impendingEngine(t *testing.T, seed uint64) (*Engine, Config) {
	t.Helper()
	e, cfg, _ := altCostEngine(t, seed, nil, []string{impendingGolemSrc}, nil)
	return e, cfg
}

// castImpending drives the end-to-end impending cast: fund exactly {1}{G},
// pick the (impending) option, drain the stack. It asserts the offer existed
// (the mode is only ever produced by the Impending path).
func castImpending(t *testing.T, e *Engine) state.ObjID {
	t.Helper()
	id := findCardObj(t, e, 0, "Impending Test Golem", state.ZHand)
	// Precondition: the parser kept the keyword with its full parameter, or
	// the offer/charge below could never appear.
	f := e.G.Obj(id).Face()
	if !f.HasKeyword("Impending") {
		t.Fatalf("setup: fixture lost the Impending keyword: %v", f.Keywords)
	}
	if got, _ := f.KeywordParam("Impending"); got != "2:1 G" {
		t.Fatalf("setup: Impending param = %q, want 2:1 G", got)
	}
	// {1}{G} exactly -- not the printed {3}{G}: the (impending) offer exists
	// only because beginCast's "impending" case charged the keyword cost.
	addMana(t, e, 0, "CG")
	submitChoices(t, e, castModeOption(t, e, id, "impending"))
	passUntilStackEmpty(t, e, 40)
	return id
}

// TestImpendingCastEntersWithTimeCountersAndIsNotACreature is the positive
// pin for CR 702.176a's entry half: the permanent enters with its N time
// counters, carries the pay-time provenance, and is not a creature -- both the
// printed-face read (EffectiveIsCreature) and the layer-derived read
// (Engine.IsCreature / Derived.Types) agree.
func TestImpendingCastEntersWithTimeCountersAndIsNotACreature(t *testing.T) {
	t.Parallel()
	if !effects.Supported()["kw:Impending"] {
		t.Fatal("kw:Impending is not registered; the coverage ratchet would still report it")
	}
	e, cfg := impendingEngine(t, 9601)
	id := castImpending(t, e)

	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("impending Golem is in %v, want battlefield (the creature-read assertions below would be vacuous)", o)
	}
	// Precondition: the impending cost was really charged ({1}{G}), or this
	// could be the plain mana-cost cast.
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("impending cast left %d mana; the impending cost was not charged", got)
	}
	if o.CastFlags&state.FlagImpending == 0 {
		t.Fatalf("impending cast carries no FlagImpending: %+v", o.CastFlags)
	}
	// The pay-time CastInfo wire really carries the flag (events.flagNames must
	// name it, or modeFlags' FlagsString would drop it and the hook above would
	// never see the bit).
	castInfo := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.CastInfo && ev.Obj == id && events.FlagsFrom(ev.Counter)&state.FlagImpending != 0 {
			castInfo = true
		}
	}
	if !castInfo {
		t.Fatal("no CastInfo event carries the impending flag")
	}
	if got := o.Counter("TIME"); got != 2 {
		t.Fatalf("entered with %d time counters, want 2 (the printed Impending 2)", got)
	}
	if o.EffectiveIsCreature() {
		t.Error("impending-dormant permanent reads as a creature via EffectiveIsCreature")
	}
	if e.IsCreature(id) {
		t.Error("impending-dormant permanent reads as a creature via the layer-derived IsCreature")
	}
	types := e.Derived(id).Types
	if !impendingContainsWord(types, "Golem") {
		t.Errorf("derived types %v dropped the creature subtype; want Golem kept", types)
	}
	if impendingContainsWord(types, "Creature") {
		t.Errorf("derived types %v still name Creature; the layer-4 switch dropped nothing", types)
	}
	// The entry rider really ran: it granted the recurring end-step removal.
	grants := 0
	for _, ce := range e.active() {
		if ce.AddTrigger != nil && ce.Source == id {
			grants++
		}
	}
	if grants != 1 {
		t.Errorf("impending Golem has %d granted end-step triggers, want 1 (the entry hook did not run)", grants)
	}
	replayCheck(t, e, cfg)
}

// TestImpendingPlainCastIsACreatureWithNoTimeCounters is the discriminating
// control: the same card cast for its printed mana cost ({3}{G}) enters with
// no time counters, carries no FlagImpending, and IS a creature. A mechanic
// that entered dormant on every cast would pass the positive test above and
// fail here.
func TestImpendingPlainCastIsACreatureWithNoTimeCounters(t *testing.T) {
	t.Parallel()
	e, cfg := impendingEngine(t, 9602)
	id := findCardObj(t, e, 0, "Impending Test Golem", state.ZHand)
	if !e.G.Obj(id).Face().HasKeyword("Impending") {
		t.Fatal("setup: fixture lost the Impending keyword")
	}
	// The printed {3}{G}, not the impending {1}{G}.
	addMana(t, e, 0, "CCCG")
	submitChoices(t, e, castModeOption(t, e, id, ""))
	passUntilStackEmpty(t, e, 40)

	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("plain-cast Golem is in %v, want battlefield", o)
	}
	// Precondition: the PLAIN cost was charged, so this is not the impending
	// cast slipping through.
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("plain cast left %d mana; the printed cost was not charged", got)
	}
	if o.CastFlags&state.FlagImpending != 0 {
		t.Fatalf("plain cast carries FlagImpending: %+v", o.CastFlags)
	}
	if got := o.Counter("TIME"); got != 0 {
		t.Fatalf("plain cast entered with %d time counters, want 0", got)
	}
	if !o.EffectiveIsCreature() {
		t.Error("plainly cast Golem is not a creature via EffectiveIsCreature")
	}
	if !e.IsCreature(id) {
		t.Error("plainly cast Golem is not a creature via the layer-derived IsCreature")
	}
	if !impendingContainsWord(e.Derived(id).Types, "Creature") {
		t.Errorf("derived types %v dropped Creature on a plain cast", e.Derived(id).Types)
	}
	replayCheck(t, e, cfg)
}

// TestImpendingEndStepRemovesATimeCounterAndWakesAtTheLast pins the two
// remaining CR 702.176a riders together: the granted end-step trigger removes
// one time counter each of the controller's end steps, and the permanent
// becomes a creature again when the last one leaves.
func TestImpendingEndStepRemovesATimeCounterAndWakesAtTheLast(t *testing.T) {
	t.Parallel()
	e, cfg := impendingEngine(t, 9603)
	id := castImpending(t, e)

	// Precondition: the end-step removal was granted at entry -- a missing
	// grant would make the counter assertions below pass by doing nothing.
	grants := 0
	for _, ce := range e.active() {
		if ce.AddTrigger != nil && ce.Source == id {
			grants++
		}
	}
	if grants != 1 {
		t.Fatalf("impending Golem has %d granted end-step triggers, want 1", grants)
	}
	if got := e.G.Obj(id).Counter("TIME"); got != 2 {
		t.Fatalf("precondition: entered with %d time counters, want 2", got)
	}

	// First end step (the turn it entered, seat 0's own).
	driveToStep(t, e, e.G.Turn, 0, state.StepEnd)
	passUntilStackEmpty(t, e, 40)
	if got := e.G.Obj(id).Counter("TIME"); got != 1 {
		t.Fatalf("after the first controller end step: %d time counters, want 1", got)
	}
	// Second controller end step: seat 0's next turn is two turns later.
	driveToStep(t, e, e.G.Turn+2, 0, state.StepEnd)
	passUntilStackEmpty(t, e, 40)

	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Golem left the battlefield during the end steps: %+v", o)
	}
	if got := o.Counter("TIME"); got != 0 {
		t.Fatalf("after the second controller end step: %d time counters, want 0", got)
	}
	if !o.EffectiveIsCreature() {
		t.Error("the last time counter is gone but the permanent is still not a creature (EffectiveIsCreature)")
	}
	if !e.IsCreature(id) {
		t.Error("the last time counter is gone but the permanent is still not a creature (derived IsCreature)")
	}
	replayCheck(t, e, cfg)
}

// TestImpendingCensusPinsCorpusCarriers is the ratchet: exactly the corpus
// cards whose face prints K:Impending are named, every one of them parses to
// a real cost and a positive count, and the support gate reports none of them
// missing a primitive. A corpus-pin bump that adds a carrier (or a new printed
// shape) fails here instead of silently joining an untested set; a carrier
// whose line stops parsing fails too.
func TestImpendingCensusPinsCorpusCarriers(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	if len(reg.Cards) == 0 {
		t.Fatal("setup: empty corpus registry; the census would pass vacuously")
	}
	supported := effects.Supported()
	if !supported["kw:Impending"] {
		t.Fatal("kw:Impending is not registered")
	}
	want := []string{
		"Lurker in the Deep",
		"Overlord of the Balemurk",
		"Overlord of the Boilerbilges",
		"Overlord of the Floodpits",
		"Overlord of the Hauntwoods",
		"Overlord of the Mistmoors",
	}
	// The five Standard Overlords are the brief's target: they must now be
	// fully supported (kw:Impending was their only blocker). Lurker in the Deep
	// is the sixth corpus carrier; it still misses api:MakeCard (Conjure), an
	// unrelated primitive, so it is only held to "kw:Impending no longer
	// missing".
	fullySupported := map[string]bool{
		"Overlord of the Balemurk":     true,
		"Overlord of the Boilerbilges": true,
		"Overlord of the Floodpits":    true,
		"Overlord of the Hauntwoods":   true,
		"Overlord of the Mistmoors":    true,
	}
	var got []string
	for _, c := range reg.Cards {
		for _, f := range c.Faces {
			if f == nil || !f.HasKeyword("Impending") {
				continue
			}
			name := c.Faces[0].Name
			got = append(got, name)
			if _, ok := impendingCost(f); !ok {
				t.Errorf("%s: K:Impending did not parse to a priceable cost", f.Name)
			}
			if n := impendingCount(f); n <= 0 {
				t.Errorf("%s: K:Impending parsed to count %d, want > 0", f.Name, n)
			}
			missing := reg.Unsupported(c, supported)
			for _, m := range missing {
				if m == "kw:Impending" {
					t.Errorf("%s still reports kw:Impending unsupported", name)
				}
			}
			if fullySupported[name] && len(missing) != 0 {
				t.Errorf("%s is missing primitives %v, want none", name, missing)
			}
			break
		}
	}
	sort.Strings(got)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("corpus Impending carriers changed:\n got %v\nwant %v\n(update the pinned list only alongside a corpus-pin move that adds a real carrier)", got, want)
	}
}

// impendingContainsWord reports whether words contains w exactly.
func impendingContainsWord(words []string, w string) bool {
	for _, x := range words {
		if x == w {
			return true
		}
	}
	return false
}
