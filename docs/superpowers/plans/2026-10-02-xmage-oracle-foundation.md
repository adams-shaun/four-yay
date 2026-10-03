# XMage Compliance Oracle — Foundation (X0–X3) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Lay the foundation the compliance oracle stands on: pinned XMage set manifests, an out-of-tree XMage build with measured costs, gorge's oracle runner callable outside tests and emitting engine-neutral snapshots, and a corpus that contains Reality Fracture.

**Architecture:** A new stdlib-only `compliance` package parses XMage (MIT) set classes into committed per-set manifests. A committed bootstrap script builds XMage out of tree under `/mnt/sata/gorge-training/xmageoracle/` and records its cost. The oracle runner moves from `rules/oracle_audit_test.go` into non-test files and gains a canonical snapshot (spec §7) and a JSON entry point. `FORGE_REF` moves to an upstream commit that carries FRA.

**Tech Stack:** Go (stdlib only), bash, Maven 3.9 + OpenJDK 21 (out of tree only).

**Spec:** `docs/superpowers/specs/2026-10-02-xmage-compliance-oracle-design.md`. This plan covers spec §9 tickets X0–X3. X4–X10 get a second plan written after Task 3 reports its measurements (spec §10's first revisit trigger depends on them).

## Global Constraints

- No cgo and no third-party Go dependencies in the card pipeline and rules core (AGENTS.md). The `compliance` package and `cmd/compliance` are stdlib-only.
- Never commit Forge card scripts or any text derived from them (GPL-3.0). XMage-derived artefacts are MIT and are credited in `NOTICE`.
- All state mutation goes through `events.Apply`. The snapshot code is read-only: it must not emit events or write any `state.Game` field.
- No nondeterminism: no wall clock, no ambient randomness, no `map` range whose order reaches output. Sort before emitting.
- One heavy job at a time on this box, inside `systemd-run --user --scope -p MemoryMax=7G`. Budget: 8 vCPU, 8 GB RAM total (spec §4).
- XMage pin: `XMAGE_REF ?= 6b602a1c85e8ed738a4b40b1ef44c54845b57f68` (XMage `master`, measured 2026-10-02).
- FRA Forge pin: `FORGE_REF ?= fb4d8091126051b0c579db5f3bfdcb7e03aae63d` (Card-Forge `master`, 2026-10-02T13:14:12Z; its `forge-gui/res/cardsfolder` carries FRA).
- Out-of-tree root: `/mnt/sata/gorge-training/xmageoracle/`. Never `/tmp` (RAM-backed on this box).
- Every change is made in its own worktree created by `scripts/agent-worktree.sh <id>`; never branch, reset or stash in the main checkout. Stage explicit paths; never `git add -A`.
- `Makefile` and `rules/oracle_audit_test.go` are hot files: keep the edits to the lines named here and land Tasks 1, 2, 3 and 6 in sequence for their `Makefile` hunks.
- No `Co-Authored-By` or other attribution trailers in commits (repo hooks reject them).

## Review Focus

1. **A set class whose `SetCardInfo` uses a variable or constant instead of a literal collector number.** Expected: the parser refuses the whole file with a count mismatch, never silently drops the card. Pinned by `TestParseSetClassRefusesUnparsedEntry` (Task 1).
2. **A card name with a Java escape (`\"Ach! Hans, Run!\"`, `é`).** Expected: the manifest holds the unescaped name, which is what the corpus uses. Pinned by `TestParseSetClassUnescapesNames` (Task 1).
3. **Taking a snapshot perturbs the game.** Expected: a scenario run with snapshots replays byte-identically from its log. Pinned by `TestOracleSnapshotDoesNotPerturbReplay` (Task 5).
4. **Two copies of one card under one controller.** Expected: snapshot refs `p1:Grizzly Bears` and `p1:Grizzly Bears#2` in arrival order, the same rule scenario refs use. Pinned by `TestOracleSnapshotRefsMatchScenarioRefs` (Task 5).
5. **A stale XMage checkout used for manifests.** Expected: `cmd/compliance manifest` refuses when the checkout's HEAD is not the `-ref` it was given, so a committed manifest never claims a pin it was not generated from. Pinned by `TestManifestCommandRefusesRefMismatch` (Task 2).

---

## File structure

| Path | Task | Responsibility |
|---|---|---|
| `compliance/doc.go` | 1 | Package doc: what compliance data is, the spec link |
| `compliance/manifest.go` | 1 | `Manifest`, `ManifestCard`, `ParseSetClass`, `MarshalLines` |
| `compliance/manifest_test.go` | 1 | Parser tests on synthetic set-class text |
| `compliance/resolve.go` | 1 | `CorpusName`: XMage name → corpus name |
| `compliance/resolve_test.go` | 1 | Name-resolution tests |
| `Makefile` | 1, 2, 3, 6 | `XMAGE_REF`/`XMAGE_ORACLE_DIR` (1), `compliance-manifests` (2), `xmage-oracle-setup` (3), `FORGE_REF` (6) |
| `cmd/compliance/main.go` | 2 | `manifest` and `resolve` subcommands |
| `cmd/compliance/main_test.go` | 2 | Ref-mismatch refusal; round-trip of written manifests |
| `compliance/manifests/<CODE>.json` | 2 | Generated, one per XMage set class (589 at the pin) |
| `NOTICE` | 2 | MIT attribution for XMage-derived files |
| `scripts/xmage-oracle-setup.sh` | 3 | Reusable out-of-tree bootstrap: Maven, clone, capped build, smoke test, measurement |
| `rules/oracle_run.go` | 4 | The oracle runner, moved verbatim out of the test file |
| `rules/oracle_audit_test.go` | 4 | Keeps only the ratchet, the loaders and the two tests |
| `rules/oracle_snapshot.go` | 5 | `OracleSnapshot` and friends; `(*oracleRun).snapshot` |
| `rules/oracle_export.go` | 5 | `OracleResult`, `RunOracleScenarioJSON` |
| `rules/oracle_snapshot_test.go` | 5 | Snapshot and entry-point tests |
| `rules/heads_test.go`, `rules/acceptance_test.go`, `rules/paramcensus_test.go`, `cmd/repro/testdata/feedback/20260914T120000Z-fb01/` | 6 | Re-pinned by the corpus bump |

---

### Task 1: `compliance` package — set-class parser and name resolution (X2a)

**Files:**
- Create: `compliance/doc.go`, `compliance/manifest.go`, `compliance/manifest_test.go`, `compliance/resolve.go`, `compliance/resolve_test.go`
- Modify: `Makefile` (add two variables directly under the `FORGE_REF` line, currently line 43)

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type Manifest struct { Code, Name, Released, SetType, XMageRef string; Cards []ManifestCard }` (JSON keys `code`, `name`, `released`, `set_type`, `xmage_ref`, `cards`)
  - `type ManifestCard struct { Name string; Numbers []string; Rarity string }` (JSON keys `name`, `numbers`, `rarity`)
  - `func ParseSetClass(src []byte, xmageRef string) (Manifest, error)`
  - `func (m Manifest) MarshalLines() []byte`: deterministic JSON, one card per line
  - `func CorpusName(has func(string) bool, xmageName string) (string, bool)`
  - Makefile: `XMAGE_REF ?= 6b602a1c85e8ed738a4b40b1ef44c54845b57f68`, `XMAGE_ORACLE_DIR ?= /mnt/sata/gorge-training/xmageoracle`

- [ ] **Step 1: Create the worktree**

```bash
cd /home/sadams/projects/gorge && scripts/agent-worktree.sh xo-manifest-parser
cd .worktrees/xo-manifest-parser
```

- [ ] **Step 2: Write the failing parser tests**

`compliance/manifest_test.go`:

```go
package compliance

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

const fixtureSet = `package mage.sets;

public final class Fixture extends ExpansionSet {
    private Fixture() {
        super("Fixture \"Set\"", "FXT", ExpansionSet.buildDate(2026, 10, 2), SetType.EXPANSION);
        this.blockName = "Fixture";
        cards.add(new SetCardInfo("Zap", 2, Rarity.COMMON, mage.cards.z.Zap.class));
        cards.add(new SetCardInfo("Aerid Konstrari", 121, Rarity.MYTHIC, mage.cards.a.AeridKonstrari.class, NON_FULL_USE_VARIOUS));
        cards.add(new SetCardInfo("Aerid Konstrari", "347a", Rarity.MYTHIC, mage.cards.a.AeridKonstrari.class, NON_FULL_USE_VARIOUS));
        cards.add(new SetCardInfo("Aerid Konstrari", 121, Rarity.MYTHIC, mage.cards.a.AeridKonstrari.class, NON_FULL_USE_VARIOUS));
        cards.add(new SetCardInfo("Fire // Ice", 9, Rarity.UNCOMMON, mage.cards.f.FireIce.class));
    }
}
`

func TestParseSetClass(t *testing.T) {
	m, err := ParseSetClass([]byte(fixtureSet), "abc123")
	if err != nil {
		t.Fatal(err)
	}
	want := Manifest{
		Code: "FXT", Name: `Fixture "Set"`, Released: "2026-10-02", SetType: "EXPANSION", XMageRef: "abc123",
		Cards: []ManifestCard{
			{Name: "Aerid Konstrari", Numbers: []string{"121", "347a"}, Rarity: "MYTHIC"},
			{Name: "Fire // Ice", Numbers: []string{"9"}, Rarity: "UNCOMMON"},
			{Name: "Zap", Numbers: []string{"2"}, Rarity: "COMMON"},
		},
	}
	if !reflect.DeepEqual(m, want) {
		t.Fatalf("got  %+v\nwant %+v", m, want)
	}
}

func TestParseSetClassOldHeader(t *testing.T) {
	src := `super("Limited Edition Alpha", "LEA", buildDate(1993, 8, 5), SetType.CORE);
        cards.add(new SetCardInfo("Black Lotus", 232, Rarity.RARE, mage.cards.b.BlackLotus.class));`
	m, err := ParseSetClass([]byte(src), "r")
	if err != nil {
		t.Fatal(err)
	}
	if m.Code != "LEA" || m.Released != "1993-08-05" || m.SetType != "CORE" || len(m.Cards) != 1 {
		t.Fatalf("got %+v", m)
	}
}

func TestParseSetClassUnescapesNames(t *testing.T) {
	src := `super("Unhinged", "UNH", ExpansionSet.buildDate(2004, 11, 19), SetType.JOKE_SET);
        cards.add(new SetCardInfo("\"Ach! Hans, Run!\"", 116, Rarity.RARE, mage.cards.a.AchHansRun.class));
        cards.add(new SetCardInfo("Déjà Vu", 26, Rarity.COMMON, mage.cards.d.DejaVu.class));`
	m, err := ParseSetClass([]byte(src), "r")
	if err != nil {
		t.Fatal(err)
	}
	got := []string{m.Cards[0].Name, m.Cards[1].Name}
	want := []string{`"Ach! Hans, Run!"`, "Déjà Vu"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestParseSetClassRefusesUnparsedEntry(t *testing.T) {
	src := `super("Fixture", "FXT", ExpansionSet.buildDate(2026, 1, 1), SetType.EXPANSION);
        cards.add(new SetCardInfo("Zap", 2, Rarity.COMMON, mage.cards.z.Zap.class));
        cards.add(new SetCardInfo("Bolt", NUMBER, Rarity.COMMON, mage.cards.b.Bolt.class));`
	_, err := ParseSetClass([]byte(src), "r")
	if err == nil || !strings.Contains(err.Error(), "2 SetCardInfo entries but 1 parsed") {
		t.Fatalf("err = %v, want a count mismatch", err)
	}
}

func TestParseSetClassRefusesMissingHeader(t *testing.T) {
	_, err := ParseSetClass([]byte(`public final class X {}`), "r")
	if err == nil || !strings.Contains(err.Error(), "no ExpansionSet super(...) header") {
		t.Fatalf("err = %v", err)
	}
}

func TestMarshalLinesRoundTripsAndIsOneCardPerLine(t *testing.T) {
	m, err := ParseSetClass([]byte(fixtureSet), "abc123")
	if err != nil {
		t.Fatal(err)
	}
	out := m.MarshalLines()
	var back Manifest
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !reflect.DeepEqual(back, m) {
		t.Fatalf("round trip: got %+v want %+v", back, m)
	}
	if n := strings.Count(string(out), `{"name":`); n != len(m.Cards) {
		t.Fatalf("%d card objects, want %d", n, len(m.Cards))
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Count(line, `{"name":`) > 1 {
			t.Fatalf("two cards on one line: %s", line)
		}
	}
	if string(m.MarshalLines()) != string(out) {
		t.Fatal("MarshalLines is not deterministic")
	}
}
```

- [ ] **Step 3: Run them to verify they fail**

Run: `go test ./compliance/ -count=1`
Expected: FAIL to compile, `undefined: ParseSetClass`, `undefined: Manifest`.

- [ ] **Step 4: Implement the parser**

`compliance/doc.go`:

```go
// Package compliance holds gorge's declared-set compliance data: per-set
// card manifests derived from XMage's set classes (MIT, credited in NOTICE),
// the sets gorge declares compliant, and the verdict rows the CI gate reads.
// See docs/superpowers/specs/2026-10-02-xmage-compliance-oracle-design.md.
//
// Nothing here reads or embeds a Forge card script.
package compliance
```

`compliance/manifest.go`:

```go
package compliance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
)

// Manifest is one set's card list, taken from an XMage set class at a pinned
// XMage commit.
type Manifest struct {
	Code     string         `json:"code"`
	Name     string         `json:"name"`
	Released string         `json:"released"` // YYYY-MM-DD
	SetType  string         `json:"set_type"` // XMage SetType, e.g. EXPANSION
	XMageRef string         `json:"xmage_ref"`
	Cards    []ManifestCard `json:"cards"`
}

// ManifestCard is one distinct card name in a set, with every collector
// number it is printed under.
type ManifestCard struct {
	Name    string   `json:"name"`
	Numbers []string `json:"numbers"`
	Rarity  string   `json:"rarity"`
}

// The two shapes every XMage set class uses, measured over all 589 classes at
// XMAGE_REF 6b602a1c (2026-10-02): 93,653 SetCardInfo entries, every one a
// literal name followed by an int or quoted collector number.
var (
	setHeaderRe = regexp.MustCompile(`super\(\s*"((?:[^"\\]|\\.)*)"\s*,\s*"([^"]+)"\s*,\s*(?:ExpansionSet\.)?buildDate\(\s*(\d+)\s*,\s*(\d+)\s*,\s*(\d+)\s*\)\s*,\s*SetType\.([A-Z_]+)`)
	setCardRe   = regexp.MustCompile(`new SetCardInfo\(\s*"((?:[^"\\]|\\.)*)"\s*,\s*("(?:[^"\\]|\\.)*"|\d+)\s*,\s*Rarity\.([A-Z_]+)`)
	anyCardRe   = regexp.MustCompile(`new SetCardInfo\(`)
)

// javaString decodes the body of a Java string literal. Java's escapes in
// set classes (\" \\ \uXXXX) are a subset of Go's.
func javaString(body string) (string, error) {
	return strconv.Unquote(`"` + body + `"`)
}

// ParseSetClass reads one XMage set class. It refuses a file in which any
// SetCardInfo entry does not match the literal shape, so a card is never
// silently dropped from a manifest.
func ParseSetClass(src []byte, xmageRef string) (Manifest, error) {
	h := setHeaderRe.FindSubmatch(src)
	if h == nil {
		return Manifest{}, fmt.Errorf("compliance: no ExpansionSet super(...) header")
	}
	name, err := javaString(string(h[1]))
	if err != nil {
		return Manifest{}, fmt.Errorf("compliance: set name %q: %v", h[1], err)
	}
	y, _ := strconv.Atoi(string(h[3]))
	mo, _ := strconv.Atoi(string(h[4]))
	d, _ := strconv.Atoi(string(h[5]))
	m := Manifest{
		Code: string(h[2]), Name: name, Released: fmt.Sprintf("%04d-%02d-%02d", y, mo, d),
		SetType: string(h[6]), XMageRef: xmageRef,
	}
	all := anyCardRe.FindAllIndex(src, -1)
	rows := setCardRe.FindAllSubmatch(src, -1)
	if len(all) != len(rows) {
		return Manifest{}, fmt.Errorf("compliance: %s: %d SetCardInfo entries but %d parsed", m.Code, len(all), len(rows))
	}
	byName := map[string]*ManifestCard{}
	for _, r := range rows {
		cn, err := javaString(string(r[1]))
		if err != nil {
			return Manifest{}, fmt.Errorf("compliance: %s: card name %q: %v", m.Code, r[1], err)
		}
		num := string(r[2])
		if num[0] == '"' {
			if num, err = strconv.Unquote(num); err != nil {
				return Manifest{}, fmt.Errorf("compliance: %s: %s: collector number %s: %v", m.Code, cn, r[2], err)
			}
		}
		c := byName[cn]
		if c == nil {
			c = &ManifestCard{Name: cn, Rarity: string(r[3])}
			byName[cn] = c
		}
		c.Numbers = append(c.Numbers, num)
	}
	names := make([]string, 0, len(byName))
	for n := range byName {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		c := byName[n]
		sort.Strings(c.Numbers)
		c.Numbers = dedupSorted(c.Numbers)
		m.Cards = append(m.Cards, *c)
	}
	return m, nil
}

func dedupSorted(s []string) []string {
	out := s[:0]
	for i, v := range s {
		if i == 0 || v != s[i-1] {
			out = append(out, v)
		}
	}
	return out
}

// MarshalLines renders the manifest as JSON with one card per line, so a
// manifest diff after an XMAGE_REF bump reads card by card.
func (m Manifest) MarshalLines() []byte {
	var b bytes.Buffer
	head := struct {
		Code     string `json:"code"`
		Name     string `json:"name"`
		Released string `json:"released"`
		SetType  string `json:"set_type"`
		XMageRef string `json:"xmage_ref"`
	}{m.Code, m.Name, m.Released, m.SetType, m.XMageRef}
	hj, _ := json.Marshal(head)
	b.Write(hj[:len(hj)-1]) // drop the closing brace
	b.WriteString(",\n\"cards\": [\n")
	for i, c := range m.Cards {
		cj, _ := json.Marshal(c)
		b.Write(cj)
		if i < len(m.Cards)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	b.WriteString("]}\n")
	return b.Bytes()
}
```

- [ ] **Step 5: Run the parser tests to verify they pass**

Run: `go test ./compliance/ -count=1 -run 'TestParseSetClass|TestMarshalLines' -v`
Expected: PASS for all six tests.

- [ ] **Step 6: Write the failing resolution test**

`compliance/resolve_test.go`:

```go
package compliance

import "testing"

func TestCorpusName(t *testing.T) {
	corpus := map[string]bool{"Lightning Bolt": true, "Fire // Ice": true, "Delver of Secrets": true}
	has := func(n string) bool { return corpus[n] }
	for _, tc := range []struct {
		in, want string
		ok       bool
	}{
		{"Lightning Bolt", "Lightning Bolt", true},
		{"Fire // Ice", "Fire // Ice", true},
		{"Delver of Secrets // Insectile Aberration", "Delver of Secrets", true},
		{"Academic Ascent", "", false},
	} {
		got, ok := CorpusName(has, tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("CorpusName(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}
```

Run: `go test ./compliance/ -count=1 -run TestCorpusName`
Expected: FAIL, `undefined: CorpusName`.

- [ ] **Step 7: Implement `CorpusName`**

`compliance/resolve.go`:

```go
package compliance

import "strings"

// CorpusName maps an XMage card name onto the corpus's name for the same
// card: the exact name first, then the front face of an "A // B" name (the
// corpus names a split card by both halves but a double-faced card by its
// front face; XMage writes both as "A // B").
func CorpusName(has func(string) bool, xmageName string) (string, bool) {
	if has(xmageName) {
		return xmageName, true
	}
	if front, _, ok := strings.Cut(xmageName, " // "); ok && has(front) {
		return front, true
	}
	return "", false
}
```

Run: `go test ./compliance/ -count=1 -v`
Expected: PASS, seven tests.

- [ ] **Step 8: Add the XMage pin to the Makefile**

Insert directly after the line `FORGE_REF  ?= 95f04e8a04c8925fa97cb226fc3341cabcc90a53`:

```make
# The XMage commit the compliance oracle (manifests, out-of-tree driver)
# is pinned to. XMage is MIT; nothing from it is a build dependency.
# docs/superpowers/specs/2026-10-02-xmage-compliance-oracle-design.md
XMAGE_REF  ?= 6b602a1c85e8ed738a4b40b1ef44c54845b57f68
XMAGE_ORACLE_DIR ?= /mnt/sata/gorge-training/xmageoracle
```

Run: `make -s -n help >/dev/null && go vet ./compliance/`
Expected: no output, exit 0.

- [ ] **Step 9: Commit**

```bash
git add compliance/doc.go compliance/manifest.go compliance/manifest_test.go compliance/resolve.go compliance/resolve_test.go Makefile
git commit -m "compliance: parse XMage set classes into per-set manifests; pin XMAGE_REF"
```

---

### Task 2: `cmd/compliance` and the generated manifests (X2b)

Depends on Task 1 (merged).

**Files:**
- Create: `cmd/compliance/main.go`, `cmd/compliance/main_test.go`, `NOTICE`, `compliance/manifests/*.json` (generated)
- Modify: `Makefile` (add the `compliance-manifests` target and its help line)

**Interfaces:**
- Consumes: `compliance.ParseSetClass`, `compliance.Manifest.MarshalLines`, `compliance.CorpusName`, `cards.OpenCorpus(dir string) (*cards.Registry, error)`, `(*cards.Registry).Lookup(name string) (*cards.Card, bool)`.
- Produces: `compliance/manifests/<CODE>.json` for every set class at `XMAGE_REF`; `make compliance-manifests`; `go run ./cmd/compliance resolve <manifest>...`.

- [ ] **Step 1: Create the worktree and fetch the set classes**

```bash
cd /home/sadams/projects/gorge && scripts/agent-worktree.sh xo-manifests
cd .worktrees/xo-manifests
D=/mnt/sata/gorge-training/xmageoracle/sets-only
git clone -q --filter=blob:none --sparse https://github.com/magefree/mage.git "$D"
git -C "$D" sparse-checkout set --no-cone 'Mage.Sets/src/mage/sets/*.java'
git -C "$D" checkout -q 6b602a1c85e8ed738a4b40b1ef44c54845b57f68
ls "$D/Mage.Sets/src/mage/sets" | wc -l
```

Expected: `589`.

- [ ] **Step 2: Write the failing command tests**

`cmd/compliance/main_test.go`:

```go
package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance"
)

// fakeXMage builds a git repo shaped like an XMage checkout with one set
// class, and returns its root and HEAD.
func fakeXMage(t *testing.T) (root, head string) {
	t.Helper()
	root = t.TempDir()
	dir := filepath.Join(root, "Mage.Sets", "src", "mage", "sets")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `super("Fixture", "FXT", ExpansionSet.buildDate(2026, 10, 2), SetType.EXPANSION);
        cards.add(new SetCardInfo("Zap", 2, Rarity.COMMON, mage.cards.z.Zap.class));`
	if err := os.WriteFile(filepath.Join(dir, "Fixture.java"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q"}, {"add", "."},
		{"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "fixture"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = cards.GitEnv() // strips inherited GIT_* (a hook's GIT_DIR would redirect this)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	out, err := gitHead(root)
	if err != nil {
		t.Fatal(err)
	}
	return root, out
}

func TestManifestCommandWritesOneFilePerSet(t *testing.T) {
	root, head := fakeXMage(t)
	out := t.TempDir()
	if err := runManifest(root, head, out); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(out, "FXT.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m compliance.Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m.XMageRef != head || len(m.Cards) != 1 || m.Cards[0].Name != "Zap" {
		t.Fatalf("got %+v", m)
	}
}

func TestManifestCommandRefusesRefMismatch(t *testing.T) {
	root, _ := fakeXMage(t)
	err := runManifest(root, "0000000000000000000000000000000000000000", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "is at") {
		t.Fatalf("err = %v, want a ref mismatch refusal", err)
	}
}
```

Run: `go test ./cmd/compliance/ -count=1`
Expected: FAIL to compile, `undefined: runManifest`, `undefined: gitHead`.

- [ ] **Step 3: Implement the command**

`cmd/compliance/main.go`:

```go
// Command compliance maintains gorge's declared-set compliance data
// (docs/superpowers/specs/2026-10-02-xmage-compliance-oracle-design.md).
//
//	compliance manifest -xmage <XMage checkout> -ref <sha> [-out compliance/manifests]
//	compliance resolve  [-cards .cards] [-v] <manifest.json>...
//
// manifest writes one <CODE>.json per XMage set class, refusing a checkout
// whose HEAD is not -ref. resolve reports, per manifest, how many of its
// cards the corpus has, so a FORGE_REF bump's coverage of a set is one line.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: compliance manifest|resolve ...")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "manifest":
		fs := flag.NewFlagSet("manifest", flag.ExitOnError)
		xmage := fs.String("xmage", "", "XMage checkout (needs Mage.Sets/src/mage/sets)")
		ref := fs.String("ref", "", "XMAGE_REF the checkout must be at")
		out := fs.String("out", "compliance/manifests", "output directory")
		fs.Parse(os.Args[2:])
		err = runManifest(*xmage, *ref, *out)
	case "resolve":
		fs := flag.NewFlagSet("resolve", flag.ExitOnError)
		dir := fs.String("cards", ".cards", "corpus directory")
		verbose := fs.Bool("v", false, "list the missing names")
		fs.Parse(os.Args[2:])
		err = runResolve(*dir, *verbose, fs.Args())
	default:
		err = fmt.Errorf("unknown subcommand %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "compliance:", err)
		os.Exit(1)
	}
}

func gitHead(dir string) (string, error) {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "HEAD")
	cmd.Env = cards.GitEnv()
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse in %s: %v", dir, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func runManifest(xmage, ref, out string) error {
	if xmage == "" || ref == "" {
		return fmt.Errorf("manifest needs -xmage and -ref")
	}
	head, err := gitHead(xmage)
	if err != nil {
		return err
	}
	if head != ref {
		return fmt.Errorf("%s is at %s, not -ref %s: check out the pin first", xmage, head, ref)
	}
	paths, err := filepath.Glob(filepath.Join(xmage, "Mage.Sets", "src", "mage", "sets", "*.java"))
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("no set classes under %s/Mage.Sets/src/mage/sets", xmage)
	}
	sort.Strings(paths)
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	seen := map[string]string{}
	for _, p := range paths {
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		m, err := compliance.ParseSetClass(src, ref)
		if err != nil {
			return fmt.Errorf("%s: %v", filepath.Base(p), err)
		}
		if prev, dup := seen[m.Code]; dup {
			return fmt.Errorf("set code %s in both %s and %s", m.Code, prev, filepath.Base(p))
		}
		seen[m.Code] = filepath.Base(p)
		if err := os.WriteFile(filepath.Join(out, m.Code+".json"), m.MarshalLines(), 0o644); err != nil {
			return err
		}
	}
	fmt.Printf("wrote %d manifests to %s\n", len(paths), out)
	return nil
}

func runResolve(dir string, verbose bool, manifests []string) error {
	reg, err := cards.OpenCorpus(dir)
	if err != nil {
		return err
	}
	has := func(n string) bool { _, ok := reg.Lookup(n); return ok }
	for _, p := range manifests {
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		var m compliance.Manifest
		if err := json.Unmarshal(b, &m); err != nil {
			return fmt.Errorf("%s: %v", p, err)
		}
		var missing []string
		for _, c := range m.Cards {
			if _, ok := compliance.CorpusName(has, c.Name); !ok {
				missing = append(missing, c.Name)
			}
		}
		fmt.Printf("%-6s cards %4d  in-corpus %4d  missing %4d\n", m.Code, len(m.Cards), len(m.Cards)-len(missing), len(missing))
		if verbose {
			for _, n := range missing {
				fmt.Printf("    %s\n", n)
			}
		}
	}
	return nil
}
```

Run: `go test ./cmd/compliance/ -count=1 -v`
Expected: PASS, two tests.

- [ ] **Step 4: Add the Make target**

Add to the `help` echo block, after the `make report` line:

```make
	@echo "  make compliance-manifests — regenerate compliance/manifests from XMage set classes at XMAGE_REF"
```

Add after the `compile-cards` target:

```make
.PHONY: compliance-manifests
compliance-manifests:
	go run ./cmd/compliance manifest -xmage $(XMAGE_ORACLE_DIR)/sets-only -ref $(XMAGE_REF) -out compliance/manifests
```

- [ ] **Step 5: Generate the manifests and check them**

```bash
make compliance-manifests
ls compliance/manifests | wc -l
du -sh compliance/manifests
go run ./cmd/compliance resolve compliance/manifests/FRA.json compliance/manifests/FDN.json
```

Expected: `wrote 589 manifests`, `589`, a total size of a few MB (above 10 MB: stop and report instead of committing). The FRA line reads `cards  269  in-corpus   30  missing  239` at the current `FORGE_REF`. That is expected until Task 6 lands.

- [ ] **Step 6: Add NOTICE**

`NOTICE`:

```
gorge
Copyright the gorge authors. Licensed under the Apache License, Version 2.0.

compliance/manifests/*.json are derived from the set classes of XMage
(https://github.com/magefree/mage), Mage.Sets/src/mage/sets, at the commit
named in each file's "xmage_ref". XMage is distributed under the MIT License:

  MIT License

  Copyright (c) 2010 betasteward@gmail.com

  Permission is hereby granted, free of charge, to any person obtaining a copy
  of this software and associated documentation files (the "Software"), to deal
  in the Software without restriction, including without limitation the rights
  to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
  copies of the Software, and to permit persons to whom the Software is
  furnished to do so, subject to the following conditions:

  The above copyright notice and this permission notice shall be included in
  all copies or substantial portions of the Software.

  THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
  IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
  FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
  AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
  LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
  OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
  SOFTWARE.
```

Before committing, compare the copyright line with `/mnt/sata/gorge-training/xmageoracle/sets-only/LICENSE.txt` and copy it exactly if it differs.

- [ ] **Step 7: Check the licence boundary test still passes**

Run: `go test ./cards/ -run Boundary -count=1`
Expected: PASS. The manifests contain names and numbers only, no script text.

- [ ] **Step 8: Commit**

```bash
git add cmd/compliance/main.go cmd/compliance/main_test.go NOTICE Makefile compliance/manifests
git commit -m "compliance: generate the 589 XMage set manifests at XMAGE_REF; cmd/compliance manifest/resolve"
```

---

### Task 3: Out-of-tree XMage build and measurement (X1)

Depends on Task 1 (merged; reads `XMAGE_REF` and `XMAGE_ORACLE_DIR` from the Makefile). Heavy: run it alone, never alongside another heavy job, preferably 22:00–07:00. Before starting, `free -g` must show at least 8 GB available. If it doesn't, wait.

**Files:**
- Create: `scripts/xmage-oracle-setup.sh`
- Modify: `Makefile` (add the `xmage-oracle-setup` target and help line)
- Out of tree (not committed): `$XMAGE_ORACLE_DIR/{maven,mage,MEASURE.md}`

**Interfaces:**
- Consumes: `XMAGE_REF`, `XMAGE_ORACLE_DIR` (Task 1).
- Produces: a built XMage at `$XMAGE_ORACLE_DIR/mage` with `mage-tests` test classes compiled and the H2 card DB created; `$XMAGE_ORACLE_DIR/maven/bin/mvn`; `$XMAGE_ORACLE_DIR/MEASURE.md` with the numbers the X4–X10 plan needs.

- [ ] **Step 1: Create the worktree**

```bash
cd /home/sadams/projects/gorge && scripts/agent-worktree.sh xo-xmage-build
cd .worktrees/xo-xmage-build
```

- [ ] **Step 2: Write the bootstrap script**

`scripts/xmage-oracle-setup.sh`:

```bash
#!/usr/bin/env bash
# xmage-oracle-setup.sh -- build XMage out of tree for the compliance oracle
# (docs/superpowers/specs/2026-10-02-xmage-compliance-oracle-design.md §4-§5).
#
# Idempotent. Re-run after an XMAGE_REF bump. Everything lands under
# $XMAGE_ORACLE_DIR (default /mnt/sata/gorge-training/xmageoracle); nothing in
# the gorge tree changes. The build and the smoke test each run inside a
# MemoryMax=7G user scope: this box allows 8 vCPU / 8 GB for the whole job.
#
#   scripts/xmage-oracle-setup.sh            # maven + clone + build + smoke + measure
#   scripts/xmage-oracle-setup.sh --smoke    # smoke + measure only (already built)
set -euo pipefail

repo=$(git rev-parse --show-toplevel)
ref=${XMAGE_REF:-$(sed -n 's/^XMAGE_REF *?= *//p' "$repo/Makefile")}
root=${XMAGE_ORACLE_DIR:-$(sed -n 's/^XMAGE_ORACLE_DIR *?= *//p' "$repo/Makefile")}
mvn_version=3.9.11
smoke_test=org.mage.test.cards.abilities.keywords.FlashbackTest
[ -n "$ref" ] && [ -n "$root" ] || { echo "xmage-oracle-setup: XMAGE_REF/XMAGE_ORACLE_DIR unset" >&2; exit 2; }
case "$root" in /tmp/*) echo "xmage-oracle-setup: $root is RAM-backed; use /mnt/sata" >&2; exit 2;; esac
mkdir -p "$root"
mvn="$root/maven/bin/mvn"
scope=(systemd-run --user --scope --quiet -p MemoryMax=7G --)

if [ "${1:-}" != "--smoke" ]; then
  if [ ! -x "$mvn" ]; then
    tgz="$root/apache-maven-$mvn_version-bin.tar.gz"
    curl -fsSL -o "$tgz" "https://archive.apache.org/dist/maven/maven-3/$mvn_version/binaries/apache-maven-$mvn_version-bin.tar.gz"
    rm -rf "$root/maven" && mkdir -p "$root/maven"
    tar -xzf "$tgz" -C "$root/maven" --strip-components=1
  fi
  if [ ! -d "$root/mage/.git" ]; then
    git clone --filter=blob:none https://github.com/magefree/mage.git "$root/mage"
  fi
  git -C "$root/mage" fetch -q origin
  git -C "$root/mage" checkout -q "$ref"
  start=$(date +%s)
  ( cd "$root/mage" && MAVEN_OPTS=-Xmx3g "${scope[@]}" /usr/bin/time -v -o "$root/build.time" \
      "$mvn" -B -q -T 2 -pl Mage.Tests -am install -DskipTests ) 2>&1 | tail -20
  echo "build_seconds=$(( $(date +%s) - start ))" > "$root/build.env"
fi

start=$(date +%s)
( cd "$root/mage" && "${scope[@]}" /usr/bin/time -v -o "$root/smoke.time" \
    "$mvn" -B -pl Mage.Tests surefire:test -Dtest="$smoke_test" -DfailIfNoTests=false ) 2>&1 | tail -30
echo "smoke_seconds=$(( $(date +%s) - start ))" > "$root/smoke.env"

report="$root/mage/Mage.Tests/target/surefire-reports/TEST-$smoke_test.xml"
[ -f "$report" ] || { echo "xmage-oracle-setup: no surefire report at $report" >&2; exit 1; }
python3 - "$report" "$root" "$ref" <<'EOF'
import sys, re, xml.etree.ElementTree as ET
report, root, ref = sys.argv[1:4]
t = ET.parse(report).getroot()
cases = sorted(float(c.get("time", 0)) for c in t.iter("testcase"))
def rss(p):
    m = re.search(r"Maximum resident set size \(kbytes\): (\d+)", open(p).read())
    return int(m.group(1)) // 1024 if m else -1
env = {}
for f in ("build.env", "smoke.env"):
    try:
        for line in open(f"{root}/{f}"):
            k, v = line.strip().split("=")
            env[k] = v
    except FileNotFoundError:
        pass
warm = cases[1:] or cases
med = warm[len(warm) // 2] if warm else -1
lines = [
    f"# XMage oracle measurement ({ref})", "",
    f"- tests: {t.get('tests')} failures: {t.get('failures')} errors: {t.get('errors')}",
    f"- build: {env.get('build_seconds', 'n/a')} s, peak RSS {rss(root + '/build.time') if 'build_seconds' in env else 'n/a'} MiB",
    f"- smoke run: {env.get('smoke_seconds')} s wall, peak RSS {rss(root + '/smoke.time')} MiB",
    f"- per-test seconds (warm, excluding the first): median {med:.3f}, max {max(warm) if warm else -1:.3f}",
    f"- first test (includes card DB load): {cases[-1] if cases else -1:.3f} s (max over all)",
    "",
    "Spec §10 revisit trigger: warm median above 0.200 s or smoke peak RSS above 2560 MiB.",
]
open(f"{root}/MEASURE.md", "w").write("\n".join(lines) + "\n")
print("\n".join(lines))
EOF
```

```bash
chmod +x scripts/xmage-oracle-setup.sh
bash -n scripts/xmage-oracle-setup.sh && shellcheck scripts/xmage-oracle-setup.sh || true
```

Expected: `bash -n` prints nothing. Fix any shellcheck error (warnings may stay).

- [ ] **Step 3: Add the Make target**

Add to the `help` echo block:

```make
	@echo "  make xmage-oracle-setup — build XMage at XMAGE_REF out of tree (heavy; run alone)"
```

Add after `compliance-manifests` (or after `compile-cards` if Task 2 has not merged):

```make
.PHONY: xmage-oracle-setup
xmage-oracle-setup:
	XMAGE_REF=$(XMAGE_REF) XMAGE_ORACLE_DIR=$(XMAGE_ORACLE_DIR) scripts/xmage-oracle-setup.sh
```

- [ ] **Step 4: Run it**

```bash
free -g | sed -n 2p      # "available" must be >= 8
make xmage-oracle-setup 2>&1 | tee /mnt/sata/gorge-training/xmageoracle/setup.log | tail -15
```

Expected: the final lines are the MEASURE.md body, with `failures: 0 errors: 0` for `FlashbackTest` (19 test methods at this pin).

If the build fails on JDK 21 (XMage targets `java.version` 8): record the first compiler error in the report, then retry once with `-Dmaven.compiler.release=8`. If that also fails, stop. The X4–X10 plan then needs a JDK 17 decision from the operator.

- [ ] **Step 5: Check the measurements against spec §10**

```bash
cat /mnt/sata/gorge-training/xmageoracle/MEASURE.md
```

If the warm median is above 0.200 s or the smoke peak RSS is above 2560 MiB, the spec's §4 budget does not hold. Say so first in the report; the operator revisits before X4.

- [ ] **Step 6: Commit**

```bash
git add scripts/xmage-oracle-setup.sh Makefile
git commit -m "scripts: xmage-oracle-setup.sh builds XMage out of tree and measures it

<paste the MEASURE.md body here>"
```

---

### Task 4: Move the oracle runner out of the test file (X3a)

Pure move: no behaviour change. Independent of Tasks 1–3 and 6.

**Files:**
- Create: `rules/oracle_run.go`
- Modify: `rules/oracle_audit_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: the unexported runner (`oracleFile`, `oracleScenario`, `oracleSeat`, `oracleStep`, `oracleObserve`, `oracleAnswer`, `oracleExpect`, `oracleOffered`, `oracleCanBlock`, `oracleCount`, `oracleHarnessError`, `harnessf`, `oracleRun` and all its methods, `oracleZones`, `oracleFiller`, the helpers through `runOracleScenario`) in a non-test file of package `rules`, so non-test code in Task 5 can call `runOracleScenario`.

- [ ] **Step 1: Create the worktree and record the baseline**

```bash
cd /home/sadams/projects/gorge && scripts/agent-worktree.sh xo-runner-move
cd .worktrees/xo-runner-move
GOMEMLIMIT=1GiB go test ./rules/ -run 'TestOracle' -count=1 2>&1 | tail -3
```

Expected: `ok  github.com/adams-shaun/gorge/rules`. Record the time. It must not be ~0.1 s; that would mean the corpus skipped.

- [ ] **Step 2: Move the declarations**

In `rules/oracle_audit_test.go`, everything from the line `type oracleFile struct {` (currently line 97) through the closing `}` of `func runOracleScenario` (currently ~line 1360) moves to `rules/oracle_run.go`. The rest stays in the test file: `oracleKnownDivergent` (above that range) and everything below `runOracleScenario` (`oracleDivergentFile`, `oracleDivergentRow`, `oracleDivergent`, `loadOracleFiles`, `TestOracleScenarioFilesWellFormed`, `TestOracleAudit`).

`rules/oracle_run.go` starts with the doc comment that opens the test file today (lines 3–21, "Oracle-text card audit … the GPL boundary, AGENTS.md"), then:

```go
package rules

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)
```

then the moved block, byte for byte. Afterwards, fix both import lists with `goimports -w rules/oracle_run.go rules/oracle_audit_test.go`. If `goimports` is absent, use `go run golang.org/x/tools/cmd/goimports@latest`; it is a dev tool, not a module dependency. Then confirm `go.mod` is unchanged.

- [ ] **Step 3: Verify the move is complete and test-free**

```bash
git diff --stat
/usr/bin/grep -c 'func (r \*oracleRun)' rules/oracle_audit_test.go        # expect 0
/usr/bin/grep -nE '"testing"|internal/testutil' rules/oracle_run.go        # expect no output
git diff go.mod go.sum                                                     # expect no output
go vet ./rules/
```

- [ ] **Step 4: Run the oracle tests and the replay check**

Run: `GOMEMLIMIT=1GiB go test ./rules/ -run 'TestOracle' -count=1 -v 2>&1 | /usr/bin/grep -cE '^\s+--- (PASS|FAIL)'` then `GOMEMLIMIT=1GiB go test ./rules/ -run 'TestOracle' -count=1`
Expected: the same subtest count as before the move, and `ok`.

- [ ] **Step 5: Run the architecture tests**

Run: `go test ./internal/archtest/... -count=1 2>&1 | tail -3` (if the directory is absent, `go test $(go list ./... | /usr/bin/grep archtest) -count=1`)
Expected: `ok`. If an arch test forbids a new non-test import into `rules`, report its message rather than weakening the test.

- [ ] **Step 6: Commit**

```bash
git add rules/oracle_run.go rules/oracle_audit_test.go
git commit -m "rules: move the oracle scenario runner out of the test file (no behaviour change)

The compliance pipeline (docs/superpowers/specs/2026-10-02-xmage-compliance-oracle-design.md
§5) needs to call runOracleScenario from non-test code."
```

---

### Task 5: Snapshot and JSON entry point (X3b)

Depends on Task 4 (merged).

**Files:**
- Create: `rules/oracle_snapshot.go`, `rules/oracle_export.go`, `rules/oracle_snapshot_test.go`
- Modify: `rules/oracle_run.go` (add three fields to `oracleRun`; record decisions in `submit`; take snapshots in `runOracleScenario`)

**Interfaces:**
- Consumes: `runOracleScenario(reg *cards.Registry, sc oracleScenario) (fails []string, transcript []string, run *oracleRun)`; `(*oracleRun).objName`; `normCounter`; `poolString`; `e.Power`, `e.Toughness`, `e.Keywords`, `e.Derived(id).Types`, `e.Colors(id) string`; `state.Object` fields `Controller`, `Owner`, `IsToken`, `Tapped`, `FaceDown`, `Damage`, `Counters`, `AttachedTo`, `AttachedPlayer`, `HasAttachedPlayer`, `IsAttacking`, `BlockedBy`, `Source`, `Ability`; `state.Game` fields `Turn`, `Step`, `Active`, `Priority`, `Over`, `Winner`, `Draw`, `Stack` (top = last element), `Players[i].Life`, `.Pool`, `.Counters`; `e.G.Zone(z, p)` (library index 0 = top).
- Produces (later plans rely on these exact names):
  - `type OracleSnapshot struct` with JSON keys `checkpoint, turn, step, active, priority, over, winner, players, permanents, stack`
  - `type OracleSnapPlayer`, `type OracleSnapPerm`, `type OracleSnapStack`, `type OracleDecision` (fields below)
  - `type OracleResult struct { Fails, Transcript []string; Snapshots []OracleSnapshot; Decisions []OracleDecision }`
  - `func RunOracleScenarioJSON(reg *cards.Registry, raw []byte) (OracleResult, error)`
  - Checkpoint names: `"setup"`, then `"step <i> (<op>)"` for i = 0…n-1.

- [ ] **Step 1: Create the worktree**

```bash
cd /home/sadams/projects/gorge && scripts/agent-worktree.sh xo-snapshot
cd .worktrees/xo-snapshot
```

- [ ] **Step 2: Write the failing tests**

`rules/oracle_snapshot_test.go`:

```go
package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

const boltTheBears = `{
  "name": "bolt-the-bears", "cr": ["608.2"], "why": "snapshot fixture",
  "setup": {
    "p0": {"hand": ["Lightning Bolt"]},
    "p1": {"battlefield": ["Grizzly Bears", "Grizzly Bears"], "library_top": ["Shock", "Lightning Bolt"]}
  },
  "steps": [
    {"op": "cast", "seat": 0, "card": "p0:Lightning Bolt", "mana": "R", "targets": ["p1:Grizzly Bears#2"]},
    {"op": "resolve"}
  ],
  "expect": [{"card": "p1:Grizzly Bears#2", "zone": "graveyard"}]
}`

func snapPerm(s OracleSnapshot, ref string) (OracleSnapPerm, bool) {
	for _, p := range s.Permanents {
		if p.Ref == ref {
			return p, true
		}
	}
	return OracleSnapPerm{}, false
}

func TestOracleSnapshotSetupAndSteps(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	res, err := RunOracleScenarioJSON(reg, []byte(boltTheBears))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("scenario failed: %v\n%s", res.Fails, strings.Join(res.Transcript, "\n"))
	}
	if got := len(res.Snapshots); got != 3 {
		t.Fatalf("%d snapshots, want 3 (setup + 2 steps)", got)
	}
	names := []string{res.Snapshots[0].Checkpoint, res.Snapshots[1].Checkpoint, res.Snapshots[2].Checkpoint}
	if strings.Join(names, "|") != "setup|step 0 (cast)|step 1 (resolve)" {
		t.Fatalf("checkpoints %q", names)
	}
	setup, final := res.Snapshots[0], res.Snapshots[2]
	bears, ok := snapPerm(setup, "p1:Grizzly Bears#2")
	if !ok || bears.PT != "2/2" || bears.Controller != 1 || bears.Owner != 1 {
		t.Fatalf("setup bears #2 = %+v, %v", bears, ok)
	}
	// p1's library: seat 1 draws nothing during seat 0's turn 1, so the
	// setup order is still intact at every checkpoint of this scenario.
	if got := setup.Players[1].LibraryTop; len(got) < 2 || got[0] != "Shock" || got[1] != "Lightning Bolt" {
		t.Fatalf("setup p1 library_top = %q, want Shock, Lightning Bolt first", got)
	}
	if _, ok := snapPerm(final, "p1:Grizzly Bears#2"); ok {
		t.Fatal("bears #2 still on the battlefield after Bolt resolved")
	}
	if _, ok := snapPerm(final, "p1:Grizzly Bears"); !ok {
		t.Fatal("bears #1 missing after Bolt resolved")
	}
	if g := final.Players[1].Graveyard; len(g) != 1 || g[0] != "Grizzly Bears" {
		t.Fatalf("p1 graveyard = %q", g)
	}
	if g := final.Players[0].Graveyard; len(g) != 1 || g[0] != "Lightning Bolt" {
		t.Fatalf("p0 graveyard = %q", g)
	}
	if len(final.Stack) != 0 {
		t.Fatalf("stack not empty: %+v", final.Stack)
	}
	if cast := res.Snapshots[1]; len(cast.Stack) != 1 || cast.Stack[0].Kind != "spell" || cast.Stack[0].Source != "p0:Lightning Bolt" {
		t.Fatalf("after cast, stack = %+v", cast.Stack)
	}
}

func TestOracleSnapshotRefsMatchScenarioRefs(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	res, err := RunOracleScenarioJSON(reg, []byte(boltTheBears))
	if err != nil {
		t.Fatal(err)
	}
	var refs []string
	for _, p := range res.Snapshots[0].Permanents {
		if p.Controller == 1 {
			refs = append(refs, p.Ref)
		}
	}
	if strings.Join(refs, "|") != "p1:Grizzly Bears|p1:Grizzly Bears#2" {
		t.Fatalf("p1 refs %q", refs)
	}
}

func TestOracleSnapshotRecordsNonPriorityDecisions(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	res, err := RunOracleScenarioJSON(reg, []byte(boltTheBears))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range res.Decisions {
		if d.Kind == "priority" {
			t.Fatalf("priority decision recorded: %+v", d)
		}
	}
	// Bolt's target is chosen either inside the cast option (no separate
	// decision) or through a target decision. If one is recorded it must be
	// seat 0's, with the bears among its picks.
	for _, d := range res.Decisions {
		if d.Kind == "target" && (d.Seat != 0 || !strings.Contains(strings.Join(d.Picks, ","), "Grizzly Bears")) {
			t.Fatalf("target decision %+v", d)
		}
	}
}

func TestOracleSnapshotDoesNotPerturbReplay(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	sc, err := decodeOracleScenario([]byte(boltTheBears))
	if err != nil {
		t.Fatal(err)
	}
	fails, transcript, run := runOracleScenario(reg, sc)
	if len(fails) != 0 || run.e == nil {
		t.Fatalf("fails %v\n%s", fails, strings.Join(transcript, "\n"))
	}
	if len(run.snaps) != 3 {
		t.Fatalf("%d snapshots", len(run.snaps))
	}
	if diff := diffGames(run.e.G, replayFromLog(t, run.cfg, run.e.L.Events)); diff != "" {
		t.Fatalf("log-only replay differs after snapshotting:\n%s", diff)
	}
}

func TestRunOracleScenarioJSONRejectsUnknownField(t *testing.T) {
	_, err := RunOracleScenarioJSON(nil, []byte(`{"name":"x","steps":[],"bogus":1}`))
	if err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("err = %v, want an unknown-field error naming bogus", err)
	}
}
```

Run: `GOMEMLIMIT=1GiB go test ./rules/ -run 'TestOracleSnapshot|TestRunOracleScenarioJSON' -count=1`
Expected: FAIL to compile, `undefined: RunOracleScenarioJSON`, `undefined: OracleSnapshot`, `undefined: decodeOracleScenario`, `run.snaps undefined`.

- [ ] **Step 3: Add the snapshot types and builder**

`rules/oracle_snapshot.go`:

```go
package rules

import (
	"fmt"
	"sort"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// OracleSnapshot is the engine-neutral observable state of an oracle
// scenario at one checkpoint (spec 2026-10-02-xmage-compliance-oracle-design
// §7). The XMage driver emits the same shape, so the comparator diffs the
// two field by field. Building one never writes game state.
type OracleSnapshot struct {
	Checkpoint string             `json:"checkpoint"`
	Turn       int32              `json:"turn"`
	Step       string             `json:"step"`
	Active     int                `json:"active"`
	Priority   int                `json:"priority"`
	Over       bool               `json:"over"`
	Winner     *int               `json:"winner,omitempty"`
	Players    []OracleSnapPlayer `json:"players"`
	Permanents []OracleSnapPerm   `json:"permanents"`
	Stack      []OracleSnapStack  `json:"stack"`
}

// OracleSnapPlayer is one seat's public and (scenarios are omniscient)
// hidden state.
type OracleSnapPlayer struct {
	Seat         int              `json:"seat"`
	Life         int32            `json:"life"`
	Counters     map[string]int32 `json:"counters,omitempty"`
	Hand         []string         `json:"hand"`      // sorted
	Graveyard    []string         `json:"graveyard"` // zone order
	Exile        []string         `json:"exile"`     // sorted
	Command      []string         `json:"command"`   // sorted
	LibraryCount int              `json:"library_count"`
	LibraryTop   []string         `json:"library_top"` // top first, at most oracleLibraryTopN
	Pool         string           `json:"pool"`        // WUBRGC letters
}

// OracleSnapPerm is one battlefield permanent. Ref follows the scenario ref
// rule: "p<controller>:<name>", "#k" for the k-th (k>1) of that name the
// seat controls in arrival (ObjID) order; tokens are "p<c>:token:<name>".
type OracleSnapPerm struct {
	Ref        string           `json:"ref"`
	Name       string           `json:"name"`
	Controller int              `json:"controller"`
	Owner      int              `json:"owner"`
	Token      bool             `json:"token,omitempty"`
	Tapped     bool             `json:"tapped,omitempty"`
	FaceDown   bool             `json:"face_down,omitempty"`
	PT         string           `json:"pt,omitempty"` // creatures only
	Damage     int32            `json:"damage,omitempty"`
	Counters   map[string]int32 `json:"counters,omitempty"`
	Types      []string         `json:"types"` // sorted
	Colors     string           `json:"colors"`
	Keywords   []string         `json:"keywords,omitempty"` // sorted
	AttachedTo string           `json:"attached_to,omitempty"`
	Attacking  bool             `json:"attacking,omitempty"`
	Blocking   bool             `json:"blocking,omitempty"`
}

// OracleSnapStack is one stack object, listed top first.
type OracleSnapStack struct {
	Kind       string `json:"kind"`   // "spell" or "ability"
	Source     string `json:"source"` // ref of the spell, or of the ability's source
	Controller int    `json:"controller"`
}

// OracleDecision is one non-priority decision the scenario answered.
type OracleDecision struct {
	Seat    int      `json:"seat"`
	Kind    string   `json:"kind"` // target, yesno, mode, choose_n, order, attackers, blockers
	Options int      `json:"options"`
	Picks   []string `json:"picks"`
}

const oracleLibraryTopN = 5

// oracleDecisionKind normalizes a decision kind to the cross-engine
// vocabulary; ok is false for kinds that are not compared.
func oracleDecisionKind(k decision.Kind) (string, bool) {
	switch k {
	case decision.KPriority, decision.KMulligan, decision.KStartingPlayer:
		return "", false
	case decision.KTriggerOptional, decision.KCommanderZone:
		return "yesno", true
	case decision.KModes:
		return "mode", true
	case decision.KChoose:
		return "choose_n", true
	case decision.KTriggerOrder, decision.KArrange, decision.KReplacement:
		return "order", true
	}
	return string(k), true
}

func (r *oracleRun) snapCounters(cs []state.Counter) map[string]int32 {
	if len(cs) == 0 {
		return nil
	}
	m := map[string]int32{}
	for _, c := range cs {
		m[normCounter(c.Kind)] += c.N
	}
	return m
}

func (r *oracleRun) zoneNames(z state.Zone, p state.PlayerID, sorted bool) []string {
	ids := r.e.G.Zone(z, p)
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, r.objName(r.e.G.Obj(id)))
	}
	if sorted {
		sort.Strings(out)
	}
	return out
}

// snapshot builds the checkpoint. Read-only: it calls only accessors.
func (r *oracleRun) snapshot(checkpoint string) OracleSnapshot {
	e, g := r.e, r.e.G
	s := OracleSnapshot{
		Checkpoint: checkpoint, Turn: g.Turn, Step: g.Step.String(),
		Active: int(g.Active), Priority: int(g.Priority), Over: g.Over,
		Players: []OracleSnapPlayer{}, Permanents: []OracleSnapPerm{}, Stack: []OracleSnapStack{},
	}
	if g.Over && !g.Draw {
		w := int(g.Winner)
		s.Winner = &w
	}
	for i := range g.Players {
		p := state.PlayerID(i)
		lib := g.Zone(state.ZLibrary, p)
		top := make([]string, 0, oracleLibraryTopN)
		for j := 0; j < len(lib) && j < oracleLibraryTopN; j++ {
			top = append(top, r.objName(g.Obj(lib[j])))
		}
		s.Players = append(s.Players, OracleSnapPlayer{
			Seat: i, Life: g.Players[i].Life, Counters: r.snapCounters(g.Players[i].Counters),
			Hand: r.zoneNames(state.ZHand, p, true), Graveyard: r.zoneNames(state.ZGraveyard, p, false),
			Exile: r.zoneNames(state.ZExile, p, true), Command: r.zoneNames(state.ZCommand, p, true),
			LibraryCount: len(lib), LibraryTop: top, Pool: poolString(g.Players[i].Pool),
		})
	}
	var field []state.ObjID
	for i := range g.Players {
		field = append(field, g.Zone(state.ZBattlefield, state.PlayerID(i))...)
	}
	sort.Slice(field, func(a, b int) bool { return field[a] < field[b] })
	refs := r.snapRefs(field)
	blocking := map[state.ObjID]bool{}
	for _, id := range field {
		for _, b := range g.Obj(id).BlockedBy {
			blocking[b] = true
		}
	}
	for _, id := range field {
		o := g.Obj(id)
		types := append([]string(nil), e.Derived(id).Types...)
		sort.Strings(types)
		kws := append([]string(nil), e.Keywords(id)...)
		sort.Strings(kws)
		p := OracleSnapPerm{
			Ref: refs[id], Name: r.objName(o), Controller: int(o.Controller), Owner: int(o.Owner),
			Token: o.IsToken, Tapped: o.Tapped, FaceDown: o.FaceDown, Damage: o.Damage,
			Counters: r.snapCounters(o.Counters), Types: types, Colors: e.Colors(id), Keywords: kws,
			Attacking: o.IsAttacking, Blocking: blocking[id],
		}
		if oracleHasFold(types, "Creature") {
			p.PT = fmt.Sprintf("%d/%d", e.Power(id), e.Toughness(id))
		}
		switch {
		case o.AttachedTo != 0:
			if ref, ok := refs[o.AttachedTo]; ok {
				p.AttachedTo = ref
			} else {
				p.AttachedTo = r.objName(g.Obj(o.AttachedTo))
			}
		case o.HasAttachedPlayer:
			p.AttachedTo = fmt.Sprintf("p%d", o.AttachedPlayer)
		}
		s.Permanents = append(s.Permanents, p)
	}
	for i := len(g.Stack) - 1; i >= 0; i-- {
		o := g.Obj(g.Stack[i])
		if o == nil {
			continue
		}
		it := OracleSnapStack{Kind: "spell", Controller: int(o.Controller)}
		src := o
		if o.Ability != nil {
			it.Kind = "ability"
			src = g.Obj(o.Source)
		}
		it.Source = r.stackRef(src)
		s.Stack = append(s.Stack, it)
	}
	return s
}

// snapRefs assigns scenario-style refs to ids, which must be in ObjID order.
func (r *oracleRun) snapRefs(ids []state.ObjID) map[state.ObjID]string {
	refs := make(map[state.ObjID]string, len(ids))
	seen := map[string]int{}
	for _, id := range ids {
		o := r.e.G.Obj(id)
		base := fmt.Sprintf("p%d:%s", o.Controller, r.objName(o))
		if o.IsToken {
			base = fmt.Sprintf("p%d:token:%s", o.Controller, r.objName(o))
		}
		seen[base]++
		if seen[base] > 1 {
			base = fmt.Sprintf("%s#%d", base, seen[base])
		}
		refs[id] = base
	}
	return refs
}

// stackRef names a spell or ability source by the scenario ref bound at
// setup when there is one (a card keeps its ref across zones), else by
// "p<owner>:<name>".
func (r *oracleRun) stackRef(o *state.Object) string {
	if o == nil {
		return ""
	}
	best := ""
	for ref, id := range r.refs {
		if id == o.ID && (best == "" || ref < best) {
			best = ref
		}
	}
	if best != "" {
		return best
	}
	return fmt.Sprintf("p%d:%s", o.Owner, r.objName(o))
}
```

The identifiers above were checked against main on 2026-10-02: `state.Object.ID` (`state/object.go:523`), `state.Counter{Kind string; N int32}` (`state/object.go:12`), `(*Engine).Colors(id) string` (`rules/layers_derived.go:1080`), `Step.String()` (`state/ids.go:109`). `stackRef` loops over a map but keeps the lexically smallest ref, so the output does not depend on iteration order.

- [ ] **Step 4: Record decisions and take snapshots in the runner**

In `rules/oracle_run.go`, add three fields to `oracleRun`:

```go
	snaps      []OracleSnapshot
	decisions  []OracleDecision
	noSnapshot bool // set by callers that only want pass/fail
```

In `func (r *oracleRun) submit(...)`, directly after the `r.logf("  [%s] p%d %s -> %q", ...)` line:

```go
	if kind, ok := oracleDecisionKind(d.Kind); ok {
		r.decisions = append(r.decisions, OracleDecision{Seat: int(d.Player), Kind: kind, Options: len(d.Options), Picks: labels})
	}
```

In `runOracleScenario`, directly after the `if err := r.build(sc); err != nil { … }` block:

```go
	if !r.noSnapshot {
		r.snaps = append(r.snaps, r.snapshot("setup"))
	}
```

and inside the step loop, directly after the `if err := r.do(st); err != nil { … }` block:

```go
		if !r.noSnapshot {
			r.snaps = append(r.snaps, r.snapshot(fmt.Sprintf("step %d (%s)", i, st.Op)))
		}
```

`noSnapshot` defaults to false, so `TestOracleAudit` also builds snapshots, and its replay check covers every existing scenario. Leave it that way unless Step 7 shows the audit's runtime more than 1.5× the Task 4 baseline. In that case set `r.noSnapshot = true` from `TestOracleAudit` only, and say so in the commit.

- [ ] **Step 5: Add the JSON entry point**

`rules/oracle_export.go`:

```go
package rules

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/adams-shaun/gorge/cards"
)

// OracleResult is one scenario's outcome for the compliance pipeline: the
// runner's mismatches against the scenario's own expectations, its
// transcript, and the checkpoint snapshots and decisions the comparator
// diffs against XMage.
type OracleResult struct {
	Fails      []string         `json:"fails,omitempty"`
	Transcript []string         `json:"transcript,omitempty"`
	Snapshots  []OracleSnapshot `json:"snapshots"`
	Decisions  []OracleDecision `json:"decisions"`
}

// decodeOracleScenario decodes one scenario object (the elements of an
// oracle file's "scenarios" array), rejecting unknown fields as the file
// loader does.
func decodeOracleScenario(raw []byte) (oracleScenario, error) {
	var sc oracleScenario
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&sc); err != nil {
		return oracleScenario{}, fmt.Errorf("oracle scenario: %v", err)
	}
	return sc, nil
}

// RunOracleScenarioJSON plays one scenario against the corpus in reg.
func RunOracleScenarioJSON(reg *cards.Registry, raw []byte) (OracleResult, error) {
	sc, err := decodeOracleScenario(raw)
	if err != nil {
		return OracleResult{}, err
	}
	fails, transcript, run := runOracleScenario(reg, sc)
	return OracleResult{
		Fails: fails, Transcript: transcript,
		Snapshots: append([]OracleSnapshot{}, run.snaps...),
		Decisions: append([]OracleDecision{}, run.decisions...),
	}, nil
}
```

- [ ] **Step 6: Run the new tests**

Run: `GOMEMLIMIT=1GiB go test ./rules/ -run 'TestOracleSnapshot|TestRunOracleScenarioJSON' -count=1 -v`
Expected: PASS for all five. If `TestOracleSnapshotSetupAndSteps` fails only on `library_top` order, read `LibraryOrder`'s apply in `events/apply.go`. Correct the snapshot's top-first reading to match how the zone stores it, not the test: the test pins the scenario schema's "first = top".

- [ ] **Step 7: Run the whole oracle audit and compare runtime**

Run: `GOMEMLIMIT=1GiB go test ./rules/ -run 'TestOracle' -count=1 2>&1 | tail -2`
Expected: `ok`. Every existing scenario still passes and replays with snapshots on. Compare the time with Task 4's baseline (see Step 4's 1.5× rule).

- [ ] **Step 8: Commit**

```bash
git add rules/oracle_snapshot.go rules/oracle_export.go rules/oracle_snapshot_test.go rules/oracle_run.go
git commit -m "rules: oracle runner emits engine-neutral snapshots and decisions; RunOracleScenarioJSON

Spec 2026-10-02-xmage-compliance-oracle-design §7: the comparator diffs
these against the XMage driver's. Snapshotting is read-only; every existing
oracle scenario still replays byte-identically with it on."
```

---

### Task 6: `FORGE_REF` bump to a corpus with Reality Fracture (X0)

Independent of Tasks 1–5. **This is the first corpus bump since 2026-09-05** (`git log -G'^FORGE_REF' -- Makefile`). `.cards` is shared: every worktree symlinks the main checkout's. The bump therefore runs against a **private** corpus in its worktree, and the shared corpus is switched only at merge (Step 9).

**Files:**
- Modify: `Makefile:43` (`FORGE_REF`), `rules/heads_test.go` (`acceptanceHeads`), `rules/acceptance_test.go` (`knownUnsupported`, only rows the bump moves), `rules/paramcensus_test.go` (`knownUnsupportedParams`, same), `rules/count_head_ratchet_test.go` (if it fails), `cmd/repro/testdata/feedback/20260914T120000Z-fb01/` (regenerated), any `rules/testdata/oracle/**` file whose `oracle_sha` went stale (re-derived, Step 6)

**Interfaces:**
- Consumes: nothing from other tasks.
- Produces: `FORGE_REF ?= fb4d8091126051b0c579db5f3bfdcb7e03aae63d` on main, so `go run ./cmd/compliance resolve compliance/manifests/FRA.json` (Task 2) reports FRA's cards in the corpus.

- [ ] **Step 1: Create the worktree with a private corpus**

```bash
cd /home/sadams/projects/gorge && scripts/agent-worktree.sh xo-forge-bump
cd .worktrees/xo-forge-bump
rm .cards                                   # the symlink only; never rm -r a shared corpus
sed -i 's/^FORGE_REF  ?= .*/FORGE_REF  ?= fb4d8091126051b0c579db5f3bfdcb7e03aae63d/' Makefile
make fetch-cards compile-cards 2>&1 | tail -3
python3 -c "import json;print(json.load(open('.cards/cards.lock'))['commit'])"
```

Expected: the last line prints `fb4d8091126051b0c579db5f3bfdcb7e03aae63d`, and `ls -ld .cards` shows a directory, not a symlink.

- [ ] **Step 2: Measure coverage before touching any test**

```bash
make report 2>&1 | /usr/bin/grep -E 'cards:|tokens:'
```

Record both lines for the commit message. Compare with AGENTS.md's figures at the old pin (`cards: 33667  playable: 31005 (92.1%)`, `tokens: 839`).

- [ ] **Step 3: Run the suite under the memory cap and collect the failures**

```bash
systemd-run --user --scope --quiet -p MemoryMax=7G -- env GOMEMLIMIT=1GiB \
  go test -p 1 ./... 2>&1 | tee /mnt/sata/gorge-training/xmageoracle/bump-suite-1.log | /usr/bin/grep -E '^(---|FAIL|ok)' | /usr/bin/grep -v '^ok' | head -60
```

Expected failures, each handled by the next steps: `TestHeads`, `TestEveryRepoDeckIsFullySupported` and/or `TestEveryRepoDeckParamsAreRead` (if any repo-deck card's script changed), `TestOracleAudit` (stale `oracle_sha`), `cmd/repro`'s committed-fixture gate (DIVERGED). Anything else is unexpected. Investigate it before going on, and list it in the report.

- [ ] **Step 4: Re-pin the golden heads**

Run: `GOMEMLIMIT=1GiB go test ./rules/ -run 'TestHeads$' -count=1 -v 2>&1 | /usr/bin/grep -iE 'head|want|got' | head -20`

Copy each reported new head into `rules/heads_test.go`'s `acceptanceHeads` for the seat counts that changed, then re-run. Expected: PASS. Before re-pinning, confirm the replay is still exact: `go test ./rules/ -run 'TestRepoDeckGamesReplayExactly' -count=1` must PASS. A head that moves only because the corpus moved is expected; a replay that diverges is a real bug.

- [ ] **Step 5: Settle the two ratchets**

Run: `GOMEMLIMIT=1GiB go test ./rules/ -run 'TestEveryRepoDeckIsFullySupported|TestEveryRepoDeckParamsAreRead' -count=1 -v 2>&1 | tail -40`

For each card the test names:
- **Newly unsupported** (its script changed and now uses an unimplemented primitive): add the row it asks for, with the reason `"FORGE_REF fb4d809: <primitive>"`.
- **Stale** (the new script no longer needs the missing primitive): delete the row.

Edit only rows the test names. Re-run until PASS.

- [ ] **Step 6: Re-derive stale oracle scenarios**

Run: `GOMEMLIMIT=1GiB go test ./rules/ -run 'TestOracleAudit$' -count=1 2>&1 | /usr/bin/grep 'Oracle text changed'`

For each file it names, print the new Oracle packet with `go run ./cmd/oraclepacket "<card>"` and compare it with the scenario's `why` and expectations (spec 2026-09-27 §7, "Oracle drift"):
- **The erratum does not touch what a scenario asserts.** Update `oracle_sha` to the printed digest, and add one line per file to the commit message: `<card>: erratum <one-line summary>, scenarios unaffected`.
- **The erratum changes an expectation.** Re-derive that scenario from the new Oracle text plus the CR, and name it in the commit message. Never fit an expectation to engine output.

Re-run until no stale-text errors remain. Any remaining ordinary failures must already be ratcheted.

- [ ] **Step 7: Regenerate the feedback fixture**

```bash
REPRO_REGEN_FIXTURE=1 GOMEMLIMIT=1GiB go test ./cmd/repro -run TestGenerateCommittedFixture -count=1
GOMEMLIMIT=1GiB go test ./cmd/repro -count=1
git status --short cmd/repro/testdata
```

Expected: the second command prints `ok`, and the status shows only the fixture's files changed. `match.json` must still contain no token script text: `/usr/bin/grep -c 'tokens_unread\|"tokens"' cmd/repro/testdata/feedback/20260914T120000Z-fb01/match.json` prints `0`.

- [ ] **Step 8: Full suite again**

```bash
systemd-run --user --scope --quiet -p MemoryMax=7G -- env GOMEMLIMIT=1GiB \
  go test -p 1 ./... 2>&1 | tee /mnt/sata/gorge-training/xmageoracle/bump-suite-2.log | /usr/bin/grep -E '^(---|FAIL)' | head
make lint 2>&1 | tail -3
```

Expected: no `FAIL`, and lint clean. Then commit:

```bash
git add Makefile rules/heads_test.go rules/acceptance_test.go rules/paramcensus_test.go cmd/repro/testdata/feedback/20260914T120000Z-fb01
git add $(git diff --name-only -- rules/count_head_ratchet_test.go 'rules/testdata/oracle/*')
git commit -m "cards: bump FORGE_REF to fb4d809 (2026-10-02) for Reality Fracture

<make report lines before -> after>
<heads re-pinned: seat counts>
<ratchet rows added/removed, one line each>
<oracle_sha updates, one line per file>"
```

- [ ] **Step 9: Cut over the shared corpus at merge (operator step)**

The merge itself is ordinary. Directly after it, the main checkout's shared `.cards`, which every live worktree symlinks, must move to the new pin. Otherwise main's own tests run the old corpus against the new heads:

```bash
cd /home/sadams/projects/gorge
make fetch-cards compile-cards 2>&1 | tail -3
GOMEMLIMIT=1GiB go test ./rules/ -run 'TestHeads$' -count=1
```

Every live branch forked before the bump will fail `TestHeads` until it rebases onto the bump. That is expected and is the reason to land this at a quiet moment. Say so in the ticket report so the controller sequences it.

---

## Self-review

- **Spec coverage.** §9 X0 → Task 6. X1 → Task 3. X2 → Tasks 1–2. X3 → Tasks 4–5. §5's committed paths are covered as follows:
  - `XMAGE_REF` → Task 1.
  - `compliance/manifests` and `NOTICE` → Task 2.
  - `rules/oracle_run.go` → Tasks 4–5.
  - `tools/xmageoracle/`, `cmd/oraclegen`, `cmd/oraclediff`, verdicts, rulings, vocab and `TestDeclaredSetsCompliant` are X4–X7, deliberately in plan 2.
  - §7's snapshot fields are all in `OracleSnapshot`. Poison is carried inside `Counters` (gorge stores poison as a player counter).
  - §4's budget is enforced by the scopes in Tasks 3 and 6.
  - §10's first trigger is checked in Task 3 Step 5.
- **Placeholders.** The only angle-bracket fields are commit-message bodies that carry measured output. Each one names the output it carries.
- **Names.** `ParseSetClass`, `MarshalLines`, `CorpusName`, `runManifest`, `gitHead`, `runResolve`, `OracleSnapshot`, `OracleSnapPlayer`, `OracleSnapPerm`, `OracleSnapStack`, `OracleDecision`, `OracleResult`, `RunOracleScenarioJSON`, `decodeOracleScenario`, `snaps`, `decisions` and `noSnapshot` are each defined once and used with the same spelling.
- **Checked against reality on 2026-10-02.**
  - The engine identifiers Task 5 uses (main).
  - The two parser regexes, over all 589 set classes at `XMAGE_REF`: 93,653 entries, 0 unparsed.
  - Task 3's smoke-test class `FlashbackTest` with 19 `@Test` methods (XMage `master`).
  - XMage's `java.version` 8 against the box's JDK 21 (Task 3 Step 4 handles a failure).
  - Upstream Forge `fb4d809` carrying FRA scripts and `Reality Fracture.txt`.
- **Not yet checked.** Whether gorge's 32,654-name match rate holds at the new pin. At the old pin, 817 XMage names had no corpus match, mostly FRA, Star Wars custom sets and Un-sets. Task 2 Step 5 and Task 6 measure it.
