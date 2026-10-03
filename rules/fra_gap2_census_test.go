package rules

import (
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestRealityFractureGap2CardsSupported pins the cards the second Reality
// Fracture gap pass made playable: the noncombat-damage-history readers
// (Grim Repriser, Whiplash Wordsmith, Command the Stage), Loot, the Anomaly's
// CombatDamageNegatePower and Sanctum Lurker's
// IgnorePlaneswalkerZeroLoyaltyRule (the 35th Empower carrier the first pass
// left out), plus Puppet Crafting, whose layer-4 type grant now adds a type
// the object already has only once.
func TestRealityFractureGap2CardsSupported(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	sup := effects.Supported()
	for _, n := range []string{
		"Grim Repriser", "Whiplash Wordsmith", "Command the Stage",
		"Loot, the Anomaly", "Sanctum Lurker", "Puppet Crafting",
	} {
		c := lookup(t, reg, n)
		if u := reg.Unsupported(c, sup); len(u) > 0 {
			t.Errorf("%s: unsupported %v", n, u)
		}
	}
}

// TestRealityFractureGap2ClassCensus walks the WHOLE corpus for every carrier
// of the three primitives this pass implemented -- the
// HasPropertywasDealtNonCombatDamage{ThisTurn,LastTurn} player properties
// (in any PlayerCount group), S:Mode$ CombatDamageNegatePower and S:Mode$
// IgnorePlaneswalkerZeroLoyaltyRule -- and asserts that no carrier's static
// parameter is unread by the code-derived read sets (cardCensusLabels) and
// that every property body names a group the evaluator answers (Players$,
// Opponents$, RegisteredOpponents$ -- hasPropertyStateBacked's three). The
// carrier counts are pinned so a corpus pin bump that adds a carrier is
// re-censused: 3 property readers (2 ThisTurn, 1 LastTurn), 1 NegatePower
// static, 1 zero-loyalty exemption static.
func TestRealityFractureGap2ClassCensus(t *testing.T) {
	_, d := measureParamCensus(t, nil)
	// The read sets must exist, or cardCensusLabels skips the mode's params
	// as "unregistered" and the unread-param check below is vacuous.
	for _, mode := range []string{"CombatDamageNegatePower", "IgnorePlaneswalkerZeroLoyaltyRule"} {
		if !d.stat[mode]["ValidCard"] {
			t.Errorf("no code-derived read of ValidCard$ for stat:%s (read set %v)", mode, d.stat[mode])
		}
	}
	reg := testutil.CorpusRegistry(t)
	var props, negate, zero, bad []string
	for _, c := range reg.Cards {
		var isProp, isNegate, isZero bool
		for _, f := range c.Faces {
			if f == nil {
				continue
			}
			for _, body := range f.SVars {
				i := strings.Index(body, "HasPropertywasDealtNonCombatDamage")
				if i < 0 {
					continue
				}
				isProp = true
				head := body[:i]
				if j := strings.LastIndexAny(head, "|:"); j >= 0 {
					head = head[j+1:]
				}
				head = strings.TrimPrefix(strings.TrimSpace(head), "Count$")
				switch head {
				case "PlayerCountPlayers$", "PlayerCountOpponents$", "PlayerCountRegisteredOpponents$":
				default:
					bad = append(bad, f.Name+": property read through unanswered group "+head)
				}
				prop := body[i:]
				if prop != "HasPropertywasDealtNonCombatDamageThisTurn" && prop != "HasPropertywasDealtNonCombatDamageLastTurn" {
					bad = append(bad, f.Name+": unmodelled property shape "+prop)
				}
			}
			for _, st := range f.Statics {
				switch st.Mode {
				case "CombatDamageNegatePower":
					isNegate = true
				case "IgnorePlaneswalkerZeroLoyaltyRule":
					isZero = true
				}
			}
			for _, body := range f.SVars {
				if strings.Contains(body, "Mode$ CombatDamageNegatePower") || strings.Contains(body, "Mode$ IgnorePlaneswalkerZeroLoyaltyRule") {
					bad = append(bad, f.Name+": SVar-delivered static (Effect path) not modelled")
				}
			}
		}
		if !isProp && !isNegate && !isZero {
			continue
		}
		name := c.Faces[0].Name
		if isProp {
			props = append(props, name)
		}
		if isNegate {
			negate = append(negate, name)
		}
		if isZero {
			zero = append(zero, name)
		}
		for _, l := range cardCensusLabels(c, d, nil) {
			if strings.HasPrefix(l, "param:stat:CombatDamageNegatePower.") || strings.HasPrefix(l, "param:stat:IgnorePlaneswalkerZeroLoyaltyRule.") {
				bad = append(bad, name+": "+l)
			}
		}
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Errorf("class carriers with an unread shape:\n  %s", strings.Join(bad, "\n  "))
	}
	if len(props) != 3 || len(negate) != 1 || len(zero) != 1 {
		t.Errorf("carrier counts moved (re-census the new shapes): noncombat-damage readers %d (want 3) %v, NegatePower %d (want 1) %v, zero-loyalty exemption %d (want 1) %v",
			len(props), props, len(negate), negate, len(zero), zero)
	}
	t.Logf("census: %d noncombat-damage-history readers %v, %d CombatDamageNegatePower %v, %d IgnorePlaneswalkerZeroLoyaltyRule %v; 0 unread class params",
		len(props), props, len(negate), negate, len(zero), zero)
}
