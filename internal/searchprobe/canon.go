package searchprobe

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"unicode/utf8"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
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
//
// The encoder itself is generated, typed code (canon_gen.go, no reflection):
// canon_gen_test.go derives it from the reflective plan the oracle in
// canon_reflect_test.go walks, TestCanonGenIsCurrent fails on any type
// change the generated file does not reflect, and TestCanonGenIsReflective
// holds its bytes to the oracle's on real boards.

// canonEncoder appends canonical encodings to buf. keys is map-key scratch.
type canonEncoder struct {
	buf     []byte
	strKeys []string
	intKeys []int64
	uKeys   []uint64
}

func (e *canonEncoder) appendString(s string) { e.buf = appendCanonString(e.buf, s) }

// appendFloat appends f's bits. encoding/json writes the shortest text that
// round-trips the value at its own width, so equal text is equal value (and
// -0 is "-0"); it refuses NaN and infinities, and so does this.
func (e *canonEncoder) appendFloat(f float64) error {
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return fmt.Errorf("canonical encoding: unsupported value %v", f)
	}
	e.buf = binary.LittleEndian.AppendUint64(e.buf, math.Float64bits(f))
	return nil
}

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

// CompareActionsJSON orders two action lists exactly as bytes.Compare
// orders their json.Marshal encodings, without encoding them: nil is
// "null", above every list ('n' > '['); lists compare element by element
// (actionJSONCompare: a complete object is never a proper prefix of
// another, so the first differing element decides); the empty list sorts
// before every other ("[]": ']' < '{'), and a non-empty list that is a
// proper prefix of another sorts AFTER it (']' > ','). internal/azmcts
// orders candidates by it, the order its keys had while they were JSON.
func CompareActionsJSON(a, b []Action) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return 1
	case b == nil:
		return -1
	}
	for i := 0; i < len(a) && i < len(b); i++ {
		if c := actionJSONCompare(&a[i], &b[i]); c != 0 {
			return c
		}
	}
	switch {
	case len(a) == len(b):
		return 0
	case len(a) == 0:
		return -1
	case len(b) == 0:
		return 1
	case len(a) < len(b):
		return 1
	}
	return -1
}

// ParseActionsKey is AppendActionsKey's inverse: the action list a key
// encodes. A string that was not valid UTF-8 comes back as encoding/json
// decodes its JSON token (each invalid byte U+FFFD), as when keys were JSON.
// internal/azmcts plays a key on another engine with it (IntentForKey).
func ParseActionsKey(k []byte) ([]Action, error) {
	r := keyReader{b: k}
	switch r.byte() {
	case 0:
		if r.err == nil && len(r.b) != 0 {
			return nil, fmt.Errorf("searchprobe: %d trailing bytes after a nil action key", len(r.b))
		}
		return nil, r.err
	case 1:
	default:
		if r.err == nil {
			r.err = fmt.Errorf("searchprobe: not an action key")
		}
		return nil, r.err
	}
	n := r.uvarint()
	if r.err == nil && n > uint64(len(r.b)) {
		r.err = fmt.Errorf("searchprobe: action key claims %d actions in %d bytes", n, len(r.b))
	}
	if r.err != nil {
		return nil, r.err
	}
	acts := make([]Action, n)
	for i := range acts {
		a := &acts[i]
		a.Decision = decision.Kind(r.str())
		a.Source = uint32(r.uvarint())
		a.Kind = r.str()
		a.Obj = uint32(r.uvarint())
		a.Attacker = uint32(r.uvarint())
		a.Player = state.PlayerID(r.uvarint())
		a.Ability = int(r.varint())
		a.AltCostIndex = int(r.varint())
		a.Amount = int(r.varint())
		a.Mode = r.str()
		a.SVar = r.str()
		a.Value = r.str()
	}
	if r.err == nil && len(r.b) != 0 {
		r.err = fmt.Errorf("searchprobe: %d trailing bytes after an action key", len(r.b))
	}
	if r.err != nil {
		return nil, r.err
	}
	return acts, nil
}

// keyReader reads AppendActionsKey's encoding; the first error sticks.
type keyReader struct {
	b   []byte
	err error
}

func (r *keyReader) fail() { r.err, r.b = fmt.Errorf("searchprobe: truncated action key"), nil }

func (r *keyReader) byte() byte {
	if r.err != nil || len(r.b) == 0 {
		if r.err == nil {
			r.fail()
		}
		return 0
	}
	c := r.b[0]
	r.b = r.b[1:]
	return c
}

func (r *keyReader) uvarint() uint64 {
	if r.err != nil {
		return 0
	}
	v, n := binary.Uvarint(r.b)
	if n <= 0 {
		r.fail()
		return 0
	}
	r.b = r.b[n:]
	return v
}

func (r *keyReader) varint() int64 {
	if r.err != nil {
		return 0
	}
	v, n := binary.Varint(r.b)
	if n <= 0 {
		r.fail()
		return 0
	}
	r.b = r.b[n:]
	return v
}

// str reads appendCanonString's encoding.
func (r *keyReader) str() string {
	tag := r.byte()
	n := r.uvarint()
	if r.err != nil {
		return ""
	}
	if n > uint64(len(r.b)) {
		r.fail()
		return ""
	}
	raw := r.b[:n]
	r.b = r.b[n:]
	switch tag {
	case 0:
		return string(raw)
	case 1:
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			r.err, r.b = fmt.Errorf("searchprobe: action key string token: %v", err), nil
		}
		return s
	}
	r.err, r.b = fmt.Errorf("searchprobe: action key string tag %d", tag), nil
	return ""
}
