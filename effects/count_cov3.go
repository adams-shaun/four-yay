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
	switch evalCov3HeadCodes.Code(string(head)) {
	case evalCov3HeadDomain:
		// "for each basic land type among lands you control" (62 corpus
		// carriers: Tribal Flames, Draco's cost reduction, Allied Strategies).
		return domainCount(h, c, c.Controller, depth), true
	case evalCov3HeadDomainActivePlayer:
		// The same census for the ACTIVE player (Collapsing Borders' upkeep
		// life gain, Mask of Intolerance).
		return domainCount(h, c, g.Active, depth), true
	case evalCov3HeadCardsInYourHand:
		// The resolving controller's hand size (Gerrard's Wisdom, Inner Fire,
		// Dread Slag's -4/-4 per card).
		if c.Controller < 0 || int(c.Controller) >= len(g.Players) {
			return 0, true
		}
		return int32(len(g.Zone(state.ZHand, c.Controller))), true
	case evalCov3HeadTopOfLibraryCMC:
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
	case evalCov3HeadTotalOppPoisonCounters:
		// The poison counters summed over the controller's living opponents
		// (Phyrexian Swarmlord, Vishgraz).
		var n int32
		for _, p := range opponentGroup(g, c) {
			n += g.Players[p].Counter("POISON")
		}
		return n, true
	case evalCov3HeadTotalTurns:
		// The number of turns this game has had (Necropotence Avatar):
		// every player's taken-turn count summed, the same log fold
		// TurnsTaken reads per player.
		var n int32
		for i := range g.Players {
			n += h.TurnsTaken(state.PlayerID(i))
		}
		return n, true
	case evalCov3HeadLeftZoneThisTurn:
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
	case evalCov3HeadMaxOppDamageThisTurn:
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
	case evalCov3HeadCardManaCost:
		// The source's own mana value (Opalescence's and March of the
		// Machines' "P/T equal to its mana value" CDA grants, Kami of
		// Mourning).
		if o := g.Obj(c.Source); o != nil {
			return objectProperty(g, o.ID, "CardManaCost"), true
		}
		return 0, true
	case evalCov3HeadYourSpeed:
		// CR 702.179's speed (Samut, the Driving Force's "where X is your
		// speed", the Start-your-engines! family's gates): the resolving
		// controller's folded state.Player.Speed, 0..4. An out-of-range seat
		// is a modelled zero.
		if c.Controller < 0 || int(c.Controller) >= len(g.Players) {
			return 0, true
		}
		return g.Players[c.Controller].Speed, true
	case evalCov3HeadTypesSharedWith:
		return typesSharedWith(h, c, arg)
	case evalCov3HeadMostProminentCreatureType:
		return mostProminentCreatureType(h, c, arg), true
	case evalCov3HeadCreaturesAttackedThisTurn:
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
	switch cov3BranchHoldsCodes.Code(string(pred)) {
	case cov3BranchHoldsDelirium:
		// CR 207.2c ability word: four or more card types among cards in
		// your graveyard -- the Host census the Delirium cost prompts share.
		return valid && h.DeliriumHolds(you), true
	case cov3BranchHoldsMetalcraft:
		// Three or more artifacts you control.
		n, _ := evalCountBody(h, c, "Valid Artifact.YouCtrl", depth+1)
		return valid && n >= 3, true
	case cov3BranchHoldsHellbent:
		// No cards in your hand.
		return valid && len(g.Zone(state.ZHand, you)) == 0, true
	case cov3BranchHoldsFatefulHour:
		// Five or less life.
		return valid && g.Players[you].Life <= 5, true
	case cov3BranchHoldsAllFourBend:
		if provider, ok := h.(interface {
			AllFourBendThisTurn(p state.PlayerID) bool
		}); ok {
			return valid && provider.AllFourBendThisTurn(you), true
		}
		return false, false
	case cov3BranchHoldsCommittedCrimeThisTurn:
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
	case cov3BranchHoldsLandfall:
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
	case cov3BranchHoldsVoid:
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
	switch playerScalarPropertyCodes.Code(string(prop)) {
	case playerScalarPropertyCardsInHand:
		return int32(len(g.Zone(state.ZHand, p))), true
	case playerScalarPropertyCardsInGraveyard:
		return int32(len(g.Zone(state.ZGraveyard, p))), true
	case playerScalarPropertyCardsInLibrary:
		return int32(len(g.Zone(state.ZLibrary, p))), true
	case playerScalarPropertyCardsDrawn:
		return h.CardsDrawnThisTurn(p), true
	case playerScalarPropertyLifeTotal:
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
	// sharedProps gates the shared property tables to the selectors no LATER
	// arm answers: evalCov3PlayerHead runs BEFORE evalCountBodyPlayer, so a
	// property returned here SHADOWS the group-prefixed arms (e.g.
	// PlayerCountOpponents$CardsInHand).
	var sharedProps bool
	switch cov3PlayerGroupCodes.Code(string(group)) {
	case cov3PlayerGroupPlayerCountPropertyYou:
		if c.Controller < 0 || int(c.Controller) >= len(g.Players) {
			return 0, false
		}
		players = []state.PlayerID{c.Controller}
	case cov3PlayerGroupPlayerCount:
		players = g.AliveFrom(0)
	case cov3PlayerGroupPlayerCountBare:
		players = g.AliveFrom(0)
		sharedProps = true
	case cov3PlayerGroupPlayerCountOpponents:
		players = opponentGroup(g, c)
	case cov3PlayerGroupPlayerCountOther:
		// Forge's PlayerCountOther$ is every living player but the resolving
		// controller (Kaya, Spirits' Justice's `PlayerCountOther$Amount`).
		players = opponentGroup(g, c)
		sharedProps = true
	case cov3PlayerGroupPlayerCountDefinedRememberedOwner:
		// Forge's PlayerCountDefinedRememberedOwner$ is the OWNERS of the
		// resolution's remembered objects (Deadly Cover-Up's survivor search).
		players = cov3RememberedOwners(g, c)
		sharedProps = true
	case cov3PlayerGroupPlayerCountRegisteredOpponen:
		// Only the per-turn noncombat damage property is answered here; every
		// other property of this group keeps its own dispatch (count.go's
		// RegisteredOpponents arm), which this early consult must not shadow.
		if prop != "NonCombatDamageDealtThisTurn" {
			return 0, false
		}
		players = opponentGroup(g, c)
	case cov3PlayerGroupPlayerCountRemembered:
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
		switch cov3PlayerPropCodes.Code(string(prop)) {
		case cov3PlayerPropAmount:
			return int32(len(players)), true
		case cov3PlayerPropValid:
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
		case cov3PlayerPropLifeLostThisTurn:
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
	case cov3PlayerGroupPlayerCountRememberedControl:
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
		if prop == "Amount" {
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
	// The shared property tables, so the selectors above need no property
	// code of their own: `Amount` is the group's size (playerGroupCount), the
	// HasProperty* family is the same read the group-prefixed arms use
	// (hasPropertyStateBacked -- bare PlayerCount$HasPropertyHasCardsInHand_
	// Card_LE1 is Aclazotz, Deepest Betrayal's / Naktamun Lorespinner's
	// "for each player who has one or fewer cards in hand"), and the per-seat
	// scalars (CardsInHand/Graveyard/Library, LifeTotal, CardsDrawn) are
	// playerScalarProperty. Reading them here -- before the scalar switch,
	// which only the bare PlayerCount group otherwise reaches -- keeps one
	// property table per family. A property unknown to all three still falls
	// through unresolvable.
	if sharedProps {
		if n, ok := cov3SharedPlayerProps(h, c, g, players, prop, arg); ok {
			return n, true
		}
	}
	switch cov3PlayerScalarCodes.Code(string(prop)) {
	case cov3PlayerScalarSacrificedThisTurn:
		return sacrificedThisTurn(g, c, players, arg), true
	case cov3PlayerScalarSacrificedPermanentTypesThis:
		// Korvold, Gleeful Glutton's "for each card type among permanents
		// you've sacrificed this turn": the distinct card types of this
		// turn's battlefield sacrifices by the counted players.
		return sacrificedPermanentTypes(g, players), true
	case cov3PlayerScalarCardsDrawn:
		// The cards the counted players drew this turn, summed (Heliod, the
		// Warped Eclipse's "for each card your opponents have drawn this
		// turn") -- the same Host log fold the Highest/Lowest extremes and
		// Count$YouDrewThisTurn read.
		var n int32
		for _, p := range players {
			n += h.CardsDrawnThisTurn(p)
		}
		return n, true
	case cov3PlayerScalarNonCombatDamageDealtThisTurn:
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
	case cov3PlayerScalarOpponentsAttackedThisTurn:
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
	case cov3PlayerScalarLifeLostLastTurn:
		// The life each counted player lost during the PREVIOUS turn (the
		// Host's log fold between the last two TurnChange events), summed
		// (Brutal Deceiver-adjacent Wicked Visitor family: First Response's
		// "if you lost life last turn").
		var n int32
		for _, p := range players {
			n += h.LifeLostLastTurn(p)
		}
		return n, true
	case cov3PlayerScalarAttackersDeclared:
		// Charging Cinderhorn's "if no creatures attacked this turn": the
		// attackers declared this turn. Only the every-player group sums to
		// the whole-turn fold the Host keeps.
		if group != "PlayerCountPlayers" && group != "PlayerCount" {
			return 0, false
		}
		return int32(h.AttackersThisTurn()), true
	case cov3PlayerScalarHasPropertyBeenAttackedThisC:
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
	case cov3PlayerScalarOpponentsAttackedThisCombat:
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
	case cov3PlayerScalarHasPropertyAttackedYouTheirLastTurn:
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
	case cov3PlayerScalarDomainPlayer:
		if group != "PlayerCountPropertyYou" {
			return 0, false
		}
		return domainCount(h, c, c.Controller, 0), true
	}
	return 0, false
}

// cov3SharedPlayerProps answers the property families every selector shares:
// `Amount` is the group's member count (playerGroupCount), the HasProperty*
// read is the same one the group-prefixed arms use (hasPropertyStateBacked),
// and the per-seat zone/life scalars are playerScalarProperty. It is called
// only for the selectors no LATER count arm answers (see sharedProps at the
// call site), so it cannot shadow PlayerCountOpponents$CardsInHand. The
// selectors that reach it are bare PlayerCount$ (every living player, where
// Aclazotz, Deepest Betrayal's `PlayerCount$HasPropertyHasCardsInHand_
// Card_LE1` reads), PlayerCountOther$ (every living player but the resolving
// controller; Kaya's per-player TargetMax$) and PlayerCountDefinedRemembered
// Owner$ (the owners of the remembered objects; Deadly Cover-Up's
// graveyard/hand/library search sizes).
func cov3SharedPlayerProps(h Host, c *Ctx, g *state.Game, players []state.PlayerID, prop, arg string) (int32, bool) {
	if n, ok := playerGroupCount(players, prop); ok {
		return n, true
	}
	if n, ok := hasPropertyStateBacked(h, g, c, players, prop, arg); ok {
		return n, true
	}
	if _, known := playerScalarProperty(h, g, 0, prop); known {
		var n int32
		for _, p := range players {
			v, _ := playerScalarProperty(h, g, p, prop)
			n += v
		}
		return n, true
	}
	return 0, false
}

// cov3RememberedOwners returns the distinct OWNERS of the resolution's
// remembered objects (Ctx.Remembered), in first-seen order. A remembered
// player entry and an object id that no longer resolves both contribute
// nothing: fail closed rather than inventing a seat.
func cov3RememberedOwners(g *state.Game, c *Ctx) []state.PlayerID {
	var out []state.PlayerID
	for _, t := range c.Remembered {
		if t.IsPlayer || t.Obj == 0 {
			continue
		}
		o := g.Obj(t.Obj)
		if o == nil {
			continue
		}
		if !slices.Contains(out, o.Owner) {
			out = append(out, o.Owner)
		}
	}
	return out
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
// that share one creature type, read from each object's layer-4 type result.
// An object with every creature type (CR 702.73a) joins every group.
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
			// The object's published layer-4 result (a granted subtype, a
			// stripped Changeling) wins; an object no type-changing effect
			// touches has no entry and reads its printed face and CDA.
			types, isAll := o.Face().Types, IntrinsicAllCreatureTypes(o)
			for _, d := range sc.Layers.DerivedTypes {
				if d.ID == id {
					types, isAll = d.Types, d.AllCreatureTypes
					break
				}
			}
			if isAll {
				all++
				continue
			}
			for _, t := range types {
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

type evalCov3HeadCode uint16

const (
	evalCov3HeadDomain evalCov3HeadCode = iota + 1
	evalCov3HeadDomainActivePlayer
	evalCov3HeadCardsInYourHand
	evalCov3HeadTopOfLibraryCMC
	evalCov3HeadTotalOppPoisonCounters
	evalCov3HeadTotalTurns
	evalCov3HeadLeftZoneThisTurn
	evalCov3HeadMaxOppDamageThisTurn
	evalCov3HeadCardManaCost
	evalCov3HeadYourSpeed
	evalCov3HeadTypesSharedWith
	evalCov3HeadMostProminentCreatureType
	evalCov3HeadCreaturesAttackedThisTurn
)

var evalCov3HeadCodes = state.NewStrCodes(
	state.StrEntry[evalCov3HeadCode]{Key: "Domain", Val: evalCov3HeadDomain},
	state.StrEntry[evalCov3HeadCode]{Key: "DomainActivePlayer", Val: evalCov3HeadDomainActivePlayer},
	state.StrEntry[evalCov3HeadCode]{Key: "CardsInYourHand", Val: evalCov3HeadCardsInYourHand},
	state.StrEntry[evalCov3HeadCode]{Key: "TopOfLibraryCMC", Val: evalCov3HeadTopOfLibraryCMC},
	state.StrEntry[evalCov3HeadCode]{Key: "TotalOppPoisonCounters", Val: evalCov3HeadTotalOppPoisonCounters},
	state.StrEntry[evalCov3HeadCode]{Key: "TotalTurns", Val: evalCov3HeadTotalTurns},
	state.StrEntry[evalCov3HeadCode]{Key: "LeftGraveyardThisTurn", Val: evalCov3HeadLeftZoneThisTurn},
	state.StrEntry[evalCov3HeadCode]{Key: "LeftBattlefieldThisTurn", Val: evalCov3HeadLeftZoneThisTurn},
	state.StrEntry[evalCov3HeadCode]{Key: "MaxOppDamageThisTurn", Val: evalCov3HeadMaxOppDamageThisTurn},
	state.StrEntry[evalCov3HeadCode]{Key: "CardManaCost", Val: evalCov3HeadCardManaCost},
	state.StrEntry[evalCov3HeadCode]{Key: "YourSpeed", Val: evalCov3HeadYourSpeed},
	state.StrEntry[evalCov3HeadCode]{Key: "TypesSharedWith", Val: evalCov3HeadTypesSharedWith},
	state.StrEntry[evalCov3HeadCode]{Key: "MostProminentCreatureType", Val: evalCov3HeadMostProminentCreatureType},
	state.StrEntry[evalCov3HeadCode]{Key: "CreaturesAttackedThisTurn", Val: evalCov3HeadCreaturesAttackedThisTurn},
)

type cov3BranchHoldsCode uint16

const (
	cov3BranchHoldsDelirium cov3BranchHoldsCode = iota + 1
	cov3BranchHoldsMetalcraft
	cov3BranchHoldsHellbent
	cov3BranchHoldsFatefulHour
	cov3BranchHoldsAllFourBend
	cov3BranchHoldsCommittedCrimeThisTurn
	cov3BranchHoldsLandfall
	cov3BranchHoldsVoid
)

var cov3BranchHoldsCodes = state.NewStrCodes(
	state.StrEntry[cov3BranchHoldsCode]{Key: "Delirium", Val: cov3BranchHoldsDelirium},
	state.StrEntry[cov3BranchHoldsCode]{Key: "Metalcraft", Val: cov3BranchHoldsMetalcraft},
	state.StrEntry[cov3BranchHoldsCode]{Key: "Hellbent", Val: cov3BranchHoldsHellbent},
	state.StrEntry[cov3BranchHoldsCode]{Key: "FatefulHour", Val: cov3BranchHoldsFatefulHour},
	state.StrEntry[cov3BranchHoldsCode]{Key: "AllFourBend", Val: cov3BranchHoldsAllFourBend},
	state.StrEntry[cov3BranchHoldsCode]{Key: "CommittedCrimeThisTurn", Val: cov3BranchHoldsCommittedCrimeThisTurn},
	state.StrEntry[cov3BranchHoldsCode]{Key: "Landfall", Val: cov3BranchHoldsLandfall},
	state.StrEntry[cov3BranchHoldsCode]{Key: "Void", Val: cov3BranchHoldsVoid},
)

type playerScalarPropertyCode uint16

const (
	playerScalarPropertyCardsInHand playerScalarPropertyCode = iota + 1
	playerScalarPropertyCardsInGraveyard
	playerScalarPropertyCardsInLibrary
	playerScalarPropertyCardsDrawn
	playerScalarPropertyLifeTotal
)

var playerScalarPropertyCodes = state.NewStrCodes(
	state.StrEntry[playerScalarPropertyCode]{Key: "CardsInHand", Val: playerScalarPropertyCardsInHand},
	state.StrEntry[playerScalarPropertyCode]{Key: "CardsInGraveyard", Val: playerScalarPropertyCardsInGraveyard},
	state.StrEntry[playerScalarPropertyCode]{Key: "CardsInLibrary", Val: playerScalarPropertyCardsInLibrary},
	state.StrEntry[playerScalarPropertyCode]{Key: "CardsDrawn", Val: playerScalarPropertyCardsDrawn},
	state.StrEntry[playerScalarPropertyCode]{Key: "LifeTotal", Val: playerScalarPropertyLifeTotal},
)

type cov3PlayerGroupCode uint16

const (
	cov3PlayerGroupPlayerCountPropertyYou cov3PlayerGroupCode = iota + 1
	cov3PlayerGroupPlayerCount
	cov3PlayerGroupPlayerCountBare
	cov3PlayerGroupPlayerCountOpponents
	cov3PlayerGroupPlayerCountOther
	cov3PlayerGroupPlayerCountDefinedRememberedOwner
	cov3PlayerGroupPlayerCountRegisteredOpponen
	cov3PlayerGroupPlayerCountRemembered
	cov3PlayerGroupPlayerCountRememberedControl
)

var cov3PlayerGroupCodes = state.NewStrCodes(
	state.StrEntry[cov3PlayerGroupCode]{Key: "PlayerCountPropertyYou", Val: cov3PlayerGroupPlayerCountPropertyYou},
	state.StrEntry[cov3PlayerGroupCode]{Key: "PlayerCount", Val: cov3PlayerGroupPlayerCountBare},
	state.StrEntry[cov3PlayerGroupCode]{Key: "PlayerCountPlayers", Val: cov3PlayerGroupPlayerCount},
	state.StrEntry[cov3PlayerGroupCode]{Key: "PlayerCountOpponents", Val: cov3PlayerGroupPlayerCountOpponents},
	state.StrEntry[cov3PlayerGroupCode]{Key: "PlayerCountOther", Val: cov3PlayerGroupPlayerCountOther},
	state.StrEntry[cov3PlayerGroupCode]{Key: "PlayerCountDefinedRememberedOwner", Val: cov3PlayerGroupPlayerCountDefinedRememberedOwner},
	state.StrEntry[cov3PlayerGroupCode]{Key: "PlayerCountRegisteredOpponents", Val: cov3PlayerGroupPlayerCountRegisteredOpponen},
	state.StrEntry[cov3PlayerGroupCode]{Key: "PlayerCountRemembered", Val: cov3PlayerGroupPlayerCountRemembered},
	state.StrEntry[cov3PlayerGroupCode]{Key: "PlayerCountRememberedController", Val: cov3PlayerGroupPlayerCountRememberedControl},
)

type cov3PlayerPropCode uint16

const (
	cov3PlayerPropAmount cov3PlayerPropCode = iota + 1
	cov3PlayerPropValid
	cov3PlayerPropLifeLostThisTurn
)

var cov3PlayerPropCodes = state.NewStrCodes(
	state.StrEntry[cov3PlayerPropCode]{Key: "Amount", Val: cov3PlayerPropAmount},
	state.StrEntry[cov3PlayerPropCode]{Key: "Valid", Val: cov3PlayerPropValid},
	state.StrEntry[cov3PlayerPropCode]{Key: "LifeLostThisTurn", Val: cov3PlayerPropLifeLostThisTurn},
)

type cov3PlayerScalarCode uint16

const (
	cov3PlayerScalarSacrificedThisTurn cov3PlayerScalarCode = iota + 1
	cov3PlayerScalarSacrificedPermanentTypesThis
	cov3PlayerScalarCardsDrawn
	cov3PlayerScalarNonCombatDamageDealtThisTurn
	cov3PlayerScalarOpponentsAttackedThisTurn
	cov3PlayerScalarLifeLostLastTurn
	cov3PlayerScalarAttackersDeclared
	cov3PlayerScalarHasPropertyBeenAttackedThisC
	cov3PlayerScalarOpponentsAttackedThisCombat
	cov3PlayerScalarHasPropertyAttackedYouTheirLastTurn
	cov3PlayerScalarDomainPlayer
)

var cov3PlayerScalarCodes = state.NewStrCodes(
	state.StrEntry[cov3PlayerScalarCode]{Key: "SacrificedThisTurn", Val: cov3PlayerScalarSacrificedThisTurn},
	state.StrEntry[cov3PlayerScalarCode]{Key: "SacrificedPermanentTypesThisTurn", Val: cov3PlayerScalarSacrificedPermanentTypesThis},
	state.StrEntry[cov3PlayerScalarCode]{Key: "CardsDrawn", Val: cov3PlayerScalarCardsDrawn},
	state.StrEntry[cov3PlayerScalarCode]{Key: "NonCombatDamageDealtThisTurn", Val: cov3PlayerScalarNonCombatDamageDealtThisTurn},
	state.StrEntry[cov3PlayerScalarCode]{Key: "OpponentsAttackedThisTurn", Val: cov3PlayerScalarOpponentsAttackedThisTurn},
	state.StrEntry[cov3PlayerScalarCode]{Key: "LifeLostLastTurn", Val: cov3PlayerScalarLifeLostLastTurn},
	state.StrEntry[cov3PlayerScalarCode]{Key: "AttackersDeclared", Val: cov3PlayerScalarAttackersDeclared},
	state.StrEntry[cov3PlayerScalarCode]{Key: "HasPropertyBeenAttackedThisCombat", Val: cov3PlayerScalarHasPropertyBeenAttackedThisC},
	state.StrEntry[cov3PlayerScalarCode]{Key: "OpponentsAttackedThisCombat", Val: cov3PlayerScalarOpponentsAttackedThisCombat},
	state.StrEntry[cov3PlayerScalarCode]{Key: "HasPropertyattackedYouTheirLastTurn", Val: cov3PlayerScalarHasPropertyAttackedYouTheirLastTurn},
	state.StrEntry[cov3PlayerScalarCode]{Key: "DomainPlayer", Val: cov3PlayerScalarDomainPlayer},
)
