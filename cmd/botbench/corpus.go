package main

import (
	"flag"
	"fmt"
	"io"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/deck"
	"github.com/adams-shaun/gorge/internal/testutil"
)

var corpusFull = flag.Bool("corpus-full", false, "open the whole card corpus even when the run's decks are known up front (by default such a run decodes only its decks' cards plus every token -- cards.OpenCorpusFor -- and plays byte-identical games)")

// openCorpusForDecks opens dir's corpus for a run that seats only decks:
// cards.OpenCorpusFor over every card the files name, which decodes just
// those cards (plus every token) unless a game could then play differently,
// and the whole SharedCorpus under -corpus-full. log, when non-nil, gets one
// line saying which registry the run got.
func openCorpusForDecks(dir string, decks []deck.File, log io.Writer) (*cards.Registry, error) {
	if *corpusFull {
		return testutil.OpenCorpusRegistry(dir)
	}
	var names []string
	for _, f := range decks {
		names = append(names, f.CardNames()...)
	}
	reg, err := cards.OpenCorpusFor(dir, names)
	if err != nil || log == nil {
		return reg, err
	}
	if reg.IsSubset() {
		fmt.Fprintf(log, "corpus: %d-card subset (+%d tokens) for %d decks\n", len(reg.Cards), len(reg.Tokens), len(decks))
	} else {
		fmt.Fprintf(log, "corpus: full, %d cards (the decks' cards need the whole name universe, or the subset could not be opened)\n", len(reg.Cards))
	}
	return reg, nil
}
