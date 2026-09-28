package spellbench

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestFDNCatalogDecks checks every FDN catalog deck parses, is a 40+ card
// limited main deck of FDN Limited cards only, and that FDNPool is exactly
// the catalog. Corpus-free.
func TestFDNCatalogDecks(t *testing.T) {
	universe, err := FDNCards()
	if err != nil {
		t.Fatal(err)
	}
	if len(universe) < 250 {
		t.Fatalf("FDN universe has %d cards", len(universe))
	}
	in := map[string]bool{}
	for _, c := range universe {
		in[c.Name] = true
	}
	ids, err := CatalogIDs(FDNLimited)
	if err != nil {
		t.Fatal(err)
	}
	pool := append([]string(nil), FDNPool...)
	sort.Strings(pool)
	if strings.Join(ids, ",") != strings.Join(pool, ",") {
		t.Fatalf("FDNPool %v != catalog %v", pool, ids)
	}
	for _, id := range ids {
		f, err := File(FDNLimited, id)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, e := range f.Cards {
			n += e.Count
			if !in[e.Name] {
				t.Errorf("%s: %q is not an FDN Limited card", id, e.Name)
			}
		}
		if n < 40 || f.Format != "limited" {
			t.Errorf("%s: %d cards, format %q", id, n, f.Format)
		}
	}
	for _, c := range Catalogs {
		got, err := CatalogByID(c.ID)
		if err != nil || got.Dir != c.Dir {
			t.Errorf("CatalogByID(%q) = %v, %v", c.ID, got, err)
		}
	}
	if c, err := CatalogByID("fdn"); err != nil || c.Dir != FDNLimited {
		t.Errorf("CatalogByID(fdn) = %v, %v", c, err)
	}
}

// TestFDNCoverage reports how much of FDN Limited gorge fully supports: per
// card (reg.Unsupported against effects.Supported, with rules' primitives
// registered by this package's rules import), weighted by 17lands play
// frequency, and per missing primitive. It is a one-way ratchet: it fails
// only when an FDN card that knownUnsupportedFDN does not list is not fully
// supported (a regression, or an unresolved name). A listed card that is now
// supported is logged as stale, not failed, so a primitive landing on main
// never breaks this branch's gate; delete stale rows when you see them.
//
// FDN_COVERAGE_OUT=<file> also writes the gap table as TSV.
func TestFDNCoverage(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	sup := effects.Supported()
	universe, err := FDNCards()
	if err != nil {
		t.Fatal(err)
	}
	type gap struct {
		card    FDNCard
		missing []string
	}
	var gaps []gap
	var tot, okW, totAll, okAll, nonbasic, okCards, nbW, nbOK int
	primW := map[string]int{}
	primCards := map[string][]string{}
	for _, fc := range universe {
		tot += fc.CopiesWR60
		totAll += fc.CopiesAll
		if !fc.Basic {
			nonbasic++
			nbW += fc.CopiesWR60
		}
		c, ok := reg.Lookup(fc.Name)
		if !ok {
			t.Errorf("%q: not in the corpus registry", fc.Name)
			continue
		}
		m := reg.Unsupported(c, sup)
		if len(m) == 0 {
			okW += fc.CopiesWR60
			okAll += fc.CopiesAll
			if !fc.Basic {
				okCards++
				nbOK += fc.CopiesWR60
			}
			if _, listed := knownUnsupportedFDN[fc.Name]; listed {
				t.Logf("stale knownUnsupportedFDN row: %s is now fully supported -- delete it", fc.Name)
			}
			continue
		}
		sort.Strings(m)
		gaps = append(gaps, gap{fc, m})
		for _, p := range m {
			primW[p] += fc.CopiesWR60
			primCards[p] = append(primCards[p], fc.Name)
		}
		if _, listed := knownUnsupportedFDN[fc.Name]; !listed {
			t.Errorf("%s is not fully supported (missing %v) and is not in knownUnsupportedFDN -- a regression, or add the row with a ticket", fc.Name, m)
		}
	}
	sort.Slice(gaps, func(i, j int) bool {
		if gaps[i].card.CopiesWR60 != gaps[j].card.CopiesWR60 {
			return gaps[i].card.CopiesWR60 > gaps[j].card.CopiesWR60
		}
		return gaps[i].card.Name < gaps[j].card.Name
	})
	pct := func(a, b int) float64 { return 100 * float64(a) / float64(max(b, 1)) }
	t.Logf("FDN Limited: %d/%d non-basic cards fully supported (%.1f%%); weighted by wr60 deck copies %.2f%% (non-basic copies %.2f%%), by all-deck copies %.2f%%",
		okCards, nonbasic, pct(okCards, nonbasic), pct(okW, tot), pct(nbOK, nbW), pct(okAll, totAll))
	for _, g := range gaps {
		t.Logf("  %-36s wr60 copies %5d  decks %4d  missing %v", g.card.Name, g.card.CopiesWR60, g.card.DecksWR60, g.missing)
	}
	prims := make([]string, 0, len(primW))
	for p := range primW {
		prims = append(prims, p)
	}
	sort.Slice(prims, func(i, j int) bool {
		if primW[prims[i]] != primW[prims[j]] {
			return primW[prims[i]] > primW[prims[j]]
		}
		return prims[i] < prims[j]
	})
	for _, p := range prims {
		t.Logf("  primitive %-40s wr60 copies %5d  cards %v", p, primW[p], primCards[p])
	}
	if out := os.Getenv("FDN_COVERAGE_OUT"); out != "" {
		var b strings.Builder
		fmt.Fprintf(&b, "# nonbasic_supported=%d/%d weighted_wr60=%.2f weighted_wr60_nonbasic=%.2f weighted_all=%.2f\n", okCards, nonbasic, pct(okW, tot), pct(nbOK, nbW), pct(okAll, totAll))
		b.WriteString("card\tcopies_wr60\tdecks_wr60\tcopies_all\tmissing\n")
		for _, g := range gaps {
			fmt.Fprintf(&b, "%s\t%d\t%d\t%d\t%s\n", g.card.Name, g.card.CopiesWR60, g.card.DecksWR60, g.card.CopiesAll, strings.Join(g.missing, " "))
		}
		if err := os.WriteFile(out, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestFDNCatalogCoverage reports, per FDN catalog deck, the cards gorge does
// not fully support (a report, like TestPauperKernelCoverage).
func TestFDNCatalogCoverage(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	sup := effects.Supported()
	full := 0
	for _, id := range FDNPool {
		cs, err := Deck(reg, FDNLimited, id)
		if err != nil {
			t.Errorf("%s: %v", id, err)
			continue
		}
		seen := map[string]bool{}
		var bad []string
		for _, c := range cs {
			n := c.Faces[0].Name
			if seen[n] {
				continue
			}
			seen[n] = true
			if len(reg.Unsupported(c, sup)) > 0 {
				bad = append(bad, n)
			}
		}
		if len(bad) == 0 {
			full++
		}
		t.Logf("%-10s %d cards, %d distinct, not fully supported: %v", id, len(cs), len(seen), bad)
	}
	t.Logf("%d of %d FDN catalog decks fully supported", full, len(FDNPool))
}
