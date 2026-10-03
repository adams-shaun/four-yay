// Command headdiff shows WHAT moved a golden acceptance chain head, instead
// of four changed hashes. It plays the deterministic acceptance game at each
// seat count -- exactly the game rules' TestHeads pins, built by the same
// non-test code (testutil.AcceptanceDecks, rules.AcceptanceConfig,
// rules.PlayAcceptance, answered by botpolicy.GameBot) -- and either records the event streams or compares
// them against a recording, naming the first event where they part ways
// with replay.FirstDivergence, the byte comparison replay uses.
//
//	headdiff                    play every seat count; print each head and
//	                            whether it matches rules/testdata/heads/
//	headdiff -dump FILE         also write the event streams to FILE
//	headdiff -against FILE      compare this tree's streams with FILE's and
//	                            print the first divergent event per seat
//	                            count, with -context events either side
//	headdiff -seats 2,8 ...     only these seat counts (default all)
//
// FILE is JSON, one object per seat count ({"seats","seed","head","events"}),
// readable with jq; a recording whose events do not re-chain to its own head
// is refused.
//
// Reviewing a head move: build headdiff at the base ref into a temp
// directory, dump the base's streams, then compare on the branch. Run both
// from the branch checkout so `-dir .cards` resolves to the shared corpus:
//
//	tmp=$(mktemp -d /mnt/sata/gorge-training/gotmp/headdiff.XXXXXX)
//	mkdir "$tmp/src" && git archive main | tar -x -C "$tmp/src"
//	(cd "$tmp/src" && go build -o "$tmp/headdiff" ./cmd/headdiff)
//	"$tmp/headdiff" -dir .cards -dump "$tmp/main.json"
//	go run ./cmd/headdiff -against "$tmp/main.json"
//
// The base's deck lists come from its own embedded internal/testutil/decks,
// so the dump is main's game even though the corpus directory is shared. If
// the two builds differ in cards/ (CompilerFingerprint), each run recompiles
// the corpus IR cache for its own fingerprint, which takes a minute.
//
// The engine runs its fast paths in production mode here, where the rules
// test binary runs them in verify mode; TestFastPathsInProductionModeKeepTheHeads
// pins that both modes produce the same heads at 2 and 4 seats, and the
// pinned-head note shows it for every seat count on each run.
//
// Exit codes: 0 every compared stream identical (or no -against); 1 a
// divergence; 2 a usage, corpus or game error. The pinned-head note never
// changes the exit code: in the review workflow the base build runs from the
// branch checkout, whose pinned files it may rightly differ from.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/replay"
	"github.com/adams-shaun/gorge/rules"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// record is one seat count's recorded acceptance game.
type record struct {
	Seats  int            `json:"seats"`
	Seed   uint64         `json:"seed"`
	Head   string         `json:"head"`
	Events []events.Event `json:"events"`
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("headdiff", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", ".cards", "corpus directory (holds ir.gob.gz / cardsfolder)")
	seatsFlag := fs.String("seats", "all", "comma-separated seat counts to play, or all")
	headsDir := fs.String("heads", filepath.Join("rules", "testdata", "heads"), "pinned head files to check against (skipped when absent)")
	dump := fs.String("dump", "", "write each game's event stream to this file")
	against := fs.String("against", "", "compare each game's event stream with this recording")
	context := fs.Int("context", 5, "events of context to print around a divergence")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stderr, "usage: headdiff [-dir .cards] [-seats all|2,4,...] [-dump FILE] [-against FILE] [-context N]")
		return 2
	}
	seats, err := parseSeats(*seatsFlag)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	var ref map[int]record
	if *against != "" {
		if ref, err = readRecords(*against); err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		for _, n := range seats {
			if _, ok := ref[n]; !ok {
				fmt.Fprintf(stderr, "headdiff: %s has no %d-seat game\n", *against, n)
				return 2
			}
		}
	}
	reg, err := testutil.OpenCorpusRegistry(*dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	var out *json.Encoder
	if *dump != "" {
		f, err := os.Create(*dump)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		defer f.Close()
		out = json.NewEncoder(f)
	}

	code := 0
	for _, n := range seats {
		rec, err := play(reg, n)
		if err != nil {
			fmt.Fprintf(stderr, "headdiff: %d seats: %v\n", n, err)
			return 2
		}
		fmt.Fprintf(stdout, "%d seats: %d events, head %s%s\n", n, len(rec.Events), rec.Head, pinnedNote(*headsDir, n, rec.Head))
		if out != nil {
			if err := out.Encode(rec); err != nil {
				fmt.Fprintln(stderr, err)
				return 2
			}
		}
		if ref != nil {
			want := ref[n]
			d := replay.FirstDivergence(want.Events, rec.Events)
			if d == nil {
				fmt.Fprintf(stdout, "  identical to the recording (%d events, head %s)\n", len(want.Events), want.Head)
				continue
			}
			code = 1
			report(stdout, want, rec, d, *context)
		}
		rec.Events = nil // one game's stream held at a time
	}
	if *dump != "" {
		fmt.Fprintf(stdout, "wrote %s\n", *dump)
	}
	return code
}

// play plays the n-seat acceptance game and returns its stream.
func play(reg *cards.Registry, n int) (record, error) {
	names, decks, err := testutil.AcceptanceDecks(reg, n)
	if err != nil {
		return record{}, err
	}
	cfg := rules.AcceptanceConfig(reg, names, decks)
	e, _, err := rules.PlayAcceptance(cfg, acceptanceBot, nil)
	if err != nil {
		return record{}, err
	}
	return record{Seats: n, Seed: e.L.Seed, Head: e.L.Head(), Events: e.L.Events}, nil
}

// acceptanceBot is the acceptance game's bot: botpolicy.GameBot, the same
// adapter rules' testBot wraps, so this tool answers every decision exactly
// as TestHeads does.
func acceptanceBot(seed uint64) rules.Answerer {
	b := botpolicy.NewGameBot(seed)
	return func(e *rules.Engine, d *decision.Decision) decision.Intent { return b.Answer(e.G, e, d) }
}

// pinnedNote compares head with dir/<n>.txt when that file exists. It is
// informational only: in the review workflow the base build runs from the
// branch checkout, so it legitimately differs from a branch that re-pinned.
func pinnedNote(dir string, n int, head string) string {
	raw, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("%d.txt", n)))
	if err != nil {
		return ""
	}
	pinned := strings.TrimSpace(string(raw))
	if pinned == head {
		return " (matches the pinned head)"
	}
	return fmt.Sprintf(" (differs from the pinned head %s)", pinned)
}

func parseSeats(s string) ([]int, error) {
	if s == "all" {
		return rules.AcceptanceSeatCounts(), nil
	}
	valid := map[int]bool{}
	for _, n := range rules.AcceptanceSeatCounts() {
		valid[n] = true
	}
	var out []int
	for _, f := range strings.Split(s, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(f))
		if err != nil || !valid[n] {
			return nil, fmt.Errorf("headdiff: -seats %q: want all or a list of %v", s, rules.AcceptanceSeatCounts())
		}
		out = append(out, n)
	}
	return out, nil
}

// readRecords loads a -dump file and verifies each stream re-chains to its
// recorded head, so a lossy round trip can never pass for a real divergence.
func readRecords(path string) (map[int]record, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	out := map[int]record{}
	for {
		var r record
		if err := dec.Decode(&r); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, fmt.Errorf("headdiff: %s: %v", path, err)
		}
		l := events.Log{Seed: r.Seed, Events: r.Events}
		if got := l.HeadAt(len(r.Events)); got != r.Head {
			return nil, fmt.Errorf("headdiff: %s: the %d-seat stream chains to %s, not its recorded head %s", path, r.Seats, got, r.Head)
		}
		out[r.Seats] = r
	}
	return out, nil
}

// report prints one divergence: the shared prefix's last ctx events, then
// each side from the divergent event on.
func report(w io.Writer, want, got record, d *replay.Divergence, ctx int) {
	at := int(d.Seq)
	fmt.Fprintf(w, "  DIVERGED at event %d (recording: %d events, head %s; this tree: %d events, head %s)\n",
		at, len(want.Events), want.Head, len(got.Events), got.Head)
	switch {
	case d.Missing:
		fmt.Fprintf(w, "  the recording ends at event %d; this tree continues\n", at)
	case d.Short:
		fmt.Fprintf(w, "  this tree ends at event %d; the recording continues\n", at)
	}
	fmt.Fprintln(w, "  common prefix:")
	for i := max(0, at-ctx); i < at; i++ {
		fmt.Fprintf(w, "      %s\n", formatEvent(want.Events[i]))
	}
	side := func(label string, evs []events.Event) {
		fmt.Fprintf(w, "  %s:\n", label)
		if at >= len(evs) {
			fmt.Fprintln(w, "      (no more events)")
			return
		}
		for i := at; i < len(evs) && i <= at+ctx; i++ {
			mark := "    "
			if i == at {
				mark = "  > "
			}
			fmt.Fprintf(w, "  %s%s\n", mark, formatEvent(evs[i]))
		}
	}
	side("recording", want.Events)
	side("this tree", got.Events)
}

// formatEvent renders an event on one line, eliding zero fields other than
// the player (seat 0 is a real seat) and a step change's step (untap is 0).
func formatEvent(e events.Event) string {
	var b strings.Builder
	fmt.Fprintf(&b, "#%d %s p%d", e.Seq, e.Kind, e.Player)
	if e.Obj != 0 {
		fmt.Fprintf(&b, " obj=%d", e.Obj)
	}
	if e.From != 0 || e.To != 0 {
		fmt.Fprintf(&b, " %s>%s", e.From, e.To)
	}
	if e.Amount != 0 {
		fmt.Fprintf(&b, " amount=%d", e.Amount)
	}
	if e.Step != 0 || e.Kind == events.StepChange { // untap is step 0
		fmt.Fprintf(&b, " step=%s", e.Step)
	}
	if e.Counter != "" {
		fmt.Fprintf(&b, " counter=%q", e.Counter)
	}
	if e.Text != "" {
		fmt.Fprintf(&b, " text=%q", e.Text)
	}
	if len(e.IDs) > 0 {
		fmt.Fprintf(&b, " ids=%v", e.IDs)
	}
	if len(e.Pairs) > 0 {
		fmt.Fprintf(&b, " pairs=%v", e.Pairs)
	}
	if e.Secret {
		b.WriteString(" secret")
	}
	return b.String()
}
