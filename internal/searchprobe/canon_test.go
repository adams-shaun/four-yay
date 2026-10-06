package searchprobe

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"testing"
	"unsafe"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/view"
)

// TestCanonicalPlanOfTheViewIsSupported: every type view.View reaches is one
// the canonical encoding models, so no observed board can hit an
// "unsupported" node. A view change that adds a MarshalJSON type, an
// interface, an embedded field or omitzero fails here.
func TestCanonicalPlanOfTheViewIsSupported(t *testing.T) {
	if u := viewPlan().unsupported(); len(u) != 0 {
		t.Fatalf("view.View reaches types the canonical encoding does not model:\n%v", u)
	}
}

// canonGameViews plays a real game and returns every remapped observation
// view (with and without its potential actions) it captures.
func canonGameViews(t *testing.T, frames int) []view.View {
	return canonGameViewsOf(t, frames, 30_000_002, "mono-red-prowess", "mono-blue-tempo")
}

func canonGameViewsOf(t *testing.T, frames int, seed uint64, names ...string) []view.View {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	decks := make([][]*cards.Card, len(names))
	for i, n := range names {
		var err error
		if decks[i], err = testutil.LoadRepoDeck(reg, n); err != nil {
			t.Fatal(err)
		}
	}
	e := rules.New(rules.Config{Seed: seed, Names: names, Decks: decks, Tokens: reg.Tokens})
	e.Advance()
	c := NewCollector(0)
	rngs := BotRandoms(11, len(names))
	board := botpolicy.NewBoard(len(names))
	var out []view.View
	pos := 0
	for i := 0; i < frames; i++ {
		v, _, err := c.observe(e, e.L.Events[pos:], true, new(frameArena))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, v)
		d := e.Pending()
		if d == nil {
			break
		}
		pos = len(e.L.Events)
		if err := e.Submit(botpolicy.Decide(botpolicy.BoardFromGameInto(e.G, e, d.Player, &board), d, rngs[d.Player])); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// canonOf is v's canonical encoding through encode, which every equality
// test here holds to JSON; it also requires encodeAt (the production path)
// to append the very same bytes, so each of those tests checks it too.
func canonOf(t *testing.T, v *view.View) []byte {
	t.Helper()
	var e, at canonEncoder
	if err := e.encode(reflect.ValueOf(v).Elem(), viewPlan()); err != nil {
		t.Fatal(err)
	}
	if err := at.encodeAt(unsafe.Pointer(v), viewPlan()); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(e.buf, at.buf) {
		t.Fatal("encodeAt and encode disagree on a view")
	}
	var gen canonEncoder
	if err := gen.encodeView(v); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(e.buf, gen.buf) {
		t.Fatal("the generated encoder and the reflective oracle disagree on a view")
	}
	return e.buf
}

// canonKinds reaches every kind and width the plan models, including the
// ones view.View does not use today.
type canonKinds struct {
	B    bool
	I8   int8
	I16  int16 `json:",omitempty"`
	I32  int32
	I    int `json:",omitempty"`
	U8   uint8
	U16  uint16 `json:",omitempty"`
	U    uint
	Up   uintptr `json:",omitempty"`
	F32  float32
	F64  float64 `json:",omitempty"`
	S    string  `json:",omitempty"`
	A    [3]int16
	A0   [0]int `json:",omitempty"`
	Sl   []canonKinds
	SlE  []string `json:",omitempty"`
	P    *canonKinds
	PE   *int `json:",omitempty"`
	M    map[string]int32
	MI   map[int8][]string `json:",omitempty"`
	MU   map[uint16]bool
	N    struct{ X, y int } `json:",omitempty"`
	Skip int                `json:"-"`
}

// TestCanonEncodeAtIsEncode: on values covering every modelled kind and
// width, nil against empty and omitempty zeros, encodeAt appends exactly
// encode's bytes.
func TestCanonEncodeAtIsEncode(t *testing.T) {
	plan := canonPlanOf(reflect.TypeFor[canonKinds]())
	if u := plan.unsupported(); len(u) != 0 {
		t.Fatal(u)
	}
	one := 1
	values := []canonKinds{
		{},
		{B: true, I8: -3, I16: 300, I32: -70000, I: 1 << 40, U8: 200, U16: 65000, U: 1 << 33, Up: 9, F32: -0.5, F64: math.Copysign(0, -1), S: "x\xff", A: [3]int16{1, -2, 3}},
		{Sl: []canonKinds{}, SlE: []string{}, M: map[string]int32{}, MI: map[int8][]string{}, MU: map[uint16]bool{}},
		{Sl: []canonKinds{{I8: 1}, {S: "é", P: &canonKinds{B: true}}}, SlE: []string{"", "a"}, PE: &one, M: map[string]int32{"b": 2, "a": 1}, MI: map[int8][]string{-1: nil, 2: {"z"}}, MU: map[uint16]bool{7: true, 3: false}},
		{N: struct{ X, y int }{X: 4}, Skip: 9, P: &canonKinds{Sl: []canonKinds{{}}}},
	}
	for i := range values {
		var e, at canonEncoder
		errE := e.encode(reflect.ValueOf(&values[i]).Elem(), plan)
		errAt := at.encodeAt(unsafe.Pointer(&values[i]), plan)
		if (errE == nil) != (errAt == nil) || !bytes.Equal(e.buf, at.buf) {
			t.Fatalf("value %d: encode %x (%v), encodeAt %x (%v)", i, e.buf, errE, at.buf, errAt)
		}
	}
	// A NaN is refused by both.
	bad := canonKinds{F32: float32(math.NaN())}
	var e, at canonEncoder
	if e.encode(reflect.ValueOf(&bad).Elem(), plan) == nil || at.encodeAt(unsafe.Pointer(&bad), plan) == nil {
		t.Fatal("a NaN encoded")
	}
}

func jsonOf(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestCanonicalEqualityIsJSONEqualityOnAGame compares every pair of a real
// game's observed boards both ways: canonical bytes equal exactly when the
// JSON is. The run must contain equal pairs, or the check is one-sided.
func TestCanonicalEqualityIsJSONEqualityOnAGame(t *testing.T) {
	views := canonGameViews(t, 220)
	js := make([][]byte, len(views))
	cs := make([][]byte, len(views))
	for i := range views {
		js[i] = jsonOf(t, &views[i])
		cs[i] = bytes.Clone(canonOf(t, &views[i]))
	}
	equal := 0
	for i := range views {
		for j := i + 1; j < len(views); j++ {
			je, ce := bytes.Equal(js[i], js[j]), bytes.Equal(cs[i], cs[j])
			if je != ce {
				t.Fatalf("frames %d and %d: JSON equal %v, canonical equal %v", i, j, je, ce)
			}
			if je {
				equal++
			}
		}
	}
	if equal == 0 {
		t.Fatal("no two boards of the game were equal: the check only saw differences")
	}
	t.Logf("%d boards, %d equal pairs", len(views), equal)
}

// canonStep is one step of a path into a value: a struct field, a slice or
// array element, a map value (by key) or a pointer's target.
type canonStep struct {
	field, index int
	key          reflect.Value
	deref        bool
}

func canonNavigate(v reflect.Value, path []canonStep) reflect.Value {
	for _, s := range path {
		switch {
		case s.deref:
			v = v.Elem()
		case s.key.IsValid():
			v = v.MapIndex(s.key)
		case s.field >= 0:
			v = v.Field(s.field)
		default:
			v = v.Index(s.index)
		}
	}
	return v
}

// canonVariants are the replacement values tried at a settable position of
// type t: the shapes where JSON and a naive comparison disagree.
func canonVariants(t reflect.Type) []reflect.Value {
	var out []reflect.Value
	add := func(v any) { out = append(out, reflect.ValueOf(v).Convert(t)) }
	switch t.Kind() {
	case reflect.Bool:
		add(true)
		add(false)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		add(0)
		add(1)
		add(-1)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		add(0)
		add(1)
		add(12)
	case reflect.Float32, reflect.Float64:
		add(0.0)
		add(math.Copysign(0, -1))
		add(0.5)
	case reflect.String:
		add("")
		add("x")
		add("x!")
		add("\xff")
		add("\xfe") // both invalid bytes encode to �: JSON-equal
		add("�")
		add("<&>")
	case reflect.Slice:
		out = append(out, reflect.Zero(t), reflect.MakeSlice(t, 0, 0), reflect.MakeSlice(t, 1, 1))
	case reflect.Map:
		out = append(out, reflect.Zero(t), reflect.MakeMap(t))
		if t.Key().Kind() == reflect.String {
			m := reflect.MakeMap(t)
			m.SetMapIndex(reflect.ValueOf("k").Convert(t.Key()), reflect.Zero(t.Elem()))
			out = append(out, m)
		}
	case reflect.Pointer:
		out = append(out, reflect.Zero(t), reflect.New(t.Elem()))
	}
	return out
}

// TestCanonicalEqualityIsJSONEqualityUnderMutation takes real boards and,
// at every reachable position (the first few occurrences of each struct
// field), substitutes the shapes that separate JSON equality from a naive
// comparison -- nil against empty, omitempty zeros, -0, invalid UTF-8 --
// and requires canonical equality to agree with JSON equality between every
// pair of variants.
func TestCanonicalEqualityIsJSONEqualityUnderMutation(t *testing.T) {
	views := canonGameViews(t, 220)
	type fieldKey struct {
		typ   reflect.Type
		field int
	}
	seen := make(map[fieldKey]int)
	positions, pairs := 0, 0
	var walk func(root *view.View, v reflect.Value, path []canonStep, depth int)
	try := func(root *view.View, path []canonStep, typ reflect.Type) {
		variants := canonVariants(typ)
		if len(variants) < 2 {
			return
		}
		positions++
		base := jsonOf(t, root)
		var cp view.View
		js := make([][]byte, len(variants))
		cs := make([][]byte, len(variants))
		for i, x := range variants {
			cp = view.View{}
			if err := json.Unmarshal(base, &cp); err != nil {
				t.Fatal(err)
			}
			target := canonNavigate(reflect.ValueOf(&cp).Elem(), path)
			if !target.CanSet() {
				return
			}
			target.Set(x)
			js[i] = jsonOf(t, &cp)
			cs[i] = bytes.Clone(canonOf(t, &cp))
		}
		for i := range variants {
			for j := i + 1; j < len(variants); j++ {
				pairs++
				if je, ce := bytes.Equal(js[i], js[j]), bytes.Equal(cs[i], cs[j]); je != ce {
					t.Fatalf("%s at %v: variants %v and %v: JSON equal %v, canonical equal %v", typ, path, variants[i], variants[j], je, ce)
				}
			}
		}
	}
	walk = func(root *view.View, v reflect.Value, path []canonStep, depth int) {
		if depth > 8 {
			return
		}
		switch v.Kind() {
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				f := v.Type().Field(i)
				if !f.IsExported() || f.Tag.Get("json") == "-" {
					continue
				}
				k := fieldKey{v.Type(), i}
				if seen[k] >= 3 {
					continue
				}
				seen[k]++
				p := append(append([]canonStep(nil), path...), canonStep{field: i})
				try(root, p, f.Type)
				walk(root, v.Field(i), p, depth+1)
			}
		case reflect.Slice, reflect.Array:
			for i := 0; i < v.Len() && i < 2; i++ {
				walk(root, v.Index(i), append(append([]canonStep(nil), path...), canonStep{field: -1, index: i}), depth+1)
			}
		case reflect.Pointer:
			if !v.IsNil() {
				walk(root, v.Elem(), append(append([]canonStep(nil), path...), canonStep{field: -1, deref: true}), depth+1)
			}
		}
	}
	// A decoded copy, so every position is reached through settable values
	// (map values are not; the map variants above cover map shapes).
	for i := range views {
		var root view.View
		if err := json.Unmarshal(jsonOf(t, &views[i]), &root); err != nil {
			t.Fatal(err)
		}
		walk(&root, reflect.ValueOf(&root).Elem(), nil, 0)
	}
	if positions < 100 {
		t.Fatalf("only %d positions mutated", positions)
	}
	t.Logf("%d positions, %d variant pairs", positions, pairs)
}

// TestActionOrderAndKeyMatchTheJSONEncoding: actionJSONCompare orders
// actions exactly as their JSON encodings' bytes do, and AppendActionsKey
// separates exactly the lists whose JSON differs.
func TestActionOrderAndKeyMatchTheJSONEncoding(t *testing.T) {
	strs := []string{"", "a", "a!", "a ", "a\"", "a\\", "ab", "b", "<", "&x", "\xff", "\xfe", "�", " ", "é", "a\n", "a\x01", "Z", "cast"}
	nums := []int{0, 1, 9, 10, 12, 2, -1, -12, 100}
	var acts []Action
	for i, s := range strs {
		n := nums[i%len(nums)]
		acts = append(acts,
			Action{Kind: s, Obj: uint32(i)},
			Action{Decision: decision.KPriority, Kind: "cast", Obj: uint32(n + 12), Value: s},
			Action{Decision: decision.KTarget, Source: uint32(i), Mode: s, Ability: n, Amount: -n},
			Action{Kind: "ability", Obj: uint32(i % 3), Ability: n, SVar: s, Value: s},
		)
	}
	for i := range acts {
		for j := range acts {
			a, b := acts[i], acts[j]
			want := bytes.Compare(jsonOf(t, a), jsonOf(t, b))
			if got := actionJSONCompare(&a, &b); (got < 0) != (want < 0) || (got > 0) != (want > 0) {
				t.Fatalf("compare(%+v, %+v) = %d, JSON order %d", a, b, got, want)
			}
			ka := AppendActionsKey(nil, []Action{a})
			kb := AppendActionsKey(nil, []Action{b})
			if bytes.Equal(ka, kb) != (want == 0) {
				t.Fatalf("key(%+v) == key(%+v) is %v, JSON equal %v", a, b, bytes.Equal(ka, kb), want == 0)
			}
		}
	}
	if bytes.Equal(AppendActionsKey(nil, nil), AppendActionsKey(nil, []Action{})) {
		t.Fatal("nil and empty action lists share a key, but their JSON differs (null, [])")
	}
	for _, s := range strs {
		if got, want := appendJSONString(nil, s), jsonOf(t, s); !bytes.Equal(got, want) {
			t.Fatalf("appendJSONString(%q) = %s, json %s", s, got, want)
		}
	}
}

// CompareActionsJSON orders action lists as their JSON encodings' bytes do
// (nil, empty, prefixes and multi-action lists included), and
// ParseActionsKey inverts AppendActionsKey.
func TestActionListOrderAndKeyRoundTrip(t *testing.T) {
	strs := []string{"", "a", "a!", "a\"", "ab", "b", "\xff", "\ufffd", "é", "cast", "attacks:3"}
	one := []Action{}
	for i, s := range strs {
		one = append(one,
			Action{Decision: decision.KPriority, Kind: "cast", Obj: uint32(i), Value: s},
			Action{Decision: decision.KTarget, Source: uint32(i), Mode: s, Ability: -i, Amount: i, AltCostIndex: i % 2, Player: 1, Attacker: 7, SVar: s},
		)
	}
	lists := [][]Action{nil, {}}
	for i := range one {
		lists = append(lists, []Action{one[i]})
		lists = append(lists, []Action{one[i], one[(i+3)%len(one)]})
		lists = append(lists, []Action{one[i], one[(i+3)%len(one)], one[(i+5)%len(one)]})
	}
	for _, a := range lists {
		for _, b := range lists {
			want := bytes.Compare(jsonOf(t, a), jsonOf(t, b))
			if got := CompareActionsJSON(a, b); (got < 0) != (want < 0) || (got > 0) != (want > 0) {
				t.Fatalf("CompareActionsJSON(%+v, %+v) = %d, JSON order %d", a, b, got, want)
			}
		}
		k := AppendActionsKey(nil, a)
		back, err := ParseActionsKey(k)
		if err != nil {
			t.Fatalf("ParseActionsKey(key(%+v)): %v", a, err)
		}
		// The round trip is the JSON round trip: an invalid UTF-8 string
		// comes back as encoding/json decodes it.
		var viaJSON []Action
		if err := json.Unmarshal(jsonOf(t, a), &viaJSON); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(back, viaJSON) || (back == nil) != (a == nil) {
			t.Fatalf("ParseActionsKey(key(%+v)) = %+v, want %+v", a, back, viaJSON)
		}
		if !bytes.Equal(AppendActionsKey(nil, back), AppendActionsKey(nil, viaJSON)) {
			t.Fatalf("re-keying %+v changed the key", back)
		}
	}
	for _, bad := range [][]byte{{}, {2}, {1}, {1, 5}, append(AppendActionsKey(nil, lists[3]), 0)} {
		if _, err := ParseActionsKey(bad); err == nil {
			t.Fatalf("ParseActionsKey(%v) accepted a malformed key", bad)
		}
	}
}
