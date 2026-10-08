//go:build gamepool

package gamepool

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/view"
)

func TestEncodeStateMZContentDerived(t *testing.T) {
	r := &Request{View: view.View{Viewer: 0, Step: "main1", Phase: "main1"},
		Decision: decision.Decision{Kind: decision.KPriority}}
	a := make([]float32, 2048)
	b := make([]float32, 2048)
	encodeStateMZ(r, a)
	encodeStateMZ(r, b)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("encodeStateMZ not deterministic")
	}
	hot := 0
	for _, x := range a {
		if x != 0 {
			hot++
		}
	}
	if hot == 0 {
		t.Fatal("no features emitted")
	}
	r2 := &Request{View: view.View{Viewer: 0, Step: "combat_declare_attackers", Phase: "combat"},
		Decision: decision.Decision{Kind: decision.KPriority}}
	c := make([]float32, 2048)
	encodeStateMZ(r2, c)
	if reflect.DeepEqual(a, c) {
		t.Fatal("different steps encoded identically")
	}
}
