package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

// corpusMode names which corpus shim this build links. One value survives:
// every run opens the whole corpus, since S4 made cards.OpenCorpusFor a
// wrapper over SharedCorpus and S8 deletes the subset route entirely.
var corpusMode = "full"

// The docs/015 §6 workloads.
var pairs = map[string][2]string{
	"A": {"FDN_top_04956_UG", "FDN_top_20626_WG"},
	"B": {"FDN_top_07961_WR", "FDN_top_02581_UR"},
}

// randomExpectedEvents is the random row's event-log size hint. Uniform-random
// play runs far longer logs than the bot: measured 2026-10-06 over 400 random
// pair-A games, final log length was min 2201, p50 6522, p95 8867, p99 9491,
// max 10299 -- 356 of 400 outgrew the default 4096 and paid a growEvents copy.
// 12288 (the next multiple of the 4096 preallocation above the observed max)
// lets a random game run its whole log without a single reallocation. The bot
// row keeps the default: its games (p50 2546, p95 3964) rarely reach 4096, so
// presizing there would only allocate more.
const randomExpectedEvents = 12288

type workload struct {
	reg     *cards.Registry
	deckDir string
	burn    string
	// expectedEvents is the event-capacity hint the row passes to every game
	// (rules.Config.ExpectedEvents). 0 keeps the engine default.
	expectedEvents int
}

// openCorpus opens the corpus: the whole corpus, as every gorge bench did at
// a4af596. A build tagged enginebench_subset could replace this with
// cards.OpenCorpusFor over the workloads' card names; S4 (pointer-free corpus
// design) made OpenCorpusFor a wrapper over SharedCorpus and S8 deletes it,
// so the tag and its shim are gone and there is one corpus open.
var openCorpus = func(dir string, names []string) (*cards.Registry, error) {
	reg, err := testutil.OpenCorpusRegistry(dir)
	if err != nil {
		return nil, fmt.Errorf("opening corpus %s: %w", dir, err)
	}
	return reg, nil
}

// decks resolves a pair name to its two decks (seat order A, B).
func (w workload) decks(pair string) ([2][]*cards.Card, [2]string, error) {
	var out [2][]*cards.Card
	if pair == "burn" {
		d, err := w.jsonDeck(w.burn)
		if err != nil {
			return out, [2]string{}, err
		}
		return [2][]*cards.Card{d, d}, [2]string{"Burn", "Burn"}, nil
	}
	names, ok := pairs[pair]
	if !ok {
		return out, names, fmt.Errorf("unknown pair %q", pair)
	}
	for i, n := range names {
		d, err := w.dckDeck(filepath.Join(w.deckDir, n+".dck"))
		if err != nil {
			return out, names, err
		}
		out[i] = d
	}
	return out, names, nil
}

type deckEntry struct {
	Name  string
	Count int
}

// dckEntries reads a Forge .dck main deck: "N [SET:num] Name" lines; NAME:
// and section headers are skipped, a [sideboard] section ends the main deck.
func dckEntries(path string) ([]deckEntry, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []deckEntry
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "NAME:") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if strings.EqualFold(line, "[sideboard]") {
				break
			}
			continue
		}
		sp := strings.IndexByte(line, ' ')
		if sp < 0 {
			return nil, fmt.Errorf("%s: bad line %q", path, line)
		}
		n, err := strconv.Atoi(line[:sp])
		if err != nil {
			return nil, fmt.Errorf("%s: bad count in %q", path, line)
		}
		name := strings.TrimSpace(line[sp+1:])
		if strings.HasPrefix(name, "[") {
			if j := strings.IndexByte(name, ']'); j >= 0 {
				name = strings.TrimSpace(name[j+1:])
			}
		}
		if j := strings.IndexByte(name, '|'); j >= 0 {
			name = strings.TrimSpace(name[:j])
		}
		out = append(out, deckEntry{name, n})
	}
	return out, sc.Err()
}

// jsonEntries reads a SpellBench pauper-kernel deck JSON.
func jsonEntries(path string) ([]deckEntry, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var d struct {
		Cards []struct {
			Name  string `json:"name"`
			Count int    `json:"count"`
		} `json:"cards"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	var out []deckEntry
	for _, e := range d.Cards {
		out = append(out, deckEntry{e.Name, e.Count})
	}
	return out, nil
}

func (w workload) dckDeck(path string) ([]*cards.Card, error) {
	es, err := dckEntries(path)
	if err != nil {
		return nil, err
	}
	return w.resolve(path, es)
}

func (w workload) jsonDeck(path string) ([]*cards.Card, error) {
	es, err := jsonEntries(path)
	if err != nil {
		return nil, err
	}
	return w.resolve(path, es)
}

func (w workload) resolve(path string, es []deckEntry) ([]*cards.Card, error) {
	var out []*cards.Card
	for _, e := range es {
		c, err := w.lookup(e.Name)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		for range e.Count {
			out = append(out, c)
		}
	}
	return out, nil
}

// allNames is every card name any workload seats (both FDN pairs and the
// Burn list), for a build whose corpus shim opens only those cards.
func allNames(deckDir, burn string) ([]string, error) {
	var names []string
	for _, pr := range []string{"A", "B"} {
		for _, n := range pairs[pr] {
			es, err := dckEntries(filepath.Join(deckDir, n+".dck"))
			if err != nil {
				return nil, err
			}
			for _, e := range es {
				names = append(names, e.Name)
			}
		}
	}
	es, err := jsonEntries(burn)
	if err != nil {
		return nil, err
	}
	for _, e := range es {
		names = append(names, e.Name)
	}
	return names, nil
}

func (w workload) lookup(name string) (*cards.Card, error) {
	if c, ok := w.reg.Lookup(name); ok {
		return c, nil
	}
	return nil, fmt.Errorf("card %q is not in the corpus", name)
}

// config is game g of a pair: seats swap every game (docs/015 §6), and the
// engine seed is base+g.
func (w workload) config(decks [2][]*cards.Card, names [2]string, base uint64, g int) rules.Config {
	a, b := 0, 1
	if g%2 == 1 {
		a, b = 1, 0
	}
	return rules.Config{
		Seed:           base + uint64(g),
		Names:          []string{names[a], names[b]},
		Decks:          [][]*cards.Card{decks[a], decks[b]},
		Tokens:         w.reg.Tokens,
		ExpectedEvents: w.expectedEvents,
	}
}
