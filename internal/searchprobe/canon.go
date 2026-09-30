package searchprobe

import (
	"encoding"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/adams-shaun/gorge/view"
)

// The canonical encoding is a compact binary serialisation of a value whose
// equality is exactly the equality of the value's encoding/json encoding:
// two values of one type encode to the same canonical bytes if and only if
// json.Marshal would encode them to the same JSON. It exists so the search
// loop can compare observed boards (and key semantic actions) without ever
// producing JSON text, while every comparison still answers what the JSON
// comparison it replaces answered.
//
// It mirrors exactly the parts of encoding/json that decide equality:
//
//   - a struct's fields are its exported, non-embedded fields minus those
//     tagged "-", in declaration order (the type is the same on both sides,
//     so field names carry no information);
//   - an omitempty field whose value encoding/json calls empty (false, 0,
//     "", a nil pointer, a zero-length slice, map or array) is absent -- so
//     a nil and an empty omitempty slice are the same, as in JSON -- and any
//     other field is present;
//   - a nil slice, map or pointer ("null") differs from an empty one ("[]",
//     "{}") and from a present value;
//   - a map's entries are in a canonical key order;
//   - a valid UTF-8 string is its bytes (JSON's escaping is injective on
//     them) and an invalid one its JSON token, since encoding/json writes
//     every invalid byte as the same \ufffd escape (appendCanonString);
//   - integers are their values (JSON's decimal text is injective too), and
//     floats their bits (JSON writes the shortest round-tripping text, so
//     equal text is an equal value, -0 included; NaN and infinities are an
//     error in both).
//
// Anything encoding/json treats specially and this encoder does not model --
// a type with its own MarshalJSON/MarshalText, an interface, a
// channel or func, an embedded field, two fields with one JSON name, the
// omitzero option -- is planned as "unsupported": encoding a non-empty value
// of it is an error, never a silent disagreement with JSON.
// TestCanonicalPlanOfTheViewIsSupported requires view.View to reach no
// unsupported node, so a view change that would need one fails there first.

type canonKind uint8

const (
	canonUnsupported canonKind = iota
	canonBool
	canonInt
	canonUint
	canonFloat
	canonString
	canonSlice
	canonArray
	canonMap
	canonPointer
	canonStruct
)

type canonPlan struct {
	kind   canonKind
	typ    reflect.Type
	elem   *canonPlan // slice, array, pointer: element; map: value
	key    canonKind  // map: canonString or canonInt/canonUint
	fields []canonField
	why    string // unsupported: the reason
}

type canonField struct {
	index     int
	omitEmpty bool
	plan      *canonPlan
}

var (
	jsonMarshaler = reflect.TypeFor[json.Marshaler]()
	textMarshaler = reflect.TypeFor[encoding.TextMarshaler]()
	canonPlans    sync.Map // reflect.Type -> *canonPlan, built once per type
)

// canonPlanOf returns t's plan, building it (and every type it reaches) on
// first use.
func canonPlanOf(t reflect.Type) *canonPlan {
	if p, ok := canonPlans.Load(t); ok {
		return p.(*canonPlan)
	}
	building := make(map[reflect.Type]*canonPlan)
	p := buildCanonPlan(t, building)
	actual, _ := canonPlans.LoadOrStore(t, p)
	return actual.(*canonPlan)
}

func buildCanonPlan(t reflect.Type, building map[reflect.Type]*canonPlan) *canonPlan {
	if p := building[t]; p != nil {
		return p
	}
	p := &canonPlan{typ: t}
	building[t] = p
	if t.Implements(jsonMarshaler) || reflect.PointerTo(t).Implements(jsonMarshaler) {
		p.why = "implements json.Marshaler"
		return p
	}
	if t.Implements(textMarshaler) || reflect.PointerTo(t).Implements(textMarshaler) {
		p.why = "implements encoding.TextMarshaler"
		return p
	}
	switch t.Kind() {
	case reflect.Bool:
		p.kind = canonBool
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		p.kind = canonInt
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		p.kind = canonUint
	case reflect.Float32, reflect.Float64:
		p.kind = canonFloat
	case reflect.String:
		p.kind = canonString
	case reflect.Slice:
		p.kind, p.elem = canonSlice, buildCanonPlan(t.Elem(), building)
	case reflect.Array:
		p.kind, p.elem = canonArray, buildCanonPlan(t.Elem(), building)
	case reflect.Pointer:
		p.kind, p.elem = canonPointer, buildCanonPlan(t.Elem(), building)
	case reflect.Map:
		kt := t.Key()
		switch {
		case kt.Implements(textMarshaler) || reflect.PointerTo(kt).Implements(textMarshaler):
			p.why = "map key implements encoding.TextMarshaler"
			return p
		case kt.Kind() == reflect.String:
			p.key = canonString
		case kt.Kind() >= reflect.Int && kt.Kind() <= reflect.Int64:
			p.key = canonInt
		case kt.Kind() >= reflect.Uint && kt.Kind() <= reflect.Uintptr:
			p.key = canonUint
		default:
			p.why = "map key kind " + kt.Kind().String()
			return p
		}
		p.kind, p.elem = canonMap, buildCanonPlan(t.Elem(), building)
	case reflect.Struct:
		names := make(map[string]bool)
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.Anonymous {
				p.why = "embedded field " + f.Name
				return p
			}
			if !f.IsExported() {
				continue
			}
			tag := f.Tag.Get("json")
			if tag == "-" {
				continue
			}
			name, opts, _ := strings.Cut(tag, ",")
			if name == "" {
				name = f.Name
			}
			if names[name] {
				p.why = "two fields named " + name
				return p
			}
			names[name] = true
			omit := false
			for _, o := range strings.Split(opts, ",") {
				switch o {
				case "omitempty":
					omit = true
				case "omitzero":
					p.why = "omitzero field " + f.Name
					return p
				}
			}
			p.fields = append(p.fields, canonField{index: i, omitEmpty: omit, plan: buildCanonPlan(f.Type, building)})
		}
		p.kind = canonStruct
	default:
		p.why = "kind " + t.Kind().String()
	}
	return p
}

// unsupported lists every unsupported node reachable from p, as
// "<path>: <type> <why>".
func (p *canonPlan) unsupported() []string {
	var out []string
	seen := make(map[*canonPlan]bool)
	var walk func(p *canonPlan, path string)
	walk = func(p *canonPlan, path string) {
		if seen[p] {
			return
		}
		seen[p] = true
		switch p.kind {
		case canonUnsupported:
			out = append(out, fmt.Sprintf("%s: %s %s", path, p.typ, p.why))
		case canonSlice, canonArray, canonPointer, canonMap:
			walk(p.elem, path+"[]")
		case canonStruct:
			for _, f := range p.fields {
				walk(f.plan, path+"."+p.typ.Field(f.index).Name)
			}
		}
	}
	walk(p, p.typ.String())
	return out
}

// canonEncoder appends canonical encodings to buf. keys is map-key scratch.
type canonEncoder struct {
	buf     []byte
	strKeys []string
	intKeys []int64
	uKeys   []uint64
}

// encode appends v's canonical encoding under plan p.
func (e *canonEncoder) encode(v reflect.Value, p *canonPlan) error {
	switch p.kind {
	case canonBool:
		if v.Bool() {
			e.buf = append(e.buf, 1)
		} else {
			e.buf = append(e.buf, 0)
		}
	case canonInt:
		e.buf = binary.AppendVarint(e.buf, v.Int())
	case canonUint:
		e.buf = binary.AppendUvarint(e.buf, v.Uint())
	case canonFloat:
		// encoding/json writes the shortest text that round-trips the value
		// at its own width, so equal text is equal value (and -0 is "-0");
		// it refuses NaN and infinities, and so does this.
		f := v.Float()
		if math.IsInf(f, 0) || math.IsNaN(f) {
			return fmt.Errorf("canonical encoding: unsupported value %v", f)
		}
		e.buf = binary.LittleEndian.AppendUint64(e.buf, math.Float64bits(f))
	case canonString:
		e.appendString(v.String())
	case canonSlice:
		if v.IsNil() {
			e.buf = append(e.buf, 0)
			return nil
		}
		e.buf = append(e.buf, 1)
		fallthrough
	case canonArray:
		n := v.Len()
		e.buf = binary.AppendUvarint(e.buf, uint64(n))
		for i := 0; i < n; i++ {
			if err := e.encode(v.Index(i), p.elem); err != nil {
				return err
			}
		}
	case canonPointer:
		if v.IsNil() {
			e.buf = append(e.buf, 0)
			return nil
		}
		e.buf = append(e.buf, 1)
		return e.encode(v.Elem(), p.elem)
	case canonMap:
		return e.encodeMap(v, p)
	case canonStruct:
		for _, f := range p.fields {
			fv := v.Field(f.index)
			if f.omitEmpty {
				if canonEmpty(fv) {
					e.buf = append(e.buf, 0)
					continue
				}
				e.buf = append(e.buf, 1)
			}
			if err := e.encode(fv, f.plan); err != nil {
				return err
			}
		}
	default:
		if canonEmpty(v) && (v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer || v.Kind() == reflect.Map || v.Kind() == reflect.Slice) {
			// A nil value of an unmodelled type encodes to JSON null.
			e.buf = append(e.buf, 0)
			return nil
		}
		return fmt.Errorf("canonical encoding: %s: %s", p.typ, p.why)
	}
	return nil
}

func (e *canonEncoder) encodeMap(v reflect.Value, p *canonPlan) error {
	if v.IsNil() {
		e.buf = append(e.buf, 0)
		return nil
	}
	e.buf = append(e.buf, 1)
	e.buf = binary.AppendUvarint(e.buf, uint64(v.Len()))
	switch p.key {
	case canonString:
		// The common map[string]int32 shape (pools, counters) skips reflect
		// iteration entirely.
		if m, ok := v.Interface().(map[string]int32); ok {
			keys := e.strKeys[:0]
			for k := range m {
				keys = append(keys, k)
			}
			slices.Sort(keys)
			for _, k := range keys {
				e.appendString(k)
				e.buf = binary.AppendVarint(e.buf, int64(m[k]))
			}
			clear(keys)
			e.strKeys = keys[:0]
			return nil
		}
		// So does the archetype posterior's map[string]float64.
		if m, ok := v.Interface().(map[string]float64); ok {
			keys := e.strKeys[:0]
			for k := range m {
				keys = append(keys, k)
			}
			slices.Sort(keys)
			for _, k := range keys {
				e.appendString(k)
				f := m[k]
				if math.IsInf(f, 0) || math.IsNaN(f) {
					return fmt.Errorf("canonical encoding: unsupported value %v", f)
				}
				e.buf = binary.LittleEndian.AppendUint64(e.buf, math.Float64bits(f))
			}
			clear(keys)
			e.strKeys = keys[:0]
			return nil
		}
		keys := make([]reflect.Value, 0, v.Len())
		it := v.MapRange()
		for it.Next() {
			keys = append(keys, it.Key())
		}
		slices.SortFunc(keys, func(a, b reflect.Value) int { return strings.Compare(a.String(), b.String()) })
		for _, k := range keys {
			e.appendString(k.String())
			if err := e.encode(v.MapIndex(k), p.elem); err != nil {
				return err
			}
		}
	case canonInt:
		keys := make([]reflect.Value, 0, v.Len())
		it := v.MapRange()
		for it.Next() {
			keys = append(keys, it.Key())
		}
		slices.SortFunc(keys, func(a, b reflect.Value) int {
			switch x, y := a.Int(), b.Int(); {
			case x < y:
				return -1
			case x > y:
				return 1
			}
			return 0
		})
		for _, k := range keys {
			e.buf = binary.AppendVarint(e.buf, k.Int())
			if err := e.encode(v.MapIndex(k), p.elem); err != nil {
				return err
			}
		}
	case canonUint:
		keys := make([]reflect.Value, 0, v.Len())
		it := v.MapRange()
		for it.Next() {
			keys = append(keys, it.Key())
		}
		slices.SortFunc(keys, func(a, b reflect.Value) int {
			switch x, y := a.Uint(), b.Uint(); {
			case x < y:
				return -1
			case x > y:
				return 1
			}
			return 0
		})
		for _, k := range keys {
			e.buf = binary.AppendUvarint(e.buf, k.Uint())
			if err := e.encode(v.MapIndex(k), p.elem); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *canonEncoder) appendString(s string) { e.buf = appendCanonString(e.buf, s) }

// appendCanonString appends s so that two strings append the same bytes
// exactly when encoding/json encodes them identically. A valid UTF-8 string
// is its bytes (JSON escaping is injective on those). An invalid one is
// tagged and carries its JSON token itself: encoding/json writes each
// invalid byte as the escape \ufffd -- which no valid string's encoding
// contains, a literal U+FFFD being written raw -- so "\xff" and "\xfe" are
// one string to JSON and to this, and neither is "\ufffd".
func appendCanonString(dst []byte, s string) []byte {
	if utf8.ValidString(s) {
		dst = append(dst, 0)
		dst = binary.AppendUvarint(dst, uint64(len(s)))
		return append(dst, s...)
	}
	dst = append(dst, 1)
	lenAt := len(dst)
	dst = appendJSONString(dst, s)
	tok := len(dst) - lenAt
	// Length-prefix the token: shift it right by the prefix's width.
	var pre [binary.MaxVarintLen64]byte
	p := binary.PutUvarint(pre[:], uint64(tok))
	dst = append(dst, pre[:p]...)
	copy(dst[lenAt+p:], dst[lenAt:lenAt+tok])
	copy(dst[lenAt:], pre[:p])
	return dst
}

// AppendActionsKey appends a canonical key of acts to dst: two lists get
// the same key exactly when json.Marshal encodes them identically (the
// canonical encoding, specialised to []Action with no reflection).
// internal/azmcts keys its candidates with it.
func AppendActionsKey(dst []byte, acts []Action) []byte {
	if acts == nil {
		return append(dst, 0)
	}
	dst = append(dst, 1)
	dst = binary.AppendUvarint(dst, uint64(len(acts)))
	for i := range acts {
		a := &acts[i]
		dst = appendCanonString(dst, string(a.Decision))
		dst = binary.AppendUvarint(dst, uint64(a.Source))
		dst = appendCanonString(dst, a.Kind)
		dst = binary.AppendUvarint(dst, uint64(a.Obj))
		dst = binary.AppendUvarint(dst, uint64(a.Attacker))
		dst = binary.AppendUvarint(dst, uint64(a.Player))
		dst = binary.AppendVarint(dst, int64(a.Ability))
		dst = binary.AppendVarint(dst, int64(a.AltCostIndex))
		dst = binary.AppendVarint(dst, int64(a.Amount))
		dst = appendCanonString(dst, a.Mode)
		dst = appendCanonString(dst, a.SVar)
		dst = appendCanonString(dst, a.Value)
	}
	return dst
}

// canonEmpty is encoding/json's isEmptyValue.
func canonEmpty(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Interface, reflect.Pointer:
		return v.IsNil()
	}
	return false
}

// viewPlan is the canonical plan of view.View, built once.
var viewPlan = sync.OnceValue(func() *canonPlan { return canonPlanOf(reflect.TypeFor[view.View]()) })
