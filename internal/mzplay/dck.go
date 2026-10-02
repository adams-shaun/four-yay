package mzplay

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// XMage's .dck deck file, as draft-zero's tools/extract_decks.py writes it
// (write_dck) and XMage's DckDeckImporter reads it:
//
//	NAME:<deck name>
//	<count> [<SET>:<number>] <card name>
//	SB: <count> [<SET>:<number>] <card name>
//	LAYOUT MAIN:... / LAYOUT SIDEBOARD:...
//
// The set code and collector number pick an XMage printing; gorge resolves a
// card by name, so they are kept only for the record. A line without the
// bracket ("3 Swamp") is accepted, as XMage accepts it.

// DeckEntry is one line of a deck.
type DeckEntry struct {
	Count  int
	Set    string
	Number string
	Name   string
}

// DeckList is a parsed .dck file.
type DeckList struct {
	Name string
	Main []DeckEntry
	Side []DeckEntry
}

// ParseDCK reads a .dck file.
func ParseDCK(r io.Reader) (DeckList, error) {
	var d DeckList
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for ln := 1; sc.Scan(); ln++ {
		line := strings.TrimSpace(strings.TrimRight(sc.Text(), "\r"))
		switch {
		case line == "" || strings.HasPrefix(line, "#"):
			continue
		case strings.HasPrefix(line, "NAME:"):
			d.Name = strings.TrimSpace(line[len("NAME:"):])
			continue
		case strings.HasPrefix(line, "LAYOUT "):
			continue
		}
		side := false
		if strings.HasPrefix(line, "SB:") {
			side = true
			line = strings.TrimSpace(line[len("SB:"):])
		}
		e, err := parseDeckLine(line)
		if err != nil {
			return DeckList{}, fmt.Errorf("line %d: %w", ln, err)
		}
		if side {
			d.Side = append(d.Side, e)
		} else {
			d.Main = append(d.Main, e)
		}
	}
	if err := sc.Err(); err != nil {
		return DeckList{}, err
	}
	return d, nil
}

func parseDeckLine(line string) (DeckEntry, error) {
	sp := strings.IndexByte(line, ' ')
	if sp < 0 {
		return DeckEntry{}, fmt.Errorf("%q is not \"<count> [SET:number] <name>\"", line)
	}
	n, err := strconv.Atoi(line[:sp])
	if err != nil || n < 1 {
		return DeckEntry{}, fmt.Errorf("%q: count %q", line, line[:sp])
	}
	e := DeckEntry{Count: n}
	rest := strings.TrimSpace(line[sp+1:])
	if strings.HasPrefix(rest, "[") {
		end := strings.IndexByte(rest, ']')
		colon := strings.IndexByte(rest, ':')
		if end < 0 || colon < 0 || colon > end {
			return DeckEntry{}, fmt.Errorf("%q: printing %q is not [SET:number]", line, rest)
		}
		e.Set, e.Number = rest[1:colon], rest[colon+1:end]
		rest = strings.TrimSpace(rest[end+1:])
	}
	if rest == "" {
		return DeckEntry{}, fmt.Errorf("%q: no card name", line)
	}
	e.Name = rest
	return e, nil
}

// LoadDCK reads a .dck file from disk.
func LoadDCK(path string) (DeckList, error) {
	f, err := os.Open(path)
	if err != nil {
		return DeckList{}, err
	}
	defer f.Close()
	d, err := ParseDCK(f)
	if err != nil {
		return DeckList{}, fmt.Errorf("%s: %w", path, err)
	}
	return d, nil
}

// MainCount is the main deck's size.
func (d DeckList) MainCount() int {
	n := 0
	for _, e := range d.Main {
		n += e.Count
	}
	return n
}

// CardLookup is the part of cards.Registry a deck is resolved against.
type CardLookup interface {
	Lookup(name string) (*cards.Card, bool)
}

// Resolve expands the main deck into cards, in list order, and returns the
// names the corpus does not hold (each once, in first-seen order). A deck
// with a missing name must not be played: the caller refuses it.
func (d DeckList) Resolve(reg CardLookup) (deck []*cards.Card, missing []string) {
	seen := map[string]bool{} // membership only -- never ranged.
	for _, e := range d.Main {
		c, ok := reg.Lookup(e.Name)
		if !ok {
			if !seen[e.Name] {
				seen[e.Name] = true
				missing = append(missing, e.Name)
			}
			continue
		}
		for i := 0; i < e.Count; i++ {
			deck = append(deck, c)
		}
	}
	return deck, missing
}

// Names lists the distinct card names of the main deck and sideboard.
func (d DeckList) Names() []string {
	seen := map[string]bool{} // membership only -- never ranged.
	var out []string
	for _, list := range [][]DeckEntry{d.Main, d.Side} {
		for _, e := range list {
			if !seen[e.Name] {
				seen[e.Name] = true
				out = append(out, e.Name)
			}
		}
	}
	return out
}

// DeckStem is ParallelDataGenerator.extractDeckName: the file name without
// its directory and without everything from the path's last dot on. The
// loop joins GAME_SUMMARY's deck_a/deck_b against its deck table by this
// string. A dot in a directory name with no extension after it makes
// upstream's substring throw; here that is the empty name.
func DeckStem(path string) string {
	slash := max(strings.LastIndexByte(path, '\\'), strings.LastIndexByte(path, '/'))
	end := len(path)
	if dot := strings.LastIndexByte(path, '.'); dot > -1 {
		end = dot
	}
	if slash+1 > end {
		return ""
	}
	return path[slash+1 : end]
}

// ReadPool reads a deck-pool file: one .dck path per line, blank lines and
// lines starting '#' skipped (ParallelDataGenerator.chooseDeck).
func ReadPool(path string) ([]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("mzplay: deck pool: %w", err)
	}
	var out []string
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) != "" && !strings.HasPrefix(line, "#") {
			out = append(out, strings.TrimSpace(line))
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("mzplay: empty deck pool: %s", path)
	}
	return out, nil
}

// ChooseDeck is ParallelDataGenerator.chooseDeck: "sequential" plays line
// gameIndex of the pool (wrapping), anything else draws uniformly with
// intn, the game's own deck stream.
func ChooseDeck(pool []string, mode string, gameIndex int, intn func(n int) int) string {
	if mode == "sequential" {
		return pool[gameIndex%len(pool)]
	}
	return pool[intn(len(pool))]
}
