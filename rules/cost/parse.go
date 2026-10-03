package cost

import (
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/adams-shaun/gorge/state"
)

// nonManaCost matches Sac<N/Spec>, Discard<N/Spec>, SubCounter<N/Kind> and
// Draw<N/Spec> tokens. Forge
// appends a human-readable "/description" after the spec and separates OR
// alternatives with ";"; the description may itself contain spaces (e.g.
// "Sac<1/Artifact;Creature/artifact or creature>"), which is why
// splitCostTokens keeps the whole <...> group atomic before nonManaCost ever
// sees it. The spec group runs up to the first "/"; the trailing
// "/description" is captured into CostPart.Desc (display only, read by
// costPhrase); the ";" alternation is folded to "," (MatchesSpec's own
// separator) at the parse site. Ruling FL-54.
var nonManaCost = regexp.MustCompile(`^(Sac|SubCounter|Discard|Draw)<(\d+)/([^/>]+)(?:/([^>]*))?>$`)

// drawDynCost matches Forge's non-literal Draw amount, Draw<X/Spec> -- the
// Champion of Wits family's "you may draw cards equal to its power. If you
// do, discard two cards" (Cost$ Draw<X/You> with SVar:X:Count$CardPower).
// The first field is the SVar token the payment resolves against the
// source's own SVar table; the second is the player spec ("You"). The
// trailing ";" OR alternation folds to "," like every other non-mana head.
// The literal form Draw<N/Spec> stays nonManaCost's.
var drawDynCost = regexp.MustCompile(`^Draw<([A-Za-z][A-Za-z0-9]*)/([^/>]+)(?:/([^>]*))?>$`)

// sacXCost matches the announced-count sacrifice form Sac<X/Spec> (Dargo, the
// Shipwrecker's "sacrifice any number of artifacts and/or creatures"): the
// player announces X (0..candidates, CR 601.2b) and exactly X permanents are
// sacrificed. The part is recorded with Spec "X" (N 0) -- the same announced
// convention PayEnergy<X> uses -- and xAsk/sacAsk consume it; a ReduceCost
// static that reads the paid X (Dargo's SVar X:Count$xPaid) resolves through
// costModifiers' SVar-aware amount read.
var sacXCost = regexp.MustCompile(`^Sac<X/([^/>]+)(?:/([^>]*))?>$`)

// exileCost matches Forge's ExileFromHand<N/Spec>, ExileFromGrave<N/Spec>
// and ExileAnyGrave<N/Spec> tokens -- exiling a matching card from the named
// zone as a cost payment (CR 118.8 lists exiling a card from one's hand among
// the payment actions; the graveyard form is the encore family's "exile this
// card from your graveyard"). AnyGrave is the same graveyard payment with the
// "any card" shape -- the payer picks the card, and beyond the spec's own
// predicates there is no zone provenance beyond "a graveyard" -- so it lands
// on the identical Exile part with Zone ZGraveyard (exg1: the 18-carrier
// Cavalier of Thorns / Thelon of Havenwood family). As with the other
// non-mana tokens the trailing "/description" is captured into CostPart.Desc
// and ";" alternations fold to ",".
// ExileFromHand evoke costs (the MH3 evoke family: Fury, Grief, ...), the
// AlternateAdditionalCost ExileFromGrave line and the ExileAnyGrave
// trigger-cost family are the corpus users.
var exileCost = regexp.MustCompile(`^Exile(FromHand|FromGrave|AnyGrave)<(X|\d+)/([^/>]+)(?:/([^>]*))?>$`)

// exileFromTopCost matches Forge's ExileFromTop<N/Card> token -- exiling the
// top N cards of the payer's OWN library as a cast/activation cost (Storm
// Elemental's "{U}, exile the top card of your library", Phyrexian
// Devourer's "Exile the top card of your library", Arc-Slogger and Whirling
// Catapult). The library is an ORDERED zone, so unlike the hand/graveyard
// Exile heads there is no chooser: the payment takes the top cards in library
// order and lands in the distinct Cost.ExileFromTop slice. The parsed Spec is
// required to be the measured "Card" (all seven corpus carriers); any other
// spec is left unmodelled (reported Unknown + one generic) rather than read as
// a deeper-card filter, because the top-of-library position is the cost's
// whole meaning. The same text is also the cumulative-upkeep action vocabulary
// (parseCumulativeAction owns that reading); this head is the ordinary Cost$
// spelling.
var exileFromTopCost = regexp.MustCompile(`^ExileFromTop<(\d+)/([^/>]+)(?:/([^>]*))?>$`)

// addCounterCost matches Forge's AddCounter<N/LOYALTY> token -- the
// planeswalker loyalty cost, and deliberately ONLY it (CR 107.4: the [+N]
// symbol): adding loyalty counters is not a payment at all, so an AddCounter
// part is a FREE cost component -- [+2] costs no mana, and AddCounter<0/LOYALTY>
// (the [0] abilities, 53 raw lines) costs nothing either. The settle is
// rules/cast.go's ability branch beside the SubCounter settle. The corpus's
// other 8 AddCounter tokens (Devoted Druid's M1M1 untap, Wall of Roots' M0M1
// mana ability, two UnlessCost$ SVars) are NOT matched by this regex and keep
// today's one-generic fallback, per the brief's scope boundary -- their
// counter semantics (M1M1/M0M1 kinds, mid-resolution UnlessCost payers) are
// their own work.
var addCounterCost = regexp.MustCompile(`^AddCounter<(\d+)/(LOYALTY)(?:/([^>]*))?>$`)

// addSelfCounterCost matches the SOURCE-ANCHORED non-loyalty AddCounter
// token -- AddCounter<N/KIND> with no third field (Wall of Roots' M0M1 mana
// ability, Devoted Druid's M1M1 untap, Mazemind Tome's PAGE). The payment
// puts N counters of KIND on the source itself, exactly the CounterChange the
// activation settle (rules/cast.go) and the mana path emit for the loyalty
// form. A third field (a chooser filter such as Creature.YouCtrl, or an
// UnlessCost$ payer anchor) is a different payment and keeps the reported
// one-generic fallback.
var addSelfCounterCost = regexp.MustCompile(`^AddCounter<(\d+)/([A-Za-z0-9_]+)>$`)

// exertCost matches the source-anchored exert cost Exert<1/CARDNAME>
// (Oasis Ritualist, Arena of Glory, Pride Sovereign). NICKNAME is Forge's
// legendary short-name spelling of the same self-reference.
var exertCost = regexp.MustCompile(`^Exert<1/(?:CARDNAME|NICKNAME)(?:/([^>]*))?>$`)

// lifeCost matches Forge's fixed life-payment token. Dynamic values such as
// PayLife<X> retain the ordinary malformed-token fallback below: this engine
// has no source from which to resolve their value.
var lifeCost = regexp.MustCompile(`^PayLife<(\d+)>$`)

var choiceCost = regexp.MustCompile(`^(Reveal|Behold|BeholdExile|tapXType)<(\d+)/([^/>]+)(?:/([^>]*))?>$`)

// choiceCostRevealOrChoose additionally recognises Forge's either-or
// `RevealOrChoose<N/Spec>` cost (Monstrous Emergence, Dragon's Fire): reveal a
// card matching Spec from hand, OR choose a permanent matching Spec you
// control. Both arms are modelled as the distinct Cost.RevealOrChoose slice
// (see its doc): the reveal arm is announced and its card rides the Revealed
// paid list, the choose arm elects an already-controlled permanent and is
// announced as a choice, never a reveal. The former unrecognised-symbol
// fallback charged one generic too much and dropped the cost entirely.
var choiceCostRevealOrChoose = regexp.MustCompile(`^RevealOrChoose<(\d+)/([^/>]+)(?:/([^>]*))?>$`)

// revealChosenCost matches the designation-reveal cost heads. Forge has two
// spellings: RevealChosen<Player> (reveal the player you secretly chose) and
// RevealChosen<Type/creature type> (reveal the creature type you secretly
// chose). Unlike Reveal<N/Spec> there is no count and no card to pick -- the
// designation was chosen earlier by a Secretly$ True ChoosePlayer/ChooseType
// -- so the regex carries no N and the trailing field is display text. The
// share of these heads used to be the unrecognised-symbol fallback, which
// priced each at one generic mana and dropped the reveal entirely.
var revealChosenCost = regexp.MustCompile(`^RevealChosen<(Player|Type)(?:/([^>]*))?>$`)

// dynTapCost matches Forge's dynamic tap-any-number tapXType tokens -- the
// heads the literal choiceCost regex above cannot read:
//
//   - tapXType<X/Spec> (Myr Battlesphere's "tap X untapped Myr", Burn at the
//     Stake's spell-cost form, Necron Overlord's "{X}, tap X artifacts"): the
//     count IS the cast's {X}. When the cost carries another announce-bearing
//     part (a printed {X}, PayEnergy<X>, Sac<X>, ...) xAsk announces it and
//     the part settles exactly that value; when it does not, the tap election
//     itself announces (CR 601.2b) and the paid count binds Count$xPaid
//     through the pay-time CastInfo. A triggered ability carrying the head
//     pays through the triggered-cost window (rules/cumulative.go), whose
//     tap election is the payment and whose empty answer is the decline.
//
//   - tapXType<Any/Spec> (Mossbridge Troll): a free count that binds no X.
//     Paying the cost still taps at least one matching permanent, so a spec
//     the filter cannot admit any candidate for (Mossbridge's
//     withTotalPowerGE10 group predicate fails closed) leaves the ability
//     unpayable rather than offering a zero-tap payment.
//
// The trailing "/description" is captured into CostPart.Desc and ";" alternations
// fold to "," like every other non-mana head.
var dynTapCost = regexp.MustCompile(`^tapXType<(X|Any)/([^/>]+)(?:/([^>]*))?>$`)

// untapYTypeCost matches Forge's untapYType<N/Spec> cost token -- untapping N
// permanents matching Spec as the payment (Forge CostUntapType: Benthic
// Explorers' "untap a tapped land an opponent controls", Halo Fountain's and
// Crackleburr's non-mana activations). N is a literal count; there is no X
// form in the corpus. It is the mirror of the literal tapXType<N/Spec> head
// (choiceCost), except the elected permanents must already be TAPPED. The
// trailing "/description" is captured into CostPart.Desc and ";" alternations
// fold to "," like every other non-mana head.
var untapYTypeCost = regexp.MustCompile(`^untapYType<(\d+)/([^/>]+)(?:/([^>]*))?>$`)

var blightCost = regexp.MustCompile(`^Blight<(\d+|X)>$`)

// groupPowerFloor matches the withTotalPowerGE<N> GROUP predicate Forge
// appends to a tapXType spec (Mossbridge Troll's
// "Creature.Other+withTotalPowerGE10", Crew's "Creature.Other+withTotalPowerGE<n>").
// The constraint is over the whole paid set -- the tapped creatures' TOTAL
// power -- not over each candidate on its own, so a per-object filter cannot
// evaluate it: stripGroupPowerFloor moves it into CostPart.MinPower instead
// of leaving the unknown token to fail the whole spec closed.
var groupPowerFloor = regexp.MustCompile(`withTotalPowerGE(\d+)`)

// stripGroupPowerFloor splits a withTotalPowerGE<N> group predicate off a
// tapXType spec: it returns the spec without the predicate (so the per-object
// filter sees only per-candidate terms) and the floor as a number. A floor
// that overflows int32 is clamped to MaxInt32 -- still an unpayable floor on
// any real board, which is the fail-closed direction. A spec without the
// predicate returns unchanged with floor 0.
func stripGroupPowerFloor(spec string) (string, int32) {
	m := groupPowerFloor.FindStringSubmatchIndex(spec)
	if m == nil {
		return spec, 0
	}
	n, err := strconv.ParseInt(spec[m[2]:m[3]], 10, 64)
	if err != nil || n < 0 {
		return spec, 0
	}
	if n > int64(math.MaxInt32) {
		n = int64(math.MaxInt32)
	}
	out := spec[:m[0]] + spec[m[1]:]
	// The predicate is joined by Forge's own "+" separator
	// ("Creature.Other+withTotalPowerGE3"); with the predicate gone the
	// trailing separator must go too, or it becomes an empty alternative the
	// per-object matcher would read as an unparseable term.
	out = strings.TrimSuffix(out, "+")
	return out, int32(n)
}

// payEnergyCost matches Forge's PayEnergy<N> and PayEnergy<X> tokens --
// removing N energy counters from the payer (CR 118.2d; Forge
// CostPayEnergy.canPay reads the payer's ENERGY counter total, and its
// getMaxAmountX bounds a dynamic PayEnergy<X> by that same total). The
// trailing "/description" Forge may append is captured into CostPart.Desc
// like every other head. The X form is recorded as a part with Spec "X": xAsk bounds the
// announced value by the payer's energy count and the settle spends exactly
// that many, so the announcement and the spend cannot disagree.
var payEnergyCost = regexp.MustCompile(`^PayEnergy<([0-9]+|X)(?:/([^>]*))?>$`)

// returnCost matches Forge's Return<N/Spec> tokens -- a permanent matching
// Spec returned to its OWNER's hand as the payment (Forge CostReturn's
// moveToHand; its payCostFromSource branch is a Spec of CARDNAME, the source
// itself -- Chthonian Nightmare's "Return Chthonian Nightmare to its owner's
// hand"). N is almost always 1 (94 corpus files carry the token; every
// parsed one is 1). The trailing description is captured into CostPart.Desc, ";"
// alternations fold to "," like every other non-mana head.
var returnCost = regexp.MustCompile(`^Return<(\d+)/([^/>]+)(?:/([^>]*))?>$`)

// putCardToLibCost matches Forge's PutCardToLibFrom<Zone><N/Pos/Spec> cost
// tokens -- moving N cards matching Spec from the payer's Hand, Graveyard or
// Battlefield to the top (Pos "0") or bottom (Pos "-1") of their own library
// as the payment (Forge CostPutCardToLib). The zone is the middle of the
// head, never a parameter, and the second field is the library position:
// Leashling/Penance/Tainted Specter place on TOP (Pos 0), the Born of the
// Gods reflective-mage family (Ardent Dustspeaker) and Battlefield Scrounger
// and Timestream Navigator put on the BOTTOM (Pos -1). The trailing
// "/description" is captured into CostPart.Desc and ";" alternations fold to
// "," like every other non-mana head. It is deliberately a POSITIVE zone list: a future
// Forge zone name this regex does not name falls through to the
// unrecognised-symbol fallback (the head is reported in Cost.Unknown, never
// silently modelled as a different zone). The separate
// PutCardToLibFromSameGrave cumulative-upkeep spelling is NOT matched here --
// it is a keyword action, not a cost token.
var putCardToLibCost = regexp.MustCompile(`^PutCardToLibFrom(Hand|Grave|Battlefield)<(\d+)/(-?\d+)/([^/>]+)(?:/([^>]*))?>$`)

// exileBattlefieldCost matches Forge's bare Exile<N/Spec> token -- exiling a
// matching permanent from the BATTLEFIELD as the payment (Karn's Sylex's
// "{X}, {T}, Exile Karn's Sylex", Mechtitan Core's "Exile CARDNAME and four
// other artifact creatures", Zombie Assassin's "{T}, Exile two cards from
// your graveyard and CARDNAME"). The zone-qualified forms are the separate
// exileCost heads above (ExileFromHand/ExileFromGrave); the trailing
// "/description" is captured into CostPart.Desc and ";" alternations fold to
// "," like every other non-mana head.
var exileBattlefieldCost = regexp.MustCompile(`^Exile<(\d+)/([^/>]+)(?:/([^>]*))?>$`)

// exiledMoveToGraveCost matches Forge's ExiledMoveToGrave<N/Spec> token --
// moving N cards matching Spec from EXILE into their OWNER's graveyard as
// the payment (the Eldrazi processor activation/trigger costs and Shelob,
// Dread Weaver's {2}{B} ability; 16 corpus files). The trailing
// "/description" is captured into CostPart.Desc and ";" alternations fold to
// "," like every other non-mana head. Before the head existed the token hit the final
// unrecognised-symbol fallback: a phantom {1} rode the price (a player with
// exactly {2}{B} could not activate Shelob) and the cost's governing action
// was silently dropped -- a fail-open defect.
var exiledMoveToGraveCost = regexp.MustCompile(`^ExiledMoveToGrave<(\d+)/([^/>]+)(?:/([^>]*))?>$`)

var millCost = regexp.MustCompile(`^Mill<(\d+)>$`)

// evidenceCost matches the CollectEvidence<N> / CollectEvidence<NAME> cost
// token (alltargeted1): the evidence-exile additional cost. The captured
// value is a literal digit string or an SVar name the payment stage resolves
// (Urgent Necropsy's X, whose body reads AllTargeted$CardManaCost over the
// whole target union). The Ward payment path intercepts its own literal
// CollectEvidence<N> spelling (ward.go's wardSpecialCost) before parseCost
// runs, so this head only prices the plain Cost$ family.
var evidenceCost = regexp.MustCompile(`^CollectEvidence<([^>]+)>$`)

// payLifeXCost matches Forge's announced life payment PayLife<X> (Toxic
// Deluge's "pay X life", Necrodominance's end-step body): the cast announces
// X like a printed {X} and the settle pays that much life, so the value is
// bounded by the payer's life total at the X ask. The fixed form is the
// lifeCost head above.
var payLifeXCost = regexp.MustCompile(`^PayLife<X>$`)

// subCounterXCost matches Forge's announced counter removal
// SubCounter<X/Kind> (Chandra, Awakened Inferno's "remove X loyalty
// counters"): the kind is read and the count is the cast's announced X,
// bounded by the counters the source actually has. The fixed form is the
// nonManaCost head above.
// subCounterCost matches Forge's counter-removal cost token in every modelled
// spelling: the two-field forms SubCounter<N/Kind> (fixed count) and
// SubCounter<X/Kind> (the announced count, CR 601.2b -- Chandra, Awakened
// Inferno's "remove X loyalty counters"), and the third-field form
// SubCounter<N|X/Kind/Target[/desc]> whose removal-target filter names whose
// counters the payment removes (Moxite Refinery's
// "SubCounter<X/Any/Artifact.YouCtrl;Creature.YouCtrl/...>": X counters of
// ANY kind from an artifact or creature you control). The count field is the
// literal digits or the announced X; the kind is read verbatim ("Any" reads
// every kind); the optional third field is the target filter, matched against
// the payer's battlefield like a Sac part's spec, and the optional fourth is
// the display description. The old subCounterXCost head (X form only, target
// dropped) is subsumed by this one.
var subCounterCost = regexp.MustCompile(`^SubCounter<(X\d+\+|X|\d+)/([^/>]+)(?:/([^/>]+))?(?:/([^>]*))?>$`)

// removeAnyCounterCost is Forge's named spelling for a counter-removal cost.
// Despite the name, the second field is the counter kind (often Any), while
// the third field restricts the permanent the counters come from.
var removeAnyCounterCost = regexp.MustCompile(`^RemoveAnyCounter<(X\d+\+|X|\d+)/([^/>]+)(?:/([^/>]+))?(?:/([^>]*))?>$`)

// damageYouCost matches Forge's DamageYou<N> token -- the payer takes N
// damage from the source as the payment (Forge CostDamage). The corpus's
// only shape is an UnlessCost$ (Vexing Devil's "have it deal 4 damage to
// them"), which the unless-pay arm prices through
// effects.ParseDamageUnlessCost; this head keeps a plain Cost$ spelling out
// of Cost.Unknown.
var damageYouCost = regexp.MustCompile(`^DamageYou<(\d+)(?:/([^>]*))?>$`)

// gainLifeCost matches Forge's GainLife<N/Player...> cost token -- the payer
// has the named player(s) gain N life as the payment (Forge CostGainLife).
// The corpus carries it as a cost head on exactly the three "opponent gains
// life" AlternativeCost cards (Invigorate, Reverent Silence, Skyshroud
// Cutter); it also appears as a Cumulative-upkeep action (Wall of Shards,
// whose own parseCumulativeAction reads it) and as a Splice cost (Roar of
// Jukai, whose keyword is unimplemented) -- neither of those routes reaches
// ParseCost. The amount is a FIXED literal N: every corpus carrier is a
// literal, so any other value form falls to the ordinary malformed-token
// fallback (never a silent dynamic reading). Spec keeps Forge's raw player
// word; the optional trailing field is the `/*` each/all marker.
var gainLifeCost = regexp.MustCompile(`^GainLife<(\d+)/(Player[^/>]+)(?:/([^>]*))?>$`)

var rollDiceCost = regexp.MustCompile(`^RollDice<([^>]*)>$`)

// xMinCost matches Forge's XMin<N> cost token -- the announced-X LOWER
// BOUND, "X can't be 0" (XMin1) or "X can't be less than 4" (XMin4). It is
// not a payment at all: it costs no mana and announces no X of its own, it
// only constrains the value the cost's {X} may take. The Suspend-only
// suspendCost special case was the sole reader until this head; every other
// carrier (Kicker's Thieving Skydiver, Flashback's Light Up the Night, a
// plain Cost$) fell through to the unrecognised-symbol fallback and charged
// one phantom generic pip. 29 corpus files carry the token at the pin (27
// XMin1, 2 XMin4).
var xMinCost = regexp.MustCompile(`^XMin(\d+)$`)

var costBraces = strings.NewReplacer("{", " ", "}", " ")

// ParseCost accepts both Forge's space-separated form ("2 U U") and the
// bracketed oracle form ("{2}{U}{U}"). "no cost" and "" are free.
func ParseCost(s string) Cost {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "no cost") {
		return Cost{}
	}
	s = costBraces.Replace(s)
	var c Cost
	for toks := (TokenIter{s: s}); ; {
		sym, ok := toks.Next()
		if !ok {
			break
		}
		switch {
		case sym == "T":
			c.Tap = true
		case sym == "Q":
			c.Untap = true
		case sym == "X":
			c.X++
		case sym == "S":
			// CR 107.4h: snow mana. One pip, payable only by a mana a snow
			// permanent produced (rules/mana.go's resolveMana pays it from the
			// parallel snow tally, never plain pool mana).
			c.Snow++
		case sym == "Forage":
			c.Forage = true
		case strings.EqualFold(sym, "Mandatory"):
			// Forge's mandatory-payment marker (Cost$ Mandatory tapXType<X/...>,
			// Mandatory Sac<...>, Mandatory PayEnergy<...> -- 31 raw cost
			// occurrences): a payment-mode marker, not a payment. It priced one
			// phantom generic mana before, so a Mandatory tapXType trigger cost
			// was silently bought for {1}; skip the token and model the rest.
			continue
		case len(sym) == 1 && strings.ContainsAny(sym, "WUBRGC"):
			c.Colored[state.ManaIndex(sym[0])]++
		case isHybrid(sym):
			c.Hybrid = append(c.Hybrid, hybridPair(sym))
		case isPhyrexian(sym):
			c.Phyrexian = append(c.Phyrexian, phyrexianColor(sym))
		case isTwobrid(sym):
			c.Twobrid = append(c.Twobrid, twobridPair(sym))
		case isHybridPhyrexian(sym):
			c.HybridPhyrexian = append(c.HybridPhyrexian, hybridPhyrexianPair(sym))
		default:
			// A generic amount ("2"): every head regexp below is anchored on a
			// letter, so a token opening with a digit or a sign matches none
			// of them and lands on the numeric parse at the end of this
			// branch. Go straight there instead of trying ~30 patterns.
			if c0 := sym[0]; c0 >= '0' && c0 <= '9' || c0 == '-' || c0 == '+' {
				if n, err := strconv.ParseInt(sym, 10, 64); err == nil && n >= 0 && n <= int64(math.MaxInt32) {
					c.Generic = AddClampedGeneric(c.Generic, n)
					continue
				}
				c.reportUnknown(sym)
				c.Generic = AddClampedGeneric(c.Generic, 1)
				continue
			}
			if m := waterbendCost.FindStringSubmatch(sym); m != nil {
				// Waterbend<N> / Waterbend<X> (the keyword action "waterbend
				// {N}": pay {N}; while paying it, each untapped artifact or
				// creature the payer taps pays for {1}). The {N} rides
				// Generic like any other generic cost; Waterbend is the
				// annotation that caps how much of that generic the taps may
				// cover (CR 701.67a). This is the ABILITY-cost reader: the
				// RaiseCost bridge (rules/raise_cost_extra.go) recognises the
				// same token itself and routes it to mods.waterbend, so its
				// Cost$ text never reaches here. Waterbend<X> contributes the
				// announced {X} and marks the amount open (WaterbendX).
				if m[1] == "X" {
					c.X++
					c.WaterbendX = true
					continue
				}
				n, err := strconv.ParseInt(m[1], 10, 64)
				if err != nil || n < 0 || n > int64(math.MaxInt32) {
					// Same safe fallback as every other malformed cost
					// token -- and REPORT it: the head is recognised, this
					// instance is not modelled.
					c.Generic = AddClampedGeneric(c.Generic, 1)
					c.reportUnknown(sym)
					continue
				}
				c.Generic = AddClampedGeneric(c.Generic, n)
				c.Waterbend = AddClampedGeneric(c.Waterbend, n)
				continue
			}
			if m := evidenceCost.FindStringSubmatch(sym); m != nil {
				// CollectEvidence<N> / CollectEvidence<NAME> (alltargeted1): a
				// real evidence-exile component, not one phantom generic mana.
				// A literal N prices directly; a NAME (the dynamic X form) is
				// resolved by the payment stage against the chosen targets.
				if n, err := strconv.ParseInt(m[1], 10, 64); err == nil && n >= 0 && n <= int64(math.MaxInt32) {
					c.Evidence = append(c.Evidence, CostPart{N: int32(n)})
					continue
				}
				c.Evidence = append(c.Evidence, CostPart{Dyn: m[1]})
				continue
			}
			if m := millCost.FindStringSubmatch(sym); m != nil {
				n, err := strconv.ParseInt(m[1], 10, 64)
				if err != nil || n < 0 || n > int64(math.MaxInt32) {
					c.Generic = AddClampedGeneric(c.Generic, 1)
					c.reportUnknown(sym)
					continue
				}
				c.Mill = append(c.Mill, CostPart{N: int32(n)})
				continue
			}
			if m := untapYTypeCost.FindStringSubmatch(sym); m != nil {
				// untapYType<N/Spec> (Forge CostUntapType): untap N matching
				// permanents as the payment. A malformed/overflowing N degrades to
				// the reported one-generic fallback like every other head.
				n, err := strconv.ParseInt(m[1], 10, 64)
				if err != nil || n <= 0 || n > int64(math.MaxInt32) {
					c.Generic = AddClampedGeneric(c.Generic, 1)
					c.reportUnknown(sym)
					continue
				}
				c.UntapPermanent = append(c.UntapPermanent, CostPart{N: int32(n),
					Spec: strings.ReplaceAll(m[2], ";", ","), Desc: m[3]})
				continue
			}
			if m := dynTapCost.FindStringSubmatch(sym); m != nil {
				// The dynamic tapXType heads (see the regex's doc): a TapPermanent
				// part whose count the tap election resolves at payment -- "X"
				// announcing the cast's {X}, "Any" free. N is unused.
				spec := strings.ReplaceAll(m[2], ";", ",")
				floor := int32(0)
				// A withTotalPowerGE<N> group predicate rides the Any form (Crew,
				// Mossbridge Troll): the set-level floor moves into MinPower and
				// out of the spec. The X form keeps the predicate in the spec,
				// where the unknown token fails closed as before.
				if m[1] == "Any" {
					spec, floor = stripGroupPowerFloor(spec)
				}
				c.TapPermanent = append(c.TapPermanent, CostPart{Dyn: m[1], Spec: spec, Desc: m[3], MinPower: floor})
				continue
			}
			if m := choiceCost.FindStringSubmatch(sym); m != nil {
				n, err := strconv.ParseInt(m[2], 10, 64)
				if err != nil || n <= 0 || n > int64(math.MaxInt32) {
					// Same safe fallback as every other malformed cost token --
					// and REPORT it: the head is recognised, this instance is
					// not modelled.
					c.Generic = AddClampedGeneric(c.Generic, 1)
					c.reportUnknown(sym)
					continue
				}
				part := CostPart{N: int32(n), Spec: strings.ReplaceAll(m[3], ";", ","), Desc: m[4]}
				switch m[1] {
				case "Reveal":
					c.Reveal = append(c.Reveal, part)
				case "Behold":
					c.Behold = append(c.Behold, part)
				case "BeholdExile":
					// Behold, then exile the beheld object (CostPart.ThenExile).
					part.ThenExile = true
					c.Behold = append(c.Behold, part)
				default:
					// A literal tapXType form may carry a group predicate too;
					// exactly N are tapped, so the set-level floor is enforced
					// against the N largest candidates (rules/cast.go).
					part.Spec, part.MinPower = stripGroupPowerFloor(part.Spec)
					c.TapPermanent = append(c.TapPermanent, part)
				}
				continue
			}
			if m := choiceCostRevealOrChoose.FindStringSubmatch(sym); m != nil {
				// RevealOrChoose<N/Spec> is an either-or cost: reveal N hand cards
				// matching Spec OR choose N permanents matching Spec you control.
				// It lands in its OWN slice so both arms stay distinct (the choose
				// arm must not be read as a hand reveal); see the slice's doc.
				n, err := strconv.ParseInt(m[1], 10, 64)
				if err != nil || n <= 0 || n > int64(math.MaxInt32) {
					c.Generic = AddClampedGeneric(c.Generic, 1)
					c.reportUnknown(sym)
					continue
				}
				c.RevealOrChoose = append(c.RevealOrChoose, CostPart{N: int32(n), Spec: strings.ReplaceAll(m[2], ";", ","), Desc: m[3]})
				continue
			}
			if m := revealChosenCost.FindStringSubmatch(sym); m != nil {
				// A designation reveal is a real, modelled, FREE cost component:
				// no generic substitution and no Unknown census entry. The
				// designation's presence is the payability gate
				// (nonManaCastable) and the payment is one public Note
				// (emitChoiceCosts).
				c.RevealChosen = append(c.RevealChosen, CostPart{Spec: m[1], Desc: m[2]})
				continue
			}
			if m := blightCost.FindStringSubmatch(sym); m != nil {
				// Blight<X> (Blighted Nightmare, Soul Immolation): the announced
				// form of the Blight cost. X is announced (CR 601.2b) exactly the
				// way Sac<X/Spec> announces its count -- the count is settled by
				// the X ask and the payment reads the announced value -- so it
				// carries an Announced part and NO {X} mana symbol: the cost is
				// paid in -1/-1 counters, not generic mana.
				if m[1] == "X" {
					c.Blight = append(c.Blight, CostPart{Spec: "Creature.YouCtrl", Announced: true})
					continue
				}
				n, err := strconv.ParseInt(m[1], 10, 64)
				if err != nil || n <= 0 || n > int64(math.MaxInt32) {
					// Same safe fallback as every other malformed cost token --
					// and REPORT it: the head is recognised, this instance is
					// not modelled.
					c.Generic = AddClampedGeneric(c.Generic, 1)
					c.reportUnknown(sym)
					continue
				}
				c.Blight = append(c.Blight, CostPart{N: int32(n), Spec: "Creature.YouCtrl"})
				continue
			}
			if m := lifeCost.FindStringSubmatch(sym); m != nil {
				n, err := strconv.ParseInt(m[1], 10, 64)
				if err != nil || n < 0 || n > int64(math.MaxInt32) {
					// Keep an out-of-range PayLife token on the same safe fallback
					// as every other malformed cost token -- and REPORT it: the
					// head is recognised, this instance is not modelled.
					c.Generic = AddClampedGeneric(c.Generic, 1)
					c.reportUnknown(sym)
					continue
				}
				c.Life = AddClampedGeneric(c.Life, n)
				continue
			}
			if m := removeAnyCounterCost.FindStringSubmatch(sym); m != nil {
				// RemoveAnyCounter is the same payment component as SubCounter;
				// its distinct head is Forge's spelling for the counter-choice
				// family. Keep the target filter and display description intact.
				kind := strings.ReplaceAll(m[2], ";", ",")
				target, desc := "", m[3]
				if t := m[3]; t != "" && !strings.ContainsAny(t, " \t") {
					target, desc = t, m[4]
				}
				target = strings.ReplaceAll(target, ";", ",")
				if strings.HasPrefix(m[1], "X") {
					if m[1] != "X" {
						if n, err := strconv.ParseInt(m[1][1:len(m[1])-1], 10, 32); err == nil && int32(n) > c.XMin {
							c.XMin = int32(n)
						}
					}
					c.SubCounter = append(c.SubCounter, CostPart{Spec: kind, Target: target, Announced: true, Desc: desc})
					continue
				}
				n, err := strconv.ParseInt(m[1], 10, 64)
				if err != nil || n < 0 || n > int64(math.MaxInt32) {
					c.Generic = AddClampedGeneric(c.Generic, 1)
					c.reportUnknown(sym)
					continue
				}
				c.SubCounter = append(c.SubCounter, CostPart{N: int32(n), Spec: kind, Target: target, Desc: desc})
				continue
			}
			if m := subCounterCost.FindStringSubmatch(sym); m != nil {
				// The counter-removal cost in every modelled spelling. The kind
				// is read verbatim ("Any" reads every kind -- the candidate/
				// bound reads in rules/cast.go); the count is the literal digits
				// or the cast's announced X (bounded at the X ask). The optional
				// third field is the removal-target filter: Forge writes the
				// restriction space-free and the display description prose, so a
				// space-bearing third field is a description and the removal
				// stays source-anchored (subCounterTargetsSource). The ";" OR
				// alternation folds to "," like every other spec.
				kind := strings.ReplaceAll(m[2], ";", ",")
				target, desc := "", m[3]
				if t := m[3]; t != "" && !strings.ContainsAny(t, " \t") {
					target, desc = t, m[4]
				}
				target = strings.ReplaceAll(target, ";", ",")
				if strings.HasPrefix(m[1], "X") {
					if m[1] != "X" {
						if n, err := strconv.ParseInt(m[1][1:len(m[1])-1], 10, 32); err == nil && int32(n) > c.XMin {
							c.XMin = int32(n)
						}
					}
					// The announced form: the count is the cast's announced X
					// (bounded at the X ask).
					c.SubCounter = append(c.SubCounter, CostPart{Spec: kind, Target: target, Announced: true, Desc: desc})
					continue
				}
				n, err := strconv.ParseInt(m[1], 10, 64)
				if err != nil || n < 0 || n > int64(math.MaxInt32) {
					// Same safe fallback as every other malformed cost token --
					// and REPORT it: the head is recognised, this instance is
					// not modelled.
					c.Generic = AddClampedGeneric(c.Generic, 1)
					c.reportUnknown(sym)
					continue
				}
				c.SubCounter = append(c.SubCounter, CostPart{N: int32(n), Spec: kind, Target: target, Desc: desc})
				continue
			}
			if m := nonManaCost.FindStringSubmatch(sym); m != nil {
				n, err := strconv.ParseInt(m[2], 10, 64)
				if err != nil || n < 0 || n > int64(math.MaxInt32) {
					// A malformed Sac/Discard/SubCounter token degrades the same way
					// an unrecognised mana token does: one generic mana,
					// never a hard parse error -- and is reported (the head is
					// recognised, this instance is not modelled).
					c.Generic = AddClampedGeneric(c.Generic, 1)
					c.reportUnknown(sym)
					continue
				}
				// Fold Forge's ";" OR alternation into the "," MatchesSpec
				// already uses, so "Artifact;Creature" matches either.
				spec := strings.ReplaceAll(m[3], ";", ",")
				part := CostPart{N: int32(n), Spec: spec, Desc: m[4]}
				switch m[1] {
				case "Sac":
					c.Sac = append(c.Sac, part)
				case "Discard":
					c.Discard = append(c.Discard, part)
				case "Draw":
					c.Draw = append(c.Draw, part)
				default:
					c.SubCounter = append(c.SubCounter, part)
				}
				continue
			}
			if m := drawDynCost.FindStringSubmatch(sym); m != nil {
				// The dynamic-amount Draw cost (Draw<X/Spec>): the count is not
				// a literal but the source's SVar named by m[1], resolved at
				// payment. Recorded with Dyn set and N unused -- the part is a
				// real modelled cost, so the unrecognised-symbol fallback that
				// used to substitute one generic mana (and report the head via
				// reportUnknown, the census's cost:Draw label) never runs.
				spec := strings.ReplaceAll(m[2], ";", ",")
				c.Draw = append(c.Draw, CostPart{Spec: spec, Dyn: m[1], Desc: m[3]})
				continue
			}
			if m := exileFromTopCost.FindStringSubmatch(sym); m != nil {
				if m[2] != "Card" {
					// Not the measured shape: leave it unmodelled rather than
					// letting an arbitrary spec select a deeper library card.
					c.Generic = AddClampedGeneric(c.Generic, 1)
					c.reportUnknown(sym)
					continue
				}
				n, err := strconv.ParseInt(m[1], 10, 64)
				if err != nil || n <= 0 || n > int64(math.MaxInt32) {
					c.Generic = AddClampedGeneric(c.Generic, 1)
					c.reportUnknown(sym)
					continue
				}
				c.ExileFromTop = append(c.ExileFromTop, CostPart{N: int32(n), Spec: "Card", Desc: m[3]})
				continue
			}
			if m := exileBattlefieldCost.FindStringSubmatch(sym); m != nil {
				n, err := strconv.ParseInt(m[1], 10, 64)
				if err != nil || n < 0 || n > int64(math.MaxInt32) {
					// Same safe fallback as every other malformed cost token --
					// and REPORT it: the head is recognised, this instance is
					// not modelled.
					c.Generic = AddClampedGeneric(c.Generic, 1)
					c.reportUnknown(sym)
					continue
				}
				spec := strings.ReplaceAll(m[2], ";", ",")
				c.Exile = append(c.Exile, CostPart{N: int32(n), Spec: spec, Zone: state.ZBattlefield, Desc: m[3]})
				continue
			}
			if m := exiledMoveToGraveCost.FindStringSubmatch(sym); m != nil {
				n, err := strconv.ParseInt(m[1], 10, 64)
				if err != nil || n < 0 || n > int64(math.MaxInt32) {
					// Same safe fallback as every other malformed cost token --
					// and REPORT it: the head is recognised, this instance is
					// not modelled.
					c.Generic = AddClampedGeneric(c.Generic, 1)
					c.reportUnknown(sym)
					continue
				}
				spec := strings.ReplaceAll(m[2], ";", ",")
				c.MoveToGrave = append(c.MoveToGrave, CostPart{N: int32(n), Spec: spec, Desc: m[3]})
				continue
			}
			if m := payLifeXCost.FindStringSubmatch(sym); m != nil {
				// The announced form: the cast announces X (bounded by the
				// payer's life at the X ask) and the settle pays that much life.
				// No generic substitution, no Unknown entry.
				c.LifeX = append(c.LifeX, CostPart{Spec: "X", Announced: true})
				continue
			}
			if m := rollDiceCost.FindStringSubmatch(sym); m != nil {
				fields := strings.Split(m[1], "/")
				if len(fields) == 3 && fields[2] == "X" {
					n, nerr := strconv.ParseInt(fields[0], 10, 32)
					sides, serr := strconv.ParseInt(fields[1], 10, 32)
					if nerr == nil && serr == nil && n > 0 && sides > 0 {
						c.RollDice = append(c.RollDice, CostPart{N: int32(n), Spec: fields[1], Dyn: fields[2]})
						continue
					}
				}
				c.Generic = AddClampedGeneric(c.Generic, 1)
				c.reportUnknown(sym)
				continue
			}
			if m := damageYouCost.FindStringSubmatch(sym); m != nil {
				n, err := strconv.ParseInt(m[1], 10, 64)
				if err != nil || n < 0 || n > int64(math.MaxInt32) {
					// Same safe fallback as every other malformed cost token --
					// and REPORT it: the head is recognised, this instance is
					// not modelled.
					c.Generic = AddClampedGeneric(c.Generic, 1)
					c.reportUnknown(sym)
					continue
				}
				c.DamageYou = append(c.DamageYou, CostPart{N: int32(n), Desc: m[2]})
				continue
			}
			if m := gainLifeCost.FindStringSubmatch(sym); m != nil {
				n, err := strconv.ParseInt(m[1], 10, 64)
				if err != nil || n < 0 || n > int64(math.MaxInt32) {
					// Same safe fallback as every other malformed cost token --
					// and REPORT it: the head is recognised, this instance is
					// not modelled.
					c.Generic = AddClampedGeneric(c.Generic, 1)
					c.reportUnknown(sym)
					continue
				}
				c.GainLife = append(c.GainLife, CostPart{N: int32(n), Spec: m[2], Each: m[3] == "*"})
				continue
			}
			if m := exileCost.FindStringSubmatch(sym); m != nil {
				if m[2] == "X" {
					if m[1] != "FromGrave" {
						c.Generic = AddClampedGeneric(c.Generic, 1)
						c.reportUnknown(sym)
						continue
					}
					spec := strings.ReplaceAll(m[3], ";", ",")
					c.Exile = append(c.Exile, CostPart{Spec: spec, Zone: state.ZGraveyard, Announced: true, Desc: m[4]})
					continue
				}
				n, err := strconv.ParseInt(m[2], 10, 64)
				if err != nil || n < 0 || n > int64(math.MaxInt32) {
					// Same safe fallback as every other malformed cost token --
					// and REPORT it: the head is recognised, this instance is
					// not modelled.
					c.Generic = AddClampedGeneric(c.Generic, 1)
					c.reportUnknown(sym)
					continue
				}
				spec := strings.ReplaceAll(m[3], ";", ",")
				part := CostPart{N: int32(n), Spec: spec, Desc: m[4]}
				if m[1] != "FromHand" {
					part.Zone = state.ZGraveyard
				}
				c.Exile = append(c.Exile, part)
				continue
			}
			if m := addCounterCost.FindStringSubmatch(sym); m != nil {
				n, err := strconv.ParseInt(m[1], 10, 64)
				if err != nil || n < 0 || n > int64(math.MaxInt32) {
					// Same degrade-to-one-generic fallback as the other
					// malformed tokens -- and reported for the same reason.
					c.Generic = AddClampedGeneric(c.Generic, 1)
					c.reportUnknown(sym)
					continue
				}
				spec := strings.ReplaceAll(m[2], ";", ",")
				c.AddCounter = append(c.AddCounter, CostPart{N: int32(n), Spec: spec, Desc: m[3]})
				continue
			}
			if m := addSelfCounterCost.FindStringSubmatch(sym); m != nil {
				n, err := strconv.ParseInt(m[1], 10, 64)
				if err != nil || n < 0 || n > int64(math.MaxInt32) {
					c.Generic = AddClampedGeneric(c.Generic, 1)
					c.reportUnknown(sym)
					continue
				}
				c.AddCounter = append(c.AddCounter, CostPart{N: int32(n), Spec: m[2]})
				continue
			}
			if m := exertCost.FindStringSubmatch(sym); m != nil {
				c.Exert = append(c.Exert, CostPart{N: 1, Spec: "CARDNAME", Desc: m[1]})
				continue
			}
			if m := sacXCost.FindStringSubmatch(sym); m != nil {
				spec := strings.ReplaceAll(m[1], ";", ",")
				c.Sac = append(c.Sac, CostPart{Spec: spec, Announced: true, Desc: m[2]})
				continue
			}
			if m := payEnergyCost.FindStringSubmatch(sym); m != nil {
				if m[1] == "X" {
					// The dynamic form: the SAME X the cast announces.
					c.Energy = append(c.Energy, CostPart{Spec: "X", Desc: m[2]})
					continue
				}
				n, err := strconv.ParseInt(m[1], 10, 64)
				if err != nil || n < 0 || n > int64(math.MaxInt32) {
					// Same safe fallback as every other malformed cost token --
					// and REPORT it: the head is recognised, this instance is
					// not modelled.
					c.Generic = AddClampedGeneric(c.Generic, 1)
					c.reportUnknown(sym)
					continue
				}
				c.Energy = append(c.Energy, CostPart{N: int32(n), Desc: m[2]})
				continue
			}
			if m := returnCost.FindStringSubmatch(sym); m != nil {
				n, err := strconv.ParseInt(m[1], 10, 64)
				if err != nil || n < 0 || n > int64(math.MaxInt32) {
					// Same safe fallback as every other malformed cost token --
					// and REPORT it: the head is recognised, this instance is
					// not modelled.
					c.Generic = AddClampedGeneric(c.Generic, 1)
					c.reportUnknown(sym)
					continue
				}
				spec := strings.ReplaceAll(m[2], ";", ",")
				c.Return = append(c.Return, CostPart{N: int32(n), Spec: spec, Desc: m[3]})
				continue
			}
			if m := putCardToLibCost.FindStringSubmatch(sym); m != nil {
				n, err := strconv.ParseInt(m[2], 10, 64)
				if err != nil || n < 0 || n > int64(math.MaxInt32) {
					// Same safe fallback as every other malformed cost token --
					// and REPORT it: the head is recognised, this instance is
					// not modelled.
					c.Generic = AddClampedGeneric(c.Generic, 1)
					c.reportUnknown(sym)
					continue
				}
				pos, err := strconv.ParseInt(m[3], 10, 32)
				if err != nil || (pos != 0 && pos != -1) {
					// Only Forge's two modelled positions are real here: 0 (top)
					// and -1 (bottom). Any other value is a recognised head whose
					// instance this build cannot place, so it degrades and reports
					// rather than silently landing somewhere.
					c.Generic = AddClampedGeneric(c.Generic, 1)
					c.reportUnknown(sym)
					continue
				}
				part := CostPart{N: int32(n), Spec: strings.ReplaceAll(m[4], ";", ","),
					LibraryPos: int32(pos), Desc: m[5]}
				switch m[1] {
				case "Hand":
					part.Zone = state.ZHand
				case "Grave":
					part.Zone = state.ZGraveyard
				default: // Battlefield
					part.Zone = state.ZBattlefield
				}
				c.PutToLib = append(c.PutToLib, part)
				continue
			}
			// XMin<N> is the announced-X lower bound, NOT a payment: it adds
			// no generic mana and reports no Unknown. A malformed or
			// out-of-range instance keeps the ordinary one-generic fallback
			// and reports the recognised head, the PayLife<N> shape.
			if m := xMinCost.FindStringSubmatch(sym); m != nil {
				n, err := strconv.ParseInt(m[1], 10, 64)
				if err != nil || n < 0 || n > int64(math.MaxInt32) {
					c.Generic = AddClampedGeneric(c.Generic, 1)
					c.reportUnknown(sym)
					continue
				}
				if int32(n) > c.XMin {
					c.XMin = int32(n)
				}
				continue
			}
			// Try to parse as a numeric token. Negative and out-of-range values
			// fall through to the +1 generic fallback.
			if n, err := strconv.ParseInt(sym, 10, 64); err == nil && n >= 0 && n <= int64(math.MaxInt32) {
				c.Generic = AddClampedGeneric(c.Generic, n)
				continue
			}
			// An unrecognised symbol (including a malformed hybrid/Phyrexian
			// token) degrades to one generic mana, never a hard parse error -- and
			// is REPORTED as unmodelled (Cost.Unknown), so the parameter census
			// can name it instead of the substitution staying silent.
			c.reportUnknown(sym)
			c.Generic = AddClampedGeneric(c.Generic, 1)
		}
	}
	return c
}

// reportUnknown records the head of one degraded cost token in Unknown,
// in order, deduplicated. Used by the final unrecognised-symbol fallback AND
// by the malformed-instance branches of the recognised heads: both shapes
// priced one generic without real semantics, so both are unmodelled.
func (c *Cost) reportUnknown(sym string) {
	if i := strings.IndexByte(sym, '<'); i > 0 {
		sym = sym[:i]
	}
	if !slices.Contains(c.Unknown, sym) {
		c.Unknown = append(c.Unknown, sym)
	}
}

// splitCostTokens splits a cost string on whitespace, but keeps each <...>
// group atomic so Forge's non-mana tokens -- whose trailing "/description"
// can contain spaces -- are not torn apart by a plain Fields split before
// nonManaCost can see them. Ruling FL-54.
func splitCostTokens(s string) []string {
	var out []string
	for toks := (TokenIter{s: s}); ; {
		tok, ok := toks.Next()
		if !ok {
			return out
		}
		out = append(out, tok)
	}
}

// TokenIter walks a cost string's tokens the way ParseCost reads them:
// split on whitespace, with each <...> group kept atomic. It allocates
// nothing; NewTokenIter builds one over s.
type TokenIter struct {
	s   string
	pos int
}

// NewTokenIter returns a TokenIter over s.
func NewTokenIter(s string) TokenIter { return TokenIter{s: s} }

// Next returns the next token and true, or "" and false at the end.
func (it *TokenIter) Next() (string, bool) {
	for it.pos < len(it.s) {
		r, size := utf8.DecodeRuneInString(it.s[it.pos:])
		if !unicode.IsSpace(r) {
			break
		}
		it.pos += size
	}
	if it.pos == len(it.s) {
		return "", false
	}
	start, depth := it.pos, 0
	for it.pos < len(it.s) {
		r, size := utf8.DecodeRuneInString(it.s[it.pos:])
		if r == '<' {
			depth++
		} else if r == '>' && depth > 0 {
			depth--
		} else if unicode.IsSpace(r) && depth == 0 {
			tok := it.s[start:it.pos]
			it.pos += size
			return tok, true
		}
		it.pos += size
	}
	return it.s[start:], true
}

// isHybrid reports whether sym is a two-colour hybrid pip: either the
// slash form ("W/U") or the concatenated form ("GW", "WB"). A second
// character of 'P' is Phyrexian, not hybrid, and is handled by
// isPhyrexian. Both faces must be distinct WUBRGC mana symbols (a doubled
// letter, "WW", is not a hybrid — it is a script typo and degrades to
// generic). This includes colourless hybrid, such as {C/W}.
func isHybrid(sym string) bool {
	var a, b byte
	if len(sym) == 3 && sym[1] == '/' {
		a, b = sym[0], sym[2]
	} else if len(sym) == 2 && sym[1] != 'P' {
		a, b = sym[0], sym[1]
	} else {
		return false
	}
	return a != b && strings.ContainsRune("WUBRGC", rune(a)) && strings.ContainsRune("WUBRGC", rune(b))
}

// hybridPair normalises a hybrid symbol to its two colours as a ManaPair.
// sym is guaranteed a hybrid by isHybrid.
func hybridPair(sym string) ManaPair {
	if len(sym) == 3 && sym[1] == '/' {
		return ManaPair{A: sym[0], B: sym[2]}
	}
	return ManaPair{A: sym[0], B: sym[1]}
}

// isPhyrexian reports whether sym is a Phyrexian pip: a WUBRG colour
// followed by 'P', either slash ("W/P") or concatenated ("UP", "BP").
// CR 107.4f: pay that colour OR two life.
func isPhyrexian(sym string) bool {
	if len(sym) == 3 && sym[1] == '/' {
		return strings.ContainsRune("WUBRG", rune(sym[0])) && sym[2] == 'P'
	}
	if len(sym) != 2 {
		return false
	}
	return sym[1] == 'P' && strings.ContainsRune("WUBRG", rune(sym[0]))
}

// phyrexianColor returns the colour letter of a Phyrexian pip. sym is
// guaranteed a Phyrexian pip by isPhyrexian.
func phyrexianColor(sym string) byte {
	return sym[0]
}

// isTwobrid reports whether sym is a monocolour hybrid pip (CR 107.4e):
// a positive number followed by one colour — Forge's concatenated `2B`, or
// the slash form `2/B`. It may be paid with that many generic mana OR one
// mana of the colour. A `2/C` (the colourless-hybrid shape the client
// knows) parses the same way; the corpus carries none (measured), so the
// shape is parse-for-parity, not corpus-driven.
func isTwobrid(sym string) bool {
	a, b, ok := splitHybridSlash(sym)
	if ok {
		return IsDigitRun(a) && len(b) == 1 && strings.ContainsRune("WUBRGC", rune(b[0]))
	}
	// Concatenated form: leading digits then exactly one colour letter.
	i := 0
	for i < len(sym) && sym[i] >= '0' && sym[i] <= '9' {
		i++
	}
	return i > 0 && i == len(sym)-1 && strings.ContainsRune("WUBRGC", rune(sym[i]))
}

// twobridPair normalises a monocolour hybrid symbol. sym is guaranteed by
// isTwobrid.
func twobridPair(sym string) Twobrid {
	if a, b, ok := splitHybridSlash(sym); ok {
		n, _ := strconv.ParseInt(a, 10, 64)
		if n < 0 {
			n = 0
		}
		if n > int64(math.MaxInt32) {
			n = int64(math.MaxInt32)
		}
		return Twobrid{Generic: int32(n), Col: b[0]}
	}
	i := 0
	for i < len(sym) && sym[i] >= '0' && sym[i] <= '9' {
		i++
	}
	n, _ := strconv.ParseInt(sym[:i], 10, 64)
	if n > int64(math.MaxInt32) {
		n = int64(math.MaxInt32)
	}
	return Twobrid{Generic: int32(n), Col: sym[i]}
}

// isHybridPhyrexian reports whether sym is a three-part hybrid-Phyrexian
// pip (CR 107.4f): two distinct WUBRG colours and P. Forge writes the usual
// `GWP`/`G/W/P` spelling and also the P-first `PRG` spelling on Lukka, Bound
// to Ruin; both mean a choice of either colour or two life. Measured corpus
// population: 4 ManaCost files (Ajani Sleeper Agent, Lukka Bound to Ruin,
// Nahiri the Unforgiving, Tamiyo Compleated Sage).
func isHybridPhyrexian(sym string) bool {
	if a, b, ok := splitHybridSlash(sym); ok {
		if len(a) != 1 || len(b) != 3 || b[1] != '/' {
			return false
		}
		if a[0] == 'P' {
			return b[0] != b[2] && strings.ContainsRune("WUBRG", rune(b[0])) &&
				strings.ContainsRune("WUBRG", rune(b[2]))
		}
		return b[2] == 'P' && a[0] != b[0] && strings.ContainsRune("WUBRG", rune(a[0])) &&
			strings.ContainsRune("WUBRG", rune(b[0]))
	}
	if len(sym) != 3 {
		return false
	}
	if sym[0] == 'P' {
		return sym[1] != sym[2] && strings.ContainsRune("WUBRG", rune(sym[1])) &&
			strings.ContainsRune("WUBRG", rune(sym[2]))
	}
	return sym[2] == 'P' && sym[0] != sym[1] && strings.ContainsRune("WUBRG", rune(sym[0])) &&
		strings.ContainsRune("WUBRG", rune(sym[1]))
}

// hybridPhyrexianPair normalises a hybrid-Phyrexian symbol to its two
// colours. sym is guaranteed by isHybridPhyrexian.
func hybridPhyrexianPair(sym string) HybridPhyrexian {
	if a, b, ok := splitHybridSlash(sym); ok {
		if a[0] == 'P' {
			return HybridPhyrexian{A: b[0], B: b[2]}
		}
		return HybridPhyrexian{A: a[0], B: b[0]}
	}
	if sym[0] == 'P' {
		return HybridPhyrexian{A: sym[1], B: sym[2]}
	}
	return HybridPhyrexian{A: sym[0], B: sym[1]}
}

// splitHybridSlash reports whether sym is an `a/b` slash form and splits it.
// The b side may itself carry slashes (`G/W/P`), so it returns the whole
// remainder after the first slash.
func splitHybridSlash(sym string) (a, b string, ok bool) {
	i := strings.IndexByte(sym, '/')
	if i <= 0 || i == len(sym)-1 {
		return "", "", false
	}
	return sym[:i], sym[i+1:], true
}

// IsDigitRun reports whether s is a non-empty run of decimal digits.
func IsDigitRun(s string) bool {
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

// ParseUnlessCost strictly parses an UnlessCost$ value for the mid-resolution
// unless-pay path. Unlike ParseCost — which degrades every token it does not
// know to one generic mana, silently buying a dynamic or unmodelled cost for
// {1} — this parser is total and strict: every token must be a mana symbol
// (a WUBRGC letter or a numeric generic), a fixed PayLife<N>, a PayEnergy<N>
// or PayEnergy<X> energy part, a Return<N/Spec> component, the
// LifeTotalHalfUp token, a fixed Mill<N> component, a Sac<N/Spec>,
// Discard<N/Spec>, SubCounter<N/Kind>,
// Draw<N/Spec>, Reveal<N/Spec> or Exile<N/Spec> (including the zone-headed
// ExileFromGrave/ExileFromHand/ExileAnyGrave forms) component, or the
// Mandatory marker.
// Anything else — an unfolded X/Y/Z (UnlessCostResolved folds an announced X
// and resolvable SVar bodies first; an unbound X never prices here),
// DamageYou<N> (the Sacrifice arm's own path), BeholdExile<...>, tapXType<...>,
// CopyCost, or any prose —
// reports ok=false, and the unless-pay arm treats that as a hard decline
// (the conservative read: a payer who "pays" a cost the engine cannot price
// has not paid it). A Reveal component is choice-bearing like Sac/Discard
// and pays through the beginUnlessPayment continuation, never synchronously.
// Return and Exile components are choice-bearing the same way.
func ParseUnlessCost(s string) (Cost, bool) {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "no cost") {
		return Cost{}, true
	}
	s = costBraces.Replace(s)
	var c Cost
	for toks := (TokenIter{s: s}); ; {
		sym, ok := toks.Next()
		if !ok {
			break
		}
		if strings.EqualFold(sym, "Mandatory") {
			// Forge's mandatory-payment marker (Cost$ Mandatory tapXType<X/...>,
			// the cumulative-upkeep shapes): a marker, never a cost, exactly
			// ParseCost's reading (mana.go's EqualFold case above).
			continue
		}
		switch {
		case sym == "T" || sym == "X":
			// An unfolded X is never priceable here: payMana does not charge
			// it, so a "pay" from an empty pool would satisfy it for free.
			// (An announced X never reaches this branch raw: UnlessCostResolved
			// folds it to {N} first, including an announced zero.)
			return Cost{}, false
		case len(sym) == 1 && strings.ContainsAny(sym, "WUBRGC"):
			c.Colored[state.ManaIndex(sym[0])]++
		default:
			if n, err := strconv.Atoi(sym); err == nil && n >= 0 {
				c.Generic = AddClampedGeneric(c.Generic, int64(n))
				continue
			}
			if m := lifeCost.FindStringSubmatch(sym); m != nil {
				n, err := strconv.ParseInt(m[1], 10, 64)
				if err != nil || n < 0 || n > int64(math.MaxInt32) {
					return Cost{}, false
				}
				c.Life = AddClampedGeneric(c.Life, n)
				continue
			}
			if m := nonManaCost.FindStringSubmatch(sym); m != nil {
				n, err := strconv.ParseInt(m[2], 10, 64)
				if err != nil || n < 0 || n > int64(math.MaxInt32) {
					return Cost{}, false
				}
				// Fold Forge's ";" OR alternation into the "," MatchesSpec
				// already uses, so "Artifact;Creature" matches either.
				spec := strings.ReplaceAll(m[3], ";", ",")
				part := CostPart{N: int32(n), Spec: spec, Desc: m[4]}
				switch m[1] {
				case "Sac":
					c.Sac = append(c.Sac, part)
				case "Discard":
					c.Discard = append(c.Discard, part)
				case "Draw":
					c.Draw = append(c.Draw, part)
				default:
					c.SubCounter = append(c.SubCounter, part)
				}
				continue
			}
			if m := payEnergyCost.FindStringSubmatch(sym); m != nil {
				// The mid-resolution unless form of the cast cost's energy token:
				// a fixed part spends its N, the dynamic X form spends the
				// RESOLVING ability's announced X (CR 107.3i) — bound at the pay
				// sites from the ctx, and unpayable there when no X was ever
				// announced, so an unbound token can never pay for free.
				if m[1] == "X" {
					c.Energy = append(c.Energy, CostPart{Spec: "X", Desc: m[2]})
					continue
				}
				n, err := strconv.ParseInt(m[1], 10, 64)
				if err != nil || n < 0 || n > int64(math.MaxInt32) {
					return Cost{}, false
				}
				c.Energy = append(c.Energy, CostPart{N: int32(n), Desc: m[2]})
				continue
			}
			if m := returnCost.FindStringSubmatch(sym); m != nil {
				// The unless form of the cast cost's Return token (the
				// cumulative-upkeep family, Karoo's non-Lair land): a permanent
				// matching Spec returned to its OWNER's hand. Choice-bearing —
				// the unless payment continuation owns the pick exactly like
				// Sac/Discard, never a first-in-zone-order stand-in.
				n, err := strconv.ParseInt(m[1], 10, 64)
				if err != nil || n < 0 || n > int64(math.MaxInt32) {
					return Cost{}, false
				}
				c.Return = append(c.Return, CostPart{N: int32(n), Spec: strings.ReplaceAll(m[2], ";", ","), Desc: m[3]})
				continue
			}
			if strings.EqualFold(sym, "LifeTotalHalfUp") {
				// Temporal Extortion's "may pay half their life, rounded up":
				// the amount is the payer's own life total at pay time, so the
				// flag rides to the unless path's gate and charge (both fold it
				// against the same payer read). A zero-life payer cannot pay.
				c.LifeHalfUp = true
				continue
			}
			// Reveal<N/Spec> is the hideaway-family choice-bearing unless cost
			// (Primal Beyond, Port Town, Xyru Specter's Challenge): the payer
			// reveals N hand cards matching the spec. It parses like the other
			// component heads and PAYS through the beginUnlessPayment
			// continuation (payUnlessCost refuses it, exactly like Sac/Discard).
			//
			// Behold<N/Spec> (CR 702.176, Elven Passage's "you may behold an
			// Elf") is the same shape one zone wider: the payer elects a
			// permanent matching Spec they control OR a card matching Spec in
			// their hand. It parses here and PAYS through the same continuation,
			// with the candidate enumeration shared with the cast flow's Behold
			// cost so the offer and the settlement cannot disagree.
			//
			// BeholdExile<N/...> and tapXType<...> stay hard declines: no corpus
			// UnlessCost$ carries either, and BeholdExile's then-exile settlement
			// is a distinct behaviour that must land with its own test.
			if m := choiceCost.FindStringSubmatch(sym); m != nil {
				if m[1] != "Reveal" && m[1] != "Behold" {
					return Cost{}, false
				}
				n, err := strconv.ParseInt(m[2], 10, 64)
				if err != nil || n <= 0 || n > int64(math.MaxInt32) {
					return Cost{}, false
				}
				part := CostPart{N: int32(n), Spec: strings.ReplaceAll(m[3], ";", ","), Desc: m[4]}
				if m[1] == "Reveal" {
					c.Reveal = append(c.Reveal, part)
				} else {
					c.Behold = append(c.Behold, part)
				}
				continue
			}
			// A fixed Mill<N> is the choice-free mill-keep cost (Deep Spawn's
			// "sacrifice CARDNAME unless you mill two cards", the only corpus
			// UnlessCost$ carrying it). It parses through the SAME millCost
			// grammar as the ordinary cast/activation Cost$ Mill<N> and, per
			// CR 701.13a, is payable at ANY library size -- the payer mills
			// every remaining card when fewer than N remain, so zero cards is
			// not an unpayable cost. payUnlessCost settles it through the
			// shared payMillCost, so the two payment sites cannot diverge.
			// A dynamic or malformed spelling (Mill<X>, Mill<>, a prose head)
			// never matches here and stays a hard decline.
			if m := millCost.FindStringSubmatch(sym); m != nil {
				n, err := strconv.ParseInt(m[1], 10, 64)
				if err != nil || n < 0 || n > int64(math.MaxInt32) {
					return Cost{}, false
				}
				c.Mill = append(c.Mill, CostPart{N: int32(n)})
				continue
			}
			// Exile<N/Spec> / ExileFromGrave / ExileFromHand / ExileAnyGrave is
			// the choice-bearing exile unless cost (Grip of Amnesia's
			// ExileFromGrave<1/All>: "Counter target spell unless its
			// controller exiles all cards from their graveyard"). The zone
			// mapping is the SAME one the cast-cost parser applies: FromHand
			// stays the hand (zone zero), every other exile head is the
			// payer's graveyard. An unfolded X amount is never priceable
			// (UnlessCostResolved folds an announced X first), so it declines
			// exactly like every other dynamic token. The part PAYS through the
			// beginUnlessPayment continuation (payUnlessCost refuses it, exactly
			// like Sac/Discard): a whole-graveyard exile is choice-bearing, and
			// the offer gate reads isWholeZoneExileSpec so the All spec names
			// the whole zone rather than a filter no card matches.
			if m := exileCost.FindStringSubmatch(sym); m != nil {
				if m[2] == "X" {
					return Cost{}, false
				}
				n, err := strconv.ParseInt(m[2], 10, 64)
				if err != nil || n < 0 || n > int64(math.MaxInt32) {
					return Cost{}, false
				}
				part := CostPart{N: int32(n), Spec: strings.ReplaceAll(m[3], ";", ","), Desc: m[4]}
				if m[1] != "FromHand" {
					part.Zone = state.ZGraveyard
				}
				c.Exile = append(c.Exile, part)
				continue
			}
			// RevealChosen<Player>/<Type> is the no-ask designation reveal
			// (Stalking Leonin's activation cost). As an UnlessCost$ it is
			// accepted here too so the shared beginUnlessPayment continuation
			// settles it (one public Note on the paid path), never a synchronous
			// payUnlessCost call that would silently omit the reveal. No corpus
			// UnlessCost$ carries it today; the threading is what a future one
			// must not mis-route through.
			if m := revealChosenCost.FindStringSubmatch(sym); m != nil {
				c.RevealChosen = append(c.RevealChosen, CostPart{Spec: m[1], Desc: m[2]})
				continue
			}
			// Every other token — a dynamic amount, an unmodelled cost verb,
			// or prose — makes the whole cost unpriceable.
			return Cost{}, false
		}
	}
	return c, true
}

// waterbendCost matches the Waterbend<N> / Waterbend<X> additional cost.
var waterbendCost = regexp.MustCompile(`^Waterbend<(X|\d+)>$`)

// MatchWaterbend reports whether sym is a Waterbend<N> or Waterbend<X>
// token and returns its amount field ("X" or the digit run). The RaiseCost
// bridge (rules/raise_cost_extra.go) reads the same head ParseCost does.
func MatchWaterbend(sym string) (amount string, ok bool) {
	if m := waterbendCost.FindStringSubmatch(sym); m != nil {
		return m[1], true
	}
	return "", false
}

// MatchPayLife reports whether sym is the fixed life-payment token
// PayLife<N> and returns its digit run. The RaiseCost mana/life reader
// (rules/statics_costmods.go raiseFromCost) reads the same head ParseCost
// does.
func MatchPayLife(sym string) (digits string, ok bool) {
	if m := lifeCost.FindStringSubmatch(sym); m != nil {
		return m[1], true
	}
	return "", false
}
