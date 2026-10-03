package effects

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// fillNonZero sets every settable field reachable from v to a non-zero
// value, so a constructor that forgets to copy one shows up as a zero.
func fillNonZero(t *testing.T, v reflect.Value) {
	t.Helper()
	switch v.Kind() {
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if f := v.Field(i); f.CanSet() {
				fillNonZero(t, f)
			}
		}
	case reflect.Slice:
		s := reflect.MakeSlice(v.Type(), 1, 1)
		fillNonZero(t, s.Index(0))
		v.Set(s)
	case reflect.Map:
		m := reflect.MakeMap(v.Type())
		k, e := reflect.New(v.Type().Key()).Elem(), reflect.New(v.Type().Elem()).Elem()
		fillNonZero(t, k)
		fillNonZero(t, e)
		m.SetMapIndex(k, e)
		v.Set(m)
	case reflect.Pointer:
		v.Set(reflect.New(v.Type().Elem()))
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			fillNonZero(t, v.Index(i))
		}
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(1)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(1)
	case reflect.String:
		v.SetString("x")
	case reflect.Func, reflect.Interface:
		// Not seeded by any constructor.
	default:
		t.Fatalf("fillNonZero: unhandled kind %s", v.Kind())
	}
}

// TestNewCtxSeedsEveryCtxInitField holds NewCtx to CtxInit: a field added to
// CtxInit that NewCtx does not copy into the Ctx field of the same name fails
// here, instead of silently constructing a context without it.
func TestNewCtxSeedsEveryCtxInitField(t *testing.T) {
	var in CtxInit
	fillNonZero(t, reflect.ValueOf(&in).Elem())
	c := NewCtx(7, 2, in)
	if c.Source != 7 || c.Controller != 2 {
		t.Fatalf("NewCtx source/controller = %d/%d, want 7/2", c.Source, c.Controller)
	}
	iv, cv := reflect.ValueOf(in), reflect.ValueOf(c)
	for i := 0; i < iv.NumField(); i++ {
		name := iv.Type().Field(i).Name
		got := cv.FieldByName(name)
		if !got.IsValid() {
			t.Errorf("CtxInit.%s has no Ctx field of the same name", name)
			continue
		}
		if !reflect.DeepEqual(got.Interface(), iv.Field(i).Interface()) {
			t.Errorf("NewCtx does not copy CtxInit.%s", name)
		}
	}
	if ptr := NewCtxPtr(7, 2, in); !reflect.DeepEqual(*ptr, c) {
		t.Error("NewCtxPtr differs from NewCtx")
	}
}

// resolvingCtx is a Ctx with every resolution-published table (and a few
// fields a derivation must NOT inherit) filled non-zero.
func resolvingCtx(t *testing.T) *Ctx {
	t.Helper()
	c := &Ctx{Source: 3, Controller: 1, X: 4}
	for _, f := range []any{&c.SVars, &c.EffectiveNames, &c.EffectiveTypes, &c.LayerTables,
		&c.StaticGoads, &c.TargetableObjects, &c.Targets, &c.Remembered, &c.TriggerContext} {
		fillNonZero(t, reflect.ValueOf(f).Elem())
	}
	return c
}

// TestSpecContextCarriesTableSpecContext holds the resolution SpecContext to
// the table derivation it spells out inline (for its inlining budget): every
// field TableSpecContext binds, SpecContext binds to the same value.
func TestSpecContextCarriesTableSpecContext(t *testing.T) {
	c := resolvingCtx(t)
	table, full := c.TableSpecContext(1), c.SpecContext(1)
	tv, fv := reflect.ValueOf(table), reflect.ValueOf(full)
	for i := 0; i < tv.NumField(); i++ {
		name := tv.Type().Field(i).Name
		if tv.Field(i).IsZero() || tv.Type().Field(i).Type.Kind() == reflect.Func {
			continue
		}
		if !reflect.DeepEqual(tv.Field(i).Interface(), fv.Field(i).Interface()) {
			t.Errorf("SpecContext.%s differs from TableSpecContext's", name)
		}
	}
	for name, v := range map[string]bool{
		"EffectiveNames": table.EffectiveNames != nil, "DerivedTypes": table.DerivedTypes != nil,
		"StaticGoads": table.StaticGoads != nil, "DerivedColors": table.DerivedColors != nil,
		"DerivedKeywords": table.DerivedKeywords != nil,
	} {
		if !v {
			t.Errorf("TableSpecContext does not bind %s", name)
		}
	}
	if table.ResolutionTargets != nil || table.Remembered != nil || table.Resolve != nil || table.TriggerCard != 0 {
		t.Error("TableSpecContext carries more than the source, perspective and layer tables")
	}
}

// TestChildCarriesResolutionTables: a child context re-anchors source and
// controller, keeps the script table and every resolution-published derived
// table, and starts every per-resolution field fresh.
func TestChildCarriesResolutionTables(t *testing.T) {
	c := resolvingCtx(t)
	k := c.Child(9, 2)
	if k.Source != 9 || k.Controller != 2 {
		t.Fatalf("Child source/controller = %d/%d, want 9/2", k.Source, k.Controller)
	}
	if !reflect.DeepEqual(k.SVars, c.SVars) || !reflect.DeepEqual(k.EffectiveNames, c.EffectiveNames) ||
		!reflect.DeepEqual(k.EffectiveTypes, c.EffectiveTypes) || !reflect.DeepEqual(k.LayerTables, c.LayerTables) ||
		!reflect.DeepEqual(k.StaticGoads, c.StaticGoads) || !reflect.DeepEqual(k.TargetableObjects, c.TargetableObjects) {
		t.Error("Child dropped a resolution table")
	}
	if k.X != 0 || k.Targets != nil || k.Remembered != nil || k.TriggerCard != 0 {
		t.Error("Child inherited a per-resolution field")
	}
}

// TestForTriggerCopiesCostLists: the minted ability's cost-paid lists are
// private copies, so the spawning resolution appending to its own cannot
// rewrite them.
func TestForTriggerCopiesCostLists(t *testing.T) {
	c := &Ctx{Source: 3, Controller: 1, Exiled: []state.ObjID{5}}
	tc := TriggerContext{TriggerCard: 8}
	k := c.ForTrigger(tc)
	c.Exiled[0] = 6
	if k.Exiled[0] != 5 || k.TriggerCard != 8 || k.Source != 3 || k.Controller != 1 {
		t.Errorf("ForTrigger = source %d controller %d exiled %v trigger card %d", k.Source, k.Controller, k.Exiled, k.TriggerCard)
	}
}
