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
			if f.Name == "" || len(f.SVars) == 0 {
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
				if len(set) == 0 {
					continue
				}
				heads := make([]string, 0, len(set))
				for h := range set {
					heads = append(heads, h)
				}
				sort.Strings(heads)
				ctx := &effects.Ctx{Source: id, Controller: 0, SVars: f.SVars}
				for _, h := range heads {
					if !referenced[h] || resolves[h] {
						continue
					}
					seen[h] = true
					probe := body
					if h != outer {
						// An operand head is read through its own bare
						// Count$ expression at run time, so that is the
						// body whose resolution this check verifies.
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
