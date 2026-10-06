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
	CompareKeywordsNamed  = "keywords_named"
	CompareOffered        = "offered"
	CompareNoLibraryOrder = "no_library_order"
	CompareHandCount      = "hand_count"
)

// ValidateCompare rejects any Compare field outside the closed opt-in vocabulary.
// The option vocabulary is closed; a new value must be added here (and to the
// comparator's wantsCompare readers) before an item may name it.
func ValidateCompare(compare []string) error {
	for _, c := range compare {
		if c != CompareKeywords && c != CompareKeywordsNamed && c != CompareOffered && c != CompareNoLibraryOrder && c != CompareHandCount {
			return fmt.Errorf("unknown compare field %q (recognised: %q, %q, %q, %q, %q)", c, CompareKeywords, CompareKeywordsNamed, CompareOffered, CompareNoLibraryOrder, CompareHandCount)
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

// namedKeywords is the wider, opt-in vocabulary of permanent keyword
// abilities whose names the two engines agree on (spec 2026-10-05-compliance-
// level-b section 2.3). It is the evergreen set plus the named-ability
// keywords a permanent can carry, which are compared by NAME only: gorge's
// "Ward:PayLife<2>" and XMage's "ward&mdash;Pay 2 life." both fold to "ward".
// A quality-parameterised keyword never folds onto its base (Tam's
// "Hexproof:CardColors" is not "hexproof"), so hexproof-from and protection
// stay outside both sets.
var namedKeywords = map[string]bool{
	"ward":        true,
	"prowess":     true,
	"wither":      true,
	"persist":     true,
	"firebending": true,
}

// foldKeyword normalizes a keyword name for comparison: trim, collapse
// internal whitespace, and lower-case, so gorge's "First Strike" and
// XMage's "First strike" fold to the same string.
func foldKeyword(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), " "))
}

// foldNamedKeyword folds one keyword to its bare NAME for the named
// vocabulary: it cuts a parameter or reminder suffix, then case- and
// whitespace-folds. gorge spells a parameter with a colon ("Ward:1",
// "Ward:PayLife<2>", "Firebending:2"), so it cuts at the first ":". XMage's
// driver emits the ability's rule text, so it cuts at the first "{" ("ward
// {1}"), the first HTML entity or em dash ("ward&mdash;Pay 2 life."), or a
// space followed by a digit ("firebending 2").
func foldNamedKeyword(name string) string {
	if i := strings.IndexByte(name, ':'); i >= 0 {
		name = name[:i]
	}
	if i := strings.IndexByte(name, '{'); i >= 0 {
		name = name[:i]
	}
	if i := strings.Index(name, "&mdash;"); i >= 0 {
		name = name[:i]
	}
	if i := strings.IndexRune(name, '\u2014'); i >= 0 {
		name = name[:i]
	}
	if i := digitAfterSpace(name); i >= 0 {
		name = name[:i]
	}
	return foldKeyword(name)
}

// digitAfterSpace returns the index of a space immediately followed by an
// ASCII digit, or -1. It bounds the named fold's "firebending 2" form without
// cutting "first strike" or "double strike" (a space followed by a letter).
func digitAfterSpace(s string) int {
	for i := 0; i+1 < len(s); i++ {
		if s[i] == ' ' && s[i+1] >= '0' && s[i+1] <= '9' {
			return i
		}
	}
	return -1
}

// EvergreenKeywords is the item-comparison view of a permanent's keywords:
// folded, evergreen-only, sorted and comma-joined, "" when none.
func EvergreenKeywords(keywords []string) string { return evergreenList(keywords) }

// ComparedKeywords is the item-comparison view of a permanent's keywords for
// the opt-in vocabulary the item names: the named set (evergreen plus the
// name-compared abilities) when named, else the evergreen set. A static that
// needs the wider set names CompareKeywordsNamed, so items the evergreen set
// already serves keep their frozen bytes.
func ComparedKeywords(keywords []string, named bool) string {
	if named {
		return namedList(keywords)
	}
	return evergreenList(keywords)
}

// evergreenList folds a permanent's keywords, keeps only the evergreen set,
// sorts and comma-joins them. Empty when nothing evergreen is present, so
// the caller omits the field entirely.
func evergreenList(keywords []string) string {
	return foldList(keywords, false)
}

// namedList folds a permanent's keywords under the wider named vocabulary:
// evergreen keywords keep their word form, a named ability folds to its bare
// name, and anything else (hexproof-from, protection) is dropped.
func namedList(keywords []string) string {
	return foldList(keywords, true)
}

// foldList is the one fold the two vocabularies share; named also admits the
// namedKeywords set and folds each entry to its name.
func foldList(keywords []string, named bool) string {
	seen := map[string]bool{}
	for _, kw := range keywords {
		f := foldKeyword(kw)
		if evergreenKeywords[f] {
			seen[f] = true
			continue
		}
		if named {
			if n := foldNamedKeyword(kw); namedKeywords[n] {
				seen[n] = true
			}
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
