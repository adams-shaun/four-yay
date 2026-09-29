package botpolicy

import (
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// threat is the policy's ranking of an opposing creature as the thing a
// targeting decision should hit. It deliberately answers "which of the
// opponent's creatures is most worth removing" rather than pitting a
// creature against the opponent's face -- that comparison is the calling
// code's business (faceScore below) -- and it reads only facts already on
// botpolicy.Creature, so neither adapter adds a Board field for it.
//
// The metric prices three things beyond power:
//
//   - persistence: remTough (Toughness minus accumulated damage) is counted,
//     so a healthy creature ranks above an identical one that is one hit
//     away from dying on its own -- removing the creature that will stick
//     around is worth more than one that dies anyway.
//   - availability: an untapped creature can block our incoming attack, so
//     it ranks above a tapped one that has already swung this turn and
//     cannot (CR 509.1).
//   - evasion: a keyword that makes the creature harder to answer with
//     ordinary damage or blocks -- Flying (hard to block, dodges ground
//     removal), Deathtouch (kills anything it trades into), First
//     Strike/Initiative (combat sentences), Vigilance (attacks and still
//     blocks), Reach (keeps the skies honest) -- each adds value a bare
//     power+toughness proxy would miss.
//
// Pure power would order a dying 4/1 above a woken 3/3; threat flips them,
// which is the whole reason the policy does not rank by Power alone.
func (c Creature) threat() int32 {
	v := c.Power*3 + c.remTough()*2
	if !c.Tapped {
		v += 6
	}
	if c.hasKeyword("Flying") {
		v += 16
	}
	if c.hasKeyword("Deathtouch") {
		v += 12
	}
	if c.hasKeyword("First Strike") || c.hasKeyword("Initiative") {
		v += 8
	}
	if c.hasKeyword("Vigilance") {
		v += 6
	}
	if c.hasKeyword("Reach") {
		v += 4
	}
	return v
}

// faceScore is what pointing a targetable-at-player effect at the
// opponent's face is worth on the PLAIN ranking path (rankOption), the one
// used when the KTarget decision carries no recognisable effect or no
// literal damage figure. It is a neutral constant, because on that path
// the policy cannot read the effect that produced the decision -- an
// absent target_effect, an unknown API, or a null/absent damage amount are
// all "the effect is unreadable", so it cannot tell a burn -- an effect
// that actually reduces a player's life -- from a draw or a removal whose
// effect never touches the face. With that fact absent, any bonus keyed on
// the opponent's life, their proximity to a lethal track's end, or their
// biggest creature would be a kill the policy cannot prove:
//
//   - a player at 2 life is no more a win than one at 18 when the effect
//     may deal no damage at all;
//   - "near" the 21 commander-damage clock is not burn reach: ordinary
//     non-combat spell damage never advances that clock (CR 903.10 counts
//     only the combat damage one commander has dealt), so it must not
//     attract face targeting;
//   - keying the score on the target's biggest creature can prefer the
//     healthier player in a multiplayer game.
//
// So on the plain path the face ranks neutrally, beneath every on-board
// creature threat, and is only chosen when no opposing creature is a legal
// target (a burn at a face, a drain, a life-gain that only targets players)
// or as a later pick in a multi-target decision. R2's board-over-face holds
// by construction: any creature ranks above it, so the face is never picked
// over a creature the policy cannot be sure the effect is better than.
// Ties among equal faces (the multiplayer case) break on option index (R7),
// the deterministic answer.
//
// When the effect IS readable (a known literal damage figure), the face is
// scored by effectRanker instead, and a face the damage reaches becomes the
// tier-2 lethal target -- the one case where the bot proves a burn. The
// unknown stays on the plain path, where the face is neutral.
func (b Board) faceScore() int32 {
	return faceNeutral
}

// faceNeutral is the standing value of the opponent's face as a target:
// neutral, and below every on-board creature threat.
const faceNeutral int32 = 0

// targetRank is one option's standing for chooseTargets: a score, the
// option's index (the deterministic tiebreak), and whether it belongs to
// the deciding seat.
type targetRank struct {
	score int32
	idx   int
	ours  bool
}

// The three tiers of the ft1 face-aiming policy, as large score offsets so
// a tier strictly outranks everything in the tier below it (a creature's
// within-tier threat() figure is bounded far under the gap):
//
//   - tierThreat (1): an opponent creature that may kill the deciding seat
//     this round, and that the damage can actually kill -- defensive removal;
//   - tierFaceLethal (2): an opponent's face, when the damage is potentially
//     lethal to them -- the win line;
//   - tierValue (3): the most powerful opponent creature the damage can kill,
//     but only when the seat has spare mana.
//
// Anything below those ranks on the ordinary board-over-face scale.
const (
	tierThreat     int32 = 1_000_000
	tierFaceLethal int32 = 800_000
	tierValue      int32 = 600_000
)

// effectDamage returns the known literal damage a KTarget decision's effect
// deals, and whether it is a recognisable direct-damage primitive with a
// finite, nonnegative amount. An absent TargetEffect, an unknown API, or a
// null/absent Amount all report (0, false): the effect is unreadable and,
// per the ft1 contract, an unknown must never be treated as lethal. This is
// the one place an unknown is resolved to "no".
func (b Board) effectDamage(d *decision.Decision) (int32, bool) {
	te := d.TargetEffect
	if te == nil || (te.API != "DealDamage" && te.API != "DamageAll") || te.Damage == nil || te.Damage.Amount == nil || *te.Damage.Amount < 0 {
		return 0, false
	}
	return int32(*te.Damage.Amount), true
}

// mayKillMe reports whether an opposing creature c is a threat that may
// kill the deciding seat this round: it can attack (untapped, so combat
// damage could reach the seat this turn) and one swing would deal lethal
// combat damage to the seat's current life total (power no less than the
// seat's life). It reads only facts the bot holds, so a life total that is
// absent answers false -- "not a provable threat", never "assume lethal".
func (b Board) mayKillMe(me state.PlayerID, c Creature) bool {
	if c.Power <= 0 || c.Tapped {
		return false
	}
	life, ok := b.Life[me]
	if !ok {
		return false
	}
	return c.Power >= life
}

// hasSpareMana is conservative: a value burn is only promoted when spending
// ANY single available pool unit or source still leaves enough mana to pay
// each cheapest instant-speed spell. A total-mana surplus alone cannot
// protect a coloured instant (one Island and one Forest cannot guarantee
// that a one-mana removal leaves the Island untouched). Only untapped basic
// lands with one fixed, literal production colour are counted: the Board's
// production summary cannot promise that a nonbasic/ability's activation
// conditions or its alternative colours will be available at this window.
// A just-played basic land still counts: summoning sickness (CR 302.6)
// forbids only a CREATURE's tap ability -- rules/legal.go refuses the tap
// cost on SummonSick only when the derived types contain Creature and no
// Haste -- and no basic land is a creature, so its {T}: add ability is live
// the moment it enters.
func (b Board) hasSpareMana() bool { return b.hasSpareManaAfter("") }

// hasSpareManaAfter is hasSpareMana generalized to a KNOWN pending payment:
// pendingCost is the printed mana cost of the spell whose target is being
// chosen. Under CR 601.2b/c that spell is already on the stack but not yet
// paid -- a cast's target ask (rules/cast.go) is posed while
// pc.stackObj == pc.card, and payCast spends the mana afterwards -- so the
// payment that follows the answer can destroy the very reserve hasSpareMana
// checked. A multi-mana removal spends as many units as its cost, but
// hasSpareMana only tested spending any ONE unit, so a {1}{G} removal read
// as spare against a {G}{G} reserve it then ate. This deducts the pending
// cost's WORST-CASE consumption from the pool, then runs hasSpareMana's
// per-unit/per-source probes on the REMAINDER -- so the claim is "the
// reserve survives BOTH the spell's own payment AND one further unit or
// source", which is the invariant the policy means. If the modelled pool
// cannot legally pay the pending spell at all, the call fails closed: the
// units covering the gap are invisible, so no remainder can be proven.
//
// The deduction is deliberately conservative (an under-promotion is fine, a
// broken reserve is not): coloured pips are taken first and must be payable,
// then the generic units from the colours the reserve's own pips need most
// (ascending slot index on a tie), the allocation that hurts the reserve
// most. An empty pendingCost reproduces hasSpareMana exactly, per-unit and
// per-source probes included; a genuinely free cost ("0", no pips) deducts
// nothing and so takes the same probe path.
func (b Board) hasSpareManaAfter(pendingCost string) bool {
	available := b.Pool
	var sources []state.Mana
	for _, c := range b.Cards {
		if !c.OnBattlefield || !c.Basic || c.Tapped || c.Produces.Any || c.Produces.Indeterminate {
			continue
		}
		var source state.Mana
		colours := 0
		for i, n := range c.Produces.Colour {
			if n > 0 {
				colours++
				source[i] = n
			}
		}
		if colours != 1 {
			continue // alternative activations are not simultaneous mana
		}
		for i, n := range source {
			available[i] += n
		}
		sources = append(sources, source)
	}
	if available.Total() == 0 {
		return false
	}
	// No instant-speed reserve: any dependable mana is spare.
	minCost := int32(-1)
	var reserves []Card
	for id, c := range b.Cards {
		if !c.Castable || !c.InstantSpeed || c.CMC <= 0 {
			continue
		}
		cost := b.castCost(id, c)
		if minCost < 0 || cost < minCost {
			minCost, reserves = cost, nil
		}
		if cost == minCost {
			reserves = append(reserves, c)
		}
	}
	if minCost < 0 {
		return true
	}
	pays := func(m state.Mana) bool {
		if m.Total() < minCost {
			return false
		}
		for _, c := range reserves {
			for i, need := range colourPips(c.ManaCost) {
				if m[i] < need {
					return false
				}
			}
		}
		return true
	}
	if pendingCost != "" {
		// The known pending payment is a REAL future spend, not a replacement
		// for the single-unit probe below: after the spell is paid, a further
		// unit or source may still be consumed by something else (the very
		// compose a {G} pending payment produces -- it eats one Forest, then a
		// later instant eats another). So deduct the worst-case payment from
		// the pool FIRST, then run the unchanged per-unit/per-source probes
		// against the remainder. A genuinely free cost deducts nothing and the
		// probes run exactly as hasSpareMana's do.
		rem, ok := b.pendingSpendWorstCase(available, pendingCost, reserves)
		if !ok {
			// The pool cannot pay the pending spell's coloured pips (or its
			// whole cost) from modeled sources. The units that would cover the
			// gap are invisible here, so nothing can be proven about the
			// remainder: fail closed rather than under-count the spend.
			return false
		}
		available = rem
	}
	if !pays(available) {
		return false
	}
	// Spending from a pool slot or tapping one source may consume the
	// reserve's only matching colour (or the whole multi-mana activation).
	// After a pending deduction a slot or source may already be gone; a probe
	// that cannot cover the spend has nothing left to take and is skipped,
	// which is exactly "that unit is no longer available".
	for i, n := range b.Pool {
		if n > 0 && available[i] > 0 {
			left := available
			left[i]--
			if !pays(left) {
				return false
			}
		}
	}
	for _, source := range sources {
		left := available
		cover := true
		for i, n := range source {
			if available[i] < n {
				cover = false
				break
			}
			left[i] -= n
		}
		if !cover {
			continue
		}
		if !pays(left) {
			return false
		}
	}
	return true
}

// pendingSpendWorstCase returns available minus a payment of pendingCost's
// units, allocated the way that hurts the reserves most, and whether that
// payment is one this pool can legally make at all. It is the conservative
// direction by construction: coloured pips are unavoidable and taken first --
// every pip a colour CANNOT cover fails the whole call, because the units
// that would cover it are invisible here and treating them as generic would
// invent a payment (a {G}{G} cost against one Forest and one Island does not
// become payable by spending the Island), then the generic remainder is
// drawn from the colours the reserves need most, ascending slot index on a
// tie -- so the units the reserve can least spare go first. A printed {X} is
// an unbounded spend on the stack (CmcOf counts it as 0) and no value is
// readable here, so it fails closed. The caller has already established that
// a reserve exists; ok is false for every case the remainder cannot be
// proven against.
func (b Board) pendingSpendWorstCase(available state.Mana, pendingCost string, reserves []Card) (state.Mana, bool) {
	if costHasX(pendingCost) {
		return state.Mana{}, false
	}
	rem := available
	pips := colourPips(pendingCost)
	for i := 0; i < 5; i++ {
		if rem[i] < pips[i] {
			// The coloured requirement is not payable from modeled sources.
			// Deducting it "up to availability" and pricing the rest as
			// generic would spend a different colour for an unpayable pip.
			return state.Mana{}, false
		}
		rem[i] -= pips[i]
	}
	generic := CmcOf(pendingCost)
	for i := 0; i < 5; i++ {
		generic -= pips[i]
	}
	if generic < 0 {
		generic = 0
	}
	if rem.Total() < generic {
		// Even after the pips, the pool cannot cover the generic remainder:
		// the spell is paid from units this Board cannot see, so no remainder
		// can be proven.
		return state.Mana{}, false
	}
	// need[i] is the reserve's binding demand in colour i: the max over every
	// reserve at the cheapest cost, exactly what pays requires of the pool.
	var need [5]int32
	for _, c := range reserves {
		for i, n := range colourPips(c.ManaCost) {
			if n > need[i] {
				need[i] = n
			}
		}
	}
	for ; generic > 0; generic-- {
		pick := -1
		for i := 0; i < 5; i++ {
			if rem[i] == 0 {
				continue
			}
			// Drain the colour with the least remaining slack above its
			// reserve demand. Equal slack is resolved by mana-slot order.
			slack := rem[i] - need[i]
			if pick < 0 || slack < rem[pick]-need[pick] {
				pick = i
			}
		}
		if pick < 0 {
			return state.Mana{}, false // unreachable given the Total check above
		}
		rem[pick]--
	}
	return rem, true
}

// costHasX reports whether a printed Forge cost carries an {X} symbol. CmcOf
// counts {X} as 0 (a printed X is 0 off the stack before a value is chosen),
// but the payment it stands for is unbounded, so a pending cost containing X
// cannot be priced conservatively from the cost string alone.
func costHasX(mc string) bool {
	for sym := range strings.FieldsSeq(braceForm.Replace(mc)) {
		if sym == "X" {
			return true
		}
	}
	return false
}

// pendingSpellCost returns the printed mana cost of the spell object src when
// it is a spell on b.Stack, or "" when the decision names no spell. At
// cast-target time the casting card is already on the stack (pc.stackObj ==
// pc.card, rules/cast.go) and Decision.Source names it, so its unpaid cost is
// readable here. An activated ability's ask leaves Source 0 -- its ability
// object is not minted until payCast -- so its pending cost stays unreadable
// and hasSpareManaAfter falls back to the no-pending probe; that is a named
// residual, never a guess.
func (b Board) pendingSpellCost(src state.ObjID) string {
	if src == 0 {
		return ""
	}
	for _, s := range b.Stack {
		if s.ID == src && s.IsSpell {
			return s.ManaCost
		}
	}
	return ""
}

// beneficialGrantModes names the Effect-granted static Mode$ values that are
// a one-way BOON to whatever the effect targets, so a target ask carrying one
// must be aimed at the deciding seat's OWN board -- never at an opponent's
// permanent while an own option exists. This is the target's POLARITY, which
// the plain ranker cannot see: an unreadable Effect is treated as hostile
// value (R1/R3 above), which is right for a removal and exactly backwards for
// a boon. The set is deliberately small and explicit; a mode is listed only
// when the card text makes the target strictly better off:
//
//   - CantBlockBy: "target creature can't be blocked this turn" (Whirler
//     Rogue, Rogue's Passage). 105 corpus activated Effect grants.
//   - CanAttackDefender: "target creature ... can attack this turn as though
//     it didn't have defender" (Assault Formation). 10 corpus files.
//
// Every other measured mode is either harmful (CantRegenerate, MustBlock) or
// shape-ambiguous (Continuous, MustAttack, ReduceCost) -- the same Mode$ can
// be a pump or a shrink, so it is left on the plain path rather than guessed.
var beneficialGrantModes = map[string]bool{
	"CantBlockBy":       true,
	"CanAttackDefender": true,
}

// boonStaticsOnly is the ONE polarity classifier for a list of granted static
// modes: it reports whether the list is non-empty and every mode in it is a
// readable one-way boon (beneficialGrantModes). Both consumers of the static
// payload go through it -- the KTarget branch reads it over the decision's
// TargetEffect.Statics (beneficialGrant), and the ability scorer reads it
// over the offer-time twin on the ability option (decision.Option's
// GrantStatics, A1c in ability.go) -- so the activation-side decline and the
// target-side aim can never disagree about what counts as a boon.
func boonStaticsOnly(statics []string) bool {
	if len(statics) == 0 {
		return false
	}
	for _, mode := range statics {
		if !beneficialGrantModes[mode] {
			return false
		}
	}
	return true
}

// beneficialGrant reports whether a KTarget decision's effect is a readable
// Effect that grants only boon statics to its target. An absent/empty Statics
// (the unknown-is-no rule), or ANY mode outside the set, reports false and
// stays on today's ranking -- an unknown is never treated as friendly, just
// as it is never treated as lethal elsewhere in this file.
func beneficialGrant(te *decision.TargetEffect) bool {
	return te != nil && boonStaticsOnly(te.Statics)
}

func (b Board) removalRanker(me state.PlayerID, effect *decision.TargetEffect) func(decision.Option) targetRank {
	return func(o decision.Option) targetRank {
		r := b.rankOption(o, me)
		if r.ours {
			return r
		}
		if effect.API == "Counter" {
			for _, s := range b.Stack {
				if s.ID == o.Obj && s.IsSpell && s.Controller != me {
					return targetRank{score: tierValue + s.CMC, idx: o.Index}
				}
			}
			return r
		}
		if o.Kind == "player" {
			return r
		}
		value := r.score
		if c, ok := b.Cards[o.Obj]; ok && value == 0 {
			value = c.CMC * 3
		}
		if value == 0 {
			return r
		}
		bonus := int32(0)
		switch effect.Removal.Kind {
		case "exile", "command":
			bonus = 30
		case "library":
			bonus = 20
		case "graveyard", "destroy", "sacrifice":
			bonus = 10
		case "bounce":
			bonus = 5
		}
		r.score = tierValue + value + bonus
		return r
	}
}

// effectRanker returns the KTarget ranking for a decision whose effect is a
// recognised direct-damage primitive with the known literal amount dmg. It
// orders options by the three ft1 tiers, most important first:
//
//   - tier 1: an opponent's creature that may kill the deciding seat this
//     round AND that dmg can actually kill (remaining toughness no more
//     than dmg) -- the defensive removal;
//   - tier 2: an opponent's face, when dmg is potentially lethal to them
//     (their life no more than dmg) -- the win line, the new capability;
//   - tier 3: the most powerful opponent creature dmg can kill -- but only
//     when the seat has spare mana; without it the removal is not spent on
//     value and these creatures fall back to the board-first ranking below.
//
// Everything that is not clearly one of those -- a face the known damage
// does not reach, a creature the damage cannot kill, a value kill with no
// spare mana -- keeps the ordinary board-over-face ranking (rankOption's
// threat()/neutral scale), so this is strictly more aggressive than the
// old neutral face but never less sound: an option the bot cannot prove
// lethal or a threat is never promoted, and an unknown never becomes
// lethal (the unknown stays on the plain path, whose face is neutral).
func (b Board) effectRanker(me state.PlayerID, dmg int32) func(decision.Option) targetRank {
	return b.effectRankerAfter(me, dmg, "")
}

// effectRankerAfter is effectRanker with the pending payment of the spell
// whose target is being chosen (empty when the ask names no readable spell: a
// sourceless activated ability). The reserve gate it builds is
// hasSpareManaAfter, so a value kill is promoted only when the seat still
// keeps its cheapest instant payable AFTER worst-casing the spell's own
// unpaid cost -- the over-claim where a {1}{G} removal could read as spare
// against the {G}{G} reserve it then spent.
func (b Board) effectRankerAfter(me state.PlayerID, dmg int32, pendingCost string) func(decision.Option) targetRank {
	spare := b.hasSpareManaAfter(pendingCost)
	return func(o decision.Option) targetRank {
		if o.Kind == "player" {
			if o.Player == me {
				return targetRank{score: 0, idx: o.Index, ours: true}
			}
			if b.Life[o.Player] <= dmg {
				return targetRank{score: tierFaceLethal, idx: o.Index}
			}
			// A face the known damage does not reach is not a good value
			// target; it ranks below every creature (board over face).
			return targetRank{score: 0, idx: o.Index}
		}
		if o.Player == me {
			sc := int32(0)
			if c, ok := b.Creatures[o.Obj]; ok {
				sc = c.threat()
			}
			return targetRank{score: sc, idx: o.Index, ours: true}
		}
		c, ok := b.Creatures[o.Obj]
		if !ok {
			return targetRank{score: 0, idx: o.Index}
		}
		if c.remTough() <= dmg {
			if b.mayKillMe(me, c) {
				return targetRank{score: tierThreat + c.threat(), idx: o.Index}
			}
			if spare {
				return targetRank{score: tierValue + c.threat(), idx: o.Index}
			}
		}
		// Not a provable tier-1/2/3 option: the ordinary board-first rank.
		return targetRank{score: c.threat(), idx: o.Index}
	}
}

// rankOption scores one KTarget option for seat me. `ours` separates "a
// permanent/player of my own" from everything else, which is the only fact
// a targeting decision's owner-rule needs. An opponent's player option is
// worth faceScore -- a neutral constant (see faceScore); an opponent's
// permanent is worth its on-board threat when it is a battlefield creature
// in the census, or 0 when its Obj is in no zone this Board can read (a
// battlefield artifact/enchantment a removal could target, or a
// Graveyard/Hand/Exile object) -- never a crash, just a low rank.
func (b Board) rankOption(o decision.Option, me state.PlayerID) targetRank {
	if o.Kind == "player" {
		if o.Player == me {
			return targetRank{score: 0, idx: o.Index, ours: true}
		}
		return targetRank{score: b.faceScore(), idx: o.Index}
	}
	if o.Player == me {
		// My own permanent (or a "player" option naming me): a target a
		// removal-shaped effect should not be pointed at, deferred to the
		// every-option-is-mine fallback below -- but scored by the same
		// threat so a forced all-own decision still ranks its own
		// permanents rather than falling back to position order.
		sc := int32(0)
		if c, ok := b.Creatures[o.Obj]; ok {
			sc = c.threat()
		}
		return targetRank{score: sc, idx: o.Index, ours: true}
	}
	if c, ok := b.Creatures[o.Obj]; ok {
		return targetRank{score: c.threat(), idx: o.Index}
	}
	return targetRank{score: 0, idx: o.Index}
}

// chooseTargets is the KTarget policy. It answers the one question the old
// policy got wrong twice -- "which legal target" -- under these rules:
//
//   - R2' (the ft1 face-aiming tiers, when the effect is readable): when
//     the decision carries a known literal damage figure (effectDamage),
//     options are ranked by the three tiers of effectRanker instead of the
//     plain scale: an opponent creature that may kill the seat this round
//     and that the damage kills (tierThreat), then an opponent's face the
//     damage reaches lethally (tierFaceLethal), then the most powerful
//     creature the damage kills when there is spare mana (tierValue), and
//     everything else falls to the board-first ranking below. An unknown
//     effect, API or amount keeps the plain scale, where the face is
//     neutral -- an unknown is never treated as lethal or as a threat.
//   - R1 (never your own while an opponent exists): an option belonging to
//     the deciding seat is only ever chosen when every other option is also
//     theirs -- a self-target decision (a pump, an aura, a recursion that
//     offers only the seat's own board), which cannot help but point at
//     themselves. When any opposing option exists, the seat's own
//     permanents are never touched.
//   - R2 (board over face): the opponent's best battlefield creature is
//     always preferred over the opponent's face when one is a legal target.
//     The policy cannot read the effect that produced the decision, so it
//     cannot prove it reaches the player at all -- a burn and a draw read
//     identically -- which makes a face target a claim the policy cannot
//     support. So the face ranks neutrally (faceScore is a constant) below
//     every on-board creature threat, and a creature offered alongside it
//     is always the pick. A player target is chosen only when no opposing
//     creature is a legal target (a burn at a face, a drain, a life-gain
//     that only targets players), or as a later pick in a multi-target
//     decision.
//     R7 (deterministic neutral tie): equal faces -- the multiplayer case,
//     or a face beside a zero-fact option -- break on option index, so no
//     map iteration order reaches the answer.
//   - R3 (rank better than pt): opposing creatures rank by threat(), which
//     prices remTough, Tapped and evasion keywords on top of power, not by
//     a bare power or power+toughness figure.
//   - R4 (unreadable options): an option whose Obj is in no zone this Board
//     reads is a 0-score low rank, never a panic; it is only chosen when
//     every other option in its decision also ranks at or below it (the
//     all-off-board graveyard pick, say).
//   - R5 (honour Min/Max): exactly d.Min targets are picked when the
//     decision demands that many, up to d.Max; a may/up-to decision
//     (Min 0) fires its full legal width up to d.Max, so multi-target
//     removal does not idle half-efforts. The count is produced here, and
//     never left for clamp: clamp's top-up fills with lowest-index
//     options -- position order, the very thing this policy replaces -- so
//     an under-filled choice would reintroduce the positional bug by
//     another door.
//
// Like the combat branches, it consumes no rng: the pick is a pure function
// of the offered options and the board facts, and every tie breaks on
// option index, so no map iteration order reaches the answer.
func (b Board) chooseTargets(d *decision.Decision) []int {
	n := len(d.Options)
	if n == 0 {
		return nil
	}
	me := d.Player

	// R5: how many targets to produce, decided here independently of clamp.
	pick := d.Min
	if pick < 1 {
		// A may/up-to decision (Min 0) fires its full legal width: up to N
		// removal needs both targets to do its job.
		pick = d.Max
		if d.Max < 1 {
			pick = 0
		}
	}
	if d.Max >= 0 && pick > d.Max {
		pick = d.Max
	}
	// A per-target-priced spell (Strive, CR 702.52a) costs more for each
	// target beyond the first. Firing the full width the caster cannot pay
	// only reverses the cast (CR 733.1) -- and re-picking it re-proposes the
	// same unpayable cast. Stay within the engine's affordable count, never
	// below the decision's own Min.
	if d.AffordableTargets > 0 && pick > d.AffordableTargets && d.AffordableTargets >= d.Min {
		pick = d.AffordableTargets
	}
	if pick > n {
		pick = n
	}

	// The ranker: effect-aware (the three ft1 tiers) when the decision
	// carries a known literal damage figure, otherwise the plain
	// board-over-face ranking. A missing effect, an unknown API or a
	// null/absent amount all take the plain path -- the unknown-is-no rule
	// collapses into one branch, never assumed lethal per call site.
	var rank func(decision.Option) targetRank
	leadOwn := false
	if dmg, ok := b.effectDamage(d); ok {
		rank = b.effectRankerAfter(me, dmg, b.pendingSpellCost(d.Source))
	} else if d.TargetEffect != nil && (d.TargetEffect.API == "Counter" || d.TargetEffect.Removal != nil) {
		rank = b.removalRanker(me, d.TargetEffect)
	} else if beneficialGrant(d.TargetEffect) {
		// A one-way boon (Whirler Rogue's "can't be blocked"): the target
		// is the seat's OWN board, ranked by what the boon is worth to
		// each own permanent. The lead-order flip below points the grant at
		// the seat's most threatening creature instead of handing the
		// opponent's best creature a benevolent ability for free.
		rank = func(o decision.Option) targetRank { return b.rankOption(o, me) }
		leadOwn = true
	} else {
		rank = func(o decision.Option) targetRank { return b.rankOption(o, me) }
	}

	var own, foreign []targetRank
	for _, o := range d.Options {
		r := rank(o)
		if r.ours {
			own = append(own, r)
		} else {
			foreign = append(foreign, r)
		}
	}
	byScore := func(s []targetRank) {
		sort.SliceStable(s, func(i, j int) bool {
			if s[i].score != s[j].score {
				return s[i].score > s[j].score
			}
			return s[i].idx < s[j].idx
		})
	}
	// byScoreAsc is the grant branch's totality floor ordering: ascending
	// threat with the same index tie-break, so the residual foreign pick is
	// the weakest offered option, never the strongest (see the R1B note
	// above). A "player" option is pinned to the very END of the order:
	// a boon's granted statics target creatures, so a face pick is never a
	// sensible answer while any foreign creature is offered -- but it must
	// remain available for totality when no foreign creature is offered.
	byScoreAsc := func(s []targetRank) {
		harm := func(r targetRank) int32 {
			if d.Options[r.idx].Kind == "player" {
				return 1 << 30 // a face pick is the least plausible grant.
			}
			return r.score
		}
		sort.SliceStable(s, func(i, j int) bool {
			if harm(s[i]) != harm(s[j]) {
				return harm(s[i]) < harm(s[j])
			}
			return s[i].idx < s[j].idx
		})
	}

	// R1: lead with the opposing options; only top up with our own when a
	// decision is all-ours or does not offer enough of theirs to meet Min
	// (totality is preserved either way). R1B (the grant-polarity branch
	// above) INVERTS the lead order: a beneficial one-way grant is aimed at
	// our own board ONLY. The no-own-target case is not a policy choice the
	// branch makes -- the ability scorer (A1c) declines the activation
	// before any cost is paid when the seat controls no creature, so a bot
	// activation never reaches a boon ask that offers no own option. What
	// remains below is the totality floor: a posed KTarget decision must be
	// answered (a bot that returns fewer than Min answers is re-asked
	// forever -- the livelock shape), and the only way to reach a boon ask
	// with no own option is an activation whose COST consumed the seat's
	// last own creature between the activation and the ask. For that
	// residue the branch picks the LEAST threatening foreign option --
	// harm minimisation, never the gift-maximising "opponent's best
	// creature" the pre-fallback code handed out; a boon's value to its
	// receiver scales with the creature's worth, so the weakest receiver
	// loses the least.
	//
	// Group discipline (Decision.Validate's per-Group cap, decision.GroupAdmits
	// -- the ONE home the clamp arms at policy.go already derive from): options
	// sharing one non-empty Group are capped at Decision.GroupCapFor(g) together
	// -- at the default cap 1 that is the mutual-exclusion rule of the
	// TargetsForEachPlayer$ shape (one target per controller, rules/stack.go
	// oneEachTargetGroup) and the cross-mode Charm ask (one mode per player,
	// askCrossModeCharmTargets), both of which attach it; a raised cap (EACH's
	// per-type ChangeNum) admits several members of one group. Choosing past a
	// group's cap would hand back an intent Validate rejects and clamp cannot
	// repair -- the same wedging shape Clamp's own top-up skips (policy.go), so
	// the pick loop admits a group exactly as GroupAdmits does, the way a
	// duplicate index is skipped. A decision whose Min exceeds what its groups'
	// caps admit cannot be met by ANY answer; the ask builders never pose one
	// (OneEach Min is the distinct-controller count, and a dynamic-max Min is
	// 0).
	chosenCounts := make(map[string]int)
	var targetController state.PlayerID
	haveTargetController := false
	fits := func(o decision.Option) bool {
		if d.TargetsWithSameController && haveTargetController && o.Controller != targetController {
			return false
		}
		return d.GroupAdmits(chosenCounts, o.Group)
	}
	choices := make([]int, 0, pick)
	pickFrom := func(s []targetRank) {
		for i := 0; i < len(s) && len(choices) < pick; i++ {
			o := d.Options[s[i].idx]
			if !fits(o) {
				continue
			}
			if d.TargetsWithSameController && !haveTargetController {
				targetController, haveTargetController = o.Controller, true
			}
			if o.Group != "" {
				chosenCounts[o.Group]++
			}
			choices = append(choices, s[i].idx)
		}
	}
	if leadOwn {
		byScore(own)
		pickFrom(own)
		if len(choices) < pick {
			// The totality floor (R1B above): no own option was offered, so
			// the residue is harm-minimised over the foreign ones -- weakest
			// threat first, index tie-break -- never gifted to the opponent's
			// best creature.
			byScoreAsc(foreign)
			pickFrom(foreign)
		}
	} else {
		byScore(foreign)
		pickFrom(foreign)
		if len(choices) < pick {
			byScore(own)
			pickFrom(own)
		}
	}
	return choices
}
