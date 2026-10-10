// Package corpuscov is the coverage census of a bot-played training corpus:
// for every (card, ability) slot of the repo decks it measures the three
// stage funnel
//
//	IN_DECK  -- the card stood in the slot's activation zone (hand for a cast
//	            or a hand ability, battlefield for a permanent's ability, ...)
//	            at some decision of the game;
//	OFFERED  -- the engine posed the slot as a legal option of a decision the
//	            card's controller answered;
//	CHOSEN   -- that player's policy picked it.
//
// It is the training-data counterpart of the card-support ratchet
// (rules/testdata/known-unsupported): the ratchet says the engine CAN play a
// card, the census says whether the corpus a policy learns from ever SHOWS it.
//
// Identity. A priority option maps back to (card, ability) without engine
// plumbing: decision.Option carries Obj (the source object), Kind
// (cast/play_land/ability/mana/...), Mode and AltCostIndex (the alternative
// cost / cast mode) and Ability (the flat Face().Abilities index the
// battlefield walk offers, rules/legal_walk_battlefield.go). A payment-plan
// cast (decision.PaymentAction without a BaseOptionIndex) is the same "cast"
// slot of its Cast.Object. Non-priority decisions (targets, optional
// triggers, modes, choose, ...) are tallied as dynamic (card, kind:optKind)
// rows: they have no static universe to measure IN_DECK against.
//
// The census is pure observation: it reads the pending Decision, the Intent
// answering it and the game state, and never calls into a policy, the
// engine's advance or any RNG, so it cannot perturb a game. Every report
// list is sorted, so output is deterministic.
package corpuscov

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// Slot is one universe row: an action a deck card can take.
type Slot struct {
	Card string // the card's front-face name
	Key  string // cast, play_land, ability#i[@Face], mana[@Face]
	// Zones are the zones (a bit per state.Zone) the card must stand in, any
	// one of them, for the slot to be IN_DECK: hand or command zone for a
	// cast (a commander is cast from the command zone), the ability's
	// ActivationZone$ (battlefield by default) for an ability.
	Zones uint16
	// Hints are the static attributes of the slot's ability that gate its
	// offer (the structural-gap classifier reads them).
	Hints []string
}

// Row is the funnel tally of one (card, key).
type Row struct {
	Card, Key string
	Universe  bool     // a static slot (false: a dynamic / token row)
	Hints     []string `json:",omitempty"`
	Decks     int      // repo decks carrying the card
	// Game counts: games in which the stage was reached at least once.
	InDeckGames, OfferedGames, ChosenGames int
	// PotentialGames: games in which the slot was in the seat's
	// rules.PotentialActions -- payable after floating every untapped mana
	// source -- at a priority decision. IN_DECK, never OFFERED but POTENTIAL
	// is the pool-priced structural gap (the offer walk prices against the
	// floating pool; nothing floated for it).
	PotentialGames int
	// Event counts.
	Offered, Chosen, ExploreChosen, Potential int
	Class                                     string `json:",omitempty"`
}

// Census accumulates over a whole run (single goroutine).
type Census struct {
	Games    int
	rows     map[string]*Row // "card\x00key"
	asked    map[decision.Kind]int
	exploreN int
	decks    map[string]map[string]bool // card -> deck names

	// per-game scratch
	seen   map[string]uint16 // card -> zones it stood in this game
	gOff   map[string]bool
	gChose map[string]bool
	gPot   map[string]bool
	gSlots []*Slot // universe slots of this game's decks
}

// New returns an empty census.
func New() *Census {
	return &Census{rows: map[string]*Row{}, asked: map[decision.Kind]int{}, decks: map[string]map[string]bool{}}
}

func rowKey(card, key string) string { return card + "\x00" + key }

func (c *Census) row(card, key string) *Row {
	k := rowKey(card, key)
	r := c.rows[k]
	if r == nil {
		r = &Row{Card: card, Key: key}
		c.rows[k] = r
	}
	return r
}

// CardName is a card's census name: its front face's name.
func CardName(cd *cards.Card) string {
	if cd == nil || len(cd.Faces) == 0 || cd.Faces[0] == nil {
		return ""
	}
	return cd.Faces[0].Name
}

func isLand(f *cards.Face) bool {
	for _, t := range f.Types {
		if t == "Land" {
			return true
		}
	}
	return false
}

// Slots enumerates a card's static universe: its front face's cast (or land
// play) and every activated ability of every face.
func Slots(cd *cards.Card) []Slot {
	name := CardName(cd)
	if name == "" {
		return nil
	}
	var out []Slot
	front := cd.Faces[0]
	if isLand(front) {
		out = append(out, Slot{Card: name, Key: "play_land", Zones: zbit(state.ZHand)})
	} else {
		out = append(out, Slot{Card: name, Key: "cast", Zones: zbit(state.ZHand) | zbit(state.ZCommand), Hints: castHints(front)})
	}
	for fi, f := range cd.Faces {
		if f == nil {
			continue
		}
		suffix := ""
		if fi > 0 {
			suffix = "@" + f.Name
		}
		manaSeen := false
		for i, ab := range f.Abilities {
			if ab == nil || !ab.IsActivated() {
				continue
			}
			zone := abilityZone(ab)
			if cards.IsManaAbilitySA(ab) && !strings.Contains(ab.ParamStr(cards.PKPlaneswalker), "True") {
				if !manaSeen {
					out = append(out, Slot{Card: name, Key: "mana" + suffix, Zones: zbit(zone), Hints: []string{"mana-ability"}})
					manaSeen = true
				}
				continue
			}
			out = append(out, Slot{Card: name, Key: "ability#" + strconv.Itoa(i) + suffix, Zones: zbit(zone), Hints: abilityHints(ab)})
		}
	}
	return out
}

func zbit(z state.Zone) uint16 { return 1 << uint(z) }

func abilityZone(ab *cards.SA) state.Zone {
	switch strings.TrimSpace(ab.ParamStr(cards.PKActivationZone)) {
	case "Hand":
		return state.ZHand
	case "Graveyard":
		return state.ZGraveyard
	case "Exile":
		return state.ZExile
	case "Command":
		return state.ZCommand
	}
	return state.ZBattlefield
}

func castHints(f *cards.Face) []string {
	h := []string{"mv" + strconv.Itoa(int(f.Cmc()))}
	if strings.Contains(f.ManaCost, "X") {
		h = append(h, "x-cost")
	}
	for _, sa := range f.Abilities {
		if sa != nil && sa.Kind == "SP" && strings.TrimSpace(sa.ParamStr(cards.PKValidTgts)) != "" {
			h = append(h, "targets")
			break
		}
	}
	return h
}

// abilityHints names the offer gates an ability carries
// (rules/legal_walk_battlefield.go's printed loop, in gate order).
func abilityHints(ab *cards.SA) []string {
	var h []string
	cost := ab.ParamStr(cards.PKCost)
	if costHasMana(cost) {
		h = append(h, "mana-cost")
	}
	if strings.Contains(cost, "T") && strings.Contains(" "+cost+" ", " T ") {
		h = append(h, "tap-cost")
	}
	if strings.Contains(cost, "SubCounter<") {
		h = append(h, "counter-cost")
	}
	if strings.Contains(cost, "Sac<") {
		h = append(h, "sac-cost")
	}
	if ab.ParamStr(cards.PKSorcerySpeed) == "True" {
		h = append(h, "sorcery-speed")
	}
	if strings.Contains(ab.ParamStr(cards.PKPlaneswalker), "True") {
		h = append(h, "loyalty")
	}
	if strings.TrimSpace(ab.ParamStr(cards.PKValidTgts)) != "" {
		h = append(h, "targets")
	}
	for _, k := range []struct {
		p cards.ParamKey
		n string
	}{{cards.PKCheckSVar, "checksvar"}, {cards.PKActivationLimit, "activation-limit"}, {cards.PKActivationPhases, "phase-window"}, {cards.PKIsPresent, "is-present"}, {cards.PKActivation, "activation-cond"}} {
		if strings.TrimSpace(ab.ParamStr(k.p)) != "" {
			h = append(h, k.n)
		}
	}
	if z := abilityZone(ab); z != state.ZBattlefield {
		h = append(h, "zone:"+zoneName(z))
	}
	return h
}

// costHasMana reports whether a Forge Cost$ string carries a mana component:
// a number or a colour/hybrid pip token (W U B R G C X, {..}).
func costHasMana(cost string) bool {
	for _, tok := range strings.Fields(cost) {
		if strings.ContainsAny(tok, "<>") || tok == "T" || tok == "Q" {
			continue
		}
		if _, err := strconv.Atoi(tok); err == nil {
			if tok != "0" {
				return true
			}
			continue
		}
		t := strings.Trim(tok, "{}")
		ok := t != ""
		for _, r := range t {
			if !strings.ContainsRune("WUBRGCXSP/0123456789", r) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func zoneName(z state.Zone) string {
	switch z {
	case state.ZLibrary:
		return "library"
	case state.ZHand:
		return "hand"
	case state.ZBattlefield:
		return "battlefield"
	case state.ZGraveyard:
		return "graveyard"
	case state.ZExile:
		return "exile"
	case state.ZStack:
		return "stack"
	case state.ZCommand:
		return "command"
	}
	return "z" + strconv.Itoa(int(z))
}

// BeginGame registers one game's decks: their slots join the universe and
// the per-game scratch resets.
func (c *Census) BeginGame(deckNames []string, decks [][]*cards.Card) {
	c.seen = map[string]uint16{}
	c.gOff = map[string]bool{}
	c.gChose = map[string]bool{}
	c.gPot = map[string]bool{}
	c.gSlots = c.gSlots[:0]
	dup := map[string]bool{}
	for di, d := range decks {
		for _, cd := range d {
			name := CardName(cd)
			if name == "" {
				continue
			}
			if c.decks[name] == nil {
				c.decks[name] = map[string]bool{}
			}
			if di < len(deckNames) {
				c.decks[name][deckNames[di]] = true
			}
			if dup[name] {
				continue
			}
			dup[name] = true
			for _, s := range Slots(cd) {
				s := s
				r := c.row(s.Card, s.Key)
				r.Universe = true
				r.Hints = s.Hints
				c.gSlots = append(c.gSlots, &s)
			}
		}
	}
}

// presenceZones are the zones whose occupants count toward IN_DECK.
var presenceZones = [...]state.Zone{state.ZHand, state.ZBattlefield, state.ZGraveyard, state.ZExile, state.ZCommand}

// ObserveState marks which deck cards stand in which zone right now. Call it
// at every decision.
func (c *Census) ObserveState(g *state.Game) {
	for p := range g.Players {
		for _, z := range presenceZones {
			for _, id := range g.Zone(z, state.PlayerID(p)) {
				o := g.Obj(id)
				if o == nil || o.IsToken || o.Card == nil {
					continue
				}
				c.seen[CardName(o.Card)] |= zbit(z)
			}
		}
	}
}

// OptionKey maps one option of d back to its (card, key) identity. ok is
// false for an option naming no card (pass, concede, a player target).
func OptionKey(g *state.Game, d *decision.Decision, opt *decision.Option) (card, key string, ok bool) {
	if opt.Obj == 0 {
		return "", "", false
	}
	o := g.Obj(opt.Obj)
	if o == nil || o.Card == nil {
		return "", "", false
	}
	card = CardName(o.Card)
	if o.IsToken {
		card = "token:" + card
	}
	if opt.Kind == "mana" || opt.Kind == "activate" {
		// A mana activation: the priority "tap for mana" action
		// (Kind "activate") or a payment-window pick (Kind "mana"/"activate").
		key = "mana"
		if f := o.Face(); f != nil && len(o.Card.Faces) > 0 && f != o.Card.Faces[0] {
			key += "@" + f.Name
		}
		return card, key, true
	}
	if d.Kind != decision.KPriority {
		k := string(d.Kind) + ":" + opt.Kind
		if opt.Mode != "" {
			k += "/" + opt.Mode
		}
		return card, k, true
	}
	switch opt.Kind {
	case "ability":
		switch {
		case opt.SVar != "":
			key = "granted:" + opt.SVar
		case opt.Keyword != "":
			key = "kwgrant:" + opt.Keyword
		case opt.GainedSource != 0:
			key = "gained"
		default:
			key = "ability#" + strconv.Itoa(opt.Ability) + faceSuffix(o, opt.Ability)
		}
	default:
		key = opt.Kind
	}
	if opt.Mode != "" && opt.Kind != "ability" {
		key += "/" + opt.Mode
	}
	if opt.AltCostIndex != 0 {
		key += "/alt" + strconv.Itoa(opt.AltCostIndex)
	}
	return card, key, true
}

// faceSuffix names the face an ability index sits on when it is not the
// card's front face (a transformed DFC's back-face ability).
func faceSuffix(o *state.Object, i int) string {
	f := o.Face()
	if f == nil || o.Card == nil || len(o.Card.Faces) == 0 || f == o.Card.Faces[0] {
		return ""
	}
	if i < len(f.Abilities) {
		return "@" + f.Name
	}
	return ""
}

// ObserveDecision tallies d's offers. payPlans says whether the deciding
// seat consumes d.PaymentActions (an auto-pay seat): a plan-only cast is
// then an offer of that card's cast slot.
func (c *Census) ObserveDecision(g *state.Game, d *decision.Decision, payPlans bool) {
	c.asked[d.Kind]++
	seen := map[string]bool{}
	for i := range d.Options {
		card, key, ok := OptionKey(g, d, &d.Options[i])
		if !ok || seen[rowKey(card, key)] {
			continue
		}
		seen[rowKey(card, key)] = true
		c.offer(card, key)
		// A variant cast/land play (an alternative cost, flashback, evoke,
		// an MDFC land face) is also an offer of the card's base slot.
		if b := baseKey(key); b != key && !seen[rowKey(card, b)] {
			seen[rowKey(card, b)] = true
			c.offer(card, b)
		}
	}
	if payPlans && d.Kind == decision.KPriority {
		for _, a := range d.PaymentActions {
			// A plan's activations are mana-ability offers of their sources.
			for _, pl := range a.Plans {
				for _, act := range pl.Activations {
					mo := decision.Option{Kind: "mana", Obj: act.Source}
					if card, key, ok := OptionKey(g, d, &mo); ok && !seen[rowKey(card, key)] {
						seen[rowKey(card, key)] = true
						c.offer(card, key)
					}
				}
			}
			if a.BaseOptionIndex != nil || len(a.Plans) == 0 {
				continue
			}
			if card, ok := objCard(g, a.Cast.Object); ok && !seen[rowKey(card, "cast")] {
				seen[rowKey(card, "cast")] = true
				c.offer(card, "cast")
			}
		}
	}
}

func objCard(g *state.Game, id state.ObjID) (string, bool) {
	o := g.Obj(id)
	if o == nil || o.Card == nil {
		return "", false
	}
	if o.IsToken {
		return "token:" + CardName(o.Card), true
	}
	return CardName(o.Card), true
}

func (c *Census) offer(card, key string) {
	r := c.row(card, key)
	r.Offered++
	c.gOff[rowKey(card, key)] = true
}

// ObserveChoice tallies the options in answers d. explored marks an answer
// coverage-directed exploration forced (ExploreSeat), counted apart so a
// census of an exploring corpus still separates policy from forcing.
func (c *Census) ObserveChoice(g *state.Game, d *decision.Decision, in decision.Intent, explored bool) {
	if explored {
		c.exploreN++
	}
	mark1 := func(card, key string) {
		r := c.row(card, key)
		if explored {
			r.ExploreChosen++
			return
		}
		r.Chosen++
		c.gChose[rowKey(card, key)] = true
	}
	mark := func(card, key string) {
		if b := baseKey(key); b != key {
			mark1(card, b)
		}
		mark1(card, key)
	}
	if in.Payment != nil {
		used := map[string]bool{}
		for _, act := range in.Payment.Plan.Activations {
			mo := decision.Option{Kind: "mana", Obj: act.Source}
			if card, key, ok := OptionKey(g, d, &mo); ok && !used[rowKey(card, key)] {
				used[rowKey(card, key)] = true
				mark(card, key)
			}
		}
		for _, a := range d.PaymentActions {
			if a.ID != in.Payment.ActionID {
				continue
			}
			if a.BaseOptionIndex != nil && *a.BaseOptionIndex < len(d.Options) {
				if card, key, ok := OptionKey(g, d, &d.Options[*a.BaseOptionIndex]); ok {
					mark(card, key)
				}
			} else if card, ok := objCard(g, a.Cast.Object); ok {
				mark(card, "cast")
			}
			return
		}
	}
	for _, ix := range in.Choices {
		if ix < 0 || ix >= len(d.Options) {
			continue
		}
		if card, key, ok := OptionKey(g, d, &d.Options[ix]); ok {
			mark(card, key)
		}
	}
}

// EndGame folds the game's scratch into the run's game counts.
func (c *Census) EndGame() {
	c.Games++
	for _, s := range c.gSlots {
		if c.seen[s.Card]&s.Zones != 0 {
			c.row(s.Card, s.Key).InDeckGames++
		}
	}
	for k := range c.gOff {
		c.rows[k].OfferedGames++
	}
	for k := range c.gPot {
		c.rows[k].PotentialGames++
	}
	for k := range c.gChose {
		c.rows[k].ChosenGames++
	}
}

// Chosen returns how often (card, key) was chosen so far (policy + explore),
// the under-sampling signal ExploreSeat ranks by.
func (c *Census) Chosen(card, key string) int {
	r := c.rows[rowKey(card, key)]
	if r == nil {
		return 0
	}
	return r.Chosen + r.ExploreChosen
}

// baseKey folds a cast/land-play variant ("cast/alt1", "cast/flashback",
// "play_land/modal_land") onto its universe slot.
func baseKey(k string) string {
	for _, b := range [...]string{"cast", "play_land"} {
		if strings.HasPrefix(k, b+"/") {
			return b
		}
	}
	return k
}

// ObservePotential tallies a priority decision's potential plays (the
// View's PotentialActions for the deciding seat).
func (c *Census) ObservePotential(g *state.Game, d *decision.Decision, pot []decision.PotentialAction) {
	if d.Kind != decision.KPriority {
		return
	}
	seen := map[string]bool{}
	for _, a := range pot {
		if a.Payable != nil && !*a.Payable {
			// The payment planner proved the cast unpayable from every
			// source the seat has: not a pool-priced miss.
			continue
		}
		opt := decision.Option{Kind: a.Kind, Obj: a.Obj, Ability: a.Ability, Mode: a.Mode}
		card, key, ok := OptionKey(g, d, &opt)
		if !ok {
			continue
		}
		for _, k := range [...]string{key, baseKey(key)} {
			if seen[rowKey(card, k)] {
				continue
			}
			seen[rowKey(card, k)] = true
			c.row(card, k).Potential++
			c.gPot[rowKey(card, k)] = true
		}
	}
}
