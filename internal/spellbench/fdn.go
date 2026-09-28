package spellbench

import (
	_ "embed"
	"fmt"
	"strconv"
	"strings"
)

// FDNLimited is the FDN Limited catalog directory: sixteen 40-card
// Foundations PremierDraft main decks from 17lands' public replay data
// (CC BY 4.0), drawn from the pool DraftZero (github.com/danieljbrooks/
// draft-zero) defines for FDN Limited -- every deck whose player sits in the
// >= 60% game-win-rate bucket, one per (draft_id, build_index). SpellBench
// lists "FDN Limited" as a proposed benchmark with no definition of its
// own yet; see docs/superpowers/reports/2026-09-28-spellbench-fdn-limited.md.
// scripts/spellbench-fdn-catalog.py regenerates the directory.
const FDNLimited = "decks/fdn-limited"

// FDNPool is the FDN Limited rotating deck pool, in catalog order.
var FDNPool = []string{
	"FDN01-UBG", "FDN02-WG", "FDN03-WB", "FDN04-WB", "FDN05-UG", "FDN06-RG", "FDN07-UR", "FDN08-WBR",
	"FDN09-BG", "FDN10-BR", "FDN11-BWR", "FDN12-UGW", "FDN13-BG", "FDN14-BR", "FDN15-UB", "FDN16-UG",
}

// Catalog is one SpellBench benchmark's deck catalog as gorge serves it.
type Catalog struct {
	ID     string   // the benchmark id (SpellBench's benchmarks/<id>)
	Dir    string   // the embedded deck directory
	Pool   []string // the rotating pool, in benchmark order
	Format string   // the ledger's format field
}

// Catalogs lists the catalogs botbench -spellbench can play.
var Catalogs = []Catalog{
	{ID: "pauper-kernel", Dir: PauperKernel, Pool: BenchmarkPool, Format: "pauper-bo1"},
	{ID: "fdn-limited", Dir: FDNLimited, Pool: FDNPool, Format: "fdn-limited-bo1"},
}

// CatalogByID finds a catalog by id; "fdn" is accepted for fdn-limited.
func CatalogByID(id string) (Catalog, error) {
	if id == "fdn" {
		id = "fdn-limited"
	}
	var ids []string
	for _, c := range Catalogs {
		if c.ID == id {
			return c, nil
		}
		ids = append(ids, c.ID)
	}
	return Catalog{}, fmt.Errorf("spellbench: unknown catalog %q (have %s)", id, strings.Join(ids, ", "))
}

//go:embed fdn-cards.tsv
var fdnCardsTSV string

// FDNCard is one card of the FDN Limited universe (the deck_* columns of
// 17lands' FDN PremierDraft replay data) with its play-frequency weights,
// measured over the distinct decks in the sample the TSV header names.
type FDNCard struct {
	Name  string
	Basic bool
	// CopiesAll counts main-deck copies over every deck in the sample.
	CopiesAll int
	// CopiesWR60 and DecksWR60 count copies and decks over the >= 60%
	// game-win-rate decks (the DraftZero pool).
	CopiesWR60, DecksWR60 int
	// GameCopiesWR60 weights each wr60 deck's copies by the games it played.
	GameCopiesWR60 int
}

// FDNCards parses the embedded FDN Limited card universe.
func FDNCards() ([]FDNCard, error) {
	var out []FDNCard
	header := true
	for i, line := range strings.Split(fdnCardsTSV, "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if header {
			header = false
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			return nil, fmt.Errorf("spellbench: fdn-cards.tsv line %d: %d fields", i+1, len(f))
		}
		n := make([]int, 5)
		for j := range n {
			v, err := strconv.Atoi(f[j+1])
			if err != nil {
				return nil, fmt.Errorf("spellbench: fdn-cards.tsv line %d: %w", i+1, err)
			}
			n[j] = v
		}
		out = append(out, FDNCard{Name: f[0], Basic: n[0] == 1, CopiesAll: n[1], CopiesWR60: n[2], DecksWR60: n[3], GameCopiesWR60: n[4]})
	}
	return out, nil
}
