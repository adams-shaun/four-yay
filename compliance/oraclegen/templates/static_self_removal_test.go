package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

func TestStaticSelfRemovalRows(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	gen := func(card, key string) (oraclegen.Item, *oraclegen.Skip) {
		t.Helper()
		c, ok := reg.Lookup(card)
		if !ok {
			t.Fatalf("%s absent from corpus", card)
		}
		for _, req := range levelb.Requirements(c) {
			if req.Key == key && req.Sub == "static.continuous" {
				return GenerateB(reg, card, req)
			}
		}
		t.Fatalf("%s has no static.continuous %s", card, key)
		return oraclegen.Item{}, nil
	}
	for _, row := range []struct{ card, key string }{
		{"Dead Weight", "static#0.0"}, {"Swampsnare Trap", "static#0.1"},
	} {
		t.Run(row.card, func(t *testing.T) {
			it, skip := gen(row.card, row.key)
			if skip != nil {
				t.Fatalf("skip: %s", skip.Reason)
			}
			res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
			if err != nil || len(res.Fails) != 0 || len(res.Snapshots) == 0 {
				t.Fatalf("scenario did not replay: err=%v fails=%v", err, res.Fails)
			}
			last := res.Snapshots[len(res.Snapshots)-1]
			card, ok := reg.Lookup(row.card)
			if !ok || len(card.Faces) == 0 {
				t.Fatalf("%s has no face", row.card)
			}
			f := card.Faces[0]
			st, _ := staticSlotOf(f, levelb.Requirement{Slot: "0"})
			var host rules.OracleSnapPerm
			for _, p := range last.Permanents {
				if p.Name == "Gigantosaurus" && p.Controller == 0 {
					host = p
				}
			}
			specs := staticProbeSpecs(reg, []string{"Gigantosaurus"})
			spec, hasSpec := specs["Gigantosaurus"]
			if host.Name == "" || !hasSpec || host.PT == spec.pt {
				t.Fatalf("precondition/effect: living p0 Gigantosaurus P/T %q did not move from printed %q: %+v", host.PT, spec.pt, host)
			}
			if !staticObserved(last, f, row.card, st, specs) {
				t.Fatalf("static not observable on surviving Aura host")
			}
		})
	}
	for _, row := range []struct{ card, key string }{
		{"Glider Staff", "static#0.0"}, {"Meteor Sword", "static#0.0"}, {"Bespoke Bō", "static#0.0"},
	} {
		t.Run(row.card, func(t *testing.T) {
			it, skip := gen(row.card, row.key)
			if skip != nil {
				t.Fatalf("skip: %s", skip.Reason)
			}
			var hasAnswer, redirected bool
			for _, step := range it.Steps {
				for _, answer := range step.Answers {
					if answer.Kind == "target" {
						hasAnswer = true
					}
					for _, pick := range answer.Pick {
						redirected = redirected || pick == "p1:Grizzly Bears"
					}
				}
			}
			if !hasAnswer || (row.card == "Meteor Sword" && !redirected) || (row.card != "Meteor Sword" && redirected) {
				t.Fatalf("precondition: wrong ETB target choice (redirected=%v): %+v", redirected, it.Steps)
			}
			res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
			if err != nil || len(res.Fails) != 0 || len(res.Snapshots) == 0 {
				t.Fatalf("scenario did not replay: err=%v fails=%v", err, res.Fails)
			}
			last := res.Snapshots[len(res.Snapshots)-1]
			card, ok := reg.Lookup(row.card)
			if !ok || len(card.Faces) == 0 {
				t.Fatalf("%s has no face", row.card)
			}
			f := card.Faces[0]
			st, _ := staticSlotOf(f, levelb.Requirement{Slot: "0"})
			var live bool
			for _, p := range last.Permanents {
				if p.Name == "Grizzly Bears" && p.Controller == 0 {
					live = p.PT != "2/2" || len(p.Keywords) != 0
				}
			}
			if !live || !staticObserved(last, f, row.card, st, staticProbeSpecs(reg, []string{staticProbe})) {
				t.Fatalf("precondition/effect: p0 probe is not alive with a visible static effect; %s", strings.Join(res.Transcript, "\n"))
			}
		})
	}
	if _, skip := gen("Crystal Barricade", "static#0.0"); skip == nil || !strings.HasSuffix(skip.Reason, "effect not observable on a probe or the card") {
		t.Fatalf("out-of-scope Crystal Barricade skip = %+v", skip)
	}
}
