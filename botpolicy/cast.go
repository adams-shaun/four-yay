package botpolicy

import (
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// Card is the casting half of the policy's picture of one object the
// deciding seat can legally see (its own hand, graveyard and battlefield):
// whether it is a creature, its derived power, its printed mana value, and
// whether it is a basic land. The facts are exactly the ones the casting
// rules below read, produced identically by BoardFromView (off the
// projected CardViews the seat receives) and BoardFromGame (off
// state.Game), so a card the policy ranks means the same thing whichever
// host asked. Every field below is read by a rule branch, so none is
// untested surface: the set is the ones chooseCast (Creature, Power,
// CMC), chooseLand (Basic), the ability ranking (AttachedTo) and the tap
// gate (Castable, ManaCost, Produces, and the creature facts it shares
// with chooseCast) actually branch on. The obligation that both halves
// fill every field identically is enforced, not aspirational: the
// adapter-parity tests (seat/integration_test.go's
// TestBotAdaptersAgreeOverWholeGame and
// TestBotAdaptersAgreeOverCommanderGame) compare the two halves' Card
// maps with maps.Equal -- every field of botpolicy.Card, on every intent
// of two whole games -- so a field one half fills and the other leaves
// zero is caught. A new Card field must be filled on both halves or not
// added at all.
//
// Power is the engine's derived power (ch.Power), which for a card in a
// hand or graveyard is its printed power — no continuous effect applies to
// a card that is not on the battlefield. Reading it the same way on both
// halves is what keeps the adapter pair in step; a fact one half fills and
// the other leaves zero would be a bot that casts differently depending on
// who asked.
type Card struct {
	Creature bool
	Power    int32
	CMC      int32
	Basic    bool
	// AttachedTo is the permanent this Aura or Equipment is currently
	// attached to, 0 when unattached (state.ObjID's own zero convention).
	// It comes straight from state.Object.AttachedTo (and the projected
	// CardView.AttachedTo, which is that same field) on the two adapter
	// halves, so A1's equip no-op detection (ability.go) judges a re-attach
	// identically whichever host asks. Only a battlefield permanent carries
	// a non-zero AttachedTo; a hand or graveyard card reads 0 on both
	// halves, which is what lets them stay a plain casting fact.
	AttachedTo state.ObjID
	// Activated is how many non-mana activated abilities this permanent was
	// activated for this turn (state.Object.ActivatedThisTurn via the
	// projected CardView.ActivatedThisTurn on the view half). A5's
	// repeatable-ability budget reads it: a source already activated this
	// many times is declined, which is what ends a tap-and-untap cycle
	// (Basalt Monolith's "{3}: Untap this artifact" re-enabling its own tap
	// forever) before the turn spins. Both halves fill it identically, so
	// the parity tests keep judging it.
	Activated int32
	// ManaCost is the printed cost in Forge notation ("1 W", "U U",
	// "X G") -- the same string both understanding halves already read for
	// CMC (CmcOf) and the View carries as CardView.ManaCost. The tap gate
	// (tap.go) re-parses it for the coloured-pip demands of the card it is
	// pricing, which is where a colour-blind tap policy would otherwise
	// float the wrong colour and stall against a multi-coloured cost.
	ManaCost string
	// Castable reports whether the deciding seat could cast this card from
	// where it currently sits, given enough mana: true for the seat's own
	// hand, for the command zone (this engine only ever puts commanders
	// there), and for the graveyard when the card has the Flashback
	// keyword; false for the battlefield (a permanent is already cast).
	// The tap gate reads it to restrict "a spell worth mana" to spells the
	// seat can actually cast -- a battlefield permanent never justifies a
	// tap. Both halves fill it from the same zone membership (the projected
	// zone lists; the engine's zone walk plus the derived keyword list), so
	// the gate sees the same castability whichever host asks.
	Castable bool
	// OnBattlefield reports whether this object is currently a permanent on
	// a battlefield (the deciding seat's own). It is the zone signal the
	// casting Card census carries but Castable does not: a graveyard card
	// is also not "castable" without Flashback, and a battlefield permanent
	// is never castable, so Castable alone cannot tell a live mana source
	// from a spent one. chooseLand (and the availability fold behind it)
	// reads OnBattlefield to know which cards are actually producing mana
	// on the battlefield, so a land drop is aimed at a colour the hand
	// still lacks rather than one a permanent already supplies. Both
	// adapter halves fill it from the same zone-membership source (the
	// projected battlefield list on the view half; the ZBattlefield zone
	// walk on the game half), so the two agree.
	OnBattlefield bool
	// InstantSpeed reports whether the card can be cast at instant speed:
	// it is an Instant, or it carries the Flash keyword (a permanent with
	// flash enters at instant speed, and an instant with flash is still an
	// instant). Both halves fill it from the same sources -- the projected
	// CardView.Types/Keywords on the view half, the face's Types and the
	// engine's derived keyword list on the game half -- so the mana reserve
	// (reserve()) prices the same hand whichever host asks. A battlefield
	// permanent that has neither is not instant-speed, which is what keeps
	// the reserve from counting a spell the seat has already cast.
	InstantSpeed bool
	// Produces is what this card's mana abilities add to the pool when a
	// tap-for-mana activation runs them (cards.ManaProduction, plain data
	// -- botpolicy must not import view or rules, Ruling F7). It is filled
	// by both adapter halves from the same source -- the projected
	// CardView.Produces on the view half, cards.Face.ManaProduction on the
	// game half -- so the tap heuristic reads the same production whichever
	// host asks. It lets chooseTap pick a source that produces a colour a cast
	// needs and spend the least flexible one first. It also describes a land
	// card while it is still in hand, and it is the fact the land-drop ranking
	// (chooseLand) reads to pick a land in the colour the hand still needs.
	Produces cards.ManaProduction
	// Counter reports whether this card's primary cast-shape ability (its
	// SP$ line) is a Counter -- i.e. casting it counters a spell. It is
	// filled by both adapter halves from the same printed face fact -- the
	// projected CardView.SpellAPI on the view half, the face's own
	// cards.Face.SpellAbility().API on the game half -- so a card is a
	// counter whichever host asked (pinned over a whole game by the
	// adapter-parity tests, non-vacuously over a counterspell-vs-creature
	// mirror). The casting rule (C8) reads it: a counter is only worth its
	// mana at a stack holding a FOREIGN spell, never at an own-spells-only
	// (or empty) stack, where casting it would spend mana to counter the
	// caster's own play. A card whose counter lives on an AB/trigger line
	// rather than its SP$ line (Mausoleum Wanderer) is NOT a counter here,
	// and is out of the cast-side rule's scope by design -- those route
	// through the ability/trigger paths, not chooseCast.
	Counter bool
	// Toughness is the engine's derived toughness (ch.Toughness), the
	// creature body's other half. It is a cast-scorer feature only -- the
	// CreatureToughness weight in castScore -- never part of the shared base
	// worth (cardWorth), so the discard and commander-zone paths keep
	// pricing a creature by power alone. Per this doc comment's parity
	// obligation above, both adapter halves fill it from the same derived
	// source -- the projected CardView.Toughness on the view half,
	// ch.Toughness(id) on the game half -- so the adapter-parity tests judge
	// it on every intent of a whole game.
	Toughness int32
	// Tapped reports whether this battlefield permanent is currently tapped
	// sideways. It exists for one reader: the cast scorer's CurveFit feature
	// counts the seat's producible mana over its UNTAPPED battlefield
	// sources (producibleMana), so a tapped source must not promise mana
	// this turn. Like every Card field it is filled identically on both
	// adapter halves -- cv.Tapped on the view half, o.Tapped on the game
	// half, the same object field both halves already read for Creature
	// facts -- and stays false for every card off the battlefield, which is
	// inert wherever the feature is not consulted.
	Tapped bool
	// Sick mirrors SummonSick from the public object/view for future
	// creature-source modelling. Basic land mana abilities remain usable
	// while their land is summoning-sick, so the target reserve does not read it.
	Sick bool
}

// braceForm normalises a brace-form mana cost ("{2}{U}{U}") to the
// space-separated form CmcOf parses. It is built once at package scope, not
// per call: a strings.Replacer is immutable and safe for concurrent use, and
// CmcOf runs on every card of every zone of every projected board — building
// the trie inside the function cost ~165MB of garbage per host test.
var braceForm = strings.NewReplacer("{", " ", "}", " ")

// CmcOf is the converted-mana-cost count a botpolicy.Card reads, a re-read
// of a card's Forge ManaCost string ("R", "U U", "1 BP BP", "X G", "no
// cost"). It deliberately re-derives rules/mana.go's ParseCost.CMC() by hand
// because botpolicy cannot import rules (Ruling F7); both adapter halves
// call this same exported function on the same printed field, so they can
// only agree. {X} counts as 0 (a printed X is 0 on the stack before a value
// is chosen — the engine's own CMC() agrees); ordinary hybrid, Phyrexian and
// colourless symbols count one, while a monocolour hybrid ({2/W}) counts its
// generic face (two). A brace-form cost ("{2}{U}{U}") is normalised to the
// space-separated form first.
func CmcOf(mc string) int32 {
	if n, ok := cmcOfPlain(mc); ok {
		return n
	}
	return cmcOfSlow(mc)
}

// cmcOfPlain is CmcOf's allocation-free path for the ordinary Forge
// spelling: pure ASCII, no braces. It splits on the ASCII whitespace
// strings.Fields/TrimSpace split on for such a string and prices each
// symbol with cmcSymbol, the slow path's own per-symbol rule, so the two
// agree on every such string (TestCmcOfPlainMatchesSlow). ok is false for
// anything else, which takes the slow path.
func cmcOfPlain(mc string) (int32, bool) {
	for i := 0; i < len(mc); i++ {
		if c := mc[i]; c >= 0x80 || c == '{' || c == '}' {
			return 0, false
		}
	}
	mc = strings.TrimSpace(mc)
	if mc == "" || strings.EqualFold(mc, "no cost") {
		return 0, true
	}
	var n int32
	for i := 0; i < len(mc); {
		for i < len(mc) && asciiSpace(mc[i]) {
			i++
		}
		j := i
		for j < len(mc) && !asciiSpace(mc[j]) {
			j++
		}
		if j > i {
			n += cmcSymbol(mc[i:j])
		}
		i = j
	}
	return n, true
}

func asciiSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\v' || c == '\f' || c == '\r'
}

// cmcSymbol prices one mana symbol the way CmcOf always has.
func cmcSymbol(sym string) int32 {
	if sym == "X" { // {X} is 0 off the stack
		return 0
	}
	if len(sym) == 1 && strings.ContainsRune("WUBRGC", rune(sym[0])) { // a single coloured/colourless pip
		return 1
	}
	// Atoi can only succeed on an optionally signed digit run; testing that
	// first spares the error allocation on every hybrid/Phyrexian symbol.
	if maybeInt(sym) {
		if v, err := strconv.Atoi(sym); err == nil && v >= 0 {
			return int32(v)
		}
	}
	if v, ok := twobridManaValue(sym); ok {
		return v
	}
	// Hybrid ("W/U"), Phyrexian ("UP"), and any other symbol: one generic.
	return 1
}

func maybeInt(s string) bool {
	if s != "" && (s[0] == '+' || s[0] == '-') {
		s = s[1:]
	}
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// cmcOfSlow is CmcOf's general path (brace form, non-ASCII input).
func cmcOfSlow(mc string) int32 {
	mc = braceForm.Replace(mc)
	mc = strings.TrimSpace(mc)
	if mc == "" || strings.EqualFold(mc, "no cost") {
		return 0
	}
	var n int32
	for sym := range strings.FieldsSeq(mc) {
		if sym == "X" { // {X} is 0 off the stack
			continue
		}
		if len(sym) == 1 && strings.ContainsRune("WUBRGC", rune(sym[0])) { // a single coloured/colourless pip
			n++
			continue
		}
		if v, err := strconv.Atoi(sym); err == nil && v >= 0 {
			n += int32(v)
			continue
		}
		if v, ok := twobridManaValue(sym); ok {
			n += v
			continue
		}
		// Hybrid ("W/U"), Phyrexian ("UP"), and any other symbol: one generic.
		n++
	}
	return n
}

// twobridManaValue recognises Forge's concatenated ("2W") and slash
// ("2/W") monocolour-hybrid spellings. It is kept alongside CmcOf because
// botpolicy cannot import cards or rules.
func twobridManaValue(sym string) (int32, bool) {
	generic, col := "", ""
	if left, right, ok := strings.Cut(sym, "/"); ok {
		generic, col = left, right
	} else {
		i := 0
		for i < len(sym) && sym[i] >= '0' && sym[i] <= '9' {
			i++
		}
		if i == 0 {
			return 0, false
		}
		generic, col = sym[:i], sym[i:]
	}
	if len(col) != 1 || !strings.ContainsRune("WUBRGC", rune(col[0])) || !maybeInt(generic) {
		return 0, false
	}
	v, err := strconv.ParseInt(generic, 10, 32)
	if err != nil || v < 0 {
		return 0, false
	}
	return int32(v), true
}

// hasTypeWord is botpolicy's type-membership test, re-expressed so the two
// adapter halves say the same thing without either importing view or rules:
// BoardFromView splits the projected CardView.Types string; BoardFromGame
// reads the face's own Types slice. "Basic" being present is how a land is
// a basic land (a basic Plains is "Types:Basic Land Plains"; a dual like
// Underground Sea is "Types:Land Island Swamp" — subtypes "Island Swamp"
// but no Basic, which is the whole point of the L1 rule).
func hasTypeWord(words []string, want string) bool {
	for _, w := range words {
		if strings.EqualFold(w, want) {
			return true
		}
	}
	return false
}

// CastWeights is the learned cast profile: the weights the cast scorer
// (cardWorth, castScore, chooseCast) dots its feature vector with. The
// default profile below reproduces the pre-refactor literal arithmetic
// EXACTLY — every C1–C8 rule and CR1 in cast.go's doc comments stays true
// of DefaultCastWeights — so a Board carrying it (or the zero value, see
// castWeights) plays the bot that has always played. Integer arithmetic
// only, fixed evaluation order, ties on option index.
type CastWeights struct {
	// CreatureBase is the per-creature base worth (C1); CreaturePower the
	// per-point-of-power term (C2). Both apply only to a creature option.
	// NonCreatureCMC is the per-mana-value term for every non-creature
	// option (C3).
	CreatureBase, CreaturePower, NonCreatureCMC int32
	// Kicked is the alternative-cost premium a kicked/surged cast earns
	// (C4); Flashback the one a flashback/miracle cast earns (C4).
	Kicked, Flashback int32
	// ReserveScale prices the C7 reserve preference: a cast that keeps the
	// pool at or above the reserve earns reserve*ReserveScale points.
	ReserveScale int32
	// CommanderTaxScale prices the CR1 command-zone recast penalty: a
	// command-zone cast scores minus CommanderTaxScale*Casts*CMC.
	CommanderTaxScale int32

	// CastThreshold is the hold threshold (C9): when the best surviving
	// cast option's final score is strictly below it, chooseCast returns
	// -1 and the whole priority rule falls through to the ability ranking
	// and the pass, exactly as when nothing was castable. This is what
	// makes the per-decision features (Precombat, OppCreatures,
	// OwnCreatures, LifeDelta) meaningful for a tuner: without a threshold
	// they add the same constant to every option's score and can only
	// reorder casts, never change WHETHER one is cast; with a threshold
	// they move the cast/hold boundary. A command-zone cast is NOT subject
	// to the threshold — CR1 prices it by value alone and its own
	// score-below-zero refusal is its only boundary, because the deck must
	// always be able to cast its commander.
	//
	// DefaultCastWeights sets it to MinInt32/2, a value no default-profile
	// score can approach, so the default bot never holds a cast it would
	// otherwise make (pinned by cast_weights_test.go's equivalence table).
	// The bound: with the default profile every context, toughness and
	// interaction weight is 0, so a candidate's score is cardWorth + mode
	// premium + reserve bonus (a non-commander cast), or the same minus the
	// CR1 tax with a score-below-zero refusal (a commander cast).
	// cardWorth is 30 + 4*Power for a creature — a hand card's Power is its
	// PRINTED power, no continuous effect applies off the battlefield,
	// measured over the compiled corpus at [-1, 20] — or a CMC in [0, 16]
	// (CmcOf only ever adds non-negative values). The premiums add at most
	// 6, the reserve bonus at most 5 * 16 = 80 (the reserve is the cheapest
	// instant-speed card's CMC, same corpus bound), so every default-profile
	// candidate score lies within [0, ~200] — roughly twelve orders of
	// magnitude above MinInt32/2 ≈ -1.07e9. A learned profile sets its own
	// threshold deliberately; see castWeights for the zero-value wrinkle
	// (an explicitly-zero threshold on an otherwise-set profile is ACTIVE).
	CastThreshold int32

	// Everything below is a NEW feature, weight 0 in the default profile —
	// the scorer reads them, the default bot does not change because of
	// them. A learned profile turns one on by setting its weight.

	// CreatureToughness adds a creature option's toughness to its score
	// (a bulkier body is worth more when power ties).
	CreatureToughness int32
	// CurveFit scores 1 for a cast whose cost exactly equals the mana the
	// seat can produce this turn (the pool plus its untapped mana sources'
	// guaranteed production), 0 otherwise — the curve-spent-exactly bonus.
	CurveFit int32
	// ManaLeft is the feature "pool total minus this cast's cost": a
	// positive weight prefers the cheaper cast, a negative one the pricier.
	// A plan-backed candidate (set by the auto-pay adapter) is priced
	// against producible mana instead of the empty pool, like C7.
	ManaLeft int32
	// Precombat scores 1 in the first main phase (Board.FirstMain).
	Precombat int32
	// InstantOnOwnTurn scores 1 for an instant-speed card cast in the seat's
	// OWN main phase (Board.MyTurn && Board.IsMain).
	InstantOnOwnTurn int32
	// OppCreatures / OwnCreatures are the public battlefield creature counts
	// on each side of the deciding seat, as one per-creature weight.
	OppCreatures, OwnCreatures int32
	// LifeDelta is the deciding seat's life minus the lowest opponent's.
	LifeDelta int32

	// Everything below is an L1b INTERACTION feature (C10): a per-option
	// conjunction of the card's own class (creature vs not, instant-speed
	// vs not) with a decision-level context, so a tuner can price a
	// creature differently from a one-shot in the same board instead of
	// only through the shared constants. All are weight 0 in the default
	// profile.

	// CreaturePrecombat scores 1 for a CREATURE option in the first main
	// phase — the creature-before-combat question, per option, where the
	// shared Precombat could only shift every option equally.
	CreaturePrecombat int32
	// CreatureOppCreatures multiplies a creature option's score by the
	// opponent creature count (a body matters more against a wide board).
	CreatureOppCreatures int32
	// NonCreatureOppCreatures multiplies a NON-creature option's score by
	// the opponent creature count — the removal proxy. The option's
	// EFFECT is still unread; the feature prices only the board pressure
	// the count carries, which is why it is a separate weight from
	// CreatureOppCreatures rather than one signed term.
	NonCreatureOppCreatures int32
	// CreatureLifeDelta multiplies a creature option's score by
	// LifeDelta (a body is the aggressive pick when the seat is ahead).
	CreatureLifeDelta int32
	// InstantSpeedOffTurnHold scores 1 for an instant-speed card cast in
	// the seat's OWN main phase — the same indicator InstantOnOwnTurn
	// reads, kept as a separate named dimension so a fitted profile's two
	// weights stay semantically legible (InstantOnOwnTurn is the "cast
	// instants now" premium; this is the paired "hold instants" term).
	// On its own a per-option term can only reorder casts; PAIRED WITH THE
	// C9 THRESHOLD it is what moves an instant-speed cast below the hold
	// boundary — a negative weight holds the instant in the seat's own
	// main phase while a creature cast (which earns no such term) stays
	// above the threshold and is still made.
	InstantSpeedOffTurnHold int32

	// The four window features below give a learned profile the timing
	// facts auto-pay made reachable (Board.Step, Board.MyTurn): under the
	// auto-pay adapter a planned instant-speed cast is offered at EVERY
	// priority window, so the scorer decides WHEN an instant is cast, and
	// these weights are the hold/cast boundaries per window (paired with
	// C9's CastThreshold, exactly as InstantSpeedOffTurnHold is). Each
	// scores 1 for an INSTANT-SPEED card in its window — the card's own
	// class, so a creature or sorcery is never reached — and all are
	// weight 0 in the default profile, so the default bot is byte-identical.

	// InstantOwnPreMain scores 1 for an instant-speed cast on the seat's
	// OWN turn before main 1: the upkeep, draw and untap steps
	// (Board.MyTurn && Board.Step in {untap, upkeep, draw}).
	InstantOwnPreMain int32
	// InstantOwnCombat scores 1 for an instant-speed cast in one of the
	// seat's OWN combat steps (begin-combat, declare-attackers,
	// declare-blockers, combat-damage, end-combat) — the "pump after
	// blocks" window.
	InstantOwnCombat int32
	// InstantOppTurn scores 1 for an instant-speed cast in ANY step of
	// another seat's turn (Board.MyTurn false).
	InstantOppTurn int32
	// InstantOppEnd scores 1 for an instant-speed cast in ANOTHER seat's
	// end step (Board.MyTurn false && Board.Step == end) — the classic
	// "use the mana at end of turn" window. It is a subset of
	// InstantOppTurn, so a profile can read the two together or price the
	// end step apart from the rest of the opponent's turn.
	InstantOppEnd int32

	// SetValue is the L1c within-turn mana-efficiency feature (C11): it
	// prices the FOLLOW-UP a cast leaves behind. For each offered cast
	// option o the scorer computes the best total castScore obtainable this
	// turn by casting o first and then a best affordable subset of the
	// OTHER offered cast options with the mana that would remain (the
	// seat's producible mana minus o's cast cost, priced with the same
	// producibleMana/castCost helpers the other features read), and adds
	// SetValue*(that total)/8 to o's score. A positive weight therefore
	// prefers the cast that leaves the best continuation — two 2-drops over
	// one 3-drop on four mana — where the greedy best-first rule alone is
	// blind to it. The subset search is bounded and deterministic: the
	// candidate cards are deduped by object id and sorted by object id, up
	// to setSubsetMaxCards are searched exhaustively (2^10 = 1024 subsets)
	// and beyond that cap a greedy-by-score pass (ties on object id) picks
	// the follow-ups, so no map iteration order can reach the choice. The
	// total is an int32 sum of the same castScore values the main loop
	// ranks with.
	//
	// Weight 0 in the default profile: the feature is not even evaluated,
	// so every default pick is byte-identical to the pre-L1c arithmetic.
	SetValue int32
}

// DefaultCastWeights is the pre-refactor arithmetic, weight for weight:
// creature 30 + 4*Power (C1/C2), non-creature its mana value (C3), kicked
// +6 and flashback +4 (C4), the reserve preference at 5 points per reserved
// mana (C7, the old reserveBonusScale) and the command-zone tax at 5 per
// prior cast per mana value (CR1, the old cmdrTaxScale). Every weight for
// the new features is 0, so the default pick is byte-identical to the
// literal pre-refactor rule (pinned by cast_weights_test.go's equivalence
// table over every cast_test.go case). The one non-zero addition is the
// CastThreshold hold boundary at MinInt32/2 — see the field's comment for
// the bound proving no default-profile score can ever fall below it.
var DefaultCastWeights = CastWeights{
	CreatureBase:      30,
	CreaturePower:     4,
	NonCreatureCMC:    1,
	Kicked:            6,
	Flashback:         4,
	ReserveScale:      5,
	CommanderTaxScale: 5,
	CastThreshold:     math.MinInt32 / 2,
}

// castWeights returns the Board's cast profile. The zero value is treated
// as DefaultCastWeights: a Board nobody configured (every existing test
// and adapter path) must keep playing the pre-refactor bot, and a learned
// profile is always set explicitly field by field — an all-zero profile
// ("never score a cast at all") is not expressible, which is the documented
// trade of this choice. The same trade reaches CastThreshold: because the
// zero STRUCT is the "unset" marker, a learned profile that sets other
// fields but leaves CastThreshold 0 gets an ACTIVE threshold at 0 (every
// non-commander cast scoring below zero is held) — a profile that wants no
// boundary at all must set CastThreshold to MinInt32/2 itself, matching
// DefaultCastWeights. The adapters do not fill Board.Cast at all (it is
// configuration, not board state), so the zero-value rule is also what the
// BoardFromGameInto reuse contract needs: a profile set once on a reused
// Board survives every refill untouched.
func (b Board) castWeights() CastWeights {
	if b.Cast == (CastWeights{}) {
		return DefaultCastWeights
	}
	return b.Cast
}

// castScore ranks one "cast" option. It is the one function whichever
// chooseCast reads, so the C-rules below are exactly this arithmetic, and
// a mutation that takes the first "cast" option, or treats every cast the
// same, changes it:
//
//   - a creature scores 30 + 4*Power, which outranks every non-creature
//     this policy can read (a non-creature scores its mana value, a
//     realistic ceiling of a handful all the while a 1/1 sits at 34) — the
//     C1 preference that the damage engine is worth more than a one-shot
//     — and higher power outranks lower (C2);
//   - a non-creature scores its mana value, so the biggest spell the bot
//     can afford wins the "which one-shot" question (C3);
//   - an alternative-cost cast (Mode kicked/surged vs flashback/miracle)
//     is never scored as an ordinary cast (C4): each adds its own small
//     premium, because a kicked/surge larger effect and a flashback/
//     miracle second use are both net-upside casts the bot is committing
//     to anyway — but the premium is tiny so it only ever decides an
//     otherwise-tied pair of the same-strength cards, not the card class
//     preference above.
//
// A cast whose Obj is in no zone the board reads cards for scores as a
// non-creature of mana value 0 (C5): it can only win against another
// zero-fact card, and never beats a real read.
func (b Board) castScore(o decision.Option) int32 {
	w := b.castWeights()
	s := b.cardWorth(o.Obj)
	// CreatureToughness is a cast-scorer feature, not part of the shared
	// base worth (cardWorth): the discard and commander-zone paths price a
	// creature by its power alone, the cast path may price the body.
	if b.Cards[o.Obj].Creature {
		s += w.CreatureToughness * b.Cards[o.Obj].Toughness
	}
	switch o.Mode {
	// The and/or Kicker's per-part modes are kicked casts too (each a
	// net-upside optional additional cost the bot commits to when scored).
	case "kicked", "kicked1", "kicked2", "kickedboth", "surged":
		s += w.Kicked
	case "flashback", "miracle", "aftermath":
		s += w.Flashback
	}
	return s
}

// cardWorth prices cast desirability, not the usefulness of keeping a hand card.
// It is the shared base-worth feature dot — CreatureBase+CreaturePower*Power
// for a creature (C1/C2), NonCreatureCMC*CMC for everything else (C3) — read
// by the cast scorer AND the paths that only want a card's standing worth
// (the discard bottoming, the commander-zone leave ask). Toughness is NOT a
// base-worth feature: it belongs to the cast scorer alone (castScore).
func (b Board) cardWorth(id state.ObjID) int32 {
	w := b.castWeights()
	c := b.Cards[id]
	if c.Creature {
		return w.CreatureBase + w.CreaturePower*c.Power
	}
	return w.NonCreatureCMC * c.CMC
}

// commandTax is the CR1 recast penalty: CommanderTaxScale*Casts*CMC — the
// mana-value-scaled recast penalty shared with commander_zone (policy.go's
// KCommanderZone branch), now priced by the Board's cast profile.
func (b Board) commandTax(id state.ObjID) int32 {
	return b.castWeights().CommanderTaxScale * b.Commanders[id].Casts * b.Cards[id].CMC
}

// chooseCast is the KPriority cast ranking: it picks ONE of the offered
// "cast" options, or returns -1 when none is offered. The rules, each
// stated for what it reads:
//
//   - C1 (board over one-shots): a creature outranks a non-creature. A
//     creature is the only card type that attacks, and therefore deals
//     damage, every turn; casting one adds a permanent source of damage —
//     the fast path to ending the game. A one-shot (burn/removal) removes
//     one thing or deals damage once and is done. With the effect unread,
//     a creature is the choice that moves the game toward a finish.
//   - C2 (more imminent damage): among creatures, the higher power wins —
//     the more damage it will deal the next time it attacks, and every
//     time after.
//   - C3 (bigger effect): among non-creatures, the higher mana value wins
//     — the printed cost the bot is spending, its best guess at the bigger
//     effect when it cannot read effects.
//   - C4 (no alternative cost is an ordinary cast): a kicked/surged/
//     flashback/miracle "cast" option scores apart from an ordinary one
//     (see castScore), read off Option.Mode never the label.
//   - C5 (unreadable): an option whose Obj carries no board card facts
//     scores as a non-creature of mana value 0, a low rank, never a crash.
//   - CR1 (the command zone is taxed, CR 903.8): a "cast" whose Obj is a
//     commander currently sitting in its owner's command zone ranks as its
//     ordinary castScore minus CommanderTaxScale*Casts*CMC — the tax priced on
//     the commander's own mana value (see the commandTax comment), and
//     an option scoring below zero is NOT cast at all. The tax is per
//     prior cast, so the bot casts its commander while the recast is
//     still worth the mana it costs — a cheap commander is recast many
//     times (a 2/2, CMC 2, scores 38, 28, 18, 8 through its fourth cast
//     and refuses only the fifth), an expensive one is abandoned early (a
//     12/12, CMC 12, scores 78, 18 and refuses its third), and a
//     commander that keeps dying is not re-recruited forever. A commander
//     cast from the HAND scores as an ordinary card — NoTax (a hand cast
//     neither costs {2} nor counts), which is the whole reason the rule
//     is gated on InCommandZone and not on "is a commander". This price
//     is a value judgment on what the recast costs, NOT an affordability
//     check: the engine already refuses what the pool cannot pay
//     (rules/cast.go's commanderTaxFor gates the offer), so this is purely
//     "is the recast still worth bothering with". A command-zone cast is
//     also NOT subject to C9's hold threshold: CR1 prices it by value
//     alone and its own score-below-zero refusal is its only boundary,
//     because the deck must always be able to cast its commander.
//   - C9 (the hold threshold): after the loop, when the best surviving
//     option's final score is strictly below CastThreshold AND that best
//     option is NOT a command-zone cast, chooseCast returns -1 and the
//     priority rule (policy.go) falls through to the ability ranking and
//     the pass exactly as it does when nothing was castable. The
//     exemption is per WINNER, not per candidate: a command-zone cast is
//     exempt only while it is itself the best option — if a hand card
//     outscores it, the hand card is the best option and the threshold
//     applies to the whole decision. The default profile's threshold is
//     MinInt32/2, unreachable by the bound in the field's comment, so the
//     default bot never holds.
//   - C10 (interaction features): five per-option conjunctions of the
//     card's class with the decision context — CreaturePrecombat
//     (creature × FirstMain), CreatureOppCreatures and
//     NonCreatureOppCreatures (each class × the opponent creature count,
//     the latter the removal proxy), CreatureLifeDelta (creature ×
//     LifeDelta) and InstantSpeedOffTurnHold (instant-speed × the seat's
//     own main phase, the hold term that pairs with C9). All are weight 0
//     in the default profile, so the default arithmetic is unchanged.
//   - C6 (deterministic tie): ties break on option index, so no map
//     iteration order reaches the answer.
//   - C7 (the mana reserve, B2): the bot keeps the mana pool at or above the
//     cost of the cheapest instant-speed card it holds (an Instant, or a
//     permanent with Flash; reserve() is 0 when it holds none, making C7
//     inert) by PREFERRING a cast that leaves that reserve over a
//     comparable one that empties it -- the reserve is a score bonus a
//     reserve-keeping cast earns, so among equivalent cards the bot holds
//     mana for an instant-speed play, but a clearly better cast (a strong
//     creature, an unprotected commander) is still made even if it spends
//     the reserve. A hard block here made the bot decline casting its good
//     hands and sit on mana the phase threw away, so C7 is deliberately a
//     preference, not a refusal. A command-zone commander cast is priced by
//     value alone, never held for the reserve. The pool this rule reads is
//     the current pool for the ordinary manual decision, but producible
//     mana (the offered untapped sources) for a plan-backed candidate
//     (decision.Option.PlanBacked, set by the auto-pay adapter), because a
//     V1 plan is offered only on an empty pool and the pool reading would
//     otherwise make C7 inert under auto-pay.
//   - C8 (a counter needs a foreign spell): a "cast" option whose Card is a
//     counter (Card.Counter — its SP$ ability is a Counter) is not cast at
//     all when the Board's stack census shows NO foreign spell — every
//     stack spell is the deciding seat's own, or the stack is empty.
//     Casting a counter then can only target the caster's own spell (the
//     commit point is HERE, before the mana is spent: once cast, the
//     target ask's Min-1 leaves no exit), so the wasted self-counter shape
//     — the bot spending {U}{U} to nuke its own Brainstorm — is refused
//     outright. A counter cast at a stack holding a foreign spell keeps
//     its ordinary score: countering an opponent's spell is exactly the
//     behaviour this rule exists to preserve, and in multiplayer a stack
//     spell by any other seat is foreign, so four-seat play keeps every
//     real target. The census is b.Stack, an ordered slice filled
//     identically by both adapter halves, so no map iteration order can
//     reach the choice.
//
// Like the target and combat branches it consumes no rng: the pick is a
// pure function of the offered options and the board facts.
func (b Board) chooseCast(d *decision.Decision) int {
	w := b.castWeights()
	best := -1
	var bestScore int32 = -1
	bestInCmd := false
	// C7 (the mana reserve, B2): the reserve is the cost of the cheapest
	// instant-speed card the seat holds (0 when it holds none, in which case
	// C7 is inert). It is a PREFERENCE, not a hard block: a cast that would
	// leave the pool at or above the reserve scores reserve points higher, so
	// among comparable cards the bot keeps mana for an instant-speed play
	// rather than emptying the pool -- but a cast that is clearly better
	// (a strong creature, an unprotected commander) still gets made even if
	// it spends the reserve, because refusing the seat's best play to hoard
	// mana is not the point (and is exactly what a deck that never casts its
	// commander does). A hard block, by contrast, made the bot decline
	// casting its good hands and sat passively on mana the phase threw away.
	res := b.reserve()
	// C8's census: is there a foreign spell on the stack for a counter to
	// eat? ForeignSpell is its one home, read in b.Stack's own order.
	foreignSpell := b.ForeignSpell(d.Player)
	ctx := b.castContextFor(d)
	// C11's candidate table: every cast option that survives C8, deduped by
	// object id (a card offered under several cast modes counts once, at
	// its best castScore) and sorted by object id. Built ONCE per decision
	// so the subset search below is independent of option order; only read
	// when SetValue is non-zero, so the default profile pays nothing.
	var entries []castEntry
	if w.SetValue != 0 {
		entries = b.castEntries(d, foreignSpell)
	}
	for _, o := range d.Options {
		if o.Kind != "cast" {
			continue
		}
		// C8: a counter with no foreign spell to counter is not cast at all,
		// whatever it would otherwise score — the wasted self-counter is
		// refused at the commit point, before the mana is ever spent.
		if b.Cards[o.Obj].Counter && !foreignSpell {
			continue
		}
		// CR1: price the command-zone tax on the commander's mana value, not
		// a flat power-equivalent, so a costly commander is abandoned before
		// a cheap one. CMC 0 (an unreadable commander, should not happen)
		// prices the tax at 0 and so casts at its base score (C5's own
		// degenerate shape); a real commander always carries CMC on both
		// adapter halves (combat.go's census fills it for the command zone,
		// and seat/bot.go's BoardFromView the same). A command-zone cast is
		// also never held for the reserve's sake (the deck must be able to
		// cast its commander), so it is priced by value alone.
		inCmd := b.Commanders[o.Obj].InCommandZone
		s := b.castScore(o)
		// The context features, dotted with their weights. With the default
		// profile every weight here is 0, so the default bot's pick is the
		// pre-refactor arithmetic to the digit (pinned by cast_weights_test.go).
		// The constants (Precombat, OppCreatures, OwnCreatures, LifeDelta) add
		// the same term to every surviving option — which is exactly why they
		// CAN flip a CR1-priced-out commander cast back above zero without ever
		// reordering an otherwise-tied pair; the per-option ones (CurveFit,
		// ManaLeft, InstantOnOwnTurn) separate casts the base worth ties. With
		// a C9 threshold set, the constants become meaningful in a second way:
		// they move the cast/hold boundary instead of only reordering casts.
		cost := b.castCost(o.Obj, b.Cards[o.Obj])
		// The mana-side features below ask what mana is LEFT after this cast
		// (ManaLeft) or whether it keeps the C7 reserve. On a plan-backed
		// candidate the auto-pay adapter performs the cast's mana
		// activations atomically and a V1 plan is offered only on an empty
		// pool (rules/payment_plan.go's paymentPlanPoolOK), so the current
		// pool always reads 0 and both features would be inert; price them
		// against producible mana (the offered untapped sources) instead.
		// Every option of the ordinary manual decision leaves PlanBacked
		// false, so the manual arithmetic is byte-identical (pinned by
		// cast_weights_test.go's equivalence table).
		pool := ctx.poolTotal
		if o.PlanBacked {
			pool = ctx.producible
		}
		if b.FirstMain {
			s += w.Precombat // Precombat feature: 1 in the first main phase
		}
		if b.MyTurn && b.IsMain && b.Cards[o.Obj].InstantSpeed {
			s += w.InstantOnOwnTurn // instant-speed card in the seat's own main phase
		}
		// The window features (fixed evaluation order): each is the
		// instant-speed class conjuncted with the step and turn the cast
		// is being offered in. A weight 0 on every one skips all four, so
		// the default arithmetic is unchanged (pinned by cast_weights_test.go).
		if b.Cards[o.Obj].InstantSpeed {
			if b.MyTurn {
				switch b.Step {
				case state.StepUntap, state.StepUpkeep, state.StepDraw:
					s += w.InstantOwnPreMain
				case state.StepBeginCombat, state.StepDeclareAttackers, state.StepDeclareBlockers,
					state.StepCombatDamage, state.StepEndCombat:
					s += w.InstantOwnCombat
				}
			} else {
				s += w.InstantOppTurn
				if b.Step == state.StepEnd {
					s += w.InstantOppEnd
				}
			}
		}
		if ctx.producible == cost {
			s += w.CurveFit // CurveFit feature: cost exactly matches producible mana
		}
		s += w.ManaLeft * (pool - cost)
		s += w.OppCreatures * ctx.oppCreatures
		s += w.OwnCreatures * ctx.ownCreatures
		s += w.LifeDelta * ctx.lifeDelta
		// C11 (SetValue): the follow-up this cast leaves. Total castScore of
		// casting o first, then a best affordable subset of the other
		// offered casts with the mana remaining. Weight 0 skips it entirely,
		// so the default arithmetic is unchanged (pinned by the equivalence
		// table); the total is divided by 8 before scaling so a learned
		// weight reads on the same scale as the other features.
		if w.SetValue != 0 {
			eff := b.setEfficiency(entries,
				castEntry{obj: o.Obj, cost: cost, score: b.castScore(o)}, ctx.producible-cost)
			s += w.SetValue * eff / 8
		}
		// C10's interaction features: the card's class conjuncted with the
		// same decision-level context, fixed evaluation order.
		card := b.Cards[o.Obj]
		if card.Creature {
			if b.FirstMain {
				s += w.CreaturePrecombat // creature × first main phase
			}
			s += w.CreatureOppCreatures * ctx.oppCreatures
			s += w.CreatureLifeDelta * ctx.lifeDelta
		} else {
			s += w.NonCreatureOppCreatures * ctx.oppCreatures
		}
		if b.MyTurn && b.IsMain && card.InstantSpeed {
			s += w.InstantSpeedOffTurnHold // the hold-instants term (pairs with C9)
		}
		if inCmd {
			s -= b.commandTax(o.Obj)
			if s < 0 {
				continue // CR1: the recast has priced itself out — do not cast
			}
		} else if res > 0 && pool-cost >= res {
			s += res * w.ReserveScale // C7: prefer a cast that keeps the reserve
		}
		if best == -1 || s > bestScore || (s == bestScore && o.Index < best) {
			best, bestScore, bestInCmd = o.Index, s, inCmd
		}
	}
	// C9: the hold threshold. Only a NON-commander best can be held — a
	// command-zone cast is priced by value alone (CR1) because the deck
	// must be able to cast its commander; see the rule's comment above for
	// the per-winner reading.
	if best >= 0 && !bestInCmd && bestScore < w.CastThreshold {
		return -1
	}
	return best
}

// castContext is the decision-level context the cast scorer's features read:
// every value here is constant across the offered options of one decision,
// so it is computed once per chooseCast call, never per option.
type castContext struct {
	// poolTotal is the deciding seat's current mana pool (state.Mana.Total).
	poolTotal int32
	// producible is the mana the seat can produce this turn: the pool plus
	// the guaranteed production (Card.Produces) of every untapped mana
	// source it controls on the battlefield. The Card census only carries
	// the deciding seat's own zones, so every OnBattlefield entry is the
	// seat's own permanent. An indeterminate production claims nothing (its
	// colour slots are 0), so an unpriceable amount is never counted. Like
	// every other read of Card.Produces this is a sum over a CAPABILITY
	// vector: a source listing several alternatives (a dual's two colours, an
	// any-colour source's five) contributes one slot per alternative even
	// though one tap adds one unit, so this feature over-states a
	// multi-alternative board.
	producible int32
	// oppCreatures / ownCreatures are the public battlefield creature counts
	// on each side of the deciding seat (Creature.Controller vs d.Player).
	// Min/count folds over maps are order-independent, so no map iteration
	// order can reach a score.
	oppCreatures, ownCreatures int32
	// lifeDelta is the seat's life minus the LOWEST opponent's life; 0 when
	// no other player has a life total on the board.
	lifeDelta int32
}

// castContextFor computes the cast scorer's decision-level context for the
// deciding seat d.Player. Every fold is order-independent (a min or a
// count), so no map iteration order reaches it.
func (b Board) castContextFor(d *decision.Decision) castContext {
	ctx := castContext{poolTotal: b.Pool.Total(), producible: b.producibleMana()}
	for _, cr := range b.Creatures {
		if cr.Controller == d.Player {
			ctx.ownCreatures++
		} else {
			ctx.oppCreatures++
		}
	}
	own := b.Life[d.Player]
	low, hasOpp := int32(0), false
	for p, life := range b.Life {
		if p == d.Player {
			continue
		}
		if !hasOpp || life < low {
			low, hasOpp = life, true
		}
	}
	if hasOpp {
		ctx.lifeDelta = own - low
	}
	return ctx
}

// producibleMana is the mana the deciding seat can produce this turn: the
// pool plus every untapped own mana source's guaranteed production (the
// CurveFit feature's "mana the seat can produce" side).
func (b Board) producibleMana() int32 {
	total := b.Pool.Total()
	for _, c := range b.Cards {
		if !c.OnBattlefield || c.Tapped {
			continue
		}
		for i := 0; i < len(c.Produces.Colour); i++ {
			total += c.Produces.Colour[i]
		}
	}
	return total
}

// colourNeed is the aggregate coloured-pip demand of the seat's castable
// non-land cards -- the colours the hand wants to be able to pay this turn.
// It is the land-drop greedy's "what the hand wants to cast" side, expressed
// as per-colour pip counts so a land producing a wanted colour lands where
// the shortfall is. A card the seat cannot cast (a battlefield permanent, a
// non-Flashback graveyard card) contributes nothing: a land drop cannot
// enable it. A land (CMC 0) contributes nothing -- it has no pips.
func (b Board) colourNeed() [5]int32 {
	var need [5]int32
	for _, c := range b.Cards {
		if !c.Castable || c.CMC <= 0 {
			continue
		}
		pips := colourPips(c.ManaCost)
		for i := 0; i < 5; i++ {
			need[i] += pips[i]
		}
	}
	return need
}

// availableColours is the coloured mana the seat can already rely on: the
// current pool plus the guaranteed production of every source already on the
// battlefield (OnBattlefield). It is the land-drop greedy's "which colours
// are already available" side. An any-colour source reports each colour it can
// really be tapped for (cards.ProducedCounts), so a colour it can fix counts as
// covered; an Indeterminate-amount source contributes 0 to every colour slot
// (detected via Colour, never by reading the Indeterminate flag), so a source
// with no priceable amount never makes a colour look already covered. The candidate lands
// themselves sit in the hand (OnBattlefield false) and are not counted, so
// their colour is exactly the marginal value chooseLand scores.
func (b Board) availableColours() [5]int32 {
	var avail [5]int32
	for i := 0; i < 5; i++ {
		avail[i] = b.Pool[i]
	}
	for _, c := range b.Cards {
		if !c.OnBattlefield {
			continue
		}
		for i := 0; i < 5; i++ {
			if c.Produces.Colour[i] > 0 {
				avail[i] += c.Produces.Colour[i]
			}
		}
	}
	return avail
}

// reserve is the mana the policy keeps unspent in its own main phase so it
// can answer on another seat's turn. It is evidence-driven, not a constant:
// the cheapest castable card in hand (or the command zone) that can be cast
// at instant speed -- an Instant, or a permanent with Flash -- its converted
// cost, or 0 when the seat holds no instant-speed castable card at all (in
// which case nothing is reserved and the policy behaves exactly as it did
// before). The rule reads this in one sentence: in its own main phase the
// bot prefers a cast that leaves the mana pool at or above the cost of the
// cheapest instant-speed card it holds over a comparable cast that empties
// it, but a clearly better cast (a strong creature, an unprotected
// commander) is still made even if it spends that reserve.
func (b Board) reserve() int32 {
	var min int32 = -1
	for _, c := range b.Cards {
		if !c.Castable || !c.InstantSpeed || c.CMC <= 0 {
			continue
		}
		if min == -1 || c.CMC < min {
			min = c.CMC
		}
	}
	if min < 0 {
		return 0
	}
	return min
}

// castCost prices one card's cast: its printed converted cost plus the CR
// 903.8 command-zone tax ({2} per prior command-zone cast) when the card is
// its owner's commander sitting in the command zone -- the same number
// poolPays reads, so the reserve and the tap gate price the same cast.
func (b Board) castCost(id state.ObjID, c Card) int32 {
	cost := c.CMC
	if cmdr, ok := b.Commanders[id]; ok && cmdr.InCommandZone {
		cost += 2 * cmdr.Casts
	}
	return cost
}

// castEntry is one card's contribution to the C11 subset search: its object
// id (the deterministic sort key and the exclusion key), its cast cost and
// its castScore.
type castEntry struct {
	obj   state.ObjID
	cost  int32
	score int32
}

// setSubsetMaxCards bounds C11's exhaustive subset search: up to this many
// candidate cards are searched over every subset (2^10 = 1024 masks); a larger
// board falls back to the deterministic greedy-by-score pass.
const setSubsetMaxCards = 10

// ForeignSpell reports C8's census: whether the stack holds a spell
// controlled by anyone other than player — the only stack a counter is worth
// its mana at. It reads b.Stack in its own order, so it is deterministic by
// construction, and it is the ONE home of the census: chooseCast and the
// auto-pay adapter (seat/bot.go, deciding whether a preferred unplanned
// counter is dead) both read it rather than re-deriving the rule.
func (b Board) ForeignSpell(player state.PlayerID) bool {
	for _, s := range b.Stack {
		if s.IsSpell && s.Controller != player {
			return true
		}
	}
	return false
}

// CounterIsDead reports C8's verdict on a specific card for player: it is a
// counter (Card.Counter) with no foreign spell on the stack, so chooseCast
// refuses to cast it at all and spending mana on it can only self-counter.
// It composes the census's one home, ForeignSpell, so every caller that needs
// C8's refusal (chooseCast, castEntries, the auto-pay adapter's plan filter)
// reads the same rule rather than re-deriving it. A non-counter is never
// dead. The card must already be in b.Cards.
func (b Board) CounterIsDead(player state.PlayerID, id state.ObjID) bool {
	return b.Cards[id].Counter && !b.ForeignSpell(player)
}

// castEntries builds C11's candidate table: every cast option that survives
// C8 (a counter with no foreign spell is never cast; foreignSpell is the same
// census chooseCast computed), deduped by object id so a card offered under
// several cast modes counts once at its best castScore, and sorted by object
// id. The map is only an accumulator -- the returned slice is sorted, so no
// map iteration order reaches a score.
func (b Board) castEntries(d *decision.Decision, foreignSpell bool) []castEntry {
	byObj := make(map[state.ObjID]castEntry)
	for _, o := range d.Options {
		if o.Kind != "cast" {
			continue
		}
		if b.Cards[o.Obj].Counter && !foreignSpell {
			continue
		}
		e := byObj[o.Obj]
		e.obj = o.Obj
		e.cost = b.castCost(o.Obj, b.Cards[o.Obj])
		if sc := b.castScore(o); sc > e.score {
			e.score = sc
		}
		byObj[o.Obj] = e
	}
	out := make([]castEntry, 0, len(byObj))
	for _, e := range byObj {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].obj < out[j].obj })
	return out
}

// setEfficiency computes C11's follow-up value: the best total castScore
// obtainable by casting first and then an affordable subset of the other
// entries within budget (the seat's producible mana minus first's cast cost).
//
// Bounded and deterministic by construction. Candidates are the entries
// whose object id is not first's and whose cost fits the budget alone, taken
// in the (object-id-sorted) order castEntries produced. Up to
// setSubsetMaxCards of them are searched exhaustively over every subset
// (cost must stay within budget; the score is summed only from subsets that
// fit); a larger set falls back to a greedy-by-score sweep, ties on object
// id, taking each affordable card at most once. Every fold is over a slice
// in a fixed order, so no map iteration order can reach the result.
func (b Board) setEfficiency(entries []castEntry, first castEntry, budget int32) int32 {
	if budget < 0 {
		budget = 0
	}
	others := make([]castEntry, 0, len(entries))
	for _, e := range entries {
		if e.obj == first.obj || e.cost > budget {
			continue
		}
		others = append(others, e)
	}
	best := int32(0)
	if len(others) > setSubsetMaxCards {
		// Beyond the cap: greedy by score (ties on object id), each card
		// taken once if the running spend still fits the budget.
		sort.Slice(others, func(i, j int) bool {
			if others[i].score != others[j].score {
				return others[i].score > others[j].score
			}
			return others[i].obj < others[j].obj
		})
		var spent int32
		for _, e := range others {
			if spent+e.cost <= budget {
				spent += e.cost
				best += e.score
			}
		}
	} else {
		masks := 1 << uint(len(others))
		for mask := 0; mask < masks; mask++ {
			var cost, score int32
			for i := range others {
				if mask&(1<<uint(i)) != 0 {
					cost += others[i].cost
					score += others[i].score
				}
			}
			if cost <= budget && score > best {
				best = score
			}
		}
	}
	return first.score + best
}

// chooseLand is the KPriority land-drop ranking: it picks ONE of the
// offered "play_land" options, or -1 when none is offered.
//
//   - L1 (colour-first): a land is ranked by how much of the hand's unmet
//     colour need it covers. The unmet need (colourNeed minus
//     availableColours) is the per-colour shortfall the existing mana base
//     and pool leave; a candidate land's coverage is the amount of that
//     shortfall its own production fills. A land that produces a colour the
//     hand still needs outranks one that does not, so a hand needing {U}{U}
//     keeps an Island rather than a Plains, and a dual feeding a wanted
//     colour beats a basic of an already-covered one. The greedy is pip-
//     coverage: it aims the single land drop at the largest colour gap, the
//     cheap proxy for "the land that unlocks the most castable cards" without
//     walking the whole hand against the whole mana base per drop.
//   - L2 (reliability on a tie): two lands of equal colour coverage tie on
//     basic-ness, the reliable basic winning, because a basic unconditionally
//     produces its colour and never enters tapped nor demands a condition the
//     policy cannot read; basic-ness is only ever a tiebreak now, never the
//     primary criterion. A colourless or non-producing land thus still ranks,
//     just below any land that covers a real colour.
//   - L3 (flexibility kept): a tied pair of equally covering, equally basic
//     lands breaks toward the one with the FEWEST distinct colours -- the
//     least-flexible is played and the flexible source is kept in hand, the
//     same "spend the least flexible first" ergonomics the tap gate uses.
//   - L4 (deterministic tie): two lands still equal (empty coverage, equal
//     basic-ness, equal flexibility) tie on option index.
//
// It consumes no rng and ranges no map: the pick is a pure function of the
// offered options plus the board facts, so no map iteration order reaches it.
func (b Board) chooseLand(d *decision.Decision) int {
	need := b.colourNeed()
	avail := b.availableColours()
	var unmet [5]int32
	for i := 0; i < 5; i++ {
		unmet[i] = need[i] - avail[i]
		if unmet[i] < 0 {
			unmet[i] = 0
		}
	}
	best := -1
	bestCover := int32(-1)
	bestBasic := false
	bestFlex := 0
	for _, o := range d.Options {
		if o.Kind != "play_land" {
			continue
		}
		c := b.Cards[o.Obj] // zero facts read as no coverage/nonbasic/flex 0, never a crash
		prod := c.Produces
		var cover int32
		for i := 0; i < 5; i++ {
			if unmet[i] > 0 {
				take := prod.Colour[i]
				if take > unmet[i] {
					take = unmet[i]
				}
				cover += take
			}
		}
		basic := c.Basic
		flex := prod.DistinctColours()
		if best == -1 || cover > bestCover ||
			(cover == bestCover && basic && !bestBasic) ||
			(cover == bestCover && basic == bestBasic && flex < bestFlex) ||
			(cover == bestCover && basic == bestBasic && flex == bestFlex && o.Index < best) {
			best, bestCover, bestBasic, bestFlex = o.Index, cover, basic, flex
		}
	}
	return best
}
