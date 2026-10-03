// Package decision defines the only vocabulary a client needs: the engine asks
// a Decision listing every legal Option, and the client answers with an Intent
// naming option indices. No rules knowledge crosses the wire.
package decision

import (
	"fmt"
	"slices"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

type Kind string

const (
	KPriority  Kind = "priority"
	KTarget    Kind = "target"
	KAttackers Kind = "attackers"
	KBlockers  Kind = "blockers"
	// KMulligan drives the pre-game London mulligan round (rules/mulligan.go),
	// which Config.Mulligans > 0 runs between the opening deal and turn 1. It
	// is one kind with two phases distinguished by option vocabulary and
	// Min/Max: a keep/mulligan ask is Min == Max == 1 over a "keep" (and,
	// while the seat still has a free mulligan, a "mulligan") option; a
	// bottoming ask is Min == Max == taken over one "bottom" option per kept-
	// hand card -- exactly the distinct-index shape Validate already enforces
	// for KTriggerOrder, so no new wire format is needed (Ruling U2).
	KMulligan Kind = "mulligan"
	// KModes is a modal pick: MinCharmNum$ (default CharmNum$) through
	// CharmNum$ (default 1) over one "mode" option per Choices$ sub-ability,
	// in Choices$ order. Spell modes
	// are announced during casting (CR 601.2b), trigger modes at placement
	// (CR 603.3c), while nested Charm and unless-pay asks may suspend
	// resolution. handleModes records ModeChosen; ResumeKind and the trigger
	// drain flag select the appropriate continuation.
	KModes Kind = "modes"
	// KTriggerOrder asks one controller for the order of the two or more
	// triggered abilities they control that triggered simultaneously (CR
	// 603.3b). It is Min == Max == len(Options) over exactly that
	// controller's own pending triggers, so Validate's existing "N distinct
	// in-range indices" rule already means "a permutation" and no new wire
	// format is needed (Ruling U2).
	//
	// DIRECTION, which is silent if a client gets it backwards: Choices[0]
	// is the trigger put on the stack FIRST, and therefore the one that
	// resolves LAST. That matches the between-player rule the engine applies
	// either side of this choice -- CR 603.3b's APNAP puts the active
	// player's triggers on the stack first and resolves them last -- so one
	// sentence describes the whole placement. The Decision's own Prompt says
	// the same thing in words a client can show a player unchanged.
	KTriggerOrder Kind = "trigger_order"
	// KTriggerOptional asks whether an optional triggered ability (Forge's
	// OptionalDecider$ on a T: line) is put on the stack at all. Min == Max
	// == 1 over exactly two options, Kind "yes" and Kind "no", in that
	// order. There is no default: an unanswered optional trigger never
	// reaches the stack, and neither does a declined one.
	KTriggerOptional Kind = "trigger_optional"
	// KCommanderZone asks a commander's OWNER what happens to the commander
	// when it is about to be put into its owner's graveyard, hand or library
	// from anywhere, or exiled from anywhere: the owner may put it into the
	// command zone instead (CR 903.9). Min == Max == 1 over exactly two
	// options, in this order: index 0 Kind "command_zone" (put it into the
	// command zone), index 1 Kind "leave" (let the zone change happen as it
	// would have). It is the OWNER who answers, never the controller -- a
	// stolen commander is sent to its owner's command zone by its owner's
	// choice -- so Player is always the owner. A decline changes nothing: the
	// original zone change happens unchanged (the engine's park re-emits the
	// deferred event verbatim).
	KCommanderZone Kind = "commander_zone"
	// KChoose is one list-pick: choose between Min and Max of the offered
	// options. Every option in one decision shares a Kind that says what is
	// being chosen — "x" (a value for {X}; options ascend), "exile" (cards
	// to exile for Delve), "sacrifice" (a permanent to sacrifice as a cost),
	// "discard" (cards the active player's cleanup step discards down to the
	// maximum hand size, CR 514.1; Min == Max == len(hand) - maxHandSize over
	// exactly one option per hand card, in hand order), "search" (an ordered
	// subset of matching cards from a hidden library, with Min 0), "dig" (the
	// cards a Dig look-and-take moves from the top DigNum$ window to
	// DestinationZone$, Min 0 when Optional$ True else ChangeNum, one option
	// per ELIGIBLE card in library order), "name"/"type"/"number" (an "as this
	// enters" choice), "yes"/"no" (a may-cast such as Miracle), "keep" (the
	// CR 704.5j legend rule's survivor pick, posed from a state-based-action
	// pass: one "keep" option per same-named legendary permanent under the
	// asking controller, in battlefield order; the unchosen ones go to their
	// owners' graveyards), "dungeon" (an api:Venture first venture's CR
	// 701.49a dungeon pick: one option per dungeon token script the game can
	// enter, in token-key sort order, Label the dungeon's printed name), and
	// "room" (an api:Venture advance's CR 701.49b next-room pick: one option
	// per NextRoom$ arrow of the marker's current room, in the script's
	// printed arrow order, Label the room's printed RoomName$). The wire
	// shape is the same as every other decision; only the vocabulary
	// of Option.Kind is new.
	KChoose Kind = "choose"
	// KReplacement is a choice about applying a replacement effect. For CR
	// 616.1 competition it is Min == Max == 1 over the currently applicable
	// replacements, in deterministic scan order; the affected player chooses
	// which applies next, each option has Kind "replacement", and Obj names
	// its source permanent. The event is parked, and applicability is checked
	// again after each rewrite. A choice-valued mana replacement then uses five
	// Kind "mana" options labelled Add W/U/B/R/G while that ManaAdd remains
	// parked. BeginPhase competition uses the same replacement options; after
	// an Optional$ effect is selected, options "apply" and "decline" ask
	// whether it gets its opportunity. A decline continues through every
	// remaining applicable phase replacement before the StepChange is logged.
	KReplacement Kind = "replacement"
	// KArrange is the ordered-subset ask a library-arranging effect poses
	// (Ruling J0): the engine offers N cards, and the answer is an ordered
	// subset of them -- the one general decision shape Scry, Surveil and
	// Dig later share.
	//
	// The contract, stated once because a client that gets it backwards gets
	// it silently wrong: the engine offers N cards. The answer is an ordered
	// subset of them. The chosen indices, IN THE ORDER THE ANSWER GIVES THEM,
	// become pile A in that order. The options NOT chosen become pile B: in
	// the order they were OFFERED by default, or in the order the answer's
	// Rest gives them when it carries one (see Intent.Rest -- the pile-B
	// order, accepted by the asks that set Restable and validated by
	// validateRest). Min/Max bound pile A's size.
	// Every option in one KArrange decision shares an Option.Kind naming
	// pile B's destination -- "bottom", "graveyard", "exile", "hand" -- so
	// a rules-ignorant client can say "the ones you pick stay on top in the
	// order you pick them; the rest go to <destination>" without learning a
	// rule. The two all-to-bottom kinds -- "hideaway_bottom" (CR 702.75a)
	// and "dig_bottom" (Dig's default remainder) -- deviate: Min == Max ==
	// N, every offered card goes to the BOTTOM, and the ANSWER order is the
	// bottom order.
	//
	// DIRECTION, which is silent if a client gets it backwards (and which
	// KTriggerOrder's own comment phrases the same way): pile A index 0 is
	// the card that ends up CLOSEST TO THE TOP -- the next card drawn.
	//
	// The wire shape is exactly the one Validate already enforces for
	// KTriggerOrder: an ordered list of distinct in-range indices (a
	// permutation when Min == Max == N). Min/Max bound pile A's size; a
	// full order (RearrangeTopOfLibrary, Ponder) is Min == Max == N with
	// pile B empty, while a Scry-2 gives Min == Max == 1 over two options
	// with the unchosen one heading to pile B ("bottom"). No new wire
	// format is needed -- only the kind is new. The one option per offered
	// card carries that card in Obj, so pile A/B are rebuilt from the
	// answer and the option list without re-reading any zone.
	KArrange Kind = "arrange"
	// KStartingPlayer is CR 103.1's second half: the winner of the pre-game
	// toss CHOOSES which player takes the first turn, and that answer -- not
	// the raw toss draw -- is authoritative. It is Min == Max == 1 over one
	// option per living player, in turn order from the toss winner (so a
	// self-choice is the option whose Player equals the asking seat), each
	// option carrying its seat in Option.Player and its label. The Decision's
	// own Player is the toss winner, never the seat that ends up starting.
	// An absent or unanswerable host takes the deterministic R-9 fallback:
	// the toss winner names themselves, which is the pre-choice seat.
	KStartingPlayer Kind = "starting_player"
)

// Kinds lists every decision Kind in declaration order. It is the static
// universe a coverage report needs to say which kinds a run NEVER asked --
// an engine ask cannot use a kind outside this slice, so universe and
// observed cannot drift the way a hand-copied list in another package
// would. Keep it in the same order as the constants above.
var Kinds = []Kind{
	KPriority, KTarget, KAttackers, KBlockers, KMulligan, KModes,
	KTriggerOrder, KTriggerOptional, KCommanderZone, KChoose, KReplacement,
	KArrange, KStartingPlayer,
}

// Unless-pay option meanings are carried in Option.Mode so payment semantics
// do not depend on list position. They are used only on unless_pay decisions.
const (
	ModeUnlessPay     = "unless_pay"
	ModeUnlessDecline = "unless_decline"
)

// Option is one legal choice. Obj and Player are echoed only so a client can
// highlight the object; selection is by Index.
type Option struct {
	Index int         `json:"index"`
	Kind  string      `json:"kind"`
	Label string      `json:"label"`
	Obj   state.ObjID `json:"obj,omitempty"`
	// ManaSymbol is the exact mana symbol selected by a colour option. Labels
	// remain presentation-only; omitempty keeps unrelated options unchanged.
	ManaSymbol string `json:"mana_symbol,omitempty"`
	// Counter identifies the counter kind for wildcard counter-removal costs.
	// It is omitted for choices that do not select a counter kind.
	Counter string `json:"counter,omitempty"`
	// Player is always emitted because 0 is a valid seat (0-indexed), unlike
	// Obj where 0 means "no object".
	Player state.PlayerID `json:"player"`
	// Attacker tells a block option's client which attacker this blocker
	// would block, so a human can see the pairing an in-process bot already
	// can (the declare-blockers step is otherwise guessing). omitempty
	// mirrors Obj: an ObjID of 0 means "no object", so an option that has
	// no attacker (any non-block option) emits no field.
	Attacker state.ObjID `json:"attacker,omitempty"`
	// Battle, on an attacker option, names the non-player permanent being
	// attacked -- a CR 310.7 battle or a planeswalker -- 0 meaning a player
	// attack. Obj is the attacking creature and Player is the permanent's seat
	// (the battle's protector, or the planeswalker's controller), so the pair
	// alone cannot tell a permanent attack from a player attack by the same
	// creature; this field is what the engine reads back at declaration time to
	// record Object.AttackingBattle. omitempty: a player attack emits no field,
	// so every existing option list serialises byte-identically.
	Battle state.ObjID `json:"battle,omitempty"`
	// Required marks an attacker option whose creature MUST attack this
	// combat (CR 508.1d): a goaded creature (CR 701.38) or one under an
	// unconditional MustAttack static. A rules-ignorant client needs the
	// flag because the engine REJECTS a declaration that omits a required
	// creature it could have included (validateAttackDeclaration) -- an
	// omission that looks legal on the wire otherwise. omitempty: a
	// non-required option emits no field, so every existing option list
	// serialises byte-identically.
	Required bool `json:"required,omitempty"`
	// BlockMust records a MustBlock candidate even when another candidate
	// is highlighted as Required on the wire. A legal declaration can meet
	// the same maximum with a different blocker or attacker; the quota must
	// count that alternative too. Server-side only.
	BlockMust bool `json:"-"`
	// AttackMust marks a block option whose ATTACKER carries a CR 509.1c
	// requirement to be blocked if able ("CARDNAME must be blocked if
	// able."): at least one legal blocker must be declared against that
	// attacker when one exists. Unlike BlockMust -- which requires a
	// particular BLOCKER to block -- the requirement is satisfied by ANY one
	// of the options naming the attacker, so the whole-declaration solver
	// (decision.blockRequiredCore) counts it per attacker, not per option.
	// Server-side only, like BlockMust: a rules-ignorant client never needs
	// to enforce it, because the engine's validator rejects an answer that
	// fails the quota and Clamp/FitRequired repairs one that does.
	AttackMust bool `json:"-"`
	// MinBlockers/MaxBlockers are the CR 509.1a MinMaxBlocker bounds on the
	// ATTACKER this block option names (Min$ N: the attacker can be blocked
	// only by 0 or at least N creatures; Max$ N: by at most N; both set)
	// together for Min$ All, where the attacker must be blocked by every
	// legal blocker). They exist for the same reason Required does: the
	// engine REJECTS a whole-declaration count outside the bounds
	// (combat.ValidateMinMaxBlockers), so a rules-ignorant client -- the bot
	// policy included -- needs the bound on the wire to answer legally.
	// Both are omitted for an unbounded attacker, so every ordinary option
	// list serialises byte-identically.
	MinBlockers int `json:"min_blockers,omitempty"`
	MaxBlockers int `json:"max_blockers,omitempty"`
	// Controller is the server-side controller key for target options. It is
	// deliberately not serialized: TargetSameController uses it to make the
	// legal-answer rule available to the generic validator and bot repair.
	Controller state.PlayerID `json:"-"`
	// Group is an exclusivity marker: two options carrying the SAME non-empty
	// Group are mutually exclusive, and at most one of them may be selected
	// in a single answer. The whole contract is that sentence -- it says
	// nothing about blockers, creatures or combat, which is exactly so a
	// rules-ignorant client may enforce it without learning any rules. A
	// client that sees the player pick an option whose Group is already
	// represented in the picked set naturally REPLACES the previously picked
	// option from that group (moving a blocker from one attacker to another
	// should just work) rather than refusing the click. No two options of
	// one Group may be selected together, which Decision.Validate enforces as
	// a general rule.
	Group string `json:"group,omitempty"`
	// SetProps is the server-side, sorted set of canonical property tokens
	// this option contributes to a target-set constraint (Decision.SetPropMode).
	// "shared" requires every chosen option's set to have at least one token
	// in common with all the others; "distinct" requires the chosen options'
	// sets to be pairwise disjoint. The tokens are derived by rules from the
	// candidate's live characteristics (card types, creature types, mana
	// value, name, toughness), so a client never learns any rules. It is
	// never serialized and never read outside the set-constraint rule, so
	// every existing option list serialises byte-identically.
	SetProps []string `json:"-"`
	// AltCostIndex says which cost a "cast" option pays: 0 is the card's own
	// (RaiseCost/ReduceCost-adjusted) cost, i+1 is alternativeCosts(p, id)[i]
	// -- an AlternativeCost static's cost instead -- so a client can show
	// which of several costs the option pays. An "ability" option uses the
	// same field for the same purpose: 0 is the ability's printed Cost$, 1 is
	// its own AlternateCost$ rider (rules/activate.go's abilityAlternateCost,
	// the K:Equip expansion's fourth colon field), so an equip offering its
	// alternate cost is distinguishable from the printed-cost option without
	// parsing the label. omitempty mirrors Obj: an
	// option paying the card's own cost (the common case, and the default
	// every other Option literal in the tree relies on) carries no field, so
	// today's payloads are unchanged for it.
	AltCostIndex int `json:"alt_cost_index,omitempty"`
	// CostLife is a block option's non-mana life component (CR 509.1b): the
	// life the defender pays for declaring this block, beside Value's mana
	// (the MaxSum budget's currency). A rules-ignorant client sums it
	// against the defender's life total the same way. omitempty: an
	// uncharged option emits no field, so every ordinary option list
	// serialises byte-identically.
	CostLife int `json:"cost_life,omitempty"`
	// CostTaps is a block option's total tapXType obligation: how many
	// permanents declaring this block taps. The engine resolves the exact
	// permanents deterministically (rules' blockTapPlan), so a client that
	// cannot see the eligible pool -- the shipped bot policy included --
	// treats a positive value as an obligation it cannot verify and declines
	// the option rather than submit a declaration the validator may reject.
	// omitempty as CostLife.
	CostTaps int `json:"cost_taps,omitempty"`
	// CostPhyrexian is the number of Phyrexian pips a combat option's charge
	// carries ({W/P} and friends, CR 107.4f): each pip is payable with one
	// mana of its colour OR two life. The wire publishes the pip COUNT, not
	// the colour below it -- a rules-ignorant client cannot see whether the
	// colour branch is reachable, so it prices the life branch (two per pip)
	// against the acting player's life total, exactly as it prices CostLife.
	// omitempty as CostLife.
	CostPhyrexian int `json:"cost_phyrexian,omitempty"`
	// TapPoolCost is the declaration-dependent half of a tapXType obligation:
	// how many of the decision's published tap-candidate pool
	// (Decision.ChargeTapPool) this option's permanent would occupy if chosen.
	// A selected attacker that matches the obligation's spec is excluded from
	// the pool the engine plans the obligation against (Hollow Warrior's
	// tapXType<1/Creature.!attacking>), so a declaration can consume the very
	// candidates its own tap obligation needs. The per-option CostTaps alone
	// cannot express this -- the excluded candidate is the SELECTED creature,
	// not the charging one -- so the option publishes its own pool cost and
	// the shared rule (ChargeTapPoolFit) compares the pool the declaration
	// leaves with the obligations it owes. 0 (omitted) means this option does
	// not consume a published pool candidate, so every ordinary option list
	// serialises byte-identically.
	TapPoolCost int `json:"tap_pool_cost,omitempty"`
	// Mode distinguishes a "cast" option's payment kind: "" the card's own
	// cost, "kicked", "surged", "flashback", "miracle" -- what the engine
	// reads in beginCast's switch. A client renders a kicked/surged/
	// flashback/miracle cast differently from an ordinary one instead of
	// parsing the label for a keyword. omitempty: an ordinary cast (Mode "")
	// carries no field.
	Mode string `json:"mode,omitempty"`
	// MayPlayPerm names the may-play permission a "may-play" cast consumes
	// (rules/mayplay.go): a MayPlayText$-typed static's limit is once per turn
	// PER STATIC, so when one card matches several permissions (Muldrotha's
	// artifact creature) the offer must say which one it plays through. The
	// value is the rules-side key "<source-obj>:<MayPlayText>"; the empty
	// string is an untyped grant (the historical per-card limit). It is never
	// serialized -- the client answers by option index and the engine reads
	// the field back off its own stored option list (Submit's firstChosen) --
	// so every existing option list stays byte-identical on the wire.
	MayPlayPerm string `json:"-"`
	// Key is the server-side selection key for an option that names a thing
	// no ObjID can express: api:Venture's "dungeon" options (the token-script
	// key of the dungeon the answered first venture enters, CR 701.49a) and
	// its "room" options (the room key the answered advance moves the
	// venture marker to, CR 701.49b). The engine reads it back off its own
	// stored option list, never the wire -- a client answers by index and
	// renders Label (the dungeon's printed name, the room's printed
	// RoomName$). json:"-" keeps every existing option list serialising
	// byte-identically.
	Key string `json:"-"`
	// Amount is the X value an "x" choose option represents. The option's
	// Index is its position in the list, not its value (see rules/cast.go's
	// xAsk), so without this field a client could not tell "X = 4" from
	// "X = 1" without rereading the label. omitempty: only x options carry
	// it, and on an x option a missing field is exactly X = 0 (the one value
	// that omits), which the option's own label "X = 0" already shows.
	Amount int `json:"amount,omitempty"`
	// Ability anchors an "ability" option to its exact activated ability:
	// the index into the source Face().Abilities being offered (Task 10), so
	// a client can pop that ability's own text up beside the right ability
	// on the card. The engine reads it in beginActivation, where a stale
	// index degrades to a no-op. omitempty: options that are not ability
	// options carry no field, and on an ability option a missing field is
	// index 0 (the first ability), the one value that omits.
	Ability int `json:"ability,omitempty"`
	// SVar anchors a "granted" option (rules/speed.go, the kw:Start your
	// engines max-speed static's AddAbility$): the SVar name on the source
	// face whose AB the activation resolves through. A granted ability is
	// not a Face().Abilities index (the ordinary "ability" anchor), so it
	// carries the name instead; beginGrantedActivation re-resolves it, so a
	// stale name degrades to a no-op. omitempty: only granted options carry
	// it.
	SVar string `json:"svar,omitempty"`
	// Keyword anchors an "ability" option whose body a DERIVED keyword line
	// grants (CR 613.1f): a layer-6 `AddKeyword$ Cycling:1 U` /
	// `AddKeyword$ TypeCycling:Sliver:3` static (Tectonic Reformation,
	// Rhet-Tomb Mystic, Jo Grant, Homing Sliver) gives a hand card a cycling
	// ability no printed face carries, so neither the Ability index nor the
	// SVar name anchors it -- the option carries the keyword line itself
	// ("Cycling:1 U"), which beginActivation synthesizes the ability body
	// from, exactly the SVar-anchor shape with the line standing in for the
	// name. omitempty: only keyword-granted options carry it, so every
	// existing option list serialises byte-identically.
	Keyword string `json:"keyword,omitempty"`
	// Cost is the activation cost of a priority-window "activate" option whose
	// mana ability costs MORE than a bare tap, in the same whitespace-delimited
	// Forge notation AbilityCosts uses (rules/mana.go's formatCost over
	// ParseCost of the ability's Cost$ param) — "T Sac<1/Lion's Eye Diamond>"
	// for Lion's Eye Diamond, "T PayLife<1>" for Mana Confluence. It is the
	// wire marker the client's empty-priority-window floor and auto-pass need
	// (fb-20260917T192520Z-26136705): isActionKind excludes every "activate"
	// because a bare tap is offered at every window and is not a play, but a
	// costly activation is exactly the play a ritual-combo deck needs the
	// window for, and an empty hand leaves it the window's ONLY action — the
	// floor passed it away unseen, and the card was unreachable for the rest
	// of the game. A bare tap (every plain land) omits the field, so every
	// existing option list and every plain-land window serialises
	// byte-identically. omitempty: only a beyond-tap activation carries it.
	//
	// An "ability" option (a printed non-mana activated ability) carries its
	// offer-time cost here too, the same string the card's AbilityCosts
	// projects: its label is "<card name>: <description>" for every ability
	// of the card, and a planeswalker's description omits the loyalty cost,
	// so the cost is what lets the client's radial tell them apart. Every
	// Cost reader that means the mana-activation marker filters on
	// Kind == "activate".
	Cost string `json:"cost,omitempty"`
	// Grant is server-side only (json:"-") and present only on an "ability"
	// option whose whole activation is a PURE, IDEMPOTENT keyword grant (the
	// ability adds one or more keywords and nothing additive -- no
	// power/toughness change, no counters, no damage, no draw). It is what
	// lets the bot policy's no-op rule (A1) tell a keyword grant that can
	// gain nothing (already in effect, or an identical one already pending
	// from the same source) from an additive ability that genuinely stacks
	// and must stay freely repeatable. It is filled by rules/legal.go from
	// the engine's own derived-keyword facts and the stack -- a human
	// client never sees it, so it is never on the wire.
	Grant *Grant `json:"-"`
	// Attach is server-side only (json:"-"), set on a printed, gained or
	// granted "ability" option whose ability is an AB$ Attach -- K:Equip, Reconfigure and
	// Fortify expand to one (cards/keywords.go). It is what scopes the bot
	// policy's attachment no-op rule (A1, botpolicy.equipNoOp: an attached
	// source's re-attach, or an attach with no creature to land on) to the
	// abilities that rule is about: an Aura's or an equipment's OTHER
	// activated abilities (Holy Armor's pump, Flickerform's flicker) are not
	// re-attaches, and before this fact existed every one of them read as a
	// no-op and was never activated. Filled by rules/legal.go from the
	// compiled ability, never on the wire.
	Attach bool `json:"-"`
	// GrantSource is server-side only (json:"-") and names the object that
	// GRANTS an "ability" option's SVar body when that grantor differs from
	// the option's Obj (the ability's own source/recipient). It is set by
	// rules/legal.go's granted-ability offer loop from the granting static's
	// source so rules/speed.go's beginGrantedActivation resolves the body
	// from the grantor (events.GrantAbilityPush). Zero means no cross-object
	// grantor: the option is a printed ability or a self-grant, and the body
	// resolves from Obj. A human client never sees it.
	GrantSource state.ObjID `json:"-"`
	// GainedSource and GainedIdx are server-side only (json:"-") and anchor a
	// "has all abilities of" activation (Forge's GainsAbilitiesOf$): the
	// ability is a compiled SA on a FOREIGN card's face, so the option names
	// that card's object id and the index of the SA in its face's Abilities.
	// rules/activation resolves it and mints through events.GainedAbilityPush,
	// which carries the same pair so a replay re-resolves the identical SA. A
	// zero GainedSource means the option is not a gained ability (every
	// printed and SVar-granted ability). A human client never sees them.
	GainedSource state.ObjID `json:"-"`
	GainedIdx    int         `json:"-"`
	// GrantStatics is server-side only (json:"-") and carries, on an
	// "ability" option whose whole activation is an Effect granting
	// continuous statics to its target, the resolved Mode$ values of those
	// statics -- the same list the follow-up target decision publishes on
	// TargetEffect.Statics (describeTargetEffect). It is the OFFER-TIME
	// twin: the ability scorer reads it BEFORE the activation's costs are
	// paid, so a bot can decline a one-way boon grant (Whirler Rogue's
	// "target creature can't be blocked") when it has no own creature to
	// receive it -- the offer loop itself cannot withhold the ability (a
	// human seat may still want to aim a boon at an opponent's creature),
	// so the polarity is bot-quality advice, never an engine gate. Filled
	// by rules/legal.go from the ability's own face's SVar table, the same
	// resolution staticModesFromSVars performs for the target ask; a human
	// client never sees it.
	GrantStatics []string `json:"-"`
	// PlanBacked is server-side only (json:"-") and marks a "cast" option
	// the seat's auto-pay adapter built as a plan-backed candidate: selecting
	// it submits a decision.PaymentSelection whose V1 plan performs the mana
	// activations atomically, so the engine -- not the seat -- decides which
	// sources tap. The engine never reads it; it exists only so the cast
	// scorer can price policy features that ask what mana is LEFT after the
	// cast against producible mana (the offered untapped sources) rather
	// than the current pool, which a plan decision leaves empty. A human
	// client never sees it, and every option the ordinary (manual) policy is
	// offered leaves it false, so the manual arithmetic is byte-identical.
	PlanBacked bool `json:"-"`
	// Value is the option's price under a decision carrying a cumulative
	// budget (Decision.MaxSum): a Dig's WithTotalCMC$ cap sums the mana values
	// of the picked cards, so each offered card names its own mana value here
	// -- what lets Decision.Validate enforce "total mana value <= N" over the
	// chosen set without learning what a card is. Zero (mana value 0, or a
	// decision with no budget) omits the field, so every existing option list
	// serialises byte-identically.
	Value int `json:"value,omitempty"`
	// Value2 is the option's price under the decision's second cumulative
	// budget (Decision.MaxSum2).
	Value2 int `json:"value2,omitempty"`
}

// Grant describes the idempotent keyword grant of one "ability" option
// (decision.Option.Grant, server-side only). A nil Grant on an ability
// means the activation is NOT a pure keyword grant -- it has an additive
// component (a stat change, a counter, damage, a draw) or is not a keyword
// grant at all -- and such abilities always stack, so they are never a
// no-op. Only a non-nil Grant can be redundant, and it is redundant exactly
// when a granted keyword is already in effect on the granting permanent
// (Already) or an identical grant from the same source is already on the
// stack unresolved (Duplicate) -- the two independent halves of "does
// activating this again change anything".
type Grant struct {
	// Keywords are the keywords this activation adds, in the ability's own
	// KW$ order.
	Keywords []string
	// Already is true when the granting permanent already has every keyword
	// in Keywords -- the grant is already in effect from an earlier
	// resolution this turn, so activating it again changes nothing.
	Already bool
	// Duplicate is true when an identical activation from the same source
	// (same granted keywords) is already on the stack unresolved, so
	// resolving another copy would not add the keyword a second time.
	Duplicate bool
}

// TargetEffect describes only the active SA being targeted, not its parent,
// sub-abilities or the eventual outcome. API is the compiled primitive name
// (e.g. DealDamage, Destroy, Counter, Draw). Consumers must treat unfamiliar
// APIs conservatively; ChangeZone alone does not imply hostile removal.
// This contains no script text, hidden state or server continuation pointers.
type TargetEffect struct {
	API string `json:"api"`
	// Damage is present only for recognised direct damage primitives
	// (DealDamage and DamageAll). Absence is not proof that a whole spell's
	// other abilities cannot deal damage.
	Damage *DamageEffect `json:"damage,omitempty"`
	// Removal classifies the active SA's direct zone-removal shape. It is
	// absent for an unknown API, a non-removal destination, or a ChangeZone
	// whose destination this vocabulary does not model.
	Removal *RemovalEffect `json:"removal,omitempty"`
	// Statics names the resolved Mode$ values of the continuous statics an
	// Effect SA grants (its StaticAbilities$ SVar bodies). It is populated
	// only for API "Effect" whose granted bodies resolve to a readable
	// Mode$. The modes describe the static's SHAPE (CantBlockBy, Continuous,
	// MustAttack, ...), never a verdict: a consumer must classify polarity
	// itself, because the same mode can be a boon (CantBlockBy on one's own
	// creature) or a restriction (Continuous shrinking an opponent). Empty
	// when nothing resolves.
	Statics []string `json:"statics,omitempty"`
}

// RemovalEffect is a conservative classification of an active removal SA.
// It describes the scripted operation, not whether the target will actually
// leave at resolution (replacement effects, conditions and legality remain
// outside a targeting decision). Kind is one of destroy, sacrifice, exile,
// bounce, graveyard, library or command; Destination is populated for the
// ChangeZone family and repeats its normalized destination for clients that
// want the zone rather than the operation.
type RemovalEffect struct {
	Kind        string `json:"kind"`
	Destination string `json:"destination,omitempty"`
}

// DamageEffect describes nominal scripted damage, NEVER guaranteed damage.
// Prevention, replacement, conditions, division among targets and resolution
// legality are not evaluated. Spell damage is not commander combat damage.
type DamageEffect struct {
	// Amount is the nonnegative literal or context-resolved amount at the
	// point the target decision is posed, or nil (JSON null) if it is absent,
	// unresolvable, invalid or outside the supported range. X and SVar
	// expressions are evaluated when the announced/resolving context supplies
	// their value. A known zero is a non-nil pointer to 0. There is deliberately
	// no numeric default: Go consumers must check nil before dereferencing; wire
	// consumers must check null before arithmetic. This is not a lethal-damage
	// claim.
	Amount *int `json:"amount"`
}

// ClashResume is the immutable snapshot and cursor for CR 701.31's sequential owner choices.
type ClashResume struct {
	Players  []state.PlayerID
	Revealed []state.ObjID
	Winner   int
	Cursor   int
}

// WindowReason is a closed-vocabulary explanation for one withheld option.
type WindowReason struct {
	Obj    state.ObjID `json:"obj"`
	Kind   string      `json:"kind"`
	Reason string      `json:"reason"`
}

// Decision is the engine asking one player for one answer.
type Decision struct {
	Seq     uint64         `json:"seq"`
	Player  state.PlayerID `json:"player"`
	Kind    Kind           `json:"kind"`
	Prompt  string         `json:"prompt"`
	Min     int            `json:"min"`
	Max     int            `json:"max"`
	Options []Option       `json:"options"`
	// WindowReasons is an opt-in diagnostic sidecar: the first gate that
	// withheld each of this seat's candidates. Tokens only; never replay
	// input. Nil when disabled, preserving every existing decision's bytes.
	WindowReasons []WindowReason `json:"window_reasons,omitempty"`
	// PaymentActions is an additive, separately indexed cast-payment
	// extension. Keeping it outside Options preserves every legacy priority
	// choice index. It remains empty until payplan-04 publishes executable
	// offers.
	PaymentActions []PaymentAction `json:"payment_actions,omitempty"`
	// PaymentActionsBuilt distinguishes an unrequested extension from a built
	// empty extension. It is engine cache state, never part of the wire.
	PaymentActionsBuilt bool `json:"-"`
	// Redirected marks a decision whose answering seat (Player) was moved
	// off the seat it is asked OF by a CR 722 control redirect: a controlled
	// player's turn (api:ControlPlayer -- Mindslaver) or an opponent's
	// library search (Opposition Agent). Actor is then the seat the answer
	// acts FOR: its options were built for Actor, and the land drop, mana
	// ability, cast or search the answer selects is Actor's own (CR 722.1:
	// the controller makes the controlled player's choices; the controlled
	// player still takes the actions). Acting is the one reader. Engine
	// state, never part of the wire: the answering seat is still Player.
	Redirected bool           `json:"-"`
	Actor      state.PlayerID `json:"-"`
	// PaymentFallback is populated only if execution falls back to the normal
	// manual payment window.
	PaymentFallback *PaymentFallback `json:"payment_fallback,omitempty"`
	// ManaPayment is present only on the announced CR 601.2g window (the
	// "select mana" prompt, announce-then-pay spec §4.1): total cost, what is
	// still owed, the pool and the Auto-fill sources. Absent everywhere else,
	// so every other decision serialises byte-identically.
	ManaPayment *ManaPaymentWindow `json:"mana_payment,omitempty"`
	// MaxSum, when > 0, is a cumulative budget over the chosen options' Value
	// fields: the sum of the picked options' Value must not exceed MaxSum.
	// The engine's first user is a Dig's WithTotalCMC$ ("put any number of
	// nonland permanent cards with total mana value 4 or less from among
	// them"), which Option.Group's exclusivity cannot express -- a group says
	// "not both of these", a budget says "not all of these". Validate enforces
	// it as one more wire contract, so a rules-ignorant client can grey out an
	// unaffordable pick without summing anything itself. 0 (no budget) omits
	// the field, so every existing decision serialises byte-identically.
	MaxSum int `json:"maxSum,omitempty"`
	// Budgeted marks MaxSum as a PRESENT budget even when it is zero or
	// negative: a MaxSum of 0 alone reads as "no budget" (the omitempty
	// zero), which cannot express a total-power cap of 0 or less
	// (MaxTotalTargetPower$ <= 0, where negative-power options can offset a
	// positive one: powers 2,-1,-1 under a cap of 0 total 0). HasBudget is
	// the one reader; false (the zero) omits the field, so every existing
	// decision serialises byte-identically.
	Budgeted bool `json:"budgeted,omitempty"`
	// MaxSum2 and Budgeted2 are the independent second cumulative budget.
	// Budgeted2 preserves a present zero or negative cap, just as Budgeted does.
	MaxSum2   int  `json:"maxSum2,omitempty"`
	Budgeted2 bool `json:"budgeted2,omitempty"`
	// PayerLife is the acting player's life total, published as the bound on
	// a combat option's combined non-mana LIFE charge: the sum of the chosen
	// options' CostLife plus each CostPhyrexian pip (priced at two life,
	// CR 107.4f). It is the same kind of published rule input as MaxSum, and
	// it exists because the combined charge is a WHOLE-declaration property
	// the per-option list cannot express: a per-attacker state-based tax
	// (Norn's Annex) offers every (attacker, defender) pair payable on its
	// own, and only the sum overruns. Decision.requiredCore, RequiredQuota
	// and FitRequired derive the charge-feasible required set from this one
	// field, so the engine's declaration check and every client repair agree.
	// 0 (omitted) means "not published" -- a player at 0 life has lost and
	// cannot be asked, in the same way MaxSum 0 means "no budget" -- so
	// every decision without a combat charge serialises byte-identically.
	PayerLife int32 `json:"payer_life,omitempty"`
	// ChargeTapPool is the number of permanents eligible to pay the
	// declaration's tapXType obligation(s), measured BEFORE any attacker is
	// declared -- the pool the obligation draws from. The engine plans the
	// obligation with the declared attackers set aside, so a declaration that
	// commits every candidate leaves the obligation unpayable; publishing the
	// pool (with each option's Option.TapPoolCost) lets a rules-ignorant client
	// see that in advance and lets ChargeTapPoolFit reject or repair such a
	// declaration. It is published only when some offered pair carries a
	// tapXType obligation whose candidates are a single readable shape, so
	// every ordinary, tap-free declaration serialises byte-identically (0 =
	// omitted = not published).
	ChargeTapPool int `json:"charge_tap_pool,omitempty"`
	// MinSum is the mirror of MaxSum: a cumulative FLOOR over the chosen
	// options' Value fields -- the sum must REACH it, not stay under it. The
	// engine's first user is the tap-cost election of a withTotalPowerGE<N>
	// group predicate (Crew's "tap any number of other untapped creatures
	// you control with total power N or greater", Mossbridge Troll's):
	// Option.Value carries the candidate's current power, and the floor is
	// the whole "total power N or greater" clause. Validate enforces it as
	// one more wire contract, so a rules-ignorant client can grey out an
	// unaffordable pick without learning what power is; FitRequired's repair
	// derives the same floor from this field, never a second copy. Every
	// corpus floor is >= 1, so 0 unambiguously reads as "no floor" and omits
	// the field, keeping every existing decision byte-identical.
	MinSum int `json:"minSum,omitempty"`
	// GroupLimit caps how many options ONE Group may contribute to an answer:
	// the sum of the picked options sharing a Group must not exceed it. It is
	// the per-type pick count of Forge's EACH multi-type search grammar
	// ("EACH Forest & Plains" with ChangeNum$ 2 finds two Forests and two
	// Plainses), where one Group is one listed type and its cap is ChangeNum --
	// a cap the single-pick exclusivity rule cannot express. 0 or 1 reads as
	// the ordinary at-most-one-per-Group rule, so every existing decision
	// serialises byte-identically and validates unchanged. Validate is the
	// rule's one home; FitRequired and botpolicy's Clamp derive the same cap
	// from GroupCap, never a second copy.
	GroupLimit int `json:"groupLimit,omitempty"`
	// GroupLimits maps a Group name to its own selection cap, overriding the
	// decision-wide GroupLimit for that group. A per-defender attack ceiling
	// (AttackRestrict's MaxAttackers$ scoped by ValidDefender$) is exactly
	// this shape: each defended player is its own Group, and two such
	// restrictions can cap different defenders differently, which one scalar
	// GroupLimit cannot express. A group absent from the map, or mapped to a
	// value below 2, falls back to GroupCap. GroupCapFor is the one reader,
	// so Validate, FitRequired and botpolicy's repair cannot drift.
	GroupLimits map[string]int `json:"groupLimits,omitempty"`
	// Repeatable relaxes Validate's no-duplicate-index rule: when true the
	// SAME option index may be chosen more than once in one answer. It is
	// set only by a modal (Charm) decision whose SA carries
	// CanRepeatModes$ True -- CR 601.2b's "you may choose the same mode more
	// than once" -- where the option list is the distinct modes and the
	// answer is an ordered multiset of them. Every other decision keeps the
	// strict rule. omitempty: a non-repeatable decision carries no field, so
	// every existing payload serialises byte-identically.
	Repeatable bool `json:"repeatable,omitempty"`
	// AllowNone makes the EMPTY answer legal beside the Min..Max range: the
	// legal answer sizes are {0} and Min..Max. It is the "you may ... exactly
	// N" shape -- Forge's Exactly$ True search ("You may reveal exactly two
	// cards you own with different names", Extrapolate the Impossible) is all
	// or nothing, so Min == Max == N carries the "exactly" and AllowNone the
	// "you may". No Min/Max pair alone can say it: Min 0 would admit one card,
	// Min N would refuse the decline. Validate is the rule's one home; a
	// client that ignores the field only loses the decline (its Min..Max
	// answers stay legal). omitempty: every other decision serialises
	// byte-identically.
	AllowNone bool `json:"allow_none,omitempty"`
	// Source names the object this decision resolves for -- the spell whose
	// {X} is being chosen, the card whose "as it enters" choice is pending
	// -- so a prompt can always name its source (survey #18) without the
	// client guessing it from the option labels. omitempty mirrors Obj: an
	// ObjID of 0 means "no object", so decisions that do not resolve for a
	// specific object (priority, mulligan, trigger order) carry no field and
	// today's payloads are unchanged for them.
	Source state.ObjID `json:"source,omitempty"`
	// EffectOptional marks only a resolving api:Effect Triggers$ body's
	// OptionalDecider$ election. Unattended bots decline this shape; printed
	// optional triggers and Miracle retain their existing policy. This is
	// runtime-only policy context, not a new legal-answer or wire rule.
	EffectOptional bool `json:"-"`
	// CopyOfCopy marks a "copy_optional" may-copy election (effects/copy.go,
	// CopySpellAbility | Optional$ True) whose spell to be copied is ITSELF a
	// copy: the continuation of a chain (Chain of Smog's "that player may
	// copy this spell", Barroom Brawl's), not its first link. Unattended
	// bots decline it, so a chain the two bots would otherwise extend
	// forever ends after one hand-over. Runtime-only policy context, like
	// EffectOptional: not a legal-answer or wire rule.
	CopyOfCopy bool `json:"-"`
	// AffordableTargets is a cast-time target ask's engine-computed hint:
	// the largest number of targets whose total cost (a Strive spell's
	// per-extra-target additional cost, CR 702.52a) the caster can provably
	// pay from the pool and the payment window's fixed-production sources.
	// Zero means no hint (the count does not change the price). It never
	// narrows Max -- CR 601.2c lets the player choose any number, and the
	// cost is determined afterwards (601.2f) -- but an unattended bot that
	// fires the full width of such a spell only reverses it (CR 733.1).
	// Runtime-only policy context, like CopyOfCopy.
	AffordableTargets int `json:"-"`
	// TargetsWithSameController marks a target decision whose selected options
	// must all have one Controller. It is server-side metadata, so the wire
	// payload remains unchanged while Validate and bot repair share the rule.
	TargetsWithSameController bool `json:"-"`
	// SetPropMode carries Forge's target-set property constraint -- the
	// TargetsWithSameCardType$/SharedCardType-family (SetPropShared) and the
	// TargetsWithDifferentCMC$/Names family (SetPropDistinct) -- over each
	// option's SetProps. It is server-side metadata: the wire payload is
	// unchanged, and Decision.Validate, decision.FitRequired and botpolicy's
	// Clamp all derive the same rule from SetPropAdmits/SetPropMerge, so no
	// repair arm can re-implement a weaker copy.
	SetPropMode SetPropMode `json:"-"`
	// TargetEffect is host-independent targeting context. It is absent on
	// other decision kinds and on older servers; absent means unknown.
	TargetEffect *TargetEffect `json:"target_effect,omitempty"`
	// Restable marks a KArrange ask whose answer MAY carry Intent.Rest -- the
	// player-chosen order for pile B, the complement of the chosen set. Set
	// only by the scry/surveil ask (effLookAndArrange), whose pile B can be
	// non-empty AND observably ordered (CR 701.17's "in any order" for both
	// piles). An ask without the flag still ACCEPTS a well-formed Rest
	// (Validate's partition rule is kind-generic), but a client should not
	// send one it was not offered -- for a Min == Max == N ask the only valid
	// Rest is empty, so the flag is how a rules-ignorant client knows the
	// second list exists. omitempty: every decision a client sees today
	// serialises byte-identically.
	Restable bool `json:"restable,omitempty"`
	// ResumeKind, ResumeSA, ResumeModes, ResumeTarget, ResumeChoices and
	// ResumeRemembered are server-side only.
	// ResumeKind selects a cast/placement/resolution continuation ("cast_modes",
	// "modes", "unless_pay", "discard", "arrange", "search", "imprint",
	// "untap", "dig"); ResumeSA
	// names the exact sub-ability involved. ResumeModes maps a filtered cast-time
	// mode option back to its SVar name while keeping wire indices dense.
	// ResumeTarget is the index into the deterministic per-library target list:
	// re-entry applies the answer to exactly the library that asked, skips
	// targets already completed before suspension, and continues with later
	// libraries. rules alone selects these fields; clients never see them. Card data is shared immutable compiled corpus, so the SA
	// pointer is safe across Clone/replay.
	ResumeKind   string       `json:"-"`
	ResumeClash  *ClashResume `json:"-"`
	ResumeSA     *cards.SA    `json:"-"`
	ResumeModes  []string     `json:"-"`
	ResumeTarget int          `json:"-"`
	// Rolls is engine-internal context for the one KChoose that asks a
	// player to choose among ALREADY-ROLLED dice (effects/dice.go's
	// ChosenSVar$/OtherSVar$ shape, the Endeavor cycle): the per-die results
	// the asking first pass rolled, in roll order, so a rules-side resume
	// point can carry them across the suspension and publish chosen/other
	// without re-rolling (a re-roll would both re-draw the seeded generator
	// and make the choice answer a different question). Each "roll" option's
	// Index names a slot in this slice. Server-side only (json:"-"): a
	// replay re-derives the same rolls from the same seeded draws.
	Rolls []int32 `json:"-"`
	// ResumeChoices carries selections completed by earlier per-player choice
	// asks. It is runtime continuation state, never client input.
	ResumeChoices     []state.Target `json:"-"`
	ResumeChosenValid bool           `json:"-"`
	ResumeRemembered  []state.Target `json:"-"`
	// ResumeNumberPicks carries the numbers every chooser answered so far in a
	// multi-chooser secret ChooseNumber election (api:ChooseNumber's
	// MatchedAbility$/UnmatchedAbility$ shape, Expert-Level Safe), in chooser
	// order. It is the numeric sibling of ResumeChoices, which cannot hold a
	// bare number: the re-entered effect appends the answered pick and asks the
	// next chooser, exactly as effPlayerVote rides ResumeChoices across its
	// per-voter asks. Server-side only (json:"-"): runtime continuation state,
	// never client input, and a replay re-derives the same picks from the same
	// recorded intents.
	ResumeNumberPicks []int32 `json:"-"`
	// ResumeSearchKnown carries the effects.Ctx.SearchKnown set of an earlier
	// ask in the same search chain (effects/zone.go effSearchLibrary): the
	// library cards the chooser has already legitimately seen. A planted
	// placement leg poses a second ask after the first leg's own suspension
	// rebuilt a fresh Ctx, and without the ride the second leg would go blind
	// again. Server-side runtime continuation state, never client input --
	// the same class as ResumeRemembered.
	ResumeSearchKnown []state.Target `json:"-"`
	// ResumeForgetOtherSnapshot is the original eligibility set for a
	// multi-owner ChangeZone whose first move cleared remembered memory.
	ResumeForgetOtherSnapshot []state.Target   `json:"-"`
	ResumeForgetOtherOwners   []state.PlayerID `json:"-"`
	ResumeForgetOtherReady    bool             `json:"-"`
	ResumeForgetOtherCleared  bool             `json:"-"`
	// ResumeDigUntilMove carries an earlier OptionalFoundMove$ answer through
	// a nested DigUntil Aura-bearer ask. It is runtime continuation state only.
	// Empty until the election is answered.
	ResumeDigUntilMove string `json:"-"`
	// ResumeClonePick carries an earlier DB$ Clone Choices$ copy-source pick
	// through a later Optional$ may-copy ask in the same walk, so the answered
	// re-entry consumes the selection rather than posing the Choices$ ask
	// again. Runtime continuation state only.
	ResumeClonePick     state.ObjID `json:"-"`
	ResumeClonePickDone bool        `json:"-"`
	// ResumeTargetsUnique carries the TargetUnique$ accumulator of the
	// resolution that posed this ask (Ctx.TargetsUnique at suspension time):
	// the resume rebuilds a fresh Ctx, which without the ride loses every
	// earlier TargetUnique pick and a later rider in the same chain re-offers
	// them. Runtime continuation state, never client input, the same class
	// as ResumeRemembered.
	ResumeTargetsUnique []state.Target `json:"-"`
	// ResumeMoved carries the objects a ShuffleNonMandatory$ search's first
	// pass already moved (Path to Exile, Stoneforge Mystic): the may-shuffle
	// confirm suspends after the moves, and the re-entry's LibraryPosition$
	// placement needs the moved list the suspension lost. It is runtime
	// continuation state, never client input, the same class as
	// ResumeRemembered.
	ResumeMoved []state.ObjID `json:"-"`
	// ResumeDigPrimary carries a Dig's primary cards when its remainder's
	// ordered-bottom ask suspends after those cards were moved to a library.
	// The arrange handler needs this to place the primary pile on top after it
	// applies the remainder order; it is runtime continuation state, never
	// client input.
	ResumeDigPrimary []state.ObjID `json:"-"`
	// ResumeObjects carries an ASK's own immutable object snapshot when the
	// continuation must walk a list the answer can shrink out from under it.
	// Time Travel (Doctor Who) is the first user: its per-object election
	// offers add/remove/skip, so deriving the walk list from the decision's
	// options would record the asked object three times instead of the full
	// eligible set, and recomputing the set on re-entry would shift the
	// cursor when a removal drops an object. The effect sets it to the exact
	// list it is walking; rules stores it on the resume point and hands it
	// back on re-entry. Runtime continuation state, never client input, the
	// same class as ResumeMoved.
	ResumeObjects []state.ObjID `json:"-"`
	// ResumeRound carries a repeating continuation's completed-repetition
	// count beside ResumeTarget's index into that repetition's own list.
	// Time Travel (Doctor Who) is the first user: The Tenth Doctor's
	// Amount$ 3 runs the action three times, so the continuation must name
	// BOTH the repetition and the object. It is a field of its own rather
	// than a pair packed into ResumeTarget because `int` is 32 bits on a
	// 32-bit build, where a `(round << 32) | idx` packing both fails to
	// compile and loses the round. Runtime continuation state, never client
	// input, the same class as ResumeMoved.
	ResumeRound int `json:"-"`
	// ResumeRepeatNext is the completed-iteration cursor for RepeatOptional$.
	ResumeRepeatNext int32 `json:"-"`
	// ResumeUptoIdx/ResumeUptoCount ride an Upto$ Draw's in-flight per-target
	// state across a Dredge ask parked inside that target's answered batch
	// (Arcane Denial's "may draw up to two"): the re-entering upto branch
	// resumes exactly that target's remaining draws instead of re-asking a
	// decision already answered. Idx -1 (the default every non-upto caller
	// leaves) means no upto is in flight. Runtime continuation state, never
	// client input, the same class as ResumeMoved.
	ResumeUptoIdx   int   `json:"-"`
	ResumeUptoCount int32 `json:"-"`
	// ResumeExploreDone rides an api:Explore destination election: the
	// number of explores the pending explorer had completed before the one
	// that asked, so the resumed Num$ loop continues at that explore instead
	// of restarting its count (effects.Ctx.ExploreCount). Runtime
	// continuation state, never client input, the same class as
	// ResumeUptoCount.
	ResumeExploreDone int32 `json:"-"`
	// ResumeVillainousVictims and ResumeVillainousIndex carry the ordered
	// victim cursor for a multi-player VillainousChoice resolution.
	ResumeVillainousVictims []state.Target `json:"-"`
	ResumeVillainousIndex   int            `json:"-"`
	// ResumeGenericChoosers and ResumeGenericChooserIndex carry the ordered
	// Defined$ player cursor for a multi-player api:GenericChoice resolution:
	// each chooser answers the same Choices$ list in turn, with that chooser
	// bound as Ctx.Remembered.
	ResumeGenericChoosers     []state.Target `json:"-"`
	ResumeGenericChooserIndex int            `json:"-"`
}

// New is a convenience constructor that fills a Decision's Player, Kind,
// Prompt, Min, Max and Options fields from positionally-presented arguments.
// It also enforces Options[i].Index == i for the options it is handed -- the
// position/Index identity a client's intent and Chosen both rely on (a
// client names option i by choosing index i, and Chosen returns Options[i])
// -- so a call that passes a drifting list fails loudly here rather than on
// the path to a seat.
//
// Note that New is NOT the enforcement point for that identity: the engine's
// authoritative guard lives in rules' Engine.ask, through which every
// Decision that can reach a seat flows (casting a decision away from there
// leaves it not pending, so no seat is ever offered it). New's own check
// therefore backstops call sites that use it -- today only the mulligan
// round -- and is redundant there, not load-bearing. The great majority of
// construction sites build &decision.Decision struct literals directly and
// are covered by ask alone. New deliberately panics on a mis-indexed list
// rather than returning an error, and it preserves that behaviour as a
// convenience for its handful of callers, but the invariant is guarded
// regardless of which side of the constructor an option list arrives on.
// Source, if any, is set by the caller on the returned Decision; it carries
// no invariant.
func New(player state.PlayerID, kind Kind, prompt string, min, max int, options []Option) *Decision {
	for i := range options {
		if options[i].Index != i {
			panic(fmt.Sprintf("decision: option %d has Index %d, want position %d (%s)",
				i, options[i].Index, i, kind))
		}
	}
	return &Decision{Player: player, Kind: kind, Prompt: prompt, Min: min, Max: max, Options: options}
}

// HasBudget reports whether the decision carries a cumulative budget over
// its options' Value fields: MaxSum > 0 (every historical setter), or
// Budgeted with any MaxSum (a zero or negative cap). Validate, the bot's
// repair (FitRequired/Clamp) and every budget-aware policy arm read this one
// predicate, so what the engine enforces and what a client assembles cannot
// disagree about whether a budget exists.
func (d *Decision) HasBudget() bool { return d.MaxSum > 0 || d.Budgeted }

// HasBudget2 reports whether the decision carries its second cumulative budget.
func (d *Decision) HasBudget2() bool { return d.MaxSum2 > 0 || d.Budgeted2 }

// BudgetsFit reports whether a running total of sum under the first budget and
// sum2 under the second is within every budget the decision publishes. It is
// the one home of the per-currency admission rule: Validate, FitRequired's
// fold and the bot's Clamp top-up all read it, so a repaired answer can never
// be one Validate rejects (the dual-budget livelock).
func (d *Decision) BudgetsFit(sum, sum2 int) bool {
	return (!d.HasBudget() || sum <= d.MaxSum) &&
		(!d.HasBudget2() || sum2 <= d.MaxSum2)
}

// PayerLifeBound reports the decision's published non-mana charge bound: the
// acting player's life total when a combat charge is on the wire, or -1 when
// the decision published none (the bound is then inert, exactly as
// ChargeOptionConstraints treats an unpublished life). It is the ONE reader
// of PayerLife, so the required-core/quota rule and the repair cannot drift.
func (d *Decision) PayerLifeBound() int32 {
	if d.PayerLife > 0 {
		return d.PayerLife
	}
	return -1
}

// GroupCap is the effective per-Group selection cap the wire enforces:
// GroupLimit when it raises one, else the ordinary at-most-one-per-Group
// rule. The single reader behind Validate, FitRequired and botpolicy's
// repair paths, so the cap cannot drift between them.
func (d *Decision) GroupCap() int {
	if d.GroupLimit > 1 {
		return d.GroupLimit
	}
	return 1
}

// GroupCapFor is the effective cap for ONE named Group: GroupLimits[group]
// when it raises one, else the decision-wide GroupCap. It is the single
// reader every enforcement site uses, so a raised cap cannot be applied by
// Validate while a repair path still assumes the default.
func (d *Decision) GroupCapFor(group string) int {
	if n := d.GroupLimits[group]; n > 1 {
		return n
	}
	return d.GroupCap()
}

// GroupAdmits reports whether adding an option of Group g is legal given
// used[g] options of that group already chosen: used[g] < GroupCapFor(g).
// The ONE incremental per-Group rule -- Decision.Validate's group branch,
// groupCapExceeded's fast path and manabrew's orderTargetOptions prefix
// walks all call it, so a walk can never front a set the validator rejects
// (and the validator cannot drift from the walk). An empty g (an option
// with no Group) always admits.
func (d *Decision) GroupAdmits(used map[string]int, g string) bool {
	return g == "" || d.groupAdmitsCount(g, used[g])
}

// groupAdmitsCount is GroupAdmits over a count the caller already holds:
// adding one more option of the non-empty Group g, with n of them chosen,
// is legal when n < GroupCapFor(g). Validate and groupCapExceeded keep their
// per-Group counts in a groupTally rather than a map.
func (d *Decision) groupAdmitsCount(g string, n int) bool {
	return n < d.GroupCapFor(g)
}

// groupTally is a map-free per-Group count for one answer: the distinct
// non-empty Groups seen so far, each with its count and the first choice
// that selected it. Answers name a handful of groups, so a linear scan over
// a stack-backed slice beats hashing every Group string into a fresh map.
type groupTally struct {
	g            string
	first, count int
}

// groupTallyAt returns the index of g in t, or -1.
func groupTallyAt(t []groupTally, g string) int {
	for i := range t {
		if t[i].g == g {
			return i
		}
	}
	return -1
}

// choiceBits returns a zeroed map-free membership set over option indices
// [0, n), backed by buf for every decision up to 512 options (the caller's
// stack array), so Validate's duplicate and partition checks allocate
// nothing.
func choiceBits(buf *[8]uint64, n int) []uint64 {
	w := (n + 63) >> 6
	if w <= len(buf) {
		return buf[:w]
	}
	return make([]uint64, w)
}

// choiceBitTestAndSet reports whether i was already in bits, then adds it.
// i must be in [0, n) for the n bits was made for.
func choiceBitTestAndSet(bits []uint64, i int) bool {
	w, m := i>>6, uint64(1)<<(uint(i)&63)
	had := bits[w]&m != 0
	bits[w] |= m
	return had
}

// SetPropMode selects one of Forge's target-SET property constraints, read
// over each option's SetProps. The zero value is no constraint, so every
// existing decision is unaffected.
type SetPropMode string

const (
	// SetPropNone is the zero value: no set-property constraint.
	SetPropNone SetPropMode = ""
	// SetPropShared requires every selected option to share at least one
	// property token with every other selected option (Forge's
	// TargetsWithSameCardType$ / TargetsWithSameCreatureType$ /
	// TargetsWithEqualToughness$). With singleton token sets this is exact
	// equality; with multi-token sets it is a non-empty common intersection,
	// which is Forge's "share a card type" sense.
	SetPropShared SetPropMode = "shared"
	// SetPropDistinct requires the selected options' property sets to be
	// pairwise disjoint (Forge's TargetsWithDifferentCMC$ /
	// TargetsWithDifferentNames$: no two chosen cards share a value).
	SetPropDistinct SetPropMode = "distinct"
)

// SetPropAdmits reports whether adding an option whose tokens are add keeps
// the set constraint satisfied, given the running accumulator acc -- the
// intersection of every already-chosen set for SetPropShared, the union for
// SetPropDistinct. It is the ONE incremental rule Decision.Validate and
// botpolicy's repair both call, so a repair can never accept a set the
// validator rejects. An empty accumulator admits anything; an option with an
// empty token set can never join a shared set (it shares nothing), and is
// vacuously disjoint for a distinct one.
func SetPropAdmits(mode SetPropMode, acc, add []string) bool {
	switch mode {
	case SetPropShared:
		// nil acc means no option has been chosen yet: the first pick has no
		// pair to violate, so even a token-less set (a creature with no
		// creature type, Nameless Race) may stand alone. A non-nil empty acc
		// means a pick was made and the running intersection is empty, so no
		// further option can share with the picked set -- the nil/non-nil
		// distinction is what keeps the rule pairwise and order-independent.
		if acc == nil {
			return true
		}
		if len(acc) == 0 {
			return false
		}
		return setPropsIntersect(acc, add)
	case SetPropDistinct:
		if len(acc) == 0 {
			return true
		}
		return !setPropsIntersect(acc, add)
	default:
		return true
	}
}

// SetPropMerge folds a newly admitted option's tokens into the accumulator:
// intersection for SetPropShared, union for SetPropDistinct. The intersection
// form keeps the shared rule exact: an option joins only while the running
// common intersection stays non-empty.
func SetPropMerge(mode SetPropMode, acc, add []string) []string {
	switch mode {
	case SetPropShared:
		if acc == nil {
			if len(add) == 0 {
				// Non-nil empty records that a pick was made and shares
				// nothing with any later pick.
				return []string{}
			}
			return append([]string(nil), add...)
		}
		out := acc[:0:0]
		for _, t := range acc {
			if slices.Contains(add, t) {
				out = append(out, t)
			}
		}
		return out
	case SetPropDistinct:
		out := append(acc[:0:0], acc...)
		for _, t := range add {
			if !slices.Contains(out, t) {
				out = append(out, t)
			}
		}
		return out
	default:
		return acc
	}
}

// SetPropCapacity returns the largest number of options that can be selected
// together under the constraint, so an ask whose mandatory Min exceeds it can
// fizzle instead of posing an unsatisfiable decision (the same role
// sameControllerTargetBounds plays for TargetsWithSameController$). For
// SetPropShared it is the largest number of options sharing one token; for
// SetPropDistinct it is a greedy maximum of pairwise-disjoint options, exact
// whenever the token sets are singletons (every corpus DifferentCMC$/Names$
// carrier) and a safe lower bound otherwise.
func SetPropCapacity(mode SetPropMode, sets [][]string) int {
	switch mode {
	case SetPropShared:
		counts := map[string]int{}
		hasEmpty := false
		for _, set := range sets {
			if len(set) == 0 {
				hasEmpty = true
				continue
			}
			// Count OPTIONS per token, not token occurrences: a set may
			// repeat a token (an amassed Sliver Army token derives "Sliver"
			// from its script and again from amass's "it's also a Sliver"
			// grant), and one option must never count as two that share it.
			for i, t := range set {
				if !slices.Contains(set[:i], t) {
					counts[t]++
				}
			}
		}
		best := 0
		for _, n := range counts {
			if n > best {
				best = n
			}
		}
		// A token-less candidate can stand alone (one pick has no pair to
		// violate), so it still admits a set of size 1 even when no token is
		// shared by two candidates.
		if best == 0 && hasEmpty {
			return 1
		}
		return best
	case SetPropDistinct:
		var acc []string
		picked := 0
		for _, set := range sets {
			if !SetPropAdmits(SetPropDistinct, acc, set) {
				continue
			}
			acc = SetPropMerge(SetPropDistinct, acc, set)
			picked++
		}
		return picked
	default:
		return len(sets)
	}
}

// setPropsIntersect reports whether a and b share at least one token.
func setPropsIntersect(a, b []string) bool {
	for _, t := range a {
		if slices.Contains(b, t) {
			return true
		}
	}
	return false
}

// setPropAnswerAdmits reports whether the choices satisfy Decision.SetPropMode
// -- FitRequired's fast path uses it so an already-valid answer is returned
// unchanged only when it also satisfies the set constraint.
func (d *Decision) setPropAnswerAdmits(choices []int) bool {
	if d.SetPropMode == SetPropNone {
		return true
	}
	var acc []string
	for _, c := range choices {
		if c < 0 || c >= len(d.Options) {
			continue
		}
		add := d.Options[c].SetProps
		if !SetPropAdmits(d.SetPropMode, acc, add) {
			return false
		}
		acc = SetPropMerge(d.SetPropMode, acc, add)
	}
	return true
}

// setPropAccumulator folds the choices' SetProps into the running accumulator
// (no admissibility test), for seeding a repair's running state.
func (d *Decision) setPropAccumulator(choices []int) []string {
	return d.SetPropsOf(choices)
}

// SetPropsOf is the exported seed for a repair's running SetProps accumulator:
// the fold of the named choices' SetProps under Decision.SetPropMode. It is
// the one reader botpolicy's Clamp uses to prime its top-up state, so the
// repair starts from exactly the accumulator Validate would have built.
func (d *Decision) SetPropsOf(choices []int) []string {
	var acc []string
	for _, c := range choices {
		if c < 0 || c >= len(d.Options) {
			continue
		}
		acc = SetPropMerge(d.SetPropMode, acc, d.Options[c].SetProps)
	}
	return acc
}

// groupCapExceeded reports whether choices select more than GroupCapFor(g)
// options of any one Group -- the same per-Group rule Validate enforces, in
// the cheapest form FitRequired's fast path needs. A repeated index counts
// each occurrence, exactly as Validate's loop does. GroupAdmits is a PRE-add
// predicate, so the admission test runs BEFORE the increment (Validate's
// group branch tests against the count BEFORE this choice is folded in): a
// set holding exactly GroupCapFor(g) options of one group is legal.
func (d *Decision) groupCapExceeded(choices []int) bool {
	var buf [8]groupTally
	tally := buf[:0]
	for _, c := range choices {
		if c < 0 || c >= len(d.Options) {
			continue
		}
		g := d.Options[c].Group
		if g == "" {
			continue
		}
		i := groupTallyAt(tally, g)
		if i < 0 {
			i = len(tally)
			tally = append(tally, groupTally{g: g})
		}
		if !d.groupAdmitsCount(g, tally[i].count) {
			return true
		}
		tally[i].count++
	}
	return false
}

// Intent is a client's answer.
type Intent struct {
	Seq     uint64         `json:"seq"`
	Player  state.PlayerID `json:"player"`
	Choices []int          `json:"choices"`
	// Rest is the answer's order for the COMPLEMENT of Choices — the second
	// ordered list a KArrange answer may carry (the pile-B order): the options
	// the player did not choose, in the order the player wants them, where
	// "where they go" is still the decision's shared Option.Kind (bottom,
	// graveyard, ...). Absent or empty means the legacy contract: the
	// complement is taken in the order the options were OFFERED. The two lists
	// together must be a partition of the offered options -- Rest's indices
	// are in range, distinct, disjoint from Choices, and
	// len(Rest) == len(Options) - len(Choices) -- which Decision.Validate
	// enforces (one rule, one home: validateRest). Every non-arrange kind
	// rejects a non-empty Rest outright. omitempty: every intent a client
	// sends today serialises byte-identically, and a recorded intent's Rest
	// rides the log and replays exactly (Ruling P2 submits intents as
	// logged).
	Rest []int `json:"rest,omitempty"`
	// Payment selects an offered payment action and exact plan witness. It is
	// exclusive with Choices and Rest.
	Payment *PaymentSelection `json:"payment,omitempty"`
	// Announce begins an offered payment action's cast without a witness, so
	// the caster pays in the CR 601.2g window (announce-then-pay spec §3). It
	// is exclusive with Choices, Rest and Payment.
	Announce *AnnounceSelection `json:"announce,omitempty"`
}

// Acting is the seat the decision is asked OF and its answer acts for: the
// controlled seat when a CR 722 redirect moved Player to its controller,
// otherwise Player itself.
func (d *Decision) Acting() state.PlayerID {
	if d.Redirected {
		return d.Actor
	}
	return d.Player
}

// RedirectTo moves the answering seat to ctl, recording the seat the decision
// was asked of as Actor the first time (a search redirect followed by a
// player-control redirect keeps the ORIGINAL seat as the actor).
func (d *Decision) RedirectTo(ctl state.PlayerID) {
	if !d.Redirected {
		d.Redirected, d.Actor = true, d.Player
	}
	d.Player = ctl
}

// Validate rejects anything the engine did not offer. Everything a client can
// get wrong is caught here, which is what lets the client stay rules-ignorant.
func (d *Decision) Validate(in Intent) error {
	if in.Seq != d.Seq {
		return fmt.Errorf("intent seq %d, pending decision seq %d", in.Seq, d.Seq)
	}
	if in.Player != d.Player {
		return fmt.Errorf("intent from player %d, decision is for player %d", in.Player, d.Player)
	}
	if in.Announce != nil {
		return d.validateAnnounce(in)
	}
	if in.Payment != nil {
		return d.validatePayment(in)
	}
	if (len(in.Choices) < d.Min && !(d.AllowNone && len(in.Choices) == 0)) || len(in.Choices) > d.Max {
		if d.AllowNone {
			return fmt.Errorf("expected 0 or %d..%d choices, got %d", d.Min, d.Max, len(in.Choices))
		}
		return fmt.Errorf("expected %d..%d choices, got %d", d.Min, d.Max, len(in.Choices))
	}
	// Map-free bookkeeping (Validate runs per candidate answer in search):
	// a stack bitset for duplicate choices and a stack-backed groupTally for
	// the per-Group counts and each Group's first choice.
	var seenBuf [8]uint64
	var seen []uint64
	if !d.Repeatable {
		seen = choiceBits(&seenBuf, len(d.Options))
	}
	var tallyBuf [8]groupTally
	tally := tallyBuf[:0]
	var controller state.PlayerID
	haveController := false
	if d.TargetsWithSameController {
		for _, c := range in.Choices {
			if c < 0 || c >= len(d.Options) {
				continue
			}
			got := d.Options[c].Controller
			if !haveController {
				controller, haveController = got, true
			} else if got != controller {
				return fmt.Errorf("choices do not share one controller")
			}
		}
	}
	// The target-set property constraint (Decision.SetPropMode): the same
	// incremental rule (SetPropAdmits/SetPropMerge) botpolicy's repair uses,
	// so a repair can never return an answer Validate rejects. The accumulator
	// is the running intersection (shared) or union (distinct) of the chosen
	// options' SetProps.
	if d.SetPropMode != SetPropNone {
		var acc []string
		for _, c := range in.Choices {
			if c < 0 || c >= len(d.Options) {
				continue
			}
			add := d.Options[c].SetProps
			if !SetPropAdmits(d.SetPropMode, acc, add) {
				if d.SetPropMode == SetPropShared {
					return fmt.Errorf("choices do not all share a required property")
				}
				return fmt.Errorf("choices share a property the set requires to differ")
			}
			acc = SetPropMerge(d.SetPropMode, acc, add)
		}
	}
	for _, c := range in.Choices {
		if c < 0 || c >= len(d.Options) {
			return fmt.Errorf("choice %d out of range (%d options)", c, len(d.Options))
		}
		if !d.Repeatable && choiceBitTestAndSet(seen, c) {
			return fmt.Errorf("duplicate choice %d", c)
		}
		// The exclusivity rule: two options sharing one non-empty Group are
		// mutually exclusive, so an intent must not select both. This is a
		// general wire contract, not a combat rule -- the group field says
		// nothing about what its members are, only that they are exclusive.
		if g := d.Options[c].Group; g != "" {
			// The per-Group cap: at most GroupCapFor(g) options of one Group may
			// be selected together. At the default cap of 1 this is the historical
			// mutual-exclusion rule with its historical message; a raised cap
			// (EACH's per-type ChangeNum) reports the count it refused. The
			// incremental admission test is decision.GroupAdmits, the same rule
			// orderTargetOptions' prefix walks apply, so the offered prefix and
			// this fence cannot drift.
			i := groupTallyAt(tally, g)
			if i < 0 {
				i = len(tally)
				tally = append(tally, groupTally{g: g, first: c})
			}
			if !d.groupAdmitsCount(g, tally[i].count) {
				limit := d.GroupCapFor(g)
				if limit == 1 {
					return fmt.Errorf("choices %d and %d are mutually exclusive (group %q)", tally[i].first, c, g)
				}
				return fmt.Errorf("choice %d exceeds the per-group limit of %d (group %q)", c, limit, g)
			}
			tally[i].count++
		}
	}
	// The cumulative-budget rule (Decision.MaxSum): the chosen options'
	// Value fields sum to at most MaxSum. This is a general wire contract --
	// the field says nothing about cards or mana values, only that the picked
	// set's total price is capped -- so a client can enforce it without
	// learning any rules.
	if d.HasBudget() {
		sum := 0
		for _, c := range in.Choices {
			sum += d.Options[c].Value
		}
		if sum > d.MaxSum {
			return fmt.Errorf("choices total %d exceeds the budget %d", sum, d.MaxSum)
		}
	}
	if d.HasBudget2() {
		sum := 0
		for _, c := range in.Choices {
			sum += d.Options[c].Value2
		}
		if sum > d.MaxSum2 {
			return fmt.Errorf("choices total %d exceeds the second budget %d", sum, d.MaxSum2)
		}
	}
	// The cumulative-floor rule (Decision.MinSum): the chosen options'
	// Value fields sum to at least MinSum. The mirror of the budget above,
	// and the same general wire contract: the field says nothing about
	// creatures or power, only that the picked set's total must reach a
	// floor -- so a rules-ignorant client can enforce it without learning
	// any rules.
	if d.MinSum > 0 {
		sum := 0
		for _, c := range in.Choices {
			sum += d.Options[c].Value
		}
		if sum < d.MinSum {
			return fmt.Errorf("choices total %d is below the required sum %d", sum, d.MinSum)
		}
	}
	// The declaration-dependent tap rule (Decision.ChargeTapPool): a
	// declaration must leave the tapXType obligation a payable pool after the
	// attackers it commits are set aside. This is a general wire contract, not
	// a combat rule -- the field says nothing about creatures or attacking,
	// only that the picked set draws on one shared pool (ChargeTapPool) and
	// each pick occupies Option.TapPoolCost of it -- so a rules-ignorant client
	// can enforce it from the published fields alone. ChargeTapPoolFit is the
	// one home; the engine's board-aware validateAttackers reads it too.
	if !d.ChargeTapPoolFit(in.Choices) {
		return fmt.Errorf("choices %v exhaust the tap obligation's candidate pool (%d)", in.Choices, d.ChargeTapPool)
	}
	if len(in.Rest) > 0 {
		if d.Kind != KArrange {
			return fmt.Errorf("rest is only accepted on an arrange answer, not %s", d.Kind)
		}
		if err := d.validateRest(in.Choices, in.Rest); err != nil {
			return err
		}
	}
	return nil
}

// validateRest is the ONE home of the pile-B partition rule: on a KArrange
// answer, Rest must be exactly the ordered complement of Choices — the same
// rule Decision.Validate enforces and botpolicy.Clamp preserves (an arrange
// repair that changes the chosen set drops Rest rather than sending a
// partition Validate rejects). Rest's indices must be in range, distinct
// from each other and from every choice, and len(Rest) must make the two
// lists cover every offered option exactly once. seen carries the choices
// Validate has already registered (in range and, for a non-repeatable
// decision, distinct).
func (d *Decision) validateRest(choices, rest []int) error {
	if len(rest) != len(d.Options)-len(choices) {
		return fmt.Errorf("rest names %d of the %d unchosen options", len(rest), len(d.Options)-len(choices))
	}
	var seenBuf [8]uint64
	seen := choiceBits(&seenBuf, len(d.Options))
	for _, c := range choices {
		// An out-of-range choice can never collide with an in-range rest
		// index, so it needs no bit (Validate has rejected it already).
		if c >= 0 && c < len(d.Options) {
			choiceBitTestAndSet(seen, c)
		}
	}
	for _, r := range rest {
		if r < 0 || r >= len(d.Options) {
			return fmt.Errorf("rest choice %d out of range (%d options)", r, len(d.Options))
		}
		if choiceBitTestAndSet(seen, r) {
			return fmt.Errorf("rest choice %d is also chosen or repeated", r)
		}
	}
	return nil
}

// Chosen resolves an intent to options, in the order the client sent them.
// Returns nil if any index is out of range [0, len(d.Options)).
// Validate is the sanctioned path for validation; Chosen returns nil rather than
// panicking on indices it was not given a chance to validate.
func (d *Decision) Chosen(in Intent) []Option {
	// All-or-nothing: if ANY index is out of range, return nil
	for _, c := range in.Choices {
		if c < 0 || c >= len(d.Options) {
			return nil
		}
	}
	out := make([]Option, 0, len(in.Choices))
	for _, c := range in.Choices {
		out = append(out, d.Options[c])
	}
	return out
}

// ChosenRest resolves an arrange answer's Rest to options, in the order the
// client sent them — the player-chosen pile-B order. It returns nil when the
// intent carries no Rest or any index is out of range, the same all-or-nothing
// rule Chosen uses; a nil return sends the caller to the legacy offered-order
// complement. Validate is the sanctioned path for the partition rule.
func (d *Decision) ChosenRest(in Intent) []Option {
	if len(in.Rest) == 0 {
		return nil
	}
	for _, c := range in.Rest {
		if c < 0 || c >= len(d.Options) {
			return nil
		}
	}
	out := make([]Option, 0, len(in.Rest))
	for _, c := range in.Rest {
		out = append(out, d.Options[c])
	}
	return out
}

// PotentialAction is one action a seat COULD take if it first floated every
// mana its untapped sources could produce: the engine's own legal-offer walk
// (rules/legal.go) priced against a hypothetical pool instead of the floating
// one. It is the server-side answer to the float-then-cast payment model --
// the engine prices a cast against the FLOATING pool only, so the priority
// window carries no cast option yet, and a client that re-derives
// "castable after tapping" on its own (printed costs, no live modifiers,
// no command zone, no flashback) drifts from the engine on every cost rule
// (Thalia's RaiseCost, a Medallion's ReduceCost, an Indeterminate Tron
// source, an X spell at 0). The projection carries only what the client's
// stop decisions need: which kind of action and where its object lives.
// A card/ability id is NOT a promise the action is currently offered -- it is
// a promise the engine WOULD offer it once the mana floated.
type PotentialAction struct {
	// Kind is the action kind, the same vocabulary decision.Option uses but
	// restricted to real plays: "cast", "ability", "granted" (a max-speed
	// granted ability), "unlock" (a Room door), "turn_face_up" (the morph
	// family), "specialize", "play_land" and "station" -- every play kind the
	// priority offer walk emits (rules.potentialPlayKind). The mana tap
	// ("activate"), pass and concede are deliberately absent -- they are
	// offered at every priority window and are never a play.
	Kind string `json:"kind"`
	// Obj is the card or permanent the action names (the spell to cast from
	// hand/command zone/graveyard, or the source of the ability), 0 when the
	// action has no object. The id is the CardView id the client already has.
	Obj state.ObjID `json:"obj,omitempty"`
	// Ability anchors an "ability" potential action to its exact activated
	// ability, the index into the source Face().Abilities, exactly as
	// Option.Ability does. omitempty: casts and land drops carry no field.
	Ability int `json:"ability,omitempty"`
	// Mode distinguishes a "cast" potential action's payment kind ("",
	// "kicked", "surged", "flashback", "miracle"), and a "specialize" one's
	// target face index, exactly as Option.Mode does. omitempty: an ordinary
	// cast carries no field.
	Mode string `json:"mode,omitempty"`
	// Label is the offer label ("Cast X", "Name: ability text") -- the same
	// string the corresponding Option would carry, so a client can surface
	// the action without re-deriving it. omitempty: never empty in practice,
	// but a defensive omit keeps the wire free of empty strings.
	Label string `json:"label,omitempty"`
}
