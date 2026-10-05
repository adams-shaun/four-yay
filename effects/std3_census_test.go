package effects

import (
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// The std3 condition census: the corpus-wide carriers of the two mechanisms
// this ticket modelled (the Adamant Count$ head and the comma-delimited
// WithCountersType$ list).
//
// The Adamant Count$ bodies are pinned as the COMPLETE distinct set the
// corpus authors, and every one must classify as the `Adamant` value head
// and be registered in ModelledValueHeads -- the honesty gate that keeps an
// unmodelled Count$ body out of the playable pool. The comma
// WithCountersType$ carriers are pinned too: the split must emit one counter
// event per kind, and a new comma carrier must be added here deliberately.

// adamantCountBodies is every DISTINCT `Count$Adamant...` body the corpus
// (all .cards/cardsfolder) authors, sorted. It includes both the authored
// trigger/SVar bodies (Count$Adamant_2.Red.2.0, Count$Adamant.Green.1.0)
// and the six the etbCounter expander COMPILES from `Adamant$ <colour|Any>`
// (Count$Adamant_3.<colour>.1.0), which is exactly what proves the expander
// emits a modelled gate for every paladin. A new body -- a new threshold,
// colour or branch shape -- changes this set and fails loudly.
var adamantCountBodies = []string{
	"Count$Adamant.Black.1.0",
	"Count$Adamant.Blue.1.0",
	"Count$Adamant.Colorless.1.0",
	"Count$Adamant.Green.1.0",
	"Count$Adamant.Red.1.0",
	"Count$Adamant.Red.4.3",
	"Count$Adamant.White.1.0",
	"Count$Adamant_1.Colorless.0.1",
	"Count$Adamant_1.Colorless.1.0",
	"Count$Adamant_2.Black.2.0",
	"Count$Adamant_2.Blue.2.0",
	"Count$Adamant_2.Green.2.0",
	"Count$Adamant_2.Red.2.0",
	"Count$Adamant_2.White.2.0",
	"Count$Adamant_3.Any.1.0",
	"Count$Adamant_3.Black.1.0",
	"Count$Adamant_3.Blue.1.0",
	"Count$Adamant_3.Green.1.0",
	"Count$Adamant_3.Red.1.0",
	"Count$Adamant_3.White.1.0",
	"Count$Adamant_7.Red.1.0",
}

// commaWithCountersTypeCarriers is the complete corpus set (sorted) of cards
// whose WithCountersType$ parameter carries a comma-delimited list.
var commaWithCountersTypeCarriers = []string{
	"Gilraen, Dúnedain Protector",
	"Perennation",
}

func TestAdamantCountBodyCensus(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	got := map[string]bool{}
	modelled := ModelledValueHeads()
	for _, c := range reg.Cards {
		for fi := range c.Faces {
			for _, s := range faceStrings(c.Faces[fi]) {
				for _, body := range extractAdamantBodies(s) {
					got[body] = true
					head, ok := cards.ValueHead(body)
					if !ok || head != "Adamant" {
						t.Errorf("%s: ValueHead(%q) = (%q, %v), want (Adamant, true)", c.Faces[0].Name, body, head, ok)
						continue
					}
					if !slices.Contains(modelled, "Adamant") {
						t.Errorf("value head %q is not registered in ModelledValueHeads", head)
					}
				}
			}
		}
	}
	if len(got) == 0 {
		t.Fatal("census is vacuous: no Count$Adamant body found in the corpus")
	}
	names := make([]string, 0, len(got))
	for b := range got {
		names = append(names, b)
	}
	sort.Strings(names)
	want := append([]string(nil), adamantCountBodies...)
	sort.Strings(want)
	if strings.Join(names, "\n") != strings.Join(want, "\n") {
		t.Fatalf("Count$Adamant bodies changed:\n got: %q\nwant: %q", names, want)
	}
}

func TestCommaWithCountersTypeCarrierCensus(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	got := map[string]bool{}
	for _, c := range reg.Cards {
		for fi := range c.Faces {
			if faceHasCommaWithCountersType(c.Faces[fi]) {
				got[c.Faces[0].Name] = true
			}
		}
	}
	if len(got) == 0 {
		t.Fatal("census is vacuous: no comma WithCountersType$ carrier found")
	}
	names := make([]string, 0, len(got))
	for n := range got {
		names = append(names, n)
	}
	sort.Strings(names)
	want := append([]string(nil), commaWithCountersTypeCarriers...)
	sort.Strings(want)
	if strings.Join(names, "\n") != strings.Join(want, "\n") {
		t.Fatalf("comma WithCountersType$ carriers changed:\n got: %q\nwant: %q", names, want)
	}
}

// faceHasCommaWithCountersType reports whether any ability, trigger or
// replacement param on the face is a WithCountersType$ list carrying a comma
// -- either a structured param (Perennation's spell ability) or the RAW
// `SVar:<name>:DB$ ... | WithCountersType$ <list>` spelling Forge uses for a
// named sub-ability (Gilraen's delayed-return TrigReturn), which the parser
// keeps as an SVar string rather than an SA.
func faceHasCommaWithCountersType(f *cards.Face) bool {
	for _, m := range faceParamMaps(f) {
		if v, ok := m["WithCountersType"]; ok && strings.Contains(v, ",") {
			return true
		}
	}
	for _, s := range f.SVars {
		if rest, ok := withCountersTypeValue(s); ok && strings.Contains(rest, ",") {
			return true
		}
	}
	return false
}

// withCountersTypeValue extracts the value of a `WithCountersType$ <list>`
// token inside s, cut at the next ` | ` field boundary or whitespace.
func withCountersTypeValue(s string) (string, bool) {
	i := strings.Index(s, "WithCountersType$")
	if i < 0 {
		return "", false
	}
	rest := s[i+len("WithCountersType$"):]
	if k := strings.IndexAny(rest, "|\t\n"); k >= 0 {
		rest = rest[:k]
	}
	return strings.TrimSpace(rest), true
}

// faceParamMaps returns every param map the face owns: its abilities'
// (recursively through Sub), its triggers' (and their effects'), and its
// replacements' (and their With SA).
func faceParamMaps(f *cards.Face) []map[string]string {
	var out []map[string]string
	var walk func(sa *cards.SA)
	walk = func(sa *cards.SA) {
		if sa == nil {
			return
		}
		out = append(out, sa.Params)
		walk(sa.Sub)
	}
	for _, sa := range f.Abilities {
		walk(sa)
	}
	for _, tr := range f.Triggers {
		out = append(out, tr.Params)
		walk(tr.Effect)
	}
	for _, r := range f.Repls {
		out = append(out, r.Params)
		walk(r.With)
	}
	return out
}

// extractAdamantBodies returns every `Count$Adamant...` body inside s, cut at
// the first whitespace (the corpus prints the body as one space-free token:
// Count$Adamant_2.Red.2.0, Count$Adamant.Green.1.0).
func extractAdamantBodies(s string) []string {
	var out []string
	for i := 0; ; {
		j := strings.Index(s[i:], "Count$Adamant")
		if j < 0 {
			return out
		}
		start := i + j
		rest := s[start:]
		if k := strings.IndexAny(rest, " \t"); k >= 0 {
			rest = rest[:k]
		}
		out = append(out, rest)
		i = start + len(rest)
	}
}

// faceStrings returns every string a face owns (SVars values plus every
// ability/trigger/replacement param value), the same surface Face.Mentions
// scans.
func faceStrings(f *cards.Face) []string {
	out := make([]string, 0, len(f.SVars))
	for _, v := range f.SVars {
		out = append(out, v)
	}
	out = append(out, f.Keywords...)
	for _, m := range faceParamMaps(f) {
		for _, v := range m {
			out = append(out, v)
		}
	}
	return out
}
