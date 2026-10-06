package rules

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/rules/resolve"
)

// The clone policy is recorded on every field of the types Clone copies
// field by field, as a struct tag `clone:"<policy>"` (zero runtime cost: the
// tag is read only here, by reflection, in tests). The four policies say what
// rules/clone.go (cloneWith, cloneResume and their helpers) does with the
// field TODAY:
//
//   - deep:  the clone owns an independent copy. A value field is copied; a
//     slice, map or pointer field is re-allocated (or copied into the
//     clone's own recycled Spare storage), so writing through either engine
//     can never reach the other.
//   - share: immutable or corpus-owned data (compiled *cards.SA, genesis
//     manifests, never-mutated offer lists, copy-on-write indexes), so the
//     clone holds the SAME reference. Clone either copies the reference or
//     leaves the field zero (a dropped cache); it never deep-copies it.
//   - reset: scratch, a memo or a transient in-flight value that is zero at
//     every intent boundary. Clone leaves it zero, re-keys it, or hands the
//     clone its own fresh (or Spare-recycled) storage -- never the original's.
//   - hook:  a harness observer (ManaAbilityHook, paymentStats) Clone
//     deliberately does not copy: it stays nil on the clone.
//
// Embedded anonymous structs (Engine's engineScratch, engineParked, ...) are
// grouping only; their members carry the tags.
const clonePolicyHelp = "every field of a type Clone copies field by field carries `clone:\"deep|share|reset|hook\"`:\n" +
	"  deep  = the clone owns an independent copy (rules/clone.go copies the value; slices, maps and pointers are re-allocated);\n" +
	"  share = immutable or corpus-owned, so the clone aliases the same reference (or drops it), never deep-copies it;\n" +
	"  reset = scratch / memo / transient state zero at an intent boundary: the clone leaves it zero or gets its own fresh or Spare-recycled storage;\n" +
	"  hook  = a harness observer Clone deliberately does not copy (nil on the clone).\n" +
	"Pick the policy that matches how a clone must treat the field, then run `go generate ./rules`: clone_gen.go renders the copy (and any if=/rekey/pool= option) from the tag."

// clonePolicyTypes are the types whose every field must carry a clone policy:
// the Engine (through its embedded clusters), the resume point of a
// rules-side payment window and the value types cloneWith deep-copies field
// by field (the pending cast, the combat, mulligan and opening-hand rounds,
// and a parked ExchangeLife transaction).
var clonePolicyTypes = []reflect.Type{
	reflect.TypeOf(Engine{}),
	reflect.TypeOf(resumePoint{}),
	reflect.TypeOf(pendingCast{}),
	reflect.TypeOf(combatRound{}),
	reflect.TypeOf(mulliganRound{}),
	reflect.TypeOf(openingRound{}),
	reflect.TypeOf(lifeExchangeTransaction{}),
	// The resolution kernel's per-engine state (rules/resolve): its fields
	// carry their own tags, and cloneWith copies Kernel.ForClone().
	reflect.TypeOf(resolve.Kernel{}),
}

var clonePolicies = map[string]bool{"deep": true, "share": true, "reset": true, "hook": true}

// clonePolicyField is one leaf field of a policy type: embedded anonymous
// structs are flattened, so path names the cluster the field lives in.
type clonePolicyField struct {
	path   string
	index  []int
	typ    reflect.Type
	policy string
	// opts are the tag's options after the policy (clone_gen_test.go's
	// vocabulary): if=, pool=, copy=, release=, rekey.
	opts map[string]string
}

// cloneTagOptions are the options a clone tag may carry after its policy;
// clone_gen_test.go documents each.
var cloneTagOptions = map[string]bool{"if": true, "pool": true, "copy": true, "release": true, "rekey": true}

// parseCloneTag splits `policy[,opt[=value]...]`.
func parseCloneTag(tag string) (string, map[string]string) {
	parts := strings.Split(tag, ",")
	var opts map[string]string
	for _, p := range parts[1:] {
		if opts == nil {
			opts = map[string]string{}
		}
		k, v, _ := strings.Cut(p, "=")
		opts[k] = v
	}
	return parts[0], opts
}

func clonePolicyFieldsOf(t reflect.Type) []clonePolicyField {
	var out []clonePolicyField
	var walk func(t reflect.Type, prefix string, index []int)
	walk = func(t reflect.Type, prefix string, index []int) {
		for i := 0; i < t.NumField(); i++ {
			sf := t.Field(i)
			idx := append(append([]int(nil), index...), i)
			if sf.Anonymous && sf.Type.Kind() == reflect.Struct {
				walk(sf.Type, prefix+sf.Name+".", idx)
				continue
			}
			policy, opts := parseCloneTag(sf.Tag.Get("clone"))
			out = append(out, clonePolicyField{path: prefix + sf.Name, index: idx, typ: sf.Type, policy: policy, opts: opts})
		}
	}
	walk(t, t.Name()+".", nil)
	return out
}

func isClonePolicyType(t reflect.Type) bool {
	for _, pt := range clonePolicyTypes {
		if pt == t {
			return true
		}
	}
	return false
}

// TestClonePolicyEveryFieldTagged fails on any field of a policy type that
// has no (or an unknown) clone policy, so a new Engine field cannot land
// without its author deciding how Clone treats it.
func TestClonePolicyEveryFieldTagged(t *testing.T) {
	counts := map[string]int{}
	var missing []string
	for _, pt := range clonePolicyTypes {
		for _, f := range clonePolicyFieldsOf(pt) {
			if !clonePolicies[f.policy] {
				got := "no clone tag"
				if f.policy != "" {
					got = "unknown clone policy " + `"` + f.policy + `"`
				}
				missing = append(missing, "rules."+f.path+" ("+f.typ.String()+"): "+got)
				continue
			}
			for o := range f.opts {
				if !cloneTagOptions[o] {
					missing = append(missing, "rules."+f.path+": unknown clone tag option "+`"`+o+`"`)
				}
			}
			counts[f.policy]++
		}
	}
	if len(missing) > 0 {
		t.Fatalf("%d field(s) without a clone policy:\n  %s\n\n%s", len(missing), strings.Join(missing, "\n  "), clonePolicyHelp)
	}
	t.Logf("clone policies: deep %d, share %d, reset %d, hook %d", counts["deep"], counts["share"], counts["reset"], counts["hook"])
}

// openField makes an unexported field readable and writable through
// reflection (test-only: the reflection never reaches a non-test path).
func openField(v reflect.Value) reflect.Value {
	if v.CanSet() || !v.CanAddr() {
		return v
	}
	return reflect.NewAt(v.Type(), unsafe.Pointer(v.UnsafeAddr())).Elem()
}

var engineType = reflect.TypeOf(Engine{})

// cloneFillZero gives a zero value a non-zero one, recursively to a bounded
// depth: a scalar becomes non-zero, a nil pointer, slice or map gets one
// filled element. Functions, interfaces and channels stay zero, and so does
// anything that would build another Engine. Non-zero values (real game
// state) are left alone.
func cloneFillZero(v reflect.Value, depth int) {
	if depth > 4 || !v.IsZero() {
		return
	}
	switch v.Kind() {
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(3)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		v.SetUint(3)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(1.5)
	case reflect.String:
		v.SetString("x")
	case reflect.Pointer:
		if v.Type().Elem() == engineType {
			return
		}
		n := reflect.New(v.Type().Elem())
		cloneFillZero(n.Elem(), depth+1)
		v.Set(n)
	case reflect.Slice:
		s := reflect.MakeSlice(v.Type(), 1, 1)
		cloneFillZero(openField(s.Index(0)), depth+1)
		v.Set(s)
	case reflect.Map:
		m := reflect.MakeMap(v.Type())
		k := reflect.New(v.Type().Key()).Elem()
		cloneFillZero(k, depth+1)
		val := reflect.New(v.Type().Elem()).Elem()
		cloneFillZero(val, depth+1)
		m.SetMapIndex(k, val)
		v.Set(m)
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			cloneFillZero(openField(v.Index(i)), depth+1)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			cloneFillZero(openField(v.Field(i)), depth+1)
		}
	}
}

// cloneRefSpan is the memory a reference-kind value points at (ok false when
// it points at nothing that could be shared).
func cloneRefSpan(v reflect.Value) (lo, hi uintptr, ok bool) {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return 0, 0, false
		}
		sz := v.Type().Elem().Size()
		if sz == 0 {
			return 0, 0, false
		}
		return v.Pointer(), v.Pointer() + sz, true
	case reflect.Map:
		if v.IsNil() {
			return 0, 0, false
		}
		return v.Pointer(), v.Pointer() + 1, true
	case reflect.Slice:
		sz := v.Type().Elem().Size()
		if v.Cap() == 0 || sz == 0 {
			return 0, 0, false
		}
		return v.Pointer(), v.Pointer() + uintptr(v.Cap())*sz, true
	}
	return 0, 0, false
}

func isCloneRef(k reflect.Kind) bool {
	return k == reflect.Pointer || k == reflect.Map || k == reflect.Slice
}

// cloneAliases reports whether two reference values share storage.
func cloneAliases(a, b reflect.Value) bool {
	alo, ahi, aok := cloneRefSpan(a)
	blo, bhi, bok := cloneRefSpan(b)
	return aok && bok && alo < bhi && blo < ahi
}

// clonePolicyTransformed names the deep fields whose clone value
// legitimately differs from the original's: cloneWith re-keys or narrows them
// rather than copying them verbatim. Each is still the clone's own (the alias
// check applies); only the equality check is waived. A field tagged `rekey`
// is transformed by its tag and needs no entry here.
var clonePolicyTransformed = map[string]string{
	"L":                              "CloneIntoFrom marks the clone's log forked: it shares the original's append-only event prefix (capped at len)",
	"engineLayerCaches.sbaQuiet":     "carried only when provably quiet (sbaQuietCarry), re-recorded at the clone's log head",
	"engineScratch.lossProof":        "forClone-style copy: the registry header (contPtr, contLen) is re-checked once on the clone",
	"engineTriggerBatches.trigGrant": "forClone: the registry header (contPtr, contLen) is re-checked once on the clone",
	"engineScratch.actIndex":         "copyActivationIndex: the folds are copied and owner re-bound to the clone",
}

type clonePolicyChecker struct {
	t         *testing.T
	zeroSpare bool // a plain Clone: reset scalars must come back zero
	nonNilRef map[string]bool
}

// check compares one value of a policy type (orig, clone: addressable struct
// values of type typ) field by field under the fields' tags.
func (k *clonePolicyChecker) check(prefix string, orig, clone reflect.Value, typ reflect.Type, depth int) {
	for _, f := range clonePolicyFieldsOf(typ) {
		rel := strings.TrimPrefix(f.path, typ.Name()+".")
		path := prefix + rel
		ov := openField(orig.FieldByIndex(f.index))
		cv := openField(clone.FieldByIndex(f.index))
		k.checkField(path, rel, f, ov, cv, depth)
	}
}

func (k *clonePolicyChecker) checkField(path, rel string, f clonePolicyField, ov, cv reflect.Value, depth int) {
	t := k.t
	kind := f.typ.Kind()
	if isCloneRef(kind) && !ov.IsZero() {
		k.nonNilRef[f.policy] = true
	}
	switch f.policy {
	case "hook":
		if !cv.IsZero() {
			t.Errorf("%s is clone:\"hook\" but the clone carries it; a harness observer must stay nil on a clone", path)
		}
	case "share":
		if cv.IsZero() {
			return
		}
		if isCloneRef(kind) {
			olo, _, ook := cloneRefSpan(ov)
			clo, _, cok := cloneRefSpan(cv)
			if ook && cok && olo != clo || ook != cok {
				t.Errorf("%s is clone:\"share\" but the clone holds different storage; a share field must alias the original's reference (or be dropped), so either make clone.go copy the reference or tag the field deep", path)
			}
			return
		}
		if d := cloneFirstDiff(ov, cv, path, map[[2]uintptr]bool{}); d != "" {
			t.Errorf("%s is clone:\"share\" but the clone's value differs from the original's: %s", path, d)
		}
	case "reset":
		if isCloneRef(kind) {
			if cloneAliases(ov, cv) {
				t.Errorf("%s is clone:\"reset\" but the clone aliases the original's storage; a reset field starts zero or with storage of its own", path)
			}
			return
		}
		if kind == reflect.Struct {
			k.checkNoImmediateAlias(path, "reset", ov, cv)
			return
		}
		if k.zeroSpare && !cv.IsZero() {
			t.Errorf("%s is clone:\"reset\" but a plain Clone carries %v (original %v); a reset scalar starts zero on the clone, or the field is deep", path, cv.Interface(), ov.Interface())
		}
	case "deep":
		switch {
		case kind == reflect.Struct && isClonePolicyType(f.typ):
			k.check(path+".", ov, cv, f.typ, depth+1)
			return
		case kind == reflect.Pointer && isClonePolicyType(f.typ.Elem()):
			if cloneAliases(ov, cv) {
				t.Errorf("%s is clone:\"deep\" but the clone shares the original's %s", path, f.typ)
				return
			}
			if ov.IsNil() != cv.IsNil() {
				t.Errorf("%s is clone:\"deep\" but nil-ness differs (original nil %v, clone nil %v)", path, ov.IsNil(), cv.IsNil())
				return
			}
			if !ov.IsNil() && depth < 6 {
				k.check(path+".", ov.Elem(), cv.Elem(), f.typ.Elem(), depth+1)
			}
			return
		}
		if isCloneRef(kind) && cloneAliases(ov, cv) {
			t.Errorf("%s is clone:\"deep\" but the clone aliases the original's storage; clone.go must re-allocate it (or the field is share)", path)
		}
		if kind == reflect.Struct {
			k.checkNoImmediateAlias(path, "deep", ov, cv)
		}
		if _, ok := clonePolicyTransformed[rel]; ok && depth == 0 {
			return
		}
		if _, ok := f.opts["rekey"]; ok {
			return
		}
		if d := cloneFirstDiff(ov, cv, path, map[[2]uintptr]bool{}); d != "" {
			t.Errorf("%s is clone:\"deep\" but the clone's value differs from the original's (an omitted copy line in clone.go?): %s", path, d)
		}
	}
}

// cloneFirstDiff names the first place two values differ (a readable
// stand-in for reflect.DeepEqual); seen guards pointer cycles. Unlike
// DeepEqual it treats a nil and an empty slice or map as equal (a copy
// written as append(T(nil), src...) turns an empty source into nil) and
// compares functions by nil-ness only.
//
// The path is rendered only when a difference is found: the clone-fidelity
// fuzz compares whole engines at every intent, and building the path string
// at every step of an equal walk was most of that test's allocation
// (3.8 GB of 6.6 GB in TestCloneFidelityShort, 2026-10-05).
func cloneFirstDiff(a, b reflect.Value, path string, seen map[[2]uintptr]bool) string {
	w := cloneDiffWalk{root: path, seen: seen}
	return w.diff(a, b)
}

// cloneDiffSeg is one step of a cloneFirstDiff path: a struct field, a
// slice/array index, a map key or a pointer dereference.
type cloneDiffSeg struct {
	kind  byte // '.' field (of typ, by index), '[' index, 'k' map key, '*' deref
	typ   reflect.Type
	index int
	key   reflect.Value
}

type cloneDiffWalk struct {
	root string
	segs []cloneDiffSeg
	seen map[[2]uintptr]bool
}

// path renders the current path exactly as the eager concatenation did.
func (w *cloneDiffWalk) path() string {
	s := w.root
	for _, g := range w.segs {
		switch g.kind {
		case '.':
			// The name is read here, not per step: Type.Field allocates.
			s = s + "." + g.typ.Field(g.index).Name
		case '[':
			s = fmt.Sprintf("%s[%d]", s, g.index)
		case 'k':
			s = fmt.Sprintf("%s[%v]", s, g.key)
		case '*':
			s = "(*" + s + ")"
		}
	}
	return s
}

func (w *cloneDiffWalk) push(g cloneDiffSeg) { w.segs = append(w.segs, g) }
func (w *cloneDiffWalk) pop()                { w.segs = w.segs[:len(w.segs)-1] }

func (w *cloneDiffWalk) diff(a, b reflect.Value) string {
	if a.Kind() != b.Kind() {
		return w.path() + ": kind differs"
	}
	switch a.Kind() {
	case reflect.Bool:
		if a.Bool() != b.Bool() {
			return fmt.Sprintf("%s: %v != %v", w.path(), a.Bool(), b.Bool())
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if a.Int() != b.Int() {
			return fmt.Sprintf("%s: %d != %d", w.path(), a.Int(), b.Int())
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if a.Uint() != b.Uint() {
			return fmt.Sprintf("%s: %d != %d", w.path(), a.Uint(), b.Uint())
		}
	case reflect.Float32, reflect.Float64:
		if a.Float() != b.Float() {
			return fmt.Sprintf("%s: %v != %v", w.path(), a.Float(), b.Float())
		}
	case reflect.String:
		if a.String() != b.String() {
			return fmt.Sprintf("%s: %q != %q", w.path(), a.String(), b.String())
		}
	case reflect.Func, reflect.Chan, reflect.UnsafePointer:
		if a.IsNil() != b.IsNil() {
			return w.path() + ": nil-ness differs"
		}
	case reflect.Interface:
		if a.IsNil() || b.IsNil() {
			if a.IsNil() != b.IsNil() {
				return w.path() + ": nil-ness differs"
			}
			return ""
		}
		return w.diff(a.Elem(), b.Elem())
	case reflect.Pointer:
		if a.IsNil() || b.IsNil() {
			if a.IsNil() != b.IsNil() {
				return fmt.Sprintf("%s: nil-ness differs (original nil %v, clone nil %v)", w.path(), a.IsNil(), b.IsNil())
			}
			return ""
		}
		key := [2]uintptr{a.Pointer(), b.Pointer()}
		if a.Pointer() == b.Pointer() || w.seen[key] {
			return ""
		}
		w.seen[key] = true
		w.push(cloneDiffSeg{kind: '*'})
		d := w.diff(a.Elem(), b.Elem())
		w.pop()
		return d
	case reflect.Slice:
		if a.Len() != b.Len() {
			return fmt.Sprintf("%s: len %d != %d", w.path(), a.Len(), b.Len())
		}
		return w.elems(a, b)
	case reflect.Array:
		return w.elems(a, b)
	case reflect.Map:
		if a.Len() != b.Len() {
			return fmt.Sprintf("%s: len %d != %d", w.path(), a.Len(), b.Len())
		}
		it := a.MapRange()
		for it.Next() {
			bv := b.MapIndex(it.Key())
			if !bv.IsValid() {
				return fmt.Sprintf("%s: key %v missing on the clone", w.path(), it.Key())
			}
			w.push(cloneDiffSeg{kind: 'k', key: it.Key()})
			d := w.diff(it.Value(), bv)
			w.pop()
			if d != "" {
				return d
			}
		}
	case reflect.Struct:
		t := a.Type()
		for i := 0; i < a.NumField(); i++ {
			w.push(cloneDiffSeg{kind: '.', typ: t, index: i})
			d := w.diff(a.Field(i), b.Field(i))
			w.pop()
			if d != "" {
				return d
			}
		}
	}
	return ""
}

// elems compares a slice's or array's elements pairwise.
func (w *cloneDiffWalk) elems(a, b reflect.Value) string {
	for i := 0; i < a.Len(); i++ {
		w.push(cloneDiffSeg{kind: '[', index: i})
		d := w.diff(a.Index(i), b.Index(i))
		w.pop()
		if d != "" {
			return d
		}
	}
	return ""
}

var cardsPkgPath = reflect.TypeOf(cards.SA{}).PkgPath()

// clonePointsIntoCorpus reports a pointer into the immutable compiled corpus
// (*cards.SA, *cards.Face, ...), which every clone shares by design.
func clonePointsIntoCorpus(t reflect.Type) bool {
	return t.Kind() == reflect.Pointer && t.Elem().PkgPath() == cardsPkgPath
}

// checkNoImmediateAlias fails when a struct-kind field's own slice, map or
// pointer members alias the original's (one level: deeper members follow
// their own copier's rules).
func (k *clonePolicyChecker) checkNoImmediateAlias(path, policy string, ov, cv reflect.Value) {
	for i := 0; i < ov.NumField(); i++ {
		sf := ov.Type().Field(i)
		if !isCloneRef(sf.Type.Kind()) || clonePointsIntoCorpus(sf.Type) {
			continue
		}
		if cloneAliases(openField(ov.Field(i)), openField(cv.Field(i))) {
			k.t.Errorf("%s is clone:%q but its member %s aliases the original's storage", path, policy, sf.Name)
		}
	}
}

// cloneFillPolicyFields fills every zero field of a policy-type value (and,
// through deep pointers and values of policy types, theirs) so that a clone
// line clone.go forgot or made shallow has something to miss or alias.
func cloneFillPolicyFields(v reflect.Value, typ reflect.Type, depth int) {
	for _, f := range clonePolicyFieldsOf(typ) {
		fv := openField(v.FieldByIndex(f.index))
		switch {
		case f.typ.Kind() == reflect.Struct && isClonePolicyType(f.typ):
			cloneFillPolicyFields(fv, f.typ, depth+1)
		case f.typ.Kind() == reflect.Pointer && isClonePolicyType(f.typ.Elem()):
			if depth > 3 {
				continue
			}
			if fv.IsNil() {
				fv.Set(reflect.New(f.typ.Elem()))
			}
			cloneFillPolicyFields(fv.Elem(), f.typ.Elem(), depth+1)
		default:
			cloneFillZero(fv, 0)
		}
	}
}
