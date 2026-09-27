// Command forgec fetches the Forge card corpus, compiles it into mtgcore's IR
// cache, and reports how much of it the engine can currently play.
//
// The corpus is GPL-3.0. forgec treats it as a build input: it is fetched into
// a gitignored directory and never redistributed.
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	// Ruling W1: forgec's report command reads effects.Supported() but never
	// otherwise touches rules, so without this blank import none of
	// rules/combat.go's, rules/statics.go's or rules/trigger.go's init()
	// functions (each an effects.RegisterNonAPI call) ever run in this
	// binary, and Supported() here sees "api:" primitives only -- the report
	// undercounts coverage by thousands of cards (trig:ChangesZone alone
	// gates 7008 of them). rules/coverage_test.go's
	// TestForgecBinaryImportsRules pins that this import stays in place.
	_ "github.com/adams-shaun/gorge/rules"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	cmd, args := os.Args[1], os.Args[2:]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	dir := fs.String("dir", ".cards", "working directory for the corpus and IR cache")
	ref := fs.String("ref", "master", "Forge git ref to fetch")
	top := fs.Int("top", 25, "how many missing primitives to list in report")
	doc := fs.String("doc", "docs/coverage.md", "coverage: file to write the full breakdown to")
	readme := fs.String("readme", "README.md", "coverage: file whose marked block carries the summary")
	check := fs.Bool("check", false, "coverage: verify the committed tables are current instead of writing them")
	fs.Parse(args)

	var err error
	switch cmd {
	case "fetch":
		_, err = cards.Fetch(*dir, *ref)
	case "compile":
		err = compile(*dir)
	case "report":
		err = report(*dir, *top)
	case "coverage":
		err = coverageCmd(*dir, *doc, *readme, *check)
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "forgec:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: forgec fetch|compile|report|coverage [-dir .cards] [-ref master] [-top 25]")
	fmt.Fprintln(os.Stderr, "       forgec coverage [-doc docs/coverage.md] [-readme README.md] [-check]")
	os.Exit(2)
}

// cachePath returns dir's fingerprint-keyed cache path for this binary's
// compiler (cards.CachePath). forgec's compile command writes it and the
// report command reads it, so a forgec built from one parser never touches the
// file another parser's forgec uses.
func cachePath(dir string) string { return cards.CachePath(dir) }

func compile(dir string) error {
	r, diags, err := cards.CompileDir(cards.CorpusDir(dir))
	if err != nil {
		return err
	}
	for _, d := range diags {
		fmt.Fprintf(os.Stderr, "%s: %s\n", d.Path, d.Msg)
	}
	cache := cachePath(dir)
	if err := r.Save(cache); err != nil {
		return err
	}
	cards.PruneCaches(dir, cache)
	fmt.Printf("compiled %d cards, %d tokens, %d diagnostics -> %s\n",
		len(r.Cards), len(r.Tokens), len(diags), cache)
	return nil
}

// loadReportRegistry loads dir's IR cache for the report command. When the
// cache is absent, unreadable or stale (e.g. a version bump in cards' cache
// schema), it falls back to compiling dir/cardsfolder fresh in memory — the
// same staleness rule cards.OpenCorpus applies everywhere else. The cache
// name is fingerprint-keyed (cards.CachePath), so a forgec built from a
// different parser never reads another build's IR; the fallback may write
// this binary's own fingerprint file, which no other parser reads.
func loadReportRegistry(dir string) (*cards.Registry, error) {
	r, err := cards.LoadRegistry(cachePath(dir))
	if err == nil {
		return r, nil
	}
	fmt.Fprintf(os.Stderr, "forgec: IR cache unusable (%v); compiling fresh from cardsfolder\n", err)
	return cards.OpenCorpus(dir)
}

func report(dir string, top int) error {
	r, err := loadReportRegistry(dir)
	if err != nil {
		return err
	}
	if l, err := cards.ReadLock(dir); err == nil {
		fmt.Printf("corpus: %s @ %s (%s, %d files)\n", l.Ref, l.Commit, l.License, l.Files)
	}
	// Task 18 wires in the real primitive set; earlier milestones passed an
	// empty map here, which made every card "not playable" but still let the
	// report rank what to build next.
	cv := r.Coverage(effects.Supported())
	fmt.Printf("cards: %d  playable: %d (%.1f%%)\n",
		cv.Cards, cv.Supported, 100*float64(cv.Supported)/float64(cv.Cards))
	fmt.Printf("tokens: %d\n", len(r.Tokens))
	fmt.Println("\ntop missing primitives (cards unlocked):")
	for i, m := range cv.TopMissing(top) {
		fmt.Printf("%3d. %-32s %6d\n", i+1, m.Name, m.Cards)
	}

	// Ruling T16-a.
	if bad := unknownFilterTypes(r); len(bad) > 0 {
		fmt.Println("\nfilter base types matching no card in the registry (likely typos):")
		for _, u := range bad {
			fmt.Printf("%3d. %-32s %6d\n", u.rank, u.token, u.count)
		}
	}
	return nil
}

// validCardFilterKeys is the set of Params keys this scan checks: every
// sa.Params/t.Params/s.Params/r.Params key that is passed directly as the
// spec argument to effects.MatchesSpec / MatchesSpecFrom somewhere in this
// build. That is the whole vocabulary matchesBase's base-type switch
// (effects/filter.go) governs, so it is exactly the set where a typo'd type
// token is dangerous the way Ruling T16-a describes: matchesBase's "non"
// handling negates a base check that never succeeds for anything, so a
// typo'd "non<Type>" silently matches EVERY object instead of none.
//
// The keys, and where each is read: ValidTgts (rules/stack.go),
// ValidCards (effects/combatfx.go, effects/counters.go, effects/damage.go,
// effects/zone.go), ValidCard/ValidBlocker (rules/statics.go),
// ValidTarget/ValidSource (rules/trigger.go), ChangeType
// (effects/zone.go's effChangeZoneAll) and ChangeValid
// (effects/cardflow.go's effDig). Fix round 1 (Important #1): ChangeType and
// ChangeValid were missing from this list even though the doc comment
// claimed a complete audit of every MatchesSpec/MatchesSpecFrom call site --
// they were not, and adding them moved several tokens materially (Remembered
// 1 -> 50, TargetedCard 5 -> 33, Targeted 16 -> 25, plus a dozen new ones).
//
// Deliberately excluded: ValidPlayer$ and ValidActivatingPlayer$ (and
// friends) use a wholly different vocabulary -- You/Opponent/Player/Any via
// MatchesPlayerSpec -- so including them here would just flood this report
// with tokens that were never card types to begin with. Also excluded, and
// genuinely out of scope rather than an oversight: effects/count.go's
// evalCountBody feeds MatchesSpecFrom an `arg` parsed out of a `Count$Valid…`
// SVar *expression body*, not a Params key -- there is no Params key to add
// for it, since the corpus SVar table effects.Ctx.SVars carries is not
// itself walked by this scan.
var validCardFilterKeys = map[string]bool{
	"ValidTgts": true, "ValidCards": true, "ValidCard": true,
	"ValidBlocker": true, "ValidTarget": true, "ValidSource": true,
	"ChangeType": true, "ChangeValid": true,
}

// pseudoFilterTypes are base tokens that are legal in a ValidTgts$-style spec
// but deliberately match no card TYPE at all, so they are not typos:
// "any"/"card"/"permanent"/"spell" are matchesBase's own hard-coded cases
// (effects/filter.go); "player"/"opponent"/"you" are the player-targeting
// alternative rules/stack.go's targetsPlayers recognises and routes to a
// player option before the spec's card-side alternatives ever reach
// matchesBase at all (e.g. "ValidTgts$ Creature,Player" -- a very common
// "target a creature or a player" shape -- would otherwise make this
// report's single biggest false positive).
var pseudoFilterTypes = map[string]bool{
	"any": true, "card": true, "permanent": true, "spell": true,
	"player": true, "opponent": true, "you": true,
}

// maxSAChainWalk bounds SubAbility$ chain walks the same way
// effects.maxChain and cards.Face.Primitives' own walk do, so a
// pathological or cyclic Sub chain in corpus data cannot hang the report.
const maxSAChainWalk = 32

type unknownFilterType struct {
	rank  int
	token string
	count int
}

// unknownFilterTypes implements Ruling T16-a: it collects every base-type
// token named in a ValidTgts$/ValidCards$/ValidCard$/ValidBlocker$/
// ValidTarget$/ValidSource$ spec anywhere in the registry, and flags every
// one that is neither a pseudo type (Any/Card/Permanent/Spell) nor an actual
// type printed on some card -- exactly the class of typo that would make
// engine code silently over-match once it starts reading that spec.
func unknownFilterTypes(r *cards.Registry) []unknownFilterType {
	types := map[string]bool{}
	for _, c := range r.Cards {
		for _, f := range c.Faces {
			for _, t := range f.Types {
				types[strings.ToLower(t)] = true
			}
		}
	}

	counts := map[string]int{}
	note := func(spec string) {
		for _, alt := range strings.Split(spec, ",") {
			alt = strings.TrimSpace(alt)
			if alt == "" {
				continue
			}
			base, _, _ := strings.Cut(alt, ".")
			// Mirror matchesBase's own single-level "non" strip exactly
			// (effects/filter.go): an unconditional TrimPrefix, not a
			// case-insensitive or repeated one.
			base = strings.TrimPrefix(base, "non")
			if base == "" {
				continue
			}
			lb := strings.ToLower(base)
			if pseudoFilterTypes[lb] || types[lb] {
				continue
			}
			counts[base]++
		}
	}
	fromParams := func(p map[string]string) {
		for k, v := range p {
			if validCardFilterKeys[k] {
				note(v)
			}
		}
	}
	var walkSA func(sa *cards.SA)
	walkSA = func(sa *cards.SA) {
		for d := 0; sa != nil && d < maxSAChainWalk; sa, d = sa.Sub, d+1 {
			fromParams(sa.Params)
		}
	}
	for _, c := range r.Cards {
		for _, f := range c.Faces {
			for _, a := range f.Abilities {
				walkSA(a)
			}
			for _, tr := range f.Triggers {
				fromParams(tr.Params)
				walkSA(tr.Effect)
			}
			for _, s := range f.Statics {
				fromParams(s.Params)
			}
			for _, rl := range f.Repls {
				fromParams(rl.Params)
				walkSA(rl.With)
			}
		}
	}

	out := make([]unknownFilterType, 0, len(counts))
	for tok, n := range counts {
		out = append(out, unknownFilterType{token: tok, count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].count != out[j].count {
			return out[i].count > out[j].count
		}
		return out[i].token < out[j].token
	})
	for i := range out {
		out[i].rank = i + 1
	}
	return out
}
