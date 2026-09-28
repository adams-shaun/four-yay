// Package kshadow is the kernel shadow: it rebuilds, at every mtg-kernel v1
// decision, a gorge engine consistent with what the acting seat can see
// (the decision's x_kernel_v5 ObservationV5), maps the kernel's candidates
// onto that engine's options, lets a gorge policy (sb-tactical, the
// az-redeal search) answer, and maps the answer back. Any decision it
// cannot stage or map is answered by v1agent.Tactical and counted.
//
// Design (spec D§4.4): the shadow is REBUILT from each observation, never
// replayed incrementally. The seat never sees the opponent's decisions
// (between two of our decisions the opponent may act several times, and the
// kernel's rules differ from gorge's in places), so an incremental replay
// desynchronises for good at the first divergence, while a rebuild bounds a
// divergence to one decision. Everything the observation does not carry
// (hidden zones, durations, delayed triggers, linked exile) is lossy and
// measured by the fidelity check (Fidelity).
//
// Hidden information: SpellBench's rotating pools are deck mirrors with the
// opponent's list visible, so both lists are known. The opponent's hand and
// both libraries are dealt uniformly from each list minus every card the
// observation shows (the M1 redeal world, spec D§5.2); a seed picks the
// deal. Nothing here reads anything but the acting seat's own observation.
//
// Staging goes through events.Emit on the new engine's own log (events.Apply
// is the only mutation, AGENTS.md) and deliberately bypasses the rules
// engine's emit: a staged permanent must not fire its enters trigger again.
// The staged engine is a throwaway hypothetical, never replayable from its
// Config.
package kshadow

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/spellbench"
)

// Setup is one game's static context: both decklists resolved to gorge
// cards (seat order p0, p1) and the token scripts the lists can create.
type Setup struct {
	Reg   *cards.Registry
	Decks [2][]*cards.Card
	// Catalog names each seat's catalog deck id.
	Catalog [2]string
	// byName folds a face name to the deck indices carrying it, per seat.
	byName [2]map[string][]int
	// tokens folds a token face name ("Blood", "Eldrazi Spawn") to its
	// script stem; preferred are the stems the decks' own cards name.
	tokens map[string]string
}

// fold normalises a card name across the kernel's and gorge's spellings
// (diacritics, punctuation, case, " Token" suffix).
func fold(n string) string {
	n = diacritics.Replace(n)
	n = cards.NormalizeName(n)
	return strings.TrimSuffix(n, " token")
}

var diacritics = strings.NewReplacer("ó", "o", "û", "u", "Ó", "O", "é", "e", "á", "a", "í", "i", "ú", "u", "ö", "o", "ä", "a", "ü", "u")

var tokenScriptRE = regexp.MustCompile(`TokenScript\$\s*([A-Za-z0-9_,]+)`)

// NewSetup resolves two pauper-kernel catalog decks against reg.
func NewSetup(reg *cards.Registry, catalog [2]string) (*Setup, error) {
	s := &Setup{Reg: reg, Catalog: catalog, tokens: map[string]string{}}
	stems := map[string]bool{} // lookup only
	for i := 0; i < 2; i++ {
		d, err := spellbench.Deck(reg, spellbench.PauperKernel, catalog[i])
		if err != nil {
			return nil, err
		}
		s.Decks[i] = d
		s.byName[i] = map[string][]int{}
		for j, c := range d {
			seen := map[string]bool{}
			for _, f := range c.Faces {
				if f == nil {
					continue
				}
				k := fold(f.Name)
				if !seen[k] {
					seen[k] = true
					s.byName[i][k] = append(s.byName[i][k], j)
				}
				collectTokenStems(f, stems)
			}
		}
	}
	// Token faces by name: the decks' own stems first, then the whole
	// token corpus in sorted stem order (first wins).
	var own, all []string
	for st := range stems {
		own = append(own, st)
	}
	sort.Strings(own)
	for st := range reg.Tokens {
		all = append(all, st)
	}
	sort.Strings(all)
	for _, list := range [][]string{own, all} {
		for _, st := range list {
			c := reg.Tokens[st]
			if c == nil || len(c.Faces) == 0 || c.Faces[0] == nil {
				continue
			}
			k := fold(c.Faces[0].Name)
			if _, ok := s.tokens[k]; !ok {
				s.tokens[k] = st
			}
		}
	}
	return s, nil
}

func collectTokenStems(f *cards.Face, into map[string]bool) {
	add := func(text string) {
		for _, m := range tokenScriptRE.FindAllStringSubmatch(text, -1) {
			for _, st := range strings.Split(m[1], ",") {
				if st = strings.TrimSpace(st); st != "" {
					into[st] = true
				}
			}
		}
	}
	for _, v := range f.SVars {
		add(v)
	}
	for _, sa := range f.Abilities {
		if sa != nil {
			add(sa.Line)
		}
	}
	for _, tr := range f.Triggers {
		if tr.Effect != nil {
			add(tr.Effect.Line)
		}
	}
}

// TokenStem returns the token script stem for a kernel token name.
func (s *Setup) TokenStem(name string) (string, bool) {
	st, ok := s.tokens[fold(name)]
	return st, ok
}

// Indices returns seat's deck indices whose card has a face named name.
func (s *Setup) Indices(seat int, name string) []int { return s.byName[seat][fold(name)] }

func (s *Setup) String() string { return fmt.Sprintf("%s vs %s", s.Catalog[0], s.Catalog[1]) }

// PoolRegistry returns a registry holding only the cards of every catalog
// deck in the pauper-kernel directory plus every token script: the shadow
// agent's working set (a full corpus registry is ~400 MB per process).
func PoolRegistry(full *cards.Registry) (*cards.Registry, error) {
	ids, err := spellbench.CatalogIDs(spellbench.PauperKernel)
	if err != nil {
		return nil, err
	}
	r := cards.NewRegistry()
	seen := map[*cards.Card]bool{} // lookup only
	for _, id := range ids {
		d, err := spellbench.Deck(full, spellbench.PauperKernel, id)
		if err != nil {
			return nil, err
		}
		for _, c := range d {
			if !seen[c] {
				seen[c] = true
				r.Add(c)
			}
		}
	}
	for k, t := range full.Tokens {
		r.Tokens[k] = t
	}
	return r, nil
}
