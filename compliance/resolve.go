package compliance

import (
	"strings"
	"unicode"

	"github.com/adams-shaun/gorge/cards"
)

// FoldName normalises a card name for cross-engine identity. It folds three
// differences that separate the same card across sources: case, diacritics
// (Forge prints "Dáin Ironfoot", XMage's set class writes "Dain Ironfoot")
// and punctuation (Forge prints "With Great Power . . .", XMage "With Great
// Power..."). Every letter is reduced to its unaccented base, lower-cased,
// and every non-alphanumeric dropped, so two spellings of one card fold
// equal.
//
// It is deliberately distinct from cards.NormalizeName, which folds case and
// punctuation but not diacritics: the corpus index must keep a card's real
// spelling, so the fold lives here, at the boundary where an XMage or printed
// name is matched onto a corpus name.
func FoldName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if base, ok := unaccent(r); ok {
			r = base
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// unaccent maps a precomposed Latin letter to its unaccented base letter.
// The corpus's non-ASCII names use the Latin-1 and Latin Extended-A ranges
// (measured 2026-10-04: 16 distinct accented letters across 92 card names),
// so the table covers those fully. A letter outside it is left as-is; the
// printed/manifest census (compliance/resolve_census_test.go) fails loudly
// when a new accented carrier appears, so a gap is visible, never silent.
func unaccent(r rune) (rune, bool) {
	if r < 0x00C0 || r > 0x017F {
		return r, false
	}
	if base, ok := latinFold[r]; ok {
		return base, true
	}
	return r, false
}

// latinFold is the Latin-1 Supplement (0x00C0-0x00FF) and Latin Extended-A
// (0x0100-0x017F) precomposed letters mapped to their base letter.
var latinFold = map[rune]rune{
	'À': 'A', 'Á': 'A', 'Â': 'A', 'Ã': 'A', 'Ä': 'A', 'Å': 'A',
	'Æ': 'A', 'Ç': 'C', 'È': 'E', 'É': 'E', 'Ê': 'E', 'Ë': 'E',
	'Ì': 'I', 'Í': 'I', 'Î': 'I', 'Ï': 'I', 'Ð': 'D', 'Ñ': 'N',
	'Ò': 'O', 'Ó': 'O', 'Ô': 'O', 'Õ': 'O', 'Ö': 'O', 'Ø': 'O',
	'Ù': 'U', 'Ú': 'U', 'Û': 'U', 'Ü': 'U', 'Ý': 'Y', 'Þ': 'T',
	'ß': 'S', 'à': 'a', 'á': 'a', 'â': 'a', 'ã': 'a', 'ä': 'a',
	'å': 'a', 'æ': 'a', 'ç': 'c', 'è': 'e', 'é': 'e', 'ê': 'e',
	'ë': 'e', 'ì': 'i', 'í': 'i', 'î': 'i', 'ï': 'i', 'ð': 'd',
	'ñ': 'n', 'ò': 'o', 'ó': 'o', 'ô': 'o', 'õ': 'o', 'ö': 'o',
	'ø': 'o', 'ù': 'u', 'ú': 'u', 'û': 'u', 'ü': 'u', 'ý': 'y',
	'þ': 't', 'ÿ': 'y',
	'Ā': 'A', 'ā': 'a', 'Ă': 'A', 'ă': 'a', 'Ą': 'A', 'ą': 'a',
	'Ć': 'C', 'ć': 'c', 'Ĉ': 'C', 'ĉ': 'c', 'Ċ': 'C', 'ċ': 'c',
	'Č': 'C', 'č': 'c', 'Ď': 'D', 'ď': 'd', 'Đ': 'D', 'đ': 'd',
	'Ē': 'E', 'ē': 'e', 'Ĕ': 'E', 'ĕ': 'e', 'Ė': 'E', 'ė': 'e',
	'Ę': 'E', 'ę': 'e', 'Ě': 'E', 'ě': 'e', 'Ĝ': 'G', 'ĝ': 'g',
	'Ğ': 'G', 'ğ': 'g', 'Ġ': 'G', 'ġ': 'g', 'Ģ': 'G', 'ģ': 'g',
	'Ĥ': 'H', 'ĥ': 'h', 'Ħ': 'H', 'ħ': 'h', 'Ĩ': 'I', 'ĩ': 'i',
	'Ī': 'I', 'ī': 'i', 'Ĭ': 'I', 'ĭ': 'i', 'Į': 'I', 'į': 'i',
	'İ': 'I', 'ı': 'i', 'Ĳ': 'I', 'ĳ': 'i', 'Ĵ': 'J', 'ĵ': 'j',
	'Ķ': 'K', 'ķ': 'k', 'ĸ': 'k', 'Ĺ': 'L', 'ĺ': 'l', 'Ļ': 'L',
	'ļ': 'l', 'Ľ': 'L', 'ľ': 'l', 'Ŀ': 'L', 'ŀ': 'l', 'Ł': 'L',
	'ł': 'l', 'Ń': 'N', 'ń': 'n', 'Ņ': 'N', 'ņ': 'n', 'Ň': 'N',
	'ň': 'n', 'ŉ': 'n', 'Ŋ': 'N', 'ŋ': 'n', 'Ō': 'O', 'ō': 'o',
	'Ŏ': 'O', 'ŏ': 'o', 'Ő': 'O', 'ő': 'o', 'Œ': 'O', 'œ': 'o',
	'Ŕ': 'R', 'ŕ': 'r', 'Ŗ': 'R', 'ŗ': 'r', 'Ř': 'R', 'ř': 'r',
	'Ś': 'S', 'ś': 's', 'Ŝ': 'S', 'ŝ': 's', 'Ş': 'S', 'ş': 's',
	'Š': 'S', 'š': 's', 'Ţ': 'T', 'ţ': 't', 'Ť': 'T', 'ť': 't',
	'Ŧ': 'T', 'ŧ': 't', 'Ũ': 'U', 'ũ': 'u', 'Ū': 'U', 'ū': 'u',
	'Ŭ': 'U', 'ŭ': 'u', 'Ů': 'U', 'ů': 'u', 'Ű': 'U', 'ű': 'u',
	'Ų': 'U', 'ų': 'u', 'Ŵ': 'W', 'ŵ': 'w', 'Ŷ': 'Y', 'ŷ': 'y',
	'Ÿ': 'Y', 'Ź': 'Z', 'ź': 'z', 'Ż': 'Z', 'ż': 'z', 'Ž': 'Z',
	'ž': 'z',
}

// FoldedNames indexes the corpus by folded name: FoldName(name) -> the
// corpus's canonical name for that card. A registry may hold two cards whose
// names fold equal (a diacritic-less reprint and its accented original); the
// first in corpus order wins, the same first-wins rule the registry's own
// name index uses.
func FoldedNames(reg *cards.Registry) map[string]string {
	out := make(map[string]string, len(reg.Cards))
	for _, c := range reg.Cards {
		for _, f := range c.Faces {
			k := FoldName(f.Name)
			if k == "" {
				continue
			}
			if _, ok := out[k]; !ok {
				out[k] = f.Name
			}
		}
	}
	return out
}

// CorpusNameFold maps an XMage or printed name onto the corpus's name for
// the same card: the exact name first, then the front face of an "A // B"
// name, then a diacritic/punctuation-folded match through folded. folded is
// FoldedNames(reg).
func CorpusNameFold(has func(string) bool, folded map[string]string, xmageName string) (string, bool) {
	if has(xmageName) {
		return xmageName, true
	}
	if front, _, ok := strings.Cut(xmageName, " // "); ok && has(front) {
		return front, true
	}
	if n, ok := folded[FoldName(xmageName)]; ok {
		return n, true
	}
	return "", false
}
