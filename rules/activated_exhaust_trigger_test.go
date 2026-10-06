package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/rules/trigmatch"
)

func TestAbilityCastValidSAExhaust(t *testing.T) {
	b := boardOf(&Engine{})
	exhaust := &cards.SA{Kind: "AB", API: "Animate", Params: map[string]string{"Exhaust": "True"}}
	ordinary := &cards.SA{Kind: "AB", API: "Animate"}
	if !trigmatch.AbilityCastValidSA(b, exhaust, "Activated.Exhaust", 0, 0, 0) {
		t.Fatal("Activated.Exhaust must match an exhaust activation")
	}
	if trigmatch.AbilityCastValidSA(b, ordinary, "Activated.Exhaust", 0, 0, 0) {
		t.Fatal("Activated.Exhaust must not match an ordinary activation")
	}
}
