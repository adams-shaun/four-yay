package v1agent

import (
	_ "embed"
	"encoding/json"
)

// Filter is a card or player filter from gorge's IR (a target class, a
// counted class, a search type), reduced to this package's vocabulary.
// Types holds lowercase card types and subtypes, any of which match ("card"
// and "permanent" match anything of their zone); the flags narrow it.
type Filter struct {
	Types    []string `json:"types,omitempty"`
	Non      []string `json:"non,omitempty"`      // excluded types/subtypes ("creature", "land", "legendary")
	Colors   []string `json:"colors,omitempty"`   // any of these colours ("red", "blue")
	With     []string `json:"with,omitempty"`     // required keywords ("defender")
	Without  []string `json:"without,omitempty"`  // excluded keywords ("flying")
	You      bool     `json:"you,omitempty"`      // you control (or own)
	Opp      bool     `json:"opp,omitempty"`      // an opponent controls
	Other    bool     `json:"other,omitempty"`    // not the source itself
	Tapped   bool     `json:"tapped,omitempty"`   // tapped permanents only
	CMCLEX   bool     `json:"cmc_le_x,omitempty"` // mana value <= the effect's X
	Named    bool     `json:"named,omitempty"`    // a specific card name (a self-search)
	Attacker bool     `json:"attacking,omitempty"`
}

// Amount is a number an effect uses: fixed, or computed at resolution.
type Amount struct {
	N int `json:"n,omitempty"`
	// Kind is "" for the fixed N, else one of: count (permanents or cards
	// matching Of in Zone), metalcraft/landfall (Hi when the condition
	// holds, else Lo), x (the X paid), power (a creature's power), other.
	Kind string  `json:"kind,omitempty"`
	Zone string  `json:"zone,omitempty"` // count: battlefield, graveyard, hand
	Of   *Filter `json:"of,omitempty"`
	Mul  int     `json:"mul,omitempty"`
	Add  int     `json:"add,omitempty"`
	Hi   int     `json:"hi,omitempty"`
	Lo   int     `json:"lo,omitempty"`
}

// CostFact is an ability or alternative cost.
type CostFact struct {
	Mana        int     `json:"mana,omitempty"` // total mana symbols (generic + coloured)
	X           bool    `json:"x,omitempty"`
	Tap         bool    `json:"tap,omitempty"`
	SacSelf     bool    `json:"sac_self,omitempty"`
	Sac         *Filter `json:"sac,omitempty"` // sacrifice other permanents
	SacN        int     `json:"sac_n,omitempty"`
	DiscardSelf bool    `json:"discard_self,omitempty"`
	Discard     int     `json:"discard,omitempty"`
	ExileSelf   bool    `json:"exile_self,omitempty"`
	ExileGrave  int     `json:"exile_grave,omitempty"`
	Return      *Filter `json:"return,omitempty"` // return a permanent to hand
	TapOthers   int     `json:"tap_others,omitempty"`
	PayLife     int     `json:"pay_life,omitempty"`
	Reveal      bool    `json:"reveal,omitempty"`
	AddCounter  bool    `json:"add_counter,omitempty"`
	Other       bool    `json:"other,omitempty"` // a cost this vocabulary does not name
}

// EffectFact summarises one effect (a spell, an activated ability or a
// trigger's effect) from gorge's IR. The first link's fields describe the
// head effect; Chain lists the sub-ability APIs, and the rest of the
// fields collect the first value any link sets.
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

	// Richer facts (sb-generic).
	DamageX    *Amount      `json:"damage_x,omitempty"`    // a computed damage amount
	PumpX      *Amount      `json:"pump_x,omitempty"`      // a computed +X/+X
	ManaX      *Amount      `json:"mana_x,omitempty"`      // a computed mana amount
	TgtX       *Amount      `json:"tgt_x,omitempty"`       // the X a target filter compares against
	Tgt        *Filter      `json:"tgt,omitempty"`         // what it targets
	TgtMax     int          `json:"tgt_max,omitempty"`     // more than one target
	Each       *Filter      `json:"each,omitempty"`        // DamageAll/PumpAll/...: what it affects
	EachPlayer string       `json:"each_player,omitempty"` // DamageAll players: opp, all
	Defined    string       `json:"defined,omitempty"`     // you, opp, self, targeted, enchanted, ...
	Origin     string       `json:"origin,omitempty"`
	Dest       string       `json:"dest,omitempty"`
	Search     *Filter      `json:"search,omitempty"` // ChangeZone/Dig: what it finds
	Until      *Filter      `json:"until,omitempty"`  // DigUntil: stop at
	Cost       *CostFact    `json:"cost,omitempty"`
	Zone       string       `json:"zone,omitempty"`    // activation zone when not the battlefield
	Sorcery    bool         `json:"sorcery,omitempty"` // sorcery-speed activation
	Keywords   []string     `json:"kw,omitempty"`      // keywords granted
	Counter    string       `json:"counter,omitempty"` // counter type placed (p1p1, m1m1, stun)
	CounterN   int          `json:"counter_n,omitempty"`
	Unless     int          `json:"unless,omitempty"`  // Counter: soft unless its controller pays N
	Life       int          `json:"life,omitempty"`    // GainLife amount
	Tokens     int          `json:"tokens,omitempty"`  // tokens created
	Token      string       `json:"token,omitempty"`   // the token's name
	Scry       bool         `json:"scry,omitempty"`    // library selection (scry, surveil, rearrange)
	Discard    int          `json:"discard,omitempty"` // you discard N after (a loot)
	DiscardOpp bool         `json:"discard_opp,omitempty"`
	Mill       int          `json:"mill,omitempty"`
	Prevent    bool         `json:"prevent,omitempty"` // prevents damage
	Kicked     bool         `json:"kicked,omitempty"`  // trigger only when kicked
	Cond       string       `json:"cond,omitempty"`    // a resolution condition ("targeted_color")
	CondColor  string       `json:"cond_color,omitempty"`
	Modes      []EffectFact `json:"modes,omitempty"` // Charm / GenericChoice modes, in order
	Untap      bool         `json:"untap,omitempty"`
	Tap        bool         `json:"tap,omitempty"`
}

// StaticFact is a continuous effect or cost modifier the card has.
type StaticFact struct {
	Mode      string    `json:"mode"`              // continuous, reduce_cost, alt_cost, cant_block, ...
	Affects   string    `json:"affects,omitempty"` // self, equipped, enchanted, yours, all
	Power     int       `json:"power,omitempty"`
	Toughness int       `json:"toughness,omitempty"`
	Keywords  []string  `json:"kw,omitempty"`
	Reduce    *Amount   `json:"reduce,omitempty"`
	Cost      *CostFact `json:"cost,omitempty"` // alt_cost / optional_cost
	Cond      string    `json:"cond,omitempty"` // a condition this vocabulary names ("artifact_present")
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

	// Richer facts (sb-generic).
	Subtypes   []string     `json:"subtypes,omitempty"` // lowercase
	Legendary  bool         `json:"legendary,omitempty"`
	ETBTapped  bool         `json:"etb_tapped,omitempty"`
	ManaAbs    []EffectFact `json:"mana_abilities,omitempty"`
	Statics    []StaticFact `json:"statics,omitempty"`
	Flashback  *CostFact    `json:"flashback,omitempty"`
	Kicker     *CostFact    `json:"kicker,omitempty"`
	Ninjutsu   bool         `json:"ninjutsu,omitempty"`
	Changeling bool         `json:"changeling,omitempty"`
	Faces      []*CardFact  `json:"faces,omitempty"` // other faces (omen, adventure, transformed)
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

// HasSubtype reports a (lowercase) subtype; a changeling has them all.
func (c *CardFact) HasSubtype(t string) bool {
	if c == nil {
		return false
	}
	if c.Changeling {
		return true
	}
	for _, x := range c.Subtypes {
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

// Effects returns every non-mana effect: the spell, then abilities, then
// triggers.
func (c *CardFact) Effects() []*EffectFact {
	if c == nil {
		return nil
	}
	var out []*EffectFact
	if c.Spell != nil {
		out = append(out, c.Spell)
	}
	for i := range c.Abilities {
		out = append(out, &c.Abilities[i])
	}
	for i := range c.Triggers {
		out = append(out, &c.Triggers[i])
	}
	return out
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
