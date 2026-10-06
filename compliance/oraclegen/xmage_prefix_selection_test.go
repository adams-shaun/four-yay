package oraclegen_test

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// Distinct loyalty costs are not necessarily distinct startsWith selectors:
// -1 selects both -1 and -10 until its colon is included. The intrinsic mana
// ability of Murmuring Bosk participates even though it has no printed line.
func TestXMageAbilityPrefixSelectionCorpus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		card string
		want []string
	}{
		{"Jace Beleren", []string{"+2", "-1:", "-10"}},
		{"Teferi, Temporal Archmage", []string{"+1", "-1:", "-10"}},
		{"Jace, the Mind Sculptor", []string{"+2", "0", "-1:", "-12"}},
		{"Mr. Monopoly, On the Go", []string{"0", "-2", "-4:", "-40"}},
		{"Murmuring Bosk", []string{"{T}: Add {W}", "{T}: Add {G}."}},
	} {
		t.Run(tc.card, func(t *testing.T) {
			c, ok := reg.Lookup(tc.card)
			if !ok || len(c.Faces) == 0 {
				t.Fatal("precondition: corpus face absent")
			}
			f := c.Faces[0]
			if len(f.Abilities) != len(tc.want) {
				t.Fatalf("precondition: %d IR abilities, want %d", len(f.Abilities), len(tc.want))
			}
			// Model XMage's full lines independently of the mapper: strip
			// Oracle loyalty brackets and include the unprinted Forest mana.
			var full []string
			for _, raw := range strings.Split(strings.ReplaceAll(f.Oracle, `\n`, "\n"), "\n") {
				line := strings.TrimSpace(raw)
				if !strings.Contains(line, ": ") || strings.HasPrefix(line, "(") {
					continue
				}
				if strings.HasPrefix(line, "[") {
					line = strings.Replace(strings.TrimPrefix(line, "["), "]", "", 1)
				}
				full = append(full, strings.ReplaceAll(line, f.Name, "{this}"))
			}
			if tc.card == "Murmuring Bosk" {
				if f.Abilities[1].Line != "intrinsic: basic land mana" || f.Abilities[1].ParamStr(cards.PKProduced) != "G" {
					t.Fatal("precondition: Forest intrinsic missing")
				}
				full = append(full, "{T}: Add {G}.")
			}
			if len(full) != len(tc.want) {
				t.Fatalf("precondition: %d full lines, want %d", len(full), len(tc.want))
			}
			// Prove the short cost is actually ambiguous in this setup.
			short := "-1"
			if tc.card == "Mr. Monopoly, On the Go" {
				short = "-4"
			} else if tc.card == "Murmuring Bosk" {
				short = "{T}"
			}
			matches := 0
			for _, line := range full {
				if strings.HasPrefix(line, short) {
					matches++
				}
			}
			if matches != 2 {
				t.Fatalf("precondition: %q selects %d lines, want 2", short, matches)
			}
			got, why := oraclegen.XMageAbility(f)
			if why != "" || len(got) != len(tc.want) {
				t.Fatalf("mapping = %v, %q; want %d unique selectors", got, why, len(tc.want))
			}
			for i, want := range tc.want {
				if !f.Abilities[i].IsActivated() {
					t.Fatalf("precondition: slot %d is not activated", i)
				}
				if got[i] != want {
					t.Errorf("slot %d = %q, want %q", i, got[i], want)
				}
				assertXMagePrefixSelectsOnly(t, got[i], i, full)
				assertXMagePrefixSelectsOnly(t, got[i], i, tc.want)
			}
		})
	}
}

func assertXMagePrefixSelectsOnly(t *testing.T, prefix string, own int, lines []string) {
	t.Helper()
	for j, line := range lines {
		if strings.HasPrefix(line, prefix) != (own == j) {
			t.Errorf("prefix %q selects line %d (%q), want only %d", prefix, j, line, own)
		}
	}
}

func TestXMageAbilityIntrinsicSelectionOrder(t *testing.T) {
	for _, intrinsicFirst := range []bool{false, true} {
		f := &cards.Face{
			Name:   "Test Forest",
			Oracle: "{T}: Add {W} or {B}.",
			Abilities: []*cards.SA{
				{Kind: "AB"},
				{Kind: "AB", Line: "intrinsic: basic land mana", Params: map[string]string{"Produced": "G"}},
			},
		}
		printed, intrinsic := 0, 1
		if intrinsicFirst {
			f.Abilities[0], f.Abilities[1] = f.Abilities[1], f.Abilities[0]
			printed, intrinsic = 1, 0
		}
		if !f.Abilities[printed].IsActivated() || f.Abilities[intrinsic].Line != "intrinsic: basic land mana" {
			t.Fatal("precondition: printed and intrinsic slots not distinct")
		}
		got, why := oraclegen.XMageAbility(f)
		if why != "" || len(got) != 2 || got[printed] != "{T}: Add {W}" || got[intrinsic] != "{T}: Add {G}." {
			t.Fatalf("intrinsicFirst=%v: mapping = %v, %q", intrinsicFirst, got, why)
		}
		full := make([]string, 2)
		full[printed], full[intrinsic] = f.Oracle, "{T}: Add {G}."
		assertXMagePrefixSelectsOnly(t, got[printed], printed, full)
		assertXMagePrefixSelectsOnly(t, got[intrinsic], intrinsic, full)
		// A complete intrinsic line dominated by a printed ability cannot
		// be extended; it must fail closed, not emit a conflicting prefix.
		f.Oracle = "{T}: Add {G}. Gain 1 life."
		got, why = oraclegen.XMageAbility(f)
		if got != nil || why != "activate xmage text ambiguous" {
			t.Fatalf("dominated intrinsic = %v, %q; want ambiguous", got, why)
		}
	}
}
