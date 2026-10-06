package rules

import (
	"reflect"
	"testing"
	"unsafe"
)

// walkFaceFactsEqual is a hand-written field list standing in for
// reflect.DeepEqual in the per-read verifier. A field added to
// walkFaceFacts and not to that list would make the verifier silently blind
// to it, so this test makes every field, one at a time, differ from the zero
// value and requires the comparator to notice.
func TestWalkFaceFactsEqualCoversEveryField(t *testing.T) {
	typ := reflect.TypeOf(walkFaceFacts{})
	for i := 0; i < typ.NumField(); i++ {
		fld := typ.Field(i)
		var a, b walkFaceFacts
		if !walkFaceFactsEqual(&a, &b) {
			t.Fatal("two zero facts compare unequal")
		}
		v := reflect.NewAt(fld.Type, unsafe.Add(unsafe.Pointer(&b), fld.Offset)).Elem()
		if !setNonZero(v) {
			t.Fatalf("field %s (%s): no non-zero value to try; extend setNonZero", fld.Name, fld.Type)
		}
		if walkFaceFactsEqual(&a, &b) {
			t.Errorf("walkFaceFactsEqual ignores field %s", fld.Name)
		}
	}
}

var walkFaceFactsEqualSentinel [8]byte

func setNonZero(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(1)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		v.SetUint(1)
	case reflect.String:
		v.SetString("x")
	case reflect.Pointer:
		v.Set(reflect.New(v.Type().Elem()))
	case reflect.UnsafePointer:
		v.SetPointer(unsafe.Pointer(&walkFaceFactsEqualSentinel))
	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 1, 1))
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			f := v.Field(i)
			f = reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr())).Elem()
			if setNonZero(f) {
				return true
			}
		}
		return false
	default:
		return false
	}
	return true
}
