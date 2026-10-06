package paymirror

import (
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
)

// The random deck source is a two-colour variant of cmd/cardfuzz's
// mono-colour generator (uniform weights, no coverage state): 20 lands -- up
// to four non-basic lands whose colour identity fits the pair (dual lands
// included, which is what exercises multi-ability sources), the rest basics
// split between the two colours -- and 40 distinct supported non-land cards
// whose identity fits the pair. Every list is a pure function of the seed.

var colourLetters = []string{"W", "U", "B", "R", "G"}
var basicNames = []string{"Plains", "Island", "Swamp", "Mountain", "Forest"}

// excludedTypes never go in a 60-card main deck (cmd/cardfuzz's list).
var excludedTypes = []string{"Conspiracy", "Scheme", "Plane", "Phenomenon", "Vanguard", "Dungeon", "Attraction", "Contraption", "Emblem", "Token"}

type randomCard struct {
	name string
	id   uint8
	land bool
}

// RandomPool is the eligible supported corpus, sorted by name.
type RandomPool struct {
	cards []randomCard
}

func eligibleRandom(c *cards.Card) bool {
	if len(c.Faces) == 0 || c.Faces[0].Name == "" {
		return false
	}
	f := c.Faces[0]
	for _, t := range f.Types {
		for _, x := range excludedTypes {
			if t == x {
				return false
			}
		}
	}
	if f.IsBasic() && f.IsLand() {
		return false
	}
	if strings.Contains(strings.ToLower(f.Oracle), "playing for ante") || strings.HasPrefix(f.Name, "A-") {
		return false
	}
	return true
}

// NewRandomPool indexes the registry's fully supported, deck-eligible cards.
func NewRandomPool(reg *cards.Registry) *RandomPool {
	sup := effects.Supported()
	p := &RandomPool{}
	for _, c := range reg.Cards {
		if !eligibleRandom(c) || len(reg.Unsupported(c, sup)) > 0 {
			continue
		}
		var id uint8
		for _, f := range c.Faces {
			id |= f.ColourIdentity()
		}
		p.cards = append(p.cards, randomCard{name: c.Faces[0].Name, id: id, land: c.Faces[0].IsLand()})
	}
	sort.Slice(p.cards, func(i, j int) bool { return p.cards[i].name < p.cards[j].name })
	return p
}

// Generate returns a label ("rand-UB") and a 60-card list for seed.
func (p *RandomPool) Generate(seed uint64) (string, []string) {
	r := rand.New(rand.NewPCG(seed, seed^0xdec4ab1e))
	a := r.IntN(5)
	b := (a + 1 + r.IntN(4)) % 5
	if b < a {
		a, b = b, a
	}
	mask := uint8(1<<a | 1<<b)
	var lands, spells []string
	for _, c := range p.cards {
		if c.id&^mask != 0 {
			continue
		}
		if c.land {
			lands = append(lands, c.name)
		} else {
			spells = append(spells, c.name)
		}
	}
	pick := func(from []string, k int) []string {
		if k > len(from) {
			k = len(from)
		}
		perm := r.Perm(len(from))
		out := make([]string, k)
		for i := range out {
			out[i] = from[perm[i]]
		}
		sort.Strings(out)
		return out
	}
	nonbasic := pick(lands, 4)
	list := append([]string(nil), nonbasic...)
	for i := 0; i < 20-len(nonbasic); i++ {
		list = append(list, basicNames[[]int{a, b}[i%2]])
	}
	list = append(list, pick(spells, 40)...)
	return "rand-" + colourLetters[a] + colourLetters[b], list
}

// ResolveList resolves a card-name list through the registry.
func ResolveList(reg *cards.Registry, names []string) ([]*cards.Card, error) {
	out := make([]*cards.Card, 0, len(names))
	for _, n := range names {
		c, ok := reg.Lookup(n)
		if !ok {
			return nil, fmt.Errorf("card %q not found", n)
		}
		out = append(out, c)
	}
	return out, nil
}
