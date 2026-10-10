package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// TestStaticGrantedStaticServed: the six AddStaticAbility$ rows this template
// serves generate a scenario that replays and whose own assertions hold. Each
// item's observation (the trigger count, the attack cap, the offered cast or
// play, the probe's changed characteristics) is checked gorge-side by the
// generator's controls, so a row whose granted static the engine does not
// apply can only skip.
func TestStaticGrantedStaticServed(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct{ card, key string }{
		{"Racers' Scoreboard", "static#0.0"},
		{"The Masamune", "static#0.1"},
		{"Tomik, Orzhov Lawmage", "static#0.0"},
		{"Frostcliff Siege", "static#0.1"},
		{"Glacierwood Siege", "static#0.1"},
		{"Windcrag Siege", "static#0.0"},
	} {
		t.Run(tc.card, func(t *testing.T) {
			req := counterReq(t, reg, tc.card, tc.key)
			it, skip := GenerateB(reg, tc.card, req)
			if skip != nil {
				t.Fatalf("GenerateB skipped: %s", skip.Reason)
			}
			if len(it.Scenario.Steps) == 0 {
				t.Fatal("served item has no observation steps")
			}
			res, ok := runStatic(reg, it.Scenario)
			if !ok {
				t.Fatal("served scenario does not replay in gorge")
			}
			if len(res.Fails) != 0 {
				t.Fatalf("served scenario's own expectations fail: %v", res.Fails)
			}
		})
	}
}

// TestStaticGrantedStaticUnservedModeNotServed: an AddStaticAbility$ body in
// a mode no shape here serves is NOT served by this template (the row keeps
// its named grant gap). Zulaport Enforcer's level-3 static grants CantBlockBy,
// a real grant shape this template does not observe.
func TestStaticGrantedStaticUnservedModeNotServed(t *testing.T) {
	reg := loadGenRegistry(t)
	const name = "Zulaport Enforcer"
	req := counterReq(t, reg, name, "static#0.1")
	c, ok := reg.Lookup(name)
	if !ok || len(c.Faces) == 0 {
		t.Fatalf("precondition: %s not in the corpus", name)
	}
	f := c.Faces[0]
	st, _ := staticSlotOf(f, req)
	// PRECONDITION: the slot really carries an AddStaticAbility$ grant, so the
	// assertion below is about a grant and not an empty slot.
	if !st.HasParam(cards.PKAddStaticAbility) {
		t.Fatalf("precondition: %s %s does not grant a static ability: %v", name, req.Key, st.Params)
	}
	if _, served := staticGrantedStaticItem(reg, f, name, req, st); served {
		t.Fatal("a granted CantBlockBy static was served, want no observation")
	}
}
