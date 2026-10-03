package effects

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/state"
)

// MatchesPlayerSpec is the player-side filter: You, Opponent, Player.
// All supported player-state qualifiers share MatchesPlayerSpecCtx; source-
// dependent properties fail closed when this unbound entry point is used.
func MatchesPlayerSpec(g *state.Game, spec string, p, you state.PlayerID) bool {
	return MatchesPlayerSpecCtx(g, spec, p, you, PlayerSpecCtx{})
}

// MatchesPlayerSpecFrom resolves the source object's event-backed choice and
// attachment state (Player.Chosen, Player.IsRemembered and
// Player.EnchantedController). Keeping the source explicit lets ordinary
// player filters retain their existing API while trigger matching can supply
// its owning permanent.
func MatchesPlayerSpecFrom(g *state.Game, spec string, p, you state.PlayerID, source state.ObjID) bool {
	return MatchesPlayerSpecCtx(g, spec, p, you, PlayerSpecCtx{Source: source})
}

// PlayerSpecCtx carries the event-role bindings a player filter can need
// beyond the source object. Source is the object whose attachment and choice
// state a Player.EnchantedController / Player.IsRemembered clause resolves
// against. DefendingPlayer is the defending-player role of the triggering
// event, which Player.TriggeredDefendingPlayer names; it is absent (IsPlayer
// false) outside a trigger that carries one, so that clause fails closed.
// Layers is the SAME derived-characteristic table set SpecContext carries (see their doc comments there, including the
// escape-analysis rationale for plain immutable slices): the
// Player.controlsCreature / Player.controlsPermanent family evaluates its
// object spec through a nested SpecContext, so without them a creature a
// layer-4 static made a Goblin is not counted by a lord question even though
// every ordinary filter site now sees it. All fields are plain data so the
// rule-engine call sites (a trigger match, a static actor match and a layer
// restriction) can populate them without a resolver callback.
type PlayerSpecCtx struct {
	Source            state.ObjID
	DefendingPlayer   state.Target
	DelayedRemembered []state.Target
	OpponentOf        []state.Target
	// Layers is the board's derived-characteristic tables (LayerTables), the
	// same value SpecContext.Layers carries.
	Layers LayerTables
}

// MatchesPlayerSpecCtx is the full player-side filter: the same grammar as
// MatchesPlayerSpecFrom, with the caller's event-role bindings supplied.
// Every rule resolves here, so a trigger match, a static actor match and a
// layer restriction agree by construction rather than by parallel copies.
func MatchesPlayerSpecCtx(g *state.Game, spec string, p, you state.PlayerID, pc PlayerSpecCtx) bool {
	for alt := range strings.SplitSeq(spec, ",") {
		alt = strings.TrimSpace(alt)
		if alt == "" {
			continue
		}
		if matchesPlayerCompoundCtx(g, alt, p, you, pc) {
			return true
		}
	}
	return false
}

// matchesPlayerCompoundFrom evaluates ONE comma alternative as a `+`-joined
// conjunction of player clauses, each optionally negated with a leading `!`.
// Forge's player specs spell their properties this way -- Tribal
// Hellkite's `Choices$ Player.Opponent+!IsRemembered`
// ("choose an opponent at random that CARDNAME didn't attack"),
// `Player.Opponent+lifeEQX`, `Player.Opponent+!EnchantedBy`. Before this the
// whole `+` string was cut on the first `.` and fell into the unknown-
// qualifier branch, so every such spec matched NOBODY -- a Choices$ pool that
// is silently empty (Territorial Hellkite could never choose an opponent, so
// its random pick always took the no-candidate arm) and a ValidTgts$ pool
// that admits no target. A clause with no `+` is a one-clause conjunction and
// behaves exactly as before, so the single-qualifier grammar is unchanged.
func matchesPlayerCompoundCtx(g *state.Game, alt string, p, you state.PlayerID, pc PlayerSpecCtx) bool {
	for clause := range strings.SplitSeq(alt, "+") {
		clause = strings.TrimSpace(clause)
		if clause == "" {
			return false
		}
		neg := strings.HasPrefix(clause, "!")
		if neg {
			clause = strings.TrimSpace(clause[1:])
		}
		// A bare property clause (IsRemembered/Chosen/ChosenPlayer) with no
		// source bound cannot be evaluated at all: fail the WHOLE conjunction
		// closed, rather than letting the negation invert the absence into a
		// match. Without this a caller that passes source 0 (MatchesPlayerSpec)
		// would newly admit every un-remembered player for
		// `Player.Opponent+!IsRemembered` -- a widened pool where the old
		// grammar matched nobody.
		if pc.Source == 0 && isBarePlayerProperty(clause) && clause != "IsCorrupted" {
			return false
		}
		if matchesPlayerClauseCtx(g, clause, p, you, pc) == neg {
			return false
		}
	}
	return true
}

// matchesPlayerClauseFrom evaluates ONE player clause (no `,` or `+`). It
// first resolves the BARE property spellings a compound uses
// (`IsRemembered`, `Chosen`, `ChosenPlayer`) against the source object's
// event-backed choice state, then falls back to the ordinary single-spec
// grammar (matchesPlayerSingleSpec) for a base.qualifier form. An absent
// source fails the bare property clauses closed, exactly as the qualified
// `Player.IsRemembered` spelling already does.
func matchesPlayerClauseCtx(g *state.Game, clause string, p, you state.PlayerID, pc PlayerSpecCtx) bool {
	if ref, is := strings.CutPrefix(clause, "wasDealtDamageThisGameBy "); is {
		// Forge's bare game-long damage-by-source clause (The Fallen's
		// `Player.Opponent+wasDealtDamageThisGameBy Self`): the same
		// reading as the base.qualifier form above, reached through the
		// compound grammar. Both spellings funnel into this one body via
		// the shared damageGameRecordHas reader.
		return playerDamageByRefThisGame(g, p, you, pc, ref)
	}
	if !isBarePlayerProperty(clause) {
		return matchesPlayerSingleSpec(g, clause, p, you, pc)
	}
	switch clause {
	case "IsCorrupted":
		return playerIsCorrupted(g, p)
	case "IsRemembered", "Chosen", "ChosenPlayer":
		o := g.Obj(pc.Source)
		if o == nil {
			return false
		}
		set := o.Chosen
		if clause == "IsRemembered" {
			set = o.Remembered
		}
		for _, t := range set {
			if t.IsPlayer && t.Player == p {
				return true
			}
		}
		return false
	}
	return false
}

// isBarePlayerProperty reports whether a player clause is one of the bare
// property spellings a compound uses (`IsRemembered`, `Chosen`,
// `ChosenPlayer`), as opposed to a base.qualifier form.
func isBarePlayerProperty(clause string) bool {
	if _, is := strings.CutPrefix(clause, "wasDealtDamageThisGameBy "); is {
		// Forge's bare wasDealtDamageThisGameBy <ref>: a compound clause with
		// no base of its own (The Fallen). Its base-position companion form
		// is handled by matchesPlayerSingleSpec; both are the shared player
		// grammar, so the census gate's knownBase consults this same list.
		return true
	}
	switch clause {
	case "IsRemembered", "Chosen", "ChosenPlayer", "IsCorrupted":
		return true
	}
	return false
}

// playerBaseMatches reports whether a bare player-spec base matches seat p
// relative to the perspective seat you. It is the shared base predicate the
// ordinary qualifier switch below already spells inline (Player/Any always,
// You is the perspective seat, Opponent/Other is anyone else) and that the
// source-anchored Chosen/IsRemembered membership read now consults first, so
// a membership read can never widen past its base. An unknown base fails
// closed, exactly as the inline switch does.
func playerBaseMatches(base string, p, you state.PlayerID) bool {
	switch base {
	case "Player", "Any":
		return true
	case "You":
		return p == you
	case "Opponent", "Other":
		return p != you
	}
	return false
}

// matchesPlayerSingleSpec is the original single-alternative player-spec
// evaluator: one clause, no `,` or `+` (the callers above split those).
func matchesPlayerSingleSpec(g *state.Game, spec string, p, you state.PlayerID, pc PlayerSpecCtx) bool {
	for alt := range strings.SplitSeq(spec, ",") {
		base, qualifier, qualified := strings.Cut(strings.TrimSpace(alt), ".")
		if inner, negated := strings.CutPrefix(qualifier, "!"); qualified && negated {
			// A negated qualifier after the dot (Crown of Doom's
			// `Player.!CardOwner`, `Player.!IsRemembered`,
			// `Player.!EnchantedBy`): the base must match and the positive
			// qualifier must NOT. Only qualifiers this evaluator reads are
			// negated -- an unread one would otherwise invert its fail-closed
			// false into admitting every seat -- and a source-anchored one
			// fails closed with no source bound.
			switch inner {
			case "CardOwner", "Owner", "IsRemembered", "EnchantedBy":
			default:
				continue
			}
			if inner != "EnchantedBy" && g.Obj(pc.Source) == nil {
				continue
			}
			if matchesPlayerSingleSpec(g, base, p, you, pc) && !matchesPlayerSingleSpec(g, base+"."+inner, p, you, pc) {
				return true
			}
			continue
		}
		if (base == "Player" || base == "Any") && qualified && (qualifier == "CardOwner" || qualifier == "Owner") {
			// Player.CardOwner and Player.Owner (Forge PlayerProperty): the
			// OWNER of the filter's source object (Crown of Doom's "target
			// player other than CARDNAME's owner" negates it). No source
			// bound fails closed. Owner is Personal Incarnation's
			// "Activator$ Player.Owner" -- "Only CARDNAME's owner may
			// activate this ability".
			if o := g.Obj(pc.Source); o != nil && o.Owner == p {
				return true
			}
			continue
		}
		if qualified && (qualifier == "Chosen" || qualifier == "IsRemembered") {
			// The source-anchored chosen/remembered membership read, on EVERY
			// base the grammar evaluates (Player/Any as always, plus the
			// qualified You/Opponent/Other spellings -- Will the Wise's
			// `Defined$ Opponent.!IsRemembered` "each opponent who doesn't"
			// is the corpus carrier). Reading it only on Player/Any left the
			// Opponent-base qualifier permanently false, which the `!`
			// spelling inverted into admitting EVERY opponent. The base is
			// checked FIRST, so a membership read cannot admit the source's
			// own controller under an `Opponent` base (or an opponent under a
			// `You` base); only the set membership is base-independent. The
			// set is still the source object's own event-backed list, so every
			// base reads one home.
			if !playerBaseMatches(base, p, you) {
				continue
			}
			o := g.Obj(pc.Source)
			if o == nil {
				continue
			}
			set := o.Chosen
			if qualifier == "IsRemembered" {
				set = o.Remembered
			}
			for _, t := range set {
				if t.IsPlayer && t.Player == p {
					return true
				}
			}
			continue
		}
		matchesBase := false
		switch base {
		case "Player", "Any":
			if kind, is := strings.CutPrefix(qualifier, "withMost"); is {
				// Forge's Player.withMost<kind> property (PlayerProperty), now
				// evaluated in the shared player filter so a control grant's
				// NewController$ and a trigger's Attacked$/restriction spec
				// resolve the same seat. Unknown kinds fail closed (no seat
				// matches) exactly like every other unlisted qualifier.
				if playerHasMost(g, p, kind) {
					return true
				}
				continue
			}
			if rem, is := strings.CutPrefix(qualifier, "controlsCard."); is {
				// This ticket models only the registration-relative card form.
				if rem == "IsTriggerRemembered" && playerControlsMatches(g, p, you, pc, "Card", rem) {
					return true
				}
				continue
			}
			if rem, is := strings.CutPrefix(qualifier, "controlsCreature."); is {
				// Forge's Player.controlsCreature.<objspec> / controlsPermanent.
				// <objspec> property (PlayerControlsCreatures/Permanents): the
				// seat qualifies when its battlefield holds an object matching
				// <objspec> as an object filter, with an optional trailing
				// _GE<n>-style count comparison. See playerControlsMatches.
				if playerControlsMatches(g, p, you, pc, "Creature", rem) {
					return true
				}
				continue
			}
			if rem, is := strings.CutPrefix(qualifier, "controlsPermanent."); is {
				if playerControlsMatches(g, p, you, pc, "Permanent", rem) {
					return true
				}
				continue
			}
			matchesBase = true
		case "You":
			matchesBase = p == you
		case "Opponent", "Other":
			matchesBase = p != you
		}
		if !matchesBase {
			continue
		}
		if !qualified {
			return true
		}
		if ref, is := strings.CutPrefix(qualifier, "wasDealtDamageThisGameBy "); is {
			// Forge's Player.wasDealtDamageThisGameBy <ref> qualifier
			// (diseased_vermin's ValidTgts$ Opponent.wasDealtDamageThisGameBy
			// Self): the seat qualifies when <ref>'s objects have dealt it
			// damage this game. This reads the SAME
			// state.Player.DamageTakenByGame record, through the SAME shared
			// damageGameRecordHas helper, as the object-side word -- never a
			// parallel re-implementation.
			if playerDamageByRefThisGame(g, p, you, pc, ref) {
				return true
			}
			continue
		}
		switch qualifier {
		case "OpponentOf Remembered":
			// This supported referent is bound by a resolving ChoosePlayer's
			// Ctx.Remembered. Other OpponentOf spellings remain fail-closed;
			// their event roles need distinct, explicit bindings.
			if base == "Player" || base == "Any" {
				for _, ref := range pc.OpponentOf {
					if ref.IsPlayer && int(ref.Player) < len(g.Players) &&
						int(p) < len(g.Players) && p != ref.Player {
						return true
					}
				}
			}
		case "IsCorrupted":
			if playerIsCorrupted(g, p) {
				return true
			}
		case "You":
			if p == you {
				return true
			}
		case "Opponent", "Other":
			if p != you {
				return true
			}
		case "Active":
			if p == g.Active {
				return true
			}
		case "NonActive":
			// NonActive is the complement of Active, evaluated after the
			// Player/You/Opponent/Other base has matched.
			if p != g.Active {
				return true
			}
		case "isMonarch":
			// CR 716.2's monarch designation, on the Player/Any base only:
			// the state-local qualifier a control static's GainControl$
			// value (Fealty to the Realm's "The monarch controls enchanted
			// creature") and any other player spec resolve through. A
			// qualified You/Opponent/Other base (You.isMonarch) still fails
			// closed, like every fx20 qualifier not listed here.
			if (base == "Player" || base == "Any") && g.IsMonarch(p) {
				return true
			}
		case "EnchantedBy":
			// An Aura may enchant a player of either seat, independent of
			// the source of the filter.
			for i := range g.Objs {
				o := &g.Objs[i]
				if o.Zone == state.ZBattlefield && o.HasAttachedPlayer && o.AttachedPlayer == p &&
					o.Face() != nil && o.Face().IsEnchantment() {
					return true
				}
			}
		case "EnchantedController":
			// Player.EnchantedController (Forge PlayerProperty): the seat is
			// the controller of the permanent this Aura/Equipment source is
			// attached to. The link is the source object's own battlefield
			// state, so a static actor match (Caster$/Activator$/ValidPlayer$),
			// a phase trigger's ValidPlayer$ and a layer restriction all
			// resolve it through this one clause.
			if ctrl, ok := playerEnchantedController(g, pc.Source); ok && ctrl == p {
				return true
			}
		case "descended":
			// CR 700.11: a permanent CARD entered this player's graveyard
			// this turn from any zone. ZoneEntry captures owner and card type at
			// the move, and TurnChange clears the ledger on replay as in play.
			if descendedThisTurn(g, p) > 0 {
				return true
			}
		case "TriggeredDefendingPlayer":
			// Player.TriggeredDefendingPlayer (Forge PlayerProperty): the
			// seat that defended against the triggering combat. The role is
			// carried by the caller through PlayerSpecCtx; with no defending
			// player bound (no trigger, or an event with no defender) the
			// clause fails closed rather than widening to the trigger's own
			// controller.
			if pc.DefendingPlayer.IsPlayer && pc.DefendingPlayer.Player == p {
				return true
			}
		default:
			// Player.counters_<CMP><n>_<KIND> (Forge PlayerProperty.Counters
			// spellings, e.g. Player.counters_EQ0_Contract): the seat's
			// counter count of KIND, read off its event-backed
			// state.Player.Counters (written only by events.Apply's
			// PlayerCounterChange), compared with the same <CMP> operators
			// the object-side counters_ predicate uses. The base has already
			// matched, so this is a pure player-state read.
			if op, n, kind, ok := splitPlayerCountCompare(qualifier); ok {
				if int(p) < len(g.Players) && playerCompare(g.Players[p].Counter(kind), op, n) {
					return true
				}
				continue
			}
			// Player.NotedFor<label> (Forge's PlayerProperty.NotedFor): the
			// seat qualifies when its event-backed note set names <label>.
			// The label is written by a DB$ Pump body's NoteCardsFor$
			// parameter (effects.effPump -> events.PlayerNoted), so the read
			// reaches the shared player filter every consumer already uses --
			// RepeatEach's RepeatPlayers$, Defined$ on Draw/Discard/ChangeZone,
			// the Continuous statics' Affected$ and a Count$ head alike. The
			// Player/Any base is required (a qualified You.NotedForX fails
			// closed, like every other qualifier here), the label is matched
			// EXACTLY (case-sensitive, as Forge's string set is), and an
			// out-of-range seat fails closed.
			if base == "Player" || base == "Any" {
				if label, is := strings.CutPrefix(qualifier, "NotedFor"); is && label != "" {
					if int(p) < len(g.Players) && playerHasNote(g, p, label) {
						return true
					}
					continue
				}
			}
			if int(p) < len(g.Players) {
				op, n, ok := splitPlayerCompare(qualifier)
				if ok && playerCompare(g.Players[p].Life, op, n) {
					return true
				}
			}
		}
	}
	return false
}

// damageGameRecordHas reports whether a game-long damage-by-source record
// (state.Object.DamageTakenByGame or state.Player.DamageTakenByGame) names
// src as a source that has dealt this recipient damage this game. It is the
// ONE shared reader behind both spellings: the object filter's
// wasDealtDamageThisGameBy/wasDealtDamageByThisGame words and the player
// filter's wasDealtDamageThisGameBy qualifier, so the object and player
// readings of Forge's game-long record can never drift. src==0 never matches
// (no source was bound; callers also refuse the unbound case through
// contextPredicateBound). Walked by index, never a map range.
func damageGameRecordHas(ids []state.ObjID, src state.ObjID) bool {
	if src == 0 {
		return false
	}
	for _, id := range ids {
		if id == src {
			return true
		}
	}
	return false
}

// playerDamageByRefThisGame is the shared body behind BOTH player spellings
// of Forge's game-long damage-by-source qualifier -- the base.qualifier form
// (diseased_vermin's Opponent.wasDealtDamageThisGameBy Self, reached through
// matchesPlayerSingleSpec) and the bare compound clause (The Fallen's
// Player.Opponent+wasDealtDamageThisGameBy Self, reached through
// matchesPlayerClauseCtx). It resolves <ref> through the same shared
// referent switch the object word uses and tests the seat's
// state.Player.DamageTakenByGame record with the shared
// damageGameRecordHas reader, so the object and player readings of one
// state record cannot drift. An out-of-range seat or an unresolvable ref
// matches nobody (fail closed).
func playerDamageByRefThisGame(g *state.Game, p, you state.PlayerID, pc PlayerSpecCtx, ref string) bool {
	if int(p) >= len(g.Players) {
		return false
	}
	sc := NewSpecContext(you, pc.Source)
	sc.DefendingPlayer = pc.DefendingPlayer
	for _, t := range sharesTypeReferents(g, sc, strings.TrimSpace(ref)) {
		if t.IsPlayer {
			continue
		}
		if damageGameRecordHas(g.Players[p].DamageTakenByGame, t.Obj) {
			return true
		}
	}
	return false
}

// descendedThisTurn counts the permanent CARDS that entered player p's
// graveyard this turn from any zone (CR 700.11). This is the single read of
// the fx20 descend provenance: the Player.descended predicate and the
// Count$YouDescendedThisTurn head both call it, so they cannot disagree.
// A token or a nonpermanent card never counts ($PermanentCard is folded
// from !IsToken/!IsCopy/IsPermanent at the move).
func descendedThisTurn(g *state.Game, p state.PlayerID) int32 {
	var n int32
	for _, entry := range g.Entered {
		if entry.To == state.ZGraveyard && entry.Owner == p && entry.PermanentCard {
			n++
		}
	}
	return n
}

// playerIsCorrupted applies the Corrupted threshold (three or more poison
// counters) to a valid player seat.
func playerIsCorrupted(g *state.Game, p state.PlayerID) bool {
	return int(p) >= 0 && int(p) < len(g.Players) && g.Players[p].Counter("POISON") >= 3
}

// playerHasNote reports whether the seat's event-backed player-notation set
// names label. The note set is state.Player.Notes, written only by
// events.Apply's PlayerNoted case (a DB$ Pump body's NoteCardsFor$), so a
// live game and a log-only replay answer identically. An out-of-range seat
// fails closed. There is no map range here (the slice is walked in its
// append order), so the result is deterministic.
func playerHasNote(g *state.Game, p state.PlayerID, label string) bool {
	if int(p) >= len(g.Players) {
		return false
	}
	for _, n := range g.Players[p].Notes {
		if n == label {
			return true
		}
	}
	return false
}

// splitCountCompare strips a trailing "_"-separated count comparison token
// ("GE1", "LT3", ...) from an object-spec remainder. It returns the remainder
// with the token removed, the comparison operator, the threshold, and whether
// a count token was present at all. A trailing token that is not a count
// comparison (e.g. the named-arg convention's "namedAether_Burst") stays part
// of the object spec, and a remainder with no "_" at all is returned whole.
func splitCountCompare(rem string) (string, string, int32, bool) {
	i := strings.LastIndex(rem, "_")
	if i < 0 {
		return rem, "", 0, false
	}
	tok := rem[i+1:]
	if len(tok) < len("GE0") {
		return rem, "", 0, false
	}
	op, digits := tok[:2], tok[2:]
	n, err := strconv.ParseInt(digits, 10, 32)
	if err != nil {
		return rem, "", 0, false
	}
	switch op {
	case "GE", "GT", "EQ", "LE", "LT":
		return rem[:i], op, int32(n), true
	}
	return rem, "", 0, false
}

// playerControlsMatches evaluates Forge's Player.controlsCreature.<spec> /
// controlsPermanent.<spec> qualifiers (PlayerProperty's
// PlayerControlsCreatures/PlayerControlsPermanents family): the seat
// qualifies when the required number of its battlefield objects match
// <spec> as an object filter. The count comparison rides a trailing
// "_GE<n>"-style token and defaults to an existential _GE1; a spec with no
// count token matches when at least one object does. The object filter is
// evaluated with the same SpecContext binding MatchesPlayerSpecFrom carries
// (the perspective seat and the source permanent), so the named<Name>,
// MultiColor, IsRemembered and EnchantedBy object predicates all resolve
// unchanged. The controlsCard.IsTriggerRemembered caller additionally binds
// the delayed registration's capture. A spec that matches nothing -- including
// one carrying an unmodelled predicate, which fails closed inside the object
// matcher -- never matches for that seat.
func playerControlsMatches(g *state.Game, p state.PlayerID, you state.PlayerID, pc PlayerSpecCtx, objBase, rem string) bool {
	spec, op, want, counted := splitCountCompare(rem)
	spec = objBase + "." + spec
	sc := NewSpecContext(you, pc.Source)
	sc.DelayedRemembered = pc.DelayedRemembered
	// The nested object filter must see the same layer-3/layer-4
	// derived characteristics every ordinary filter site does; a
	// controlsCreature/controlsPermanent spec otherwise reads the
	// printed face alone.
	sc.Layers = pc.Layers
	n := int32(0)
	for _, id := range g.Zone(state.ZBattlefield, p) {
		if MatchesObjectCtx(g, spec, g.Obj(id), sc) {
			n++
		}
	}
	if !counted {
		return n > 0
	}
	return playerCompare(n, op, want)
}

// playerHasMost is the shared evaluator for Forge's Player.withMost<kind>
// property (PlayerProperty.java). Kinds the corpus spells: Life (ties match
// -- every seat holding the maximum life), CardsInHand (Forge's
// strictly-greater scan keeps the FIRST holder in player order on a tie),
// PermanentInPlay (most permanents; ties match every holder) and
// Type<X>[Only] (most battlefield permanents of type X; "Only" requires a
// UNIQUE holder, and when the top count is shared nobody matches -- Forge
// returns false for every player). Dead seats take part in the scan exactly
// like Forge's game.getPlayers(); the control-grant caller walks only the
// living seats before consulting this, so a dead seat can win a filter match
// but never gain control.
func playerHasMost(g *state.Game, p state.PlayerID, kind string) bool {
	if int(p) >= len(g.Players) {
		return false
	}
	only := false
	if x, is := strings.CutSuffix(kind, "Only"); is {
		only = true
		kind = x
	}
	switch kind {
	case "Life":
		best := g.Players[0].Life
		for i := range g.Players {
			if g.Players[i].Life > best {
				best = g.Players[i].Life
			}
		}
		return g.Players[p].Life == best
	case "CardsInHand":
		// Forge's getPlayerWithMostCardsInHand starts with no candidate and
		// only binds when a player has a positive hand; all-empty hands name
		// nobody. Ties retain the first player in seat order.
		best, holder := 0, -1
		for i := range g.Players {
			if n := len(g.Zone(state.ZHand, state.PlayerID(i))); n > best {
				best, holder = n, i
			}
		}
		return holder >= 0 && int(p) == holder
	}
	count := func(pi state.PlayerID) int {
		n := 0
		for _, id := range g.Zone(state.ZBattlefield, pi) {
			o := g.Obj(id)
			if o == nil {
				continue
			}
			if t, is := strings.CutPrefix(kind, "Type"); is {
				if hasType(o, t) {
					n++
				}
				continue
			}
			if kind == "PermanentInPlay" {
				n++
			}
		}
		return n
	}
	best, holders := -1, 0
	for i := range g.Players {
		n := count(state.PlayerID(i))
		if n > best {
			best, holders = n, 1
		} else if n == best {
			holders++
		}
	}
	// Forge requires a unique leader for PermanentInPlay as well as the
	// explicit Type...Only spelling. A shared top count names nobody.
	if (only || kind == "PermanentInPlay") && holders != 1 {
		return false
	}
	return count(p) == best
}

// splitPlayerCompare accepts Forge's literal lifeGE1/lifeLT7 qualifiers.
func splitPlayerCompare(s string) (string, int32, bool) {
	op, rhs, ok := splitPlayerCompareToken(s)
	if !ok {
		return "", 0, false
	}
	n, err := strconv.ParseInt(rhs, 10, 32)
	if err != nil {
		return "", 0, false
	}
	return op, int32(n), true
}

func splitPlayerCompareToken(s string) (string, string, bool) {
	if !strings.HasPrefix(s, "life") || len(s) < len("lifeGE0") {
		return "", "", false
	}
	op := s[4:6]
	switch op {
	case "GE", "GT", "EQ", "LE", "LT":
		return op, s[6:], true
	}
	return "", "", false
}

// MatchesPlayerSpecWithSVars resolves symbolic life-comparison thresholds in
// the asking ability's SVar table, then delegates all other grammar to the
// shared player matcher. Unknown count bodies fail closed for that alternative.
func MatchesPlayerSpecWithSVars(h Host, c *Ctx, spec string, p, you state.PlayerID) bool {
	if h == nil || c == nil {
		return false
	}
	for alt := range strings.SplitSeq(spec, ",") {
		clauses := strings.Split(strings.TrimSpace(alt), "+")
		resolved := true
		for i, clause := range clauses {
			neg := strings.HasPrefix(clause, "!")
			plain := strings.TrimPrefix(clause, "!")
			base, qualifier, hasDot := strings.Cut(plain, ".")
			if !hasDot {
				base, qualifier = "Player", plain
			}
			if base != "Player" && base != "Opponent" && base != "Other" && base != "You" {
				continue
			}
			op, rhs, ok := splitPlayerCompareToken(qualifier)
			if !ok {
				continue
			}
			if _, err := strconv.ParseInt(rhs, 10, 32); err == nil {
				continue
			}
			body, found := c.SVars[rhs]
			if !found {
				if o := h.Game().Obj(c.Source); o != nil && o.Face() != nil {
					body, found = o.Face().SVars[rhs]
				}
			}
			if !found {
				resolved = false
				break
			}
			threshold, ok := EvalCountOK(h, c, body)
			if !ok {
				resolved = false
				break
			}
			prefix := ""
			if neg {
				prefix = "!"
			}
			clauses[i] = prefix + base + ".life" + op + strconv.FormatInt(int64(threshold), 10)
		}
		if resolved && MatchesPlayerSpecCtx(h.Game(), strings.Join(clauses, "+"), p, you, PlayerSpecCtx{
			Source:     c.Source,
			OpponentOf: c.Remembered,
		}) {
			return true
		}
	}
	return false
}

// playerEnchantedController returns the controller of the permanent this
// Aura/Equipment source is attached to, and whether that link exists. A
// missing source, an unattached source, or a bearer that has left the game
// fails closed. A player-attached Aura has no permanent bearer, so it does
// not satisfy this distinct EnchantedController property.
func playerEnchantedController(g *state.Game, source state.ObjID) (state.PlayerID, bool) {
	o := g.Obj(source)
	if o == nil || o.AttachedTo == 0 {
		return 0, false
	}
	bearer := g.Obj(o.AttachedTo)
	if bearer == nil {
		return 0, false
	}
	return bearer.Controller, true
}

// splitPlayerCountCompare parses Forge's player-counter qualifier
// counters_<CMP><n>_<KIND> (e.g. counters_EQ0_Contract). It mirrors the
// object-side counters_ predicate's grammar so the player and object
// spellings cannot drift; an unknown <CMP>, a missing/empty KIND or a
// non-integer RHS fails closed (a resolver-bearing RHS is not carried on the
// player side, so a symbolic RHS is simply unrecognised).
func splitPlayerCountCompare(s string) (string, int32, string, bool) {
	rest, ok := strings.CutPrefix(s, "counters_")
	if !ok || len(rest) < 4 {
		return "", 0, "", false
	}
	op := rest[:2]
	numStr, kind, okSplit := strings.Cut(rest[2:], "_")
	if !okSplit || kind == "" {
		return "", 0, "", false
	}
	n, err := strconv.Atoi(numStr)
	if err != nil {
		return "", 0, "", false
	}
	switch op {
	case "GE", "GT", "EQ", "LE", "LT":
		return op, int32(n), kind, true
	}
	return "", 0, "", false
}

func playerCompare(have int32, op string, want int32) bool {
	switch op {
	case "GE":
		return have >= want
	case "GT":
		return have > want
	case "EQ":
		return have == want
	case "LE":
		return have <= want
	case "LT":
		return have < want
	}
	return false
}
