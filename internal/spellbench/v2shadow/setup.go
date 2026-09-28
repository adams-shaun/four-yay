// Package v2shadow is the v2 shadow: at a SpellBench protocol v2 decision
// it rebuilds a gorge engine consistent with what the acting seat can see
// (the decision's neutral observation, spec §6), lets a gorge-native policy
// (sb-tactical, optionally arbitrated by a rollout search over redealt
// worlds) answer there, and maps the gorge answer back onto one of the v2
// candidates. It is the v2 counterpart of the kernel shadow (package kshadow
// on wt/sb-kernel-shadow, design spec D§3.6), whose staging it follows.
//
// Design (spec D§4, D§11.2): the shadow is REBUILT from every observation,
// never replayed: between two decisions of our seat the opponent may act any
// number of times and the seat never sees those decisions as actions, so an
// incremental replay would desynchronise for good at the first divergence.
// Gorge object ids stay stable across rebuilds: a v2 object id keeps the deck
// object it claimed first (Tracker), so a policy that plans across several
// wire decisions (a payment lowering, a combat declaration) sees the same
// objects at every one of them.
//
// Hidden information: SpellBench's rotating pools are mirrors with the
// opponent's list visible (spec §12.2), so both lists are known; the
// opponent's hand and both libraries are dealt uniformly from each list
// minus every card the observation shows (the M1 redeal world, D§5.2).
// Nothing here reads anything but the seat's own message stream.
//
// Every decision the shadow cannot stage or map is answered by the fallback
// policy and counted by reason (Stats): the agent never forfeits, never
// refuses and never loses a legal action.
//
// Staging goes through events.Emit on the new engine's own log (events.Apply
// is the only mutation, AGENTS.md) and deliberately bypasses the rules
// engine's emit: a staged permanent must not fire its enters trigger again.
// The staged engine is a throwaway hypothetical, never replayable from its
// Config.
package v2shadow

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/spellbench"
	"github.com/adams-shaun/gorge/internal/spellbench/v2agent"
)

// Setup is one game's static context: both decklists resolved to gorge
// cards (seat order p0, p1) and the token scripts the lists can create.
type Setup struct {
	Reg   *cards.Registry
	Decks [2][]*cards.Card
	// OppListAssumed is set when the opponent's list was hidden and our own
	// list stands in for it (the pools are mirrors).
	OppListAssumed bool
	// byName folds a face name (or an "A // B" full name) to the deck
	// indices carrying it, per seat.
	byName [2]map[string][]int
	// tokens folds a token face name to its script stem; the decks' own
	// stems win over the rest of the corpus.
	tokens map[string]string
}

// fold normalises a card name (case, punctuation, diacritics).
func fold(n string) string {
	n = diacritics.Replace(n)
	return cards.NormalizeName(n)
}

var diacritics = strings.NewReplacer("ó", "o", "û", "u", "Ó", "O", "é", "e", "á", "a", "í", "i", "ú", "u", "ö", "o", "ä", "a", "ü", "u")

var tokenScriptRE = regexp.MustCompile(`TokenScript\$\s*([A-Za-z0-9_,]+)`)

// resolveList resolves a v2 decklist against reg: a full name "A // B" is
// looked up as itself and then by its front face (the registry keys a
// multi-face card by the front).
func resolveList(reg *cards.Registry, d *v2agent.Deck) ([]*cards.Card, error) {
	if d == nil {
		return nil, fmt.Errorf("no decklist")
	}
	var out []*cards.Card
	for _, r := range d.Decklist {
		c, ok := reg.Lookup(r.Name)
		if !ok {
			if front, _, multi := strings.Cut(r.Name, " // "); multi {
				c, ok = reg.Lookup(front)
			}
		}
		if !ok {
			return nil, fmt.Errorf("card %q is not in the registry", r.Name)
		}
		for k := uint32(0); k < r.Count; k++ {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("empty decklist")
	}
	return out, nil
}

// NewSetup resolves both seats' lists from game_start. The opponent's list
// is its visible list, or (hidden lists) our own as the mirror prior.
func NewSetup(reg *cards.Registry, g *v2agent.GameStart) (*Setup, error) {
	if reg == nil || g == nil {
		return nil, fmt.Errorf("v2shadow: no registry or game_start")
	}
	me := seatIndex(g.Seat)
	own, err := resolveList(reg, g.OwnDeck)
	if err != nil {
		return nil, fmt.Errorf("v2shadow: own deck: %w", err)
	}
	s := &Setup{Reg: reg, tokens: map[string]string{}}
	opp := own
	if g.OpponentDeck != nil {
		if opp, err = resolveList(reg, g.OpponentDeck); err != nil {
			return nil, fmt.Errorf("v2shadow: opponent deck: %w", err)
		}
	} else {
		s.OppListAssumed = true
	}
	s.Decks[me], s.Decks[1-me] = own, opp
	stems := map[string]bool{} // lookup only
	for i := 0; i < 2; i++ {
		s.byName[i] = map[string][]int{}
		for j, c := range s.Decks[i] {
			seen := map[string]bool{} // lookup only
			add := func(n string) {
				if k := fold(n); k != "" && !seen[k] {
					seen[k] = true
					s.byName[i][k] = append(s.byName[i][k], j)
				}
			}
			var faces []string
			for _, f := range c.Faces {
				if f == nil {
					continue
				}
				add(f.Name)
				faces = append(faces, f.Name)
				collectTokenStems(f, stems)
			}
			if len(faces) >= 2 {
				add(strings.Join(faces[:2], " // "))
			}
		}
	}
	var own2, all []string
	for st := range stems {
		own2 = append(own2, st)
	}
	sort.Strings(own2)
	for st := range reg.Tokens {
		all = append(all, st)
	}
	sort.Strings(all)
	for _, list := range [][]string{own2, all} {
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

// TokenStem returns the token script stem for a token's name.
func (s *Setup) TokenStem(name string) (string, bool) {
	st, ok := s.tokens[fold(name)]
	return st, ok
}

// Indices returns seat's deck indices whose card carries name.
func (s *Setup) Indices(seat int, name string) []int { return s.byName[seat][fold(name)] }

// seatIndex maps "p1" to 1 and anything else to 0.
func seatIndex(seat string) int {
	if seat == "p1" {
		return 1
	}
	return 0
}

// PoolRegistry returns a registry holding only the cards of every catalog
// deck in the pauper-kernel directory plus every token script: the agent's
// working set (a full corpus registry is ~400 MB per process). Save it with
// Registry.Save and open it with OpenRegistry.
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

// OpenRegistry opens a card registry: a saved registry file (*.gob.gz, a
// PoolRegistry written with Save) or a corpus directory.
func OpenRegistry(path string) (*cards.Registry, error) {
	if st, err := os.Stat(path); err == nil && !st.IsDir() && strings.HasSuffix(path, ".gob.gz") {
		return cards.LoadRegistry(path)
	}
	return cards.SharedCorpus(path)
}
