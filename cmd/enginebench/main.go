// Command enginebench re-measures gorge on the rows of draft-zero's engine
// comparison (docs/015-rules-engine-comparison.md §1, method in §6): random
// play turns/s on two FDN pairs and the Pauper Burn mirror, the heuristic
// bot's games/s, Engine.Clone cost on a mid-game state, one search step
// (Clone + Submit), and the built-in searches' per-thread rates.
//
// One invocation runs ONE row once and appends one JSON line to -out, so a
// driver script can interleave builds and repetitions (A/B/C per repetition)
// and pin each run to one core. Every row is a pure function of its flags
// and the corpus except the wall-clock readings it reports.
//
// Compatibility: the harness uses only APIs that exist at a4af596 (the pin
// docs/015 measured) and at today's main: rules.New/Advance/Pending/Submit/
// Clone, decision.Decision/Intent, seat.NewBot(+EnableAutoPayMana),
// botpolicy.BoardFromGameInto, internal/bench.PlayGame, internal/searchseat
// (Feed, NewSearchBot, Watch) and testutil.OpenCorpusRegistry. The azmcts
// row lives behind the build tag enginebench_az (az.go), because
// internal/azmcts does not exist at a4af596; built without the tag, -row az
// reports "unavailable".
//
// The wall clock is read only to report elapsed time; no game, decision or
// seed reads it (internal/archtest's time allowlist names this command).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
	"strings"
	"syscall"
	"time"

	"github.com/adams-shaun/gorge/rules/resolve"
)

type result struct {
	Label string  `json:"label"`
	Row   string  `json:"row"`
	Pair  string  `json:"pair,omitempty"`
	Rep   int     `json:"rep"`
	Unix  int64   `json:"unix"`
	Load1 float64 `json:"load1"`
	Procs int     `json:"gomaxprocs"`
	// HeapMB is the live heap after the corpus is loaded (a forced GC): the
	// floor every GC cycle of the run marks.
	HeapMB float64 `json:"heap_live_mb"`
	Corpus string  `json:"corpus"`   // "full" or "subset"
	Secs   float64 `json:"secs"`     // wall seconds of the timed region
	CPU    float64 `json:"cpu_secs"` // process CPU seconds (user+sys) of the timed region
	Err    string  `json:"err,omitempty"`
	Detail any     `json:"detail,omitempty"`

	Games     int     `json:"games,omitempty"`
	Turns     int     `json:"turns,omitempty"`
	Decisions int     `json:"decisions,omitempty"`
	Stalls    int     `json:"stalls,omitempty"`
	TurnsPS   float64 `json:"turns_per_s,omitempty"`
	GamesPS   float64 `json:"games_per_s,omitempty"`
	DecPS     float64 `json:"decisions_per_s,omitempty"`

	// clone/step rows: means over the roots of this run.
	USPer       float64 `json:"us_per_op,omitempty"`
	USPerCPU    float64 `json:"us_per_op_cpu,omitempty"`    // GC-free batches
	USPerCPUGC  float64 `json:"us_per_op_cpu_gc,omitempty"` // default GOGC, collections paid
	AllocBPer   float64 `json:"alloc_bytes_per_op,omitempty"`
	MallocsPer  float64 `json:"mallocs_per_op,omitempty"`
	RetainedPer float64 `json:"retained_bytes_per_clone,omitempty"`

	// search rows
	Roots   int     `json:"roots,omitempty"`
	Sims    int     `json:"sims,omitempty"`
	SimsPS  float64 `json:"sims_per_s,omitempty"`
	SimsPSC float64 `json:"sims_per_cpu_s,omitempty"`
	MSPerRt float64 `json:"ms_per_root,omitempty"`
}

func main() {
	row := flag.String("row", "", "random | bot | clone | step | az | sampler")
	pair := flag.String("pair", "A", "A | B | burn (random, bot); search rows use A+B")
	deckDir := flag.String("decks", "/mnt/sata/gorge-training/searchbench/draft-zero/assets/sample/decks", "directory of FDN .dck files")
	burn := flag.String("burn", "/mnt/sata/gorge-training/enginecmp/decks/burn.json", "Burn mirror deck (SpellBench pauper-kernel JSON)")
	cardsDir := flag.String("cards", ".cards", "corpus directory")
	secs := flag.Float64("secs", 20, "minimum timed wall seconds for game-playing and search rows")
	seed := flag.Uint64("seed", 1, "base seed; game g uses seed+g")
	autopay := flag.Bool("autopay", false, "bot row: the auto-pay bot (EnableAutoPayMana) instead of manual taps")
	sims := flag.Int("sims", 100, "az row: simulations per searched decision")
	copies := flag.Int("copies", 2000, "clone/step rows: timed copies per root")
	label := flag.String("label", "", "build label recorded in the output")
	rep := flag.Int("rep", 0, "repetition index recorded in the output")
	out := flag.String("out", "", "append the JSON result line here (default stdout)")
	cpuprof := flag.String("cpuprofile", "", "write a CPU profile of the whole run here")
	flag.Parse()
	if *cpuprof != "" {
		f, err := os.Create(*cpuprof)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	r := result{Label: *label, Row: *row, Pair: *pair, Rep: *rep, Unix: time.Now().Unix(), Load1: load1(), Procs: runtime.GOMAXPROCS(0)}
	err := func() error {
		names, err := allNames(*deckDir, *burn)
		if err != nil {
			return err
		}
		reg, err := openCorpus(*cardsDir, names)
		if err != nil {
			return err
		}
		w := workload{reg: reg, deckDir: *deckDir, burn: *burn}
		runtime.GC()
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		r.HeapMB = float64(ms.HeapAlloc) / (1 << 20)
		r.Corpus = corpusMode
		switch *row {
		case "random":
			return runRandom(&r, w, *pair, *seed, *secs)
		case "bot":
			return runBot(&r, w, *pair, *seed, *secs, *autopay)
		case "clone":
			return runClone(&r, w, *seed, *copies, false)
		case "step":
			return runClone(&r, w, *seed, *copies, true)
		case "az":
			return runSearch(&r, w, *seed, *secs, "az", *sims)
		case "sampler":
			return runSearch(&r, w, *seed, *secs, "sampler", 0)
		default:
			return fmt.Errorf("unknown -row %q", *row)
		}
	}()
	if err != nil {
		r.Err = err.Error()
	}
	if st := resolve.ReadStats(); st != (resolve.Stats{}) {
		// The resolution kernel's counters for this run (a kernel-on
		// binary), on stderr.
		fmt.Fprintf(os.Stderr, "resolve %+v\n", st)
	}
	b, _ := json.Marshal(r)
	if *out == "" {
		fmt.Println(string(b))
	} else {
		f, ferr := os.OpenFile(*out, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if ferr != nil {
			fmt.Fprintln(os.Stderr, ferr)
			os.Exit(1)
		}
		f.Write(append(b, '\n'))
		f.Close()
		fmt.Println(string(b))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "enginebench:", err)
		os.Exit(1)
	}
}

func load1() float64 {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0
	}
	var l float64
	fmt.Sscanf(strings.Fields(string(b))[0], "%g", &l)
	return l
}

// cpuNow is the process's CPU seconds (user+sys). The box these runs share is
// loaded, so a pinned single-threaded run also reports CPU time: wall time
// there includes waiting for the core.
func cpuNow() float64 {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0
	}
	return float64(ru.Utime.Sec+ru.Stime.Sec) + float64(ru.Utime.Usec+ru.Stime.Usec)/1e6
}
