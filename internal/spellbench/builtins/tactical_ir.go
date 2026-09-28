package builtins

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// This file is sb-tactical's card knowledge: a small, conservative reading of
// gorge's card IR (cards.Face abilities, triggers and SVars) into effect
// labels with a DIRECTION. A label says what an effect does to the thing it
// touches (a creature dies, a player draws, a creature grows), so the scorer
// can point harm at the opponent's side and help at its own, and so the
// priority scorer can tell a counterspell from a cantrip from a combat
// trick. It reads printed card facts only -- a card's name resolves the same
// IR for every seat -- never hidden game state.
//
// Anything not recognised is effOther, which every consumer treats as "no
// opinion" (the tactical seat then defers to gorge's default policy).

type effClass uint8

const (
	effOther     effClass = iota
	effDraw               // a player draws (amount = cards)
	effSelect             // scry / surveil / look-and-rearrange: card quality
	effTutor              // library -> hand
	effRamp               // mana: a land onto the battlefield, a mana ability, ritual mana
	effToken              // creates tokens (amount = count)
	effClue               // a clue / blood / map style card-for-mana token
	effRemoval            // a permanent leaves the battlefield (destroy / exile / bounce)
	effDamage             // damage to a target (amount = damage)
	effDamageAll          // damage to each creature and/or each opponent
	effDrain              // damage / life loss to each opponent, untargeted
	effCounter            // counter a spell
	effPump               // +N/+N or a beneficial keyword (target or self)
	effPumpAll            // a team pump
	effDebuff             // -N/-N or -N/-0 (a curse pump)
	effLifeGain           // gain life
	effDiscard            // a target player (opponent) discards
	effLoot               // the caster discards (a cost-like rider)
	effTap                // tap a target permanent
	effUntap              // untap a target (or lands: a refund)
	effRecursion          // graveyard -> hand / battlefield
	effGraveHate          // exile a graveyard
	effMill               // a target player mills (Balustrade Spy)
	effCheat              // put this card onto the battlefield (ninjutsu)
	effAttach             // equip / aura onto a creature
	effInitiative         // take the initiative (a strong card-advantage engine)
	effFog                // prevent combat damage this turn
)

// polarity is an effect's direction relative to the permanent or player it
// touches: +1 the recipient benefits, -1 it is harmed, 0 no opinion.
func (c effClass) polarity() int {
	switch c {
	case effRemoval, effDamage, effCounter, effDebuff, effDiscard, effTap, effMill, effGraveHate:
		return -1
	case effPump, effLifeGain, effUntap, effRecursion, effAttach, effDraw:
		return +1
	}
	return 0
}

// tEffect is one labelled effect of an ability chain.
type tEffect struct {
	class    effClass
	api      string
	amount   int32 // the literal amount (damage, cards, tokens, life); 0 when unknown
	known    bool  // amount is a literal
	att, def int32 // Pump / PutCounter P/T change (literal parts only)
	kw       string
	targeted bool   // ValidTgts$ present
	valid    string // ValidTgts$ / ValidCards$
	players  bool   // the target list admits players (Any / Player / Opponent)
	bounce   bool   // a removal that returns the permanent to hand
	lands    bool   // an untap / ramp of lands
	oppOnly  bool   // ValidTgts names only the opponent's side (OppCtrl / Opponent)
	ownOnly  bool   // ValidTgts names only the caster's side (YouCtrl / YouOwn)
	curse    bool   // IsCurse$ True
	rider    bool   // DefinedPlayer$ TargetedController: a rider that benefits the target's controller
	kicked   bool   // from a trigger that fires only when the spell was kicked
	self     bool   // Defined$ Self / You (not targeted)
	unless   bool   // a soft counter (UnlessCost$)
	unlessN  int32  // the UnlessCost$ mana
	xExpr    string // the SVar body a variable amount names ("Count$Valid Elf")
	toBattlefield bool // a recursion that returns to the battlefield (reanimation)
}

// tAbility is one activated ability (a Face.Abilities entry).
type tAbility struct {
	effects  []tEffect
	cost     string
	tapCost  bool
	sacSelf  bool
	manaCost int32 // generic + coloured mana symbols in the cost
	sorcery  bool
	fromHand bool // ActivationZone$ Hand (cycling, ninjutsu)
	fromGY   bool // ActivationZone$ Graveyard (embalm)
	ninjutsu bool
	mana     bool // a mana ability
	limit1   bool // ActivationLimit$ 1
}

// tProfile is one card's labelled profile.
type tProfile struct {
	known                               bool
	name                                string
	creature, land, instant, sorcery    bool
	flash, haste, defender, permanent   bool
	power, toughness                    int32
	cmc                                 int32
	flying, reach, deathtouch, lifelink bool
	trample, firstStrike, doubleStrike  bool
	vigilance, menace, hexproof, indest bool
	spell                               []tEffect // the SP$ chain (instants, sorceries, and any permanent's cast)
	etb                                 []tEffect // enter / cast-trigger effects (Card.Self)
	dies                                []tEffect // leaves-the-battlefield value (Outlaw Medic, Ichor Wellspring)
	abilities                           []tAbility
	playMain1                           bool // Forge's AI hint SVar:PlayMain1
	manaSource                          bool // an activated mana ability on a permanent
	engine                              bool // a recurring trigger (SpellCast, DamageDone, ...)
	flashback                           bool
	spellCost                           string // the SP$ Cost$ (additional costs: Sac<>, Discard<>)
	altCost                             string // an AlternativeCost's cost (Fireblast: Sac<2/Mountain>)
	flashbackCost                       string // the Flashback keyword's cost
	affinity                            bool
	xCost                               bool
	consumable                          bool // a noncreature, nonland permanent with a sacrifice-itself ability
}

// reactive reports whether a card's natural use is on the opponent's turn or
// in response: a counterspell, an instant-speed removal/burn/trick, or a
// flash creature. The timing group holds these rather than spending them
// proactively in its own main phase.
func (p *tProfile) reactive() bool {
	if !p.instant && !p.flash {
		return false
	}
	if p.flash && p.creature {
		return true
	}
	for _, e := range p.spell {
		switch e.class {
		case effCounter, effRemoval, effDamage, effPump, effDebuff, effDraw, effSelect, effTap, effFog:
			return true
		}
	}
	return false
}

func (p *tProfile) has(c effClass) bool {
	for _, e := range p.spell {
		if e.class == c {
			return true
		}
	}
	for _, e := range p.etb {
		if e.class == c {
			return true
		}
	}
	return false
}

// profileOf labels a registry card. A nil card is an unknown profile.
func profileOf(c *cards.Card) *tProfile {
	p := &tProfile{}
	if c == nil || len(c.Faces) == 0 || c.Faces[0] == nil {
		return p
	}
	f := c.Faces[0]
	p.known = true
	p.name = f.Name
	p.creature = f.IsCreature()
	p.land = f.IsLand()
	p.instant = f.IsInstant()
	p.sorcery = f.IsSorcery()
	p.permanent = f.IsPermanent()
	p.power, p.toughness = int32(f.Power()), int32(f.Toughness())
	p.cmc = f.Cmc()
	p.xCost = strings.Contains(" "+f.ManaCost+" ", " X ")
	for _, k := range f.Keywords {
		switch strings.ToLower(cards.KeywordHead(k)) {
		case "flash":
			p.flash = true
		case "haste":
			p.haste = true
		case "defender":
			p.defender = true
		case "flying":
			p.flying = true
		case "reach":
			p.reach = true
		case "deathtouch":
			p.deathtouch = true
		case "lifelink":
			p.lifelink = true
		case "trample":
			p.trample = true
		case "first strike":
			p.firstStrike = true
		case "double strike":
			p.doubleStrike = true
		case "vigilance":
			p.vigilance = true
		case "menace":
			p.menace = true
		case "hexproof":
			p.hexproof = true
		case "indestructible":
			p.indest = true
		case "flashback":
			p.flashback = true
			if i := strings.IndexByte(k, ':'); i >= 0 {
				p.flashbackCost = k[i+1:]
			}
		case "affinity":
			p.affinity = true
		}
	}
	if v, ok := f.SVars["PlayMain1"]; ok && strings.EqualFold(strings.TrimSpace(v), "TRUE") {
		p.playMain1 = true
	}
	p.abilities = make([]tAbility, len(f.Abilities))
	for i, a := range f.Abilities {
		if a == nil {
			continue
		}
		if a.Kind == "SP" {
			if p.spell == nil {
				p.spell = chainEffects(f, a, false)
				p.spellCost = a.Params["Cost"]
			}
			continue
		}
		ab := tAbility{effects: chainEffects(f, a, false), cost: a.Params["Cost"]}
		ab.mana = a.API == "Mana"
		ab.tapCost = hasTapCost(ab.cost)
		ab.sacSelf = strings.Contains(ab.cost, "Sac<1/CARDNAME")
		ab.manaCost = costManaSymbols(ab.cost)
		ab.sorcery = strings.EqualFold(a.Params["SorcerySpeed"], "True")
		ab.limit1 = a.Params["ActivationLimit"] == "1"
		switch a.Params["ActivationZone"] {
		case "Hand":
			ab.fromHand = true
		case "Graveyard":
			ab.fromGY = true
		}
		ab.ninjutsu = a.Params["Keyword"] == "Ninjutsu"
		if ab.mana && p.permanent && !p.land && !ab.sacSelf {
			p.manaSource = true
		}
		if ab.sacSelf && p.permanent && !p.creature && !p.land {
			p.consumable = true
		}
		p.abilities[i] = ab
	}
	for i := range f.Triggers {
		t := &f.Triggers[i]
		valid := t.Params["ValidCard"]
		self := strings.HasPrefix(valid, "Card.Self")
		switch {
		case t.Mode == "ChangesZone" && self && t.Params["Destination"] == "Battlefield":
			for _, e := range chainEffects(f, t.Effect, strings.Contains(valid, "+kicked")) {
				p.etb = append(p.etb, e)
			}
		case t.Mode == "SpellCast" && self:
			p.etb = append(p.etb, chainEffects(f, t.Effect, false)...)
		case t.Mode == "ChangesZone" && self && t.Params["Origin"] == "Battlefield":
			p.dies = append(p.dies, chainEffects(f, t.Effect, false)...)
		case t.Mode == "SpellCast" || t.Mode == "DamageDone" || t.Mode == "Attacks" || t.Mode == "Sacrificed":
			p.engine = true
		}
	}
	for i := range f.Statics {
		st := &f.Statics[i]
		if st.Mode == "AlternativeCost" {
			p.altCost = st.Params["Cost"]
		}
	}
	return p
}

// chainEffects labels an ability and its SubAbility$ chain.
func chainEffects(f *cards.Face, sa *cards.SA, kicked bool) []tEffect {
	var out []tEffect
	for depth := 0; sa != nil && depth < 12; depth, sa = depth+1, sa.Sub {
		switch sa.API {
		case "Charm", "GenericChoice":
			// A modal spell: the modes' union, each marked so the scorer can
			// take the best one (the KModes answer is gorge's default policy).
			for _, name := range strings.Split(sa.Params["Choices"], ",") {
				if m := cards.ResolveSVar(f.SVars, strings.TrimSpace(name)); m != nil {
					out = append(out, chainEffects(f, m, kicked)...)
				}
			}
			continue
		}
		e := labelSA(sa)
		e.kicked = kicked
		if sa.API == "Fog" || sa.API == "Effect" && strings.Contains(f.SVars[sa.Params["ReplacementEffects"]], "Prevent$ True") {
			e.class = effFog
		}
		if !e.known || e.class == effPump && (e.att == 2 && strings.Contains(sa.Params["NumAtt"], "X")) {
			for _, k := range []string{"NumDmg", "NumCards", "LifeAmount", "NumAtt", "Amount"} {
				if v := strings.TrimPrefix(sa.Params[k], "+"); v != "" {
					if _, lit := literal(v); !lit {
						e.xExpr = f.SVars[v]
					}
					break
				}
			}
		}
		if strings.Contains(e.valid, "cmcLEX") {
			e.xExpr = f.SVars["X"] // "mana value X or less": X is the card's own SVar
		}
		if e.class != effOther || sa.API != "" {
			out = append(out, e)
		}
	}
	return out
}

// labelSA labels one SA (no sub-chain).
func labelSA(sa *cards.SA) tEffect {
	pm := sa.Params
	e := tEffect{api: sa.API, valid: pm["ValidTgts"]}
	e.targeted = e.valid != ""
	e.curse = strings.EqualFold(pm["IsCurse"], "True")
	e.rider = pm["DefinedPlayer"] == "TargetedController"
	def := pm["Defined"]
	e.self = def == "Self" || def == "You" || strings.HasPrefix(def, "TriggeredCardController")
	e.unless = pm["UnlessCost"] != ""
	e.unlessN = costManaSymbols(pm["UnlessCost"])
	v := e.valid
	e.players = v == "Any" || strings.Contains(v, "Player") || strings.Contains(v, "Opponent")
	e.oppOnly = strings.Contains(v, "OppCtrl") || v == "Opponent" || strings.Contains(v, "Player.Opponent")
	e.ownOnly = strings.Contains(v, "YouCtrl") || strings.Contains(v, "YouOwn")
	amt := func(keys ...string) {
		for _, k := range keys {
			if s, ok := pm[k]; ok {
				e.amount, e.known = literal(s)
				return
			}
		}
		e.amount, e.known = 1, true
	}
	switch sa.API {
	case "Draw":
		e.class = effDraw
		amt("NumCards")
		if strings.Contains(def, "Opponent") {
			e.class = effOther
		}
	case "Scry", "Surveil", "RearrangeTopOfLibrary", "PeekAndReveal", "Explore":
		e.class = effSelect
	case "Dig", "DigUntil":
		if e.curse || (e.targeted && e.players) {
			e.class = effMill
			break
		}
		e.class = effDraw
		n, ok := literal(pm["DigNum"])
		if !ok || n < 1 {
			n = 2
		}
		switch {
		case pm["ChangeValid"] != "" && pm["ChangeNum"] == "All":
			e.amount = (n + 1) / 2 // Winding Way: about half the cards are the chosen type
		case pm["ChangeValid"] != "":
			e.amount = (n + 2) / 3 // Lead the Stampede: the creatures among n
		case pm["ChangeNum"] == "All":
			e.amount = n // impulse draw
		default:
			e.amount = 1
		}
		e.known = true
	case "ChangeZone", "ChangeZoneAll":
		origin, dest := pm["Origin"], pm["Destination"]
		switch {
		case strings.Contains(origin, "Battlefield") && (e.targeted || sa.API == "ChangeZoneAll") && def == "":
			e.class = effRemoval
			e.bounce = dest == "Hand"
		case strings.Contains(origin, "Library") && dest == "Hand":
			e.class = effTutor
			amt("ChangeNum")
		case strings.Contains(origin, "Library") && dest == "Battlefield":
			e.class = effTutor
			if strings.Contains(pm["ChangeType"], "Land") || strings.Contains(pm["ChangeType"], "Forest") ||
				strings.Contains(pm["ChangeType"], "Basic") {
				e.class = effRamp
				e.lands = true
			}
			amt("ChangeNum")
		case strings.Contains(origin, "Graveyard") && (dest == "Hand" || dest == "Battlefield"):
			e.class = effRecursion
			e.toBattlefield = dest == "Battlefield"
			amt("TargetMax", "ChangeNum")
		case strings.Contains(origin, "Graveyard") && dest == "Exile":
			e.class = effGraveHate
		case origin == "Hand" && dest == "Battlefield":
			e.class = effCheat
		case origin == "Hand" && dest == "Exile" && e.curse:
			e.class = effDiscard
		}
	case "Destroy", "DestroyAll", "Sacrifice", "SacrificeAll":
		e.class = effRemoval
		if sa.API == "Sacrifice" && !strings.Contains(def, "Opponent") && !e.players {
			e.class = effOther // a self-sacrifice rider
		}
	case "DealDamage":
		e.class = effDamage
		amt("NumDmg")
		switch {
		case strings.Contains(def, "Opponent"):
			e.class = effDrain
		case def == "You":
			e.class = effOther
		}
	case "DamageAll":
		e.class = effDamageAll
		e.valid = pm["ValidCards"]
		amt("NumDmg")
		if e.valid == "" && strings.Contains(pm["ValidPlayers"], "Opponent") {
			e.class = effDrain
		}
	case "LoseLife":
		e.class = effDrain
		amt("LifeAmount")
	case "Counter":
		e.class = effCounter
	case "Pump", "PumpAll":
		e.att, _ = literal(pm["NumAtt"])
		e.def, _ = literal(pm["NumDef"])
		e.kw = pm["KW"]
		switch {
		case e.curse || e.att < 0 || e.def < 0:
			e.class = effDebuff
		case sa.API == "PumpAll":
			e.class = effPumpAll
			e.valid = pm["ValidCards"]
		default:
			e.class = effPump
			// "+X" reads 0 above; a variable pump is still a pump.
			if strings.Contains(pm["NumAtt"], "X") || strings.Contains(pm["NumDef"], "X") {
				e.att, e.def = 2, 2
			}
		}
	case "PutCounter", "PutCounterAll":
		n, _ := literal(pm["CounterNum"])
		if n < 1 {
			n = 1
		}
		switch pm["CounterType"] {
		case "P1P1":
			e.class, e.att, e.def = effPump, n, n
		case "M1M1":
			e.class, e.att, e.def = effDebuff, -n, -n
		case "STUN":
			e.class = effTap
		}
	case "Token", "CopyPermanent":
		e.class = effToken
		amt("TokenAmount", "NumCopies")
		if s := pm["TokenScript"]; strings.HasPrefix(s, "c_a_") {
			e.class = effClue
		}
	case "Investigate":
		e.class, e.amount, e.known = effClue, 1, true
	case "Mana":
		e.class = effRamp
	case "GainLife":
		e.class = effLifeGain
		amt("LifeAmount")
	case "Discard":
		e.class = effDiscard
		amt("NumCards")
		if def == "You" {
			e.class = effLoot
		}
	case "Tap", "TapAll":
		e.class = effTap
	case "Untap", "UntapAll":
		e.class = effUntap
		e.lands = pm["UntapType"] == "Land"
		amt("Amount")
	case "Attach":
		e.class = effAttach
	case "TakeInitiative":
		e.class = effInitiative
	}
	return e
}

// literal parses a Forge amount ("3", "+2", "-2"); a variable ("X",
// "+X") reports false.
func literal(s string) (int32, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "+")
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return int32(n), true
}

func hasTapCost(cost string) bool {
	for _, f := range strings.Fields(cost) {
		if f == "T" {
			return true
		}
	}
	return false
}

// costManaSymbols counts the mana symbols of an ability cost ("2 U T" -> 3).
func costManaSymbols(cost string) int32 {
	var n int32
	for _, f := range strings.Fields(cost) {
		if strings.ContainsAny(f, "<>") || f == "T" || f == "Q" {
			continue
		}
		if k, err := strconv.Atoi(f); err == nil {
			n += int32(k)
			continue
		}
		if len(f) <= 3 && strings.Trim(f, "WUBRGCP/") == "" {
			n++
		}
	}
	return n
}
