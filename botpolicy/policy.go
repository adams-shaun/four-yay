// Package botpolicy holds the one bot decision policy, in one copy.
//
// Both hosts that need a bot policy -- seat.Bot, answering a view.View, and
// the rules package's fuzz-test testBot, answering with only a *state.Game
// in hand -- build a Board and call Decide. rules cannot import seat without
// running the dependency order backwards (cards -> state -> decision ->
// events -> effects -> rules -> view -> seat; Ruling F7), so before this
// package existed the two sides each carried a line-for-line copy of the
// policy under a "keep the two in step" comment. This package is that
// comment made code: there is one policy, and the two adapters are thin
// value constructions (seat/bot.go's BoardFromView; rules/testbot_test.go's
// answer) feeding the same Decide with the same rng consumption points.
// seat/integration_test.go's TestBotAdaptersAgree* pins the two halves to
// the same Board for the same game facts.
package botpolicy

import (
	"math/rand/v2"
	"slices"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/deck"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// Board is the plain-data picture of the game the policy reads. Every fact
// the policy branches on lives here, produced identically by the two
// adapter halves — seat/bot.go's BoardFromView lifts it off the projected
// View a real client receives, and BoardFromGame (combat.go) lifts it off
// the engine's state.Game — so whatever the heuristic sees, whichever host
// asks, is the same board (pinned over a whole game by seat/integration_test.go's
// TestBotAdaptersAgreeOverWholeGame, and the commander twin pinned the
// same way by TestBotAdaptersAgreeOverCommanderGame). The fields are
// deliberately not speculative: a board fact no policy branch reads would
// be untested surface. Priority reads IsMain, Pool, Cards, Life and
// Commanders; combat reads Creatures, Life and Commanders; the cast scorer
// (cast.go) additionally reads Cast, FirstMain and MyTurn.
type Board struct {
	// OwnDeck is the deciding seat's genesis manifest, when supplied by the
	// rules engine; it is never an opponent or spectator deck list. It is
	// READ-ONLY: BoardFromGameInto fills it with the engine's shared genesis
	// manifest (rules.Engine.OwnDeckShared), so a write through it would
	// reach engine storage. Copy it (deck.Manifest.Clone) before changing it.
	OwnDeck *deck.Manifest
	// IsMain reports whether sorcery-speed actions are legal right now.
	// The seat adapter lifts it off the projected View's Phase
	// ("main1"/"main2", seat/bot.go); the rules test adapter lifts it off
	// the engine's own step (g.Step.IsMain), which is the same function of
	// the same five-phase string.
	IsMain bool
	// Creatures is every creature on every battlefield, keyed by object id:
	// the attacker and blocker census the combat heuristic reads. Both
	// adapters fill it from public facts only — a battlefield is public
	// for every seat — so the bot reasons about exactly the creatures the
	// seat can legally see, and the two halves agree on every one of them.
	Creatures CreatureTable
	// Life is every player's life total, keyed by seat. The blocking rule
	// reads the defender's own life to decide when a chump block is
	// warranted, which is public on both halves.
	Life LifeTable
	// Cards is the casting ranking's card-facts table, keyed by ObjID and
	// filled by both adapters for the DECIDING seat's own legally-seen
	// zones (its hand, graveyard, battlefield and command zone) from
	// cast.go (creature, power, mana value, basic-ness). The casting
	// policy (cast.go) reads an offered "cast"/"play_land" option's facts
	// here by Obj; the tap gate (tap.go) reads a card's printed mana cost
	// and its castability (which zone it sits in). The seat adapter fills
	// it off the projected CardViews the viewer receives; BoardFromGame
	// fills it off state.Game for the deciding seat — the two fill exactly
	// the same legal zones with the same derived facts, so a card ranks
	// the same whichever host asks (pinned over a whole game by
	// seat/integration_test.go's TestBotAdaptersAgreeOverWholeGame). A
	// hand/graveyard/battlefield/command-zone fact is the deciding seat's
	// own, so carrying it in the Board is no information leak (Ruling C0):
	// it is exactly what that seat may see.
	Cards CardTable
	// Pool is the deciding seat's current mana pool. The tap gate
	// (tap.go) reads it to decide whether another tap could newly enable a
	// cast: a pool that already pays some castable card's cost is a pool
	// that needs no more tapping, and a pool that pays none of them is the
	// "keep tapping" signal. Both halves fill it from the same numbers —
	// the projected View's own-pool map (poolView, the exact pool the
	// engine state carries) and state.Game's Pool field — so the gate sees
	// the same pool whichever host asks (pinned non-vacuously by
	// seat/integration_test.go's pool agreement). A mana pool is the
	// deciding seat's own private state, like its hand, so carrying it in
	// the Board is no information leak (Ruling C0).
	Pool state.Mana
	// PoolRestricted is the part of Pool, slot for slot, that carries a
	// RestrictValid$ spend limit (Myr Reservoir's "spend this mana only to
	// cast Myr spells": state.Player.RestrictedMana batches with a non-empty
	// Valid, the same batches the projected View names in PoolRestrictions).
	// PoolRestricted[i] <= Pool[i]. The T3 converter gate (tap.go) prices a
	// conversion over the UNRESTRICTED pool only: the engine will not spend a
	// restricted unit on an ability its restriction does not admit, so a
	// simulation that paid a converter's {1} out of restricted {C} saw
	// progress where the engine paid the {B} it then added back, forever
	// (cardfuzz batch5 line 9: Initiates of the Ebon Hand beside Myr
	// Reservoir's floating {C}{C}).
	PoolRestricted state.Mana
	// Commanders is the CR 903.6/903.10 commander bookkeeping, keyed by
	// object id: every commander object in the match (each player's
	// Commanders list, in Config order), with the CR 903.8 tax base
	// (Casts: times it has been cast from the command zone — the next such
	// cast costs an additional {2} per entry), whether it currently sits
	// in its owner's command zone (InCommandZone — the only zone a cast
	// of it is taxed), and the 21-damage clock (Damage: cumulative combat
	// damage it has dealt, keyed by the player who took it; nil when it
	// has dealt none). Both adapter halves fill it from public facts — the
	// command zone is public (m30 made ZCommand project), and commander
	// identity, cast counts and damage are open information — so the
	// casting rule and the combat clock read the same facts whichever
	// host asks. Damage is keyed by the commander, not its controller:
	// CR 903.10 charges a commander's tally whether the object currently
	// attacks for its owner or anyone else. A fact the policy never reads
	// (the commander's owner) is deliberately not carried.
	//
	//	Stack is the public stack census, bottom to top in the stack's own
	//	order (state.Game.Stack and view.View.Stack are both order-preserving,
	//	so the census is an ordered slice and no map iteration reaches any
	//	choice that reads it). The stack is a public zone (Ruling T23-u;
	//	view.View.Stack already carries Controller and Kind for exactly this),
	//	so carrying it is no information leak (Ruling C0). The casting rule
	//	(cast.go's C8) reads it to tell whose spells are on the stack: a
	//	counter cast is worth its mana only when a FOREIGN spell is there to
	//	counter, never at an own-spells-only (or empty) stack. Both adapter
	//	halves fill it identically (seat/bot.go's BoardFromView off the
	//	projected StackView list; BoardFromGame off state.Game.Stack), pinned
	//	on every intent of a whole game by seat/integration_test.go's parity
	//	tests.
	Commanders CommanderTable
	// Stack is the public stack census, bottom to top in the stack's own
	// order (state.Game.Stack and view.View.Stack are both
	// order-preserving, so the census is an ordered slice and no map
	// iteration reaches any choice that reads it). The stack is a public
	// zone (Ruling T23-u; view.View.Stack already carries Controller and
	// Kind for exactly this), so carrying it is no information leak
	// (Ruling C0). The casting rule (cast.go's C8) reads it to tell whose
	// spells are on the stack: a counter cast is worth its mana only when
	// a FOREIGN spell is there to counter, never at an own-spells-only (or
	// empty) stack. Both adapter halves fill it identically (seat/bot.go's
	// BoardFromView off the projected StackView list; BoardFromGame off
	// state.Game.Stack), pinned on every intent of a whole game by
	// seat/integration_test.go's parity tests.
	Stack []StackEntry
	// Cast is the learned cast profile the cast scorer dots its feature
	// vector with (cast.go's CastWeights). It is CONFIGURATION, not board
	// state: the adapters never fill it (BoardFromGameInto's refill contract
	// leaves it untouched, so a profile set once on a reused Board survives
	// every refill), and the zero value is treated as DefaultCastWeights --
	// the pre-refactor arithmetic, byte for byte (cast.go's castWeights).
	// A Board nobody configured plays the default bot.
	Cast CastWeights
	// FirstMain reports whether the current main phase is the FIRST one
	// (main1, not main2): the Precombat feature's board half. It is filled
	// exactly like IsMain on both adapter halves -- the projected View's
	// Phase string ("main1") on the view half, g.Step == state.StepMain1 on
	// the game half -- so both halves agree on it wherever they agree on
	// IsMain itself.
	FirstMain bool
	// MyTurn reports whether the deciding seat is the ACTIVE player (the
	// seat whose turn it is): the InstantOnOwnTurn feature's board half. A
	// main phase can belong to another seat (an opponent holding priority
	// during the active seat's main phase), so IsMain alone cannot say
	// "own main phase". Filled from the projected View's Active field on
	// the view half and g.Active on the game half, the same field both
	// halves already agree is public.
	MyTurn bool
	// Step is the exact engine step (CR 500.1's turn structure) the
	// deciding seat is currently in: the window facts the cast scorer's
	// timing features read (InstantOwnPreMain, InstantOwnCombat,
	// InstantOppTurn, InstantOppEnd). The game half lifts it straight off
	// g.Step; the view half parses the projected View.Step string back
	// through state.ParseStep, which is view.View.Step's own producer
	// (view/view.go sets Step = g.Step.String()), so both halves name the
	// same engine step. A step is public information (the projected View
	// already carries it for every seat), so carrying it in the Board is
	// no information leak (Ruling C0). The zero value is StepUntap; the
	// new timing features are all weight 0 in the default profile, so a
	// Board that never sets it -- every pre-timing test and adapter path --
	// plays identically.
	Step state.Step
	// LibrarySize and HandSize are the deciding seat's own library and hand
	// card counts -- public counts (view.PlayerView's library_size and
	// hand_size), so carrying them is no information leak (Ruling C0). The
	// RepeatOptional$ election arm reads them: every corpus carrier whose
	// do/while body consumes a zone (Ad Nauseam and Dance with Calamity dig
	// the library, Kindle the Carnage discards from hand) makes no progress
	// once that zone is empty, and repeating it then is a legal but endless
	// loop (the the-epic-storm botbench livelock). Filled from
	// len(g.Zone(...)) on the game half and the projected PlayerView on the
	// view half.
	LibrarySize int32
	HandSize    int32

	// explore is set only by ExploreDecide (the value copy it receives), so
	// no adapter fills it and every production policy reads false.
	explore bool
	// attackSim is set only by AttackSimDecide (attacksim.go, the opt-in
	// combat-simulation attacker); nil in every production policy.
	attackSim *AttackSimParams
	// kwArena backs every Creatures entry's Keywords: BoardFromGameInto
	// truncates and refills it with the creatures, so a reused Board copies
	// keyword lists without a per-creature allocation (valid, like the
	// tables, until the next refill).
	kwArena []string
}

// Commander is the Board's per-commander commander-format bookkeeping,
// filled identically by both adapter halves (seat/bot.go's BoardFromView
// off the projected View's Commanders/CommanderCasts/CmdDamage fields,
// BoardFromGame off state.Game's Player.Commanders/CmdCasts/CmdDamage —
// combat.go). Casts is the CR 903.8 tax base: the next command-zone cast
// of this commander costs an additional {2} for each prior such cast, and
// the casting rule prices exactly that. InCommandZone is whether the
// commander currently sits in its owner's command zone — the only zone a
// "cast" of it is taxed (a hand cast is an ordinary cast). Damage is the
// CR 903.10 21-clock: cumulative combat damage this commander has dealt,
// keyed by the player who took it; nil (on BOTH halves) until it has
// dealt some.
type Commander struct {
	Casts         int32
	InCommandZone bool
	Damage        map[state.PlayerID]int32
}

// StackEntry is one object on the stack, the plain-data shape of a
// StackView the policy can read without importing view (Ruling F7). CMC is
// carried for target ranking, so a counter can prefer a more valuable spell.
// IsSpell is true for a spell object (a card cast onto the stack, which a
// counter can target) and false for an ability object (minted by a
// TriggerPush/AbilityPush — Face-less; the engine's target census skips
// Face-less stack objects (rules/stack.go), so no counter can be cast at one
// today. Ability-targeting counters (Stifle) and Defined$ ValidStack
// counter-all spells (Glen Elendra's Answer) are open engine gaps; revisit
// C8 when they land). Controller is the object's controller, which for a
// stack spell is the seat that cast it. Both adapter halves fill it the
// same way, so C8 judges the same stack whichever host asked.
type StackEntry struct {
	ID         state.ObjID
	Controller state.PlayerID
	IsSpell    bool
	CMC        int32
	// ManaCost is the spell's printed cost in Forge notation, empty for an
	// ability object (which is Face-less -- the view half's sv.Card is nil
	// for a "trigger"/"ability"). It carries the coloured pips CMC cannot:
	// the target policy reads it to price the PENDING payment a cast-target
	// ask precedes (CR 601.2b/c -- the spell is on the stack but unpaid), so
	// effectRanker's spare-mana reserve test deducts what the payment will
	// actually consume instead of assuming at most one unit. Both adapter
	// halves fill it the same way from the same printed face (the game half's
	// o.Face().ManaCost for a spell, the view half's StackView.Card.ManaCost),
	// so C8's stack census stays equal whichever host asked.
	ManaCost string
}

// closesClock reports whether an unblocked swing from the creature id —
// whose creature facts are a — would take player p to 21 or more
// commander damage from that commander (CR 903.10: a loss that ignores
// life). It is the single expression behind all four clock rules
// (cast.go's CR1 reads Damage only indirectly through the tax; combat.go's
// AR5/BR3/BR4 read it directly), so the attack and block heuristics price
// the same second track. A creature that is not a commander (no
// Commanders entry) or whose commander has never hit p (no Damage entry)
// never closes a clock.
func (b Board) closesClock(p state.PlayerID, id state.ObjID, a Creature) bool {
	cm, ok := b.Commanders.Lookup(id)
	if !ok {
		return false
	}
	return cm.Damage[p]+a.Power >= 21
}

// Decide implements every decision.Kind the engine (or a future one) can
// ask, so the policy needs no rules knowledge either (Ruling P7):
//
//   - KPriority: tap for mana only in a main phase (Ruling T25-b -- tapping
//     during the upkeep or combat empties the pool before there is anything
//     worth spending it on), then make a land drop, then cast, then pass.
//     IsMain comes from b, so whichever adapter built the Board decides
//     the gate (the seat reads the View's Phase; the rules test host reads
//     e.G.Step.IsMain()). The "activate" block before everything reaches
//     the bot's tap-for-mana activations: legalActions (rules/legal.go)
//     offers only mana abilities as "activate" options ("Tap X for mana"),
//     never non-mana activated abilities (those are "ability"), so it
//     fills the pool in a main phase before the land drop and cast. Which
//     "activate" is tapped stays position-first within the offered group;
//     WHAT the gate asks before tapping any of them is need (tap.go, T1):
//     activate only while the pool cannot currently pay ANY card the seat
//     could cast from a castable zone (hand, command zone, graveyard with
//     Flashback), priced with the CR 903.8 commander tax when the card sits
//     in the command zone, and fall through to the land drop and the cast
//     the moment it can. The pool the gate reads is b.Pool, filled
//     identically by both adapter halves. The old position-first block
//     tapped until nothing untapped was left, floating mana in bulk and
//     letting each step's end (CR 500.4) empty whatever a cast never
//     spent; the need gate keeps the tap until the first cast opens and
//     lets the cast take over (measured, op6: the waste share of floated
//     mana fell from 32.9% to 22.8% constructed and 28.0% to 24.6%
//     four-seat commander, see the task report). The land
//     drop and the cast then order with chooseLand before chooseCast (G0,
//     cast.go).
//   - "concede" (M2d-3): never picked. It is another priority option kind,
//     served last after "pass", but no policy wants to leave the game it
//     is winning; the explicit kind scans below return before any blind
//     fallback, and clamp's top-up prefers "pass" (Ruling T25-g), which
//     legalActions (rules/legal.go) always offers. Unknown option kinds are
//     otherwise never chosen by this switch at all.
//   - KTarget: the targeting heuristic in target.go's chooseTargets (R1-R5,
//     stated there): never target your own permanent while an opposing
//     option exists, prefer the opponent's best battlefield creature over
//     their face (ranked by threat(), not pt() or raw Power), and honour
//     Min/Max without leaning on clamp. Consumes no rng.
//   - KAttackers/KBlockers: the combat heuristic in combat.go's
//     chooseAttackers/chooseBlockers (AR1-AR6 / BR1-BR4, stated there),
//     including the per-attacker defender choice (AR6) and the commander
//     clock (AR5/BR3/BR4). The opt-in LethalPressureDecide variant adds
//     AR7. Neither consumes the rng: the choice is a pure
//     function of the offered options and the board facts both adapters
//     supply.
//   - KTriggerOrder: a permutation of the offered indices drawn from the
//     bot's own rng, so ordering paths get fuzz coverage too.
//   - KTriggerOptional: accept printed optional triggers and Miracle, but
//     decline only api:Effect OptionalDecider$ elections (EffectOptional).
//     Both choices are deterministic and consume no rng.
//   - KChoose: every option in one decision shares a Kind (Option.Kind, not
//     d.Kind) that says what is being chosen. "x" takes the highest option
//     (the most an {X} cost can pay for -- options ascend); "exile"/
//     "sacrifice" take the first Max options; "yes"/"no" always answers yes
//     (Kind "yes" is first, per how askers build the two-option list),
//     except a copy_optional election on a copy of a copy (CH1: a may-copy
//     chain ends after one hand-over) and the repeat_optional stop rules;
//     "name"/"type"/"number" take the first offer. No rng is consumed,
//     unlike KBlockers/KTriggerOrder/KTriggerOptional above.
//   - KMulligan: the London round (Config.Mulligans > 0) offers two shapes on
//     one kind. A bottoming decision (every option Kind "bottom", Min ==
//     Max == taken) bottoms the taken lowest-indexed cards -- Choices
//     [0,1,...,taken-1] in ascending index order. A keep/mulligan decision
//     mulligans with probability 1/3 off the bot's own rng when a
//     "mulligan" option is offered (the determinism mirror of
//     KTriggerOptional), otherwise keeps (the "keep" option at index 0). The
//     rng is consumed only where a real mulligan choice exists.
//   - KModes: a modal announcement or mid-resolution pick -- choose the first Min options
//     in order (Choices [0, 1, …, Min-1]), the recorded mirror of the
//     engine-side first-mode stand-in, no rng. This also answers an
//     UnlessCost$ may-pay, shaped as the same KModes kind: option 0 is
//     "Pay … — make a copy", so the policy always offers to pay and the
//     engine declines for it only when the payer's pool cannot cover the
//     cost.
//
// Anything else — any kind added later, and any case
// above that found nothing to pick — falls to the last resort: pass if one
// is offered and Min == 0, otherwise whatever clamp below tops up with.
//
// Ruling T25-c (fix round 1): every branch used to return its pick
// unclamped, so it only ever validated by coincidence of the Min/Max shapes
// today's engine happens to emit (rules/stack.go's askTarget already names
// TargetMin/TargetMax as coming). clamp is now the last thing every return
// does, so the totality guarantee holds by construction for any Min/Max the
// wire format allows, not only today's. Every access into d.Options remains
// guarded against the list being empty.
func Decide(b Board, d *decision.Decision, r *rand.Rand) decision.Intent {
	// main's 9be52252 promoted AR7 lethal pressure into the default bot, so the
	// default carries lethalPressure; the AR8 combined-lethal test stays opt-in
	// (CombinedLethalDecide only), per this branch's 162a8acc.
	return decide(b, d, r, true, false, false)
}

// LethalPressureDecide is the measured opt-in policy used by botbench. It is
// identical to Decide except that a combat attack which is lethal if
// unblocked is made even when the defender can trade for it cheaply.
func LethalPressureDecide(b Board, d *decision.Decision, r *rand.Rand) decision.Intent {
	return decide(b, d, r, true, false, false)
}

// CombinedLethalDecide is the opt-in AR8 bench policy: on top of the AR7
// per-attacker lethal test (LethalPressureDecide), a combat attack whose
// ATTACKING SET is lethal once the defender's minimum blocking response is
// subtracted is made even when no single attacker would be. It is exposed to
// cmd/botbench as the "ar8" policy and is NEVER wired into the hosted or
// production bot (the default Decide carries only AR7 lethal pressure, never
// the combined-lethal test).
func CombinedLethalDecide(b Board, d *decision.Decision, r *rand.Rand) decision.Intent {
	return decide(b, d, r, true, true, false)
}

// BlocksDecide is the opt-in BLK bench policy: identical to Decide except
// that KBlockers is answered by the whole-assignment heuristic in
// blocks.go (chooseBlockAssignment, rules B0-B3) instead of the per-blocker
// chooseBlockers. It is exposed to cmd/botbench as the "blocks" policy and
// is NEVER wired into the hosted or production bot.
func BlocksDecide(b Board, d *decision.Decision, r *rand.Rand) decision.Intent {
	return decide(b, d, r, true, false, true)
}

// ExploreDecide is the opt-in coverage-exploration policy cmd/cardfuzz
// seats play: Decide (lethal pressure included) with the activated-ability
// choices widened so a random-deck fuzzer reaches the abilities the
// production ranking never picks. It is NEVER wired into the hosted or
// production bot and is absent from host.NormalizeBotPolicy's vocabulary; the
// production Decide's answers, and every golden chain head, are unchanged by
// it. The differences (explore.go) are:
//
//   - X1 was promoted into the production policy: A1's attachment no-op
//     rule applies to attach abilities only for every policy (ability.go,
//     equipNoOp), so it is no longer an explore difference.
//   - X2: among the worth-taking abilities the pick is uniform over the
//     seat's rng instead of A2's cheapest-label ranking, so every loyalty
//     ability and every sibling ability of one source is reached (A2 reads the
//     first number in the DESCRIPTION, which is not the cost: Brightling's
//     "+1/-1" mode outranked its three {W} abilities forever).
//   - X3: in a main phase, with probability 1/3, an ability is activated
//     before the land drop and the cast (cycling, channel and other hand
//     abilities of a card the cast would otherwise always consume).
//   - X4: outside a main phase, with probability 1/4, an ability is
//     activated at an ordinary priority window (combat-only targets, upkeep
//     windows, a response to a spell on the stack).
//   - X5: a mana-ability / mana-colour pick is uniform instead of the first
//     offer (exploreManaPick).
//   - X6: mana is floated speculatively (exploreFloat), because the engine
//     offers an ability only once the floating pool pays it and the
//     production tap gate floats only toward a castable card.
//
// A5's per-source per-turn budget still bounds every repeat, so X3/X4
// cannot loop a turn, and X6 taps only plain {T} sources, so it ends when
// they are all tapped.
func ExploreDecide(b Board, d *decision.Decision, r *rand.Rand) decision.Intent {
	b.explore = true
	return decide(b, d, r, true, false, false)
}

func decide(b Board, d *decision.Decision, r *rand.Rand, lethalPressure, combinedLethal, blocksAssignment bool) decision.Intent {
	in := decision.Intent{Seq: d.Seq, Player: d.Player}
	switch d.Kind {
	case decision.KPriority:
		if b.explore {
			// X3/X4 (ExploreDecide): the early activation.
			if pick := b.exploreEarlyAbility(d, r); pick >= 0 {
				in.Choices = []int{pick}
				return Clamp(d, in)
			}
		}
		if b.IsMain {
			// A specialize option is already gated by the engine's shared
			// legality predicate (face, activation riders and payment). Take
			// the offered face deterministically before ordinary main-phase
			// development actions.
			for _, o := range d.Options {
				if o.Kind == "specialize" {
					in.Choices = []int{o.Index}
					return Clamp(d, in)
				}
			}
			// T1 (tap.go): the need-aware tap gate. The block this replaced
			// tapped the first "activate" option in every main phase until
			// every source was spent -- need-blind and position-first -- which
			// floated mana in bulk and let each step's end (CR 500.4) empty
			// whatever the cast never spent. chooseTap taps only while the
			// pool cannot pay any card the seat could cast from a castable
			// zone, and falls through (returns -1) the moment it can or there
			// is nothing to pay for, so the land drop below is reached with
			// unmade taps instead of exhausted ones.
			if pick := b.chooseTap(d); pick >= 0 {
				in.Choices = []int{pick}
				return Clamp(d, in)
			}
		}
		// G0 (cast.go): the land drop ranks before the cast. A land drop is
		// free and unconditional and only raises the mana ceiling for the
		// rest of the turn, so taking it first is weakly strictly better
		// than casting first (which would only tie — you would play the
		// land next priority anyway — and can never enable more). The one
		// "cast before land" argument, choosing the right-coloured land for
		// the spell you intend, is the chooseLand ranking's own job (L1),
		// not this ordering's. chooseLand picks the best land, chooseCast the
		// best spell.
		if pick := b.chooseLand(d); pick >= 0 {
			in.Choices = []int{pick}
			return Clamp(d, in)
		}
		if pick := b.chooseCast(d); pick >= 0 {
			in.Choices = []int{pick}
			return Clamp(d, in)
		}
		// chooseAbility (ability.go, A1-A4) ranks the offered "ability"
		// options by value instead of taking the first: a provable no-op
		// (an equip on an already-attached permanent, or one with no
		// creature to attach to, A1 — equipNoOp reads the attachment state
		// itself, never a projected target) is never activated, cheaper
		// abilities outrank costlier ones (A2), ties break on option index
		// (A3), and the block falls through to the explicit pass below when
		// nothing ranks as worth taking (A4). legalActions only offers
		// non-mana activated abilities as legal sorcery-speed actions here,
		// so no extra isMain gate is needed beyond this block's own check.
		if b.IsMain {
			pick := -1
			if b.explore {
				pick = b.exploreAbility(d, r) // X2
			} else {
				pick = b.chooseAbility(d)
			}
			if pick >= 0 {
				in.Choices = []int{pick}
				return Clamp(d, in)
			}
		}
		// Ruling T25-g (fix round 2): explicitly pass here, before clamp
		// ever runs. This is the common case -- outside a main phase, or
		// with nothing affordable in one -- and legalActions
		// (rules/legal.go) lists every "activate" option before "pass", so
		// without this, clamp's blind first-unused-index top-up (needed
		// because a priority decision is always Min:1/Max:1, so falling
		// through with in.Choices empty is not itself a legal answer) would
		// reach for an activation and reintroduce I-1(b) through the
		// fallback path -- exactly what fix round 1 missed, because its own
		// synthetic regression decision carried a play_land option the loop
		// above matches unconditionally, a shape the live engine never
		// offers outside sorcery speed. Pass is offered on every priority
		// decision the engine emits; if it is somehow absent, this falls
		// through to the shared last resort below, same as any other kind.
		// M2d-3: the "concede" option sits directly after "pass" in the
		// option list, so this explicit scan is also what keeps the bot from
		// ever conceding -- it returns pass before clamp, or any
		// position-based fallback, can reach the new final option.
		if b.explore {
			// X6 (ExploreDecide): float mana toward abilities.
			if pick := b.exploreFloat(d, r); pick >= 0 {
				in.Choices = []int{pick}
				return Clamp(d, in)
			}
		}
		for _, o := range d.Options {
			if o.Kind == "pass" {
				in.Choices = []int{o.Index}
				return Clamp(d, in)
			}
		}

	case decision.KTarget:
		in.Choices = b.chooseTargets(d)
		return Clamp(d, in)

	case decision.KAttackers:
		if b.attackSim != nil {
			in.Choices = b.chooseAttackersSim(d, b.attackSim)
		} else {
			in.Choices = b.chooseAttackersMode(d, lethalPressure, combinedLethal)
		}
		// The attack-cost non-mana charge (CostLife/CostTaps) is a
		// whole-declaration constraint the per-pair option list cannot express,
		// and the engine rejects an unpayable declaration; the combat
		// heuristic is charge-blind, so route every KAttackers answer through
		// the one guard before Clamp repairs the mana budget and quota.
		in.Choices = LegalAttackChoices(b, d, in.Choices)
		return Clamp(d, in)

	case decision.KBlockers:
		if b.attackSim != nil && b.attackSim.Blocks {
			// The attack-sim arm's simulated blocks (blocksim.go).
			in.Choices = b.chooseBlockersSim(d, b.attackSim)
		} else if blocksAssignment {
			// BLK: the opt-in whole-assignment policy (blocks.go, B0-B3).
			in.Choices = b.chooseBlockAssignment(d)
		} else {
			in.Choices = b.chooseBlockers(d)
		}
		// CR 509.1a MinMaxBlocker bounds are a WHOLE-declaration constraint
		// the per-pair option list cannot express, and the engine crashes a
		// match on a rejected bot intent. Every policy's answer therefore
		// goes through this one guard, which drops any pair that would leave
		// its attacker's count outside the published bounds.
		in.Choices = legalBlockChoices(b, d, in.Choices)
		return Clamp(d, in)

	case decision.KTriggerOrder:
		// dp1: no more Fisher-Yates. The order the bot's own simultaneous
		// triggers hit the stack is a deterministic ranking of the offered
		// triggers by their source card's worth (trigger.go's
		// chooseTriggerOrder), reading the same card-worth arithmetic the
		// cast rule reads and consuming no rng -- so the same game state
		// always orders the same way, and the randomisation that widened
		// the bench's confidence intervals is gone.
		if n := len(d.Options); n > 0 {
			in.Choices = b.chooseTriggerOrder(d)
			return Clamp(d, in)
		}

	case decision.KTriggerOptional:
		// Only the api:Effect delayed body's no-host election defaults to
		// decline. Printed triggers and Miracle retain dp1's accept policy.
		if d.EffectOptional {
			in.Choices = declineOptional(d)
		} else if len(d.Options) > 0 && d.Options[0].Kind == "yes" {
			in.Choices = []int{d.Options[0].Index}
		}
		return Clamp(d, in)

	case decision.KCommanderZone:
		// Share CR1's CMC-scaled penalty and its nonnegative acceptance
		// boundary. Leaving does not guarantee access from the destination.
		if len(d.Options) > 0 && d.Options[0].Kind == "command_zone" {
			src := d.Options[0].Obj
			if b.cardWorth(src)-b.commandTax(src) >= 0 {
				in.Choices = []int{d.Options[0].Index}
			} else {
				for _, o := range d.Options {
					if o.Kind == "leave" {
						in.Choices = []int{o.Index}
						break
					}
				}
			}
		}
		return Clamp(d, in)

	case decision.KReplacement:
		// CR 616.1: the affected player orders competing replacement effects.
		// The effects themselves are unread -- botpolicy sees only the source
		// permanent each option names -- so the arm ranks the real
		// "replacement" options by the same standing-worth proxy the
		// trigger-order arm uses (cardWorth), descending, with the offered
		// index as the deterministic tie-break. A "skip_replacement" opt-out
		// is bypassed while at least one real replacement is offered. Every
		// other KReplacement shape (a colour-valued "mana" pick, an optional
		// "apply"/"decline") keeps the ordinary fallback below.
		if pick := b.chooseReplacementOrder(d); pick >= 0 {
			in.Choices = []int{pick}
		}
		return Clamp(d, in)

	case decision.KStartingPlayer:
		// CR 103.1's second half: the winner of the toss chooses who takes the
		// first turn. The bot names itself -- the deterministic default, the
		// pre-choice seat, consuming no rng. The kind's contract is that the
		// self-choice is the option whose Player equals the asking seat; if it
		// is somehow absent, option 0 (the first offered seat in turn order)
		// is the clamp-legal fallback, the same R-9 shape every kind shares.
		if len(d.Options) > 0 {
			in.Choices = []int{d.Options[0].Index}
			for _, o := range d.Options {
				if o.Player == d.Player {
					in.Choices = []int{o.Index}
					break
				}
			}
		}
		return Clamp(d, in)

	case decision.KChoose:
		// A mana-payment window (any choose offering "activate" sources:
		// the cast payment window, the UnlessCost$/Ward windows, cumulative
		// upkeep) is a payment continuation, not a generic choose: activate
		// one source at a time while one is offered, and submit Done once
		// the engine has closed the source list (its pool covers the
		// charge). A non-tapping pool converter is never taken here -- see
		// chooseManaWindow (T4).
		if pick, ok := chooseManaWindow(d); ok {
			in.Choices = []int{pick}
			return Clamp(d, in)
		}
		if len(d.Options) == 0 {
			break
		}
		if b.explore {
			// X5 (ExploreDecide): a mana-ability or mana-colour pick (every
			// option a "mana" option: the stage-1 "choose a mana ability of
			// <source>" wheel, or the stage-2 colour ask) is uniform over the
			// seat's rng instead of the first offer, so a dual land's second
			// colour, a filter's or storage land's costed ability and a
			// sacrifice-for-mana ability are reached.
			if pick, ok := exploreManaPick(d, r); ok {
				in.Choices = []int{pick}
				return Clamp(d, in)
			}
		}
		if d.ResumeKind == "copy_optional" && d.CopyOfCopy {
			// CH1 (chain copies): decline a may-copy election whose spell is
			// itself a copy. The chain family ("that player may copy this
			// spell": Chain of Smog, Barroom Brawl) hands the copy to another
			// seat, whose copy offers the copy back, and two bots that always
			// accept extend it forever -- a legal loop no state change ever
			// ends (cardfuzz batch1: Chain of Smog on empty hands, 20000
			// intents in one main phase). Accepting the FIRST link (the
			// original spell) keeps the card's real value; declining every
			// copy-of-a-copy bounds each chain at one hand-over. The fact is
			// engine-filled (decision.Decision.CopyOfCopy), so both adapter
			// halves see it identically, and no rng is consumed.
			for _, o := range d.Options {
				if o.Kind == "no" {
					in.Choices = []int{o.Index}
					return Clamp(d, in)
				}
			}
		}
		if d.ResumeKind == "repeat_optional" {
			// Repeat while life remains above the deterministic safety margin;
			// this is deliberately conservative for Ad Nauseam and legal for
			// every yes/no RepeatOptional$ election. Stop, too, once the
			// deciding seat's library or hand is empty: the carriers' bodies
			// dig the library or discard from hand, so a further iteration
			// reveals/discards nothing and repeating it forever is a no-
			// progress loop (a 0-mana-value library drains Ad Nauseam without
			// any life loss, so the life gate alone never fires). For the
			// carriers that consume neither zone this only stops early.
			if b.Life.Get(d.Player) > 5 && b.LibrarySize > 0 && b.HandSize > 0 {
				in.Choices = []int{d.Options[0].Index}
			} else if len(d.Options) > 1 {
				in.Choices = []int{d.Options[1].Index}
			}
			break
		}
		switch d.Options[0].Kind {
		case "gift_decline", "gift_promise":
			// CR 702.168: the Gift election. The deterministic bot declines
			// (the plain-cast direction, exactly the R-9 no-ask stand-in's
			// behaviour), selected by KIND rather than index so a reordered
			// option list that put a promise first still takes the decline.
			// The option Kind is the one shared identifier the engine's
			// castAnswer arm dispatches on too, so the bot can never submit an
			// answer the flow routes differently. Min 1 / Max 1 is the whole
			// legal-answer rule, and any offered option satisfies it -- the
			// botpolicy gift test runs this answer back through
			// Decision.Validate.
			in.Choices = []int{d.Options[0].Index}
			for _, o := range d.Options {
				if o.Kind == "gift_decline" {
					in.Choices = []int{o.Index}
					break
				}
			}
		case "protector":
			// CR 310.10 protector. The protector is the opponent the battle
			// will be attacked by/for, so prefer the opponent closest to
			// losing; ties retain the offered (seat) order, making the choice
			// deterministic. Board.Life carries every player's life (both
			// adapters fill it), so this reads the same totals whichever host
			// asks.
			best := 0
			for i := 1; i < len(d.Options); i++ {
				if b.Life.Get(d.Options[i].Player) < b.Life.Get(d.Options[best].Player) {
					best = i
				}
			}
			in.Choices = []int{d.Options[best].Index}
		case "vote_card":
			// A card ballot is a political vote: remove the opponent's most
			// valuable offered permanent, not merely the first one. Council's
			// Judgment's ballot excludes only the CASTER's permanents, so at
			// 3+ seats a voter's own permanent can be on the ballot beside an
			// opponent's (Option.Player is the subject's controller). A voter
			// must never vote to exile its own card when a foreign one is
			// offered, so the worth ranking runs over the non-self options
			// first, falling back to all options only when every offered
			// permanent is the voter's own (the caster excluded itself and no
			// opponent has a legal permanent -- the vote still has to name
			// something).
			best := -1
			for i, o := range d.Options {
				if o.Player == d.Player {
					continue
				}
				if best < 0 || b.cardWorth(o.Obj) > b.cardWorth(d.Options[best].Obj) {
					best = i
				}
			}
			if best < 0 {
				// Every offered permanent is the voter's own: name the
				// highest-worth one rather than reading past the option list.
				best = 0
				for i := 1; i < len(d.Options); i++ {
					if b.cardWorth(d.Options[i].Obj) > b.cardWorth(d.Options[best].Obj) {
						best = i
					}
				}
			}
			in.Choices = []int{d.Options[best].Index}
		case "name":
			// The full corpus list is deliberately large and hidden cards are
			// not available in Board. Choose its deterministic first legal name;
			// this is also the R-9 no-host fallback and always validates.
			in.Choices = []int{d.Options[0].Index}
		case "x":
			in.Choices = []int{d.Options[len(d.Options)-1].Index} // the most it can pay for
		case "discard":
			in.Choices = b.chooseDiscard(d)
		case "exile":
			// Preserve the existing exile policy: this task adds sacrifice
			// choices, not a new policy for unrelated exile effects.
			in.Choices = b.chooseWorst(d)
		case "sacrifice":
			// Player-facing sacrifice asks use the same least-value choice as
			// the pre-existing mandatory give-up decisions. Optional asks still
			// take their first offered permanent, matching the no-host fallback.
			if d.Min > 0 {
				in.Choices = b.chooseWorst(d)
			} else {
				in.Choices = []int{d.Options[0].Index}
			}
		case "counter_kinds":
			for i := 0; i < len(d.Options) && i < 2; i++ {
				in.Choices = append(in.Choices, d.Options[i].Index)
			}
		case "dig", "hand_move", "hidden_pick", "counter_dist", "counter_pick", "counter_kind", "blight", "proliferate", "move_counter_kind", "reveal":
			// A Dig look-and-take, a "choose N matching cards from hand"
			// ChangeZone (handmove1), a Hidden$ True public-origin pick
			// (hiddenpick1), a DividedAsYouChoose$ PutCounter distribution
			// pick (Vastwood Hydra), a bare-Choices$ PutCounter pick
			// (Promise of Loyalty's vow), a Blight's per-player creature
			// pick (CR 701.60), or a hand-reveal pick (infernaltutor1:
			// Infernal Tutor's "Reveal a card from your hand"): take the
			// first Max options in offered (zone) order
			// -- the exact mirror of effDig's / effChangeZoneHand's /
			// effHiddenPick's / putCounterPickDistribute's no-ask stand-in (R-9),
			// so a bot-answered ask emits
			// the same MoveZone events the silent build did and no golden game
			// moves for the ask alone. An Optional$ Min-0 ask still takes the
			// full Max: the stand-in it mirrors plays "you may" as "do",
			// deterministically.
			//
			// A Dig carrying a cumulative budget (WithTotalCMC$, so
			// d.MaxSum > 0) is the exception: a blind first-Max answer can
			// exceed the sum cap, Decision.Validate rejects it, and the bot
			// re-derives the same rejected answer forever. Fill greedily in
			// offered order while the running Value sum fits the budget -- the
			// exact mirror of effDig's forced greedy take, so a budget dig
			// moves the same cards the no-host stand-in would.
			if d.HasBudget() {
				sum := 0
				// Bound by COUNT (len(in.Choices) < d.Max), not by index --
				// the same run-length bound effDig's forced greedy take uses
				// (effects/cardflow.go: `len(greedy) >= changeNum`). A
				// non-fitting option sitting before the cap must be skipped
				// and the scan continued, or the bot takes fewer cards than
				// the no-choice stand-in it mirrors (michelangelos_technique:
				// options [4,4,2], budget 6, Max 2 -> [0,2], not [0]).
				for j := 0; j < len(d.Options) && len(in.Choices) < d.Max; j++ {
					if sum+d.Options[j].Value > d.MaxSum {
						continue
					}
					sum += d.Options[j].Value
					in.Choices = append(in.Choices, d.Options[j].Index)
				}
				// A mandatory budget ask (Min > 0) whose greedy fill came up
				// short must still satisfy Min -- but every top-up must fit the
				// budget too, or Clamp's blind index-order padding (below)
				// hands back an intent Validate rejects and the bot
				// livelocks. effDig lowers Min to the forced affordable count
				// for exactly this reason, so a satisfying set always exists.
				if len(in.Choices) < d.Min {
					have := make(map[int]bool, len(in.Choices))
					for _, c := range in.Choices {
						have[c] = true
					}
					for _, o := range d.Options {
						if len(in.Choices) >= d.Min {
							break
						}
						if have[o.Index] || sum+o.Value > d.MaxSum {
							continue
						}
						have[o.Index] = true
						sum += o.Value
						in.Choices = append(in.Choices, o.Index)
					}
				}
			} else {
				// A per-type EACH ask (an option Group is one listed type, capped
				// at d.groupLimit picks) needs a Group-aware fill: a blind first-Max
				// run would spend the whole Max inside the FIRST Group and hand
				// back an intent the per-Group cap rejects (a bot livelock). For
				// ungrouped options -- every ask that routed here before -- the
				// count map never fires and the fill is the historical first-Max
				// take, byte-identical.
				groups := make(map[string]int)
				for j := 0; j < len(d.Options) && len(in.Choices) < d.Max; j++ {
					o := d.Options[j]
					if o.Group != "" && groups[o.Group] >= d.GroupCapFor(o.Group) {
						continue
					}
					if o.Group != "" {
						groups[o.Group]++
					}
					in.Choices = append(in.Choices, o.Index)
				}
			}
		case "multikick", "replicate", "squad":
			// CR 702.43 (multikicker), CR 702.55a (replicate) and CR 702.66
			// (squad) pose the same optional-count ask: option 0 is the
			// decline ("No multikick"/"No replicate"/"No squad", Amount 0)
			// and the options ascend to the largest count the board can still
			// pay, so taking the highest Amount is the same "most it can pay
			// for" rule the "x" arm reads. Paying the count is the positive
			// play -- the kicked/replicated/squadded cast mode was chosen for
			// the cost's effect -- and the count is bounded by affordability,
			// so the maximum is deterministic and legal.
			in.Choices = []int{d.Options[0].Index}
			best := d.Options[0].Amount
			for _, o := range d.Options {
				if o.Amount > best {
					best = o.Amount
					in.Choices = []int{o.Index}
				}
			}
		case "mutate_place":
			// CR 702.140b: place the mutating creature UNDER the target
			// (Amount 0) rather than on top (Amount 1, option 0). The target is
			// not chosen until after this ask -- the placement is announced
			// before CR 601.2c -- so the arm cannot compare the two bodies. A
			// merged permanent keeps the TOP card's name, types and P/T but
			// gains all abilities of every card beneath (CR 702.140d), so
			// mutating under preserves the characteristics of the permanent
			// already on the battlefield and still grants the mutating card's
			// abilities. Both options are legal, so this never wedges.
			in.Choices = []int{d.Options[0].Index}
			for _, o := range d.Options {
				if o.Kind == "mutate_place" && o.Amount == 0 {
					in.Choices = []int{o.Index}
					break
				}
			}
		case "move_counter":
			// A MoveCounter CounterNum$ Any amount pick: unlike the shared
			// first-Max arm above, option 0 here means "move ZERO counters",
			// not "take the first offered object". The R-9 no-host stand-in
			// effMoveCounter takes is take-ALL, so the bot must answer the
			// option whose Amount is the offered maximum, or a bot-answered
			// ask would move fewer counters than the silent build and a golden
			// game would move for the ask alone. The options are built 0..max
			// in order, so the highest Amount wins (ties keep the earlier
			// offer, deterministically).
			in.Choices = []int{d.Options[0].Index}
			best := d.Options[0].Amount
			for _, o := range d.Options {
				if o.Amount > best {
					best = o.Amount
					in.Choices = []int{o.Index}
				}
			}
		case "pay_life", "pay_W", "pay_U", "pay_B", "pay_R", "pay_G":
			// A mana pip's payment alternatives (manaAsk): hybrid colours plus
			// a phyrexian pip's "Pay 2 life". The first offer is a POOL colour,
			// and taking it can strand the cost's generic remainder -- measured
			// (commander bench, seed 1295, Solphim's {1}{R/P}{R/P} ability):
			// the bot paid the only R into a pip, the generic {1} went unpaid,
			// and the activation aborted with no progress -- every window,
			// forever, until the intent cap fired. Pool mana is the scarcer
			// resource (it is the ONLY thing that can pay a generic pip; life
			// pays only these alternatives), so prefer the life payment when
			// it is offered and the seat has life to spare; otherwise take the
			// first offer (the old behaviour). The 6 threshold leaves two pips'
			// worth of buffer, so a two-pip cost never lands the seat at 0.
			in.Choices = []int{d.Options[0].Index}
			for _, o := range d.Options {
				if o.Kind == "pay_life" && b.Life.Get(d.Player) >= 6 {
					in.Choices = []int{o.Index}
					break
				}
			}
		case "player":
			// api:Vote's PLAYER ballot (ResumeKind "vote", task votepb1): each
			// voter picks the player who should receive the vote. Take the first
			// offered entry that is not the voter themselves -- a vote that
			// damages or rewards its caster's own seat is the one a real player
			// avoids whenever the ballot allows it, and Círdan's `VotePlayer$
			// Player` ballot does allow self-votes, so the clamp fallback's
			// option 0 could be self. Mob Verdict's `VotePlayer$ Other` never
			// offers self, so its first entry is already an opponent. Any other
			// "player" KChoose (ChoosePlayer's "choice" arm) keeps the clamp
			// fallback: this arm only overrides the vote.
			if d.ResumeKind == "vote" {
				in.Choices = []int{d.Options[0].Index}
				for _, o := range d.Options {
					if o.Player != d.Player {
						in.Choices = []int{o.Index}
						break
					}
				}
			}
		case "search":
			// A budgeted search (WithTotalCMC$, so d.MaxSum > 0) mirrors its
			// R-9 stand-in, which picks greedy[:min]: for a quantity-only
			// filter the engine lowers Min to the forced greedy count (the
			// mandatory-budget rule), so a fill up to d.Min IS the greedy set;
			// for a stated-quality filter Min stays 0 and the stand-in finds
			// nothing, so the empty decline is the mirror. The group skip
			// below still applies (a DifferentNames search's options carry
			// name Groups even under a budget; 0 corpus carriers combine
			// them), so the fill cannot name one card twice and hand back an
			// intent Validate's mutual-exclusion rule rejects.
			if d.HasBudget() {
				sum := 0
				groups := make(map[string]bool)
				for _, o := range d.Options {
					if len(in.Choices) >= d.Min {
						break
					}
					if o.Group != "" && groups[o.Group] {
						continue
					}
					if sum+o.Value > d.MaxSum {
						continue
					}
					if o.Group != "" {
						groups[o.Group] = true
					}
					sum += o.Value
					in.Choices = append(in.Choices, o.Index)
				}
				break
			}
			// A hidden-library search whose options carry no Group keeps the
			// first-offer answer it has always taken (Min 0, so one card). An
			// EACH "EACH Forest & Plains" search (each1) builds one option per
			// eligible card with the type's ordinal in Group -- at most one per
			// Group may be selected -- so the answer takes the FIRST option of
			// each new Group up to Max: one card per listed type, in the
			// decision's deterministic option order (types in spec order,
			// library order within a type). A DifferentNames search's
			// name-Groups get the same shape, which is what the constraint
			// itself asks for (distinct names, filled to Max). Both are legal
			// under Decision.Validate by construction; clamp's group skip is
			// the backstop, never the path.
			if d.Options[0].Group == "" {
				in.Choices = []int{d.Options[0].Index}
				break
			}
			// groups counts picks per Group against d.GroupCapFor -- the same
			// cap Decision.Validate enforces -- so an EACH search's per-type
			// ChangeNum (each type contributes up to that many) is filled per
			// type, in option order, and the answer stays legal by construction.
			// At the default cap of 1 this is the historical one-per-Group fill.
			groups := make(map[string]int)
			for _, o := range d.Options {
				if len(in.Choices) >= d.Max {
					break
				}
				if o.Group == "" || groups[o.Group] >= d.GroupCapFor(o.Group) {
					continue
				}
				groups[o.Group]++
				in.Choices = append(in.Choices, o.Index)
			}
		default: // yes/no (yes is first), name, type, number: the first offer
			in.Choices = []int{d.Options[0].Index}
		}
		return Clamp(d, in)

	case decision.KMulligan:
		// The London round, two shapes on one kind (rules/mulligan.go).
		// Bottoming is a hand-retention decision, like discard.
		if len(d.Options) > 0 && d.Options[0].Kind == "bottom" {
			in.Choices = b.chooseDiscard(d)
			return Clamp(d, in)
		}
		// Keep/mulligan: mulligan with probability 1/3 when one is offered
		// (consuming the bot rng only where a real choice exists, the
		// determinism mirror of KTriggerOptional), else keep.
		if len(d.Options) > 1 {
			for _, o := range d.Options {
				if o.Kind == "mulligan" && r.IntN(3) == 0 {
					in.Choices = []int{o.Index}
					return Clamp(d, in)
				}
			}
		}
		if len(d.Options) > 0 {
			in.Choices = []int{d.Options[0].Index} // keep
			return Clamp(d, in)
		}

	case decision.KModes:
		// The Sacrifice unless-pay damage offer (Vexing Devil: "any opponent
		// may have it deal 4 damage to them") gets a deliberate arm, not the
		// first-option clamp: an opponent offered "take N to kill it"
		// decides on the offered permanent's worth against the life the
		// damage costs.
		if c := b.unlessSacrificeOffer(d); c != nil {
			in.Choices = c
			return Clamp(d, in)
		}
		// A mana UnlessCost$ election (Mana Leak, Daze, Spell Pierce, the
		// Chain Lightning pay-to-copy) is value-aware too: the first-option arm
		// would pay every tax, so a payer the opposing board already kills this
		// turn declines when the tax is a real drain -- spending the pool that
		// a race depends on does not change the lethal outcome. A safe payer
		// (and any non-lethal board) keeps the ordinary pay answer.
		if c := b.unlessManaPayOffer(d); c != nil {
			in.Choices = c
			return Clamp(d, in)
		}
		if d.ResumeKind == "unless_pay" {
			// The option marker, not its position, is the payment decision.
			// A decline-only ask has no pay marker and therefore stays a decline.
			for _, option := range d.Options {
				if option.Mode == decision.ModeUnlessPay {
					in.Choices = []int{option.Index}
					return Clamp(d, in)
				}
			}
			for _, option := range d.Options {
				if option.Mode == decision.ModeUnlessDecline {
					in.Choices = []int{option.Index}
					return Clamp(d, in)
				}
			}
		}
		// A modal announcement or mid-resolution pick: choose the first Min options
		// in order — the recorded mirror of the engine-side first-mode
		// stand-in, so bot-vs-bot behaviour is largely unchanged, and the
		// answer stays seed-deterministic. This also answers an UnlessCost$
		// may-pay, shaped as the same KModes kind: option 0 is "Pay … — make
		// a copy", so the bot always offers to pay and the engine declines
		// for it only when the payer's pool cannot cover the cost. No rng is
		// consumed: the first modes are a fixed policy, not a coin.
		//
		// A KModes carrying a cumulative budget (WithTotalCMC$, so d.MaxSum >
		// 0 -- a Play grant: Invoke Calamity, Rod of Absorption, Primeval
		// Spawn) is the exception: a blind first-Min answer can exceed the
		// sum cap, Decision.Validate rejects it, and the bot re-derives the
		// same rejected answer forever. Fill greedily in offered order while
		// the running Value sum fits -- the same fill the shared KChoose
		// budget arm uses. A Min-0 (Optional$) budget ask picks nothing, so
		// the decline stands-in unchanged; the engine lowers a mandatory
		// budget ask's Min to what the budget affords, so a satisfying set
		// always exists and Clamp's budget-aware top-up covers the rest.
		if d.HasBudget() {
			sum := 0
			for j := 0; j < len(d.Options) && len(in.Choices) < d.Min; j++ {
				if sum+d.Options[j].Value > d.MaxSum {
					continue
				}
				sum += d.Options[j].Value
				in.Choices = append(in.Choices, d.Options[j].Index)
			}
			return Clamp(d, in)
		}
		for j := 0; j < len(d.Options) && j < d.Min; j++ {
			in.Choices = append(in.Choices, d.Options[j].Index)
		}
		return Clamp(d, in)
	}

	// Last resort: pass if Min == 0 and one is offered; clamp below handles
	// everything else, including topping up to Min when nothing above (or
	// this) picked enough.
	if d.Min == 0 {
		for _, o := range d.Options {
			if o.Kind == "pass" {
				in.Choices = []int{o.Index}
				return Clamp(d, in)
			}
		}
	}
	return Clamp(d, in)
}

// facingLethal reports whether the opposing board already forces the
// deciding seat to zero: the total power of untapped opposing creatures that
// could attack reaches the seat's life. It is deliberately conservative --
// tapped creatures and an unknown life total are never lethal (the same
// missing-life convention AR7 uses) -- so a seat that is not clearly dying
// keeps paying its taxes.
func (b Board) facingLethal(p state.PlayerID) bool {
	life, ok := b.Life.Lookup(p)
	if !ok {
		return false
	}
	total := int32(0)
	for _, c := range b.Creatures.All() {
		if c.Controller == p || c.Tapped || c.Power <= 0 {
			continue
		}
		total += c.Power
	}
	return total >= life
}

// unlessManaPayOffer answers a payable UnlessCost$ election. It returns nil
// for a single-option ask (leaving the ordinary KModes arm in charge), and
// otherwise declines only when the payer faces lethal board damage -- the one
// case where spending the pool cannot win the race. The Sacrifice-damage
// offer is handled by unlessSacrificeOffer before this arm, so this reads
// only the plain pay/decline shape.
func (b Board) unlessManaPayOffer(d *decision.Decision) []int {
	if d.ResumeKind != "unless_pay" || d.ResumeSA == nil || len(d.Options) < 2 {
		return nil
	}
	if b.facingLethal(d.Player) {
		for _, option := range d.Options {
			if option.Mode == decision.ModeUnlessDecline {
				return []int{option.Index}
			}
		}
	}
	return nil
}

// unlessSacrificeOffer answers the Sacrifice unless-pay damage offer —
// Vexing Devil's "any opponent may have it deal N damage to them; if a
// player does, sacrifice it", posed as a KModes ask whose option 0 is
// "Take N damage" and option 1 "Refuse — it stays". The arm is deliberate,
// not the clamp fallback:
//
//   - accept when the offered permanent is worth more alive than the life
//     the damage costs (cardWorth >= n — a creature prices at 30+4×power,
//     so a real creature always clears a small N) AND the damage is not
//     lethal (n < the deciding seat's life) — taking four to kill a 4/3 is
//     normally correct; taking lethal damage to kill anything is not;
//   - decline otherwise (a worthless or unreadable permanent, or lethal
//     damage).
//
// It returns nil — leaving the ordinary KModes arm in charge — for every
// other KModes ask, including Sacrifice's mana unless-pay ("pay {1} or
// sacrifice it"), where the first-option arm's "pay" answer is right and the
// engine declines it only when the pool cannot cover the cost.
func (b Board) unlessSacrificeOffer(d *decision.Decision) []int {
	if d.ResumeKind != "unless_pay" || d.ResumeSA == nil || d.ResumeSA.API != "Sacrifice" {
		return nil
	}
	n, dmg := effects.ParseDamageUnlessCost(d.ResumeSA.Params["UnlessCost"])
	if !dmg || len(d.Options) == 0 {
		return nil
	}
	var pay, decline *decision.Option
	for i := range d.Options {
		option := &d.Options[i]
		switch option.Mode {
		case decision.ModeUnlessPay:
			pay = option
		case decision.ModeUnlessDecline:
			decline = option
		}
	}
	if pay == nil || decline == nil {
		return nil
	}
	if b.cardWorth(pay.Obj) >= int32(n) && b.Life.Get(d.Player) > int32(n) {
		return []int{pay.Index}
	}
	return []int{decline.Index}
}

// Clamp enforces [Min, Max] on top of whatever a policy's switch (or its
// fallback) picked (Ruling T25-c): truncate to at most Max, in the order
// already chosen, then -- if that leaves fewer than Min -- top up with the
// lowest-index unused options until Min is reached or none remain. It is the
// shared last resort every botpolicy return runs and the one seat.PolicyNetBot
// reuses for its own scored answers (L9c), so no second clamp copy can drift
// from it.
// the ones reachable today.
//
// Ruling T25-g (fix round 2): the top-up prefers an unused "pass" option
// over anything else. The KPriority branch above already returns "pass"
// explicitly before clamp ever runs, so this is defense in depth for
// whatever reaches here anyway (an absent "pass", or a future caller) --
// without it, a blind first-unused-index top-up would reach for whatever
// legalActions (rules/legal.go) happens to list first, which is every
// "activate" option, before "pass". That is precisely how fix round 1's own
// clamp reintroduced I-1(b): a Min:1 priority decision falling through with
// nothing chosen got topped up into an activation instead of a pass.
func Clamp(d *decision.Decision, in decision.Intent) decision.Intent {
	// A same-controller target answer must be repaired as one controller's
	// complete decision, not by retaining the first controller in the input:
	// that controller can lack Min legal options while a later controller can
	// satisfy Min, Groups and the budget. Build a feasible local decision
	// before the ordinary repair so Clamp never returns an answer Validate
	// rejects.
	if d.TargetsWithSameController {
		in.Choices = sameControllerChoices(d, in.Choices)
	}
	// A shared target-set answer has the same anchor trap the same-controller
	// repair above exists for, and SetPropShared is the one mode that cannot
	// fix it by appending. The running intersection is seeded by the first
	// pick: when that pick is alone in its shared class, every later option is
	// refused by SetPropAdmits, so the append-only top-up below can only return
	// an answer shorter than Min -- which Decision.Validate rejects and the
	// deterministic bot re-derives forever. Re-anchor the whole decision on
	// each represented shared class before the ordinary repair, exactly as
	// sameControllerChoices re-anchors on each represented controller. Only
	// SetPropShared needs this: SetPropDistinct's union accumulator always
	// admits any single pick, so its top-up can never be anchored out.
	if d.SetPropMode == decision.SetPropShared {
		if choices := setPropSharedChoices(d, in.Choices); choices != nil {
			in.Choices = choices
		}
	}
	var targetController state.PlayerID
	var haveTargetController bool
	if d.TargetsWithSameController && len(in.Choices) > 0 {
		targetController = d.Options[in.Choices[0]].Controller
		haveTargetController = true
	}
	// The decision's joint constraints -- the Max ceiling, the cumulative
	// budget (Decision.MaxSum, which Decision.Validate enforces) and the
	// Required quota (Option.Required, CR 508.1d's "attacks if able", which
	// the engine's declaration check enforces) -- are repaired FIRST, through
	// decision.FitRequired: the same rule (Decision.RequiredQuota) the engine
	// validates against, so an arm that builds its answer without pricing its
	// picks (KAttackers' combat heuristic, a KModes budget fill) can never
	// hand back an intent the engine refuses -- Submit would reject it without
	// consuming the decision and the deterministic bot would re-derive it
	// forever. An answer that already satisfies all three is returned
	// untouched, so every budget-free, requirement-satisfied answer is
	// byte-identical; otherwise the answer is rebuilt from the cheapest
	// affordable required picks, then the arm's own picks, in its order, are
	// swapped or appended while they fit.
	in.Choices = d.FitRequired(in.Choices)
	max := d.Max
	if max < 0 {
		max = 0
	}
	if len(in.Choices) > max {
		in.Choices = append([]int(nil), in.Choices[:max]...)
	}
	min := d.Min
	if min < 0 {
		min = 0
	}
	if len(in.Choices) < min {
		have := make(map[int]bool, len(in.Choices)) // membership only -- never ranged.
		// groups counts picked options per Group against d.GroupCap() -- the
		// same cap Decision.Validate enforces -- so a top-up can never hand
		// back an intent Validate rejects. At the default cap of 1 a nonzero
		// count is the historical boolean, so every limit-free top-up is
		// byte-identical.
		groups := make(map[string]int) // picked options per Group.
		sum := 0                       // running MaxSum budget over the chosen set.
		sum2 := 0                      // running MaxSum2 budget over the chosen set.
		setAcc := d.SetPropsOf(in.Choices)
		for _, c := range in.Choices {
			have[c] = true
			if c >= 0 && c < len(d.Options) {
				if d.Options[c].Group != "" {
					groups[d.Options[c].Group]++
				}
				sum += d.Options[c].Value
				sum2 += d.Options[c].Value2
			}
		}
		// fits reports whether topping up with o keeps the intent within the
		// decision's cumulative budgets. Both currencies go through Decision's
		// one admission rule (BudgetsFit), so a dual-budget target ask (power
		// in MaxSum, mana value in MaxSum2) can never be topped up into an
		// intent Decision.Validate rejects. A budget-less decision keeps the
		// byte-identical top-up.
		fits := func(o decision.Option) bool { return d.BudgetsFit(sum+o.Value, sum2+o.Value2) }
		add := func(o decision.Option) {
			if d.TargetsWithSameController && !haveTargetController {
				targetController, haveTargetController = o.Controller, true
			}
			have[o.Index] = true
			sum += o.Value
			sum2 += o.Value2
			setAcc = decision.SetPropMerge(d.SetPropMode, setAcc, o.SetProps)
			in.Choices = append(in.Choices, o.Index)
		}
		for _, o := range d.Options {
			if len(in.Choices) >= min {
				break
			}
			if o.Kind == "pass" && !have[o.Index] && fits(o) &&
				(!d.TargetsWithSameController || !haveTargetController || o.Controller == targetController) {
				add(o)
			}
		}
		for _, o := range d.Options {
			if len(in.Choices) >= min {
				break
			}
			if have[o.Index] {
				continue
			}
			// Options sharing one non-empty Group may contribute at most
			// groupLimit picks (Decision.Validate's general rule): topping up
			// past a Group's cap would hand back an intent Validate rejects --
			// an answer the engine cannot accept and clamp cannot repair, so
			// the capped Group is skipped the way a duplicate index is.
			if o.Group != "" && groups[o.Group] >= d.GroupCapFor(o.Group) {
				continue
			}
			if !fits(o) || (d.TargetsWithSameController && haveTargetController && o.Controller != targetController) ||
				!decision.SetPropAdmits(d.SetPropMode, setAcc, o.SetProps) {
				continue
			}
			if o.Group != "" {
				groups[o.Group]++
			}
			add(o)
		}
		// A Repeatable decision (a CanRepeatModes$ Charm, CR 601.2b) may need
		// MORE picks than it has distinct options -- CharmNum$ 3 over 2 legal
		// modes is answered as one mode twice. The two loops above cannot
		// exceed the option count, so fill the remaining slots by repeating an
		// ungrouped option; Decision.Validate permits the duplicate for exactly
		// this ask. Repeating a grouped option is never attempted: a repeatable
		// modal ask carries no Groups, and a group's exclusivity outranks the
		// arity nudge.
		//
		// The fill cycles the ungrouped options until Min is met: a single
		// pass tops up by at most len(Options), which leaves CharmNum$ 3
		// over ONE legal mode (every other mode's target gone) one pick
		// short -- an intent Validate rejects and the bot re-derives forever.
		if d.Repeatable {
			var ungrouped []int
			for _, o := range d.Options {
				if o.Group == "" {
					ungrouped = append(ungrouped, o.Index)
				}
			}
			for i := 0; len(ungrouped) > 0 && len(in.Choices) < min && len(in.Choices) < max; i++ {
				in.Choices = append(in.Choices, ungrouped[i%len(ungrouped)])
			}
		}
	}
	// The pile-B order (Intent.Rest) is the one repair-fragile answer shape:
	// the partition rule (Decision.validateRest, the same rule Validate
	// enforces) binds Rest to the exact chosen set, so a rest whose choices
	// were repaired (or that never was a partition) must drop Rest and fall
	// back to the legacy offered-order complement rather than hand back an
	// intent Submit rejects. An arrange answer whose choices and rest still
	// validate keeps both.
	if d.Kind == decision.KArrange && len(in.Rest) > 0 && d.Validate(in) != nil {
		in.Rest = nil
	}
	return in
}

// sameControllerChoices tries each represented controller in deterministic
// input-then-option order. It projects that controller's options into a local
// decision and uses Clamp recursively to apply the ordinary Max, Group,
// budget and Required constraints before accepting only a locally valid
// result. The projection prevents a controller with too few compatible picks
// from trapping repair when another controller has a legal answer.
func sameControllerChoices(d *decision.Decision, choices []int) []int {
	controllers := make([]state.PlayerID, 0, len(d.Options))
	seen := make(map[state.PlayerID]bool, len(d.Options))
	addController := func(p state.PlayerID) {
		if !seen[p] {
			seen[p] = true
			controllers = append(controllers, p)
		}
	}
	for _, c := range choices {
		if c >= 0 && c < len(d.Options) {
			addController(d.Options[c].Controller)
		}
	}
	for _, o := range d.Options {
		addController(o.Controller)
	}
	for _, controller := range controllers {
		local := *d
		local.TargetsWithSameController = false
		local.Options = nil
		original := make([]int, 0, len(d.Options))
		index := make(map[int]int, len(d.Options))
		for _, o := range d.Options {
			if o.Controller != controller {
				continue
			}
			index[o.Index] = len(local.Options)
			original = append(original, o.Index)
			o.Index = len(local.Options)
			local.Options = append(local.Options, o)
		}
		localIn := decision.Intent{Seq: d.Seq, Player: d.Player}
		for _, c := range choices {
			if i, ok := index[c]; ok {
				localIn.Choices = append(localIn.Choices, i)
			}
		}
		localOut := Clamp(&local, localIn)
		if local.Validate(localOut) != nil {
			continue
		}
		out := make([]int, len(localOut.Choices))
		for i, c := range localOut.Choices {
			out[i] = original[c]
		}
		return out
	}
	return nil
}

// setPropSharedChoices tries each represented shared class in deterministic
// input-then-option order. A shared-class answer is legal iff every pick
// carries one common property token, so projecting the decision onto a single
// token's class and clamping there makes the constraint vacuous (SetPropNone)
// and lets the ordinary Max, Group, budget and Required repair find a full
// answer. Each projected clamp is accepted only when it validates; the first
// valid one wins, and nil says no class can satisfy the decision (in which
// case the ordinary repair still runs and the caller keeps its fallback). The
// projection prevents a class too small to meet Min from trapping repair when
// a later class can. Input-order anchors first keep an already-valid answer on
// its own class, so its clamp is byte-identical.
func setPropSharedChoices(d *decision.Decision, choices []int) []int {
	// Anchors are the distinct tokens carried by the input picks (in input
	// order) then by every option (in offer order) -- determinism is the
	// whole reason for that ordering.
	var anchors []string
	seen := make(map[string]bool, len(d.Options))
	addAnchor := func(props []string) {
		for _, t := range props {
			if !seen[t] {
				seen[t] = true
				anchors = append(anchors, t)
			}
		}
	}
	for _, c := range choices {
		if c >= 0 && c < len(d.Options) {
			addAnchor(d.Options[c].SetProps)
		}
	}
	for _, o := range d.Options {
		addAnchor(o.SetProps)
	}
	for _, anchor := range anchors {
		local := *d
		local.SetPropMode = decision.SetPropNone
		local.Options = nil
		original := make([]int, 0, len(d.Options))
		index := make(map[int]int, len(d.Options))
		for _, o := range d.Options {
			if !slices.Contains(o.SetProps, anchor) {
				continue
			}
			index[o.Index] = len(local.Options)
			original = append(original, o.Index)
			o.Index = len(local.Options)
			local.Options = append(local.Options, o)
		}
		localIn := decision.Intent{Seq: d.Seq, Player: d.Player}
		for _, c := range choices {
			if i, ok := index[c]; ok {
				localIn.Choices = append(localIn.Choices, i)
			}
		}
		localOut := Clamp(&local, localIn)
		if local.Validate(localOut) != nil {
			continue
		}
		out := make([]int, len(localOut.Choices))
		for i, c := range localOut.Choices {
			out[i] = original[c]
		}
		return out
	}
	return nil
}
