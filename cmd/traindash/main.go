// traindash is a read-only, live web dashboard over policynet/PPO training
// runs (cmd/exitloop output). Usage:
//
//	go run ./cmd/traindash -root /mnt/sata/gorge-training -addr 127.0.0.1:8086
//
// It discovers every exitloop run directory under each -root (repeatable or
// comma-separated) -- a directory holding plan.txt, timing.tsv or roundN/genN
// subdirectories -- groups them by experiment (the root's top-level directory,
// e.g. pn15), and serves one embedded page that polls /api/runs every 10s:
// an overview card per experiment (status, stage, latest eval vs the bot
// control), a cross-experiment win-rate compare chart with CI bands, a per-run
// detail view (per-kind KL/clip/flip against the -ppo-kl target, mean_adv,
// value log loss, per-pair win rates, timing, stderr tail), an alerts banner
// (eval below control-0.05, a drop over 0.08 round-on-round, a kind's
// final_kl over 3x the target) and the markdown reports found under the root.
// It also surfaces ad-hoc runs (kind "adhoc"): a directory below an experiment
// holding report.md and/or *.jsonl, or a <name>.stdout/.stderr pair with no
// <name> directory -- listed with status, jsonl size and record count and the
// report.md tail, never alerted on.
// Every API call rescans (parsed files are cached by mtime and size); it never
// writes under a root.
package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
)

//go:embed index.html
var assets embed.FS

type rootList []string

func (r *rootList) String() string { return strings.Join(*r, ",") }
func (r *rootList) Set(v string) error {
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			*r = append(*r, p)
		}
	}
	return nil
}

// defaultDeckRoot is where sb-gauntlet.sh keeps its per-candidate-run
// matches.jsonl ledgers (gauntlet/ref/<key>/), the narrow tree the per-deck
// panel reads. It is deliberately not the training root: that holds hundreds
// of unrelated scratch ledgers.
const defaultDeckRoot = "/mnt/sata/gorge-training/spellbench-work/gauntlet"

func main() {
	var roots rootList
	flag.Var(&roots, "root", "training root to scan (repeatable or comma list; default /mnt/sata/gorge-training)")
	addr := flag.String("addr", "127.0.0.1:8086", "listen address (never 8080/8081: the demo)")
	var deckFocus rootList
	flag.Var(&deckFocus, "deck-focus", "policy name(s) whose per-deck win rate the matches.jsonl ledgers report (repeatable or comma list; default: each ledger's dominant policy)")
	var deckRoots rootList
	flag.Var(&deckRoots, "deck-root", "directory scanned for matches.jsonl per-deck ledgers, separate from -root (repeatable or comma list; default /mnt/sata/gorge-training/spellbench-work/gauntlet)")
	flag.Parse()
	if len(roots) == 0 {
		roots = rootList{"/mnt/sata/gorge-training"}
	}
	if len(deckRoots) == 0 {
		deckRoots = rootList{defaultDeckRoot}
	}
	for _, r := range roots {
		if fi, err := os.Stat(r); err != nil || !fi.IsDir() {
			log.Fatalf("traindash: root %q is not a directory", r)
		}
	}
	// A missing deck root is not fatal: the per-deck panel simply stays
	// empty, and the rest of the dashboard still works. Only the training
	// roots are required.
	var presentDeckRoots rootList
	for _, r := range deckRoots {
		if fi, err := os.Stat(r); err == nil && fi.IsDir() {
			presentDeckRoots = append(presentDeckRoots, r)
		}
	}
	s := NewScanner(roots)
	s.DeckFocus = deckFocus
	s.DeckRoots = presentDeckRoots
	log.Printf("traindash: serving %s on http://%s/", roots.String(), *addr)
	log.Fatal(http.ListenAndServe(*addr, newMux(s)))
}

func newMux(s *Scanner) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		b, _ := assets.ReadFile("index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(b)
	})
	mux.HandleFunc("GET /api/runs", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, s.Scan())
	})
	// /api/artifact?id=<id> serves only an artifact the scan itself listed,
	// so the id can never name a path outside the roots.
	mux.HandleFunc("GET /api/artifact", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		for _, root := range s.Roots {
			for _, a := range findArtifacts(root, len(s.Roots) > 1) {
				if a.ID != id {
					continue
				}
				b, err := os.ReadFile(a.Path)
				if err != nil {
					http.Error(w, err.Error(), http.StatusNotFound)
					return
				}
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				w.Write(b)
				return
			}
		}
		http.Error(w, fmt.Sprintf("no artifact %q", id), http.StatusNotFound)
	})
	return mux
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	enc := json.NewEncoder(w)
	if err := enc.Encode(v); err != nil {
		log.Printf("traindash: encode: %v", err)
	}
}
