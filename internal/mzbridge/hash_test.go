package mzbridge

import (
	"bufio"
	"encoding/hex"
	"os"
	"strconv"
	"strings"
	"testing"
)

// goldenRows reads a tab-separated golden file, skipping '#' comment lines.
func goldenRows(t *testing.T, name string) [][]string {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rows [][]string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		if line := sc.Text(); line != "" && !strings.HasPrefix(line, "#") {
			rows = append(rows, strings.Split(line, "\t"))
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return rows
}

func unhex(t *testing.T, h string) string {
	t.Helper()
	if h == "-" {
		return ""
	}
	b, err := hex.DecodeString(h)
	if err != nil {
		t.Fatalf("bad hex %q: %v", h, err)
	}
	return string(b)
}

func parseU64(t *testing.T, s string) uint64 {
	t.Helper()
	v, err := strconv.ParseUint(s, 16, 64)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func atoi(t *testing.T, s string) int {
	t.Helper()
	v, err := strconv.Atoi(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// TestHashMatchesJava holds Hash64, IndexFor, JavaHashCode and the two tail
// formulas to vectors produced by the unmodified Features.java on a JVM.
func TestHashMatchesJava(t *testing.T) {
	rows := goldenRows(t, "hash_vectors.tsv")
	if len(rows) < 200 {
		t.Fatalf("only %d vectors; the golden file should hold at least 200", len(rows))
	}
	lengths := map[int]bool{}
	nonASCII := 0
	for _, r := range rows {
		name, seed := unhex(t, r[0]), parseU64(t, r[1])
		lengths[len(name)] = true
		for i := 0; i < len(name); i++ {
			if name[i] >= 0x80 {
				nonASCII++
				break
			}
		}
		h := Hash64(name, seed)
		if want := parseU64(t, r[2]); h != want {
			t.Errorf("Hash64(%q, %#x) = %#x, Java %#x", name, seed, h, want)
		}
		if got, want := IndexFor(h), int32(atoi(t, r[3])); got != want {
			t.Errorf("IndexFor(%#x) = %d, Java %d", h, got, want)
		}
		if got := FeatureID(name, seed); got != int32(atoi(t, r[3])) {
			t.Errorf("FeatureID(%q) = %d", name, got)
		}
		hc := JavaHashCode(name)
		if want := int32(atoi(t, r[4])); hc != want {
			t.Errorf("JavaHashCode(%q) = %d, Java %d", name, hc, want)
		}
		if got, want := floorMod(hc, 380), atoi(t, r[5]); got != want {
			t.Errorf("floorMod(hashCode(%q), 380) = %d, Java %d", name, got, want)
		}
		if got, want := legacyHashedIndex(name), atoi(t, r[6]); got != want {
			t.Errorf("legacyHashedIndex(%q) = %d, Java %d", name, got, want)
		}
	}
	for n := 0; n <= 17; n++ {
		if !lengths[n] {
			t.Errorf("no vector with a %d-byte name", n)
		}
	}
	if nonASCII < 20*7 {
		t.Errorf("only %d non-ASCII vectors", nonASCII)
	}
}

// TestIndexForMatchesJava covers the signed edge cases directly, among them
// Long.MIN_VALUE, the one input whose index is negative.
func TestIndexForMatchesJava(t *testing.T) {
	rows := goldenRows(t, "index_vectors.tsv")
	seenMin := false
	for _, r := range rows {
		h := parseU64(t, r[0])
		seenMin = seenMin || h == 1<<63
		if got, want := IndexFor(h), int32(atoi(t, r[1])); got != want {
			t.Errorf("IndexFor(%#x) = %d, Java %d", h, got, want)
		}
	}
	if !seenMin {
		t.Error("no Long.MIN_VALUE vector")
	}
	if IndexFor(1<<63) != -2 {
		t.Errorf("IndexFor(Long.MIN_VALUE) = %d, want -2", IndexFor(1<<63))
	}
}

func TestJavaHashCodeIntMin(t *testing.T) {
	// the well-known string whose hashCode is Integer.MIN_VALUE, where
	// Math.abs is a no-op and the legacy tail formula goes negative
	if got := JavaHashCode("polygenelubricants"); got != -1<<31 {
		t.Fatalf("hashCode = %d", got)
	}
	if got := legacyHashedIndex("polygenelubricants"); got != -7 {
		t.Fatalf("legacy index = %d, want Java's -7", got)
	}
}
