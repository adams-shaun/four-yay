package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

func TestStaticContinuousEquipmentAttachesToProbe(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, name := range []string{"Goblin Plate Mail", "Short Bow"} {
		t.Run(name, func(t *testing.T) {
			card, ok := reg.Lookup(name)
			if !ok || len(card.Faces) == 0 {
				t.Fatalf("precondition: %s missing from corpus", name)
			}
			var req levelb.Requirement
			for _, candidate := range levelb.Requirements(card) {
				if candidate.Sub == "static.continuous" {
					req = candidate
					break
				}
			}
			if req.Sub == "" {
				t.Fatalf("precondition: %s has no static.continuous requirement", name)
			}
			item, skip := GenerateB(reg, name, req)
			if skip != nil {
				t.Fatalf("%s unexpectedly skipped: %s", name, skip.Reason)
			}
			foundAttach := false
			for _, step := range item.Steps {
				if step.Op == "attach" {
					foundAttach = true
					if step.Card != "p0:"+name || step.AttachedTo != "p0:Grizzly Bears" {
						t.Fatalf("attach step = %+v", step)
					}
				}
			}
			if !foundAttach {
				t.Fatalf("scenario has no attach step: %+v", item.Steps)
			}
			res, err := rules.RunOracleScenarioJSON(reg, item.Raw())
			if err != nil || len(res.Fails) != 0 || len(res.Snapshots) == 0 {
				t.Fatalf("scenario replay failed: err=%v fails=%v", err, res.Fails)
			}
			probeFound, changed := false, false
			for _, permanent := range res.Snapshots[len(res.Snapshots)-1].Permanents {
				if permanent.Name == "Grizzly Bears" && permanent.Controller == 0 {
					probeFound = true
					changed = permanent.PT != "2/2" || len(permanent.Keywords) != 0
				}
			}
			if !probeFound || !changed {
				t.Fatalf("precondition/effect: p0 probe found=%t changed=%t, permanents=%+v", probeFound, changed, res.Snapshots[len(res.Snapshots)-1].Permanents)
			}
		})
	}
}

func TestStaticContinuousAuraStillObserved(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Cryoshatter")
	if !ok || len(card.Faces) == 0 {
		t.Fatal("precondition: Cryoshatter missing from corpus")
	}
	var req levelb.Requirement
	for _, candidate := range levelb.Requirements(card) {
		if candidate.Sub == "static.continuous" {
			req = candidate
			break
		}
	}
	if req.Sub == "" {
		t.Fatal("precondition: Cryoshatter has no static.continuous requirement")
	}
	item, skip := GenerateB(reg, "Cryoshatter", req)
	if skip != nil {
		t.Fatalf("Aura unexpectedly skipped: %s", skip.Reason)
	}
	res, err := rules.RunOracleScenarioJSON(reg, item.Raw())
	if err != nil || len(res.Fails) != 0 || len(res.Snapshots) == 0 {
		t.Fatalf("scenario replay failed: err=%v fails=%v", err, res.Fails)
	}
	changed := false
	for _, permanent := range res.Snapshots[len(res.Snapshots)-1].Permanents {
		if permanent.Name == "Grizzly Bears" && permanent.Controller == 1 {
			if permanent.PT != "2/2" {
				changed = true
			}
		}
	}
	if !changed {
		t.Fatalf("Aura did not modify the targeted probe: %+v", res.Snapshots[len(res.Snapshots)-1].Permanents)
	}
}
