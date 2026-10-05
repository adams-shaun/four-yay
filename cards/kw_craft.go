package cards

import (
	"regexp"
	"strings"
)

var craftUniform = regexp.MustCompile(`^\s*(?:(?:\d+|[WUBRGCX]|[WUBRGC]/[WUBRGC]|XMin\d+)\s+)*ExileCtrlOrGrave<(?:X|\d+)/[A-Za-z0-9_.]+(?:/[^>]*)?>\s*$`)

// CraftUniformShape certifies only the single-material form implemented by
// this slice. Keeping this classifier beside the expander makes the primitive
// census fail closed for every other Craft grammar.
func keywordParam(line string) string {
	if i := strings.IndexByte(line, ':'); i >= 0 {
		return strings.TrimSpace(line[i+1:])
	}
	return ""
}

func CraftUniformShape(param string) bool {
	if !craftUniform.MatchString(param) {
		return false
	}
	return strings.Count(param, "ExileCtrlOrGrave<") == 1
}

func kwCraft(f *Face, _ int, k, _, param string, has func(kind, line string) bool) {
	if has("A", k) || !CraftUniformShape(param) {
		return
	}
	cost := strings.TrimSpace(param) + " Exile<1/CARDNAME>"
	sa, _ := parseSA("", "AB$ ChangeZone | Cost$ "+cost+
		" | ActivationZone$ Battlefield | SorcerySpeed$ True | Defined$ Self | Origin$ Exile | Destination$ Battlefield | SubAbility$ __CraftTransform | Keyword$ Craft | SpellDescription$ Craft -- pay "+strings.TrimSpace(param)+": return this transformed")
	if sa == nil {
		return
	}
	sa.Params["KeywordLine"] = k
	f.Abilities = append(f.Abilities, sa)
	if f.SVars == nil {
		f.SVars = make(map[string]string)
	}
	f.SVars["__CraftTransform"] = "DB$ SetState | Defined$ Self | Mode$ Transform"
}

func init() { registerKeyword(kwCraft, "Craft") }
