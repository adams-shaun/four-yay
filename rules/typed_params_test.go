package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
)

// TestCharmKnownKeysMatchTheCensus holds the modal compiler's known-key table
// (behind CharmParams.Unread and effCharm's loud Note) equal to the census.
// Charm and GenericChoice share effCharm and so one table, which must equal
// each API's census on its own.
func TestCharmKnownKeysMatchTheCensus(t *testing.T) {
	checkKnownKeysMatchTheCensus(t, "Charm", effects.CharmKnownKeys())
	checkKnownKeysMatchTheCensus(t, "GenericChoice", effects.CharmKnownKeys())
}

// TestPumpKnownKeysMatchTheCensus is the same check for api:Pump.
func TestPumpKnownKeysMatchTheCensus(t *testing.T) {
	checkKnownKeysMatchTheCensus(t, "Pump", effects.PumpKnownKeys())
}

// TestDrawKnownKeysMatchTheCensus is the same check for api:Draw.
func TestDrawKnownKeysMatchTheCensus(t *testing.T) {
	checkKnownKeysMatchTheCensus(t, "Draw", effects.DrawKnownKeys())
}
