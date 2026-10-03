// Package adopt turns the compliance data into the set-adoption queue
// (docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md section
// 11.3): the primitive impact table (C5), one ticket per finding class
// (C4), honest tournament scoping (C7) and the per-set / per-format status
// dashboard with its shrink-only ratchet (C8).
//
// Everything is derived from committed data -- the XMage manifests, the
// printed lists, compliance/formats.json, the verdicts and triage clusters
// -- plus the corpus and effects.Supported() at this head. Nothing here
// files a ticket or reads a Forge card script's text.
package adopt

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance"
	"github.com/adams-shaun/gorge/effects"
	// rules registers the non-API primitives (keywords, triggers, statics)
	// into effects.Supported() at init; without it every such primitive
	// reads as unsupported.
	_ "github.com/adams-shaun/gorge/rules"
)

// FormatsFile is the committed certification-target config, relative to
// the repo root.
const FormatsFile = "compliance/formats.json"

// Format is one certification target: a set list, or every set of the
// given XMage set types released on or after From, or (Tournament) every
// set whose type is not a non-tournament type.
type Format struct {
	Name       string   `json:"name"`
	Sets       []string `json:"sets,omitempty"`
	From       string   `json:"from,omitempty"`
	SetTypes   []string `json:"set_types,omitempty"`
	Tournament bool     `json:"tournament,omitempty"`
}

// Config is compliance/formats.json.
type Config struct {
	Formats                 []Format `json:"formats"`
	NonTournamentSetTypes   []string `json:"non_tournament_set_types"`
	NonTournamentCardTypes  []string `json:"non_tournament_card_types"`
	NonTournamentPrimitives []string `json:"non_tournament_primitives"`
}

// LoadConfig reads compliance/formats.json under root.
func LoadConfig(root string) (*Config, error) {
	p := filepath.Join(root, FormatsFile)
	raw, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("%s: %v", p, err)
	}
	if len(c.Formats) == 0 {
		return nil, fmt.Errorf("%s: no formats", p)
	}
	return &c, nil
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// TournamentSetType reports whether a set of this XMage type is played in
// any sanctioned paper format.
func (c *Config) TournamentSetType(setType string) bool {
	return !contains(c.NonTournamentSetTypes, setType)
}

// Includes reports whether format f covers set m.
func (c *Config) Includes(f Format, m compliance.Manifest) bool {
	switch {
	case len(f.Sets) > 0:
		return contains(f.Sets, m.Code)
	case len(f.SetTypes) > 0:
		return contains(f.SetTypes, m.SetType) && m.Released >= f.From
	case f.Tournament:
		return c.TournamentSetType(m.SetType)
	}
	return false
}

// Card is one distinct card across every manifest and printed list.
type Card struct {
	Name     string   // corpus name, or the printed name when the corpus lacks it
	InCorpus bool     //
	Sets     []string // every set printing it (manifests and printed lists), sorted
	Formats  []string // the formats whose sets print it, in Config order
	Missing  []string // unsupported primitives at this head
	// NonTournament says why the card is outside every tournament target
	// ("" for a tournament card).
	NonTournament string
}

// Census is every card of every committed set, classified at this head.
type Census struct {
	Config     *Config
	Sets       map[string]compliance.Manifest // set code -> XMage manifest
	SetFormats map[string][]string            // set code -> formats covering it, Config order
	Cards      []*Card                        // sorted by Name
	byName     map[string]*Card
	setCards   map[string][]*Card
}

// Lookup returns the census card with this corpus (or printed) name.
func (cs *Census) Lookup(name string) (*Card, bool) {
	c, ok := cs.byName[name]
	return c, ok
}

// SetCards lists the cards a set prints (its printed list when it has one,
// else its manifest), sorted by name.
func (cs *Census) SetCards(set string) []*Card { return cs.setCards[set] }

// SetCodes lists every manifest's set code, sorted.
func (cs *Census) SetCodes() []string {
	out := make([]string, 0, len(cs.Sets))
	for k := range cs.Sets {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// FormatSets lists the sets format f covers, sorted.
func (cs *Census) FormatSets(f string) []string {
	var out []string
	for _, s := range cs.SetCodes() {
		if contains(cs.SetFormats[s], f) {
			out = append(out, s)
		}
	}
	return out
}

// corpusName is the single place the census maps a printed or XMage name
// onto the corpus.
func corpusName(reg *cards.Registry, name string) (string, bool) {
	return compliance.CorpusName(func(n string) bool { _, ok := reg.Lookup(n); return ok }, name)
}

// Build reads every committed manifest and printed list under root and
// classifies each card against reg and effects.Supported().
func Build(reg *cards.Registry, root string) (*Census, error) {
	cfg, err := LoadConfig(root)
	if err != nil {
		return nil, err
	}
	cs := &Census{Config: cfg, Sets: map[string]compliance.Manifest{}, SetFormats: map[string][]string{},
		byName: map[string]*Card{}, setCards: map[string][]*Card{}}
	paths, err := filepath.Glob(filepath.Join(root, "compliance", "manifests", "*.json"))
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no manifests under %s", root)
	}
	sup := effects.Supported()
	tournamentSet := map[string]bool{}
	card := func(printed string) *Card {
		name, ok := corpusName(reg, printed)
		if !ok {
			name = printed
		}
		c := cs.byName[name]
		if c == nil {
			c = &Card{Name: name, InCorpus: ok}
			if ok {
				rc, _ := reg.Lookup(name)
				c.Missing = reg.Unsupported(rc, sup)
				sort.Strings(c.Missing)
				if len(rc.Faces) > 0 {
					for _, t := range rc.Faces[0].Types {
						if contains(cfg.NonTournamentCardTypes, t) {
							c.NonTournament = "card type " + t
						}
					}
				}
			}
			for _, p := range c.Missing {
				if c.NonTournament == "" && contains(cfg.NonTournamentPrimitives, p) {
					c.NonTournament = "primitive " + p
				}
			}
			cs.byName[name] = c
		}
		return c
	}
	inSet := map[string]map[*Card]bool{}
	add := func(set string, c *Card) {
		if inSet[set] == nil {
			inSet[set] = map[*Card]bool{}
		}
		inSet[set][c] = true
	}
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var m compliance.Manifest
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, fmt.Errorf("%s: %v", p, err)
		}
		cs.Sets[m.Code] = m
		for _, f := range cfg.Formats {
			if cfg.Includes(f, m) {
				cs.SetFormats[m.Code] = append(cs.SetFormats[m.Code], f.Name)
			}
		}
		tournamentSet[m.Code] = cfg.TournamentSetType(m.SetType)
		pr, perr := compliance.LoadPrinted(filepath.Join(root, "compliance", "printed"), m.Code)
		switch {
		case perr == nil:
			for _, n := range pr.Cards {
				add(m.Code, card(n))
			}
		case os.IsNotExist(perr):
			for _, mc := range m.Cards {
				add(m.Code, card(mc.Name))
			}
		default:
			return nil, perr
		}
	}
	for set, members := range inSet {
		for c := range members {
			cs.setCards[set] = append(cs.setCards[set], c)
			c.Sets = append(c.Sets, set)
		}
		sort.Slice(cs.setCards[set], func(i, j int) bool { return cs.setCards[set][i].Name < cs.setCards[set][j].Name })
	}
	for _, c := range cs.byName {
		sort.Strings(c.Sets)
		anyTournament := false
		for _, s := range c.Sets {
			anyTournament = anyTournament || tournamentSet[s]
		}
		if c.NonTournament == "" && !anyTournament {
			c.NonTournament = "printed only in non-tournament sets " + strings.Join(c.Sets, " ")
		}
		for _, f := range cfg.Formats {
			for _, s := range c.Sets {
				if contains(cs.SetFormats[s], f.Name) {
					c.Formats = append(c.Formats, f.Name)
					break
				}
			}
		}
		cs.Cards = append(cs.Cards, c)
	}
	sort.Slice(cs.Cards, func(i, j int) bool { return cs.Cards[i].Name < cs.Cards[j].Name })
	return cs, nil
}
