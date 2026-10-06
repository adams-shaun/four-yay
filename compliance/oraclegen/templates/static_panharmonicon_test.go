package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/rules"
)

// panharmoniconOnBattlefield reports whether a permanent named name is on the
// battlefield in snap. The doubling assertion depends on the card under test
// (or the probe) actually being there, so the test asserts it rather than
// trusting the setup.
func panharmoniconOnBattlefield(snap rules.OracleSnapshot, name string) bool {
	for _, p := range snap.Permanents {
		if p.Name == name {
			return true
		}
	}
	return false
}

// panharmoniconReq finds name's Panharmonicon requirement.
func panharmoniconReq(t *testing.T, reg *cards.Registry, name string) levelb.Requirement {
	t.Helper()
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("%s not in the corpus", name)
	}
	for _, r := range levelb.Requirements(c) {
		if r.Sub == "static.panharmonicon" {
			return r
		}
	}
	t.Fatalf("precondition: %s has no static.panharmonicon requirement", name)
	return levelb.Requirement{}
}

// TestStaticPanharmoniconServed: each card is served with exactly two ability
// entries from the listener when the card is on the battlefield and one when
// it is not. The control count is asserted so a probe whose trigger is never
// doubled (or that fires twice anyway) cannot pass.
func TestStaticPanharmoniconServed(t *testing.T) {
	reg := loadGenRegistry(t)
	cases := []struct {
		card     string
		listener string // "" = any probe
	}{
		{"Starfield Vocalist", "Soul Warden"},
		{"Virtue of Knowledge", ""},
		{"Traveling Chocobo", "Steppe Lynx"},
		{"Delney, Streetwise Lookout", ""},
		{"Katara, the Fearless", ""},
		{"Twinflame Travelers", ""},
		{"Annie Joins Up", ""},
	}
	for _, tc := range cases {
		t.Run(tc.card, func(t *testing.T) {
			req := panharmoniconReq(t, reg, tc.card)
			it, skip := GenerateB(reg, tc.card, req)
			if skip != nil {
				t.Fatalf("skipped: %s", skip.Reason)
			}
			with := runItemScenario(t, reg, it, true)
			without := runItemScenario(t, reg, it, false)
			last := with.Snapshots[len(with.Snapshots)-1]
			// Preconditions: the card under test is on the battlefield in the
			// observation and gone in the control; otherwise the count below
			// would be measuring something other than the static.
			if !panharmoniconOnBattlefield(last, tc.card) {
				t.Fatalf("precondition: %s is not on the battlefield in the observation", tc.card)
			}
			if panharmoniconOnBattlefield(without.Snapshots[len(without.Snapshots)-1], tc.card) {
				t.Fatalf("precondition: %s is still on the battlefield in the control", tc.card)
			}
			if len(last.Stack) == 0 {
				t.Fatalf("observation stack is empty")
			}
			ref := last.Stack[0].Source
			n, others := listenerAbilities(last, ref)
			if n != 2 || others != 0 {
				t.Errorf("with %s: %d ability entries from %s (+%d others), want 2", tc.card, n, ref, others)
			}
			n, others = listenerAbilities(without.Snapshots[len(without.Snapshots)-1], ref)
			if n != 1 || others != 0 {
				t.Errorf("without %s: %d ability entries from %s (+%d others), want 1", tc.card, n, ref, others)
			}
			if tc.listener != "" && ref != "p0:"+tc.listener {
				t.Errorf("listener %s, want %s", ref, tc.listener)
			}
			if strings.TrimPrefix(ref, "p0:") == tc.card {
				t.Errorf("the listener is the card under test")
			}
		})
	}
}

// TestStaticPanharmoniconNamedSkips: the shapes no recipe reaches carry a named
// skip, never the generic gap.
func TestStaticPanharmoniconNamedSkips(t *testing.T) {
	reg := loadGenRegistry(t)
	cases := []struct{ card, want string }{
		{"Roaming Throne", "as-enters creature-type choice"},
		{"Cloud, Midgar Mercenary", "equipped-self filter"},
		{"Splinter, Radical Rat", "no creature probe with a simple enters trigger"},
	}
	for _, tc := range cases {
		req := panharmoniconReq(t, reg, tc.card)
		_, skip := GenerateB(reg, tc.card, req)
		if skip == nil {
			t.Errorf("%s: served, want a named skip", tc.card)
			continue
		}
		if !strings.HasPrefix(skip.Reason, "static Panharmonicon ") || !strings.Contains(skip.Reason, tc.want) {
			t.Errorf("%s: skip %q, want static Panharmonicon ... %q", tc.card, skip.Reason, tc.want)
		}
	}
}

// TestStaticPanharmoniconUnmodelledParamsSkipIsDeterministic: a static with
// several unmodelled parameters names all of them, sorted, so the reason that
// reaches the skips file does not depend on map order. Each card is generated
// repeatedly; with map-order selection a multi-key static varies run to run.
func TestStaticPanharmoniconUnmodelledParamsSkipIsDeterministic(t *testing.T) {
	reg := loadGenRegistry(t)
	cases := []struct{ card, key, want string }{
		{"Felix Five-Boots", "static#0.0", "static Panharmonicon parameters CombatDamage,ValidSource,ValidTarget are not modelled by the probes"},
		{"Gandalf the White", "static#0.2", "static Panharmonicon parameters Origin,Secondary are not modelled by the probes"},
	}
	for _, tc := range cases {
		c, ok := reg.Lookup(tc.card)
		if !ok {
			t.Fatalf("%s not in the corpus", tc.card)
		}
		var req *levelb.Requirement
		for _, r := range levelb.Requirements(c) {
			if r.Key == tc.key && r.Sub == "static.panharmonicon" {
				req = &r
			}
		}
		if req == nil {
			t.Fatalf("precondition: %s has no static.panharmonicon requirement %s", tc.card, tc.key)
		}
		for i := 0; i < 20; i++ {
			_, skip := GenerateB(reg, tc.card, *req)
			if skip == nil || skip.Reason != tc.want {
				t.Fatalf("%s %s run %d: skip %+v, want %q", tc.card, tc.key, i, skip, tc.want)
			}
		}
	}
	params := map[string]string{"Mode": "Panharmonicon", "Zeta": "x", "Alpha": "x", "Mid": "x", "Beta": "x"}
	for i := 0; i < 50; i++ {
		if got := strings.Join(panharmoniconUnmodelledParams(params), ","); got != "Alpha,Beta,Mid,Zeta" {
			t.Fatalf("run %d: unmodelled params %q, want Alpha,Beta,Mid,Zeta", i, got)
		}
	}
}
