package templates_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/compliance/oraclegen/templates"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

// grantedTriggerFired reports whether any snapshot shows the granted trigger
// on the stack: an ability entry whose Trigger slot is empty (a granted
// trigger carries no face-trigger index, rules/oracle_snapshot.go
// stackTriggerSlot) and whose source names the card (a self grant) or, for an
// attachment grant, the permanent the card is attached to.
func grantedTriggerFired(res rules.OracleResult, card string, host string) bool {
	wants := []string{strings.ToLower(card), strings.ToLower(host)}
	for _, s := range res.Snapshots {
		for _, e := range s.Stack {
			if e.Kind != "ability" || e.Trigger != "" {
				continue
			}
			source := strings.ToLower(e.Source)
			for _, want := range wants {
				if want != "" && strings.Contains(source, want) {
					return true
				}
			}
		}
	}
	return false
}

// grantedTriggerGateOn returns the gate-on assertion the item must hold:
// speed 4 in setup, or the gate counters, or the siege in hand for its
// as-enters mode choice.
func grantedTriggerPrecondition(t *testing.T, it oraclegen.Item, card string, mode string) {
	t.Helper()
	p0 := it.Setup["p0"]
	switch {
	case mode != "":
		if !slices.Contains(p0.Hand, card) {
			t.Fatalf("precondition: %s not in p0's hand for its mode choice: %v", card, p0.Hand)
		}
		cast, resolve := false, false
		for _, s := range it.Steps {
			if s.Op == "cast" && s.Card == "p0:"+card {
				cast = true
			}
			if s.Op == "resolve" {
				for _, a := range s.Answers {
					if a.Kind == "modes" && len(a.Pick) == 1 && a.Pick[0] == mode {
						resolve = true
					}
				}
			}
		}
		if !cast {
			t.Fatalf("precondition: no cast of %s before the trigger cause", card)
		}
		if !resolve {
			t.Fatalf("precondition: no mode answer selecting %q on the resolve", mode)
		}
	case it.Setup["p0"].Speed != 0:
		if it.Setup["p0"].Speed != 4 {
			t.Fatalf("precondition: p0 speed = %d, want the MaxSpeed gate's 4", it.Setup["p0"].Speed)
		}
		if !slices.Contains(p0.Battlefield, card) {
			t.Fatalf("precondition: %s not on p0's battlefield: %v", card, p0.Battlefield)
		}
	default:
		t.Fatalf("precondition: neither a mode choice nor a speed gate in the scenario")
	}
}

// grantedTriggerItem is the level-B item for one static requirement, failing
// when the requirement vanished or the card left the corpus.
func grantedTriggerItem(t *testing.T, card, key string) oraclegen.Item {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup(card)
	if !ok {
		t.Fatalf("precondition: %s absent from the corpus", card)
	}
	for _, req := range levelb.Requirements(c) {
		if req.Key != key {
			continue
		}
		if req.Sub != "static.continuous" {
			t.Fatalf("precondition: %s %s = %s, want static.continuous", card, key, req.Sub)
		}
		it, skip := templates.GenerateB(reg, card, req)
		if skip != nil {
			t.Fatalf("GenerateB: %s (the granted-trigger observation regressed to a skip)", skip.Reason)
		}
		return it
	}
	t.Fatalf("precondition: %s has no requirement %s", card, key)
	return oraclegen.Item{}
}

// grantedTriggerCheckpoint is the item replayed with its trailing resolve
// steps dropped and a pass pair plus a priority pass_to appended, so the
// granted trigger the cause put on the stack is still on it at the final
// snapshot (the item itself resolves it, as every served trigger row does).
func grantedTriggerCheckpoint(it oraclegen.Item) oraclegen.Item {
	out := it
	out.Setup = map[string]oraclegen.Seat{"p0": it.Setup["p0"], "p1": it.Setup["p1"]}
	steps := slices.Clone(it.Steps)
	for len(steps) > 0 && steps[len(steps)-1].Op == "resolve" {
		steps = steps[:len(steps)-1]
	}
	out.Steps = append(steps,
		oraclegen.Step{Op: "pass", Seat: 0}, oraclegen.Step{Op: "pass", Seat: 1},
		oraclegen.Step{Op: "pass_to", Decision: "priority"})
	return out
}

// TestStaticGrantedTriggerFiresOnlyWithTheGateOn serves the three shapes of
// the AddTrigger$ grant the observation covers: a MaxSpeed gate (Aether
// Syphon), a MaxSpeed gate over a granted die trigger (Embalmed Ascendant)
// and a ChosenMode gate (Barrensteppe Siege's Abzan branch). Each asserts,
// first, that the gate the scenario supplies is really in the fixture, then
// that the granted trigger fires in gorge with the gate on, and that the
// same cause with the gate OFF fires nothing -- so the trigger on the stack
// is the static's grant and not an ability the cause would produce anyway.
func TestStaticGrantedTriggerFiresOnlyWithTheGateOn(t *testing.T) {
	for _, tc := range []struct {
		card, key, mode string
		// gateOff is the same scenario with the gate turned off: speed 0, or
		// the OTHER mode chosen at the siege's as-enters choice.
		control func(it oraclegen.Item) oraclegen.Item
	}{
		{"Aether Syphon", "static#0.0", "", func(it oraclegen.Item) oraclegen.Item {
			out := it
			out.Setup = map[string]oraclegen.Seat{"p0": it.Setup["p0"], "p1": it.Setup["p1"]}
			p0 := out.Setup["p0"]
			p0.Speed = 0
			out.Setup["p0"] = p0
			return out
		}},
		{"Embalmed Ascendant", "static#0.0", "", func(it oraclegen.Item) oraclegen.Item {
			out := it
			out.Setup = map[string]oraclegen.Seat{"p0": it.Setup["p0"], "p1": it.Setup["p1"]}
			p0 := out.Setup["p0"]
			p0.Speed = 0
			out.Setup["p0"] = p0
			return out
		}},
		{"Barrensteppe Siege", "static#0.0", "Abzan", func(it oraclegen.Item) oraclegen.Item {
			out := it
			out.Setup = map[string]oraclegen.Seat{"p0": it.Setup["p0"], "p1": it.Setup["p1"]}
			out.Steps = slices.Clone(it.Steps)
			for i, s := range out.Steps {
				if s.Op != "resolve" {
					continue
				}
				for j, a := range s.Answers {
					if a.Kind == "modes" && len(a.Pick) == 1 {
						out.Steps[i].Answers[j].Pick = []string{"Mardu"}
					}
				}
			}
			return out
		}},
	} {
		t.Run(tc.card+"/"+tc.key, func(t *testing.T) {
			it := grantedTriggerItem(t, tc.card, tc.key)
			grantedTriggerPrecondition(t, it, tc.card, tc.mode)
			with := runZone(t, grantedTriggerCheckpoint(it))
			if len(with.Fails) != 0 {
				t.Fatalf("with the gate on: %v", with.Fails)
			}
			if !grantedTriggerFired(with, tc.card, "Grizzly Bears") {
				t.Fatalf("the granted trigger never reached the stack with the gate on")
			}
			ctl := grantedTriggerCheckpoint(tc.control(it))
			if res := runZone(t, ctl); grantedTriggerFired(res, tc.card, "Grizzly Bears") {
				t.Fatalf("the granted trigger still fired with the gate off: the firing is not the static's grant")
			}
		})
	}
}

// TestStaticGrantedTriggerRowCountPinned keeps the served shape count visible
// and every named card in the corpus.
func TestStaticGrantedTriggerRowCountPinned(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, r := range []struct{ card, key string }{
		{"Aether Syphon", "static#0.0"},
		{"Embalmed Ascendant", "static#0.0"},
		{"Endrider Spikespitter", "static#0.0"},
		{"Mendicant Core, Guidelight", "static#0.1"},
		{"Pride of the Road", "static#0.0"},
		{"Risen Necroregent", "static#0.0"},
		{"The Aetherspark", "static#0.1"},
		{"Zahur, Glory's Past", "static#0.0"},
		{"Dawnsire, Sunstar Dreadnought", "static#0.0"},
		{"Entropic Battlecruiser", "static#0.0"},
		{"Infinite Guideline Station", "static#0.0"},
		{"Specimen Freighter", "static#0.0"},
		{"Synthesizer Labship", "static#0.0"},
		{"Glowcap Lantern", "static#0.0"},
		{"Barrensteppe Siege", "static#0.0"},
		{"Barrensteppe Siege", "static#0.1"},
		{"Frostcliff Siege", "static#0.0"},
		{"Glacierwood Siege", "static#0.0"},
		{"Hollowmurk Siege", "static#0.0"},
		{"Hollowmurk Siege", "static#0.1"},
		{"Windcrag Siege", "static#0.1"},
	} {
		if _, ok := reg.Lookup(r.card); !ok {
			t.Errorf("precondition: %s absent from the corpus", r.card)
			continue
		}
		grantedTriggerItem(t, r.card, r.key)
	}
}

// TestStaticGrantedTriggerOpponentDiscardAndAttachment covers the two
// non-DFT shapes the observation built custom causes for: an
// "opponent discards" granted trigger (p1 discards through an instant) and an
// attachment grant's trigger (the equipped creature attacks). Each asserts
// the trigger fires with the static in place and, for the attachment, that
// the un-attached control fires nothing.
func TestStaticGrantedTriggerOpponentDiscardAndAttachment(t *testing.T) {
	t.Run("Entropic Battlecruiser discard", func(t *testing.T) {
		it := grantedTriggerItem(t, "Entropic Battlecruiser", "static#0.0")
		if got := it.Setup["p0"].Counters["Entropic Battlecruiser"]["CHARGE"]; got != 1 {
			t.Fatalf("precondition: Entropic Battlecruiser holds %d CHARGE counters, want the GE1 gate's 1", got)
		}
		cast := false
		for _, s := range it.Steps {
			cast = cast || (s.Op == "cast" && s.Seat == 1 && strings.HasSuffix(s.Card, "Occult Epiphany"))
		}
		if !cast {
			t.Fatalf("precondition: no p1 discard cause in the scenario: %+v", it.Steps)
		}
		with := runZone(t, it)
		if len(with.Fails) != 0 {
			t.Fatalf("with the static in place: %v", with.Fails)
		}
		if !grantedTriggerFired(with, "Entropic Battlecruiser", "") {
			t.Fatalf("the granted discard trigger never reached the stack")
		}
		// The control: the same cause with the counter gate off (the
		// Spacecraft unstationed), so the static grants nothing.
		ctl := it
		ctl.Setup = map[string]oraclegen.Seat{"p0": it.Setup["p0"], "p1": it.Setup["p1"]}
		p0 := ctl.Setup["p0"]
		p0.Counters = nil
		ctl.Setup["p0"] = p0
		if res := runZone(t, ctl); grantedTriggerFired(res, "Entropic Battlecruiser", "") {
			t.Fatalf("the granted discard trigger still fired without the gate: the firing is not the grant's")
		}
	})
	t.Run("Glowcap Lantern equipped creature attacks", func(t *testing.T) {
		it := grantedTriggerItem(t, "Glowcap Lantern", "static#0.0")
		attach := false
		for _, s := range it.Steps {
			attach = attach || (s.Op == "attach" && s.Card == "p0:Glowcap Lantern" && s.AttachedTo == "p0:Grizzly Bears")
		}
		if !attach {
			t.Fatalf("precondition: the scenario never attaches the Lantern to the probe: %+v", it.Steps)
		}
		with := runZone(t, it)
		if len(with.Fails) != 0 {
			t.Fatalf("with the static in place: %v", with.Fails)
		}
		if !grantedTriggerFired(with, "", "Grizzly Bears") {
			t.Fatalf("the equipped creature's granted attack trigger never reached the stack")
		}
		// The control: the same attack with the Lantern left unattached (it
		// is on the battlefield, so nothing else changes but the grant).
		ctl := it
		ctl.Steps = slices.DeleteFunc(slices.Clone(it.Steps), func(s oraclegen.Step) bool {
			return s.Op == "attach" && s.Card == "p0:Glowcap Lantern"
		})
		if len(ctl.Steps) >= len(it.Steps) {
			t.Fatalf("precondition: control kept every step (%d of %d)", len(ctl.Steps), len(it.Steps))
		}
		if res := runZone(t, ctl); grantedTriggerFired(res, "", "Grizzly Bears") {
			t.Fatalf("the granted attack trigger still fired unattached: the firing is not the grant's")
		}
	})
}
