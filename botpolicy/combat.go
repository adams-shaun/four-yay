package botpolicy

import (
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/deck"
	"github.com/adams-shaun/gorge/state"
)

// Chars is the derived-characteristics surface the game-shaped half of the
// adapter pair reads a state.Game through. *rules.Engine satisfies it (the
// view package's Chars interface already pins Engine.Power/Toughness/
// Keywords with a compile-time assertion), so BoardFromGame derives exactly
// the post-effects P/T and keyword set the view-shaped half reads off the
// projected View — without botpolicy importing rules or view (Ruling F7).
type Chars interface {
	Power(state.ObjID) int32
	Toughness(state.ObjID) int32
	Keywords(state.ObjID) []string
}

// combinedChars is an optional fast path for Chars implementations that can
// derive power, toughness and keywords together. The returned keyword slice
// may be scratch-backed, so callers must copy it only when retaining it across
// the next Characteristics call. Implementations that provide only Chars keep
// the legacy per-characteristic path unchanged.
type combinedChars interface {
	Characteristics(state.ObjID) (power, toughness int32, keywords []string)
}

// derivedReadScoper is an optional Chars fast path: an implementation that
// memoizes derived characteristics for the duration of a pure read
// (*rules.Engine: BeginDerivedReads/EndDerivedReads) lets the board build
// reuse what the engine derived at this same state -- the priority offer
// walk the decision was just built from -- instead of recomputing it. The
// build reads only between the two calls and never mutates the engine.
type derivedReadScoper interface {
	BeginDerivedReads()
	EndDerivedReads()
}

// Creature is one battlefield creature's combat-relevant facts, in the
// plain-data shape that keeps the adapter pair in step: the view-shaped
// half (seat/bot.go's BoardFromView) reads Power/Toughness/Keywords off the
// projected View (which the engine derives post-effects) and the rest off
// public CardView fields; the game-shaped half (BoardFromGame) reads the
// same facts off the engine and state.Game. seat/integration_test.go's
// TestBotAdaptersAgreeOverWholeGame pins the two halves to identical facts
// over a whole game. Fields exist only because a heuristic branch reads
// them; a fact no branch reads would be untested surface.
//
// Keywords carries the engine's derived keyword list, parameter-stripped
// and case-insensitively comparable by hasKeyword, so a keyword that means
// something in rules (Flying gates blocking, Deathtouch kills at any
// amount, ...) means the same thing here on both halves; keywords whose
// combat effect is not visible in the board facts (an attack trigger, a
// can't-be-blocked static) are deliberately not guessed at.
type Creature struct {
	Power      int32
	Toughness  int32
	Damage     int32
	Keywords   []string
	Tapped     bool
	Controller state.PlayerID
}

// hasKeyword is the engine's own keyword match (rules/engine.go's
// cardsKeywordHead) re-expressed for a Creature, so botpolicy and rules
// agree on what a derived keyword is without botpolicy importing rules.
func (c Creature) hasKeyword(kw string) bool {
	for _, k := range c.Keywords {
		if strings.EqualFold(cards.KeywordHead(k), kw) {
			return true
		}
	}
	return false
}

// remTough is how much more damage this creature can take this turn before
// combat damage is lethal to it (CR 119.6: damage accumulates).
func (c Creature) remTough() int32 { return c.Toughness - c.Damage }

// pt is the policy's proxy for a creature's worth: power plus toughness.
// It is deliberately crude — no cost, no ability value — and it is only
// ever used to decide which of two trades is favourable; the rules in
// chooseAttackers/chooseBlockers say exactly where.
func (c Creature) pt() int32 { return c.Power + c.Toughness }

// NewBoard returns a Board whose three maps are allocated at the sizes
// BoardFromGame fills them to (empty, but ready to grow without reallocating
// their buckets), for a caller that intends to reuse them across decisions
// via BoardFromGameInto. The host match loop keeps one such Board per match
// (Task d2): a cleared map keeps its buckets, so reusing it costs none of
// the per-call allocation a fresh make(map, n) would. capCapitals is the seat
// count the Life map is sized to (len(g.Players) at the time).
func NewBoard(numPlayers int) Board {
	return Board{
		Creatures:  makeTable[state.ObjID, Creature](32),
		Life:       makeTable[state.PlayerID, int32](numPlayers),
		Cards:      makeTable[state.ObjID, Card](16),
		Commanders: makeTable[state.ObjID, Commander](8),
		Stack:      make([]StackEntry, 0, 8),
		inc:        new(boardInc),
	}
}

// BoardFromGame is the game-shaped half of the adapter pair: the Board a
// decision is answered from, derived from the engine's own state.Game the
// way seat/bot.go's BoardFromView derives it from the projected View a real
// client would receive. me is the player the decision that will be answered
// is asked of — the same seat whose hand the view-shaped half projects — so
// the casting Card facts below come from exactly the zones that seat may
// legally see. Both halves read public facts (every player's battlefield,
// each player's life) plus the deciding seat's own private ones (its hand
// and graveyard), so agreeing on them is agreeing on what that seat can
// see, not cheating that happens to be consistent. The creature census
// matches the View's exactly: every object the View lists in each
// battlefield (it already drops cardless ability objects and
// off-battlefield ephemerals), further filtered to creatures the same way
// BoardFromView filters on the joined type list (cards/face.go's hasType is
// an EqualFold membership check on exactly those words).
func BoardFromGame(g *state.Game, ch Chars, me state.PlayerID) Board {
	b := NewBoard(len(g.Players))
	return BoardFromGameInto(g, ch, me, &b)
}

// BoardFromGameInto is BoardFromGame's buffered-reuse form (Task d2): it
// fills the Board derivable from state.Game into the caller-owned Board b,
// clearing and reusing b's own three maps rather than allocating fresh ones
// — clear on a map keeps its buckets, so a Board built once (NewBoard) and
// refilled per decision costs no per-decision map allocation, which is the
// bulk of BoardFromGame's measured cumulative in ./host. It returns *b.
//
// OWNERSHIP CONTRACT: the returned Board's maps ARE b's maps (shared
// references), only repopulated. The host match loop refills one Board per
// decision, so a Board is only valid until the next refill overwrites it: a
// caller (a seat, the policy) must not read or retain a Board, or any of
// its maps, once the decision has been answered. botpolicy.Decide and
// everything it calls never retain the Board or its maps beyond the call
// (pinned by TestBoardOwnership), which is what makes the host's
// build-under-lock → Decide → reuse-next loop safe.
func BoardFromGameInto(g *state.Game, ch Chars, me state.PlayerID, b *Board) Board {
	if sc, ok := ch.(derivedReadScoper); ok {
		sc.BeginDerivedReads()
		defer sc.EndDerivedReads()
	}
	combined, hasCombined := ch.(combinedChars)
	b.Creatures.Reset()
	b.Life.Reset()
	b.Cards.Reset()
	b.Commanders.Reset()
	b.kwArena = b.kwArena[:0]
	// The public stack census (C8's facts): the stack's own bottom-to-top
	// order, truncated in place so the reused Board's slice never carries a
	// stale entry from the previous refill (the same clear-the-buckets
	// discipline the maps above get). IsSpell is o.Ability == nil — exactly
	// the test the view-shaped half mirrors as StackView.Kind == "spell"
	// (view/view.go's stackViews projects an ability object as
	// "trigger"/"ability" and a card object as "spell"), so the two halves
	// agree entry for entry, order included, on every intent of a whole
	// game (seat/integration_test.go's parity tests).
	b.Stack = b.Stack[:0]
	for _, id := range g.Stack {
		o := g.Obj(id)
		if o == nil {
			continue
		}
		var cmc int32
		var manaCost string
		if f := o.Face(); f != nil {
			cmc = cmcOfFace(f)
			// Only a spell carries a printed payment; an ability object is
			// Face-less anyway, and the view half's StackView.Card is nil for
			// a "trigger"/"ability", so guarding on IsSpell keeps the halves
			// in step even if a face ever rides an ability stack object.
			if o.Ability == nil {
				manaCost = f.ManaCost
			}
		}
		b.Stack = append(b.Stack, StackEntry{ID: id, Controller: o.Controller, IsSpell: o.Ability == nil, CMC: cmc, ManaCost: manaCost})
	}
	// The manifest is read, never written, by everything a Board reaches,
	// so the engine's shared read-only pointer (rules.Engine.OwnDeckShared)
	// serves it without the per-decision copy OwnDeck makes; a Chars that
	// only offers OwnDeck still gets its copy.
	if shared, ok := ch.(interface {
		OwnDeckShared(state.PlayerID) *deck.Manifest
	}); ok {
		b.OwnDeck = shared.OwnDeckShared(me)
	} else if manifests, ok := ch.(interface {
		OwnDeck(state.PlayerID) *deck.Manifest
	}); ok {
		b.OwnDeck = manifests.OwnDeck(me)
	} else {
		b.OwnDeck = nil
	}
	b.IsMain = g.Step.IsMain()
	// The cast scorer's two board-half features (cast.go): FirstMain is the
	// FIRST main phase (the Precombat feature) and MyTurn whether the
	// deciding seat is the active player (the InstantOnOwnTurn feature's
	// "own main phase" half -- a main phase can belong to another seat, so
	// IsMain alone cannot say it). Same function of the same engine state
	// the view half reads off the projected View (v.Phase == "main1",
	// v.Active == v.Viewer), so the halves agree wherever they agree on
	// IsMain itself.
	b.FirstMain = g.Step == state.StepMain1
	b.MyTurn = g.Active == me
	// The exact engine step (the cast scorer's timing features): the game
	// half reads g.Step directly; the view half parses the projected
	// View.Step string (view/view.go sets it to g.Step.String()) back
	// through state.ParseStep, so both halves name the same step.
	b.Step = g.Step
	b.Pool = g.Players[me].Pool
	b.PoolRestricted = RestrictedPool(g.Players[me].RestrictedMana)
	b.LibrarySize = int32(len(g.Zone(state.ZLibrary, me)))
	b.HandSize = int32(len(g.Zone(state.ZHand, me)))
	if b.fillTablesInc(g, ch, me) {
		fillCommanders(g, b)
		if boardIncVerify {
			verifyIncBoard(g, ch, me, b)
		}
		return *b
	}
	for i := range g.Players {
		p := &g.Players[i]
		b.Life.Set(p.ID, p.Life)
		for _, id := range g.Zone(state.ZBattlefield, p.ID) {
			o := g.Obj(id)
			if o == nil || o.Face() == nil || o.Ephemeral() || !o.Face().IsCreature() {
				continue
			}
			var power, toughness int32
			var keywords []string
			if hasCombined {
				power, toughness, keywords = combined.Characteristics(id)
			} else {
				power = ch.Power(id)
				toughness = ch.Toughness(id)
				keywords = ch.Keywords(id)
			}
			// The Board's own copy of the derived list (the engine's slice
			// is scratch), carved from the reused keyword arena: capped at
			// its length so no reader's append can reach a neighbour, and
			// nil for an empty list exactly as the per-creature copy was.
			if len(keywords) == 0 {
				keywords = nil
			} else {
				start := len(b.kwArena)
				b.kwArena = append(b.kwArena, keywords...)
				keywords = b.kwArena[start:len(b.kwArena):len(b.kwArena)]
			}
			*b.Creatures.slot(id) = Creature{
				Power:      power,
				Toughness:  toughness,
				Damage:     o.Damage,
				Keywords:   keywords,
				Tapped:     o.Tapped,
				Controller: o.Controller,
			}
		}
	}
	fillCommanders(g, b)
	// The casting Card census: every object in the deciding seat's own hand,
	// graveyard, battlefield and command zone — exactly the zones
	// BoardFromView fills from the viewer's own Hand/Graveyard/Battlefield/
	// Command CardViews. Reading the face's Types and ManaCost and the
	// engine's derived Power here, and the CardView's matching fields on the
	// view side, fills the same fact with the same function (CmcOf,
	// hasTypeWord), so a card ranks identically on both halves — including
	// a commander sitting in the command zone, which is why the casting
	// rule can read its power and mana value like any other castable. The
	// zone walk also fills the two tap-gate facts (tap.go, T1): ManaCost is
	// the printed cost the gate re-parses for coloured pips, and Castable
	// is the zone membership that says whether a card is worth mana at all
	// — true for a hand card (the engine offers its cast as soon as the
	// pool pays the cost), true for the command zone (a commander the CR
	// 903.8 tax prices), true for a graveyard card with the Flashback
	// keyword (derived, mirroring rules/legal.go's own flashback gate read
	// off the same Derived keyword list the View projects), and false for
	// the battlefield, whose permanents are already cast.
	for _, z := range [...]state.Zone{state.ZHand, state.ZGraveyard, state.ZBattlefield, state.ZCommand} {
		for _, id := range g.Zone(z, me) {
			o := g.Obj(id)
			if o == nil || o.Ephemeral() {
				continue
			}
			f := o.Face()
			if f == nil {
				continue
			}
			var power, toughness int32
			var castable, instantSpeed bool
			if cr := b.ownCreature(hasCombined, z, id); cr != nil {
				// An own battlefield creature: the creature census above
				// already holds its combined characteristics (keywords
				// included, copied), so read them instead of querying the
				// same object again. A battlefield card is never castable.
				power, toughness = cr.Power, cr.Toughness
				instantSpeed = f.TypeLineHas("Instant", twInstant) || hasFlash(cr.Keywords)
			} else if hasCombined {
				var keywords []string
				power, toughness, keywords = combined.Characteristics(id)
				castable = z == state.ZHand || z == state.ZCommand || (z == state.ZGraveyard && hasFlashback(keywords))
				instantSpeed = f.TypeLineHas("Instant", twInstant) || hasFlash(keywords)
			} else {
				power = ch.Power(id)
				toughness = ch.Toughness(id)
				castable = z == state.ZHand || z == state.ZCommand || (z == state.ZGraveyard && hasFlashback(ch.Keywords(id)))
				instantSpeed = f.TypeLineHas("Instant", twInstant) || hasFlash(ch.Keywords(id))
			}
			*b.Cards.slot(id) = Card{
				Creature:      f.IsCreature(),
				Power:         power,
				Toughness:     toughness,
				CMC:           cmcOfFace(f),
				Basic:         f.TypeLineHas("Basic", twBasic),
				AttachedTo:    o.AttachedTo,
				Activated:     o.ActivatedThisTurn,
				ManaCost:      f.ManaCost,
				Castable:      castable,
				OnBattlefield: z == state.ZBattlefield,
				Tapped:        o.Tapped,
				Sick:          o.SummonSick,
				Produces:      f.ManaProduction(),
				InstantSpeed:  instantSpeed,
				Counter:       isCounterSpell(f),
			}
		}
	}
	// The public battlefield census (vote_card1): every OTHER player's
	// battlefield permanents — public information under CR 400.2, and
	// exactly the objects a ballot offers a voter (VoteCard$'s Council's
	// Judgment filter names only permanents the caster does not control) —
	// filled with the same WORTH facts the viewer's own battlefield walk
	// fills above (Creature/Power/Toughness/CMC/Basic/ManaCost, what
	// cardWorth prices), so a card ballot's bot policy prices an offered
	// opponent permanent instead of reading the zero Card{} it got when
	// b.Cards held the deciding seat's own zones alone.
	//
	// The seat-relative facts stay ZERO on a foreign entry: OnBattlefield,
	// Produces, Tapped, Castable, Activated, InstantSpeed and AttachedTo
	// are the deciding seat's OWN-board facts (cast.go's land-drop greedy
	// and reserve hold the invariant that every OnBattlefield/Castable
	// entry is a source the seat itself controls — producibleMana and
	// availableColours would otherwise count an opponent's lands as the
	// seat's own mana), and a foreign permanent must never inflate them.
	for i := range g.Players {
		p := &g.Players[i]
		if p.ID == me {
			continue
		}
		for _, id := range g.Zone(state.ZBattlefield, p.ID) {
			o := g.Obj(id)
			if o == nil || o.Face() == nil || o.Ephemeral() {
				continue
			}
			if o.FaceDown {
				// CR 708.5 redaction parity: another seat's facedown
				// permanent projects as a stripped CardView (ID, Controller,
				// Owner, nothing printed), so the view half's fillZone lands
				// a zero-fact entry for it and this half writes the same
				// entry, never a printed-face fact the voter cannot see.
				b.Cards.Set(id, Card{})
				continue
			}
			var power, toughness int32
			if cr := b.Creatures.Ref(id); cr != nil {
				// The public creature census above (the ZBattlefield pass that
				// walks every seat) already queried this object's combined
				// characteristics; reuse them rather than querying the same
				// object twice (TestBoardFromGameUsesCombinedCharacteristicsOncePerObject
				// pins one combined query per projected object).
				power, toughness = cr.Power, cr.Toughness
			} else if hasCombined {
				power, toughness, _ = combined.Characteristics(id)
			} else {
				power = ch.Power(id)
				toughness = ch.Toughness(id)
			}
			f := o.Face()
			*b.Cards.slot(id) = Card{
				Creature:  f.IsCreature(),
				Power:     power,
				CMC:       cmcOfFace(f),
				Basic:     f.TypeLineHas("Basic", twBasic),
				ManaCost:  f.ManaCost,
				Toughness: toughness,
			}
		}
	}
	return *b
}

// The commander bookkeeping: every commander in the match, in the
// dense order rules.New assigns at genesis (player order, then each
// player's CmdCasts-parallel Commanders order) — the same index every
// player's CmdDamage slice is keyed by, transposed here to the
// per-commander, per-damaged-player shape the clock rules read
// (closesClock). InCommandZone is zone-LIST membership, the exact
// mirror of the view half's p.Command membership, so a cast commander
// (moved out of the zone list) reads false on both halves.
func fillCommanders(g *state.Game, b *Board) {
	dense := 0
	for i := range g.Players {
		p := &g.Players[i]
		for k, id := range p.Commanders {
			var casts int32
			if k < len(p.CmdCasts) {
				casts = p.CmdCasts[k]
			}
			cmdr := Commander{Casts: casts}
			for _, zid := range g.Zone(state.ZCommand, p.ID) {
				if zid == id {
					cmdr.InCommandZone = true
					break
				}
			}
			for q := range g.Players {
				// Guarded to totality: a game whose CmdDamage was never
				// sized (a non-Commander game, or a hand-built state) reads
				// nothing here, never a panic.
				if dense < len(g.Players[q].CmdDamage) && g.Players[q].CmdDamage[dense] != 0 {
					if cmdr.Damage == nil {
						cmdr.Damage = make(map[state.PlayerID]int32, len(g.Players))
					}
					cmdr.Damage[g.Players[q].ID] = g.Players[q].CmdDamage[dense]
				}
			}
			b.Commanders.Set(id, cmdr)
			dense++
		}
	}
}

// isCounterSpell reports whether f's spell ability is a Counter (one
// SpellAbility lookup, not two).
func isCounterSpell(f *cards.Face) bool {
	sa := f.SpellAbility()
	return sa != nil && sa.API == "Counter"
}

// ownCreature is the creature census entry the Cards pass may reuse for id:
// only an own battlefield card, and only when the census was filled from
// the combined characteristics query.
func (b *Board) ownCreature(hasCombined bool, z state.Zone, id state.ObjID) *Creature {
	if !hasCombined || z != state.ZBattlefield {
		return nil
	}
	return b.Creatures.Ref(id)
}

// canBlockLike is the policy's approximation of the engine's canBlock for
// creatures it is only evaluating (the defender's options during declare
// attackers), not deciding over: the blocker must be untapped and, against
// a flying attacker, itself fly or have Reach (CR 702.9b). The engine's
// offered option list is the authority for actual block choices; here the
// gates the bot cannot see (protection, Can't-Block statics) make it
// overestimate the block risk, which errs toward staying home.
func canBlockLike(a, b Creature) bool {
	if b.Tapped {
		return false
	}
	if a.hasKeyword("Flying") && !b.hasKeyword("Flying") && !b.hasKeyword("Reach") {
		return false
	}
	return true
}

// blockCombat simulates the engine's combat-damage model for one attacker a
// against an ordered block list: a deals its power to the blockers in
// order, lethal damage to each in turn, the last absorbing the remainder;
// every surviving blocker deals its power back; First Strike runs as its
// own earlier step and a creature dead after that step does not act in the
// regular one (rules/combat.go's damageStep, including Deathtouch's
// kill-at-any-amount). The simulation is only ever used to ORDER a
// decision, never to mutate game state, so a small divergence from the
// engine's exact resolution would cost a slightly off bot move, not a
// replay violation.
func blockCombat(a Creature, block []Creature) (aDead bool, dead []bool) {
	dead = make([]bool, len(block))
	aRem := a.remTough()
	taken := make([]int32, len(block))
	dt := make([]bool, len(block)) // the caller marked these with Deathtouch damage
	var aTaken int32
	aDT := false // a took Deathtouch damage from some blocker
	rem := func(i int) int32 { return block[i].remTough() }
	fsA := a.hasKeyword("First Strike")

	// aAssign is one full attack round: a's power to the blockers in order,
	// every blocker but the last capped at its own need, the last absorbing
	// what remains — the engine's bounded loop verbatim.
	aAssign := func() {
		remaining := a.Power
		for i := range block {
			if remaining <= 0 {
				break
			}
			need := rem(i)
			give := remaining
			if i < len(block)-1 && give > need {
				give = need
			}
			if give > 0 {
				taken[i] += give
				if a.hasKeyword("Deathtouch") {
					dt[i] = true
				}
			}
			remaining -= give
		}
	}
	// aHit is one blocker's hit-back: its power (a 0-power creature deals
	// nothing), plus a Deathtouch mark when it has the keyword.
	aHit := func(i int) {
		if b := block[i]; b.Power > 0 {
			aTaken += b.Power
			if b.hasKeyword("Deathtouch") {
				aDT = true
			}
		}
	}
	aLethal := func() bool { return aTaken >= aRem || aDT }

	anyFS := fsA
	for i := range block {
		anyFS = anyFS || block[i].hasKeyword("First Strike")
	}
	if anyFS {
		// First-strike step: participants with First Strike deal, then the
		// state-based actions between the two steps resolve: a dead a deals
		// nothing in the regular step, and a blocker a already killed does
		// not hit back.
		if fsA {
			aAssign()
		}
		for i := range block {
			if block[i].hasKeyword("First Strike") {
				aHit(i)
			}
		}
		if aLethal() {
			return true, dead
		}
		// Regular step: a (unless it is first-strike, which already dealt)
		// and every surviving blocker without First Strike deal, all
		// simultaneous.
		if !fsA {
			aAssign()
		}
		for i := range block {
			if block[i].hasKeyword("First Strike") {
				continue
			}
			if taken[i] >= rem(i) || dt[i] {
				continue // a's first-strike step already killed this one
			}
			aHit(i)
		}
	} else {
		aAssign()
		for i := range block {
			aHit(i)
		}
	}
	for i := range block {
		dead[i] = taken[i] >= rem(i) || dt[i]
	}
	return aLethal(), dead
}

// blocker is a defender creature carrying its object id, so the kill-block
// search and the sorting below stay deterministic (ties break on ObjID,
// never on map iteration order).
type blocker struct {
	id state.ObjID
	c  Creature
}

// killBlockCost returns the defender's cheapest legal block — smallest total
// pt of the creatures it loses — that kills a, and whether any block can at
// all. It evaluates every lone legal blocker against the full damage
// simulation (unless a has Menace), then every legal prefix of the blockers
// sorted by ascending pt (the defender leads with its cheapest creatures so
// the valuable ones absorb only the remainder), whose combined power is at
// least a's remaining toughness. A team is never cheaper than its cheapest
// killing member for pure cost counting, but a larger prefix can be: {2/2,
// 4/4} kills a 5/5 for one 2/2, cheaper than the {4/4, 4/4} team — so every
// killing prefix is simulated, not just the first.
func killBlockCost(def []blocker, a Creature) (int32, bool) {
	var can []int
	for i := range def {
		if canBlockLike(a, def[i].c) {
			can = append(can, i)
		}
	}
	if len(can) == 0 {
		return 0, false
	}
	best := int32(-1)
	consider := func(cost int32) {
		if best == -1 || cost < best {
			best = cost
		}
	}
	// Lone blockers that kill a outright (a 3/3 stops a 2/2 for free; a
	// 1/1 Deathtouch stops a 5/5 for one 1/1; a First-Strike 2/2 kills a
	// 2/2 before it deals). Menace makes every lone block declaration illegal.
	if !a.hasKeyword("Menace") {
		for _, i := range can {
			aDead, dead := blockCombat(a, []Creature{def[i].c})
			if aDead && dead[0] {
				consider(def[i].c.pt())
			}
		}
	}
	// Teams: cheapest-first prefixes whose power sums high enough. Menace
	// requires the prefix to contain at least two creatures.
	sort.SliceStable(can, func(i, j int) bool {
		pi, pj := def[can[i]].c.pt(), def[can[j]].c.pt()
		if pi != pj {
			return pi < pj
		}
		return def[can[i]].id < def[can[j]].id
	})
	// Without First Strike anywhere in the fight, growing the prefix by one
	// blocker leaves every earlier blocker's damage (and so its death)
	// unchanged -- the attacker's assignment to the first k is identical,
	// the (k+1)-th only takes what the k-th used to absorb -- so a longer
	// killing prefix never loses fewer creatures, and the first killing
	// prefix is the cheapest. Stopping there keeps the scan linear instead
	// of quadratic in the defender's board. First Strike can end the fight
	// early (a larger team kills the attacker before it deals damage), so
	// that case keeps the full scan.
	firstStrike := a.hasKeyword("First Strike")
	for _, i := range can {
		firstStrike = firstStrike || def[i].c.hasKeyword("First Strike")
	}
	var power int32
	for k := 1; k <= len(can); k++ {
		power += def[can[k-1]].c.Power
		if a.hasKeyword("Menace") && k < 2 {
			continue
		}
		if power < a.remTough() {
			continue // this team cannot kill a yet
		}
		team := make([]Creature, k)
		for i := 0; i < k; i++ {
			team[i] = def[can[i]].c
		}
		aDead, dead := blockCombat(a, team)
		if !aDead {
			continue
		}
		var cost int32
		for i := range dead {
			if dead[i] {
				cost += def[can[i]].c.pt()
			}
		}
		consider(cost)
		if !firstStrike {
			break
		}
	}
	return best, best != -1
}

// chooseAttackers is the KAttackers policy: it picks which of the offered
// attacker options to declare. The rules, each stated for what it reads:
//
//   - AR1 (nothing to gain): a creature with power <= 0 deals no damage and
//     trades nothing; it stays home.
//   - AR6 (the defender is a choice, CR 506.2): every option is one
//     (attacker, defender) pair, and a creature may attack at most one
//     opponent -- the engine rejects an intent naming it twice, so the
//     policy itself picks one defender per attacker. Each attacker is
//     evaluated against EVERY defender it was offered against, a defender
//     on which AR3 applies is vetoed, and the best remaining defender's
//     option is chosen. "Best" is a tiered ranking of how hard that
//     defender has to answer the swing: a defender whose commander clock
//     the swing closes ranks highest (AR5 -- a second-track win), then a
//     defender whose life total the swing can end (AR7), then a
//     defender that cannot block it at all (AR2 -- guaranteed damage), then
//     one whose every block fails to kill it, then one it can only trade
//     even-or-worse with; ties favour the lowest defender life, then the
//     first offered option. With a single opponent the pair list is one
//     option per creature and this reduces to the M1 choice verbatim.
//   - AR2 (unblockable): if no creature OF THAT DEFENDER can block it
//     (Flying against a defender with no Flying/Reach; no untapped
//     defenders), that defender gets the swing -- the damage is guaranteed
//     and no block can punish it.
//   - AR3 (deadly block): if THAT defender has a block that kills it while
//     costing the defender less than the creature's own pt (its power plus
//     toughness) -- a free kill, a chump-plus-finisher team, a First-Strike
//     or Deathtouch ambush -- the creature does not attack THAT defender; it
//     only swings there when every way the defender can kill it is a trade
//     the attacker wins or survives.
//   - AR5 (the clock wins, CR 903.10): an exception to AR3 -- a commander
//     whose unblocked swing would take THAT defender to 21 or more
//     commander damage from it (closesClock) attacks even when the defender
//     could kill it for less than it is worth. The swing ends the game on
//     the second track if it gets through, so it is worth presenting every
//     turn: the defender must answer it or lose, and every answer (even a
//     cheap block) spends resources the defender must keep spending. When
//     the swing does NOT close the clock yet (the tally is, say, 16 and the
//     power 4 -- 20 of 21), AR3's veto stands: the commander dies for a
//     block that only delays the closing, and the tax makes that recast a
//     losing exchange. closesClock is read against the defender each option
//     names, so a commander pounding one opponent's clock attacks that
//     opponent -- the clock is per (commander, damaged player) -- and is
//     evaluated normally against every other.
//   - AR7 (opt-in lethal pressure): a creature whose unblocked power reaches the
//     defender's known life total attacks even when a cheaper blocker can
//     kill it. The defender must spend a blocker or lose immediately. A
//     missing life fact is unknown and never treated as zero/lethal.
//   - AR8 (opt-in combined-attacker lethal pressure, AR7 implied): with
//     combinedLethal set, each defender's offered attackers are searched for
//     an attacking SUBSET whose damage, after the defender's minimum legal
//     blocking response, still reaches the defender's known life -- even
//     when no single attacker does -- and that subset is forced to attack.
//     The search is deterministic (attackers sorted by power desc then ObjID
//     asc, subsets in increasing bitmask order) and capped at
//     ar8MaxAttackers, beyond which AR7's per-attacker test substitutes. A
//     missing life fact means no AR8 effect at all; the option is OFF in the
//     default Decide.
//   - AR4 (leave a blocker): if attacking with the chosen set would leave
//     the board with no untapped creature that can block (an attacker with
//     Vigilance never taps and still blocks) while ANY opponent has a
//     creature of its own, the best blockable attacker is held back, so an
//     attack never leaves the board undefended. A commander whose swing
//     closes its target's clock (AR5), or reaches that target's life total
//     (AR7), is never the one held back -- it is the game-ending pressure;
//     the hold-back picks from the other chosen attackers instead.
//
// The decision is purely a function of the offered options and the board
// facts both adapters supply; no rng is consumed, and no map iteration
// order reaches the answer (ties break on ObjID or option index after
// game facts; the per-defender and per-opponent maps below are membership sets
// and order-independent aggregates, never ranged into a choice).
func (b Board) chooseAttackers(d *decision.Decision) []int {
	return b.chooseAttackersMode(d, false, false)
}

// ar8MaxAttackers is the subset search's ceiling: beyond this many offered
// attackers against one defender the 2^n enumeration is abandoned and AR8
// degrades to AR7's per-attacker test (2^12 = 4096 subsets, the brief's
// cap). Deterministic, so the fallback is a function of the offered count
// alone.
const ar8MaxAttackers = 12

// ar8Attacker is one offered (attacker, defender) pair during the AR8 subset
// search: the attacker's object id and facts plus the option index that names
// this defender.
type ar8Attacker struct {
	id state.ObjID
	a  Creature
	oi int
}

// ar8BlockAbsorbs reports whether a block by bl against at is one the
// defender would keep: it must be legal (untapped and Flying/Reach-correct
// via canBlockLike) and not a PURE CHUMP -- the blocker survives the strike
// (its remaining toughness exceeds the attacker's power) or kills the
// attacker outright (its power reaches the attacker's remaining toughness).
// This is the policy's reading of the brief's "minimum legal blocking
// response": the defender makes the blocks it wants (a trade or a
// survive) and does not throw a creature away to a strictly bigger
// attacker with nothing back. A pure chump therefore does NOT reduce the
// damage that gets through, which is the aggressive direction the combined
// lethal-pressure experiment deliberately takes (AR7 makes the same
// attack-through-a-bad-block bet one attacker at a time).
func ar8BlockAbsorbs(at, bl Creature) bool {
	if at.Power <= 0 || !canBlockLike(at, bl) {
		return false
	}
	return bl.remTough() > at.Power || bl.Power >= at.remTough()
}

// ar8DamageThrough is how much of the chosen attackers' power reaches the
// defender under a MINIMUM legal blocking response: every untapped defender
// creature can block at most one attacker (two against a Menace attacker,
// which no lone blocker may stop), Flying/Reach gate which attackers a
// blocker may legally block, each block absorbs one attacker whose block is
// keepable (ar8BlockAbsorbs), and a blocker is never spent on a chump. The
// response is greedy and DETERMINISTIC: it repeatedly absorbs the
// largest-power still-reachable attacker (ties by ObjID asc), spending the
// first free blockers that can legally block it, until no further keepable
// block is possible. No map or ambient order reaches the result.
func ar8DamageThrough(atks []ar8Attacker, blockers []blocker) int32 {
	free := make([]blocker, 0, len(blockers))
	for _, bl := range blockers {
		if !bl.c.Tapped {
			free = append(free, bl)
		}
	}
	// Deterministic blocker order: ObjID asc.
	sort.SliceStable(free, func(i, j int) bool { return free[i].id < free[j].id })
	absorbed := make([]bool, len(atks))
	used := make([]bool, len(free))

	for {
		bestAtk := -1
		var bestPower int32
		var bestNeed int
		for i := range atks {
			if absorbed[i] || atks[i].a.Power <= 0 {
				continue
			}
			need := 1
			if atks[i].a.hasKeyword("Menace") {
				need = 2
			}
			avail := 0
			for j := range free {
				if !used[j] && ar8BlockAbsorbs(atks[i].a, free[j].c) {
					avail++
				}
			}
			if avail < need {
				continue
			}
			if bestAtk == -1 || atks[i].a.Power > bestPower ||
				(atks[i].a.Power == bestPower && atks[i].id < atks[bestAtk].id) {
				bestAtk, bestPower, bestNeed = i, atks[i].a.Power, need
			}
		}
		if bestAtk == -1 {
			break
		}
		spent := 0
		for j := range free {
			if spent == bestNeed {
				break
			}
			if !used[j] && ar8BlockAbsorbs(atks[bestAtk].a, free[j].c) {
				used[j] = true
				spent++
			}
		}
		absorbed[bestAtk] = true
	}

	var through int32
	for i := range atks {
		if !absorbed[i] {
			through += atks[i].a.Power
		}
	}
	return through
}

// ar8LethalSubset searches the attacking subsets of atks for one whose
// ar8DamageThrough reaches the defender's known life, returning that subset
// (and true) or none. Deterministic: atks must already be sorted by power
// desc then ObjID asc (the caller does), and masks are enumerated in
// increasing numeric order, so the first lethal mask wins and ties in the
// blocking response break on ObjID. A defender with no known life never
// yields a subset (the missing fact is unknown, never zero). Beyond
// ar8MaxAttackers the enumeration is skipped and the AR7 per-attacker test is
// used instead: every attacker whose own unblocked power reaches life.
func (b Board) ar8LethalSubset(defender state.PlayerID, atks []ar8Attacker, blockers []blocker) ([]ar8Attacker, bool) {
	life, ok := b.Life.Lookup(defender)
	if !ok || life <= 0 {
		return nil, false
	}
	if len(atks) > ar8MaxAttackers {
		var force []ar8Attacker
		for _, at := range atks {
			if at.a.Power > 0 && at.a.Power >= life {
				force = append(force, at)
			}
		}
		if len(force) > 0 {
			return force, true
		}
		return nil, false
	}
	chosen := make([]ar8Attacker, 0, len(atks))
	for mask := 1; mask < (1 << len(atks)); mask++ {
		chosen = chosen[:0]
		for i := range atks {
			if mask&(1<<i) != 0 {
				chosen = append(chosen, atks[i])
			}
		}
		if ar8DamageThrough(chosen, blockers) >= life {
			out := make([]ar8Attacker, len(chosen))
			copy(out, chosen)
			return out, true
		}
	}
	return nil, false
}

// swarmLethalSubset is AR9: the smallest attacking set against defender
// whose damage reaches the defender's known life under the defender's BEST
// possible blocking response. The response is modelled pessimistically for
// the attacker: every untapped defender creature blocks (ignoring Flying,
// Menace, protection and every other restriction that could only let more
// through) and each block stops its attacker's damage entirely (no
// trample), so the defender stops at most its k untapped creatures' worth of
// attackers -- and it stops the k biggest. With atks sorted by power desc
// (the caller sorts), the damage that gets through a prefix of m attackers
// is therefore the sum of powers k..m-1, and the smallest lethal prefix is
// the first whose tail sum reaches life. Attackers already committed
// elsewhere (taken) and powerless ones are skipped. A defender with no known
// life never yields a set (the missing fact is unknown, never zero). The
// result is a pure function of the sorted list and the board: no map order
// reaches it.
func (b Board) swarmLethalSubset(defender state.PlayerID, atks []ar8Attacker, blockers []blocker, taken map[state.ObjID]int) []ar8Attacker {
	life, ok := b.Life.Lookup(defender)
	if !ok || life <= 0 {
		return nil
	}
	k := 0
	for _, bl := range blockers {
		if !bl.c.Tapped {
			k++
		}
	}
	var out []ar8Attacker
	var through int64
	for _, at := range atks {
		if at.a.Power <= 0 {
			break // sorted by power desc: nothing after this deals damage
		}
		if _, done := taken[at.id]; done {
			continue
		}
		out = append(out, at)
		if len(out) > k {
			through += int64(at.a.Power)
			if through >= int64(life) {
				return out
			}
		}
	}
	return nil
}

// profileKey is the memo key for a creature's combat facts: two creatures
// with the same key are indistinguishable to every block-risk read
// (canBlockLike, blockCombat, killBlockCost).
func (c Creature) profileKey() string {
	var sb strings.Builder
	sb.Grow(32)
	sb.WriteString(strconv.Itoa(int(c.Power)))
	sb.WriteByte('/')
	sb.WriteString(strconv.Itoa(int(c.Toughness)))
	sb.WriteByte('/')
	sb.WriteString(strconv.Itoa(int(c.Damage)))
	if c.Tapped {
		sb.WriteString("/T")
	}
	for _, k := range c.Keywords {
		sb.WriteByte('|')
		sb.WriteString(k)
	}
	return sb.String()
}

func (b Board) chooseAttackersMode(d *decision.Decision, lethalPressure, combinedLethal bool) []int {
	if len(d.Options) == 0 {
		return nil
	}
	me := d.Player
	lethalToLife := func(defender state.PlayerID, power int32) bool {
		life, ok := b.Life.Lookup(defender)
		return ok && power > 0 && power >= life
	}

	// The per-defender board facts: each opponent's creatures -- the block
	// risk an option against them carries -- and whether any of them has a
	// creature with power (AR4's "could attack back"). Keyed by the
	// defender each option names: every attacker may now choose its own
	// opponent (CR 506.2), so the analysis the M1 policy ran against ONE
	// defender runs here against each option's own.
	defBlockers := make(map[state.PlayerID][]blocker, len(d.Options))
	defHasThreat := make(map[state.PlayerID]bool, len(d.Options))
	for id, c := range b.Creatures.All() {
		if c.Controller == me {
			continue
		}
		defBlockers[c.Controller] = append(defBlockers[c.Controller], blocker{id, c})
		if c.Power > 0 {
			defHasThreat[c.Controller] = true
		}
	}

	// Group the offered options by attacker, preserving the engine's
	// enumeration order (first-seen position is the deterministic tiebreak,
	// exactly like chooseBlockers). One attacker contributes one option per
	// defender and may attack at most one of them (AR6).
	type atk struct {
		id   state.ObjID
		a    Creature
		pos  int
		opts []int
	}
	var attackers []*atk
	byID := make(map[state.ObjID]*atk, len(d.Options))
	// CR 508.1d requirements travel on the options (Option.Required, the
	// engine marks goaded creatures and MustAttack statics): a required
	// attacker is declared no matter what the value tiers say, because the
	// engine REJECTS a declaration that omits one it could have included --
	// an unmarked bot once declared around a goaded Knight and the whole
	// run aborted on "must attack with as many required creatures as
	// possible" (seed 1283, commander bench 2026-09-15).
	required := make(map[state.ObjID]bool, len(d.Options))
	for i := range d.Options {
		if d.Options[i].Required {
			required[d.Options[i].Obj] = true
		}
	}
	for i := range d.Options {
		o := &d.Options[i]
		at, ok := byID[o.Obj]
		if !ok {
			at = &atk{id: o.Obj, a: b.Creatures.Get(o.Obj), pos: i}
			byID[o.Obj] = at
			attackers = append(attackers, at)
		}
		at.opts = append(at.opts, i)
	}

	// score ranks one attacker's option against its own defender: higher
	// tier is a better swing, ok == false vetoes that defender outright
	// (AR3). An option naming a creature with no facts reads as a 0/0 and
	// never reaches score (AR1 above).
	//
	// The block-risk half (AR2/AR3) reads only the attacker's combat facts
	// and the defender's creatures, so it is memoised per (defender,
	// profile): a token army of identical creatures pays the block
	// simulation once, not once per token -- the per-attacker scan over
	// every defender creature made a thousands-strong board quadratic.
	type riskKey struct {
		def state.PlayerID
		pk  string
	}
	risk := make(map[riskKey]int)
	blockRisk := func(defender state.PlayerID, a Creature) int {
		k := riskKey{defender, a.profileKey()}
		if t, ok := risk[k]; ok {
			return t
		}
		t := 1 // blockable, but every block trades even-or-worse for them
		blockers := 0
		for _, db := range defBlockers[defender] {
			if canBlockLike(a, db.c) {
				blockers++
			}
		}
		blockable := blockers > 0
		if a.hasKeyword("Menace") {
			blockable = blockers >= 2
		}
		if !blockable {
			t = 2 // AR2: nothing of theirs can block it
		} else if cost, ok := killBlockCost(defBlockers[defender], a); ok && cost < a.pt() {
			t = 0 // AR3: it dies for less than it is worth
		}
		risk[k] = t
		return t
	}
	score := func(at *atk, oi int) (int, bool) {
		o := &d.Options[oi]
		a := at.a
		defender := o.Player
		if b.closesClock(defender, at.id, a) {
			return 4, true // AR5: the swing closes THIS defender's clock
		}
		if lethalPressure && lethalToLife(defender, a.Power) {
			return 3, true // experimental: win unblocked or force a blocker
		}
		if t := blockRisk(defender, a); t > 0 {
			return t, true
		}
		return 0, false // AR3 veto
	}

	// AR8 (opt-in combined-attacker lethal pressure): no single attacker may
	// be lethal, but the attacking SET is. Search each defender's offered
	// attackers for a subset whose damage survives the defender's minimum
	// legal blocking response and reaches its known life; force that subset
	// to attack that defender. ar8OptionFor maps an attacker to the option
	// index AR8 chose for it, and ar8Forced marks those options so AR4 never
	// holds one back. Empty when the option is off, no defender has known
	// life, or no subset is lethal -- in which case AR7's per-attacker logic
	// (below) still stands unchanged.
	ar8OptionFor := make(map[state.ObjID]int)
	ar8Forced := make(map[int]bool)
	// One offered pair set per defender, in a DETERMINISTIC defender order:
	// first-seen option order (never map order), so which defender wins an
	// attacker offered against several is a function of the offer list
	// alone. Attackers are sorted by power desc then ObjID asc (the brief's
	// order); options keep offered order for the ObjID tiebreak. Shared by
	// AR8 (opt-in) and AR9 (always on).
	defAtks := make(map[state.PlayerID][]ar8Attacker, len(d.Options))
	var defOrder []state.PlayerID
	seenDef := make(map[state.PlayerID]bool, len(d.Options))
	for _, at := range attackers {
		for _, oi := range at.opts {
			def := d.Options[oi].Player
			if !seenDef[def] {
				seenDef[def] = true
				defOrder = append(defOrder, def)
			}
			defAtks[def] = append(defAtks[def], ar8Attacker{id: at.id, a: at.a, oi: oi})
		}
	}
	for _, defender := range defOrder {
		atks := defAtks[defender]
		sort.SliceStable(atks, func(i, j int) bool {
			if atks[i].a.Power != atks[j].a.Power {
				return atks[i].a.Power > atks[j].a.Power
			}
			return atks[i].id < atks[j].id
		})
	}
	if combinedLethal {
		for _, defender := range defOrder {
			subset, ok := b.ar8LethalSubset(defender, defAtks[defender], defBlockers[defender])
			if !ok {
				continue
			}
			for _, at := range subset {
				// First lethal defender to name this attacker wins, so an
				// attacker offered against several defenders is deterministic.
				if _, done := ar8OptionFor[at.id]; done {
					continue
				}
				ar8OptionFor[at.id] = at.oi
				ar8Forced[at.oi] = true
			}
		}
	}
	// AR9 (swarm lethal, always on): force the smallest attacking set whose
	// damage reaches a defender's known life even when that defender blocks
	// as well as it possibly can (swarmLethalSubset). It is the case AR3's
	// per-attacker veto can never see -- a thousand 1/1 tokens facing three
	// 2/2s, each token individually "dies for free", and the bot stayed home
	// for dozens of turns while the board doubled every turn.
	//
	// A commander whose swing closes some defender's clock (AR5) keeps its
	// tier-4 choice and is left out of the swarm, so the swarm never
	// redirects the clock's game-ending swing.
	swarmSkip := make(map[state.ObjID]int, len(ar8OptionFor))
	for id, oi := range ar8OptionFor {
		swarmSkip[id] = oi
	}
	for _, at := range attackers {
		for _, oi := range at.opts {
			if b.closesClock(d.Options[oi].Player, at.id, at.a) {
				swarmSkip[at.id] = oi
				break
			}
		}
	}
	for _, defender := range defOrder {
		for _, at := range b.swarmLethalSubset(defender, defAtks[defender], defBlockers[defender], swarmSkip) {
			swarmSkip[at.id] = at.oi
			ar8OptionFor[at.id] = at.oi
			ar8Forced[at.oi] = true
		}
	}

	var chosen []int
	for _, at := range attackers {
		if at.a.Power <= 0 && !required[at.id] {
			continue // AR1 (a required 0-power creature still attacks: the requirement is not a value judgement)
		}
		// AR8 forces this attacker's lethal-subset option; it skips the value
		// tiers the same way a required attacker does.
		if oi, ok := ar8OptionFor[at.id]; ok {
			chosen = append(chosen, oi)
			continue
		}
		best := -1
		bestTier := -1
		var bestLife int32
		for _, oi := range at.opts {
			t, ok := score(at, oi)
			if !ok {
				// AR3 vetoes this defender outright. A creature it is free to
				// leave home obeys the veto; a REQUIRED attacker keeps scanning
				// the rest of its options, because a later offered defender may
				// be scoreable -- attacking the first offered option just
				// because it was offered first would throw a legal, better
				// swing away (multiplayer goad combats offer one option per
				// defending seat).
				continue
			}
			// At equal combat risk, pressure the opponent closest to dying
			// rather than the lowest seat. Life is public on both adapters.
			// Equal life keeps the first option (opts is in offered order).
			life := b.Life.Get(d.Options[oi].Player)
			if t > bestTier || (t == bestTier && life < bestLife) {
				best, bestTier, bestLife = oi, t, life
			}
		}
		if best < 0 {
			if !required[at.id] {
				continue // every defender vetoed and the creature is free to stay home
			}
			// A required attacker with every defender vetoed still swings at
			// its first offered option (deterministic), so the requirement is
			// always answered by an offered option.
			best = at.opts[0]
		}
		if best >= 0 {
			chosen = append(chosen, best)
		}
	}

	// AR4: hold back a best blocker if attacking leaves nobody to block
	// with. Zero remaining after the attack (attackers tap without
	// Vigilance) plus an opponent that could attack back is the trigger;
	// only blockable attackers are candidates, since an unblockable one is
	// the way a defensive board still wins. Task m34 generalizes the M1
	// check from the single defender to ANY opponent: "leave the board
	// undefended" is a fact about the whole table, and any opponent with a
	// threat creature can punish it.
	canStillBlock := 0
	attackingSet := make(map[state.ObjID]bool, len(chosen))
	for _, oi := range chosen {
		attackingSet[d.Options[oi].Obj] = true
	}
	for id, c := range b.Creatures.All() {
		if c.Controller != me || c.Tapped {
			continue
		}
		if !attackingSet[id] || c.hasKeyword("Vigilance") {
			canStillBlock++
		}
	}
	anyOppThreat := false
	for _, has := range defHasThreat {
		if has {
			anyOppThreat = true
			break
		}
	}
	if canStillBlock == 0 && anyOppThreat && len(chosen) > 0 {
		hold := -1 // position in chosen, NOT an option index -- the slice
		// below cuts at it (a latent M1 bug wrote the option index here,
		// which only happened to equal the position while every attacker
		// had exactly one option and none were skipped; the Task-m34
		// cross-product option list made the mismatch reachable).
		var holdScore int32 = -1
		var holdID state.ObjID
		// Memoised per attacker profile: the any-defender blockability scan
		// reads only the attacker's facts (an order-independent any-match).
		anyBlockable := make(map[string]bool)
		for j, oi := range chosen {
			a := b.Creatures.Get(d.Options[oi].Obj)
			// Blockable by any opponent's creature: the held-back creature
			// defends the board against every future attacker (an
			// order-independent any-match over the defenders).
			pk := a.profileKey()
			blockable, seen := anyBlockable[pk]
			if !seen {
				for _, dbs := range defBlockers {
					blockers := 0
					for _, db := range dbs {
						if canBlockLike(a, db.c) {
							blockers++
						}
					}
					if blockers > 0 && (!a.hasKeyword("Menace") || blockers >= 2) {
						blockable = true
						break
					}
				}
				anyBlockable[pk] = blockable
			}
			if !blockable {
				continue
			}
			// A required attacker is never the one held back: cutting it
			// makes the whole declaration illegal (CR 508.1d).
			if required[d.Options[oi].Obj] {
				continue
			}
			// AR5: a commander whose swing closes its target's clock is
			// never held back -- the attack's game-ending piece.
			if b.closesClock(d.Options[oi].Player, d.Options[oi].Obj, a) {
				continue
			}
			if lethalPressure && lethalToLife(d.Options[oi].Player, a.Power) {
				continue
			}
			// AR8: part of a lethal attacking set -- held back would undo the
			// combined pressure AR8 exists for.
			if ar8Forced[oi] {
				continue
			}
			if a.pt() > holdScore || (a.pt() == holdScore && d.Options[oi].Obj < holdID) {
				hold, holdScore, holdID = j, a.pt(), d.Options[oi].Obj
			}
		}
		if hold >= 0 {
			chosen = append(chosen[:hold:hold], chosen[hold+1:]...)
		}
	}
	// A MaxAttackers$ ceiling (CR 508.1j) bounds the whole declaration and
	// the engine exposes it as the decision's Max. chosen is in option
	// first-seen order (the attackers' iteration order), NOT required-first,
	// so when the ceiling forces a choice between requirements the slice is
	// stably reordered to put the required attackers first before
	// truncating -- a plain cut could drop a required attacker whose option
	// was seen later and keep a non-required one, and validateAttackDeclaration
	// would then reject the whole declaration (the seed-1283 run-abort class).
	if d.Max < len(chosen) && d.Max >= 0 {
		ordered := make([]int, 0, len(chosen))
		for _, oi := range chosen {
			if required[d.Options[oi].Obj] || ar8Forced[oi] {
				ordered = append(ordered, oi)
			}
		}
		for _, oi := range chosen {
			if !required[d.Options[oi].Obj] && !ar8Forced[oi] {
				ordered = append(ordered, oi)
			}
		}
		chosen = ordered[:d.Max]
	}
	return chosen
}

// chooseBlockers is the KBlockers policy: it picks which of the offered
// (blocker, attacker) pairs to declare. The rules:
//
//   - BR1 (favourable and even trades): a blocker blocks when it kills the
//     attacker and either survives the return swing or trades even-or-up
//     (the blocker's pt is no more than the attacker's). Among the viable
//     blockers for one attacker the cheapest is spent, preserving the big
//     creatures for the rest of the combat.
//   - BR2 (chump only for lethal): a blocker that dies without killing
//     blocks only when the still-unblocked damage this combat would
//     otherwise be lethal (life - damage <= 0). A chump against a trample
//     attacker saves only the blocker's own toughness, which is exactly
//     what the bookkeeping subtracts.
//   - BR3 (the clock is a second lethal line, CR 903.10): BR2's "would
//     otherwise kill" also fires when the attacker is a commander whose
//     unblocked swing would take THIS defender to 21 or more commander
//     damage from it (closesClock) — a loss the commander's own cumulative
//     track records regardless of life. A healthy life total is no excuse
//     to let a 19-tally 2/2 commander through; blocking it (even as a
//     chump) keeps the clock from turning.
//   - BR4 (the clock ranks first): attackers are processed biggest-threat-
//     first, and a commander whose unblocked swing would close its clock
//     against this defender counts as power 21 for that ordering — a
//     must-answer threat outranks every plain attacker, whatever its power,
//     because an unanswered one ends the game on the second track. Among
//     non-closing attackers the ordering stays power-descending.
//   - BR5 (Menace): any chosen block against a Menace attacker includes a
//     second unused blocker. If fewer than two are available, the bot leaves
//     it unblocked rather than submitting an illegal declaration forever.
//   - Nothing else blocks: a creature that dies holding the line while the
//     attacker survives is thrown away for nothing.
//
// Attackers are processed biggest-power-first, so the biggest threat takes
// the first pick of blockers and the first chump when one is needed. One
// blocker is never given to two attackers (used is a membership set, as
// before). The choice is deterministic: no rng, ties on option index.
func (b Board) chooseBlockers(d *decision.Decision) []int {
	if len(d.Options) == 0 {
		return nil
	}
	me := d.Player
	myLife := b.Life.Get(me)

	// Group the offered options by attacker, preserving the engine's
	// enumeration order (first-seen position is the deterministic tiebreak).
	type atk struct {
		id   state.ObjID
		a    Creature
		pos  int // first option index for this attacker
		opts []int
	}
	var attackers []*atk
	byID := make(map[state.ObjID]*atk)
	for i := range d.Options {
		o := &d.Options[i]
		at, ok := byID[o.Attacker]
		if !ok {
			at = &atk{id: o.Attacker, a: b.Creatures.Get(o.Attacker), pos: i}
			byID[o.Attacker] = at
			attackers = append(attackers, at)
		}
		at.opts = append(at.opts, i)
	}
	sort.SliceStable(attackers, func(i, j int) bool {
		// BR4: a commander whose unblocked swing would close THIS defender's
		// clock ranks as power 21 -- a must-answer threat above every plain
		// attacker, whatever its power. Non-closing attackers keep the
		// plain power ordering.
		clock := func(at *atk) int32 {
			if b.closesClock(me, at.id, at.a) {
				return 21
			}
			return 0
		}
		pi, pj := attackers[i].a.Power+clock(attackers[i]), attackers[j].a.Power+clock(attackers[j])
		if pi != pj {
			return pi > pj // biggest threat first
		}
		return attackers[i].pos < attackers[j].pos
	})

	unblocked := int32(0) // damage that will reach me if nothing more blocks
	for _, at := range attackers {
		unblocked += at.a.Power
	}

	used := map[state.ObjID]bool{}
	// support returns the cheapest second blocker for a Menace block. It is
	// called before first is marked used, so it excludes that object itself.
	support := func(at *atk, first int) int {
		best := -1
		for _, oi := range at.opts {
			obj := d.Options[oi].Obj
			if obj == d.Options[first].Obj || used[obj] {
				continue
			}
			if best == -1 || b.Creatures.Get(obj).pt() < b.Creatures.Get(d.Options[best].Obj).pt() ||
				(b.Creatures.Get(obj).pt() == b.Creatures.Get(d.Options[best].Obj).pt() && oi < best) {
				best = oi
			}
		}
		return best
	}

	var ch []int
	for _, at := range attackers {
		// An attacker with nothing to block (no power to stop, no lethal
		// damage to save — including a factless option that reads as a 0/0)
		// is passed over entirely; BR1 and BR2 both exist to change damage
		// going to a player, and this one deals none.
		if at.a.Power <= 0 || at.a.remTough() <= 0 {
			continue
		}
		// BR1: cheapest unused blocker that kills the attacker on a
		// trade that is at least even (the blocker survives, or the two
		// trade with the attacker worth no less).
		best := -1
		var bestScore int32 = -1
		for _, oi := range at.opts {
			ob := &d.Options[oi]
			if used[ob.Obj] {
				continue
			}
			bl := b.Creatures.Get(ob.Obj)
			aDead, dead := blockCombat(at.a, []Creature{bl})
			if !aDead {
				continue // this block does not kill the attacker
			}
			if dead[0] && at.a.pt() < bl.pt() {
				continue // it dies trading down; not even
			}
			if best == -1 || bl.pt() < bestScore || (bl.pt() == bestScore && oi < best) {
				best, bestScore = oi, bl.pt()
			}
		}
		if best >= 0 {
			second := -1
			if at.a.hasKeyword("Menace") {
				second = support(at, best)
				if second < 0 {
					best = -1 // a lone block would be an illegal declaration
				}
			}
			if best >= 0 {
				ch = append(ch, best)
				used[d.Options[best].Obj] = true
				if second >= 0 {
					ch = append(ch, second)
					used[d.Options[second].Obj] = true
				}
				unblocked -= at.a.Power // a dead attacker deals nothing, trample or not
				continue
			}
		}
		// BR2: chump only when the unblocked damage would otherwise kill —
		// where "kill" includes the commander clock (BR3): an unblocked
		// swing from a commander that would take this defender to 21+ from
		// it is lethal on the second track no matter the life total.
		if myLife-unblocked <= 0 || b.closesClock(me, at.id, at.a) {
			chump := -1
			for _, oi := range at.opts {
				if used[d.Options[oi].Obj] {
					continue
				}
				if chump == -1 || b.Creatures.Get(d.Options[oi].Obj).pt() < b.Creatures.Get(d.Options[chump].Obj).pt() {
					chump = oi
				}
			}
			if chump >= 0 {
				second := -1
				if at.a.hasKeyword("Menace") {
					second = support(at, chump)
					if second < 0 {
						chump = -1 // leave it unblocked rather than submit one blocker
					}
				}
				if chump >= 0 {
					ch = append(ch, chump)
					used[d.Options[chump].Obj] = true
					if second >= 0 {
						ch = append(ch, second)
						used[d.Options[second].Obj] = true
					}
					saved := at.a.Power
					if at.a.hasKeyword("Trample") {
						saved = b.Creatures.Get(d.Options[chump].Obj).remTough()
						if second >= 0 {
							saved += b.Creatures.Get(d.Options[second].Obj).remTough()
						}
						if saved > at.a.Power {
							saved = at.a.Power
						}
					}
					unblocked -= saved
				}
			}
		}
	}
	return ch
}
