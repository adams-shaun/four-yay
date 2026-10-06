package rules

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
)

// TestEqualVerifyCoversEveryField holds equal_verify.go's field lists to
// the structs: a field added later fails here until it is compared.
func TestEqualVerifyCoversEveryField(t *testing.T) {
	for _, c := range []struct {
		v    any
		want int
	}{
		{ContinuousEffect{}, continuousEffectFieldCount}, {attackOffer{}, attackOfferFieldCount},
		{blockCharge{}, blockChargeFieldCount}, {effects.ObjectTypes{}, objectTypesFieldCount},
		{decision.Option{}, optionFieldCount}, {decision.Grant{}, grantFieldCount},
	} {
		ty := reflect.TypeOf(c.v)
		if n := ty.NumField(); n != c.want {
			t.Errorf("%s has %d fields, equal_verify.go covers %d: extend its equal function and count", ty, n, c.want)
		}
	}
}

// mutations returns, for each field of v's struct type, copies of v with
// that field set to a non-zero value and (for slices and maps) to an empty
// non-nil one, so a field the typed compare skips shows up as a
// disagreement with reflect.DeepEqual.
func mutations(t *testing.T, v any) []reflect.Value {
	t.Helper()
	ty := reflect.TypeOf(v)
	var out []reflect.Value
	for i := 0; i < ty.NumField(); i++ {
		ft := ty.Field(i).Type
		var vals []reflect.Value
		switch ft.Kind() {
		case reflect.String:
			vals = append(vals, reflect.ValueOf("x").Convert(ft))
		case reflect.Bool:
			vals = append(vals, reflect.ValueOf(true))
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			vals = append(vals, reflect.ValueOf(int64(1)).Convert(ft))
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			vals = append(vals, reflect.ValueOf(uint64(1)).Convert(ft))
		case reflect.Slice:
			vals = append(vals, reflect.MakeSlice(ft, 0, 0), reflect.MakeSlice(ft, 1, 1))
		case reflect.Map:
			m := reflect.MakeMap(ft)
			vals = append(vals, reflect.MakeMap(ft))
			m.SetMapIndex(reflect.ValueOf("k"), reflect.ValueOf("v"))
			vals = append(vals, m)
		case reflect.Pointer:
			vals = append(vals, reflect.New(ft.Elem()))
		case reflect.Struct:
			for _, inner := range mutations(t, reflect.Zero(ft).Interface()) {
				vals = append(vals, inner)
			}
		default:
			t.Fatalf("%s.%s: unhandled kind %s", ty, ty.Field(i).Name, ft.Kind())
		}
		for _, fv := range vals {
			c := reflect.New(ty).Elem()
			c.Set(reflect.ValueOf(v))
			reflect.NewAt(ft, c.Field(i).Addr().UnsafePointer()).Elem().Set(fv)
			out = append(out, c)
		}
	}
	return out
}

func checkAgainstDeepEqual[T any](t *testing.T, eq func(a, b *T) bool) {
	t.Helper()
	var zero T
	vals := append([]reflect.Value{reflect.ValueOf(zero)}, mutations(t, zero)...)
	for i := range vals {
		for j := range vals {
			a, b := vals[i].Interface().(T), vals[j].Interface().(T)
			if got, want := eq(&a, &b), reflect.DeepEqual(a, b); got != want {
				t.Fatalf("%T: typed equal(%d, %d) = %v, DeepEqual %v", zero, i, j, got, want)
			}
		}
	}
}

func TestEqualVerifyMatchesDeepEqual(t *testing.T) {
	checkAgainstDeepEqual(t, continuousEffectEqual)
	checkAgainstDeepEqual(t, optionEqual)
	checkAgainstDeepEqual(t, func(a, b *attackOffer) bool {
		return attackOffersEqual([]attackOffer{*a}, []attackOffer{*b})
	})
	checkAgainstDeepEqual(t, func(a, b *effects.ObjectTypes) bool {
		return objectTypesListEqual([]effects.ObjectTypes{*a}, []effects.ObjectTypes{*b})
	})
}
