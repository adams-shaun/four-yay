package templates

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// PlayLand plays the land from hand.
var PlayLand = Template{ID: "play-land", Version: 1}

func playLand(name string, f *cards.Face) oraclegen.Item {
	return PlayLand.item(name, oraclegen.Scenario{
		Setup: map[string]oraclegen.Seat{"p0": {Hand: []string{name}}},
		// A land may also carry K:MayEffectFromOpeningHand (Gemstone
		// Caverns): decline it so the land is still in hand when the play
		// step runs, exactly as a spell scenario does.
		SetupAnswers: oraclegen.OpeningHandAnswers(f),
		Steps:        []oraclegen.Step{{Op: "play", Seat: 0, Card: "p0:" + name}},
	})
}
