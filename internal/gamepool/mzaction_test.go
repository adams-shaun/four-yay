//go:build gamepool

package gamepool

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

func TestJavaHash(t *testing.T) {
	if got := javaHash("hello"); got != 99162322 {
		t.Fatalf("javaHash(hello)=%d", got)
	}
}

func TestEncodeActionMZ(t *testing.T) {
	v := view.View{Players: []view.PlayerView{{Hand: []view.CardView{{ID: 7, Name: "Lightning Bolt"}}}}}
	r := &Request{View: v, Decision: decision.Decision{Kind: decision.KPriority}}
	a := make([]float32, 128)
	b := make([]float32, 128)
	encodeActionMZ(r, decision.Option{Kind: "cast", Label: "Cast Lightning Bolt", Obj: state.ObjID(7)}, a)
	encodeActionMZ(r, decision.Option{Kind: "cast", Label: "Cast Lightning Bolt", Obj: state.ObjID(7)}, b)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("not deterministic")
	}
	c := make([]float32, 128)
	encodeActionMZ(r, decision.Option{Kind: "pass", Label: "Pass priority"}, c)
	if reflect.DeepEqual(a, c) {
		t.Fatal("distinct options encoded identically")
	}
}
