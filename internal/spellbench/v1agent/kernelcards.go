package v1agent

import (
	_ "embed"
	"encoding/json"
	"strings"
)

// KernelCard is one row of mtg-kernel's card DB (data/cards_v1.json, MIT),
// trimmed: the kernel identifies a card by its array index (card_db_id),
// which is how stack items and hidden-then-revealed objects reach us.
type KernelCard struct {
	Name      string   `json:"name"`
	ManaCost  string   `json:"mana_cost"`
	MV        int      `json:"mv"`
	Types     []string `json:"types"`
	Produces  []string `json:"produces"`
	Mechanics []string `json:"mechanics"`
}

//go:embed kernelcards.json
var kernelCardsJSON []byte

var kernelCards, kernelByName = func() ([]KernelCard, map[string]*KernelCard) {
	var doc struct {
		Cards []KernelCard `json:"cards"`
	}
	if err := json.Unmarshal(kernelCardsJSON, &doc); err != nil {
		panic("v1agent: kernelcards.json: " + err.Error())
	}
	by := map[string]*KernelCard{}
	for i := range doc.Cards {
		by[normName(doc.Cards[i].Name)] = &doc.Cards[i]
	}
	return doc.Cards, by
}()

// KernelCardByID returns the DB row for a card_db_id (nil when out of range).
func KernelCardByID(id uint16) *KernelCard {
	if int(id) >= len(kernelCards) {
		return nil
	}
	return &kernelCards[id]
}

// KernelCardByName returns the DB row for a card name.
func KernelCardByName(name string) *KernelCard { return kernelByName[normName(name)] }

// CostColors returns the colored symbols of the mana cost, e.g. "RR" for
// {1}{R}{R}; hybrid symbols contribute their first color.
func (k *KernelCard) CostColors() string {
	if k == nil {
		return ""
	}
	var b strings.Builder
	for _, sym := range strings.Split(strings.ReplaceAll(k.ManaCost, "}", ""), "{") {
		if sym == "" {
			continue
		}
		switch sym[0] {
		case 'W', 'U', 'B', 'R', 'G':
			b.WriteByte(sym[0])
		}
	}
	return b.String()
}

// Has reports a kernel mechanic tag.
func (k *KernelCard) Has(mech string) bool {
	if k == nil {
		return false
	}
	for _, m := range k.Mechanics {
		if m == mech {
			return true
		}
	}
	return false
}

// IsType reports a printed type ("Creature", "Instant", ...).
func (k *KernelCard) IsType(t string) bool {
	if k == nil {
		return false
	}
	for _, x := range k.Types {
		if x == t {
			return true
		}
	}
	return false
}
