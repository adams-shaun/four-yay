package shape

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/compliance"
)

// TestRowEqualCoversEveryField perturbs each VerdictRow field in turn and
// requires rowEqual to notice, so a field added to VerdictRow without a
// matching comparison in rowEqual fails here.
func TestRowEqualCoversEveryField(t *testing.T) {
	base := compliance.VerdictRow{Frozen: []compliance.Frozen{{At: "a", Field: "f", Value: "v"}}}
	if !rowEqual(base, base) {
		t.Fatal("rowEqual(x, x) = false")
	}
	rt := reflect.TypeOf(base)
	for i := 0; i < rt.NumField(); i++ {
		mod := base
		f := reflect.ValueOf(&mod).Elem().Field(i)
		switch f.Kind() {
		case reflect.String:
			f.SetString("changed")
		case reflect.Slice:
			mod.Frozen = []compliance.Frozen{{At: "b"}}
		default:
			t.Fatalf("VerdictRow.%s: unhandled kind %s; extend rowEqual and this test", rt.Field(i).Name, f.Kind())
		}
		if rowEqual(base, mod) {
			t.Errorf("rowEqual misses VerdictRow.%s", rt.Field(i).Name)
		}
	}
	empty := base
	empty.Frozen = []compliance.Frozen{}
	nilled := base
	nilled.Frozen = nil
	if rowEqual(empty, nilled) {
		t.Error("rowEqual treats nil and empty Frozen as equal; DeepEqual did not")
	}
}
