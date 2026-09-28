package v1agent

import (
	_ "embed"
	"encoding/json"
)

// EffectFact summarises one effect (a spell, an activated ability or a
// trigger's effect) from gorge's IR.
type EffectFact struct {
	API           string   `json:"api"`
	Chain         []string `json:"chain,omitempty"`
	Trigger       string   `json:"trigger,omitempty"`
	Damage        int      `json:"damage,omitempty"`
	PumpPower     int      `json:"pump_power,omitempty"`
	PumpToughness int      `json:"pump_toughness,omitempty"`
	Cards         int      `json:"cards,omitempty"`
	Target        string   `json:"target,omitempty"`
	Harmful       bool     `json:"harmful,omitempty"`
}

// CardFact is one card's gorge-IR-derived facts.
type CardFact struct {
	CMC       int          `json:"cmc"`
	Types     []string     `json:"types,omitempty"`
	HasPT     bool         `json:"has_pt,omitempty"`
	Power     int          `json:"power,omitempty"`
	Toughness int          `json:"toughness,omitempty"`
	Keywords  []string     `json:"keywords,omitempty"`
	Mana      bool         `json:"mana,omitempty"`
	Spell     *EffectFact  `json:"spell,omitempty"`
	Abilities []EffectFact `json:"abilities,omitempty"`
	Triggers  []EffectFact `json:"triggers,omitempty"`
}

// HasType reports a (lowercase) card type.
func (c *CardFact) HasType(t string) bool {
	if c == nil {
		return false
	}
	for _, x := range c.Types {
		if x == t {
			return true
		}
	}
	return false
}

// HasKeyword reports a keyword head.
func (c *CardFact) HasKeyword(k string) bool {
	if c == nil {
		return false
	}
	for _, x := range c.Keywords {
		if x == k {
			return true
		}
	}
	return false
}

//go:embed cardfacts.json
var cardFactsJSON []byte

var cardFacts = func() map[string]*CardFact {
	m := map[string]*CardFact{}
	if err := json.Unmarshal(cardFactsJSON, &m); err != nil {
		panic("v1agent: cardfacts.json: " + err.Error())
	}
	return m
}()

// Fact returns the facts for a card name (nil when unknown).
func Fact(name string) *CardFact { return cardFacts[name] }
