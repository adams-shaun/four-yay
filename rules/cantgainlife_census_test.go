package rules

// cantgainlife_census_test.go — the corpus census for the Effect-delivered
// CR 614.1 CantGainLife static (task cantgainlife1). The registered
// restriction is pinned behaviourally in effect_cantgainlife_test.go and the
// Screaming Nemesis oracle scenario covers the real chain; this file proves
// the class by naming every corpus carrier, so a carrier that drifts (or a
// dropped registration entry) fails loudly rather than silently shrinking
// the class being claimed.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/effects"
)

// effectCantGainLifeCarriers is every corpus card measured on 2026-10-01 to
// deliver a CantGainLife static through an Effect's StaticAbilities$ entry:
//
//	for f in $(/usr/bin/grep -rlE 'StaticAbilities\$ [A-Za-z0-9_]+' .cards/cardsfolder); do
//	  /usr/bin/grep -qE 'Mode\$ CantGainLife' "$f" && echo "$f"; done   ==  7
//
// Three write an explicit Duration$ Permanent ("for the rest of the game");
// the other four write none and are this-turn by their own oracle text.
var effectCantGainLifeCarriers = []string{
	"Atarka's Command",
	"Call In a Professional",
	"Roiling Vortex",
	"Screaming Nemesis",
	"Skullcrack",
	"Stigma Lasher",
	"Welcome the Darkness",
}

// TestEffectDeliveredCantGainLifeCensus pins the class: the primitive is
// registered in effects.Supported(), and each of the seven corpus carriers
// still carries an SVar static body `Mode$ CantGainLife` whose ValidPlayer$
// spelling is one the registration path reads, with the expected 3-Persistent
// / 4-absent-Duration split.
func TestEffectDeliveredCantGainLifeCensus(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	if !effects.Supported()["stat:CantGainLife"] {
		t.Fatal("effects.Supported() lacks stat:CantGainLife")
	}
	permanent, absentDuration := 0, 0
	scopes := map[string]int{}
	for _, name := range effectCantGainLifeCarriers {
		c := searchCorpusCard(t, reg, name)
		found := false
		for _, f := range c.Faces {
			for sname, body := range f.SVars {
				if !strings.Contains(body, "Mode$ CantGainLife") {
					continue
				}
				found = true
				if _, rest, ok := strings.Cut(body, "ValidPlayer$ "); ok {
					val := rest
					if i := strings.IndexAny(val, " |\t"); i >= 0 {
						val = val[:i]
					}
					scopes[val]++
				} else {
					t.Errorf("%s: SVar %s carries Mode$ CantGainLife without ValidPlayer$", name, sname)
				}
				// The Duration lives on the granting delivery, not the static
				// body: an SVar chain names it on the DB$ Effect line; a
				// direct A:SP$/A:AB$ Effect ability (Skullcrack, Call In a
				// Professional, Roiling Vortex) carries it in the ability's own
				// params (absent there).
				delivered, dur := false, ""
				for _, dbody := range f.SVars {
					if !strings.Contains(dbody, "StaticAbilities$") || !strings.Contains(dbody, sname) {
						continue
					}
					delivered = true
					if _, rest, ok := strings.Cut(dbody, "Duration$ "); ok {
						dur = rest
						if i := strings.IndexAny(dur, " |"); i >= 0 {
							dur = dur[:i]
						}
					}
				}
				if !delivered {
					for _, ab := range f.Abilities {
						if strings.Contains(ab.Params["StaticAbilities"], sname) {
							delivered = true
							dur = ab.Params["Duration"]
						}
					}
				}
				if !delivered {
					t.Errorf("%s: SVar %s has Mode$ CantGainLife but no StaticAbilities$ delivery", name, sname)
				}
				switch dur {
				case "Permanent":
					permanent++
				case "":
					absentDuration++
				default:
					t.Errorf("%s: unexpected explicit Duration$ %q", name, dur)
				}
			}
		}
		if !found {
			t.Errorf("%s no longer carries an Effect-delivered CantGainLife static", name)
		}
	}
	if permanent != 3 || absentDuration != 4 {
		t.Errorf("CantGainLife census: %d Duration$ Permanent + %d absent-Duration carriers, want 3 + 4", permanent, absentDuration)
	}
	for spec, n := range map[string]int{"You": 1, "Player": 2, "Player.Opponent": 2, "Player.IsRemembered": 2} {
		if scopes[spec] != n {
			t.Errorf("ValidPlayer$ scope census: %q seen %d times, want %d (all scopes: %v)", spec, scopes[spec], n, scopes)
		}
	}
}
