package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// The bearer- and filter-scoped combat-legality templates
// (cli-20261009T041714Z-d0f325e1). Each row's generated item is replayed in
// gorge; the test then proves the observation is not vacuous three ways: the
// restricted expectation inverted FAILS with the engine's own message, the
// row's control (the source unattached/uncast/absent, the gate one card
// short, the counter moved) makes the inverted expectation HOLD, and the item
// carries the in-scenario splits (a blocker the static spares, an attacker it
// does not bind) that show the restriction is the filter's and not blanket.

// copyScenario deep-copies the parts a pin test mutates.
func copyScenario(sc oraclegen.Scenario) oraclegen.Scenario {
	out := sc
	out.Setup = make(map[string]oraclegen.Seat, len(sc.Setup))
	for k, v := range sc.Setup {
		v.Battlefield = append([]string(nil), v.Battlefield...)
		v.Graveyard = append([]string(nil), v.Graveyard...)
		out.Setup[k] = v
	}
	out.Steps = make([]oraclegen.Step, len(sc.Steps))
	for i, st := range sc.Steps {
		st.Expect = append([]oraclegen.Expect(nil), st.Expect...)
		out.Steps[i] = st
	}
	return out
}

// restricted finds the one expectation carrying the static's restriction:
// the can_block / can_attack asserted false, or the can_block asserted with
// a Max$ bound above zero. Its position is (step, expect).
func restricted(t *testing.T, sc oraclegen.Scenario) (int, int) {
	t.Helper()
	var hits [][2]int
	for i, st := range sc.Steps {
		for j, e := range st.Expect {
			bound := e.CanBlock != nil && e.CanBlock.MaxBlockers != nil && *e.CanBlock.MaxBlockers > 0
			refused := (e.CanBlock != nil || e.CanAttack != nil) && e.Want != nil && !*e.Want
			if bound || refused {
				hits = append(hits, [2]int{i, j})
			}
		}
	}
	if len(hits) != 1 {
		t.Fatalf("precondition: item carries %d restricted expectations, want exactly 1", len(hits))
	}
	return hits[0][0], hits[0][1]
}

// invert flips the restricted expectation at (i, j): a refusal becomes "may",
// a Max$ bound becomes "no bound".
func invert(sc oraclegen.Scenario, i, j int) oraclegen.Scenario {
	e := sc.Steps[i].Expect[j]
	switch {
	case e.CanBlock != nil && e.CanBlock.MaxBlockers != nil:
		cb := *e.CanBlock
		cb.MaxBlockers = intPtr(0)
		e.CanBlock = &cb
	default:
		e.Want = boolPtr(true)
	}
	sc.Steps[i].Expect[j] = e
	return sc
}

// stripPreAttack drops every step before the first attack: the cast Aura,
// the attach, the token maker -- the source's whole binding.
func stripPreAttack(sc oraclegen.Scenario) oraclegen.Scenario {
	for i, st := range sc.Steps {
		if st.Op == "attack" {
			sc.Steps = sc.Steps[i:]
			return sc
		}
	}
	return sc
}

func dropFromBattlefield(sc oraclegen.Scenario, seat, name string) oraclegen.Scenario {
	s := sc.Setup[seat]
	kept := s.Battlefield[:0:0]
	for _, b := range s.Battlefield {
		if b != name {
			kept = append(kept, b)
		}
	}
	s.Battlefield = kept
	sc.Setup[seat] = s
	return sc
}

// TestScopedLegalityRowsAreObserved pins every served row of the bearer- and
// filter-scoped families.
func TestScopedLegalityRowsAreObserved(t *testing.T) {
	reg := loadGenRegistry(t)
	type row struct {
		card, key, sub string
		wantFail       string // the engine's message when the restriction is claimed away
		// control makes the restriction not bind; nil where the card is its
		// own restricted attacker and no engine-independent change lifts it
		// (the in-scenario splits are then the control).
		control func(sc oraclegen.Scenario, card string) oraclegen.Scenario
	}
	dropCard := func(sc oraclegen.Scenario, card string) oraclegen.Scenario {
		return dropFromBattlefield(sc, "p0", card)
	}
	dropGrave := func(sc oraclegen.Scenario, card string) oraclegen.Scenario {
		s := sc.Setup["p0"]
		s.Graveyard = s.Graveyard[:len(s.Graveyard)-1]
		sc.Setup["p0"] = s
		return sc
	}
	addCounter := func(sc oraclegen.Scenario, card string) oraclegen.Scenario {
		sc.Setup["p0"] = oraclegen.WithCounters(sc.Setup["p0"], card, "P1P1", 1)
		return sc
	}
	dropCounters := func(sc oraclegen.Scenario, card string) oraclegen.Scenario {
		s := sc.Setup["p0"]
		s.Counters = nil
		sc.Setup["p0"] = s
		return sc
	}
	shrink := func(sc oraclegen.Scenario, card string) oraclegen.Scenario {
		sc.Setup["p0"] = oraclegen.WithCounters(sc.Setup["p0"], card, "M1M1", 1)
		return sc
	}
	strip := func(sc oraclegen.Scenario, _ string) oraclegen.Scenario { return stripPreAttack(sc) }
	rows := []row{
		{"Cryptic Coat", "static#0.1", "static.cant-block-by-bearer", "= false, want true", strip},
		{"My Precious", "static#0.1", "static.cant-block-by-bearer", "= false, want true", strip},
		{"Rope", "static#0.1", "static.max-blockers-bearer", "with bound 1, want 0", strip},
		{"Meltstrider's Resolve", "static#0.1", "static.max-blockers-bearer", "with bound 1, want 0", strip},
		{"Eriette of the Charmed Apple", "static#0.0", "static.cant-attack-bearer", "can attack = false, want true", strip},
		{"Burden of Proof", "static#0.2", "static.cant-block-by-filter", "= false, want true", strip},
		{"Storm, Windrider", "static#0.0", "static.cant-attack-filter", "can attack = false, want true", dropCard},
		{"Storm, Windrider", "static#0.1", "static.cant-block-by-filter", "= false, want true", dropCard},
		{"Wall Crawl", "static#0.1", "static.cant-block-by-filter", "= false, want true", dropCard},
		{"Delney, Streetwise Lookout", "static#0.0", "static.cant-block-by-filter", "= false, want true", addCounter},
		{"Duskwatch Hunter", "static#0.0", "static.cant-block-by-filter", "= false, want true", nil},
		{"Flopsie, Bumi's Buddy", "static#0.0", "static.max-blockers-filter", "with bound 1, want 0", shrink},
		{"Michelangelo, Mutant BFF", "static#0.0", "static.max-blockers-filter", "with bound 1, want 0", dropCounters},
		{"Rocksteady, Crash Courser", "static#0.1", "static.max-blockers-filter", "with bound 1, want 0", dropCard},
		{"Secret Tunnel", "static#0.0", "static.cant-block-by-animated-self", "= false, want true", nil},
		{"Waterlogged Hulk", "static#1.0", "static.cant-block-by-crewed", "= false, want true", dropGrave},
	}
	for _, r := range rows {
		t.Run(r.card+"/"+r.key, func(t *testing.T) {
			it := staticItemFor(t, reg, r.card, r.key, r.sub)
			res, ok := runStatic(reg, it.Scenario)
			if !ok || len(res.Fails) != 0 {
				t.Fatalf("precondition: the item's own scenario does not hold: %v", res.Fails)
			}
			i, j := restricted(t, it.Scenario)
			e := it.Scenario.Steps[i].Expect[j]
			var refs []string
			switch {
			case e.CanBlock != nil:
				refs = []string{e.CanBlock.Attacker, e.CanBlock.Blocker}
			case e.CanAttack != nil:
				refs = []string{e.CanAttack.Attacker}
			}
			for _, ref := range refs {
				// A token's snapshot ref is not its scenario ref; the token
				// is checked through the blocker expectation holding.
				if !strings.Contains(ref, ":token:") && !onBattlefield(res, ref) {
					t.Fatalf("precondition: %q is not on the battlefield at the checkpoint", ref)
				}
			}
			// The inverted claim fails with the engine's own message.
			flipped := invert(copyScenario(it.Scenario), i, j)
			fres, ok := runStatic(reg, flipped)
			if !ok || !failsName(fres.Fails, r.wantFail) {
				t.Fatalf("claiming the restriction away must fail on %q, got ok=%v fails=%v", r.wantFail, ok, fres.Fails)
			}
			// The control: the restriction does not bind, so the inverted
			// claim holds.
			if r.control != nil {
				ctl := r.control(invert(copyScenario(it.Scenario), i, j), r.card)
				if cres, ok := runStatic(reg, ctl); !ok || len(cres.Fails) != 0 {
					t.Fatalf("the control must let the inverted claim hold: ok=%v fails=%v", ok, cres.Fails)
				}
			}
			// In-scenario split: the same decision asserts an unrestricted
			// creature (an attacker the filter does not bind, a blocker it
			// spares) as may, so the restriction is not blanket.
			spared := 0
			for k, o := range it.Scenario.Steps[i].Expect {
				if k != j && (o.CanBlock != nil || o.CanAttack != nil) && (o.Want == nil || *o.Want) {
					spared++
				}
			}
			if spared == 0 {
				t.Fatalf("precondition: the decision asserts no unrestricted creature beside the restricted one")
			}
		})
	}
}

// TestAetherSparkAttackedAttachedIsAnEngineGapSkip pins the one recognized
// row the engine does not enforce: the planeswalker Equipment's Target$ names
// itself plus AttachedTo, which combat.RestrictionTargetMatches does not
// read, so the attached card is still offered as a defender. The item is a
// precisely-named skip, not a vacuous pass; when the engine reads the Target$
// the skip disappears and this test names the row to pin.
func TestAetherSparkAttackedAttachedIsAnEngineGapSkip(t *testing.T) {
	reg := loadGenRegistry(t)
	c, ok := reg.Lookup("The Aetherspark")
	if !ok {
		t.Fatal("The Aetherspark not in the corpus")
	}
	var found bool
	for _, r := range levelb.Requirements(c) {
		if r.Key != "static#0.0" {
			continue
		}
		found = true
		if r.Sub != "static.cant-be-attacked-attached" || r.Gap != "" {
			t.Fatalf("classified %q gap %q, want static.cant-be-attacked-attached with no gap", r.Sub, r.Gap)
		}
		_, skip := GenerateB(reg, "The Aetherspark", r)
		if skip == nil || !strings.Contains(skip.Reason, "engine gap: Target$ Card.Self+AttachedTo Creature") {
			t.Fatalf("want the named engine-gap skip, got %+v", skip)
		}
	}
	if !found {
		t.Fatal("precondition: The Aetherspark carries no static#0.0")
	}
}
