// Command kshadowcheck measures the kernel shadow offline over recorded
// mtg-kernel games (scripts/spellbench-arena/kshadow_corpus.py output): for
// every recorded decision it stages the acting seat's observation into a
// gorge engine and reports per-field fidelity (kshadow.Compare), the staged
// decision kind against the kernel's, and candidate mapping coverage.
//
//	kshadowcheck -cards .cards -corpus 'DIR/*.jsonl.gz' [-nopumps] [-dump N]
package main

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/adams-shaun/gorge/internal/spellbench/kshadow"
	"github.com/adams-shaun/gorge/internal/spellbench/v1agent"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

type rec struct {
	Header   bool            `json:"header"`
	GameID   string          `json:"game_id"`
	Decks    []string        `json:"decks"`
	Step     int64           `json:"step"`
	Seat     string          `json:"seat"`
	Decision json.RawMessage `json:"decision"`
	Pick     int             `json:"pick"`
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("kshadowcheck", flag.ContinueOnError)
	dir := fs.String("cards", ".cards", "corpus directory")
	glob := fs.String("corpus", "", "recorded game files (glob)")
	noPumps := fs.Bool("nopumps", false, "do not stage unexplained P/T and keyword deltas")
	dump := fs.Int("dump", 0, "print the first N decisions' candidates and staged options")
	limit := fs.Int("limit", 0, "stop after N decisions (0: all)")
	dumpUnmapped := fs.String("dump-unmapped", "", "dump decisions whose unmapped candidates contain this substring (with -dump N as the cap)")
	savePool := fs.String("save-pool", "", "write the pool registry (pauper-kernel catalog cards + tokens) to this .gob.gz and exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	reg, err := kshadow.OpenRegistry(*dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if *savePool != "" {
		pool, err := kshadow.PoolRegistry(reg)
		if err == nil {
			err = pool.Save(*savePool)
		}
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "pool registry: %d cards, %d tokens -> %s\n", len(pool.Cards), len(pool.Tokens), *savePool)
		return 0
	}
	files, _ := filepath.Glob(*glob)
	sort.Strings(files)
	fid := kshadow.NewFidelity()
	lossy := map[string]int{}
	kinds := map[string]int{}
	cov := kshadow.NewCoverage()
	fatal, unmapped, unused := map[string]int{}, map[string]int{}, map[string]int{}
	n, dumped := 0, 0
	var buildNS int64
	for _, fn := range files {
		f, err := os.Open(fn)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		zr, err := gzip.NewReader(f)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		sc := bufio.NewScanner(zr)
		sc.Buffer(make([]byte, 1<<20), 1<<26)
		var setup *kshadow.Setup
		var game string
		for sc.Scan() {
			var r rec
			if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			if r.Header {
				game = r.GameID
				setup, err = kshadow.NewSetup(reg, [2]string{r.Decks[0], r.Decks[1]})
				if err != nil {
					fmt.Fprintln(stderr, err)
					return 1
				}
				continue
			}
			if r.Decision == nil || setup == nil {
				continue
			}
			d, err := v1agent.ParseDecision(r.Decision)
			if err != nil || d.Kernel == nil {
				fmt.Fprintln(stderr, "parse:", err)
				continue
			}
			class := kshadow.Classify(d)
			t0 := time.Now()
			sh := setup.Build(&d.Kernel.Obs, kshadow.Options{Seed: uint64(r.Step)*7919 + 1, NoPumps: *noPumps, Priority: class == kshadow.ClassPriority})
			buildNS += time.Since(t0).Nanoseconds()
			fid.Add(kshadow.Compare(sh, &d.Kernel.Obs))
			for _, l := range sh.Lossy {
				lossy[l]++
			}
			pk := "fatal"
			if sh.Fatal == "" {
				pk = string(sh.E.Pending().Kind)
			}
			kinds[class+" -> "+pk]++
			m := kshadow.MapDecision(sh, d)
			cov.Add(class, m)
			if sh.Fatal != "" {
				fatal[class+": "+sh.Fatal]++
			}
			for _, k := range m.UnmappedKinds {
				unmapped[class+": "+k]++
			}
			for _, k := range m.UnusedKinds {
				unused[k]++
			}
			hit := *dumpUnmapped == ""
			for _, k := range m.UnmappedKinds {
				if *dumpUnmapped != "" && strings.Contains(k, *dumpUnmapped) {
					hit = true
				}
			}
			if dumped < *dump && sh.Fatal == "" && hit {
				p := d.Kernel.Obs.Projection
				fmt.Fprintf(stdout, "  turn %d phase %s active %s prio %s stack %d pool %v lands %v\n", p.Turn, p.Phase, p.ActivePlayer, p.PriorityPlayer, len(p.Stack), p.ManaPools, sh.E.G.Step)
				dumped++
				fmt.Fprintf(stdout, "== %s step %d seat %s class %s pending %s\n", game, r.Step, r.Seat, class, sh.E.Pending().Kind)
				for i, c := range d.Candidates {
					fmt.Fprintf(stdout, "  k%-2d %s\n", i, string(c.Semantic.Raw))
				}
				for _, o := range sh.E.Pending().Options {
					fmt.Fprintf(stdout, "  g%-2d %s %q obj=%d\n", o.Index, o.Kind, o.Label, o.Obj)
				}
				for j, a := range sh.E.Pending().PaymentActions {
					base := -1
					if a.BaseOptionIndex != nil {
						base = *a.BaseOptionIndex
					}
					fmt.Fprintf(stdout, "  pay%-2d %q obj=%d origin=%s base=%d plans=%d\n", j, a.Label, a.Cast.Object, a.Cast.Origin, base, len(a.Plans))
				}
				fmt.Fprintf(stdout, "  map: %v\n", m)
			}
			n++
			if *limit > 0 && n >= *limit {
				break
			}
		}
		zr.Close()
		f.Close()
		if *limit > 0 && n >= *limit {
			break
		}
	}
	fmt.Fprintf(stdout, "decisions %d, build %.2f ms/decision\n", n, float64(buildNS)/1e6/float64(max(n, 1)))
	fmt.Fprintf(stdout, "\n%-26s %8s %8s %7s  %s\n", "field", "checked", "mismatch", "rate", "example")
	for _, k := range fid.Fields() {
		c, m := fid.Checked[k], fid.Mismatch[k]
		fmt.Fprintf(stdout, "%-26s %8d %8d %6.2f%%  %s\n", k, c, m, 100*float64(m)/float64(c), trunc(fid.Examples[k], 90))
	}
	fmt.Fprintln(stdout, "\nstaged decision kind (kernel class -> gorge pending):")
	printCounts(stdout, kinds)
	fmt.Fprintln(stdout, "\nlossy staging:")
	printCounts(stdout, lossy)
	fmt.Fprintln(stdout, "\nfatal staging:")
	printCounts(stdout, fatal)
	fmt.Fprintln(stdout, "\nunmapped kernel candidates:")
	printCounts(stdout, unmapped)
	fmt.Fprintln(stdout, "\ngorge options with no kernel candidate:")
	printCounts(stdout, unused)
	fmt.Fprintln(stdout, "\nmapping coverage:")
	cov.Print(stdout)
	return 0
}

func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

func printCounts(w io.Writer, m map[string]int) {
	type kv struct {
		k string
		v int
	}
	var xs []kv
	for k, v := range m {
		xs = append(xs, kv{k, v})
	}
	sort.Slice(xs, func(i, j int) bool {
		if xs[i].v != xs[j].v {
			return xs[i].v > xs[j].v
		}
		return xs[i].k < xs[j].k
	})
	for i, x := range xs {
		if i >= 40 {
			fmt.Fprintf(w, "  ... %d more\n", len(xs)-i)
			break
		}
		fmt.Fprintf(w, "  %6d  %s\n", x.v, strings.TrimSpace(x.k))
	}
}
