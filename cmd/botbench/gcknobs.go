package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
)

// The GC knobs set the collector's pacing for a measurement run without an
// environment variable: -gc-percent is debug.SetGCPercent (a negative value
// turns the proportional trigger off, GOGC=off) and -mem-limit is
// debug.SetMemoryLimit (the soft limit that then paces collection,
// GOMEMLIMIT). Both default to "leave the runtime's setting alone", so a run
// that names neither behaves exactly as before. Neither can change a game:
// collection timing is invisible to the engine.
var (
	gcPercent = flag.Int("gc-percent", math.MinInt32, "debug.SetGCPercent at startup (negative = GC off, as GOGC=off; default: unchanged)")
	memLimit  = flag.String("mem-limit", "", "debug.SetMemoryLimit at startup, bytes or with a KiB/MiB/GiB suffix (e.g. 1500MiB; default: unchanged)")
)

func applyGCFlags() {
	if *gcPercent != math.MinInt32 {
		debug.SetGCPercent(*gcPercent)
	}
	if *memLimit != "" {
		n, err := parseByteSize(*memLimit)
		if err != nil {
			fmt.Fprintln(os.Stderr, "botbench: -mem-limit:", err)
			os.Exit(2)
		}
		debug.SetMemoryLimit(n)
	}
}

// parseByteSize reads a byte count with an optional binary suffix
// (B, KiB, MiB, GiB, TiB; the GOMEMLIMIT spellings).
func parseByteSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	mult := int64(1)
	for _, u := range []struct {
		suffix string
		mult   int64
	}{{"TiB", 1 << 40}, {"GiB", 1 << 30}, {"MiB", 1 << 20}, {"KiB", 1 << 10}, {"B", 1}} {
		if strings.HasSuffix(s, u.suffix) {
			s, mult = strings.TrimSuffix(s, u.suffix), u.mult
			break
		}
	}
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || n < 0 || n > math.MaxInt64/mult {
		return 0, fmt.Errorf("bad size %q", s)
	}
	return n * mult, nil
}
