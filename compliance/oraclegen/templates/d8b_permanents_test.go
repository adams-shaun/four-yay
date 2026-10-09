package templates_test

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen/templates"
	"github.com/adams-shaun/gorge/rules"
)

// runRow generates one level-B item for card/template and replays it on the
// gorge side, failing the test on a generator skip or a scenario failure.
func runRow(t *testing.T, reg *cards.Registry, card, template string) rules.OracleResult {
	t.Helper()
	item, skip := templates.ItemFor(reg, card, template)
	if skip != nil {
		t.Fatalf("%s/%s: generator skip: %s", card, template, skip.Reason)
	}
	res, err := rules.RunOracleScenarioJSON(reg, item.Raw())
	if err != nil {
		t.Fatalf("%s/%s: scenario error: %v", card, template, err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("%s/%s: scenario fails: %v", card, template, res.Fails)
	}
	return res
}

// permOf finds one permanent in a snapshot by face name.
func snapPerm(t *testing.T, snap rules.OracleSnapshot, name string) rules.OracleSnapPerm {
	t.Helper()
	for _, p := range snap.Permanents {
		if cards.NormalizeName(p.Name) == cards.NormalizeName(name) {
			return p
		}
	}
	t.Fatalf("no permanent named %q in checkpoint %q", name, snap.Checkpoint)
	return rules.OracleSnapPerm{}
}

// TestDoranPumpXIsPowerMinusToughness pins the d8b-permanents fix on the
// rows' own scenario: Doran's attack pump reads |power - toughness| through
// the SVar chain (X1 = SVar$Y1/Abs, Y1 = TriggeredAttacker$CardPower/Minus.Z1),
// so a 0/5 Doran attacking resolves 5/10, and a 2/2 blocker's zero difference
// leaves it 2/2. Before the operand-aware read the chain answered power
// alone: Doran pumped +0/+0, the blocker +2/+2.
func TestDoranPumpXIsPowerMinusToughness(t *testing.T) {
	reg := corpusReg(t)
	res := runRow(t, reg, "Doran, Besieged by Time", "trigger#0.0")
	doran := snapPerm(t, res.Snapshots[len(res.Snapshots)-1], "Doran, Besieged by Time")
	if doran.PT != "5/10" {
		t.Fatalf("attacking Doran = %s, want 5/10 (|0-5| pump applied)", doran.PT)
	}
	// The Blocks twin (trigger#0.1): p0's bear blocks p1's bear; a 2/2's
	// difference is 0, so the blocker stays 2/2.
	res2 := runRow(t, reg, "Doran, Besieged by Time", "trigger#0.1")
	var blocker *rules.OracleSnapPerm
	for _, snap := range res2.Snapshots {
		for i := range snap.Permanents {
			p := &snap.Permanents[i]
			if cards.NormalizeName(p.Name) == cards.NormalizeName("Grizzly Bears") &&
				p.Controller == 0 && p.Blocking {
				blocker = p
			}
		}
	}
	if blocker == nil {
		t.Fatal("no blocking p0 Grizzly Bears in the trigger#0.1 replay")
	}
	if blocker.PT != "2/2" {
		t.Fatalf("blocking bear = %s, want 2/2 (zero difference pumps nothing)", blocker.PT)
	}
}

// TestZhaoRemoveLandTypesStripsPrintedForest pins the land-type vocabulary
// fix (NamedCorpus fallback): Zhao's continuous static (RemoveLandTypes$
// True, gated on his conqueror counter) makes nonbasic lands lose their
// printed land types, so a setup Dryad Arbor reads Land+Mountain without
// Forest. Before the fallback the harness's nil NameUniverse built an empty
// vocabulary and the strip silently removed nothing.
func TestZhaoRemoveLandTypesStripsPrintedForest(t *testing.T) {
	reg := corpusReg(t)
	res := runRow(t, reg, "Zhao, the Moon Slayer", "static#0.0")
	arbor := snapPerm(t, res.Snapshots[0], "Dryad Arbor")
	if slices.Contains(arbor.Types, "Forest") {
		t.Fatalf("Dryad Arbor keeps Forest under Zhao's strip: %v", arbor.Types)
	}
	if !slices.Contains(arbor.Types, "Mountain") {
		t.Fatalf("Dryad Arbor did not gain Mountain: %v", arbor.Types)
	}
	if !slices.Contains(arbor.Types, "Land") || !slices.Contains(arbor.Types, "Dryad") {
		t.Fatalf("the strip ate a non-land type word: %v", arbor.Types)
	}
}

// TestFaceDownDetectiveEntryTriggersNothing pins the CR 708.8 gate on the
// rows' own scenarios: a face-down Basilica Stalker entering disguised is not
// a Detective, so neither the Case's ETB counter trigger nor Perimeter
// Enforcer's +1/+1 pump fires. Before the gate the printed face matched:
// the Case's creature carried a +1/+1 counter at its first resolve and the
// Enforcer read 2/2.
func TestFaceDownDetectiveEntryTriggersNothing(t *testing.T) {
	reg := corpusReg(t)
	res := runRow(t, reg, "Case of the Pilfered Proof", "trigger#0.1")
	// The divergence checkpoint: step 1 (resolve), the disguise's entry.
	var facedown *rules.OracleSnapPerm
	for _, snap := range res.Snapshots {
		if snap.Checkpoint != "step 1 (resolve)" {
			continue
		}
		for i := range snap.Permanents {
			p := &snap.Permanents[i]
			if p.FaceDown {
				facedown = p
			}
		}
	}
	if facedown == nil {
		t.Fatal("no face-down permanent at the disguise's first resolve")
	}
	if len(facedown.Counters) != 0 {
		t.Fatalf("face-down creature entered with counters %v (a printed-subtype trigger fired)", facedown.Counters)
	}
	// The Enforcer twin: its own +1/+1 pump must not fire either.
	res2 := runRow(t, reg, "Perimeter Enforcer", "trigger#0.1")
	for _, snap := range res2.Snapshots {
		if snap.Checkpoint != "step 1 (resolve)" {
			continue
		}
		enforcer := snapPerm(t, snap, "Perimeter Enforcer")
		if enforcer.PT != "1/1" {
			t.Fatalf("Enforcer = %s after the face-down entry, want 1/1 (the pump must not fire)", enforcer.PT)
		}
		return
	}
	t.Fatal("no step 1 (resolve) checkpoint in the Enforcer replay")
}
