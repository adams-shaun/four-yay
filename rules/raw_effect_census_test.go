package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

func TestParamCensusCatchesEffectSVarRawChildren(t *testing.T) {
	t.Parallel()
	_, d := measureParamCensus(t, nil)
	if !d.trig["ChangesZone"]["Destination"] || !d.stat["Continuous"]["Affected"] || !d.repl["Moved"]["Destination"] {
		t.Fatal("fixture premise broken: expected derived reads are missing")
	}
	c := &cards.Card{Faces: []*cards.Face{{
		Abilities: []*cards.SA{{Kind: "AB", API: "Effect", Params: map[string]string{
			"Triggers": "T", "StaticAbilities": "S", "ReplacementEffects": "R",
		}}},
		SVars: map[string]string{
			"T": "Mode$ ChangesZone | RawUnread$ x",
			"S": "Mode$ Continuous | RawUnread$ x",
			"R": "Event$ Moved | RawUnread$ x",
		},
	}}}
	got := cardCensusLabels(c, d, nil)
	want := map[string]bool{
		"param:trig:ChangesZone.RawUnread": true,
		"param:stat:Continuous.RawUnread":  true,
		"param:repl:Moved.RawUnread":       true,
	}
	for _, label := range got {
		delete(want, label)
	}
	for label := range want {
		t.Errorf("missing %s in census labels %v", label, got)
	}
}
