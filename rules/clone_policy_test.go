package rules

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
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
	"Pick the policy that matches what rules/clone.go does with the field, and add the matching copy line there when it is deep."

// clonePolicyTypes are the types whose every field must carry a clone policy:
// the Engine (through its embedded clusters), the resume chain cloneResume
// copies and its repeat cursor, the continuation frame the chain is built
// from, and the value types cloneWith deep-copies field by field (the pending
// cast, the combat, mulligan and opening-hand rounds, and a parked
// ExchangeLife transaction).
//
// contFrame never crosses a clone boundary (Engine.contChain is reset: it is
// empty at every intent boundary); its tags record the policy each field
// takes once buildContinuationChain turns the frame into a resumePoint, so
// the two parallel structures stay in step. The behavioural test below does
// not exercise it.
var clonePolicyTypes = []reflect.Type{
	reflect.TypeOf(Engine{}),
	reflect.TypeOf(resumePoint{}),
	reflect.TypeOf(contFrame{}),
	reflect.TypeOf(repeatCursor{}),
	reflect.TypeOf(pendingCast{}),
	reflect.TypeOf(combatRound{}),
	reflect.TypeOf(mulliganRound{}),
	reflect.TypeOf(openingRound{}),
	reflect.TypeOf(lifeExchangeTransaction{}),
}

var clonePolicies = map[string]bool{"deep": true, "share": true, "reset": true, "hook": true}

// clonePolicyField is one leaf field of a policy type: embedded anonymous
// structs are flattened, so path names the cluster the field lives in.
type clonePolicyField struct {
	path   string
	index  []int
	typ    reflect.Type
	policy string
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
			out = append(out, clonePolicyField{path: prefix + sf.Name, index: idx, typ: sf.Type, policy: sf.Tag.Get("clone")})
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
// check applies); only the equality check is waived.
var clonePolicyTransformed = map[string]string{
	"L":                                     "CloneIntoFrom marks the clone's log forked: it shares the original's append-only event prefix (capped at len)",
	"engineLayerCaches.staticVersion":       "re-keyed onto the clone's own zero continuousVersion",
	"engineLayerCaches.sbaQuiet":            "carried only when provably quiet (sbaQuietCarry), re-recorded at the clone's log head",
	"engineDerivedTables.renameVersion":     "re-keyed onto the clone's continuousVersion (rekeyVersion)",
	"engineDerivedTables.typesVersion":      "re-keyed onto the clone's continuousVersion (rekeyVersion)",
	"engineDerivedTables.typesProbeVersion": "re-keyed onto the clone's own zero continuousVersion",
	"engineScratch.atkOffersVer":            "re-keyed onto the clone's own zero continuousVersion",
	"engineScratch.lossProof":               "forClone-style copy: the registry header (contPtr, contLen) is re-checked once on the clone",
	"engineTriggerBatches.trigGrant":        "forClone: the registry header (contPtr, contLen) is re-checked once on the clone",
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
func cloneFirstDiff(a, b reflect.Value, path string, seen map[[2]uintptr]bool) string {
	if a.Kind() != b.Kind() {
		return path + ": kind differs"
	}
	switch a.Kind() {
	case reflect.Bool:
		if a.Bool() != b.Bool() {
			return fmt.Sprintf("%s: %v != %v", path, a.Bool(), b.Bool())
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if a.Int() != b.Int() {
			return fmt.Sprintf("%s: %d != %d", path, a.Int(), b.Int())
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if a.Uint() != b.Uint() {
			return fmt.Sprintf("%s: %d != %d", path, a.Uint(), b.Uint())
		}
	case reflect.Float32, reflect.Float64:
		if a.Float() != b.Float() {
			return fmt.Sprintf("%s: %v != %v", path, a.Float(), b.Float())
		}
	case reflect.String:
		if a.String() != b.String() {
			return fmt.Sprintf("%s: %q != %q", path, a.String(), b.String())
		}
	case reflect.Func, reflect.Chan, reflect.UnsafePointer:
		if a.IsNil() != b.IsNil() {
			return path + ": nil-ness differs"
		}
	case reflect.Interface:
		if a.IsNil() || b.IsNil() {
			if a.IsNil() != b.IsNil() {
				return path + ": nil-ness differs"
			}
			return ""
		}
		return cloneFirstDiff(a.Elem(), b.Elem(), path, seen)
	case reflect.Pointer:
		if a.IsNil() || b.IsNil() {
			if a.IsNil() != b.IsNil() {
				return fmt.Sprintf("%s: nil-ness differs (original nil %v, clone nil %v)", path, a.IsNil(), b.IsNil())
			}
			return ""
		}
		key := [2]uintptr{a.Pointer(), b.Pointer()}
		if a.Pointer() == b.Pointer() || seen[key] {
			return ""
		}
		seen[key] = true
		return cloneFirstDiff(a.Elem(), b.Elem(), "(*"+path+")", seen)
	case reflect.Slice:
		if a.Len() != b.Len() {
			return fmt.Sprintf("%s: len %d != %d", path, a.Len(), b.Len())
		}
		for i := 0; i < a.Len(); i++ {
			if d := cloneFirstDiff(a.Index(i), b.Index(i), fmt.Sprintf("%s[%d]", path, i), seen); d != "" {
				return d
			}
		}
	case reflect.Array:
		for i := 0; i < a.Len(); i++ {
			if d := cloneFirstDiff(a.Index(i), b.Index(i), fmt.Sprintf("%s[%d]", path, i), seen); d != "" {
				return d
			}
		}
	case reflect.Map:
		if a.Len() != b.Len() {
			return fmt.Sprintf("%s: len %d != %d", path, a.Len(), b.Len())
		}
		it := a.MapRange()
		for it.Next() {
			bv := b.MapIndex(it.Key())
			if !bv.IsValid() {
				return fmt.Sprintf("%s: key %v missing on the clone", path, it.Key())
			}
			if d := cloneFirstDiff(it.Value(), bv, fmt.Sprintf("%s[%v]", path, it.Key()), seen); d != "" {
				return d
			}
		}
	case reflect.Struct:
		for i := 0; i < a.NumField(); i++ {
			if d := cloneFirstDiff(a.Field(i), b.Field(i), path+"."+a.Type().Field(i).Name, seen); d != "" {
				return d
			}
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

// TestClonePolicyHoldsOnAMidGameEngine checks the tags against what Clone
// actually does, on a real mid-game engine whose otherwise-empty fields are
// filled by reflection first:
//
//   - deep:  the clone's value equals the original's (an omitted copy line
//     shows up as a zero), and a slice, map or pointer field -- or a struct
//     field's own slice, map and pointer members -- holds storage of its own;
//     fields of the policy types are checked recursively under their own
//     tags.
//   - share: the clone either drops the field or holds the very same
//     reference (or an equal value).
//   - reset: the clone never aliases the original's storage, and on a plain
//     Clone a reset scalar comes back zero.
//   - hook:  the clone's field is zero.
//
// The run is repeated on CloneInto with a Spare released by an earlier
// clone, so recycled storage is held to the same no-alias rule.
//
// Limits: the fill is one element deep for slices and maps and bounded in
// depth (four levels; resume chains three links), functions and interfaces
// are never filled, and a deep field whose type is not a policy type is
// alias-checked only at its own level (plus one level of members for
// structs) -- what it points at follows its copier's own rules, checked by
// equality but not for aliasing. Fields cloneWith copies only under a
// condition (a cache built under the current registry, an open damage
// batch, a pending cast) are covered only as far as the filled fixture meets
// that condition.
func TestClonePolicyHoldsOnAMidGameEngine(t *testing.T) {
	names, decks := testutil.SampleDecks(t, 2)
	e := New(Config{Seed: 3, Names: names, Decks: decks})
	e.Advance()
	drive(t, e, newTestBot(3), 30)
	seedInternalQueues(t, e)
	cloneFillPolicyFields(reflect.ValueOf(e).Elem(), engineType, 0)
	// Make the version-keyed caches current, so cloneWith carries them (a
	// stale key would just drop them and leave the copy lines unexercised).
	e.staticVersion, e.atkOffersVer = e.continuousVersion, e.continuousVersion
	e.ManaAbilityHook = func(state.PlayerID, state.ObjID, *cards.SA) {}

	k := &clonePolicyChecker{t: t, zeroSpare: true, nonNilRef: map[string]bool{}}
	c := e.Clone()
	k.check("Engine.", reflect.ValueOf(e).Elem(), reflect.ValueOf(c).Elem(), engineType, 0)

	sp := e.Clone().Release()
	k2 := &clonePolicyChecker{t: t, nonNilRef: map[string]bool{}}
	c2 := e.CloneInto(&sp)
	k2.check("Engine(CloneInto).", reflect.ValueOf(e).Elem(), reflect.ValueOf(c2).Elem(), engineType, 0)
	for _, p := range []string{"deep", "share", "reset", "hook"} {
		if !k.nonNilRef[p] {
			t.Errorf("the filled fixture holds no non-nil %s reference field: the fill did not run", p)
		}
	}
}
