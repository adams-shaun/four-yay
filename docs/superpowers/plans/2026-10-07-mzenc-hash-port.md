# mzenc hash-port Implementation Plan (Milestone 1)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reproduce MageZero v0.2's `Features` feature-id computation (hashing, occurrence cardinality, numeric thermometer, the feature tree, and the index→name research table) in pure Go, proven byte-identical by replaying golden vectors generated from the vendored Java source.

**Architecture:** A Go package `internal/mzenc` ports `Features.java` op-for-op (design spec `docs/superpowers/specs/2026-10-07-mzenc-design.md` §4). A standalone Java probe (`FeaturesProbe.java`, compiled against a vendored `Features.java` that needs no XMage) records a scripted op sequence and the resulting Java index set as JSON under `testdata/golden/`. `go test` replays each op sequence through the Go engine and asserts exact set equality of ids. This is Milestone 1 of the spec; the `StateEncoder` walkers (spec milestones 2–3) are a separate follow-up plan because they need the gorGE `state.Game` API mapping worked out first.

**Tech Stack:** Go 1.25 (`github.com/adams-shaun/gorge`); JDK 17 (oracle generation only, not in the normal test loop); encoding/json; `math/bits`, `encoding/binary`, `hash/fnv` is NOT used.

## Global Constraints

- Pure Go, **no cgo, no third-party dependencies** in `internal/mzenc`.
- No wall clock, no ambient randomness, no `map` range order that reaches an id. The oracle replay must iterate ops in file order.
- All state mutation discipline is irrelevant here (this package reads nothing); it only computes ids from an explicit op sequence.
- Tests fit the operator budget: 2 GB RSS, 2 vCPU, 1 min wall. Run focused and capped:
  `systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run <X> ./internal/mzenc`.
  Never `go test ./...`, never `-count=1`.
- The byte-identical gate is **exact set equality of `int32` ids** against the Java output. Never approximate, never sort-then-truncate.
- Reference source: `WillWroble/mage` @ master (v0.2, `cb7e9c6f`), `Mage.Server.Plugins/Mage.Player.AI/src/main/java/mage/player/ai/encoder/Features.java`. `TABLE_SIZE` default `2_147_483_647`.
- Commit messages are Conventional Commits (`feat(mzenc): …`, `test(mzenc): …`).

---

### Task 1: Vendor the Java source and build the oracle probe

**Files:**
- Create: `internal/mzenc/testdata/javaharness/Features.java` (vendored copy)
- Create: `internal/mzenc/testdata/javaharness/StateEncoder.java` (stub)
- Create: `internal/mzenc/testdata/javaharness/FeatureMap.java` (stub)
- Create: `internal/mzenc/testdata/javaharness/FeaturesProbe.java`
- Create: `internal/mzenc/testdata/javaharness/README.md`

**Interfaces:**
- Produces: `testdata/golden/*.json` files shaped `{"name": string, "ops": [...] , "indices": [int...]}`; op kinds `feature`, `numeric`, `push`, `pop`.

- [ ] **Step 1: Vendor `Features.java`, and stub its two collaborators**

Copy the v0.2 `Features.java` byte-for-byte, then:
1. Delete line 3 (`import org.apache.log4j.Logger;`) — unused (verified: `Logger` occurs only in the import), so `javac` needs no classpath.
2. Add a one-line comment at the top recording the origin commit (`WillWroble/mage` `cb7e9c6f`, `Features.java`) so the copy is auditable.

`Features` names two types we must NOT pull in: `StateEncoder` (imports all of XMage) and `FeatureMap` (imports `javafx.util.Pair`). Provide minimal same-package stubs in the harness — a test double, not a reimplementation:

```java
// StateEncoder.java (stub, same package)
package mage.player.ai.encoder;
import java.util.*;
public class StateEncoder {
  public final Set<Integer> featureVector = new HashSet<>();   // Features.addIndex writes here
  public final FeatureMap featureMap = new FeatureMap();       // and here, when useFeatureMap
}
```

```java
// FeatureMap.java (stub, same package)
package mage.player.ai.encoder;
import java.io.Serializable;
import java.util.*;
public class FeatureMap implements Serializable {
  public final Map<Integer, Set<String>> map = new TreeMap<>();
  public void addFeature(String name, long namespace, int idx) {
    map.computeIfAbsent(idx, k -> new TreeSet<>()).add(namespace + "/" + name);
  }
}
```

The stubs are faithful for the two things the oracle reads: `featureVector` receives exactly the ids `Features.addIndex` produces, and `featureMap` records `(namespace, name)` per idx with the same key the real class uses. Document in the README that they are test doubles, not the shipped classes.

- [ ] **Step 2: Write `FeaturesProbe.java`**

Put it in the same package (`mage.player.ai.encoder`) and add two accessors to the vendored `Features.java` for the probe:

```java
public java.util.Set<Integer> __vec() { return encoder.featureVector; }
public FeatureMap __fm() { return encoder.featureMap; }
```

Probe body (stdin: one JSON object `{"useFeatureMap":bool,"ops":[...]}`; stdout: `{"indices":[...],"map":...}`):

```java
package mage.player.ai.encoder;

import java.io.*;
import java.nio.charset.StandardCharsets;
import java.util.*;

public final class FeaturesProbe {
  public static void main(String[] args) throws Exception {
    String in = new String(System.in.readAllBytes(), StandardCharsets.UTF_8);
    boolean useFm = in.contains("\"useFeatureMap\":true");
    Features.useFeatureMap = useFm;
    StateEncoder enc = new StateEncoder();          // stub: owns featureVector + featureMap
    Features root = new Features();
    root.setEncoder(enc);
    Deque<Features> st = new ArrayDeque<>();
    st.push(root);
    for (Map<String, Object> op : parseOps(in)) {
      String k = (String) op.get("op");
      String name = (String) op.getOrDefault("name", "");
      boolean callParent = !Boolean.FALSE.equals(op.get("callParent"));
      switch (k) {
        case "feature": st.peek().addFeature(name, callParent); break;
        case "numeric": st.peek().addNumericFeature(name, ((Number) op.get("num")).intValue(), callParent); break;
        case "push":   st.push(st.peek().getSubFeatures(name, !Boolean.FALSE.equals(op.get("passToParent")))); break;
        case "pop":    if (st.size() > 1) st.pop(); break;
        default: throw new IllegalArgumentException("op " + k);
      }
    }
    List<Integer> idx = new ArrayList<>(enc.featureVector);
    Collections.sort(idx);
    StringBuilder sb = new StringBuilder("{\"indices\":[");
    for (int i = 0; i < idx.size(); i++) { if (i > 0) sb.append(','); sb.append(idx.get(i)); }
    sb.append(']');
    if (useFm) {                                   // research path: idx -> [namespace/name]
      sb.append(",\"map\":{");
      boolean first = true;
      for (Map.Entry<Integer, Set<String>> e : enc.featureMap.map.entrySet()) {
        if (!first) sb.append(',');
        first = false;
        sb.append('"').append(e.getKey()).append("\":[");
        boolean f2 = true;
        for (String v : e.getValue()) { if (!f2) sb.append(','); f2 = false; sb.append('"').append(v).append('"'); }
        sb.append(']');
      }
      sb.append('}');
    }
    sb.append('}');
    System.out.println(sb);
  }
  // parseOps: a ~30-line char scanner for the fixed {"op":..,"name":..,"num":..,"passToParent":..,"callParent":..} objects.
  static List<Map<String,Object>> parseOps(String s) { /* implement in this task */ return null; }
}
```

Implement `parseOps` fully (scan objects, read `op`/`name` string values, `num` integer, the two booleans). This is throwaway generator code; correctness is proven by producing the goldens and by hand-checking one.

- [ ] **Step 3: Write `testdata/javaharness/README.md`**

Document: the origin commit, the removed unused import, the op schema, and regeneration:
```bash
MZENC_JDK=/mnt/sata/gorge-training/xmageoracle/jdk   # or any JDK 17
$MZENC_JDK/bin/javac -d /tmp/mzenc-probe \
  internal/mzenc/testdata/javaharness/Features.java \
  internal/mzenc/testdata/javaharness/StateEncoder.java \
  internal/mzenc/testdata/javaharness/FeatureMap.java \
  internal/mzenc/testdata/javaharness/FeaturesProbe.java
for f in internal/mzenc/testdata/golden/*.json; do
  jq -c '{useFeatureMap:(.useFeatureMap//false),ops:.ops}' "$f" \
    | $MZENC_JDK/bin/java -cp /tmp/mzenc-probe mage.player.ai.encoder.FeaturesProbe
done   # paste the "indices" back into each golden's "indices" field
```

- [ ] **Step 4: Compile the probe to prove it builds**

Run: `MZENC_JDK=/mnt/sata/gorge-training/xmageoracle/jdk; $MZENC_JDK/bin/javac -d /tmp/mzenc-probe internal/mzenc/testdata/javaharness/*.java`
Expected: exit 0, no errors.

- [ ] **Step 5: Commit**

```bash
git add internal/mzenc/testdata/javaharness
git commit -m "test(mzenc): vendor Features.java and add the oracle probe"
```

---

### Task 2: Hash core — `mix64`, `hash64`, `indexFor`

**Files:**
- Create: `internal/mzenc/features.go`
- Test: `internal/mzenc/features_test.go`

**Interfaces:**
- Produces: `func mix64(z uint64) uint64`; `func hash64(s string, seed uint64) uint64`; `func indexFor(h uint64, table int64) int32`.
- Produces constants: `globalSeed uint64 = 0x9E3779B185EBCA87`; `defaultTable int64 = 2_147_483_647`; `numericBreakpoints = [...]int{32,64,128,256,512}`.

- [ ] **Step 1: Write the failing test**

```go
package mzenc

import "testing"

func TestIndexForMirrorsJavaSignedLong(t *testing.T) {
	// Java: if (h<0) h=-h; return (int)(h % TABLE_SIZE);
	cases := []struct {
		h    uint64
		want int32
	}{
		{0, 0},
		{1, 1},
		{0x8000000000000000, int32(int64(0x8000000000000000) % defaultTable)}, // MinInt64 overflow, same lattice as Java
		{0xFFFFFFFFFFFFFFFF, int32(int64(1) % defaultTable)},                 // -1 -> negate -> 1
	}
	for _, c := range cases {
		if got := indexFor(c.h, defaultTable); got != c.want {
			t.Fatalf("indexFor(%#x)=%d want %d", c.h, got, c.want)
		}
	}
	if globalSeed != 0x9E3779B185EBCA87 {
		t.Fatalf("globalSeed bit pattern changed")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -run TestIndexForMirrorsJavaSignedLong ./internal/mzenc`
Expected: FAIL — `undefined: indexFor` / `defaultTable`.

- [ ] **Step 3: Implement**

```go
package mzenc

import (
	"encoding/binary"
	"math/bits"
)

const (
	globalSeed   uint64 = 0x9E3779B185EBCA87
	defaultTable int64  = 2_147_483_647
)

var numericBreakpoints = [...]int{32, 64, 128, 256, 512}

func mix64(z uint64) uint64 {
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

func hash64(s string, seed uint64) uint64 {
	data := []byte(s)
	h := mix64(seed ^ (uint64(len(data)) * 0x9E3779B185EBCA87))
	for len(data) >= 8 {
		k := binary.LittleEndian.Uint64(data)
		h ^= mix64(k)
		h = bits.RotateLeft64(h, 27)*0x9E3779B185EBCA87 + 0x165667B19E3779F9
		data = data[8:]
	}
	var k uint64
	for i, b := range data {
		k ^= uint64(b) << (8 * i)
	}
	h ^= mix64(k)
	h ^= h >> 33
	h *= 0xff51afd7ed558ccd
	h ^= h >> 33
	h *= 0xc4ceb9fe1a85ec53
	h ^= h >> 33
	return h
}

// indexFor mirrors Features.indexFor exactly, including the Java long
// overflow when h is math.MinInt64 and the sign of Java's % operator.
func indexFor(h uint64, table int64) int32 {
	sh := int64(h)
	if sh < 0 {
		sh = -sh
	}
	return int32(sh % table)
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test -run TestIndexForMirrorsJavaSignedLong ./internal/mzenc`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mzenc/features.go internal/mzenc/features_test.go
git commit -m "feat(mzenc): port Features hash core (mix64/hash64/indexFor)"
```

---

### Task 3: The feature tree — occurrences, sub-features, thermometer

**Files:**
- Modify: `internal/mzenc/features.go`
- Test: `internal/mzenc/features_test.go`

**Interfaces:**
- Produces: `type Encoder struct { … }` with `NewEncoder(table int64) *Encoder`, `(*Encoder) IDs() map[int32]struct{}`, `(*Encoder) Root() *Node`.
- Produces: `type Node struct { … }` with `(*Node) AddFeature(name string)`, `(*Node) AddFeatureCall(name string, callParent bool)`, `(*Node) AddNumericFeature(name string, num int, callParent bool)`, `(*Node) SubFeatures(name string, passToParent bool) *Node`, `(*Node) StateRefresh()`.

- [ ] **Step 1: Write the failing test (hand-computed tree semantics)**

```go
func TestOccurrenceCardinalityIsDistinctIDs(t *testing.T) {
	e := NewEncoder(defaultTable)
	e.Root().AddFeature("Card")
	e.Root().AddFeature("Card")
	if len(e.IDs()) != 2 {
		t.Fatalf("two repeats of one name must hash Card#1 and Card#2, got %d ids", len(e.IDs()))
	}
}

func TestNumericThermometerEmitsBreakpointsAndLowCounters(t *testing.T) {
	e := NewEncoder(defaultTable)
	e.Root().AddNumericFeature("Power", 50, true)
	// 50 >= 32 -> Power@32; then Power@0..@19 (20 ids). Total 21.
	if len(e.IDs()) != 21 {
		t.Fatalf("Power=50 warm-up: want 21 ids, got %d", len(e.IDs()))
	}
	e2 := NewEncoder(defaultTable)
	e2.Root().AddNumericFeature("Power", 0, true) // 0 < 32, then no 0..n loop -> no ids
	if len(e2.IDs()) != 0 {
		t.Fatalf("Power=0: want 0 ids, got %d", len(e2.IDs()))
	}
}

func TestSubFeatureReusesKeyedNodeAcrossCalls(t *testing.T) {
	e := NewEncoder(defaultTable)
	a := e.Root().SubFeatures("Battlefield", true)
	a.AddFeature("Tapped")
	b := e.Root().SubFeatures("Battlefield", true) // same second call -> same node
	b.AddFeature("Tapped")                          // first add already consumed occurrence 1; this is #2
	if len(e.IDs()) < 2 {
		t.Fatalf("expected distinct ids, got %d", len(e.IDs()))
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test -run 'TestOccurrence|TestNumeric|TestSubFeature' ./internal/mzenc`
Expected: FAIL — `undefined: NewEncoder`.

- [ ] **Step 3: Implement the tree (append to `features.go`)**

```go
import "strconv"

type Encoder struct {
	table      int64
	root       *Node
	vec        map[int32]struct{}
	useFeatureMap bool
}

func NewEncoder(table int64) *Encoder {
	e := &Encoder{table: table, vec: map[int32]struct{}{}}
	e.root = &Node{name: "root", seed: globalSeed, enc: e,
		occurrences: map[string]int{}, subs: map[string]*Node{}, idMap: map[string]string{}}
	return e
}

func (e *Encoder) Root() *Node        { return e.root }
func (e *Encoder) IDs() map[int32]struct{} { return e.vec }

func (e *Encoder) addIndex(h uint64, key string, n *Node) {
	e.vec[indexFor(h, e.table)] = struct{}{}
}

type Node struct {
	parent       *Node
	name         string
	seed         uint64
	passToParent bool
	occurrences  map[string]int
	subs         map[string]*Node
	idMap        map[string]string
	enc          *Encoder
}

func newChild(p *Node, name string) *Node {
	return &Node{parent: p, name: name, seed: hash64(name, p.seed), enc: p.enc,
		occurrences: map[string]int{}, subs: map[string]*Node{}, idMap: map[string]string{}}
}

func (n *Node) AddFeature(name string) { n.AddFeatureCall(name, true) }

func (n *Node) AddFeatureCall(name string, callParent bool) {
	if n.parent != nil && callParent && n.passToParent {
		n.parent.AddFeatureCall(name, true)
	}
	n.occurrences[name]++
	key := name + "#" + strconv.Itoa(n.occurrences[name])
	n.enc.addIndex(hash64(key, n.seed), key, n)
}

func (n *Node) AddNumericFeature(name string, num int, callParent bool) {
	for _, b := range numericBreakpoints {
		if num < b {
			break
		}
		n.AddFeatureCall(name+"@"+strconv.Itoa(b), callParent)
	}
	for i := 0; i < num && i < 20; i++ {
		n.AddFeatureCall(name+"@"+strconv.Itoa(i), callParent)
	}
}

func (n *Node) SubFeatures(name string, passToParent bool) *Node {
	n.AddFeature(name)
	key := name + "#" + strconv.Itoa(n.occurrences[name])
	if c, ok := n.subs[key]; ok {
		return c
	}
	c := newChild(n, key)
	c.passToParent = passToParent
	n.subs[key] = c
	return c
}

func (n *Node) StateRefresh() {
	for k := range n.occurrences {
		n.occurrences[k] = 0
	}
	for _, c := range n.subs {
		c.StateRefresh()
	}
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test -run 'TestOccurrence|TestNumeric|TestSubFeature' ./internal/mzenc`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mzenc/features.go internal/mzenc/features_test.go
git commit -m "feat(mzenc): port the Features feature tree (occurrences, subs, thermometer)"
```

---

### Task 4: The golden-vector oracle replay gate

**Files:**
- Create: `internal/mzenc/testdata/golden/basic.json`
- Create: `internal/mzenc/testdata/golden/cardinality.json`
- Create: `internal/mzenc/testdata/golden/thermometer.json`
- Create: `internal/mzenc/testdata/golden/nested.json`
- Test: `internal/mzenc/oracle_test.go`

**Interfaces:**
- Consumes: `NewEncoder`, `Root`, `AddFeatureCall`, `AddNumericFeature`, `SubFeatures`, `IDs` (Task 3).
- Produces: `type oracleOp struct { Op string `json:"op"`; Name string `json:"name"`; Num int `json:"num"`; CallParent *bool `json:"callParent"`; PassToParent *bool `json:"passToParent"` }` and the replay helper `replay(t *testing.T, ops []oracleOp) map[int32]struct{}`.

- [ ] **Step 1: Author the op sequences and fill `indices` from the Java probe**

Write each golden with its `ops`; run the Task 1 probe (README recipe) on it and paste the printed `indices`. `nested.json` must exercise a three-level chain with `passToParent` both true and false. Example `thermometer.json` ops:

```json
{"ops":[
  {"op":"numeric","name":"Power","num":3},
  {"op":"numeric","name":"Toughness","num":32},
  {"op":"push","name":"Battlefield","passToParent":true},
  {"op":"feature","name":"Tapped"},
  {"op":"pop"}
],
"indices":[]}
```

- [ ] **Step 2: Write the failing test**

```go
package mzenc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type oracleOp struct {
	Op           string `json:"op"`
	Name         string `json:"name"`
	Num          int    `json:"num"`
	CallParent   *bool  `json:"callParent"`
	PassToParent *bool  `json:"passToParent"`
}
type golden struct {
	Name    string     `json:"name"`
	Ops     []oracleOp `json:"ops"`
	Indices []int32    `json:"indices"`
}

func replay(t *testing.T, ops []oracleOp) map[int32]struct{} {
	t.Helper()
	e := NewEncoder(defaultTable)
	stack := []*Node{e.Root()}
	top := func() *Node { return stack[len(stack)-1] }
	boolOr := func(p *bool, def bool) bool { if p == nil { return def }; return *p }
	for _, op := range ops {
		switch op.Op {
		case "feature":
			top().AddFeatureCall(op.Name, boolOr(op.CallParent, true))
		case "numeric":
			top().AddNumericFeature(op.Name, op.Num, boolOr(op.CallParent, true))
		case "push":
			stack = append(stack, top().SubFeatures(op.Name, boolOr(op.PassToParent, true)))
		case "pop":
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		default:
			t.Fatalf("unknown op %q", op.Op)
		}
	}
	return e.IDs()
}

func TestHashOracle(t *testing.T) {
	files, err := filepath.Glob("testdata/golden/*.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("no golden vectors found: %v", err)
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		var g golden
		if err := json.Unmarshal(b, &g); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		want := map[int32]struct{}{}
		for _, id := range g.Indices {
			want[id] = struct{}{}
		}
		got := replay(t, g.Ops)
		if len(got) != len(want) {
			t.Fatalf("%s: id-count got %d want %d", f, len(got), len(want))
		}
		for id := range want {
			if _, ok := got[id]; !ok {
				t.Fatalf("%s: missing java id %d", f, id)
			}
		}
		for id := range got {
			if _, ok := want[id]; !ok {
				t.Fatalf("%s: extra id %d not in java output", f, id)
			}
		}
	}
}
```

- [ ] **Step 3: Run it and drive to green**

Run: `go test -run TestHashOracle ./internal/mzenc`
Expected: first run may FAIL on a real porting bug (that is the point). Fix `features.go` until PASS. If a golden is suspected wrong, regenerate it from the probe, never edit `indices` by hand.

- [ ] **Step 4: Confirm the fixed case is genuinely covered**

Add to `oracle_test.go` an explicit `int32` id assertion you can read in the failure output for `cardinality.json` (two `Card` adds must yield two ids), so a future regression names the file. Run again.

- [ ] **Step 5: Commit**

```bash
git add internal/mzenc/testdata/golden internal/mzenc/oracle_test.go
git commit -m "test(mzenc): golden-vector oracle replay gate (byte-identical ids)"
```

---

### Task 5: FeatureMap research table (optional path)

**Files:**
- Create: `internal/mzenc/featuremap.go`
- Test: `internal/mzenc/featuremap_test.go`

**Interfaces:**
- Produces: `type FeatureMap struct { … }` with `(*FeatureMap) AddFeature(name string, namespace int64, idx int32)`, `(*FeatureMap) Print(w io.Writer)` emitting `idx: [namespace/name], ...` sorted by idx.
- `Encoder` gains a `UseFeatureMap bool` field; when set, `addIndex` also records `key`, namespace `indexFor(hash64(nodeName, parentSeed), table)` (or `-1` at the root), and `idx`.

- [ ] **Step 1: Write the failing test** (level-2 check: one feature's namespace equals the index of its parent's name under the root seed; mirrors `Features.java:156-167`). Then generate a `golden/featuremap.json` with `"useFeatureMap":true` and assert the Go table's `[namespace/name]` set matches the probe's `map` output for that file.

- [ ] **Step 2: Run to verify it fails** — `undefined: FeatureMap`.

- [ ] **Step 3: Implement `featuremap.go` and wire `Encoder.UseFeatureMap` into `addIndex`.**

- [ ] **Step 4: Run** `go test -run TestFeatureMap ./internal/mzenc` — PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mzenc/featuremap.go internal/mzenc/featuremap_test.go internal/mzenc/testdata/golden/featuremap.json
git commit -m "feat(mzenc): port FeatureMap index-to-name research table"
```

---

### Task 6: Package invariants and docs

**Files:**
- Create: `internal/mzenc/doc.go`
- Test: `internal/mzenc/features_test.go` (append)

**Interfaces:**
- Consumes everything above. Produces the package's doc comment naming the spec and the byte-identical contract.

- [ ] **Step 1: Write the failing test**

```go
func TestPackageHasNoClockOrAmbientRand(t *testing.T) {
	// Guard the pure-function contract: the encoder must be a function of its
	// op sequence alone. Assert determinism (same ops -> same ids) here; the
	// repo-wide no-clock/no-rand rule is enforced by internal/archtest.
	for i := 0; i < 2; i++ {
		a := NewEncoder(defaultTable)
		a.Root().AddNumericFeature("Power", 7, true)
		a.Root().SubFeatures("Hand", true).AddFeature("Card")
		b := NewEncoder(defaultTable)
		b.Root().AddNumericFeature("Power", 7, true)
		b.Root().SubFeatures("Hand", true).AddFeature("Card")
		if len(a.IDs()) != len(b.IDs()) {
			t.Fatalf("nondeterministic id count")
		}
	}
}
```

- [ ] **Step 2: Run** `go test -run TestPackageHasNoClockOrAmbientRand ./internal/mzenc` — verify it passes (the code is already deterministic; if it fails, a `map` range reached an id, which is a bug).

- [ ] **Step 3: Write `doc.go`** — one paragraph: package `mzenc` is a byte-identical Go port of MageZero v0.2's `Features` hash; the oracle is `testdata/golden` + `testdata/javaharness`; the `StateEncoder` walkers are the follow-up plan; see the spec.

- [ ] **Step 4: Run the whole package under the capped budget**

Run: `systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m ./internal/mzenc`
Expected: PASS, under 1 min, under 2 GB.

- [ ] **Step 5: Commit**

```bash
git add internal/mzenc/doc.go internal/mzenc/features_test.go
git commit -m "docs(mzenc): package doc and determinism test"
```

---

## Self-Review

**Spec coverage (Milestone 1):** §4 hash engine → Tasks 2–3; occurrence/cardinality → Task 3; thermometer → Task 3; `TABLE_SIZE` default → Task 2; `FeatureMap` research path → Task 5; oracle harness + golden testdata + committed vectors → Tasks 1, 4; `TestHashOracle` hard gate → Task 4; `TestNoClockNoRand` → Task 6; the `indexFor` MinInt64 quirk → Task 2 (explicit case) and Task 4 (real vectors). Spec milestones 2–3 (`StateEncoder` walkers) and spec §7's `TestExtractorCoverage` are **not in this plan** — see below.

**Deferred (separate plan):** Milestones 2–3, the `StateEncoder` walkers over gorGE `state.Game` (`internal/mzenc/state.go`), `unsupportedFeatures` register, and the extractor-vs-XMage measured capture. They need the gorGE `state.Game`/`view` API mapped first; writing them now would be a fabricated plan. The spec §8 keeps them.

**Placeholder scan:** no TBDs. Task 1's `parseOps` is the one method left to the implementer with a stated size (~30 lines throwaway generator) and a validation path (goldens produced + one hand-checked). Task 5 step 1 names the exact parity assertion rather than hand-writing ids.

**Type consistency:** ids are `int32` throughout (`IDs() map[int32]struct{}`, `indexFor` returns `int32`, golden `Indices []int32`) — matches the corrected spec §4. `Node.SubFeatures` returns `*Node` and is spelled consistently. `AddFeatureCall`/`AddFeature`/`AddNumericFeature`/`SubFeatures`/`StateRefresh`/`NewEncoder`/`IDs`/`Root` used identically across tasks.
