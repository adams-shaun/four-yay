package cards

import (
	"sort"
	"strings"
)

// ValueHeadPrefix is the primitive kind a count-expression head reports
// under: "count:Domain" for a face that reads SVar:X:Count$Domain,
// "count:PlayerCountPropertyYou$SacrificedThisTurn" for the bare
// PlayerCount family. It is a coverage primitive exactly like api:/trig:/
// kw:, answered by effects.Supported, but it is NOT part of Primitives():
// Primitives stays the symbol set the engine dispatches on (botbench's
// deck-coverage plans and the IR tests read it), and a value head is a
// PARAMETER-level read. Registry.Unsupported unions the two, so a card
// whose gate or amount reads a count head the evaluator does not model is
// reported unsupported instead of joining the playable pool with a check
// that can never pass.
const ValueHeadPrefix = "count:"

// ValueHead extracts the gated head of one SVar value body, or ok=false
// when the body is not a head this gate classifies. Two families are
// classified -- the ones whose verdict does not depend on a resolution
// context (a target, a trigger event, a remembered set):
//
//   - Count$<Head>...: the head is the text after "Count$" up to the first
//     space, '/', '$', '.', ':' or '_' (Count$Void.1.0 -> Void,
//     Count$ThisTurnCast_Creature -> ThisTurnCast, Count$ValidGraveyard,Exile
//     X -> ValidGraveyard,Exile, Count$CardCounters.P1P1 -> CardCounters);
//   - PlayerCount<Group>$<Property>...: the head is the whole group
//     reference plus the property up to the first space, '/', '.' or '_'
//     (PlayerCountPropertyYou$SacrificedThisTurn Permanent ->
//     PlayerCountPropertyYou$SacrificedThisTurn).
//
// Every other body (Number$, SVar$, Remembered$, Targeted$..., TriggerCount$,
// AI hint values) is unclassified.
func ValueHead(body string) (string, bool) {
	body = strings.TrimSpace(body)
	if rest, ok := strings.CutPrefix(body, "Count$"); ok {
		if i := strings.IndexAny(rest, " /$.:_"); i >= 0 {
			rest = rest[:i]
		}
		if rest == "" {
			return "", false
		}
		return rest, true
	}
	if strings.HasPrefix(body, "PlayerCount") {
		group, prop, ok := strings.Cut(body, "$")
		if !ok || strings.ContainsAny(group, " /") {
			return "", false
		}
		if i := strings.IndexAny(prop, " /._"); i >= 0 {
			prop = prop[:i]
		}
		if prop == "" {
			return "", false
		}
		return group + "$" + prop, true
	}
	return "", false
}

// ValueHeadOperands lists the count-expression heads NESTED inside body's
// arithmetic suffix chain, deduplicated and sorted; the outer head is NOT
// included. Forge writes such an operand as
// `Count$CardPower/Minus.Count$CardBasePower` (Sovereign Okinec Ahau): the
// operand after a `Plus.`/`Minus.`/`Times.` suffix is itself a full Count$
// expression the evaluator resolves through its own dispatch (effects'
// applyCountOpOperandOK), so the head it names is a separate coverage
// primitive the outer ValueHead token cannot see — ValueHeads must attribute
// it or the honesty gate never checks it and the card joins the playable
// pool with an amount that reads the unresolved verdict.
//
// The walk mirrors exactly the subset of the grammar the evaluator routes:
// only a body ValueHead classifies as a bare Count$ head contributes (a
// ReplaceCount$/TriggerCount$ body takes a different evaluator route, and a
// mid-body `Count$` token inside an ability-line parameter is not an SVar
// body this gate reads), and an operand that is not itself Count$-prefixed —
// a numeric Plus.1 or a bare SVar-name operand the evaluator resolves
// through the face's SVar table — contributes nothing. rules'
// TestValueHeadRegistryMatchesEvaluator resolves each operand head through
// this same helper, so the census and the honesty check cannot disagree
// about the grammar.
func ValueHeadOperands(body string) []string {
	body = strings.TrimSpace(body)
	seen := map[string]struct{}{}
	if _, ok := strings.CutPrefix(body, "Count$"); ok {
		_, op, hasOp := strings.Cut(body, "/")
		if hasOp {
			for _, suffix := range strings.Split(op, "/") {
				for _, prefix := range []string{"Plus.", "Minus.", "Times."} {
					operand, ok := strings.CutPrefix(suffix, prefix)
					if !ok {
						continue
					}
					operand = strings.TrimSpace(operand)
					if !strings.HasPrefix(operand, "Count$") {
						continue
					}
					if head, ok := ValueHead(operand); ok {
						seen[head] = struct{}{}
					}
				}
			}
		}
	}
	for head := range ValueHeadRecipeExpressions(body) {
		seen[head] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for head := range seen {
		out = append(out, head)
	}
	sort.Strings(out)
	return out
}

// ValueHeadRecipeExpressions returns nested heads and the exact numeric
// parameter expressions the recipe passes to the evaluator. Keep this
// deliberately keyed to evaluator-read API/parameter pairs; scanning arbitrary
// recipe text attributes labels in descriptions and nonnumeric parameters.
func ValueHeadRecipeExpressions(body string) map[string]string {
	body = strings.TrimSpace(body)
	apiLine := strings.SplitN(body, "|", 2)[0]
	kind, api, ok := strings.Cut(apiLine, "$")
	if !ok || (strings.TrimSpace(kind) != "DB" && strings.TrimSpace(kind) != "AB" && strings.TrimSpace(kind) != "SP") || strings.TrimSpace(api) != "Token" {
		return nil
	}
	params := parseParams(body)
	expr, ok := params["TokenAmount"]
	if !ok {
		return nil
	}
	head, ok := ValueHead(expr)
	if !ok || !strings.HasPrefix(strings.TrimSpace(expr), "Count$") {
		return nil
	}
	return map[string]string{head: strings.TrimSpace(expr)}
}

// ValueHeads lists the "count:<head>" primitives this face's REFERENCED
// value SVars read. An SVar counts as referenced when its name appears as a
// token in any ability line, trigger/static/replacement parameter, keyword,
// or another SVar body of the face (a compare operand's GE<name> spelling
// included); an unreferenced body is an AI hint or dead text and never
// reaches the evaluator. Sorted and de-duplicated.
func (f *Face) ValueHeads() []string {
	if len(f.SVars) == 0 {
		return nil
	}
	refs := f.referencedNames()
	set := map[string]struct{}{}
	for name, body := range f.SVars {
		if !refs[name] {
			continue
		}
		if head, ok := ValueHead(body); ok {
			set[ValueHeadPrefix+head] = struct{}{}
		}
		for _, head := range ValueHeadOperands(body) {
			set[ValueHeadPrefix+head] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ValueHeads is the union across every face.
func (c *Card) ValueHeads() []string {
	set := map[string]struct{}{}
	for _, f := range c.Faces {
		for _, h := range f.ValueHeads() {
			set[h] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// referencedNames tokenises every place a face can name an SVar.
func (f *Face) referencedNames() map[string]bool {
	refs := map[string]bool{}
	add := func(s string) {
		for _, tok := range strings.FieldsFunc(s, func(r rune) bool {
			return !(r == '_' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z')
		}) {
			refs[tok] = true
			if len(tok) > 2 {
				switch tok[:2] {
				case "GE", "GT", "LE", "LT", "EQ", "NE":
					refs[tok[2:]] = true
				}
			}
		}
	}
	addParams := func(p map[string]string) {
		for _, v := range p {
			add(v)
		}
	}
	var walk func(sa *SA, depth int)
	walk = func(sa *SA, depth int) {
		if sa == nil || depth > maxSVarDepth {
			return
		}
		addParams(sa.Params)
		walk(sa.Sub, depth+1)
	}
	for _, a := range f.Abilities {
		walk(a, 0)
	}
	for _, t := range f.Triggers {
		addParams(t.Params)
		walk(t.Effect, 0)
	}
	for _, s := range f.Statics {
		addParams(s.Params)
	}
	for _, r := range f.Repls {
		addParams(r.Params)
		walk(r.With, 0)
	}
	for _, k := range f.Keywords {
		add(k)
	}
	for _, body := range f.SVars {
		add(body)
	}
	return refs
}
