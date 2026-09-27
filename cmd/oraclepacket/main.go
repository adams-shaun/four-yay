// Command oraclepacket prints the printed-card view (name, cost, type line,
// P/T, Oracle text, oracle_sha) of corpus cards -- the ONLY card text an
// Oracle-text audit author sees (docs/superpowers/specs/
// 2026-09-27-oracle-text-card-audit.md). It never prints script lines.
//
//	go run ./cmd/oraclepacket "Fatal Push" "Relic Vial"
//	go run ./cmd/oraclepacket -decks internal/testutil/decks -grep 'as long as'
//	go run ./cmd/oraclepacket -grep '(?i)\brevolt\b' -names
//
// -grep selects cards by a regexp over their Oracle text (the whole corpus,
// or only the repo decks with -decks), which is how an audit batch is chosen
// by mechanism without reading any script. The output is corpus-derived:
// write it to a gitignored path, never commit it.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/deck"
)

func main() {
	corpus := flag.String("corpus", ".cards", "corpus directory (holds cardsfolder/)")
	decks := flag.String("decks", "", "restrict to the cards of every *.json deck in this directory")
	grep := flag.String("grep", "", "select cards whose Oracle text matches this regexp")
	namesOnly := flag.Bool("names", false, "print only the selected card names")
	flag.Parse()

	reg, err := cards.SharedCorpus(*corpus)
	if err != nil {
		fmt.Fprintln(os.Stderr, "oraclepacket:", err)
		os.Exit(1)
	}
	var pool []*cards.Card
	switch {
	case flag.NArg() > 0:
		for _, n := range flag.Args() {
			c, ok := reg.Lookup(n)
			if !ok {
				fmt.Fprintf(os.Stderr, "oraclepacket: %q not in the corpus\n", n)
				os.Exit(1)
			}
			pool = append(pool, c)
		}
	case *decks != "":
		paths, _ := filepath.Glob(filepath.Join(*decks, "*.json"))
		seen := map[string]bool{}
		for _, p := range paths {
			_, cs, err := deck.Load(reg, p)
			if err != nil {
				fmt.Fprintf(os.Stderr, "oraclepacket: %s: %v\n", p, err)
				continue
			}
			for _, c := range cs {
				if name := c.Faces[0].Name; !seen[name] {
					seen[name] = true
					pool = append(pool, c)
				}
			}
		}
	case *grep != "":
		pool = append(pool, reg.Cards...)
	default:
		flag.Usage()
		os.Exit(2)
	}
	var re *regexp.Regexp
	if *grep != "" {
		re = regexp.MustCompile(*grep)
	}
	sort.Slice(pool, func(i, j int) bool { return pool[i].Faces[0].Name < pool[j].Faces[0].Name })
	for _, c := range pool {
		if re != nil {
			hit := false
			for _, f := range c.Faces {
				hit = hit || re.MatchString(f.OracleText())
			}
			if !hit {
				continue
			}
		}
		if *namesOnly {
			fmt.Println(c.Faces[0].Name)
			continue
		}
		fmt.Println(cards.OraclePacket(c))
	}
}
