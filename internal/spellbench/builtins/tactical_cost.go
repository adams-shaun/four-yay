package builtins

import (
	"strings"

	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// costValue prices a Forge cost string's non-mana parts against the board
// ("1 B Sac<1/Artifact;Creature>", "Sac<3/Creature>", "Discard<1/Card>",
// "Return<1/Forest>", "tapXType<1/Creature.White>", "PayLife<2>"): each
// sacrifice costs the cheapest matching permanents we control, each discard
// a card from hand. A cost we cannot pay prices at 100. The card's own
// "CARDNAME" parts are priced by the caller (self-sacrifice, cycling).
func (t *tactical) costValue(s *tstate, cost string, src state.ObjID) float64 {
	v := 0.0
	for rest := cost; ; {
		i := strings.IndexByte(rest, '<')
		if i < 0 {
			return v
		}
		j := strings.IndexByte(rest[i:], '>')
		if j < 0 {
			return v
		}
		head := rest[:i]
		if k := strings.LastIndexByte(head, ' '); k >= 0 {
			head = head[k+1:]
		}
		body := rest[i+1 : i+j]
		rest = rest[i+j+1:]
		parts := strings.Split(body, "/")
		n, ok := literal(parts[0])
		if !ok || n < 1 {
			n = 1
		}
		typ := ""
		if len(parts) > 1 {
			typ = parts[1]
		}
		switch head {
		case "Sac":
			if typ == "CARDNAME" {
				continue
			}
			v += t.sacCost(s, int(n), typ, src)
		case "Discard":
			if typ == "CARDNAME" {
				continue
			}
			v += t.discardCost(s, src) * float64(n)
		case "Return":
			v += t.landCost(s) * 0.5 * float64(n)
		case "tapXType":
			v += 0.3 * float64(n)
		case "ExileFromGrave":
			v += 0.3
		case "PayLife":
			v += s.lifeValue(s.myLife, n)
		case "AddCounter":
			v += 0.5
		}
	}
}

// sacCost is the cost of sacrificing the n cheapest permanents we control
// matching typ ("Creature", "Artifact;Creature", "Mountain"), src excluded.
func (t *tactical) sacCost(s *tstate, n int, typ string, src state.ObjID) float64 {
	var vals []float64
	for i := range s.meP.Battlefield {
		cv := &s.meP.Battlefield[i]
		if cv.ID == src || !sacMatches(typ, cv) {
			continue
		}
		vals = append(vals, t.permValue(s, cv))
	}
	if len(vals) < n {
		return 100
	}
	// insertion sort ascending
	for i := 1; i < len(vals); i++ {
		for j := i; j > 0 && vals[j] < vals[j-1]; j-- {
			vals[j], vals[j-1] = vals[j-1], vals[j]
		}
	}
	c := 0.0
	for i := 0; i < n; i++ {
		c += vals[i]
	}
	return c
}

func sacMatches(typ string, cv *view.CardView) bool {
	if typ == "" {
		return true
	}
	for _, alt := range strings.Split(typ, ";") {
		base, _, _ := strings.Cut(alt, ".")
		if base == "Permanent" || hasWord(cv.Types, base) {
			return true
		}
	}
	return false
}

// permValue is what losing one of our permanents costs.
func (t *tactical) permValue(s *tstate, cv *view.CardView) float64 {
	switch cv.Name {
	case "Clue", "Food", "Blood", "Treasure", "Map", "Eldrazi Spawn", "Eldrazi Scion", "Lotus Petal":
		return t.w.SacToken
	}
	if c := s.cre[cv.ID]; c != nil {
		return s.creValue(c)
	}
	if isLandView(cv) {
		return t.landCost(s)
	}
	return 1.5 + 0.7*float64(botpolicyCMC(cv.ManaCost))
}
