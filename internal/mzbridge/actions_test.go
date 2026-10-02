package mzbridge

import (
	"os"
	"strings"
	"testing"
)

// A hand-written vocabulary in the upstream format. Its largest action index
// is 643, so the action tail is 380 wide like the real file's and the golden
// file's Java floorMod column applies to it directly.
const testVocab = "# comment\n" +
	"\n" +
	"dim\t1024\n" +
	"A\t0\tPass\n" +
	"A\t1\t{T}: Add {B}.\r\n" +
	"A\t22\tCast Test Spell\n" +
	"A\t30\tfirst line\\nsecond line\n" +
	"A\t31\ttext\twith a tab\n" +
	"A\t40\trepeated\n" +
	"A\t41\trepeated\n" +
	"A\t643\tPlay Test Land\n" +
	"X\t5\tignored kind\n" +
	"A\t7\n" +
	"T\t0\tStop Choosing\n" +
	"T\t1\tPlayerA\n" +
	"T\t2\tPlayerB\n" +
	"T\t9\tTest Bear\n"

func TestParseVocab(t *testing.T) {
	v, err := ParseVocab(strings.NewReader(testVocab))
	if err != nil {
		t.Fatal(err)
	}
	if v.Dim() != 1024 || v.ActionHashStart() != 644 || v.TargetHashStart() != 10 {
		t.Fatalf("dim %d, tails %d %d", v.Dim(), v.ActionHashStart(), v.TargetHashStart())
	}
	for label, want := range map[string]int{
		"Pass": 0, "{T}: Add {B}.": 1, "Cast Test Spell": 22, "first line\nsecond line": 30,
		"text\twith a tab": 31, "repeated": 41, "Play Test Land": 643,
	} {
		if got := v.ActionIndex(label); got != want || !v.KnownAction(label) {
			t.Errorf("ActionIndex(%q) = %d, want %d", label, got, want)
		}
	}
	for name, want := range map[string]int{"Stop Choosing": 0, "PlayerA": 1, "PlayerB": 2, "Test Bear": 9} {
		if got := v.TargetIndex(name); got != want || !v.KnownTarget(name) {
			t.Errorf("TargetIndex(%q) = %d, want %d", name, got, want)
		}
	}
	if v.KnownAction("ignored kind") || v.KnownAction("") || v.KnownAction("Test Bear") || v.KnownTarget("Pass") {
		t.Error("a malformed or other-kind line became an entry")
	}
	if UseIndex(false) != 0 || UseIndex(true) != 1 {
		t.Error("UseIndex")
	}
}

// TestUnknownLabelsMatchJavaTail checks the hashed tail against the JVM:
// the golden file's sixth column is Math.floorMod(name.hashCode(), 380).
func TestUnknownLabelsMatchJavaTail(t *testing.T) {
	v, err := ParseVocab(strings.NewReader(testVocab))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, r := range goldenRows(t, "hash_vectors.tsv") {
		name := unhex(t, r[0])
		if v.KnownAction(name) || v.KnownTarget(name) {
			continue
		}
		n++
		got := v.ActionIndex(name)
		if want := 644 + atoi(t, r[5]); got != want {
			t.Errorf("ActionIndex(%q) = %d, Java %d", name, got, want)
		}
		if got < 644 || got >= 1024 {
			t.Errorf("ActionIndex(%q) = %d outside the tail", name, got)
		}
		hc := int64(atoi(t, r[4])) // Java's own hashCode
		tw := int64(1024 - 10)
		got = v.TargetIndex(name)
		if want := 10 + int((hc%tw+tw)%tw); got != want {
			t.Errorf("TargetIndex(%q) = %d, want %d", name, got, want)
		}
		if got < 10 || got >= 1024 {
			t.Errorf("TargetIndex(%q) = %d outside the tail", name, got)
		}
	}
	if n < 700 {
		t.Fatalf("only %d unknown-label vectors", n)
	}
}

func TestLegacyVocab(t *testing.T) {
	v := LegacyVocab()
	if v.Dim() != 128 || v.ActionIndex("Pass") != 0 || v.ActionIndex("{T}: Add {C}.") != 6 || v.TargetIndex("PlayerB") != 2 {
		t.Fatal("reserved names")
	}
	for _, r := range goldenRows(t, "hash_vectors.tsv") {
		name := unhex(t, r[0])
		if v.KnownAction(name) || v.KnownTarget(name) {
			continue
		}
		if got, want := v.ActionIndex(name), atoi(t, r[6]); got != want {
			t.Errorf("legacy ActionIndex(%q) = %d, Java %d", name, got, want)
		}
		if got, want := v.TargetIndex(name), atoi(t, r[6]); got != want {
			t.Errorf("legacy TargetIndex(%q) = %d, Java %d", name, got, want)
		}
	}
}

func TestParseVocabRejects(t *testing.T) {
	for name, text := range map[string]string{
		"no tail for actions": "dim\t4\nA\t3\tx\n",
		"no tail for targets": "dim\t4\nT\t3\tx\n",
		"bad index":           "dim\t8\nA\tx\ty\n",
		"bad dim":             "dim\tx\n",
		"bare dim":            "dim\n",
	} {
		if _, err := ParseVocab(strings.NewReader(text)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// no dim line: the upstream default of 128 stands
	v, err := ParseVocab(strings.NewReader("A\t0\tPass\n"))
	if err != nil || v.Dim() != 128 || v.ActionHashStart() != 1 || v.TargetHashStart() != 0 {
		t.Fatalf("default dim: %v %+v", err, v)
	}
}

func TestActionTypeNames(t *testing.T) {
	for a, want := range map[ActionType]string{Priority: "PRIORITY", ChooseNum: "CHOOSE_NUM", Blank: "BLANK",
		ChooseTarget: "CHOOSE_TARGET", MakeChoice: "MAKE_CHOICE", ChooseUse: "CHOOSE_USE"} {
		if a.String() != want {
			t.Errorf("%d = %q, want %q", int(a), a.String(), want)
		}
	}
	if Priority != 0 || ChooseTarget != 3 || ChooseUse != 5 {
		t.Error("ordinals the trainer switches on moved")
	}
}

// realVocabPath is draft-zero's FDN vocabulary on this box. The file is not
// in the repository (see actions.go), so this test needs the clone.
const realVocabPath = "/mnt/sata/gorge-training/searchbench/draft-zero/assets/vocab/FDN_SPG.tsv"

// TestRealFDNVocab loads the vocabulary the #2a run uses.
func TestRealFDNVocab(t *testing.T) {
	path := os.Getenv(MZActionVocabEnv)
	if path == "" {
		path = realVocabPath
	}
	if _, err := os.Stat(path); err != nil {
		t.Skipf("no draft-zero vocabulary at %s (set %s): the real-file assertions did not run", path, MZActionVocabEnv)
	}
	v, err := LoadVocab(path)
	if err != nil {
		t.Fatal(err)
	}
	if v.Dim() != 1024 || v.NumActions() != 644 || v.NumTargets() != 530 || v.ActionHashStart() != 644 || v.TargetHashStart() != 530 {
		t.Fatalf("dim %d, %d actions (tail %d), %d targets (tail %d)", v.Dim(), v.NumActions(), v.ActionHashStart(), v.NumTargets(), v.TargetHashStart())
	}
	for label, want := range map[string]int{"Pass": 0, "{T}: Add {B}.": 1, "{T}: Add {G}.": 2, "{T}: Add {R}.": 3,
		"{T}: Add {U}.": 4, "{T}: Add {W}.": 5, "{T}: Add {C}.": 6, "Cast Abrade": 22} {
		if got := v.ActionIndex(label); got != want {
			t.Errorf("ActionIndex(%q) = %d, want %d", label, got, want)
		}
	}
	for name, want := range map[string]int{"Stop Choosing": 0, "PlayerA": 1, "PlayerB": 2, "Abrade": 3, "Zul Ashur, Lich Lord": 529} {
		if got := v.TargetIndex(name); got != want {
			t.Errorf("TargetIndex(%q) = %d, want %d", name, got, want)
		}
	}
	// every slot below the tail is taken exactly once
	seenA, seenT := make([]int, 644), make([]int, 530)
	for _, i := range v.actions {
		seenA[i]++
	}
	for _, i := range v.targets {
		seenT[i]++
	}
	for i, n := range seenA {
		if n != 1 {
			t.Fatalf("action slot %d used %d times", i, n)
		}
	}
	for i, n := range seenT {
		if n != 1 {
			t.Fatalf("target slot %d used %d times", i, n)
		}
	}
	// unknown labels: the tail, by Java's floorMod (golden column, width 380)
	for _, r := range goldenRows(t, "hash_vectors.tsv") {
		name := unhex(t, r[0])
		if v.KnownAction(name) {
			continue
		}
		if got, want := v.ActionIndex(name), 644+atoi(t, r[5]); got != want {
			t.Errorf("ActionIndex(%q) = %d, Java %d", name, got, want)
		}
	}
}
