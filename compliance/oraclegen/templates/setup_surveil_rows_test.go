package templates

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// The three stored `diverge` verdict rows whose only difference was
// `setup p0.graveyard` (ticket D1, class level-B setup arrange fallback):
// XMage's unscripted default sends the upkeep-surveil card to the graveyard,
// so gorge's setup-drive fallback must too. generateServed pins the replay
// against the live corpus; a generator regression that unserves a row fails
// at the generate step.
func TestSetupParityD1SurveilRows(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, row := range []struct {
		name string
		gy   []string
	}{
		{"Broodheart Engine", []string{"Grizzly Bears", "Wastes"}},
		{"Essence Anchor", []string{"Nessian Asp", "Wastes"}},
		{"Morcant's Eyes", []string{"Wastes"}},
	} {
		t.Run(row.name, func(t *testing.T) {
			_, res := generateServed(t, reg, row.name, "activate#0.0")
			snap := res.Snapshots[0]
			if snap.Checkpoint != "setup" {
				t.Fatalf("precondition: %s checkpoint = %s, want setup", row.name, snap.Checkpoint)
			}
			gy := snap.Players[0].Graveyard
			if !reflect.DeepEqual(gy, row.gy) {
				t.Errorf("%s setup p0 graveyard = %v, want %v (the stored XMage list)", row.name, gy, row.gy)
			}
		})
	}
}
