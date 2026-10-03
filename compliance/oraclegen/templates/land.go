package templates

import "github.com/adams-shaun/gorge/compliance/oraclegen"

// PlayLand plays the land from hand.
var PlayLand = Template{ID: "play-land", Version: 1}

func playLand(name string) oraclegen.Item {
	return PlayLand.item(name, oraclegen.Scenario{
		Setup: map[string]oraclegen.Seat{"p0": {Hand: []string{name}}},
		Steps: []oraclegen.Step{{Op: "play", Seat: 0, Card: "p0:" + name}},
	})
}
