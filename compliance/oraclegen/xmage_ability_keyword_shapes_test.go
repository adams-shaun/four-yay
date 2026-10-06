package oraclegen_test

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/internal/testutil"
)

func TestXMageAbilityKeywordShapes(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		card string
		want map[int]string
	}{
		{"Hobbit Hole", map[int]string{0: "{T}, Sacrifice {this}", 1: "Halflingcycling {4}"}},
		{"Cool but Rude", map[int]string{0: "{1}{R}: Level 2", 1: "{1}{R}: Level 3"}},
		{"Leader's Talent", map[int]string{0: "{2}{W}: Level 2", 1: "{3}{W}: Level 3"}},
		{"Bard's Bow", map[int]string{0: "<i>Perseus's Bow</i> &mdash; Equip {6}"}},
		{"Dragoon's Lance", map[int]string{0: "<i>Gae Bolg</i> &mdash; Equip {4}"}},
		{"Cori-Steel Cutter", map[int]string{0: "Equip {1}{R}"}},
		{"Caduceus Staff of Hermes", map[int]string{0: "Equip {W}{W}"}},
		{"A.I.M. Scientists", map[int]string{0: "Basic landcycling {2}"}},
		{"Silver-Fur Master", map[int]string{0: "Ninjutsu {U}{B}"}},
		// Forge spells these costs in a collapsed form ("T W", "UB", "BP BP",
		// "4 R XMin1 ...") that does not round-trip to the printed braces, so
		// the line must be matched on its own shape, not on Forge's cost.
		{"A-Silver-Fur Master", map[int]string{0: "Ninjutsu {U/B}"}},
		{"Abzan Battle Priest", map[int]string{0: "Outlast {W}"}},
		{"Architects of Will", map[int]string{0: "Cycling {U/B}"}},
		{"Saheeli's Lattice", map[int]string{0: "Craft with one or more Dinosaurs {4}{R}"}},
		{"Lashwrithe", map[int]string{0: "Equip {B/P}{B/P}"}},
		// Two Equip lines on one face are told apart by the printed cost.
		{"Thinking Cap", map[int]string{0: "Equip Detective {1}", 1: "Equip {3}"}},
		{"Ace's Baseball Bat", map[int]string{0: "Equip legendary creature {1}", 1: "Equip {3}"}},
		// The static "Equip abilities you activate ... cost {1} less" line is
		// prose, not the keyword's own line.
		{"Bladehold War-Whip", map[int]string{0: "Equip {3}{R}{W}"}},
	} {
		t.Run(tc.card, func(t *testing.T) {
			c, ok := reg.Lookup(tc.card)
			if !ok || len(c.Faces) == 0 {
				t.Fatalf("precondition: %s missing from corpus", tc.card)
			}
			got, why := oraclegen.XMageAbility(c.Faces[0])
			if why != "" {
				t.Fatalf("mapping ambiguous: %s", why)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("mapped %v, want %v", got, tc.want)
			}
			for i, want := range tc.want {
				if got[i] != want {
					t.Errorf("ability %d prefix = %q, want %q", i, got[i], want)
				}
			}
		})
	}
}

// TestXMageAbilityKeywordLinesDoNotRegress maps every face whose only
// activated abilities are keyword ABs and whose Oracle prints each keyword
// exactly once. Such a face has nothing ambiguous in it, so any failure is a
// keyword-line matcher bug (the cost-spelling regression: Outlast, hybrid
// Cycling, Craft). The corpus uniqueness ratchet skips every unmapped face and
// cannot see one.
func TestXMageAbilityKeywordLinesDoNotRegress(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	checked := map[string]int{}
	for _, card := range reg.Cards {
		for _, face := range card.Faces {
			keywords := map[string]int{}
			onlyKeywords := true
			for _, sa := range face.Abilities {
				if !sa.IsActivated() {
					continue
				}
				kw := sa.ParamStr(cards.PKKeyword)
				if kw == "" {
					onlyKeywords = false
					break
				}
				keywords[kw]++
			}
			if !onlyKeywords || len(keywords) == 0 {
				continue
			}
			printedOnce := true
			for kw, n := range keywords {
				if n != 1 || kw == "Class" || kw == "TypeCycling" || kw == "Level up" {
					printedOnce = false
					break
				}
				lines := 0
				for _, raw := range strings.Split(strings.ReplaceAll(face.Oracle, "\n", `\n`), `\n`) {
					line := strings.TrimSpace(raw)
					if strings.HasPrefix(line, kw+" ") || strings.HasPrefix(line, kw+"{") {
						lines++
					}
				}
				if lines != 1 {
					printedOnce = false
					break
				}
			}
			if !printedOnce {
				continue
			}
			for kw := range keywords {
				checked[kw]++
			}
			if _, why := oraclegen.XMageAbility(face); why != "" {
				t.Errorf("%s: single keyword-only face is unmapped: %s", face.Name, why)
			}
		}
	}
	for _, kw := range []string{"Outlast", "Cycling", "Craft", "Equip", "Ninjutsu"} {
		if checked[kw] == 0 {
			t.Errorf("precondition: no corpus face exercised keyword %s", kw)
		}
	}
}
