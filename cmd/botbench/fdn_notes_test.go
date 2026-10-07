package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	gbench "github.com/adams-shaun/gorge/internal/bench"
	"github.com/adams-shaun/gorge/internal/spellbench"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
)

// TestFDNNoteSweep is a report, not a gate: it plays the FDN Limited pool and
// tallies every Note the engine logs, by source card and text. A Note is how
// a primitive says "I could not do this as written and used a default"
// (an unresolved amount, an unimplemented parameter), so the tally is the
// list of FDN cards that play, but not as printed -- the class the parameter
// census cannot see, because it counts a parameter as read once the code
// looks at it. Off unless FDN_NOTE_SWEEP_OUT names the TSV to write;
// FDN_NOTE_SWEEP_GAMES is the games per deck and matchup (default 20).
func TestFDNNoteSweep(t *testing.T) {
	out := os.Getenv("FDN_NOTE_SWEEP_OUT")
	if out == "" {
		t.Skip("set FDN_NOTE_SWEEP_OUT to run the FDN note sweep")
	}
	games := 20
	if v := os.Getenv("FDN_NOTE_SWEEP_GAMES"); v != "" {
		if _, err := fmt.Sscanf(v, "%d", &games); err != nil || games <= 0 {
			t.Fatalf("FDN_NOTE_SWEEP_GAMES %q", v)
		}
	}
	reg := testutil.CorpusRegistry(t)
	setTacticalRegistry(reg)
	cat, err := spellbench.CatalogByID("fdn")
	if err != nil {
		t.Fatal(err)
	}
	type key struct{ card, text string }
	notes := map[key]int{}
	noteGames := map[key]int{}
	matchups := [][2]string{{"bot", "bot"}, {"sb-tactical", "bot"}, {"bot", "sb-tactical"}}
	// FDN_NOTE_SWEEP_TRACE=<substring> logs the events around the first
	// three notes whose text contains it.
	trace, traced := os.Getenv("FDN_NOTE_SWEEP_TRACE"), 0
	played, failed := 0, 0
	play := func(label string, slot int, deck []*cards.Card) {
		for mi, m := range matchups {
			for g := 0; g < games; g++ {
				seed := sbGameSeed(20261002, slot*len(matchups)+mi, g)
				var res sbResult
				seats := []seat.Seat{policies[m[0]](seed ^ 1), policies[m[1]](seed ^ 2)}
				cfg := rules.Config{Seed: seed, Names: []string{"p0", "p1"}, Decks: [][]*cards.Card{deck, deck},
					Tokens: reg.Tokens, NameUniverse: reg.Universe()}
				_, e, err := gbench.PlayGame(cfg, seats, 200, 20000, gbench.Hooks{Submit: sbSubmitWithFallback(seats, &res)})
				played++
				if err != nil || e == nil {
					failed++
					t.Logf("%s %v game %d: %v", label, m, g, err)
					continue
				}
				seen := map[key]bool{}
				for i := range e.L.Events {
					ev := &e.L.Events[i]
					if ev.Kind != events.Note {
						continue
					}
					if trace != "" && traced < 3 && strings.Contains(ev.Text, trace) {
						traced++
						t.Logf("TRACE %s %v game %d seed %d: note %q at event %d", label, m, g, seed, ev.Text, i)
						for j := max(0, i-25); j <= min(len(e.L.Events)-1, i+3); j++ {
							x := &e.L.Events[j]
							name := ""
							if o := e.G.Obj(x.Obj); o != nil && o.Face() != nil {
								name = o.Face().Name
							}
							t.Logf("  %d %s p%d obj=%d(%s) %s->%s amt=%d ctr=%q ids=%v text=%q", j, x.Kind, x.Player, x.Obj, name, x.From, x.To, x.Amount, x.Counter, x.IDs, x.Text)
						}
					}
					k := key{card: "-", text: noteShape(ev.Text)}
					if o := e.G.Obj(ev.Obj); o != nil && o.Face() != nil {
						k.card = o.Face().Name
					}
					notes[k]++
					if !seen[k] {
						seen[k] = true
						noteGames[k]++
					}
				}
			}
		}
	}
	for di, deckID := range cat.Pool {
		deck, err := spellbench.Deck(reg, cat.Dir, deckID)
		if err != nil {
			t.Fatal(err)
		}
		play(deckID, di, deck)
	}
	// The pool's 16 decks hold about 170 of the 281 FDN cards. The synthetic
	// decks cover the rest: for every colour pair, the FDN non-basics castable
	// in those two colours, dealt 23 at a time (one copy each) beside 17
	// basics, so every card sits in a deck that can cast it.
	if os.Getenv("FDN_NOTE_SWEEP_SYNTH") != "" {
		for si, d := range fdnSynthDecks(t, reg) {
			play(fmt.Sprintf("synth%d", si), len(cat.Pool)+si, d)
		}
	}
	keys := make([]key, 0, len(notes))
	for k := range notes {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if noteGames[keys[i]] != noteGames[keys[j]] {
			return noteGames[keys[i]] > noteGames[keys[j]]
		}
		if keys[i].card != keys[j].card {
			return keys[i].card < keys[j].card
		}
		return keys[i].text < keys[j].text
	})
	var b strings.Builder
	fmt.Fprintf(&b, "# games %d failed %d\ngames\tnotes\tcard\ttext\n", played, failed)
	for _, k := range keys {
		fmt.Fprintf(&b, "%d\t%d\t%s\t%s\n", noteGames[k], notes[k], k.card, k.text)
	}
	if err := os.WriteFile(out, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("FDN note sweep: %d games, %d failed, %d distinct (card, note) rows -> %s", played, failed, len(keys), out)
}

// noteShape replaces every run of digits in a Note's text with '#', so notes
// that differ only by an object id or an amount tally together.
func noteShape(s string) string {
	var b strings.Builder
	digits := false
	for _, r := range s {
		if r >= '0' && r <= '9' {
			if !digits {
				b.WriteByte('#')
			}
			digits = true
			continue
		}
		digits = false
		b.WriteRune(r)
	}
	return b.String()
}

// fdnSynthDecks builds the synthetic two-colour FDN decks: for each of the
// ten colour pairs, the non-basic FDN cards whose mana cost uses only those
// colours, in name order, 23 distinct cards per deck with 17 basics.
func fdnSynthDecks(t *testing.T, reg *cards.Registry) [][]*cards.Card {
	t.Helper()
	universe, err := spellbench.FDNCards()
	if err != nil {
		t.Fatal(err)
	}
	basics := map[byte]string{'W': "Plains", 'U': "Island", 'B': "Swamp", 'R': "Mountain", 'G': "Forest"}
	lookup := func(name string) *cards.Card {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("FDN card %q is not in the corpus", name)
		}
		return c
	}
	var decks [][]*cards.Card
	const colours = "WUBRG"
	for a := 0; a < len(colours); a++ {
		for b := a + 1; b < len(colours); b++ {
			var pool []*cards.Card
			for _, fc := range universe {
				if fc.Basic {
					continue
				}
				c := lookup(fc.Name)
				ok := true
				for i := 0; i < len(c.Faces[0].ManaCost); i++ {
					ch := c.Faces[0].ManaCost[i]
					if strings.IndexByte(colours, ch) >= 0 && ch != colours[a] && ch != colours[b] {
						ok = false
					}
				}
				if ok {
					pool = append(pool, c)
				}
			}
			for at := 0; at < len(pool); at += 23 {
				var deck []*cards.Card
				for i := 0; i < 23; i++ {
					deck = append(deck, pool[(at+i)%len(pool)])
				}
				for i := 0; i < 17; i++ {
					col := colours[a]
					if i%2 == 1 {
						col = colours[b]
					}
					deck = append(deck, lookup(basics[col]))
				}
				decks = append(decks, deck)
			}
		}
	}
	return decks
}
