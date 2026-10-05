package cards

import (
	"sort"
	"strings"
	"testing"
)

// The etbCounter condition-field census.
//
// `K:etbCounter:<kind>:<n>:<condition>:<description>` carries a condition
// gate in its first extra colon field: Revolt$ True (a permanent you
// controlled left the battlefield this turn), Adamant$ <colour|Any> (at
// least three mana of that colour -- or of one same colour -- was spent to
// cast it), IsPresent$ <spec> (a permanent matching <spec> is on the
// battlefield). The expander must preserve each gate in the compiled
// replacement; dropping one applies the counters unconditionally, which is
// exactly the Putrid Pals defect (5/5 for a 3/3 on a non-Revolt turn).
//
// This is a CORPUS-WIDE census ratchet: it walks every compiled card's
// etbCounter keyword, asserts the compiled replacement carries the matching
// condition parameter, and pins the complete set of carrier card names.
// Adding a new carrier -- or losing the gate from an existing one -- changes
// the pinned set and fails loudly, so the next etbCounter condition cannot
// silently join the corpus ungated.

// etbCounterConditionCarriers is the COMPLETE corpus set (sorted) of cards
// whose etbCounter keyword carries a condition field. Pinned from
// .cards/cardsfolder at the corpus pin the repo currently builds; a new
// carrier must be added here deliberately, together with a real behavioural
// test for its shape.
var etbCounterConditionCarriers = []string{
	"Ardenvale Paladin",
	"Ascendant Packleader",
	"Embereth Paladin",
	"Greenwheel Liberator",
	"Henge Walker",
	"Locthwain Paladin",
	"Narnam Renegade",
	"Night Market Aeronaut",
	"Putrid Pals",
	"Vantress Paladin",
	"Garenbrig Paladin",
}

// TestEtbCounterConditionCensus pins the corpus carriers and their compiled
// gates. It also proves the census is non-vacuous: at least one carrier of
// each condition family must exist, so a corpus move that dropped them all
// cannot pass by emptiness.
func TestEtbCounterConditionCensus(t *testing.T) {
	t.Parallel()
	reg := compiledCorpus(t)
	got := map[string]bool{}
	families := map[string]bool{}
	for _, c := range reg.Cards {
		for fi := range c.Faces {
			f := c.Faces[fi]
			for _, kw := range f.Keywords {
				if !strings.HasPrefix(kw, "etbCounter:") {
					continue
				}
				cond := etbCounterConditionField(kw)
				if cond == "" {
					continue
				}
				got[c.Faces[0].Name] = true
				field, _, _ := strings.Cut(cond, "$")
				families[strings.TrimSpace(field)] = true
				assertEtbCounterCompiledGate(t, f, cond)
			}
		}
	}
	if !families["Revolt"] || !families["Adamant"] || !families["IsPresent"] {
		t.Fatalf("census is vacuous: condition families seen = %v, want Revolt, Adamant and IsPresent present", families)
	}
	names := make([]string, 0, len(got))
	for name := range got {
		names = append(names, name)
	}
	sort.Strings(names)
	want := append([]string(nil), etbCounterConditionCarriers...)
	sort.Strings(want)
	if strings.Join(names, "\n") != strings.Join(want, "\n") {
		t.Fatalf("etbCounter condition carriers changed:\n got: %q\nwant: %q", names, want)
	}
}

// etbCounterConditionField extracts the first extra colon field of an
// etbCounter keyword when that field is a condition gate (a `Key$ value`
// fragment naming one of the recognised condition keys), else "".
func etbCounterConditionField(kw string) string {
	rest := strings.TrimPrefix(kw, "etbCounter:")
	// strip <kind>:<n>:
	for i := 0; i < 2; i++ {
		j := strings.IndexByte(rest, ':')
		if j < 0 {
			return ""
		}
		rest = rest[j+1:]
	}
	first, _, _ := strings.Cut(rest, ":")
	if !strings.Contains(first, "$") {
		return ""
	}
	name, _, _ := strings.Cut(strings.TrimSpace(first), "$")
	switch strings.TrimSpace(name) {
	case "Revolt", "Adamant", "IsPresent":
		return strings.TrimSpace(first)
	}
	return ""
}

// assertEtbCounterCompiledGate checks that the compiled replacement for an
// etbCounter condition keyword carries the condition parameter the expander
// promises. A missing or mismatched parameter is the silent-drop defect.
func assertEtbCounterCompiledGate(t *testing.T, f *Face, cond string) {
	t.Helper()
	field, val, _ := strings.Cut(cond, "$")
	key := strings.TrimSpace(field)
	val = strings.TrimSpace(val)
	var repl *Repl
	for i := range f.Repls {
		if _, ok := f.Repls[i].Params["KeywordLine"]; ok && strings.HasPrefix(f.Repls[i].Params["KeywordLine"], "etbCounter:") {
			repl = &f.Repls[i]
			break
		}
	}
	if repl == nil {
		t.Fatalf("%s: etbCounter keyword %q produced no compiled replacement", f.Name, cond)
	}
	switch key {
	case "Revolt":
		if repl.Params["Revolt"] != val {
			t.Fatalf("%s: Revolt gate = %q, want %q; params=%v", f.Name, repl.Params["Revolt"], val, repl.Params)
		}
	case "IsPresent":
		if repl.Params["IsPresent"] != val {
			t.Fatalf("%s: IsPresent gate = %q, want %q; params=%v", f.Name, repl.Params["IsPresent"], val, repl.Params)
		}
	case "Adamant":
		want := "Count$Adamant_3." + val + ".1.0"
		if repl.Params["CheckSVar"] != want {
			t.Fatalf("%s: Adamant gate = %q, want %q; params=%v", f.Name, repl.Params["CheckSVar"], want, repl.Params)
		}
	}
}
