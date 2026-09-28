package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// smokeDigestGolden is the games.jsonl digest of the sb-uniform,sb-heuristic,bot
// smoke run (1 pair/deck, the Burn and Faeries mirrors, base seed 20260926,
// max turns 40, max intents 8000): every line re-encoded with its wall_ms key
// dropped (the one wall-clock field), the SHA-256 of the canonical lines. It
// pins the -spellbench mode's per-game output: any change to the schedule, the
// seats a policy name builds, the per-seat seed derivation, or the recorded
// extras shows up here. The sb-* and bot seats are registry-built
// (internal/spellbench/registry); the goldens were taken before that refactor,
// so the test also proves the registry resolves the same seats the old
// policies map did.
//
// The golden was re-pinned at the spellbench-prep merge (mrg1): prep's
// builtins.Seat.Refused feature (commit 5f78dfd84, "builtins never lose a
// chosen, payable play" and the sb-actions merge) asks a refused answer again
// before falling back, which changes the per-game fallbacks/first_reject
// extras this digest records. Measured: with sbPlay building seats through
// the old policies map instead of registry.Build the digest is identical, so
// the registry refactor is seat-identical and the move is prep's behaviour
// alone.
//
// Re-pinned again at sb-pursuit: the sb seats no longer pursue a play the
// planner cannot price by naive tapping. rules.PotentialPaymentPlans now
// witnesses mode, {X} and hybrid casts and answers scripted prefixes for
// sources outside its census, the seat decides any other unpriced play with
// the exact search (rules.Engine.PotentialPlayScript), and builtins.Stats,
// which games.jsonl records per seat, gained the script counters.
const smokeDigestGolden = "8936bd7c7999398717193009fc6425213496b6e3d0223e5c348891ba3e260897"

// TestSpellbenchSmokeDigestIsStable plays the smoke run and compares its
// games.jsonl digest against the golden above.
func TestSpellbenchSmokeDigestIsStable(t *testing.T) {
	dir := corpusDirForSpellbench(t)
	o := sbOpts{
		bots: "sb-uniform,sb-heuristic,bot", pairs: 1, decks: "Burn,Faeries",
		out: t.TempDir(), baseSeed: 20260926,
		// catalog is the -spellbench-catalog flag default. The test
		// constructs sbOpts directly (bypassing flag parsing), so it must
		// set the same default the CLI does or spellbenchExit rejects the
		// empty id. Burn/Faeries come from the pauper-kernel catalog.
		catalog: "pauper-kernel",
	}
	if code := spellbenchExit(o, dir, 2, 40, 8000, "", io.Discard, io.Discard); code != 0 {
		t.Fatalf("spellbenchExit = %d, want 0", code)
	}
	got := digestGamesJSON(t, filepath.Join(o.out, "games.jsonl"))
	if got != smokeDigestGolden {
		t.Fatalf("games.jsonl digest %s != golden %s", got, smokeDigestGolden)
	}
}

// corpusDirForSpellbench resolves the .cards/ corpus the way
// testutil.CorpusRegistry does (git toplevel, cards.GitEnv against a
// hook-exported GIT_DIR) and returns the path for spellbenchExit.
func corpusDirForSpellbench(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "-C", ".", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		env := cards.GitEnv()
		cmd := exec.Command("git", "-C", ".", "rev-parse", "--show-toplevel")
		cmd.Env = env
		out, err = cmd.Output()
		if err != nil {
			t.Fatalf("resolving repo root: %v", err)
		}
	}
	dir := filepath.Join(strings.TrimSpace(string(out)), ".cards")
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("no .cards/ corpus present (%v) -- run `make fetch-cards compile-cards`", err)
	}
	return dir
}

// digestGamesJSON hashes a games.jsonl: each line decoded, wall_ms dropped,
// re-encoded (map keys sort), the whole stream SHA-256'd.
func digestGamesJSON(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	h := sha256.New()
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("decoding %s: %v", path, err)
		}
		delete(row, "wall_ms")
		canonical, err := json.Marshal(row)
		if err != nil {
			t.Fatalf("encoding canonical row: %v", err)
		}
		fmt.Fprintf(h, "%s\n", canonical)
	}
	return hex.EncodeToString(h.Sum(nil))
}
