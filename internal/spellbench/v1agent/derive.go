package v1agent

import "sync"

// Hint-free card knowledge (sb-generic). Everything hints.go and the named
// branches of tactical.go say about a card is derived here from card data
// only: cardfacts.json (gorge's compiled IR) and kernelcards.json (the
// kernel card DB). No card or deck name appears in this file.

// play is one way a spell can be used: the spell itself, or one mode of a
// modal spell.
type play struct {
	role   Role
	eff    *EffectFact
	dmg    int     // fixed damage
	dmgX   *Amount // computed damage
	face   bool    // the damage can hit a player
	tgt    *Filter
	bounce bool // removal that does not last (bounce, tap, library)
	edict  bool // the opponent sacrifices (no target)
	pol    int
}

// profile is a card's derived knowledge. The embedded hint carries the
// fields hints.go writes by hand (role, ab, pol, bonus, tapped, dmg,
// flash, sacCost, selfLand); the rest replace named branches.
type profile struct {
	hint
	name string
	fact *CardFact
	kc   *KernelCard

	plays       []play           // the spell's uses, one per mode
	abBy        map[string]abUse // activated-ability use by the zone the source is in
	abEff       map[string]*EffectFact
	oneShotMana bool // a sacrifice-for-mana permanent (cast only to accelerate)
	engine      bool // a creature worth deploying early (spell-cast trigger, scaling mana)
	looter      bool // draws, then discards
	madness     bool
	altSacLands int  // alternative cost: sacrifice N lands
	altFree     bool // alternative cost without mana or sacrifice
	kickTeam    bool // the kicked effect pumps our creatures
	scry        bool // looks at the library top and bottoms what it does not want
	initiative  bool
	powerOnly   bool // a harmful -N/-0: value a target by its power
	reanimHand  bool
	reanimLife  bool
	millUntil   bool // mills a target player until a land (self-mill enabler)
	gyFinisher  bool // ETB damage to an opponent per creature card in our graveyard
	gyScaling   bool // gets better with cards in our graveyard (cost or damage)
	flashSacN   int  // flashback cost: sacrifice N creatures
	counterETB  *EffectFact
	pumpAb      *EffectFact // an activated +X/+X for a creature
	millTarget  bool        // a targeted mill (aim it by graveyard synergy)
}

var (
	profMu    sync.Mutex
	profCache = map[string]*profile{}
)

// profileFor derives (and caches) the profile of a card name.
func profileFor(name string) *profile {
	n := normName(name)
	profMu.Lock()
	defer profMu.Unlock()
	if p, ok := profCache[n]; ok {
		return p
	}
	p := derive(name)
	profCache[n] = p
	return p
}

func hasAPI(e *EffectFact, api string) bool {
	if e == nil {
		return false
	}
	if e.API == api {
		return true
	}
	for _, c := range e.Chain {
		if c == api {
			return true
		}
	}
	return false
}

func (f *Filter) has(t string) bool {
	if f == nil {
		return false
	}
	for _, x := range f.Types {
		if x == t {
			return true
		}
	}
	return false
}

func (f *Filter) only(t string) bool { return f != nil && len(f.Types) == 1 && f.Types[0] == t }

func (f *Filter) hasNon(t string) bool {
	if f == nil {
		return false
	}
	for _, x := range f.Non {
		if x == t {
			return true
		}
	}
	return false
}

// isLandSearch: a search whose finds are all lands (basic types, "land").
func isLandSearch(f *Filter) bool {
	if f == nil || len(f.Types) == 0 {
		return false
	}
	for _, t := range f.Types {
		switch t {
		case "land", "forest", "island", "swamp", "mountain", "plains", "gate":
		default:
			return false
		}
	}
	return true
}

// classify turns one effect into a play.
func classify(e *EffectFact) play {
	p := play{eff: e, tgt: e.Tgt, dmg: e.Damage, dmgX: e.DamageX, pol: 1}
	if e.Harmful {
		p.pol = -1
	}
	switch {
	case e.Prevent:
		p.role, p.pol = RoleProtect, 1
	case e.API == "Counter":
		p.role, p.pol = RoleCounter, -1
	case e.API == "DealDamage" && e.Defined != "you":
		p.role, p.pol = RoleBurn, -1
		p.face = e.Tgt == nil || e.Tgt.has("player")
		if e.Tgt != nil && e.Tgt.only("creature") {
			p.face = false
		}
	case e.API == "DamageAll":
		p.role, p.pol = RoleSweeper, -1
	case e.API == "Destroy" && e.Tgt.only("land") && hasAPI(e, "Draw") && hasAPI(e, "ChangeZone"):
		p.role, p.pol = RoleDraw, -1 // land destruction that refunds the land and draws
	case e.API == "Destroy" || e.API == "Sacrifice" ||
		(e.API == "ChangeZone" && e.Origin == "battlefield" && e.Dest != "battlefield" && e.Tgt != nil):
		p.role, p.pol = RoleRemoval, -1
		p.bounce = e.Dest == "hand" || e.Dest == "library"
		p.edict = e.API == "Sacrifice"
	case e.API == "Tap" && e.Tgt != nil:
		p.role, p.pol, p.bounce = RoleRemoval, -1, true
	case e.API == "Pump" && (e.PumpPower > 0 || e.PumpX != nil || (len(e.Keywords) > 0 && e.PumpPower >= 0)):
		p.role, p.pol = RolePump, 1
	case e.API == "Pump" && e.PumpToughness < 0:
		p.role, p.pol, p.face, p.dmg = RoleBurn, -1, false, -e.PumpToughness
	case e.API == "PutCounter" && (e.Counter == "p1p1" || e.Counter == ""):
		p.role, p.pol = RolePump, 1
	case e.API == "ChangeZone" && e.Origin == "graveyard" && (e.Dest == "battlefield" || e.Dest == "hand") && e.Tgt != nil:
		p.role, p.pol = RoleReanimate, 1
	case (e.API == "ChangeZone" || e.API == "Dig") && e.Origin == "library" && isLandSearch(e.Search):
		p.role, p.pol = RoleRamp, 1
	case e.API == "Token" || (hasAPI(e, "Token") && !hasAPI(e, "Draw")):
		p.role = RoleToken
	case e.API == "Draw" || e.API == "Dig" || e.API == "Scry" || e.API == "RearrangeTopOfLibrary" ||
		e.API == "Mill" || e.API == "ChooseType" || e.API == "Explore" || e.API == "Investigate" ||
		(e.API == "ChangeZone" && e.Origin == "library") || e.API == "Discard" || hasAPI(e, "Draw"):
		p.role = RoleDraw
		if e.API == "Discard" {
			p.pol = -1
		}
	case e.API == "ChangeZoneAll" && e.Origin == "graveyard":
		p.role, p.pol = RoleNone, -1 // graveyard hate
	case e.API == "Effect":
		p.role = RoleNever
	}
	return p
}

func derive(name string) *profile {
	p := &profile{name: name, fact: FactN(name), kc: KernelCardByName(name),
		abBy: map[string]abUse{}, abEff: map[string]*EffectFact{}}
	f, k := p.fact, p.kc
	if f == nil && k == nil {
		return p
	}
	isType := func(fact, kernel string) bool { return f.HasType(fact) || k.IsType(kernel) }
	creature := isType("creature", "Creature")
	land := isType("land", "Land")
	p.tapped = land && (f != nil && f.ETBTapped || k.Has("enters_tapped"))
	p.flash = f.HasKeyword("Flash")
	p.madness = f.HasKeyword("Madness")

	if f != nil {
		p.deriveSpell(f)
		p.deriveAbilities(f, creature)
		p.deriveTriggers(f)
		p.deriveStatics(f)
		if f.Flashback != nil && f.Flashback.Sac != nil && f.Flashback.Sac.has("creature") {
			p.flashSacN = f.Flashback.SacN
		}
	}
	switch {
	case land:
		p.role = RoleNone
	case creature:
		p.role = RoleCreature
		p.deriveBonus(f)
	case len(p.plays) > 0:
		p.role = bestRole(p.plays)
	default:
		p.derivePermanentRole(f, k)
	}
	p.pol = p.derivePolarity()
	for _, pl := range p.plays {
		if pl.role == RoleBurn {
			p.dmg = pl.dmg
			break
		}
	}
	return p
}

// bestRole picks the role a modal spell is filed under (hand values,
// searches): the strongest of its modes.
func bestRole(plays []play) Role {
	rank := func(r Role) int {
		switch r {
		case RoleCounter:
			return 9
		case RoleRemoval:
			return 8
		case RoleBurn:
			return 7
		case RoleSweeper:
			return 6
		case RoleReanimate, RoleProtect:
			return 5
		case RoleDraw, RoleToken, RoleRamp:
			return 4
		case RolePump:
			return 3
		case RoleNone:
			return 1
		case RoleNever:
			return 0
		}
		return 2
	}
	best := plays[0].role
	for _, pl := range plays[1:] {
		if rank(pl.role) > rank(best) {
			best = pl.role
		}
	}
	return best
}

func (p *profile) deriveSpell(f *CardFact) {
	e := f.Spell
	if e == nil {
		return
	}
	if len(e.Modes) > 0 {
		for i := range e.Modes {
			m := &e.Modes[i]
			pl := classify(m)
			if m.Cost != nil && m.Cost.Sac != nil {
				pl.role = RoleDraw // "sacrifice X: draw" modes
			}
			p.plays = append(p.plays, pl)
		}
	} else if e.API == "Attach" && f.HasSubtype("aura") {
		// an aura: harmful when its enchant-time effects hurt the creature
		harm := false
		for _, tr := range f.Triggers {
			if tr.Trigger == "ETB" && (tr.Tap || tr.Harmful) && tr.Defined == "enchanted" {
				harm = true
			}
		}
		for _, s := range f.Statics {
			if s.Affects == "enchanted" && (s.Power < 0 || s.Mode == "cant_block" || s.Mode == "cant_attack") {
				harm = true
			}
		}
		if harm {
			p.plays = append(p.plays, play{role: RoleRemoval, eff: e, tgt: e.Tgt, pol: -1, bounce: true})
		} else {
			p.plays = append(p.plays, play{role: RolePump, eff: e, tgt: e.Tgt, pol: 1})
		}
	} else if !f.HasType("creature") {
		pl := classify(e)
		if pl.role == RoleReanimate && e.Dest == "hand" {
			p.reanimHand = true
			p.reanimLife = hasAPI(e, "GainLife")
		}
		p.plays = append(p.plays, pl)
	}
	if e.Cost != nil && e.Cost.Sac != nil {
		p.sacCost = true
	}
	if e.API == "Destroy" && e.Tgt.only("land") && hasAPI(e, "ChangeZone") && hasAPI(e, "Draw") {
		p.selfLand = true // the land's controller gets a land back: aim at our own indestructible one
	}
	if e.Cards > 0 && e.Discard >= e.Cards {
		p.looter = true
	}
	if e.Scry {
		p.scry = true
	}
	if e.API == "DigUntil" && e.Until.has("land") && e.Dest == "graveyard" && e.Tgt.has("player") {
		p.millUntil = true
	}
	if e.Mill > 0 && e.Tgt.has("player") {
		p.millTarget = true
	}
	for _, s := range f.Statics {
		if s.Mode == "alt_cost" && s.Cost != nil {
			c := s.Cost
			switch {
			case c.Sac != nil && (c.Sac.has("land") || isLandSearch(c.Sac)):
				p.altSacLands = c.SacN
			case c.Mana == 0 && c.Sac == nil && !c.SacSelf && c.Discard == 0 && c.PayLife <= 2:
				p.altFree = true
			}
		}
	}
}

func (p *profile) deriveTriggers(f *CardFact) {
	for i := range f.Triggers {
		tr := &f.Triggers[i]
		if tr.Trigger == "ETB" && tr.API == "Counter" && p.counterETB == nil {
			p.counterETB = tr
		}
		if tr.Kicked && (tr.API == "PumpAll") && tr.Each.has("creature") && tr.Each.You {
			p.kickTeam = true
		}
		if tr.Scry {
			p.scry = true
		}
		if hasAPI(tr, "TakeInitiative") || hasAPI(tr, "VentureInto") {
			p.initiative = true
		}
		if tr.API == "Pump" && tr.Harmful && tr.PumpPower < 0 && tr.PumpToughness == 0 {
			p.powerOnly = true
		}
		if tr.API == "DigUntil" && tr.Until.has("land") && tr.Dest == "graveyard" && tr.Tgt.has("player") {
			p.millUntil = true
		}
		if tr.Trigger == "ETB" && tr.API == "DealDamage" && tr.DamageX != nil && tr.DamageX.Kind == "count" &&
			tr.DamageX.Zone == "graveyard" && tr.DamageX.Of.has("creature") {
			p.gyFinisher = true
		}
		if tr.Mill > 0 && tr.Tgt.has("player") {
			p.millTarget = true
		}
	}
	if f.Spell != nil && f.Spell.DamageX != nil && f.Spell.DamageX.Zone == "graveyard" {
		p.gyScaling = true
	}
	if p.gyFinisher {
		p.gyScaling = true
	}
}

func (p *profile) deriveStatics(f *CardFact) {
	for _, s := range f.Statics {
		if s.Mode == "reduce_cost" && s.Reduce != nil && s.Reduce.Kind == "count" && s.Reduce.Zone == "graveyard" {
			p.gyScaling = true
		}
	}
}

// abUseFor classifies one activated ability.
func abUseFor(f *CardFact, e *EffectFact) abUse {
	c := e.Cost
	if c == nil {
		c = &CostFact{}
	}
	switch {
	case f.Ninjutsu && e.Zone == "hand":
		return abNinjutsu
	case e.API == "Attach" && e.Tgt != nil && e.Tgt.You:
		return abEquip
	case e.Zone == "hand" && c.DiscardSelf && (e.API == "Draw" || (e.API == "ChangeZone" && e.Origin == "library")):
		return abCycle
	case e.Zone == "hand":
		return abNever
	case e.API == "Pump" && (e.PumpPower > 0 || e.PumpX != nil):
		return abCombat
	case e.API == "DamageAll":
		return abShaman
	case e.API == "DealDamage" && e.Tgt.has("player") && (c.Sac != nil || c.SacSelf):
		return abPing
	case e.Harmful && e.Tgt != nil && (e.API == "PutCounter" || e.API == "Destroy" || e.API == "Tap"):
		return abStun
	case (e.API == "Draw" && (e.Discard > 0 || c.Discard > 0 || c.DiscardSelf)):
		return abLoot
	case e.Untap, e.API == "ChangeZoneAll", e.API == "DealDamage", e.API == "ChangeZone" && e.Zone == "":
		// untappers, graveyard hate, conditional pings: keep for the
		// engine's own uses (mana), never spend voluntarily -- except a
		// land fetch, and returning our own cards from the graveyard
		if e.API == "ChangeZone" && (isLandSearch(e.Search) || (e.Origin == "graveyard" && e.Dest == "hand")) {
			if e.Sorcery {
				return abMain2
			}
			return abEndStep
		}
		return abNever
	case e.API == "Token" || e.API == "GainLife" || e.API == "Draw" || e.API == "CopyPermanent" ||
		e.API == "Explore" || e.API == "ChangeZone":
		if e.Sorcery {
			return abMain2
		}
		return abEndStep
	}
	return abNever
}

func (p *profile) deriveAbilities(f *CardFact, creature bool) {
	for i := range f.Abilities {
		e := &f.Abilities[i]
		zone := e.Zone
		if zone == "" {
			zone = "battlefield"
		}
		if _, ok := p.abBy[zone]; ok {
			continue
		}
		u := abUseFor(f, e)
		p.abBy[zone], p.abEff[zone] = u, e
		if u == abCombat && p.pumpAb == nil {
			p.pumpAb = e
		}
	}
	p.ab = p.abBy["battlefield"]
	if u, ok := p.abBy["hand"]; ok && u != abNever {
		p.ab = u
	}
	for _, m := range f.ManaAbs {
		if m.Cost != nil && m.Cost.SacSelf && !creature && !f.HasType("land") && len(f.Abilities) == 0 {
			p.oneShotMana = true
		}
	}
}

func (p *profile) deriveBonus(f *CardFact) {
	if f == nil {
		return
	}
	for _, m := range f.ManaAbs {
		if m.Cost != nil && m.Cost.SacSelf {
			p.bonus -= 0.5 // a disposable mana body
			continue
		}
		if m.ManaX != nil && m.ManaX.Kind == "count" {
			p.bonus += 2
			p.engine = true
		} else {
			p.bonus += 0.5
		}
		break
	}
	for _, tr := range f.Triggers {
		if tr.Trigger == "SpellCast" && tr.API == "DealDamage" {
			p.bonus += 2
			p.engine = true
		}
		if tr.Trigger == "SpellCast" && tr.API == "Token" {
			p.bonus++
		}
		if tr.Trigger == "DamageDone" && hasAPI(&tr, "Draw") {
			p.bonus++
		}
		if hasAPI(&tr, "TakeInitiative") {
			p.bonus++
		}
	}
	if p.pumpAb != nil && p.pumpAb.PumpX != nil {
		p.bonus++
	}
	if f.HasKeyword("Protection") {
		p.bonus += 2
	}
	for _, s := range f.Statics {
		if s.Mode == "cant_block" {
			p.bonus++ // evasion: hard to block
		}
	}
}

func (p *profile) derivePermanentRole(f *CardFact, k *KernelCard) {
	if f == nil {
		if k.Has("draw_card") {
			p.role = RoleDraw
		} else {
			p.role = RoleArtifact
		}
		return
	}
	if p.oneShotMana {
		p.role = RoleNever
		return
	}
	for i := range f.Triggers {
		tr := &f.Triggers[i]
		if tr.Trigger == "ETB" && tr.Tgt != nil {
			pl := classify(tr)
			if pl.role == RoleRemoval {
				p.plays = append(p.plays, pl)
				p.role = RoleRemoval
				return
			}
		}
	}
	if len(f.Abilities) == 0 && len(f.Triggers) == 0 && k.Has("draw_card") && f.HasType("enchantment") {
		p.role = RoleDraw // a saga: chapters draw
		return
	}
	p.role = RoleArtifact
}

// derivePolarity: harmful (-1) when a targeted effect of the card hurts
// what it targets, else beneficial. A removal, burn, counter or sweeper
// role is harmful whatever the IR flags say.
func (p *profile) derivePolarity() int {
	switch p.role {
	case RoleBurn, RoleRemoval, RoleCounter, RoleSweeper:
		return -1
	case RolePump, RoleReanimate, RoleProtect:
		return 1
	}
	f := p.fact
	if f == nil {
		return 1
	}
	targeted, harm := false, false
	for _, e := range f.Effects() {
		for _, m := range append([]EffectFact{*e}, e.Modes...) {
			if m.Tgt == nil {
				continue
			}
			targeted = true
			if m.Harmful || (m.API == "ChangeZoneAll" && m.Origin == "graveyard") {
				harm = true
			}
		}
	}
	if !targeted {
		for _, e := range f.Effects() {
			harm = harm || e.Harmful
		}
	}
	if harm {
		return -1
	}
	return 1
}

// ---- counting over the board ----

// matchCard reports whether card c (controlled by mine) matches filter f.
func matchCard(f *Filter, c *KCard, mine bool) bool {
	if f == nil {
		return true
	}
	if f.You && !mine || f.Opp && mine {
		return false
	}
	if f.Tapped && !c.Tapped {
		return false
	}
	fact := FactN(c.Name)
	is := func(t string) bool {
		ty := &c.Characteristics.Types
		switch t {
		case "card", "permanent":
			return true
		case "creature":
			return ty.Creature
		case "land":
			return ty.Land
		case "artifact":
			return ty.Artifact
		case "enchantment":
			return ty.Enchantment
		case "instant":
			return ty.Instant
		case "sorcery":
			return ty.Sorcery
		}
		return fact.HasSubtype(t)
	}
	ok := len(f.Types) == 0
	for _, t := range f.Types {
		if is(t) {
			ok = true
		}
	}
	if !ok {
		return false
	}
	for _, t := range f.Non {
		if is(t) {
			return false
		}
	}
	kwOf := func(k string) bool {
		kw := Kw(c)
		switch k {
		case "flying":
			return kw.Flying
		case "defender":
			return kw.Defender
		case "reach":
			return kw.Reach
		case "trample":
			return kw.Trample
		}
		return false
	}
	for _, k := range f.With {
		if !kwOf(k) {
			return false
		}
	}
	for _, k := range f.Without {
		if kwOf(k) {
			return false
		}
	}
	return true
}

// evalAmount evaluates a at the board for an effect controlled by
// controller mine (true: us). ok is false when the amount cannot be known
// (an X, a creature's power).
func evalAmount(a *Amount, b *Board, mine bool) (n int, ok bool) {
	if a == nil {
		return 0, false
	}
	switch a.Kind {
	case "":
		return a.N, true
	case "count":
		switch a.Zone {
		case "battlefield":
			sides := []struct {
				cs   []*KCard
				mine bool
			}{{b.Mine, true}, {b.Theirs, false}}
			for _, s := range sides {
				ctrlMine := s.mine == mine // "you" is the effect's controller
				for _, c := range s.cs {
					if matchCard(a.Of, c, ctrlMine) {
						n++
					}
				}
			}
		case "graveyard":
			g := b.MyGrave
			if !mine {
				g = b.TheirGrave
			}
			for i := range g {
				if matchCard(a.Of, &g[i], true) {
					n++
				}
			}
		case "hand":
			if !mine {
				return 0, false
			}
			for _, h := range b.Hand {
				if k := KernelCardByName(h.Name); k != nil {
					c := &KCard{Name: h.Name}
					setTypes(c, k)
					if matchCard(a.Of, c, true) {
						n++
					}
				}
			}
		}
	case "metalcraft":
		arts := 0
		for _, c := range sideOf(b, mine) {
			if c.Characteristics.Types.Artifact {
				arts++
			}
		}
		if arts >= 3 {
			return a.Hi, true
		}
		return a.Lo, true
	case "landfall":
		if mine && b.LandsPlayed > 0 {
			return a.Hi, true
		}
		return a.Lo, true
	default:
		return 0, false
	}
	if a.Mul > 0 {
		n *= a.Mul
	}
	return n + a.Add, true
}

func sideOf(b *Board, mine bool) []*KCard {
	if mine {
		return b.Mine
	}
	return b.Theirs
}

// setTypes fills a synthetic card's type flags from the kernel DB.
func setTypes(c *KCard, k *KernelCard) {
	ty := &c.Characteristics.Types
	ty.Land, ty.Creature, ty.Instant = k.IsType("Land"), k.IsType("Creature"), k.IsType("Instant")
	ty.Sorcery, ty.Artifact, ty.Enchantment = k.IsType("Sorcery"), k.IsType("Artifact"), k.IsType("Enchantment")
}

// playDamage is the damage a play deals now (0 when unknown).
func playDamage(pl *play, b *Board, mine bool) int {
	if pl.dmg > 0 {
		return pl.dmg
	}
	if n, ok := evalAmount(pl.dmgX, b, mine); ok {
		return n
	}
	if pl.dmgX != nil && pl.dmgX.Kind == "power" && mine {
		// a creature's power (revealed from hand, or one of ours): the
		// biggest we have
		best := 0
		for _, c := range Creatures(b.Mine) {
			if c.Power() > best {
				best = c.Power()
			}
		}
		for _, h := range b.Hand {
			if f := FactN(h.Name); f != nil && f.HasPT && f.Power > best {
				best = f.Power
			}
		}
		return best
	}
	return 0
}

// ---- deck style from the decklist ----

// deckStyle reads a decklist's plan from its cards: burnDeck when it runs
// a lot of face damage, aggro when it is a low-curve creature-and-burn
// deck.
type deckStyle struct {
	burn, aggro bool
	known       bool
}

func styleOf(deck map[string]int) deckStyle {
	if deck == nil {
		return deckStyle{}
	}
	nonland, power, faceDmg, defenders, interaction := 0, 0, 0, 0, 0
	for name, n := range deck {
		p := profileFor(name)
		if p.kc != nil && p.kc.IsType("Land") || p.fact.HasType("land") {
			continue
		}
		nonland += n
		if p.role == RoleCreature && p.fact != nil {
			power += p.fact.Power * n
			if p.fact.HasKeyword("Defender") {
				defenders += n
			}
			if p.engine && !p.fact.Mana {
				faceDmg += 2 * n // a spell-cast face-damage engine
			}
		}
		switch p.role {
		case RoleRemoval, RoleCounter:
			interaction += n
		}
		for _, pl := range p.plays {
			if pl.role == RoleBurn && pl.face {
				d := pl.dmg
				if d == 0 && pl.dmgX != nil {
					d = pl.dmgX.Lo
				}
				faceDmg += d * n
				break
			}
		}
	}
	if nonland == 0 {
		return deckStyle{}
	}
	st := deckStyle{known: true}
	// burn: the deck's face damage is at least 0.4 per nonland card
	st.burn = faceDmg*10 >= nonland*4
	// aggro: 0.8 attacking power-or-burn per nonland card, under one
	// removal/counter in ten, few walls
	st.aggro = (power+faceDmg)*10 >= nonland*8 && interaction*10 < nonland && defenders*100 < nonland*15
	return st
}
