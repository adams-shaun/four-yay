package host

import (
	"fmt"
	"math/rand/v2"
	"os"
	"strconv"
	"strings"
)

// MatchSeed derives match k's engine seed from its table's seed with
// splitmix64's finaliser over tableSeed XOR k·φ, so a table's whole history
// is a pure function of its configuration (spec D14) and consecutive
// matches share nothing but the table.
func MatchSeed(tableSeed uint64, k int) uint64 {
	z := tableSeed ^ (uint64(k) * 0x9E3779B97F4A7C15)
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

// SeededShuffle returns in reordered by a deterministic Fisher-Yates keyed
// on seed, so a caller can assign decks "at random" and have the assignment
// be a pure function of the seed: the same seed reproduces the same order,
// which is what keeps an on-demand game replayable from its configuration.
// It is the one place in host that turns a seed into an ordering; the game
// creator (cmd/gorged) shuffles a format's deck pool with the game's own
// seed, and the resulting order is persisted verbatim in TableConfig.Decks,
// so both the dealt decks and the match are reproducible from the recorded
// config. in is not modified.
func SeededShuffle(seed uint64, in []string) []string {
	out := append([]string(nil), in...)
	rng := rand.New(rand.NewPCG(seed, seed^0x9E3779B97F4A7C15))
	for i := len(out) - 1; i > 0; i-- {
		j := rng.IntN(i + 1)
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// NextGameID returns an unused "g<N>" table id. On-demand table records are
// intentionally removed on restart because their credentials are process
// scoped, but their replay logs stay under Dir/gN. Those directories are part
// of a feedback/replay trail, so their IDs are reserved too: reusing g1 after
// restart would overwrite match 1's events and sidecar. Exported for the game
// creator (cmd/gorged), which allocates ids when a browser asks for a fresh
// game.
func NextGameID(reg *Registry) TableID {
	max := 0
	for _, t := range reg.Tables() {
		if n := gameIDNumber(TableID(t.ID)); n > max {
			max = n
		}
	}
	// A current registry does not contain process-scoped tables dropped by
	// load. Reserve their persisted directory names as a high-water mark.
	// ReadDir failures intentionally leave the in-memory check intact: AddTable
	// will still reject a live collision, and a broken persistence directory
	// must not make ID allocation fail through this convenience helper.
	if reg.opts.Dir != "" {
		if entries, err := os.ReadDir(reg.opts.Dir); err == nil {
			for _, entry := range entries {
				if n := gameIDNumber(TableID(entry.Name())); n > max {
					max = n
				}
			}
		}
	}
	return TableID(fmt.Sprintf("g%d", max+1))
}

func gameIDNumber(id TableID) int {
	s := string(id)
	if len(s) < 2 || s[0] != 'g' {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimPrefix(s, "g"))
	if err != nil || n < 1 {
		return 0
	}
	return n
}
