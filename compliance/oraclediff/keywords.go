package oraclediff

import (
	"fmt"
	"sort"
	"strings"
)

// Compare option vocabulary. Each opt-in changes only its named comparison
// surface; all ordinary fields remain compared.
const (
	CompareKeywords       = "keywords"
	CompareOffered        = "offered"
	CompareNoLibraryOrder = "no_library_order"
)

// ValidateCompare rejects any Compare field outside the closed opt-in vocabulary.
// The option vocabulary is closed; a new value must be added here (and to the
// comparator's wantsCompare readers) before an item may name it.
func ValidateCompare(compare []string) error {
	for _, c := range compare {
		if c != CompareKeywords && c != CompareOffered && c != CompareNoLibraryOrder {
			return fmt.Errorf("unknown compare field %q (recognised: %q, %q, %q)", c, CompareKeywords, CompareOffered, CompareNoLibraryOrder)
		}
	}
	return nil
}

// evergreenKeywords is the closed vocabulary the comparator folds keyword
// names onto before comparing. Both engines spell keywords differently
// (gorge "First Strike", XMage "First strike"), so a permanent's keywords
// are case-folded and intersected with this set: any keyword outside it
// (Ward:2, a card-specific keyword) is ignored, since the two engines'
// vocabularies for it are not yet aligned (spec 2026-10-05-compliance-level-b
// section 2.3, hypothesis H5).
var evergreenKeywords = map[string]bool{
	"flying":         true,
	"first strike":   true,
	"double strike":  true,
	"deathtouch":     true,
	"defender":       true,
	"haste":          true,
	"hexproof":       true,
	"indestructible": true,
	"lifelink":       true,
	"menace":         true,
	"reach":          true,
	"trample":        true,
	"vigilance":      true,
}

// foldKeyword normalizes a keyword name for comparison: trim, collapse
// internal whitespace, and lower-case, so gorge's "First Strike" and
// XMage's "First strike" fold to the same string.
func foldKeyword(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), " "))
}

// EvergreenKeywords is the item-comparison view of a permanent's keywords:
// folded, evergreen-only, sorted and comma-joined, "" when none.
func EvergreenKeywords(keywords []string) string { return evergreenList(keywords) }

// evergreenList folds a permanent's keywords, keeps only the evergreen set,
// sorts and comma-joins them. Empty when nothing evergreen is present, so
// the caller omits the field entirely.
func evergreenList(keywords []string) string {
	seen := map[string]bool{}
	for _, kw := range keywords {
		if f := foldKeyword(kw); evergreenKeywords[f] {
			seen[f] = true
		}
	}
	if len(seen) == 0 {
		return ""
	}
	out := make([]string, 0, len(seen))
	for f := range seen {
		out = append(out, f)
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}
