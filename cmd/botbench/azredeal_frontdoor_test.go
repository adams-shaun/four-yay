package main

// BP-16 seats az-redeal through bots/azredeal.New, which builds the az seat
// with a nil net: the hosted entry is generation 0 by construction. A
// -checkpoint model can no longer drive it, so azFrontDoor refuses loudly
// (cmd/botbench/azcost.go) instead of silently benching a different bot than
// -checkpoint names.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/azmcts"
	"github.com/adams-shaun/gorge/internal/policynet"
)

func TestAZFrontDoorRefusesCheckpointOnHostedRedeal(t *testing.T) {
	prevCfg, prevWorld, prevCorpus := azCfg, azWorldArg, azCorpusPath
	t.Cleanup(func() { azCfg, azWorldArg, azCorpusPath = prevCfg, prevWorld, prevCorpus })
	azCfg = azmcts.DefaultSeatConfig()
	azWorldArg = azmcts.WorldRedeal
	azCorpusPath = ""
	m := &policynet.Model{ValueHidden: 1, Features: policynet.FeaturesMZ}
	if err := azFrontDoor("az-redeal", "bot", m); err == nil || !strings.Contains(err.Error(), "generation 0") {
		t.Fatalf("checkpoint with hosted az-redeal: err = %v, want the generation-0 refusal", err)
	}
	// The refusal is the checkpoint's, not the world's: the same lineup
	// without a model is accepted, and the checkpointed honest-world shape
	// (policy az with -az-world redeal) stays available.
	if err := azFrontDoor("az-redeal", "bot", nil); err != nil {
		t.Fatalf("az-redeal without -checkpoint: err = %v, want nil", err)
	}
	if err := azFrontDoor("az", "bot", m); err != nil {
		t.Fatalf("az with -az-world redeal and -checkpoint: err = %v, want nil", err)
	}
}
