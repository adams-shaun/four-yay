package rules

// W3 step 2's dual-run tests for the zone movers' converted asks: the
// library search (pick, Optional$ confirmation, may-shuffle), the Defined$
// library fetch's election, the hidden-hand walk (pick and confirmation,
// one and several owners), the public-origin hidden pick (pick and
// confirmation), the player sacrifice (the pick, the strict election, the
// object election), ChangeZone's AlternativeDecider$ and Imprint$ choices,
// its object-path may-shuffle, manifest dread, and the generic mid-resolution
// target asks ("tgts" and ChangeZone's "choice"). Each runs on legacy and on
// the kernel and must stay byte-identical with every ask served from the tape.

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// tapeZoneSorcery is a synthetic one-mana sorcery with the given ability
// lines.
func tapeZoneSorcery(name, body string) string {
	return "Name:" + name + "\nManaCost:B\nTypes:Sorcery\n" + body + "\nOracle:x\n"
}

// tapeZoneETB is a synthetic creature whose ETB trigger runs body (its
// Execute SVar is TrigBody), so a ValidTgts$ sub of the body is the
// mid-resolution "tgts"/"choice" ask no placement or cast covers.
func tapeZoneETB(name, body string) string {
	return "Name:" + name + "\nManaCost:B\nTypes:Creature Rogue\nPT:1/1\n" +
		"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigBody | TriggerDescription$ x\n" +
		body + "\nOracle:x\n"
}

func tapeZoneToGraveyard(n int) func(t *testing.T, e *Engine) {
	return func(t *testing.T, e *Engine) {
		for p := 0; p < len(e.G.Players); p++ {
			for i := 0; i < n; i++ {
				moveByName(t, e, state.PlayerID(p), "Mountain", state.ZGraveyard)
			}
		}
	}
}

func tapeZoneToHand(n int) func(t *testing.T, e *Engine) {
	return func(t *testing.T, e *Engine) {
		for p := 0; p < len(e.G.Players); p++ {
			for i := 0; i < n; i++ {
				moveByName(t, e, state.PlayerID(p), "Mountain", state.ZHand)
			}
		}
	}
}

func tapeZoneToBattlefield(n int) func(t *testing.T, e *Engine) {
	return func(t *testing.T, e *Engine) {
		for p := 0; p < len(e.G.Players); p++ {
			for i := 0; i < n; i++ {
				moveByName(t, e, state.PlayerID(p), "Mountain", state.ZBattlefield)
			}
		}
	}
}
