package rules

import (
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestValueHeadRegistryMatchesEvaluator keeps effects' modelledValueHeads
// list -- the "count:<head>" primitives the supported-card gate
// (cards.Registry.Unsupported) checks -- honest in BOTH directions against
// the evaluator itself: every referenced value SVar body in the corpus is
// evaluated with effects.EvalCountOK against a real engine, and a head is
// modelled exactly when at least one of its corpus bodies resolves. A head
// that resolves but is unlisted keeps playable cards out of the pool; a
// listed head that never resolves puts cards with an unreadable gate into
// it (the fuzz-cov3 audit's registration gap). Both are named.
func TestValueHeadRegistryMatchesEvaluator(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	resolves := map[string]bool{}
	seen := map[string]bool{}
	for _, c := range reg.AllCards() {
		for _, f := range c.Faces {
			if f.Name == "" {
				continue
			}
			referenced := map[string]bool{}
			for _, h := range f.ValueHeads() {
				referenced[strings.TrimPrefix(h, cards.ValueHeadPrefix)] = true
			}
			if len(referenced) == 0 {
				continue
			}
			// An off-zone object: the source a count reads, without a
			// battlefield presence that would grow every later census.
			id := e.G.AddObject(c, 0).ID
			ctx := &effects.Ctx{Source: id, Controller: 0, SVars: f.SVars}

			// Printed ability lines carry their own TokenAmount$ Count$…,
			// read through Num exactly as an SVar recipe's is. Probe the
			// same expressions Face.ValueHeads attributes, through the same
			// cards.ValueHeadTokenRecipeExpressions gate, so a newly
			// attributed head cannot go unchecked here.
			var walkSA func(sa *cards.SA, depth int)
			walkSA = func(sa *cards.SA, depth int) {
				if sa == nil || depth > 32 {
					return
				}
				for h, expr := range cards.ValueHeadTokenRecipeExpressions(sa.Kind, sa.API, sa.Params) {
					if !referenced[h] || resolves[h] {
						continue
					}
					seen[h] = true
					if _, ok := effects.EvalCountOK(e, ctx, expr); ok {
						resolves[h] = true
					}
				}
				walkSA(sa.Sub, depth+1)
			}
			for _, sa := range f.Abilities {
				walkSA(sa, 0)
			}
			for i := range f.Triggers {
				walkSA(f.Triggers[i].Effect, 0)
			}
			for i := range f.Repls {
				walkSA(f.Repls[i].With, 0)
			}

			names := make([]string, 0, len(f.SVars))
			for name := range f.SVars {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				body := f.SVars[name]
				outer, _ := cards.ValueHead(body)
				// A body's arithmetic suffixes carry operands the evaluator
				// resolves as full Count$ expressions (Okinec's
				// Count$CardPower/Minus.Count$CardBasePower), and the head an
				// operand names is its own coverage primitive. The grammar
				// comes from the same cards.ValueHeadOperands the census
				// attributes with, so this check and ValueHeads can never
				// disagree about what a body reads.
				set := map[string]struct{}{}
				if h, ok := cards.ValueHead(body); ok {
					set[h] = struct{}{}
				}
				for _, h := range cards.ValueHeadOperands(body) {
					set[h] = struct{}{}
				}
				recipeExpressions := cards.ValueHeadRecipeExpressions(body)
				if len(set) == 0 {
					continue
				}
				heads := make([]string, 0, len(set))
				for h := range set {
					heads = append(heads, h)
				}
				sort.Strings(heads)
				for _, h := range heads {
					if !referenced[h] || resolves[h] {
						continue
					}
					seen[h] = true
					probe := body
					if expression, nested := recipeExpressions[h]; nested {
						// A recipe parameter is passed to Num as this exact
						// expression, not evaluated as a bare SVar body.
						probe = expression
					} else if h != outer {
						// An arithmetic operand head is read through its own
						// bare Count$ expression at run time.
						probe = "Count$" + h
					}
					if _, ok := effects.EvalCountOK(e, ctx, strings.TrimSpace(probe)); ok {
						resolves[h] = true
					}
				}
			}
		}
	}
	listed := map[string]bool{}
	for _, h := range effects.ModelledValueHeads() {
		listed[h] = true
	}
	sup := effects.Supported()
	var unlisted, stale []string
	for h := range seen {
		if resolves[h] && !listed[h] {
			unlisted = append(unlisted, h)
		}
		if !resolves[h] && listed[h] {
			stale = append(stale, h)
		}
	}
	for h := range listed {
		if !seen[h] {
			stale = append(stale, h+" (no corpus carrier)")
		}
		if !sup[cards.ValueHeadPrefix+h] {
			t.Errorf("modelled head %q is not registered in effects.Supported", h)
		}
	}
	sort.Strings(unlisted)
	sort.Strings(stale)
	if len(unlisted) > 0 {
		t.Errorf("count heads the evaluator resolves but effects.modelledValueHeads omits (add them):\n  %s", strings.Join(unlisted, "\n  "))
	}
	if len(stale) > 0 {
		t.Errorf("count heads effects.modelledValueHeads lists but no corpus body resolves (remove them):\n  %s", strings.Join(stale, "\n  "))
	}
}
