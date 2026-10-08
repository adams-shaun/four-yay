package cost_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules/cost"
)

// corpusTextValues returns every distinct parameter value, mana cost,
// keyword (whole and per ':' segment) and SVar body in the corpus and its
// token scripts, sorted. It is deliberately wider than "the cost
// parameters": any string a cost parser could be handed is in it, so the
// equivalence below covers every cost token the corpus spells.
func corpusTextValues(reg *cards.Registry) []string {
	seen := map[string]bool{}
	add := func(s string) { seen[s] = true }
	params := func(m map[string]string) {
		for _, v := range m {
			add(v)
		}
	}
	sa := func(a *cards.SA) {
		for ; a != nil; a = a.Sub {
			params(a.Params)
		}
	}
	all := append([]*cards.Card(nil), reg.AllCards()...)
	for _, t := range reg.Tokens {
		all = append(all, t)
	}
	for _, c := range all {
		for _, f := range c.Faces {
			add(f.ManaCost)
			for _, k := range f.Keywords {
				add(k)
				for seg := range strings.SplitSeq(k, ":") {
					add(seg)
				}
			}
			for _, v := range f.SVars {
				add(v)
			}
			for _, a := range f.Abilities {
				sa(a)
			}
			for _, t := range f.Triggers {
				params(t.Params)
				sa(t.Effect)
			}
			for _, s := range f.Statics {
				params(s.Params)
			}
			for _, r := range f.Repls {
				params(r.Params)
				sa(r.With)
			}
		}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

var braces = strings.NewReplacer("{", " ", "}", " ")

// TestTokenMatchersAgreeOverTheCorpus is the W5 E1 proof that compiling the
// cost-token regexps by hand changed no parse: every token ParseCost or
// ParseUnlessCost can see in the corpus -- each value tokenized raw and
// brace-normalized exactly as the parsers do -- and a deterministic
// boundary-mutation sweep of each token gets the same match and the same
// capture groups from every hand matcher as from the regexp it replaced
// (the grammar consts in parse.go). ParseCost and ParseUnlessCost differ
// from their regexp versions only in which function answers those matches,
// so agreement here is agreement of the parsed Cost.
func TestTokenMatchersAgreeOverTheCorpus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	toks := map[string]bool{}
	for _, v := range corpusTextValues(reg) {
		for _, s := range []string{strings.TrimSpace(v), braces.Replace(strings.TrimSpace(v))} {
			for it := cost.NewTokenIter(s); ; {
				tok, ok := it.Next()
				if !ok {
					break
				}
				toks[tok] = true
			}
		}
	}
	sorted := make([]string, 0, len(toks))
	for tok := range toks {
		sorted = append(sorted, tok)
	}
	sort.Strings(sorted)
	checked := 0
	for _, tok := range sorted {
		cost.CheckToken(t, tok)
		checked++
		if strings.IndexByte(tok, '<') < 0 && !strings.HasPrefix(tok, "XMin") {
			continue // the mutation sweep targets the Head<...> grammar
		}
		for _, m := range cost.Mutations(tok) {
			cost.CheckToken(t, m)
			checked++
		}
		if t.Failed() {
			t.FailNow()
		}
	}
	if len(sorted) < 1000 {
		t.Fatalf("only %d distinct corpus tokens; the corpus did not load", len(sorted))
	}
	t.Logf("%d distinct corpus tokens, %d tokens checked against %d matchers", len(sorted), checked, cost.MatcherCount())
}
