package botpolicy

import (
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// abilityScore ranks ONE offered "ability" option (a non-mana activated
// ability, offered by rules/legal.go in an "ability" option whose Obj is the
// source permanent and whose Ability field is the index into its face's
// abilities). It is the one function chooseAbility reads, so the A-rules
// below are exactly this arithmetic, and a mutation that takes the first
// "ability" option, or treats every ability the same, changes it:
//
//   - A1 (no no-ops): an ability that provably changes nothing on the current
//     board is never activated. Two shapes are provable: a re-attach (an
//     equipment that is ALREADY attached to any permanent is declined,
//     because an already-attached permanent's activated ability is never
//     worth re-activating — an equip can only re-site the equipment, and
//     whether the move is a gain is a value read this policy cannot make)
//     (equipNoOp), and a redundant keyword grant (an activation whose whole
//     effect is a pure idempotent keyword grant that the granting permanent
//     already has, or an identical grant already pending from the same
//     source, is declined — grantNoOp, read off the engine-supplied
//     decision.Option.Grant). A third shape (A1c, boonHasNoOwnTarget)
//     declines a one-way BOON grant whose seat controls no creature to
//     receive it, before any cost is paid. Additive abilities (+1/+1 pumps,
//     counters, damage, draw) are never a no-op under grantNoOp and stay
//     freely repeatable; a pure keyword grant is the one activation this
//     policy can prove gains nothing the second time. Where the policy
//     cannot prove a no-op it may still activate — this rule is exactly the
//     set of changes it can prove are nil, not a guess. A1 scoring as
//     not-worth-taking is what lets the priority policy fall through to its
//     explicit pass and end a turn it would otherwise loop on.
//   - A2 (free before costly): among abilities worth activating, the cheaper
//     one ranks higher. A free ability forgoes nothing, so it is always at
//     least as good a trial as one that spends mana. The cost is read from
//     the ability option's own display label (legalActions formats it
//     "<Name>: <SpellDescription>", and Forge spells an activated ability's
//     cost — "Equip 2", "3, Sacrifice …" — into that text), so this is a
//     general cost read, not an equipment special case; an ability whose
//     label carries no number is treated as costly (ranks below a
//     provably-free one).
//   - A5 (repeatability budget): an ability whose source has already been
//     activated maxActivationsPerTurn times this turn is never activated
//     again (Board.Card.Activated reads state.Object.ActivatedThisTurn, the
//     events.Apply AbilityPush census). The rule exists because some
//     repeatable abilities form a cycle with the bot's own tap gate: Basalt
//     Monolith's "{3}: Untap this artifact" re-enables the {T} the tap gate
//     (T1's need gate) just spent the produced mana on, and the pair loops
//     forever -- net zero mana, board unchanged, one decision per cycle
//     (measured: seed 0 of the ulalek-eldrazi commander deck spun past
//     60,000 intents on turn 5 with only Tap/ManaAdd/AbilityPush events in
//     the window). The budget is bot-quality advice, never a rules gate: a
//     human seat's offers are unlimited (CR 605.1a) and the engine never
//     withholds anything for it. Four activations per source per turn is
//     generous for real repeat uses (double equip triggers, repeated land
//     returns) while each cycle consumes the budget, so every turn ends:
//     after the budget the policy falls through to its explicit pass
//     (A4), and with the untap declined the tap gate has nothing left to
//     re-enable. No map iteration reaches a choice (the count is a scalar
//     per option) and the pick stays a pure function of the Board, the
//     decision and the rng consumption points.
//   - A3 (deterministic tie): two abilities of equal score tie on option
//     index, so the whole function (and the policy through it) stays a pure
//     function of (Board, Decision, rng consumption points). No wall clock
//     and never a map range; both adapters answering the same decision
//     derive the same pick.
//   - A4 (progress always available): when NO ability ranks as worth
//     taking, chooseAbility returns -1 and the priority policy's explicit
//     pass fallthrough takes over, ending the turn. That fallthrough is the
//     thing that closes a loop, so it is pinned by a named test.
//
// An "ability" option whose source is not a battlefield permanent in the
// Board (a graveyard card, a hand card) carries zero board facts — its
// AttachedTo reads 0, so A1 never fires on it and A2's cost read still
// ranks it; it is never a crash. Like the target and combat branches this
// consumes no rng: the pick is a pure function of the offered options and
// the board facts.
func (b Board) abilityScore(o decision.Option, me state.PlayerID) (score int32, worth bool) {
	if b.equipNoOp(o, me) {
		return 0, false // A1: a provable no-op is never activated.
	}
	if b.grantNoOp(o) {
		return 0, false // A1: a redundant keyword grant is never activated.
	}
	if b.boonHasNoOwnTarget(o, me) {
		return 0, false // A1c: a one-way boon with no sensible own creature to receive it.
	}
	if b.Cards[o.Obj].Activated >= maxActivationsPerTurn {
		return 0, false // A5: the repeatability budget is spent.
	}
	// A2: cheaper ranks higher (only among worth-taking abilities; A1 above
	// already returned for the no-op case).
	return 1000 - abilityCost(o.Label), true
}

// boonHasNoOwnTarget is A1c's pre-activation decline, the fb-20260927T153930Z
// fix's activation-side half: an ability option whose whole activation is an
// Effect granting only one-way BOON statics to a target (decision.Option's
// GrantStatics, the offer-time twin of TargetEffect.Statics) is never
// activated when the seat controls no sensible own creature to receive it --
// the grant could then only land on an opponent's creature, the exact bug the
// report captured (the bot spent two artifacts to make an opponent's creature
// unblockable). The polarity classification is the shared helper
// (boonStaticsOnly, target.go), so this decline and the follow-up target
// ask's aim branch can never disagree about what counts as a boon.
//
// The census EXCLUDES the activation's own source permanent (o.Obj): an
// evasion grant is a this-turn effect, so a "can't be blocked" grant on the
// source itself is an attack plan this scorer cannot prove at offer time --
// with the source included, Whirler Rogue (itself a 2/2 creature) would
// always count as its own sensible target and the gate would never fire on
// the reported board (Whirler Rogue, two artifacts, no other own creature).
// With the source excluded, one Thopter from its ETB trigger is enough to
// make the activation worth taking again; the follow-up target ask then
// leads with the own board (target.go's R1B branch) and the grant lands
// there. The decline is bot-quality advice, never an engine gate: the offer
// loop still offers the ability (a human seat may want to aim a boon at an
// opponent's creature, and CR 605.1a's unlimited activations are untouched).
// An activation whose cost could sacrifice the seat's last own creature
// before the target ask is the one residue the decline cannot see at offer
// time; target.go's totality floor answers that ask harm-minimally instead.
func (b Board) boonHasNoOwnTarget(o decision.Option, me state.PlayerID) bool {
	if !boonStaticsOnly(o.GrantStatics) {
		return false
	}
	// Existential fold: order-independent, so no map iteration order can
	// reach a choice.
	for id, c := range b.Creatures {
		if id != o.Obj && c.Controller == me {
			return false
		}
	}
	return true
}

// AbilityWorthTaking reports whether the "ability" option o passes the
// bot's own activation guards for seat me: A1 (not a provable no-op -- a
// re-attach, an attach with no creature, a redundant keyword grant) and A5
// (the per-source, per-turn repeatability budget). It is the SAME test
// chooseAbility applies, exported so a search seat that offers the bot's
// alternatives as candidates (internal/azmcts) never offers one the bot's
// termination guarantee rests on declining: a free re-equip picked over the
// pass at every priority freezes the turn forever.
func (b Board) AbilityWorthTaking(o decision.Option, me state.PlayerID) bool {
	_, worth := b.abilityScore(o, me)
	return worth
}

// equipNoOp is A1's provable no-op: does activating the "ability" option o
// leave the board exactly as it is? Every permanent's AttachedTo is on the
// Board (companion to Power/CMC/Basic), filled by both adapter halves from
// the same facts a real seat sees. An equip (an AB$ Attach targeting a
// creature the controller controls, which is what K:Equip expands to in
// cards/keywords.go) leaves the board unchanged in exactly two provable
// ways:
//
//   - already attached: the equipment currently sits on a permanent
//     (AttachedTo != 0), so its Equip can only re-site it. An
//     already-attached permanent's activated ability is never worth
//     re-activating: moving an Equipment to a DIFFERENT own creature is a
//     value read this policy cannot make (it cannot read the granted
//     abilities — haste, shroud — that make a different bearer better), so
//     the re-attach is never taken. The first attach, from AttachedTo == 0,
//     still happens normally.
//   - no legal target: the controller has no battlefield creature at all
//     (hasOwnCreature(me) is false), so an attach-ability cannot land
//     anywhere and any activation fizzles with AttachedTo unchanged — a
//     free equip with nothing to equip onto, which is how a wiped board
//     freezes a game on the very equip the re-attach rule caught while a
//     creature survived.
//
// equipNoOp reads the attachment state itself; it never PREDICTS the target
// an equip would choose, so it cannot drift out of step with the target
// branch (target.go) that actually answers the follow-up target decision.
// AttachedTo == 0 with at least one creature is NOT a no-op — the equip
// really sits the equipment somewhere. A1 therefore only ever fires where
// the activation provably reproduces the current attach state.
//
// Scope (X1, promoted from the explore policy): both halves are facts about
// an ATTACH, so equipNoOp fires only on an option whose ability is an AB$
// Attach (decision.Option.Attach -- K:Equip, Reconfigure, Fortify, a gained
// Equip). Every other activated ability of an Aura or an attached Equipment
// (Holy Armor's pump, Flickerform's flicker) and every non-attach ability
// while the seat controls no creature (Tower of Eons, Well of Knowledge, a
// Rishadan Port) is not a re-site and is scored like any other ability. The
// production policy and ExploreDecide share this one implementation.
func (b Board) equipNoOp(o decision.Option, me state.PlayerID) bool {
	if !o.Attach {
		return false
	}
	if !b.hasOwnCreature(me) {
		// No battlefield creature to attach to: an equip has no legal target
		// and fizzles unchanged.
		return true
	}
	return b.Cards[o.Obj].AttachedTo != 0
}

// grantNoOp is A1's other provable no-op: an activation whose whole effect
// is a pure, idempotent keyword grant (decision.Option.Grant non-nil) gains
// nothing when either independent half holds -- the granting permanent
// already has every granted keyword (Already), or an identical activation
// from the same source is already on the stack unresolved (Duplicate). The
// stack half is the one that actually failed here (a Goblin Balloon Brigade
// granted flying three times in one main phase, all three pending), so
// checking only the resolved keyword set would miss it; checking only the
// stack would miss a re-activation after the first resolved. Additive
// abilities carry a nil Grant and always stack, so only a pure keyword
// grant can be a no-op here -- a +1/+1 pump or a damage/ draw effect is
// never suppressed. The Grant fact is engine-filled server-side (json:"-"),
// never derived from the option label, and both adapter halves receive the
// same Decision, so they agree on it.
func (b Board) grantNoOp(o decision.Option) bool {
	g := o.Grant
	return g != nil && (g.Already || g.Duplicate)
}

// hasOwnCreature is A1's no-legal-target census: does the deciding seat
// control at least one battlefield creature (the target test an Equip's
// "Creature.YouCtrl" ask needs)? It is ONLY the census — the threat-ranked
// "best own creature" projection the fix-round-1 A1 elected an equip
// target from is gone, because a projection is a second copy of the target
// heuristic and is not guaranteed to agree with the target branch that
// actually answers the follow-up decision. No ranking is produced and none
// is read, so a plain boolean is the honest shape (a map range here cannot
// leak order into a choice: the fold's result is the same for any order).
func (b Board) hasOwnCreature(me state.PlayerID) bool {
	for _, c := range b.Creatures {
		if c.Controller == me {
			return true
		}
	}
	return false
}

// abilityCost is A2's cost read for an "ability" option: the first number
// token in its display label, which is where the engine-facing cost lands
// (legalActions labels an ability "<Name>: <SpellDescription>", and Forge
// spells "Equip 2", "3, Sacrifice …" into SpellDescription). It is an
// approximation — the label is display text, not a parsed ManaCost — but it
// is the only cost the Board's offered options expose, it is deterministic
// (no map order, no clock), and it only orders two otherwise-worthy picks.
// An ability whose label carries no number reads as an arbitrarily costly
// ability (20), which ranks below any provably-free one without ever
// suppressing it (every worthy score stays above the -1 "nothing" sentinel,
// so an unnumbered ability is still worth taking).
func abilityCost(label string) int32 {
	var cur int32
	have := false
	for i := 0; i < len(label); i++ {
		c := label[i]
		if c >= '0' && c <= '9' {
			if have {
				cur = cur*10 + int32(c-'0')
			} else {
				cur = int32(c - '0')
				have = true
			}
		} else if have {
			return cur // the first digit run is the cost
		}
	}
	if have {
		return cur
	}
	return 20
}

// chooseAbility is the KPriority ability ranking: it picks ONE of the
// offered "ability" options, or returns -1 when none is worth taking. It
// replaces the position-first "take the first offered ability" that froze
// a whole game (see the package/cast.go docs and A4): every offered ability
// is scored by abilityScore (A1-A3) and the highest-scoring, lowest-index
// winner is chosen. -1 (nothing worth taking) is what lets the priority
// policy reach its explicit pass, which is what actually ends a turn the
// old code spent re-activating a no-op forever (A4). Consumes no rng.
func (b Board) chooseAbility(d *decision.Decision) int {
	best := -1
	var bestScore int32 = -1
	me := d.Player
	for _, o := range d.Options {
		if o.Kind != "ability" {
			continue
		}
		s, worth := b.abilityScore(o, me)
		if !worth {
			continue // A1: a provable no-op is never worth an activation.
		}
		// A3: ties break on the option's own Index (never map order).
		if best == -1 || s > bestScore || (s == bestScore && o.Index < best) {
			best, bestScore = o.Index, s
		}
	}
	return best
}

// maxActivationsPerTurn is A5's per-source, per-turn budget on the bot's own
// non-mana ability activations (state.Object.ActivatedThisTurn's census).
// Advice on the BOT's own behaviour only -- never an engine gate, so a human
// seat's unlimited activations (CR 605.1a) are untouched -- and generous
// enough that every legitimate repeat use this deck's cards ask for
// (double-kicker triggers, repeated equip re-sites) still happens.
const maxActivationsPerTurn = 4
