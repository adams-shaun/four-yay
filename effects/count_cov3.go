package effects

import (
	"slices"
	"strings"

	"github.com/adams-shaun/gorge/state"
)

// The count heads the cardfuzz coverage audit (fuzz-cov3) found unmodelled
// while their cards sat in the "supported" pool -- every one a replay-safe
// read of the event-derived state (zone lists, g.Entered, the player
// records) or a Host log fold, never the wall clock or ambient randomness.
// evalCov3Head is consulted by evalCountBody's switch fallthrough; ok=false
// means "not one of these heads", so the rest of the dispatch keeps its
// verdict.

// basicLandTypes is CR 305.6's five basic land types in WUBRG order (the
// order is only a fixed walk; the count reads it as a set).
var basicLandTypes = [...]string{"Plains", "Island", "Swamp", "Mountain", "Forest"}

// domainCount is the "domain" ability word's count: the number of basic land types among the
// lands player p controls -- counted through the same Count$Valid machinery
// (a `<Type>.YouCtrl` spec from p's perspective) that every other
// battlefield census reads, so a layer-granted land type counts exactly as a
// printed one does.
func domainCount(h Host, c *Ctx, p state.PlayerID, depth int) int32 {
	if p < 0 {
		return 0
	}
	sub := *c
	sub.Controller = p
	var n int32
	for _, t := range basicLandTypes {
		if v, ok := evalCountBody(h, &sub, "Valid "+t+".YouCtrl", depth+1); ok && v > 0 {
			n++
		}
	}
	return n
}

// evalCov3Head answers the fuzz-cov3 heads. head/arg are evalCountBody's
// head/argument split.
func evalCov3Head(h Host, c *Ctx, head, arg string, depth int) (int32, bool) {
	g := h.Game()
	switch evalCov3Headefd1Codes.Code(string(head)) {
	case evalCov3Headefd1Domain:
		// "for each basic land type among lands you control" (62 corpus
		// carriers: Tribal Flames, Draco's cost reduction, Allied Strategies).
		return domainCount(h, c, c.Controller, depth), true
	case evalCov3Headefd1DomainActivePlayer:
		// The same census for the ACTIVE player (Collapsing Borders' upkeep
		// life gain, Mask of Intolerance).
		return domainCount(h, c, g.Active, depth), true
	case evalCov3Headefd1CardsInYourHand:
		// The resolving controller's hand size (Gerrard's Wisdom, Inner Fire,
		// Dread Slag's -4/-4 per card).
		if c.Controller < 0 || int(c.Controller) >= len(g.Players) {
			return 0, true
		}
		return int32(len(g.Zone(state.ZHand, c.Controller))), true
	case evalCov3Headefd1TopOfLibraryCMC:
		// The mana value of the top card of the controller's library
		// (Counterbalance, Riddle of Lightning); an empty library reads 0.
		if c.Controller < 0 || int(c.Controller) >= len(g.Players) {
			return 0, true
		}
		lib := g.Zone(state.ZLibrary, c.Controller)
		if len(lib) == 0 {
			return 0, true
		}
		if o := g.Obj(lib[0]); o != nil {
			return objectProperty(g, o.ID, "CardManaCost"), true
		}
		return 0, true
	case evalCov3Headefd1TotalOppPoisonCounters:
		// The poison counters summed over the controller's living opponents
		// (Phyrexian Swarmlord, Vishgraz).
		var n int32
		for _, p := range opponentGroup(g, c) {
			n += g.Players[p].Counter("POISON")
		}
		return n, true
	case evalCov3Headefd1TotalTurns:
		// The number of turns this game has had (Necropotence Avatar):
		// every player's taken-turn count summed, the same log fold
		// TurnsTaken reads per player.
		var n int32
		for i := range g.Players {
			n += h.TurnsTaken(state.PlayerID(i))
		}
		return n, true
	case evalCov3Headefd1LeftGraveyardThisTurn:
		// The cards that left a graveyard (Bonecache Overseer's "three or
		// more cards left your graveyard this turn", Syrix, Living History)
		// or the battlefield (Kutzil's Flanker, Tale of Momo) THIS TURN,
		// folded off the per-turn zone-entry list g.Entered: every entry
		// whose origin is the named zone, counted once per object and
		// matched against the spec in the zone it moved to -- the same
		// destination-zone read Count$ThisTurnEntered_<zone>_from_<zone>
		// takes, since the record is the same.
		from := state.ZGraveyard
		if head == "LeftBattlefieldThisTurn" {
			from = state.ZBattlefield
		}
		spec := strings.TrimSpace(arg)
		if spec == "" {
			spec = "Card"
		}
		seen := map[state.ObjID]bool{}
		var n int32
		for _, en := range g.Entered {
			if en.From != from || seen[en.Obj] {
				continue
			}
			if matchesZoneSpecCtx(g, spec, en.Obj, c.SpecContext(c.Controller), en.To) {
				seen[en.Obj] = true
				n++
			}
		}
		return n, true
	case evalCov3Headefd1MaxOppDamageThisTurn:
		// The most damage any one opponent was dealt this turn (Spinerock
		// Knoll's "if an opponent was dealt 7 or more damage this turn",
		// Lightning Phoenix): the Host's per-player log fold
		// DamageTakenThisTurn, maximised over the living opponents.
		var best int32
		for _, p := range opponentGroup(g, c) {
			if v := h.DamageTakenThisTurn(p); v > best {
				best = v
			}
		}
		return best, true
	case evalCov3Headefd1CardManaCost:
		// The source's own mana value (Opalescence's and March of the
		// Machines' "P/T equal to its mana value" CDA grants, Kami of
		// Mourning).
		if o := g.Obj(c.Source); o != nil {
			return objectProperty(g, o.ID, "CardManaCost"), true
		}
		return 0, true
	case evalCov3Headefd1YourSpeed:
		// CR 702.179's speed (Samut, the Driving Force's "where X is your
		// speed", the Start-your-engines! family's gates): the resolving
		// controller's folded state.Player.Speed, 0..4. An out-of-range seat
		// is a modelled zero.
		if c.Controller < 0 || int(c.Controller) >= len(g.Players) {
			return 0, true
		}
		return g.Players[c.Controller].Speed, true
	case evalCov3Headefd1TypesSharedWith:
		return typesSharedWith(h, c, arg)
	case evalCov3Headefd1MostProminentCreatureType:
		return mostProminentCreatureType(h, c, arg), true
	case evalCov3Headefd1CreaturesAttackedThisTurn:
		// The creatures matching the spec that were declared as attackers
		// this turn (Neyali's "for each creature that attacked this turn",
		// Robber of the Rich's Rogue gate), each counted once, read off the
		// Host's log fold of this turn's DeclareAttackers events and matched
		// against the live object.
		spec := strings.TrimSpace(arg)
		if spec == "" {
			spec = "Creature"
		}
		var n int32
		for _, id := range h.AttackersDeclaredThisTurn() {
			if o := g.Obj(id); o != nil && matchesZoneSpecCtx(g, spec, id, c.SpecContext(c.Controller), o.Zone) {
				n++
			}
		}
		return n, true
	}
	return 0, false
}

// cov3BranchHolds answers the fuzz-cov3 yes/no branch predicates of the
// Count$<Predicate>.<yes>.<no> family: ok=false for a predicate this table
// does not know.
func cov3BranchHolds(h Host, c *Ctx, pred string, depth int) (holds, ok bool) {
	g := h.Game()
	you := c.Controller
	valid := you >= 0 && int(you) < len(g.Players)
	switch cov3BranchHoldsefd2Codes.Code(string(pred)) {
	case cov3BranchHoldsefd2Delirium:
		// CR 207.2c ability word: four or more card types among cards in
		// your graveyard -- the Host census the Delirium cost prompts share.
		return valid && h.DeliriumHolds(you), true
	case cov3BranchHoldsefd2Metalcraft:
		// Three or more artifacts you control.
		n, _ := evalCountBody(h, c, "Valid Artifact.YouCtrl", depth+1)
		return valid && n >= 3, true
	case cov3BranchHoldsefd2Hellbent:
		// No cards in your hand.
		return valid && len(g.Zone(state.ZHand, you)) == 0, true
	case cov3BranchHoldsefd2FatefulHour:
		// Five or less life.
		return valid && g.Players[you].Life <= 5, true
	case cov3BranchHoldsefd2AllFourBend:
		if provider, ok := h.(interface {
			AllFourBendThisTurn(p state.PlayerID) bool
		}); ok {
			return valid && provider.AllFourBendThisTurn(you), true
		}
		return false, false
	case cov3BranchHoldsefd2CommittedCrimeThisTurn:
		// CR 700.13: the resolving controller targeted an opponent, a
		// permanent or a spell/ability an opponent controls, or a card in an
		// opponent's graveyard this turn (Seize the Secrets' "costs {1} less
		// if you've committed a crime this turn"). The per-turn record is
		// rules' TargetsChosen ledger, captured at the choice (a later move of
		// the target cannot un-commit the crime); a Host without it leaves
		// the predicate unmodelled.
		if provider, ok := h.(interface {
			CommittedCrimeThisTurn(p state.PlayerID) bool
		}); ok {
			return valid && provider.CommittedCrimeThisTurn(you), true
		}
		return false, false
	case cov3BranchHoldsefd2Landfall:
		// A land entered the battlefield under your control this turn
		// (Groundswell, Tomb Hex): a battlefield entry of a land whose
		// controller is you, off the per-turn g.Entered record.
		if !valid {
			return false, true
		}
		for _, en := range g.Entered {
			if en.To != state.ZBattlefield {
				continue
			}
			if o := g.Obj(en.Obj); o != nil && o.Zone == state.ZBattlefield && o.Controller == you && hasType(o, "Land") {
				return true, true
			}
		}
		return false, true
	case cov3BranchHoldsefd2Void:
		// Edge of Eternities' Void: a nonland permanent left the battlefield
		// this turn, or a spell was warped this turn (any player's, both
		// halves) -- a battlefield departure of a nonland object off
		// g.Entered, and the Host's cast-provenance fold of this turn's
		// warp casts.
		for _, en := range g.Entered {
			if en.From != state.ZBattlefield {
				continue
			}
			if o := g.Obj(en.Obj); o != nil && !hasType(o, "Land") {
				return true, true
			}
		}
		return h.SpellsCastThisTurnMatching(you, "Card.CastSa Spell.Warp") > 0, true
	}
	return false, false
}

// playerScalarProperty is one player's own numeric property for the
// PlayerCount<group>$Highest<Prop>/Lowest<Prop> extremes whose property is a
// zone size or a per-turn tally (Highest/LowestCardsInHand,
// HighestCardsInGraveyard, HighestCardsDrawn): ok=false for any other name.
func playerScalarProperty(h Host, g *state.Game, p state.PlayerID, prop string) (int32, bool) {
	if p < 0 || int(p) >= len(g.Players) {
		return 0, false
	}
	switch playerScalarPropertyefd3Codes.Code(string(prop)) {
	case playerScalarPropertyefd3CardsInHand:
		return int32(len(g.Zone(state.ZHand, p))), true
	case playerScalarPropertyefd3CardsInGraveyard:
		return int32(len(g.Zone(state.ZGraveyard, p))), true
	case playerScalarPropertyefd3CardsInLibrary:
		return int32(len(g.Zone(state.ZLibrary, p))), true
	case playerScalarPropertyefd3CardsDrawn:
		return h.CardsDrawnThisTurn(p), true
	case playerScalarPropertyefd3LifeTotal:
		return g.Players[p].Life, true
	}
	return 0, false
}

// sacrificedThisTurn counts this turn's sacrifices (the g.Entered entries
// events.Apply stamped Sacrificed) whose sacrificer is in players and whose
// object matches spec in the zone it went to. The PlayerCount<group>$
// SacrificedThisTurn <spec> head (Mayhem Devil-adjacent gates: Feast on the
// Fallen, Paladin of Atonement, The Balrog; 17 corpus carriers).
func sacrificedThisTurn(g *state.Game, c *Ctx, players []state.PlayerID, spec string) int32 {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		spec = "Card"
	}
	var n int32
	for _, en := range g.Entered {
		if !en.Sacrificed {
			continue
		}
		member := false
		for _, p := range players {
			if p == en.Sacrificer {
				member = true
				break
			}
		}
		if !member {
			continue
		}
		if matchesZoneSpecCtx(g, spec, en.Obj, c.SpecContext(en.Sacrificer), en.To) {
			n++
		}
	}
	return n
}

// evalCov3PlayerHead answers the PlayerCount<group>$<Property> heads the
// fuzz-cov3 audit added, for the three groups that name a fixed player set:
// PlayerCountPropertyYou$ (the resolving controller), PlayerCount$ and
// PlayerCountPlayers$ (every living player) and PlayerCountOpponents$.
// ok=false for anything else.
func evalCov3PlayerHead(h Host, c *Ctx, head, arg string, depth int) (int32, bool) {
	g := h.Game()
	group, prop, found := strings.Cut(head, "$")
	if !found {
		return 0, false
	}
	var players []state.PlayerID
	switch evalCov3PlayerHeadefd4Codes.Code(string(group)) {
	case evalCov3PlayerHeadefd4PlayerCountPropertyYou:
		if c.Controller < 0 || int(c.Controller) >= len(g.Players) {
			return 0, false
		}
		players = []state.PlayerID{c.Controller}
	case evalCov3PlayerHeadefd4PlayerCount:
		players = g.AliveFrom(0)
	case evalCov3PlayerHeadefd4PlayerCountOpponents:
		players = opponentGroup(g, c)
	case evalCov3PlayerHeadefd4PlayerCountRegisteredOpponen:
		// Only the per-turn noncombat damage property is answered here; every
		// other property of this group keeps its own dispatch (count.go's
		// RegisteredOpponents arm), which this early consult must not shadow.
		if prop != "NonCombatDamageDealtThisTurn" {
			return 0, false
		}
		players = opponentGroup(g, c)
	case evalCov3PlayerHeadefd4PlayerCountRemembered:
		// The players the resolving ability remembered (Ctx.Remembered's
		// player entries, the resolution's own set the Remembered$ head
		// reads): Mindblaze-adjacent "that player's life total / hand size"
		// reads (Sorin's Thirst-family PlayerCountRemembered$LifeTotal,
		// 13 corpus carriers). Summed over the members, Forge's reading.
		for _, t := range c.Remembered {
			if t.IsPlayer {
				players = append(players, t.Player)
			}
		}
		switch evalCov3PlayerHeadefd5Codes.Code(string(prop)) {
		case evalCov3PlayerHeadefd5Amount:
			return int32(len(players)), true
		case evalCov3PlayerHeadefd5Valid:
			// Forge's PlayerCountRemembered$Valid <spec> is a CARD count, not
			// a player count: every corpus carrier (Pox's
			// `Valid Creature.RememberedPlayerCtrl/ThirdUp`, Pox Plague's
			// `Valid Permanent.RememberedPlayerCtrl/HalfDown`, Fraying
			// Omnipotence's `Valid Creature.RememberedPlayerCtrl/HalfUp`,
			// Batwing Brume's `Valid Creature.YouCtrl+attacking`) carries a
			// filter and sizes a per-iteration sacrifice/life-loss amount.
			// It is exactly the `Valid <spec>` zone-count fold the plain
			// Count$Valid head answers, so route the trailing spec there
			// rather than re-implementing it: the /Op suffix was already
			// peeled by evalCountExprOK, and the fold's per-candidate match
			// reads the RememberedPlayerCtrl predicate off Ctx.Remembered
			// (the same binding Count$Valid uses for Legate Lanius). The
			// Count$ path peels /Op before dispatch; the bare SVar path does
			// that in evalCountExprOK before this Valid arm is reached.
			if arg == "" {
				return 0, false
			}
			return evalCountBody(h, c, "Valid "+arg, depth+1)
		case evalCov3PlayerHeadefd5LifeLostThisTurn:
			var n int32
			for _, p := range players {
				n += h.LifeLostThisTurn(p)
			}
			return n, true
		}
		if _, known := playerScalarProperty(h, g, 0, prop); !known {
			return 0, false
		}
		var n int32
		for _, p := range players {
			v, _ := playerScalarProperty(h, g, p, prop)
			n += v
		}
		return n, true
	case evalCov3PlayerHeadefd4PlayerCountRememberedControl:
		// Forge's PlayerCountRememberedController<group>: the CONTROLLERS of
		// the remembered OBJECTS -- never the remembered player entries, which
		// are not remembered objects. Tempt with Mayhem's "an additional time
		// for each opponent who copied the spell this way"
		// (X:PlayerCountRememberedController$Amount/Plus.1) reads the
		// controllers of the copy objects its per-opponent DBCopy remembered;
		// Eradicate/Sowing Salt/Splinter read a remembered card's controller's
		// hand/library size; Faerie Slumber Party counts the remembered
		// creatures' controllers that are opponents.
		//
		// Distinct controllers (Forge's set semantics), so two remembered
		// objects controlled by one seat count once -- Tempt's "for each
		// opponent", Faerie's "for each opponent who controlled". A remembered
		// player entry contributes nothing, and an object entry whose id no
		// longer resolves contributes nothing: fail closed rather than
		// inventing a seat.
		for _, t := range c.Remembered {
			if t.IsPlayer || t.Obj == 0 {
				continue
			}
			o := g.Obj(t.Obj)
			if o == nil {
				continue
			}
			if !slices.Contains(players, o.Controller) {
				players = append(players, o.Controller)
			}
		}
		switch prop {
		case "Amount":
			return int32(len(players)), true
		}
		if spec, hasSpec := strings.CutPrefix(prop, "HasProperty"); hasSpec {
			// The controllers of the remembered objects filtered by the shared
			// player grammar (Faerie Slumber Party's HasPropertyOpponent: one
			// per opponent who controlled a remembered creature). The one home
			// for the spec grammar is the shared player filter.
			var n int32
			for _, p := range players {
				if MatchesPlayerSpec(g, spec, p, c.Controller) {
					n++
				}
			}
			return n, true
		}
		if _, known := playerScalarProperty(h, g, 0, prop); !known {
			return 0, false
		}
		var n int32
		for _, p := range players {
			v, _ := playerScalarProperty(h, g, p, prop)
			n += v
		}
		return n, true
	default:
		return 0, false
	}
	switch evalCov3PlayerHeadefd6Codes.Code(string(prop)) {
	case evalCov3PlayerHeadefd6SacrificedThisTurn:
		return sacrificedThisTurn(g, c, players, arg), true
	case evalCov3PlayerHeadefd6SacrificedPermanentTypesThis:
		// Korvold, Gleeful Glutton's "for each card type among permanents
		// you've sacrificed this turn": the distinct card types of this
		// turn's battlefield sacrifices by the counted players.
		return sacrificedPermanentTypes(g, players), true
	case evalCov3PlayerHeadefd6CardsDrawn:
		// The cards the counted players drew this turn, summed (Heliod, the
		// Warped Eclipse's "for each card your opponents have drawn this
		// turn") -- the same Host log fold the Highest/Lowest extremes and
		// Count$YouDrewThisTurn read.
		var n int32
		for _, p := range players {
			n += h.CardsDrawnThisTurn(p)
		}
		return n, true
	case evalCov3PlayerHeadefd6NonCombatDamageDealtThisTurn:
		// Chandra's Incinerator's "the total amount of noncombat damage dealt
		// to your opponents this turn": each counted player's damage taken
		// this turn (the Host's Damage-event fold) minus the combat damage
		// the per-turn combat ledger recorded landing on them.
		hits := h.CombatDamageToPlayersThisTurn()
		var n int32
		for _, p := range players {
			v := h.DamageTakenThisTurn(p)
			for _, hit := range hits {
				if hit.Player == p {
					v -= hit.Amount
				}
			}
			if v > 0 {
				n += v
			}
		}
		return n, true
	case evalCov3PlayerHeadefd6OpponentsAttackedThisTurn:
		// Fast Forward's "for each opponent you attacked this turn": the
		// distinct opponents the resolving controller declared attacks on
		// this turn, read off the Host's DeclareAttackers log fold (the live
		// combat state forgets them once combat ends).
		if group != "PlayerCountPropertyYou" {
			return 0, false
		}
		provider, ok := h.(interface {
			OpponentsAttackedThisTurn(p state.PlayerID) int32
		})
		if !ok {
			return 0, false
		}
		return provider.OpponentsAttackedThisTurn(c.Controller), true
	case evalCov3PlayerHeadefd6LifeLostLastTurn:
		// The life each counted player lost during the PREVIOUS turn (the
		// Host's log fold between the last two TurnChange events), summed
		// (Brutal Deceiver-adjacent Wicked Visitor family: First Response's
		// "if you lost life last turn").
		var n int32
		for _, p := range players {
			n += h.LifeLostLastTurn(p)
		}
		return n, true
	case evalCov3PlayerHeadefd6AttackersDeclared:
		// Charging Cinderhorn's "if no creatures attacked this turn": the
		// attackers declared this turn. Only the every-player group sums to
		// the whole-turn fold the Host keeps.
		if group != "PlayerCountPlayers" && group != "PlayerCount" {
			return 0, false
		}
		return int32(h.AttackersThisTurn()), true
	case evalCov3PlayerHeadefd6HasPropertyBeenAttackedThisC:
		// "only if you've been attacked this step" (Eightfold Maze,
		// Kongming's Contraptions, Warrior's Stand; 15 corpus carriers): 1
		// when a creature is attacking the resolving controller (or a
		// permanent they control -- Object.Attacking names the defending
		// seat either way), from the live combat state.
		if group != "PlayerCountPropertyYou" {
			return 0, false
		}
		for i := range g.Objs {
			o := &g.Objs[i]
			if o.Zone == state.ZBattlefield && o.IsAttacking && o.Attacking == c.Controller && o.Controller != c.Controller {
				return 1, true
			}
		}
		return 0, true
	case evalCov3PlayerHeadefd6OpponentsAttackedThisCombat:
		// The number of distinct opponents the resolving controller's
		// creatures are attacking this combat (the Myriad-adjacent "for each
		// opponent you attacked" family), from the live combat state.
		if group != "PlayerCountPropertyYou" {
			return 0, false
		}
		var seen [256]bool
		var n int32
		for i := range g.Objs {
			o := &g.Objs[i]
			if o.Zone != state.ZBattlefield || !o.IsAttacking || o.Controller != c.Controller || o.Attacking == c.Controller {
				continue
			}
			if !seen[o.Attacking] {
				seen[o.Attacking] = true
				n++
			}
		}
		return n, true
	case evalCov3PlayerHeadefd6HasPropertyattackedYouTheirL:
		// The counted players who attacked the resolving controller during
		// their last turn (Avenge's cost reduction gate), the Host's log
		// walk over each member's most recent completed turn.
		var n int32
		for _, p := range players {
			if p != c.Controller && h.AttackedDuringLastTurn(p, c.Controller) {
				n++
			}
		}
		return n, true
	case evalCov3PlayerHeadefd6DomainPlayer:
		if group != "PlayerCountPropertyYou" {
			return 0, false
		}
		return domainCount(h, c, c.Controller, 0), true
	}
	return 0, false
}

// sacrificedPermanentTypes counts the distinct card types among this turn's
// battlefield sacrifices (g.Entered's Sacrificed stamp) whose sacrificer is
// one of players, each object read through its card face (CR 205.2a's card
// types; the face a sacrificed permanent had is the card it went to the
// graveyard as).
func sacrificedPermanentTypes(g *state.Game, players []state.PlayerID) int32 {
	seen := map[string]bool{}
	for _, en := range g.Entered {
		if !en.Sacrificed || en.From != state.ZBattlefield || !slices.Contains(players, en.Sacrificer) {
			continue
		}
		o := g.Obj(en.Obj)
		if o == nil || o.Face() == nil {
			continue
		}
		for _, typ := range o.Face().Types {
			if cardTypeWords[typ] || typ == "Kindred" {
				seen[typ] = true
			}
		}
	}
	return int32(len(seen))
}

// typesSharedWith answers Count$TypesSharedWith <ZoneHead> <spec> (Cemetery
// Prowler's AffectedX: "for each card type they share with cards exiled with
// CARDNAME"): the number of card types the AFFECTED object (Ctx.AffectedObj,
// the spell a cost static is pricing; Source when unbound -- Forge's host
// card) shares with the union of the card types of the objects in the named
// zone matching spec. An unknown zone head or an empty spec is unresolvable.
func typesSharedWith(h Host, c *Ctx, arg string) (int32, bool) {
	zoneHead, spec, _ := strings.Cut(strings.TrimSpace(arg), " ")
	spec = strings.TrimSpace(spec)
	zone, ok := countZone(zoneHead)
	if !ok || spec == "" {
		return 0, false
	}
	g := h.Game()
	subject := c.AffectedObj
	if subject == 0 {
		subject = c.Source
	}
	so := g.Obj(subject)
	if so == nil || so.Face() == nil {
		return 0, true
	}
	isCardType := func(t string) bool { return cardTypeWords[t] || t == "Kindred" }
	pool := map[string]bool{}
	sc := c.SpecContext(c.Controller)
	for i := range g.Players {
		if zone == state.ZStack && i > 0 {
			break
		}
		for _, id := range g.Zone(zone, state.PlayerID(i)) {
			o := g.Obj(id)
			if o == nil || o.Face() == nil || !matchesZoneSpecCtx(g, spec, id, sc, zone) {
				continue
			}
			for _, t := range o.Face().Types {
				if isCardType(t) {
					pool[t] = true
				}
			}
		}
	}
	var n int32
	counted := map[string]bool{}
	for _, t := range so.Face().Types {
		if isCardType(t) && pool[t] && !counted[t] {
			counted[t] = true
			n++
		}
	}
	return n, true
}

// mostProminentCreatureType answers Count$MostProminentCreatureType <spec>
// (Synchronized Eviction's "at least two creatures that share a creature
// type"): the size of the largest group of battlefield objects matching spec
// that share one creature type. A changeling (CR 702.73a: every creature
// type) joins every group.
func mostProminentCreatureType(h Host, c *Ctx, spec string) int32 {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		spec = "Creature"
	}
	g := h.Game()
	sc := c.SpecContext(c.Controller)
	counts := map[string]int32{}
	var all int32
	for i := range g.Players {
		for _, id := range g.Zone(state.ZBattlefield, state.PlayerID(i)) {
			o := g.Obj(id)
			if o == nil || o.Face() == nil || !matchesZoneSpecCtx(g, spec, id, sc, state.ZBattlefield) {
				continue
			}
			if h.HasKeyword(id, "Changeling") {
				all++
				continue
			}
			for _, t := range o.Face().Types {
				if creatureSubtypeWords[t] {
					counts[t]++
				}
			}
		}
	}
	var best int32
	for _, n := range counts {
		if n > best {
			best = n
		}
	}
	return best + all
}

const (
	evalCov3Headefd1Domain                    uint16 = 1  // "Domain"
	evalCov3Headefd1DomainActivePlayer        uint16 = 2  // "DomainActivePlayer"
	evalCov3Headefd1CardsInYourHand           uint16 = 3  // "CardsInYourHand"
	evalCov3Headefd1TopOfLibraryCMC           uint16 = 4  // "TopOfLibraryCMC"
	evalCov3Headefd1TotalOppPoisonCounters    uint16 = 5  // "TotalOppPoisonCounters"
	evalCov3Headefd1TotalTurns                uint16 = 6  // "TotalTurns"
	evalCov3Headefd1LeftGraveyardThisTurn     uint16 = 7  // "LeftGraveyardThisTurn", "LeftBattlefieldThisTurn"
	evalCov3Headefd1MaxOppDamageThisTurn      uint16 = 8  // "MaxOppDamageThisTurn"
	evalCov3Headefd1CardManaCost              uint16 = 9  // "CardManaCost"
	evalCov3Headefd1YourSpeed                 uint16 = 10 // "YourSpeed"
	evalCov3Headefd1TypesSharedWith           uint16 = 11 // "TypesSharedWith"
	evalCov3Headefd1MostProminentCreatureType uint16 = 12 // "MostProminentCreatureType"
	evalCov3Headefd1CreaturesAttackedThisTurn uint16 = 13 // "CreaturesAttackedThisTurn"
)

var evalCov3Headefd1Codes = state.NewStrCodes(
	state.StrEntry[uint16]{Key: "Domain", Val: evalCov3Headefd1Domain},
	state.StrEntry[uint16]{Key: "DomainActivePlayer", Val: evalCov3Headefd1DomainActivePlayer},
	state.StrEntry[uint16]{Key: "CardsInYourHand", Val: evalCov3Headefd1CardsInYourHand},
	state.StrEntry[uint16]{Key: "TopOfLibraryCMC", Val: evalCov3Headefd1TopOfLibraryCMC},
	state.StrEntry[uint16]{Key: "TotalOppPoisonCounters", Val: evalCov3Headefd1TotalOppPoisonCounters},
	state.StrEntry[uint16]{Key: "TotalTurns", Val: evalCov3Headefd1TotalTurns},
	state.StrEntry[uint16]{Key: "LeftGraveyardThisTurn", Val: evalCov3Headefd1LeftGraveyardThisTurn},
	state.StrEntry[uint16]{Key: "LeftBattlefieldThisTurn", Val: evalCov3Headefd1LeftGraveyardThisTurn},
	state.StrEntry[uint16]{Key: "MaxOppDamageThisTurn", Val: evalCov3Headefd1MaxOppDamageThisTurn},
	state.StrEntry[uint16]{Key: "CardManaCost", Val: evalCov3Headefd1CardManaCost},
	state.StrEntry[uint16]{Key: "YourSpeed", Val: evalCov3Headefd1YourSpeed},
	state.StrEntry[uint16]{Key: "TypesSharedWith", Val: evalCov3Headefd1TypesSharedWith},
	state.StrEntry[uint16]{Key: "MostProminentCreatureType", Val: evalCov3Headefd1MostProminentCreatureType},
	state.StrEntry[uint16]{Key: "CreaturesAttackedThisTurn", Val: evalCov3Headefd1CreaturesAttackedThisTurn},
)

const (
	cov3BranchHoldsefd2Delirium               uint16 = 1 // "Delirium"
	cov3BranchHoldsefd2Metalcraft             uint16 = 2 // "Metalcraft"
	cov3BranchHoldsefd2Hellbent               uint16 = 3 // "Hellbent"
	cov3BranchHoldsefd2FatefulHour            uint16 = 4 // "FatefulHour"
	cov3BranchHoldsefd2AllFourBend            uint16 = 5 // "AllFourBend"
	cov3BranchHoldsefd2CommittedCrimeThisTurn uint16 = 6 // "CommittedCrimeThisTurn"
	cov3BranchHoldsefd2Landfall               uint16 = 7 // "Landfall"
	cov3BranchHoldsefd2Void                   uint16 = 8 // "Void"
)

var cov3BranchHoldsefd2Codes = state.NewStrCodes(
	state.StrEntry[uint16]{Key: "Delirium", Val: cov3BranchHoldsefd2Delirium},
	state.StrEntry[uint16]{Key: "Metalcraft", Val: cov3BranchHoldsefd2Metalcraft},
	state.StrEntry[uint16]{Key: "Hellbent", Val: cov3BranchHoldsefd2Hellbent},
	state.StrEntry[uint16]{Key: "FatefulHour", Val: cov3BranchHoldsefd2FatefulHour},
	state.StrEntry[uint16]{Key: "AllFourBend", Val: cov3BranchHoldsefd2AllFourBend},
	state.StrEntry[uint16]{Key: "CommittedCrimeThisTurn", Val: cov3BranchHoldsefd2CommittedCrimeThisTurn},
	state.StrEntry[uint16]{Key: "Landfall", Val: cov3BranchHoldsefd2Landfall},
	state.StrEntry[uint16]{Key: "Void", Val: cov3BranchHoldsefd2Void},
)

const (
	playerScalarPropertyefd3CardsInHand      uint16 = 1 // "CardsInHand"
	playerScalarPropertyefd3CardsInGraveyard uint16 = 2 // "CardsInGraveyard"
	playerScalarPropertyefd3CardsInLibrary   uint16 = 3 // "CardsInLibrary"
	playerScalarPropertyefd3CardsDrawn       uint16 = 4 // "CardsDrawn"
	playerScalarPropertyefd3LifeTotal        uint16 = 5 // "LifeTotal"
)

var playerScalarPropertyefd3Codes = state.NewStrCodes(
	state.StrEntry[uint16]{Key: "CardsInHand", Val: playerScalarPropertyefd3CardsInHand},
	state.StrEntry[uint16]{Key: "CardsInGraveyard", Val: playerScalarPropertyefd3CardsInGraveyard},
	state.StrEntry[uint16]{Key: "CardsInLibrary", Val: playerScalarPropertyefd3CardsInLibrary},
	state.StrEntry[uint16]{Key: "CardsDrawn", Val: playerScalarPropertyefd3CardsDrawn},
	state.StrEntry[uint16]{Key: "LifeTotal", Val: playerScalarPropertyefd3LifeTotal},
)

const (
	evalCov3PlayerHeadefd4PlayerCountPropertyYou       uint16 = 1 // "PlayerCountPropertyYou"
	evalCov3PlayerHeadefd4PlayerCount                  uint16 = 2 // "PlayerCount", "PlayerCountPlayers"
	evalCov3PlayerHeadefd4PlayerCountOpponents         uint16 = 3 // "PlayerCountOpponents"
	evalCov3PlayerHeadefd4PlayerCountRegisteredOpponen uint16 = 4 // "PlayerCountRegisteredOpponents"
	evalCov3PlayerHeadefd4PlayerCountRemembered        uint16 = 5 // "PlayerCountRemembered"
	evalCov3PlayerHeadefd4PlayerCountRememberedControl uint16 = 6 // "PlayerCountRememberedController"
)

var evalCov3PlayerHeadefd4Codes = state.NewStrCodes(
	state.StrEntry[uint16]{Key: "PlayerCountPropertyYou", Val: evalCov3PlayerHeadefd4PlayerCountPropertyYou},
	state.StrEntry[uint16]{Key: "PlayerCount", Val: evalCov3PlayerHeadefd4PlayerCount},
	state.StrEntry[uint16]{Key: "PlayerCountPlayers", Val: evalCov3PlayerHeadefd4PlayerCount},
	state.StrEntry[uint16]{Key: "PlayerCountOpponents", Val: evalCov3PlayerHeadefd4PlayerCountOpponents},
	state.StrEntry[uint16]{Key: "PlayerCountRegisteredOpponents", Val: evalCov3PlayerHeadefd4PlayerCountRegisteredOpponen},
	state.StrEntry[uint16]{Key: "PlayerCountRemembered", Val: evalCov3PlayerHeadefd4PlayerCountRemembered},
	state.StrEntry[uint16]{Key: "PlayerCountRememberedController", Val: evalCov3PlayerHeadefd4PlayerCountRememberedControl},
)

const (
	evalCov3PlayerHeadefd5Amount           uint16 = 1 // "Amount"
	evalCov3PlayerHeadefd5Valid            uint16 = 2 // "Valid"
	evalCov3PlayerHeadefd5LifeLostThisTurn uint16 = 3 // "LifeLostThisTurn"
)

var evalCov3PlayerHeadefd5Codes = state.NewStrCodes(
	state.StrEntry[uint16]{Key: "Amount", Val: evalCov3PlayerHeadefd5Amount},
	state.StrEntry[uint16]{Key: "Valid", Val: evalCov3PlayerHeadefd5Valid},
	state.StrEntry[uint16]{Key: "LifeLostThisTurn", Val: evalCov3PlayerHeadefd5LifeLostThisTurn},
)

const (
	evalCov3PlayerHeadefd6SacrificedThisTurn           uint16 = 1  // "SacrificedThisTurn"
	evalCov3PlayerHeadefd6SacrificedPermanentTypesThis uint16 = 2  // "SacrificedPermanentTypesThisTurn"
	evalCov3PlayerHeadefd6CardsDrawn                   uint16 = 3  // "CardsDrawn"
	evalCov3PlayerHeadefd6NonCombatDamageDealtThisTurn uint16 = 4  // "NonCombatDamageDealtThisTurn"
	evalCov3PlayerHeadefd6OpponentsAttackedThisTurn    uint16 = 5  // "OpponentsAttackedThisTurn"
	evalCov3PlayerHeadefd6LifeLostLastTurn             uint16 = 6  // "LifeLostLastTurn"
	evalCov3PlayerHeadefd6AttackersDeclared            uint16 = 7  // "AttackersDeclared"
	evalCov3PlayerHeadefd6HasPropertyBeenAttackedThisC uint16 = 8  // "HasPropertyBeenAttackedThisCombat"
	evalCov3PlayerHeadefd6OpponentsAttackedThisCombat  uint16 = 9  // "OpponentsAttackedThisCombat"
	evalCov3PlayerHeadefd6HasPropertyattackedYouTheirL uint16 = 10 // "HasPropertyattackedYouTheirLastTurn"
	evalCov3PlayerHeadefd6DomainPlayer                 uint16 = 11 // "DomainPlayer"
)

var evalCov3PlayerHeadefd6Codes = state.NewStrCodes(
	state.StrEntry[uint16]{Key: "SacrificedThisTurn", Val: evalCov3PlayerHeadefd6SacrificedThisTurn},
	state.StrEntry[uint16]{Key: "SacrificedPermanentTypesThisTurn", Val: evalCov3PlayerHeadefd6SacrificedPermanentTypesThis},
	state.StrEntry[uint16]{Key: "CardsDrawn", Val: evalCov3PlayerHeadefd6CardsDrawn},
	state.StrEntry[uint16]{Key: "NonCombatDamageDealtThisTurn", Val: evalCov3PlayerHeadefd6NonCombatDamageDealtThisTurn},
	state.StrEntry[uint16]{Key: "OpponentsAttackedThisTurn", Val: evalCov3PlayerHeadefd6OpponentsAttackedThisTurn},
	state.StrEntry[uint16]{Key: "LifeLostLastTurn", Val: evalCov3PlayerHeadefd6LifeLostLastTurn},
	state.StrEntry[uint16]{Key: "AttackersDeclared", Val: evalCov3PlayerHeadefd6AttackersDeclared},
	state.StrEntry[uint16]{Key: "HasPropertyBeenAttackedThisCombat", Val: evalCov3PlayerHeadefd6HasPropertyBeenAttackedThisC},
	state.StrEntry[uint16]{Key: "OpponentsAttackedThisCombat", Val: evalCov3PlayerHeadefd6OpponentsAttackedThisCombat},
	state.StrEntry[uint16]{Key: "HasPropertyattackedYouTheirLastTurn", Val: evalCov3PlayerHeadefd6HasPropertyattackedYouTheirL},
	state.StrEntry[uint16]{Key: "DomainPlayer", Val: evalCov3PlayerHeadefd6DomainPlayer},
)
