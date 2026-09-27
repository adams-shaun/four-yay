package paymirror

import (
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Diff is one field-level difference between the two engines a mirror check
// compares. Path is the full reflective path from the engine root
// ("G.Objs[41].Tapped"); A and B are bounded renderings of the two values.
type Diff struct {
	Path string `json:"path"`
	A    string `json:"a"`
	B    string `json:"b"`
}

// maxDiffs bounds one comparison. A divergence that reaches the cap is still
// reported (the signature is computed from what was collected), but a runaway
// structural difference cannot turn one check into a multi-megabyte report.
const maxDiffs = 64

// excludedField names one struct field the equivalence projection leaves out,
// with the reason recorded in the package documentation (see doc.go). The
// key is "<package>.<Type>.<field>" so a same-named field on another type is
// never excluded by accident.
type excludedField struct{ owner, field string }

// excluded lists the rules.Engine fields that are NOT game state: the event
// log (compared separately and semantically, see events.go), per-decision
// memo caches, scratch buffers reused across calls, and counters that tally
// the intent stream rather than the game. Every entry is justified in
// doc.go's exclusion table; keep the two in step.
var excluded = map[excludedField]bool{
	// The log is compared semantically (compareEvents): Seq, DecisionAsk,
	// DecisionMade and priority-reset events legitimately differ because
	// the manual route answers more decisions.
	{"rules.Engine", "L"}: true,
	// The pending decision carries a Seq and Seq-bound payment IDs; it is
	// compared by comparePending with those masked.
	{"rules.Engine", "pending"}: true,
	// Derived-read memo caches and their generation counters: pure caches
	// of reads of G, keyed by an epoch that advances with every ask.
	{"rules.Engine", "derivedMemo"}:          true,
	{"rules.Engine", "derivedMemoStack"}:     true,
	{"rules.Engine", "derivedMemoDepth"}:     true,
	{"rules.Engine", "derivedMemoGen"}:       true,
	{"rules.Engine", "derivedMemoTail"}:      true,
	{"rules.Engine", "derivedMemoAliasFrom"}: true,
	{"rules.Engine", "derivedMemoAliasTo"}:   true,
	{"rules.Engine", "derivedKW"}:            true,
	{"rules.Engine", "derivedTypes"}:         true,
	{"rules.Engine", "derivedDepth"}:         true,
	{"rules.Engine", "derivedPTFrames"}:      true,
	{"rules.Engine", "boardStaticsCache"}:    true,
	{"rules.Engine", "activeStaticsCache"}:   true,
	{"rules.Engine", "mayPlaysCache"}:        true,
	// Layer/static rebuild caches keyed by epoch/version counters.
	{"rules.Engine", "staticContinuous"}: true,
	{"rules.Engine", "staticEpoch"}:      true,
	{"rules.Engine", "staticVersion"}:    true,
	{"rules.Engine", "staticObjs"}:       true,
	{"rules.Engine", "staticQueueBuf"}:   true,
	{"rules.Engine", "activeBuf"}:        true,
	{"rules.Engine", "activeEpoch"}:      true,
	{"rules.Engine", "activeVersion"}:    true,
	{"rules.Engine", "activeDepth"}:      true,
	{"rules.Engine", "activeObjs"}:       true,
	{"rules.Engine", "renames"}:          true,
	{"rules.Engine", "renameEpoch"}:      true,
	{"rules.Engine", "renameVersion"}:    true,
	{"rules.Engine", "renameBuilding"}:   true,
	{"rules.Engine", "layer4Types"}:      true,
	{"rules.Engine", "typesEpoch"}:       true,
	{"rules.Engine", "typesVersion"}:     true,
	{"rules.Engine", "typesObjs"}:        true,
	{"rules.Engine", "typesBuilding"}:    true,
	{"rules.Engine", "sbaQuiet"}:         true,
	{"rules.Engine", "sbaUnquiet"}:       true,
	// Incremental scans and census caches keyed by the log length or a
	// continuous-registry version (the registry itself, e.continuous, is
	// compared; the version only invalidates caches built over it).
	{"rules.Engine", "ascend"}:            true,
	{"rules.Engine", "storied"}:           true,
	{"rules.Engine", "turnsTaken"}:        true,
	{"rules.Engine", "turnsTakenEpoch"}:   true,
	{"rules.Engine", "turnStartTurns"}:    true,
	{"rules.Engine", "turnStartEpoch"}:    true,
	{"rules.Engine", "continuousVersion"}: true,
	// Trigger-scan lookup caches keyed by compiled face pointer.
	{"rules.Engine", "triggerEventMasks"}:  true,
	{"rules.Engine", "triggerObjectMasks"}: true,
	{"rules.Engine", "trigZones"}:          true,
	{"rules.Engine", "trigZonesEp"}:        true,
	{"rules.Engine", "trigFaceZones"}:      true,
	{"rules.Engine", "phaseSpecs"}:         true,
	// Scratch buffers reused across calls (contents after use are garbage).
	{"rules.Engine", "legalOptBuf"}: true,
	{"rules.Engine", "manaAbBuf"}:   true,
	{"rules.Engine", "manaLabels"}:  true,
	{"rules.Engine", "intentBuf"}:   true,
	{"rules.Engine", "sbaIDBuf"}:    true,
	{"rules.Engine", "foreachBuf"}:  true,
	// Intent-stream watchdogs and counters: the manual route answers more
	// decisions by construction, so these count the route, not the game.
	{"rules.Engine", "loop"}:     true,
	{"rules.Engine", "askCount"}: true,
}

// differ walks two values of the same type in lockstep and records every
// primitive-level difference. It reads unexported fields through reflect
// (read-only), so the whole rules.Engine can be compared from outside the
// rules package without an accessor in the engine.
type differ struct {
	diffs   []Diff
	visited map[[2]uintptr]bool
	// excludedHits counts the excluded fields actually encountered, so a test
	// can prove the exclusion table is not stale (a renamed field would
	// silently stop being excluded and start being compared).
	excludedHits map[excludedField]int
}

func newDiffer() *differ {
	return &differ{visited: make(map[[2]uintptr]bool), excludedHits: make(map[excludedField]int)}
}

func (d *differ) add(path string, a, b reflect.Value) {
	if len(d.diffs) >= maxDiffs {
		return
	}
	d.diffs = append(d.diffs, Diff{Path: path, A: render(a, 3), B: render(b, 3)})
}

func (d *differ) full() bool { return len(d.diffs) >= maxDiffs }

// typeKey is the "<pkg>.<Type>" spelling excluded uses.
func typeKey(t reflect.Type) string {
	pkg := t.PkgPath()
	if i := strings.LastIndex(pkg, "/"); i >= 0 {
		pkg = pkg[i+1:]
	}
	return pkg + "." + t.Name()
}

func (d *differ) walk(path string, a, b reflect.Value) {
	if d.full() {
		return
	}
	if !a.IsValid() || !b.IsValid() {
		if a.IsValid() != b.IsValid() {
			d.add(path, a, b)
		}
		return
	}
	if a.Type() != b.Type() {
		d.add(path+"<type>", a, b)
		return
	}
	switch a.Kind() {
	case reflect.Bool:
		if a.Bool() != b.Bool() {
			d.add(path, a, b)
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if a.Int() != b.Int() {
			d.add(path, a, b)
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if a.Uint() != b.Uint() {
			d.add(path, a, b)
		}
	case reflect.Float32, reflect.Float64:
		if a.Float() != b.Float() {
			d.add(path, a, b)
		}
	case reflect.Complex64, reflect.Complex128:
		if a.Complex() != b.Complex() {
			d.add(path, a, b)
		}
	case reflect.String:
		if a.String() != b.String() {
			d.add(path, a, b)
		}
	case reflect.Array:
		for i := 0; i < a.Len(); i++ {
			d.walk(path+"["+strconv.Itoa(i)+"]", a.Index(i), b.Index(i))
		}
	case reflect.Slice:
		// nil and empty are the same game fact (an absent Remembered list is
		// an empty one); only length and element content are compared.
		if a.Len() != b.Len() {
			d.add(path+".len", a, b)
			return
		}
		if a.Len() > 0 && a.Pointer() == b.Pointer() {
			return // the same backing array: shared immutable data
		}
		for i := 0; i < a.Len(); i++ {
			d.walk(path+"["+strconv.Itoa(i)+"]", a.Index(i), b.Index(i))
		}
	case reflect.Map:
		d.walkMap(path, a, b)
	case reflect.Pointer:
		if a.IsNil() || b.IsNil() {
			if a.IsNil() != b.IsNil() {
				d.add(path, a, b)
			}
			return
		}
		if a.Pointer() == b.Pointer() {
			return // shared: compiled corpus data both clones reference
		}
		key := [2]uintptr{a.Pointer(), b.Pointer()}
		if d.visited[key] {
			return
		}
		d.visited[key] = true
		d.walk(path, a.Elem(), b.Elem())
	case reflect.Interface:
		if a.IsNil() || b.IsNil() {
			if a.IsNil() != b.IsNil() {
				d.add(path, a, b)
			}
			return
		}
		d.walk(path, a.Elem(), b.Elem())
	case reflect.Struct:
		t := a.Type()
		owner := typeKey(t)
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			key := excludedField{owner, f.Name}
			if excluded[key] {
				d.excludedHits[key]++
				continue
			}
			sub := f.Name
			if path != "" {
				sub = path + "." + f.Name
			}
			d.walk(sub, a.Field(i), b.Field(i))
		}
	case reflect.Func, reflect.Chan, reflect.UnsafePointer:
		if a.IsNil() != b.IsNil() {
			d.add(path, a, b)
		}
	}
}

// walkMap pairs the two maps' entries by a canonical rendering of the key,
// never by Go's map iteration order, so the report is deterministic.
func (d *differ) walkMap(path string, a, b reflect.Value) {
	if a.Len() == 0 && b.Len() == 0 {
		return
	}
	if !a.IsNil() && a.Pointer() == b.Pointer() {
		return // one shared map object (Game.Tokens): immutable corpus data
	}
	ka := mapByKey(a)
	kb := mapByKey(b)
	keys := make([]string, 0, len(ka)+len(kb))
	for k := range ka {
		keys = append(keys, k)
	}
	for k := range kb {
		if _, ok := ka[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		va, oka := ka[k]
		vb, okb := kb[k]
		sub := path + "{" + k + "}"
		switch {
		case oka && okb:
			d.walk(sub, va, vb)
		case oka:
			d.add(sub, va, reflect.Value{})
		default:
			d.add(sub, reflect.Value{}, vb)
		}
		if d.full() {
			return
		}
	}
}

func mapByKey(m reflect.Value) map[string]reflect.Value {
	out := make(map[string]reflect.Value, m.Len())
	it := m.MapRange()
	for it.Next() {
		out[render(it.Key(), 4)] = it.Value()
	}
	return out
}

// render formats a value (exported or not) without reflect.Value.Interface,
// which panics on values read through unexported fields. depth bounds nested
// composites; a pointer renders as its pointee so two clones' equal values
// read the same.
func render(v reflect.Value, depth int) string {
	var sb strings.Builder
	renderTo(&sb, v, depth)
	s := sb.String()
	if len(s) > 240 {
		s = s[:240] + "..."
	}
	return s
}

func renderTo(sb *strings.Builder, v reflect.Value, depth int) {
	if !v.IsValid() {
		sb.WriteString("<absent>")
		return
	}
	switch v.Kind() {
	case reflect.Bool:
		sb.WriteString(strconv.FormatBool(v.Bool()))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		sb.WriteString(strconv.FormatInt(v.Int(), 10))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		sb.WriteString(strconv.FormatUint(v.Uint(), 10))
	case reflect.Float32, reflect.Float64:
		sb.WriteString(strconv.FormatFloat(v.Float(), 'g', -1, 64))
	case reflect.String:
		sb.WriteString(strconv.Quote(v.String()))
	case reflect.Pointer:
		if v.IsNil() {
			sb.WriteString("nil")
			return
		}
		if depth <= 0 {
			fmt.Fprintf(sb, "&%s", v.Type().Elem().Name())
			return
		}
		sb.WriteString("&")
		renderTo(sb, v.Elem(), depth-1)
	case reflect.Interface:
		if v.IsNil() {
			sb.WriteString("nil")
			return
		}
		renderTo(sb, v.Elem(), depth)
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			sb.WriteString("[]")
			return
		}
		sb.WriteString("[")
		for i := 0; i < v.Len(); i++ {
			if i > 0 {
				sb.WriteString(" ")
			}
			if i >= 16 || depth <= 0 {
				fmt.Fprintf(sb, "...(%d)", v.Len())
				break
			}
			renderTo(sb, v.Index(i), depth-1)
		}
		sb.WriteString("]")
	case reflect.Map:
		keys := make([]string, 0, v.Len())
		vals := make(map[string]string, v.Len())
		it := v.MapRange()
		for it.Next() {
			k := render(it.Key(), depth-1)
			keys = append(keys, k)
			vals[k] = render(it.Value(), depth-1)
		}
		sort.Strings(keys)
		sb.WriteString("map[")
		for i, k := range keys {
			if i > 0 {
				sb.WriteString(" ")
			}
			sb.WriteString(k + ":" + vals[k])
		}
		sb.WriteString("]")
	case reflect.Struct:
		t := v.Type()
		// Compiled corpus types render by name only: their content is shared
		// and immutable, and a full rendering is kilobytes.
		switch typeKey(t) {
		case "cards.Card", "cards.Face":
			if f := v.FieldByName("Name"); f.IsValid() && f.Kind() == reflect.String {
				sb.WriteString(t.Name() + "(" + strconv.Quote(f.String()) + ")")
				return
			}
		case "cards.SA":
			if f := v.FieldByName("Line"); f.IsValid() && f.Kind() == reflect.String {
				sb.WriteString("SA(" + strconv.Quote(f.String()) + ")")
				return
			}
		}
		if depth <= 0 {
			sb.WriteString(t.Name() + "{...}")
			return
		}
		sb.WriteString(t.Name() + "{")
		first := true
		for i := 0; i < t.NumField(); i++ {
			f := v.Field(i)
			if f.IsZero() {
				continue
			}
			if !first {
				sb.WriteString(" ")
			}
			first = false
			sb.WriteString(t.Field(i).Name + ":")
			renderTo(sb, f, depth-1)
		}
		sb.WriteString("}")
	case reflect.Func, reflect.Chan, reflect.UnsafePointer:
		if v.IsNil() {
			sb.WriteString("nil")
		} else {
			sb.WriteString("<" + v.Kind().String() + ">")
		}
	default:
		sb.WriteString("<" + v.Kind().String() + ">")
	}
}

var (
	indexRE = regexp.MustCompile(`\[\d+\]`)
	keyRE   = regexp.MustCompile(`\{[^{}]*\}`)
)

// normalizePath folds per-object indices and map keys so the same divergence
// on different objects shares one signature ("G.Objs[*].Tapped").
func normalizePath(p string) string {
	p = indexRE.ReplaceAllString(p, "[*]")
	return keyRE.ReplaceAllString(p, "{*}")
}
