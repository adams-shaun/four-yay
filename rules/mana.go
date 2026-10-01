package rules

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// CostPart is one non-mana cost component: Sac<N/Spec> (sacrifice N
// permanents matching Spec), Discard<N/Spec> (discard N matching cards), or
// SubCounter<N/Kind> (remove N counters of Kind from the source), or
// announced-count ExileFromGrave<X/Spec>.
type CostPart struct {
	N    int32
	Spec string
	// Referent is a source-relative object name bound by a granted ability.
	// For Exile<N/OriginalHost>, this is the grantor rather than the recipient
	// permanent that carries the activated ability.
	Referent state.ObjID
	// Zone is the zone an Exile cost part pays from: ZHand for an
	// ExileFromHand token (the default zero value), ZGraveyard for an
	// ExileFromGrave or ExileAnyGrave token. Sac/Discard/SubCounter parts
	// never read it.
	Zone state.Zone
	// Announced marks a variable-count Sac<X/Spec> or ExileFromGrave<X/Spec>
	// part: the player announces the count as the cast's X (CR 601.2b)
	// and exactly that many matching objects are paid. N is unused for an Announced part.
	Announced bool
	// Dyn is the non-literal amount token of a Draw part (Forge's
	// Draw<X/Spec>): N is unused and the count is resolved at payment from
	// the resolving source's SVar table by the named token (SVar:X for
	// Draw<X/...>), the way fixLifeXCost resolves an SVar-valued PayLife<X>.
	// A part whose SVar is absent or unresolvable fails closed -- the cost is
	// unpayable, never a silent zero draw. Empty for an ordinary literal
	// Draw<N/Spec>.
	//
	// On a TapPermanent part Dyn carries Forge's dynamic tapXType heads
	// instead: "X" (tapXType<X/Spec> -- the tap count announces the cast's
	// {X}, CR 601.2b: when the cost carries no other announce-bearing part
	// the tap election IS the announcement and binds Count$xPaid through the
	// pay-time CastInfo; when it does, the part settles exactly the announced
	// X) and "Any" (tapXType<Any/Spec> -- a free count that binds nothing;
	// paying the cost taps at least one matching permanent, so a spec no
	// candidate satisfies leaves the cost unpayable). N is unused for both.
	Dyn string
	// LibraryPos is the library slot a PutToLib cost part places the moved
	// card(s) at, read from Forge's <N/Pos/Spec> middle field: -1 is the
	// bottom (Forge CostPutCardToLib's "-1"), 0 is the top (its absent/
	// "0" default). It is unused by every other cost head, whose zero value
	// is inert.
	LibraryPos int32
	// Target is the removal-target filter of a SubCounter part's third field
	// (SubCounter<N|X/Kind/Target[/desc]>): the permanent WHOSE counters the
	// payment removes (Ghave, Guru of Spores' "remove a +1/+1 counter from a
	// creature you control"), matched with MatchesSpecFrom against the payer's
	// battlefield exactly like a Sac part's spec. Empty (and Forge's
	// CARDNAME/NICKNAME spellings, rules/cast.go's subCounterTargetsSource)
	// anchors the removal on the SOURCE itself -- the reading every two-field
	// SubCounter<N/Kind> token has always had. Forge writes the restriction
	// space-free and the display description prose, so a space-bearing third
	// field is a description, not a filter.
	Target string
	// Desc is Forge's trailing human-readable description field
	// (the ".../another creature" in Sac<1/Creature.Other/another creature>),
	// captured verbatim so a player-facing prompt can render prose instead of
	// the raw token (rules/mana.go's costPhrase). It is DISPLAY ONLY: no
	// payment path reads it, so a part with an empty Desc behaves exactly as
	// before. Every cost head that can carry a description captures it here;
	// a head whose regex has no description group (PayLife<N>, Blight<N>)
	// leaves it empty.
	Desc string
	// MinPower is the SET-level floor a withTotalPowerGE<N> group predicate
	// places on a TapPermanent part (Mossbridge Troll's "total power 10 or
	// greater", Crew's "total power N or greater"): the predicate constrains
	// the TAPPED SET's total power, not each candidate on its own, so it is
	// not a filter the per-object matcher can evaluate. stripGroupPowerFloor
	// moves it out of the spec at parse time -- into this field, so the
	// offer gate (rules/cast.go nonManaCastable) and the tap election
	// (Decision.MinSum over Option.Value = the candidate's current power)
	// enforce it against the set the payment actually taps. It is stripped
	// for the Any form and the literal N form; an X-form part keeps the
	// predicate in its spec, which fails closed as before (a floor whose
	// count the announcement fixes first would need re-pricing at settle --
	// no corpus carrier combines them).
	MinPower int32
	// ThenExile marks a Behold part spelled BeholdExile<N/Spec> (the
	// Lorwyn Champion cycle's "behold a Kithkin and exile it"): the beheld
	// object -- a permanent the payer controls or a card revealed from their
	// hand -- is then exiled as part of the same payment, linked to the
	// paying source (ExiledWith) so "return the exiled card" reads it.
	ThenExile bool
	// Each marks a GainLife part spelled GainLife<N/Player.../*>, Forge's
	// "each" marker on the trailing third field (Reverent Silence's "each
	// other player gains 6 life", Skyshroud Cutter's 5). It is preserved as
	// parsed data -- never re-derived from the Spec text at payment -- so the
	// prose and any future per-part reading agree with the token. It is
	// unused by every other head. Note this is a DESCRIPTION of the printed
	// wording, not the payment semantics: the payment pays every player the
	// Spec matches relative to the payer (which for Player.Other/* is exactly
	// the oracle), with or without the marker.
	Each bool
}

// ManaPair is one two-face hybrid symbol: each face is a WUBRGC mana symbol,
// and either one spells the pip (CR 107.4e).
type ManaPair struct{ A, B byte }

// Twobrid is one monocolour hybrid symbol (CR 107.4e, Forge's `2B` shape):
// it may be paid with Generic generic mana OR with one mana of colour Col.
type Twobrid struct {
	Generic int32
	Col     byte
}

// HybridPhyrexian is one three-part symbol (Forge's `GWP` shape, CR 107.4f):
// it may be paid with one mana of colour A, one of colour B, or two life.
type HybridPhyrexian struct{ A, B byte }

// Cost is a parsed cost. X counts how many "X" symbols appeared (almost
// always 0 or 1; WithX folds a chosen value into Generic once per symbol).
// Life, Tap, Sac, Discard and SubCounter are non-mana components a cast or
// activation must satisfy separately from mana payment; Exile<N/Spec> (from
// ExileFromHand/ExileFromGrave tokens) exiles matching cards as the payment;
// AddCounter<N/LOYALTY> is a free non-mana component (a planeswalker's [+N]
// loyalty gain) settled beside SubCounter by the ability branch in
// rules/cast.go. Life is paid through payMana's LifeChange event; Tap, Sac,
// Discard, SubCounter and Exile are settled by the cast-flow stages in
// rules/cast.go. Pay and CanPay remain pool-only helpers.
//
// Hybrid and Phyrexian symbols are no longer flattened to generic. A hybrid
// pip (GW) is recorded in Hybrid as the pair of colours it accepts; a
// Phyrexian pip (UP) is recorded in Phyrexian as its colour (CR 107.4f: pay
// that colour OR two life). Both are “announcement” costs: which half of a
// hybrid and whether a Phyrexian pip is paid with life is a player choice at
// cast time (CR 601.2b), not something a parser decides. Because a Phyrexian
// pip may be paid with life, the mana-only Pay/CanPay below cannot fully own
// it; rule/cast.go's payment stage resolves the announced choice and spends
// against both the pool and the payer's life (see Cost.payable).
type Cost struct {
	Colored state.Mana
	Generic int32
	Life    int32
	X       int
	// XMin is the LOWER BOUND an XMin<N> cost token places on the announced
	// X ("X can't be 0"): XMin1 means the cost's {X} must be at least 1.
	// It is not a payment -- it adds no generic mana and reports no Unknown
	// -- only a floor for xAsk's option list and the offer gate's minimum-X
	// price. XMin is a property of the shared announced X, so Plus takes the
	// max of the two bounds and WithX (the announcement) clears it.
	XMin            int32
	Hybrid          []ManaPair
	Phyrexian       []byte
	Twobrid         []Twobrid
	HybridPhyrexian []HybridPhyrexian
	Snow            int32
	// Waterbend carries the amount a Waterbend<N> cost token lets tapped
	// artifacts and creatures help pay (CR 701.67a): the {N} is already
	// folded into Generic by the same token (so Waterbend adds no charge of
	// its own, it is the CAP on how much of that generic the taps may
	// cover), and each untapped artifact or creature the payer taps while
	// paying the cost pays for {1}. WaterbendX marks a Waterbend<X> part,
	// whose amount is the announced {X} (the token also contributes that X
	// to X, exactly like a RaiseCost Waterbend<X>). The field is the shared
	// reading both an ability's own Cost$ (Giant Koi) and a spell's
	// additional part (the optional-cost Waterbend<4> family) expose, so the
	// offer gate can credit the taps and the payment can offer them.
	Waterbend  int32
	WaterbendX bool
	Tap        bool
	Untap      bool
	Sac        []CostPart
	Discard    []CostPart
	SubCounter []CostPart
	AddCounter []CostPart
	Exile      []CostPart
	// ExileFromTop carries Forge's ExileFromTop<N/Card> parts -- exiling the
	// top N cards of the payer's OWN library as a cast/activation cost (Storm
	// Elemental, Phyrexian Devourer, Arc-Slogger, Whirling Catapult). It is a
	// DISTINCT slice from Exile on purpose: state.ZLibrary is the zero Zone,
	// and CostPart.Zone's zero value means "the hand" to every hand/grave
	// exile path, so a library part recorded in Exile would be read back as a
	// hand exile. The library is ordered, so the payment takes the actual top
	// N cards and never poses a chooser.
	ExileFromTop []CostPart
	Reveal       []CostPart
	// RevealOrChoose carries Forge's either-or `RevealOrChoose<N/Spec>` cost
	// (Monstrous Emergence, Dragon's Fire): reveal N hand cards matching Spec
	// OR choose N permanents matching Spec you control. It is deliberately a
	// DISTINCT slice from Reveal: the reveal arm is a real hand-card reveal
	// (public Note, the card rides the Revealed paid list), while the choose
	// arm elects a permanent already on the battlefield -- a different
	// provenance that must not be read as a hand reveal. Both arms' elected
	// objects land in the same paid list the `Revealed$<Property>` refs read
	// (Forge's CostReveal owns both arms), but only the hand arm is announced
	// as a reveal. Spec serves both arms (the card's own chooser reads one
	// type for the hand card and the permanent).
	RevealOrChoose []CostPart
	// RevealChosen carries RevealChosen<Player> and RevealChosen<Type/...>
	// components (Stalking Leonin, Guardian Archon, Emissary of Grudges, A
	// Killer Among Us): the payer publicly reveals a designation that was
	// chosen SECRETLY earlier in the game (a player, or a creature type).
	// Unlike Reveal<N/Spec> there is NO hand choice and no mana -- the whole
	// part is free -- so the payability gate is the designation's presence on
	// the ability's source, and the payment is one public Note. Spec is
	// "Player" or "Type".
	RevealChosen []CostPart
	Behold       []CostPart
	TapPermanent []CostPart
	// UntapPermanent carries untapYType<N/Spec> parts -- untapping N
	// permanents matching Spec as a cost (Benthic Explorers' "{T}, Untap a
	// tapped land an opponent controls: Add one mana of any type that land
	// could produce"). It is DISTINCT from Cost.Untap (the {Q} symbol,
	// untapping the SOURCE itself): this head untaps OTHER permanents the
	// spec names, so the settle elects them like the literal tapXType form.
	// The trailing "/description" is captured into CostPart.Desc.
	UntapPermanent []CostPart
	Blight         []CostPart
	// Exert carries Exert<1/CARDNAME> parts (CR 701.39: the source will not
	// untap during its controller's next untap step). Paid by one
	// events.Exert on the source -- the same event the declare-attackers
	// exert election emits, so "whenever you exert" triggers and the untap
	// skip read one fold. Only the source-anchored form is modelled; any
	// other spelling keeps the reported one-generic fallback.
	Exert  []CostPart
	Forage bool
	// Draw carries Draw<N/Spec> components: paying one draws N cards for the
	// player(s) the spec names (default the payer). payMana never charges it;
	// the mid-resolution unless-pay path pays it (payUnlessCost), and the
	// cast/activation flow pays the payer's own parts beside the other
	// non-mana components.
	Draw   []CostPart
	Energy []CostPart
	// LifeX carries the announced PayLife<X> part (Toxic Deluge's "pay X
	// life"): the cast announces X (CR 601.2b, bounded by the payer's life
	// total) and the settle pays it as one LifeChange per part beside
	// payMana's fixed Life charge. PayLife<N> is the fixed part and lives in
	// Life above; a malformed PayLife<...> value still degrades to the
	// reported one-generic fallback.
	LifeX []CostPart
	// LifeHalfUp marks a LifeTotalHalfUp token (Temporal Extortion's "may pay
	// half their life, rounded up"): the amount is the PAYER's life total at
	// pay time, half rounded up, so the strict parser cannot fold it — the
	// unless path's gate and charge sites resolve it against the payer
	// (unlessFoldDynamicLife) before the mana/life machinery reads Life.
	// A payer at zero life cannot pay (nothing to halve — fail closed).
	LifeHalfUp bool
	// DamageYou carries DamageYou<N> parts -- the payer takes N damage from
	// the source. The corpus's only shape is an UnlessCost$ DamageYou<N>
	// (the Vexing Devil family), which the unless-pay arm pays through
	// effects.ParseDamageUnlessCost; the head is modelled here so ParseCost
	// stops substituting generic mana for it, and a plain Cost$ part is
	// settled by the cast flow like every other damage payment.
	DamageYou []CostPart
	// GainLife carries GainLife<N/Player...> parts -- the PAYER has the named
	// player(s) gain N life as the payment (Forge CostGainLife; the
	// Invigorate/Reverent Silence/Skyshroud Cutter AlternativeCost family,
	// "rather than pay this spell's mana cost, you may have an opponent gain
	// N life"). It is a payment of the cost, not an effect on the payer:
	// each part emits one POSITIVE LifeChange per player the part's Spec
	// matches relative to the payer, routed through the ordinary
	// applyLifeReplacements machinery (CR 616 GainLife replacements apply).
	// Spec keeps Forge's raw player word (Player.Opponent / Player.Other);
	// Each is the trailing `/*` marker (see CostPart.Each) for the
	// "each other player" spelling. Before the head existed the token hit
	// the unrecognised-symbol fallback -- a phantom generic pip and the
	// whole alternative withheld (altCostParse fails closed on Unknown).
	GainLife []CostPart
	// Return carries Return<N/Spec> tokens: a permanent (usually the source
	// itself, Spec CARDNAME) returned to its OWNER's hand as the payment
	// (Forge CostReturn.moveToHand; CR 118.2a lists returning a permanent to
	// its owner's hand among the payment actions).
	Return []CostPart
	// PutToLib carries PutCardToLibFrom<Zone><N/Pos/Spec> tokens: the payer
	// moves N cards matching Spec from Zone (Spec's Forge head names Hand,
	// Grave or Battlefield) to their OWN library at position Pos (-1 bottom,
	// 0 top). It is Forge's CostPutCardToLib family -- the printed activation
	// costs of Leashling, Ardent Dustspeaker, Battlefield Scrounger,
	// Timestream Navigator and Penance/Tainted Specter's UnlessCost$ -- and
	// is DISTINCT from the cumulative-upkeep PutCardToLibFromSameGrave action
	// (rules/cumulative.go), which is a keyword-expansion action rather than
	// a parsed cost token. Zone is the origin, LibraryPos the position.
	PutToLib []CostPart
	// MoveToGrave carries ExiledMoveToGrave<N/Spec> tokens: N cards matching
	// Spec move out of EXILE into their OWNER's graveyard as the payment
	// (Forge CostExiledMoveToGrave; the Eldrazi processor family's "put a
	// card an opponent owns from exile into that player's graveyard" and
	// Shelob, Dread Weaver's "put a creature card exiled with Shelob into its
	// owner's graveyard"). The origin is always every player's exile zone
	// -- exiled cards live in their OWNER's exile zone (events/apply.go's
	// zoneOwner), so a candidate scan iterates the players -- and the
	// destination is the owner's graveyard, which is why this is its own
	// slice rather than an Exile part (whose Zone is the ORIGIN and whose
	// destination is always exile). The specs in measured use are
	// Card.OppOwn (12 lines) and Card/Creature.ExiledWithSource (4 lines);
	// both read through the ordinary filter grammar (effects/filter.go's
	// OppOwn and ExiledWithSource predicates, bound to the PAYER and the
	// ability's source).
	MoveToGrave []CostPart
	// Mill carries Mill<N> cost components paid from the payer's library.
	Mill []CostPart
	// Evidence carries CollectEvidence<N> and CollectEvidence<NAME> tokens
	// (alltargeted1): the payer exiles cards from their OWN graveyard whose
	// total mana value reaches N or greater (CR 701.30b's collect evidence;
	// the same action the Ward payment path pays). N is a literal, or a name
	// (Urgent Necropsy's X) resolved through the source face's SVar table
	// AFTER the CR 601.2c targets exist -- the corpus carrier's body reads
	// AllTargeted$CardManaCost over the whole root/sub-ability target union,
	// so the resolution needs the pre-asked sub-ability targets too. The
	// payment is the evidenceAsk stage (cast.go), which runs after the
	// target stages for exactly that reason; at OFFER time the amount is
	// unknowable, so nonManaCastable deliberately does not gate on this
	// part (fail-open there) and the payment stage aborts (CR 733.1) when
	// the graveyard cannot reach the resolved total.
	Evidence []CostPart
	// RollDice carries RollDice<N/Sides/XVar> free cost components. The payment
	// rolls each die and publishes its canonical effects.DieRollNote; Dyn names
	// the ability X binding (currently only X is modelled).
	RollDice []CostPart
	// Withheld lists the parts of a RaiseCost static's Cost$ that the
	// payment machinery cannot pay (a head no stage settles, a named count
	// with no resolvable reading). It is never produced by ParseCost: only
	// the RaiseCost bridge (rules/raise_cost_extra.go) writes it, and
	// nonManaCastable refuses any cost that carries it, so the spell or
	// ability is withheld at the offer gate instead of being offered with
	// the additional cost silently dropped.
	Withheld []string
	// Unknown lists the HEAD (the text before any "<...>") of every cost
	// token this parse did not model, in order of appearance, deduplicated.
	// A token lands here exactly when ParseCost could not give it real
	// semantics and priced it as one generic mana (or one life-equivalent
	// of nothing) instead: the final unrecognised-symbol fallback AND the
	// malformed/out-of-range instances of otherwise-recognised heads (an
	// unparseable or int-overflow "PayLife<...>", "Sac<...>",
	// "AddCounter<...>" value — the head is known, that INSTANCE is not
	// modelled). Payment behaviour is unchanged by this field: it is a pure
	// report, read by the parameter census (rules/paramcensus_test.go) so
	// the repo-deck ratchet can name cost tokens a card's script carries
	// that the engine silently substitutes generic mana for (e.g. Chthonian
	// Nightmare's "PayEnergy<X> ... Return<1/CARDNAME>").
	Unknown []string
}

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

// isWholeHandRevealSpec reports whether a Reveal cost part's type slot names
// the WHOLE hand rather than a card filter. Forge spells "reveal your hand"
// as Reveal<N/Hand> (Land Grant's free-cast cost, Sasaya, Orochi Ascendant's
// flip cost) -- the count is display noise, the same reading
// discardCandidates gives Discard<1/Hand> and Discard<0/Hand> -- and no card
// ever matches the bare word "Hand" as a filter, so reading it as one leaves
// the cost permanently unpayable and every carrier unplayable. Revealing an
// empty hand is legal (CR 701.20a), so a whole-hand reveal is payable with
// ANY hand, including an empty one.
func isWholeHandRevealSpec(spec string) bool {
	return strings.EqualFold(spec, "Hand")
}

// isSameColorRevealSpec reports whether a Reveal cost part's type slot is the
// RELATIONAL "SameColor" (Reveal<2/SameColor>, Illuminated Folio's "Reveal
// two cards from your hand that share a color"). SameColor names a relation
// BETWEEN the revealed cards, not a card property: no card's type line
// carries it, so reading it as one left the ability permanently unoffered.
// The reading lives beside the offer gate and the payment ask
// (rules/cast.go's nonManaCastable / revealCostAsk) as a decision.SetPropShared
// constraint over each candidate's DERIVED colour tokens -- see
// sameColorRevealSets in rules/setprops.go.
func isSameColorRevealSpec(spec string) bool {
	return strings.EqualFold(spec, "SameColor")
}

// isWholeZoneExileSpec reports whether an Exile cost part's type slot names
// the WHOLE zone rather than a card filter. Forge spells "exile your hand"
// (Herigast, Erupting Nullkite) as ExileFromHand<N/All> and "exile all cards
// from their graveyard" (Grip of Amnesia) as ExileFromGrave<N/All> -- the
// count is display noise, the same reading isWholeHandRevealSpec gives
// Reveal<N/Hand> and discardCandidates gives Discard<N/Hand>. No card ever
// matches the bare word "All" as a filter, so reading it as one leaves the
// cost permanently unpayable and every carrier's pay window decline-only.
// Unlike the whole-hand reveal, an ALL-zone exile is NOT payable empty: the
// token still demands part.N cards (the corpus writes 1), so a zone holding
// fewer than part.N cards cannot pay.
func isWholeZoneExileSpec(spec string) bool {
	return strings.EqualFold(spec, "All")
}

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

// millCostTotal returns the total number of cards every Mill<N> cost
// component requires (0 when there is no Mill part). The total is int64 so
// the sum of arbitrary parts cannot overflow int on a 32-bit build; ok is
// false for a negative requirement, which can never be paid.
func millCostTotal(parts []CostPart) (int64, bool) {
	var total int64
	for _, part := range parts {
		if part.N < 0 {
			return 0, false
		}
		total += int64(part.N)
	}
	return total, true
}

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
	for toks := (costTokenIter{s: s}); ; {
		sym, ok := toks.next()
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
					c.Generic = addClampedGeneric(c.Generic, 1)
					c.reportUnknown(sym)
					continue
				}
				c.Generic = addClampedGeneric(c.Generic, n)
				c.Waterbend = addClampedGeneric(c.Waterbend, n)
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
					c.Generic = addClampedGeneric(c.Generic, 1)
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
					c.Generic = addClampedGeneric(c.Generic, 1)
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
					c.Generic = addClampedGeneric(c.Generic, 1)
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
					c.Generic = addClampedGeneric(c.Generic, 1)
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
					c.Generic = addClampedGeneric(c.Generic, 1)
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
					c.Generic = addClampedGeneric(c.Generic, 1)
					c.reportUnknown(sym)
					continue
				}
				c.Life = addClampedGeneric(c.Life, n)
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
					c.Generic = addClampedGeneric(c.Generic, 1)
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
					c.Generic = addClampedGeneric(c.Generic, 1)
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
					c.Generic = addClampedGeneric(c.Generic, 1)
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
					c.Generic = addClampedGeneric(c.Generic, 1)
					c.reportUnknown(sym)
					continue
				}
				n, err := strconv.ParseInt(m[1], 10, 64)
				if err != nil || n <= 0 || n > int64(math.MaxInt32) {
					c.Generic = addClampedGeneric(c.Generic, 1)
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
					c.Generic = addClampedGeneric(c.Generic, 1)
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
					c.Generic = addClampedGeneric(c.Generic, 1)
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
				c.Generic = addClampedGeneric(c.Generic, 1)
				c.reportUnknown(sym)
				continue
			}
			if m := damageYouCost.FindStringSubmatch(sym); m != nil {
				n, err := strconv.ParseInt(m[1], 10, 64)
				if err != nil || n < 0 || n > int64(math.MaxInt32) {
					// Same safe fallback as every other malformed cost token --
					// and REPORT it: the head is recognised, this instance is
					// not modelled.
					c.Generic = addClampedGeneric(c.Generic, 1)
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
					c.Generic = addClampedGeneric(c.Generic, 1)
					c.reportUnknown(sym)
					continue
				}
				c.GainLife = append(c.GainLife, CostPart{N: int32(n), Spec: m[2], Each: m[3] == "*"})
				continue
			}
			if m := exileCost.FindStringSubmatch(sym); m != nil {
				if m[2] == "X" {
					if m[1] != "FromGrave" {
						c.Generic = addClampedGeneric(c.Generic, 1)
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
					c.Generic = addClampedGeneric(c.Generic, 1)
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
					c.Generic = addClampedGeneric(c.Generic, 1)
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
					c.Generic = addClampedGeneric(c.Generic, 1)
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
					c.Generic = addClampedGeneric(c.Generic, 1)
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
					c.Generic = addClampedGeneric(c.Generic, 1)
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
					c.Generic = addClampedGeneric(c.Generic, 1)
					c.reportUnknown(sym)
					continue
				}
				pos, err := strconv.ParseInt(m[3], 10, 32)
				if err != nil || (pos != 0 && pos != -1) {
					// Only Forge's two modelled positions are real here: 0 (top)
					// and -1 (bottom). Any other value is a recognised head whose
					// instance this build cannot place, so it degrades and reports
					// rather than silently landing somewhere.
					c.Generic = addClampedGeneric(c.Generic, 1)
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
					c.Generic = addClampedGeneric(c.Generic, 1)
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
				c.Generic = addClampedGeneric(c.Generic, n)
				continue
			}
			// An unrecognised symbol (including a malformed hybrid/Phyrexian
			// token) degrades to one generic mana, never a hard parse error -- and
			// is REPORTED as unmodelled (Cost.Unknown), so the parameter census
			// can name it instead of the substitution staying silent.
			c.reportUnknown(sym)
			c.Generic = addClampedGeneric(c.Generic, 1)
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
	for toks := (costTokenIter{s: s}); ; {
		tok, ok := toks.next()
		if !ok {
			return out
		}
		out = append(out, tok)
	}
}

type costTokenIter struct {
	s   string
	pos int
}

func (it *costTokenIter) next() (string, bool) {
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

// addClampedGeneric adds n to the int32 generic count, saturating at
// math.MaxInt32 on top and refusing to go below zero, so no accumulation of
// numeric tokens (nor a WithX fold) can ever wrap Generic negative. Task 20.
func addClampedGeneric(v int32, n int64) int32 {
	total := int64(v) + n
	if total > math.MaxInt32 {
		return math.MaxInt32
	}
	if total < 0 {
		return 0
	}
	return int32(total)
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
		return isDigitRun(a) && len(b) == 1 && strings.ContainsRune("WUBRGC", rune(b[0]))
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

// isDigitRun reports whether s is a non-empty run of decimal digits.
func isDigitRun(s string) bool {
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

func (c Cost) CMC() int32 {
	// A monocolour hybrid's mana value is its generic face, not one: {2/W}
	// has mana value 2 (CR 202.4b). This matters to SetCost/MinMana floors
	// before its payment face is announced.
	twobrid := int32(0)
	for _, t := range c.Twobrid {
		twobrid = addClampedGeneric(twobrid, int64(t.Generic))
	}
	return c.Colored.Total() + c.Generic + int32(len(c.Hybrid)) + int32(len(c.Phyrexian)) +
		twobrid + int32(len(c.HybridPhyrexian)) + c.Snow
}

// WithX folds a chosen X value into Generic, once per X symbol the cost
// carried, then clears X: once a value is chosen, {X} is no longer a
// distinct requirement, it is simply that much more generic mana.
func (c Cost) WithX(x int32) Cost {
	c.Generic = addClampedGeneric(c.Generic, int64(c.X)*int64(x))
	c.X = 0
	// The lower bound is consumed by the announcement: once an X is chosen
	// the bound has served its purpose, and clearing it keeps a later
	// WithX from double-charging the floor as if it were generic mana.
	c.XMin = 0
	return c
}

// Plus sums two costs (Kicker's own cost added to the card's printed cost):
// colours, generic and life add, X counts add, Tap ORs, and each side's
// non-mana parts concatenate.
func (c Cost) Plus(d Cost) Cost {
	for i := range c.Colored {
		c.Colored[i] += d.Colored[i]
	}
	c.Generic += d.Generic
	c.Life = addClampedGeneric(c.Life, int64(d.Life))
	c.X += d.X
	// The shared announced X takes the higher of the two lower bounds
	// (Thieving Skydiver: the printed cost carries none, the kicked Kicker
	// part carries XMin1, so the composed cast's X must be at least 1).
	if d.XMin > c.XMin {
		c.XMin = d.XMin
	}
	c.Tap = c.Tap || d.Tap
	c.Untap = c.Untap || d.Untap
	if len(d.Hybrid) > 0 {
		c.Hybrid = append(append([]ManaPair(nil), c.Hybrid...), d.Hybrid...)
	}
	if len(d.Phyrexian) > 0 {
		c.Phyrexian = append(append([]byte(nil), c.Phyrexian...), d.Phyrexian...)
	}
	if len(d.Twobrid) > 0 {
		c.Twobrid = append(append([]Twobrid(nil), c.Twobrid...), d.Twobrid...)
	}
	if len(d.HybridPhyrexian) > 0 {
		c.HybridPhyrexian = append(append([]HybridPhyrexian(nil), c.HybridPhyrexian...), d.HybridPhyrexian...)
	}
	c.Snow = addClampedGeneric(c.Snow, int64(d.Snow))
	if len(d.Sac) > 0 {
		c.Sac = append(append([]CostPart(nil), c.Sac...), d.Sac...)
	}
	if len(d.Discard) > 0 {
		c.Discard = append(append([]CostPart(nil), c.Discard...), d.Discard...)
	}
	if len(d.SubCounter) > 0 {
		c.SubCounter = append(append([]CostPart(nil), c.SubCounter...), d.SubCounter...)
	}
	if len(d.AddCounter) > 0 {
		c.AddCounter = append(append([]CostPart(nil), c.AddCounter...), d.AddCounter...)
	}
	if len(d.Exile) > 0 {
		c.Exile = append(append([]CostPart(nil), c.Exile...), d.Exile...)
	}
	if len(d.ExileFromTop) > 0 {
		c.ExileFromTop = append(append([]CostPart(nil), c.ExileFromTop...), d.ExileFromTop...)
	}
	if len(d.MoveToGrave) > 0 {
		c.MoveToGrave = append(append([]CostPart(nil), c.MoveToGrave...), d.MoveToGrave...)
	}
	if len(d.Mill) > 0 {
		c.Mill = append(append([]CostPart(nil), c.Mill...), d.Mill...)
	}
	if len(d.Evidence) > 0 {
		c.Evidence = append(append([]CostPart(nil), c.Evidence...), d.Evidence...)
	}
	if len(d.Reveal) > 0 {
		c.Reveal = append(append([]CostPart(nil), c.Reveal...), d.Reveal...)
	}
	if len(d.RevealOrChoose) > 0 {
		c.RevealOrChoose = append(append([]CostPart(nil), c.RevealOrChoose...), d.RevealOrChoose...)
	}
	if len(d.RevealChosen) > 0 {
		c.RevealChosen = append(append([]CostPart(nil), c.RevealChosen...), d.RevealChosen...)
	}
	if len(d.Behold) > 0 {
		c.Behold = append(append([]CostPart(nil), c.Behold...), d.Behold...)
	}
	if len(d.TapPermanent) > 0 {
		c.TapPermanent = append(append([]CostPart(nil), c.TapPermanent...), d.TapPermanent...)
	}
	if len(d.UntapPermanent) > 0 {
		c.UntapPermanent = append(append([]CostPart(nil), c.UntapPermanent...), d.UntapPermanent...)
	}
	if len(d.Blight) > 0 {
		c.Blight = append(append([]CostPart(nil), c.Blight...), d.Blight...)
	}
	if len(d.Exert) > 0 {
		c.Exert = append(append([]CostPart(nil), c.Exert...), d.Exert...)
	}
	if len(d.Energy) > 0 {
		c.Energy = append(append([]CostPart(nil), c.Energy...), d.Energy...)
	}
	if len(d.Return) > 0 {
		c.Return = append(append([]CostPart(nil), c.Return...), d.Return...)
	}
	if len(d.PutToLib) > 0 {
		c.PutToLib = append(append([]CostPart(nil), c.PutToLib...), d.PutToLib...)
	}
	if len(d.Draw) > 0 {
		c.Draw = append(append([]CostPart(nil), c.Draw...), d.Draw...)
	}
	if len(d.LifeX) > 0 {
		c.LifeX = append(append([]CostPart(nil), c.LifeX...), d.LifeX...)
	}
	if len(d.DamageYou) > 0 {
		c.DamageYou = append(append([]CostPart(nil), c.DamageYou...), d.DamageYou...)
	}
	if len(d.GainLife) > 0 {
		c.GainLife = append(append([]CostPart(nil), c.GainLife...), d.GainLife...)
	}
	c.Forage = c.Forage || d.Forage
	c.Waterbend = addClampedGeneric(c.Waterbend, int64(d.Waterbend))
	c.WaterbendX = c.WaterbendX || d.WaterbendX
	if len(d.Withheld) > 0 {
		c.Withheld = append(append([]string(nil), c.Withheld...), d.Withheld...)
	}
	return c
}

// commanderTaxFor composes base with the CR 903.8 commander tax: an
// additional generic {2} -- on top of everything else -- for each previous
// time id has been cast from the command zone this game. It is the ONE place
// a command-zone cast's extra cost is composed, and BOTH sides of the
// two-sided gate apply it to the same base: the legality offer (the
// command-zone walk in rules/legal.go calls commanderTaxFor(base) before
// castable) and the payment (rules/cast.go's beginCast applies
// commanderTaxFor to the cost it resolves). Because both derive the taxed
// cost from this same function over
// the same board, an offered command-zone cast and the cost it charges are
// structurally incapable of disagreeing -- the no-progress spin
// stalledCastLimit bounds is exactly what that disagreement used to cause.
//
// base is expected to be the already-reduced output of adjustedCost (kicker/
// surge/flashback recast it afterwards, but a card in the command zone is
// only ever cast here by its plain cost). Applying the tax to base means cost
// reductions land BEFORE it and never spill onto it: an additional cost is
// outside a plain "cost to cast" reduction, exactly how Kicker's own
// additional {N} composes in beginCast. Colored requirements are untouched.
//
// The Commander-format gate is explicit, not incidental: outside the format
// -- even when a card sits in the (usually empty) command zone -- base passes
// through unchanged. A card that is not its owner's commander, or a commander
// no longer in the command zone, is likewise untaxed.
func (e *Engine) commanderTaxFor(p state.PlayerID, id state.ObjID, base Cost) Cost {
	base.Generic += e.commanderTaxAmount(p, id)
	return base
}

// commanderTaxAmount is the CR 903.8 commander-tax generic amount for a
// command-zone commander cast by p: 2 per prior command-zone cast of id, 0
// for every other card, zone or format. It is the single O(1) tax read; both
// commanderTaxFor (the offer side) and beginCast's taxGeneric capture use it.
func (e *Engine) commanderTaxAmount(p state.PlayerID, id state.ObjID) int32 {
	if e.format != FormatCommander {
		return 0
	}
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZCommand {
		return 0
	}
	for k, cid := range e.G.Players[p].Commanders {
		if cid != id {
			continue
		}
		if n := e.G.Players[p].CmdCasts[k]; n > 0 {
			return 2 * n
		}
		return 0
	}
	return 0
}

// rawBaseCost is id's printed mana cost, without any cost modifier applied:
// the CR 601.2f "mana cost or alternative cost" basis onto which the chosen
// {X} and the RaiseCost/ReduceCost composition (manaToPay) are built. A
// missing object or a Face()-less one degrades to the zero Cost rather than
// panicking, matching adjustedCost's own guard.
func (e *Engine) rawBaseCost(p state.PlayerID, id state.ObjID) Cost {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return Cost{}
	}
	return e.faceCost(o.Face())
}

// castOfferBase is the composed RAW base every ordinary cast offer is gated on:
// the printed mana cost with CR 702.51 Convoke's creatures and CR 702.66
// Improvise's artifacts credited as generic, in that order (improviseCost
// excludes convokeTaps so one permanent is never committed twice). It is ONE
// helper so the plain cast offer (rules/legal.go's hand walk) and the
// MayFlashCost alternate offer (rules/mayflash.go) cannot drift: the mayflash
// offer charges the same base PLUS its extra cost, and both must price the
// printed cost identically. Returns a value (not the committed taps) because
// the offer gate only needs the credit; the actual commitment is re-derived
// per cast by convokeAsk.
func (e *Engine) castOfferBase(p state.PlayerID, id state.ObjID) Cost {
	// Without either keyword both credits are the identity (convokeCost and
	// improviseCost return the cost they were handed), so the raw base is
	// the answer and the two Cost round trips are skipped.
	if !e.hasCastConvoke(id) && !e.hasCastImprovise(id) {
		return e.rawBaseCost(p, id)
	}
	base, taps := e.convokeCost(p, id, e.rawBaseCost(p, id))
	base, _ = e.improviseCost(p, id, base, taps)
	return base
}

// offerCostFor is the CR 601.2f-composed cost an offer is gated on: the
// selected base cost (a spell's printed mana cost, or an alternative/
// flashback/surge/kicker cost) with RaiseCost then ReduceCost applied to
// Generic, and (for a spell) the CR 903.8 commander tax added last because an
// additional cost is never reduced. {X} is not yet chosen at offer time, so it
// contributes zero generic here and is not reduced; the offer stays
// conservative (a spell offering itself is withheld only when even X=0 is
// unpayable) while the actual charge (manaToPay) applies the modifiers after
// X is folded -- the two never disagree on a card with no {X} in its cost.
func (e *Engine) offerCostFor(p state.PlayerID, id state.ObjID, base Cost, scope costScope) Cost {
	return e.offerCostForUsing(e.collectCostStatics(), p, id, base, scope)
}

func (e *Engine) offerCostForUsing(statics costStaticViews, p state.PlayerID, id state.ObjID, base Cost, scope costScope) Cost {
	return e.composedOfferCost(p, id, base, e.costModifiersWithTargetsUsing(statics, p, id, scope, nil, false), scope)
}

// composedOfferCost is offerCostFor with the modifier collection factored
// out, so offerCastable can evaluate the composition once and reuse it for
// both the per-face enumeration and the composed castable check.
func (e *Engine) composedOfferCost(p state.PlayerID, id state.ObjID, base Cost, mods costMods, scope costScope) Cost {
	c := mods.apply(base)
	if scope.kind != "Ability" && scope.kind != "Foretell" && scope.kind != "Static" {
		c = e.commanderTaxFor(p, id, c)
	}
	return c
}

// offerCastable is THE offer-side gate every cast/activation option is gated
// on. base is the RAW (pre-modifier) cost beginCast stores in pendingCast
// for this exact option and scope the costScope its modifiers are collected
// with, so the gate composes the very charge the payment will make: the
// scope's CR 601.2f modifiers over base, then (for a spell) the CR 903.8
// commander tax, never reduced by either.
//
// The mana feasibility question is posed over the still-unresolved flexible
// pip faces (costMods.feasibleAny): with a SetCost/MinMana floor in the
// composition, CR 202.4b's generic-face mana value of an unresolved twobrid
// pip can overprice the cheaper face the announcement resolves it to, and a
// composed-payable offer would then have no legal announcement. castable on
// the composed cost runs on top, supplying the non-mana parts (Sac/Discard/
// SubCounter/Tap) the enumeration does not model; composed and per-face
// feasibility are conjunctive, and the stricter composed answer can only
// withhold a legal offer (the safe direction), never offer an illegal one.
func (e *Engine) offerCastable(p state.PlayerID, id state.ObjID, base Cost, scope costScope, ability bool) bool {
	return e.offerCastableUsing(e.collectCostStatics(), p, id, base, scope, ability, nil)
}

// fixLifeXCost resolves an announced PayLife<X> cost part whose source face
// defines SVar:X with a body that is NOT Count$xPaid. Forge's SVar:X is the
// definition that body gives the cost's X, and exactly two shapes exist in
// the corpus:
//
//   - Count$xPaid (Toxic Deluge, Krumar Initiate's "Cost$ X B T PayLife<X>",
//     every SP face carrying the token): "the announced X" -- the payer
//     announces X freely (bounded by life, xAsk) and the life part settles at
//     the same value the printed {X} folds at. The cost passes through
//     unchanged.
//   - any other resolvable body (Murderous Betrayal's
//     SVar:X:Count$YourLifeTotal/HalfUp, Tornado's
//     SVar:X:Count$CardCounters.VELOCITY/Times.3): the value is FIXED -- the
//     payer announces nothing, and the settle pays exactly the evaluated
//     amount. Each such part is folded into Cost.Life, the fixed additional
//     cost both the payable gates (resolveManaWith's life check) and the
//     settle (payMana's LifeChange) already price and charge -- the same
//     place a RaiseCost's fixed life raise lands, so CR 601.2f's ordering
//     treats it as an additional cost that no reduction ever touches.
//
// The verdict is three-way, and ok=false is WITHHOLD: the SVar:X body is
// present, not Count$xPaid, and either unresolvable (War Room's commander
// colour identity) or negative -- the ability is not offered at all (the
// fail-closed direction ParseUnlessCost and manaAbilityPayable already take)
// rather than offered with an arbitrary announcement the payer cannot be
// held to. A cost pairing the non-xPaid body with a printed {X} or another
// announced part sharing the X (Sac<X/Spec>, PayEnergy<X>,
// SubCounter<X/Kind>) is also withheld: the corpus carries no such face, and
// what the fixed body would mean for a shared announcement is undefined
// here. The idempotence contract matters: offerCastable converts at the
// gate, the payment sites convert again, and a converted cost (no LifeX
// left) early-returns unchanged.
func (e *Engine) fixLifeXCost(p state.PlayerID, id state.ObjID, c Cost) (Cost, bool) {
	if len(c.LifeX) == 0 {
		return c, true
	}
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return c, true
	}
	body, present := o.Face().SVars["X"]
	if !present || strings.EqualFold(strings.TrimSpace(body), "Count$xPaid") {
		return c, true
	}
	if c.X > 0 || costAnnouncesSacX(c) {
		return c, false
	}
	for _, part := range c.Energy {
		if part.Spec == "X" {
			return c, false
		}
	}
	for _, part := range c.SubCounter {
		if part.Announced {
			return c, false
		}
	}
	ctx := &effects.Ctx{Source: id, Controller: p, SVars: o.Face().SVars}
	n, resolvable := effects.EvalCountOK(e, ctx, body)
	if !resolvable || n < 0 {
		return c, false
	}
	out := c
	out.LifeX = nil
	for range len(c.LifeX) {
		out.Life = addClampedGeneric(out.Life, int64(n))
	}
	return out, true
}

// drawCostCount resolves one Draw cost part's count at payment time. A
// literal part (Dyn == "") is simply N. A dynamic part (Forge's
// Draw<X/Spec>, Champion of Wits' "draw cards equal to its power") reads the
// source face's SVar table: the body named by part.Dyn (SVar:X for
// Draw<X/...>) is evaluated exactly the way fixLifeXCost evaluates its
// PayLife<X> body, with the source object bound as the count context so
// Count$CardPower reads the drawing permanent's own power. ok=false means
// the source face, the SVar, or the body is unavailable -- the cost is
// unpayable (the fail-closed direction), never a silent zero draw.
func (e *Engine) drawCostCount(id state.ObjID, you state.PlayerID, part CostPart) (int32, bool) {
	return e.drawCostCountTrig(id, you, part, nil)
}

// drawCostCountTrig is drawCostCount with an optional fire-time trigger
// context seeded into the evaluation: the triggered-cost window's dynamic
// Draw<X/Spec> part (Hordewing Skaab's "draw cards equal to the number of
// opponents dealt damage this way", SVar:X:TriggeredPlayersTargets$Amount)
// reads the DAMAGE BATCH the triggering event captured, which the bare
// cast-flow context carries nothing of. A nil context is the ordinary
// cast/activation read, unchanged.
func (e *Engine) drawCostCountTrig(id state.ObjID, you state.PlayerID, part CostPart, tcx *effects.TriggerContext) (int32, bool) {
	if part.Dyn == "" {
		return part.N, true
	}
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return 0, false
	}
	body, present := o.Face().SVars[part.Dyn]
	if !present {
		return 0, false
	}
	ctx := &effects.Ctx{Source: id, Controller: you, SVars: o.Face().SVars}
	if tcx != nil {
		ctx.TriggerContext = *tcx
	}
	n, resolvable := effects.EvalCountOK(e, ctx, body)
	if !resolvable || n < 0 {
		return 0, false
	}
	return n, true
}

// offerCastableUsing is offerCastable's core with the statics collected
// once (the walk shares one collection) and the mana pool optionally
// overridden: hyp nil is the ordinary real-pool gate, hyp non-nil prices the
// mana feasibility against the potential-action walk's hypothetical bound
// (the pool the seat would hold after floating every untapped source) while
// every non-mana read stays real. The two modes share one body, so the walk
// cannot drift from the offer it mirrors.
func (e *Engine) offerCastableUsing(statics costStaticViews, p state.PlayerID, id state.ObjID, base Cost, scope costScope, ability bool, hyp *state.Mana) bool {
	// The SVar-fixed PayLife<X> conversion (fixLifeXCost) shapes the cost the
	// gate prices into the exact cost the payment will store (beginCast and
	// beginActivation convert through the same helper), so an offered cost and
	// the charge can never disagree about the life part. A face whose SVar:X
	// body is present but unresolvable is WITHHELD here -- the fail-closed
	// direction the rest of the cost grammar takes -- rather than offered with
	// an arbitrary announcement.
	// fixLifeXCost is the identity on a cost with no PayLife<X> part.
	if len(base.LifeX) > 0 {
		var ok bool
		if base, ok = e.fixLifeXCost(p, id, base); !ok {
			return false
		}
	}
	// The activated ability's own XMin$ parameter (task cost:xmin-param): the
	// same announcement floor xAsk folds from the ability being activated,
	// read here off scope.ab -- the exact printed or granted SA the offer walk
	// scoped. The fold RAISES the cost's own XMin<N> bound by maximum and
	// stops there: feasibleAny's bound pricing (WithX at the smallest legal
	// announcement) composes the cheapest legal price from it, so the offer
	// gate and xAsk share one floor answer and an unannounced cost still
	// reports no charge. A cost that announces no X binds nothing: no {X} pip
	// and no announced-X part means no announcement exists to floor.
	if scope.ab != nil && costAnnouncesX(base) {
		if n := xMinAbilityParam(scope.ab); n > base.XMin {
			base.XMin = n
		}
	}
	// mayApply/provenance capture what the potential-target retry below
	// needs to know of this pass (offerRetryFutile).
	mayApply := false
	mods := e.costModifiersCompose(statics, p, id, scope, nil, false, 0, &mayApply)
	provenance := e.costProvenanceSeen
	// A Waterbend<N>/<X> part carried by the cost itself (an ability's own
	// Cost$ like Giant Koi's, or a spell's optional-cost part) credits the
	// same taps a RaiseCost Waterbend does (mods.waterbend), so the offer is
	// priced with the help the payment will offer.
	if base.Waterbend > 0 {
		mods.waterbend = addClampedGeneric(mods.waterbend, int64(base.Waterbend))
	}
	if base.WaterbendX {
		mods.waterbendX = true
		mods.waterbendPartX++
	}
	// withWaterbendOfferCredit returns mods unchanged unless some waterbend
	// credit is wanted (its want is 0 when both counts are).
	if mods.waterbend != 0 || mods.waterbendPartX != 0 {
		mods = e.withWaterbendOfferCredit(p, id, base.XMin, mods)
	}
	tax := int32(0)
	if scope.kind != "Ability" && scope.kind != "Foretell" && scope.kind != "Static" {
		tax = e.commanderTaxAmount(p, id)
	}
	delve := int32(0)
	if e.hasKeywordH(id, kwhDelve) {
		delve = int32(len(e.G.Zone(state.ZGraveyard, p)))
	}
	if !e.manaFeasiblePricedP(p, id, ability, &base, &mods, tax, delve, hyp) {
		// A target-dependent reducer cannot be in the ordinary pre-target
		// snapshot, but it may make one legal target choice payable. Retry with
		// exactly those potential reductions; target-dependent raises/floors
		// remain absent until the actual target is known (see the helper's
		// contract).
		//
		// The two passes differ ONLY through ValidTarget$: it is the one
		// place costStaticApplies reads the targets, and the potential pass
		// skips just the ValidTarget$ raises/floors. With no cost static
		// carrying the key, both passes compose the same modifier set, so the
		// retry would re-ask the exact question that just failed: the target
		// census (a pure read) is skipped, not changed.
		var potential costMods
		potentialOK := false
		if futile := offerRetryFutile(scope, mods, mayApply, provenance); statics.validTarget && (!futile || walkSkipVerify) {
			potential, potentialOK = e.potentialCostModsUsing(statics, p, id, scope, e.costPotentialTargets(p, id, scope), 0, func(m costMods) bool {
				return e.manaFeasiblePriced(p, id, ability, base, m, tax, delve, hyp) &&
					e.nonManaCastable(p, id, e.composedOfferCost(p, id, base, m, scope), ability, tapCostSAKind(scope.ab))
			})
			if futile && potentialOK {
				panic(fmt.Sprintf("rules: futile potential-target retry for obj %d accepted %+v", id, potential))
			}
		}
		if potentialOK {
			mods = potential
		} else if accepted, ok := e.offerSacXModsGated(p, id, ability, &base, statics, scope, tax, delve, hyp); ok {
			// The cost announces a Sac<X/Spec> count whose resulting X-dependent
			// reduction (Dargo's "{2} less for each permanent sacrificed this
			// way", read through Count$xPaid) can make the cast payable at a
			// nonzero X the X=0 snapshot misprices. Accept when, and only when,
			// SOME legal announcement is payable -- the same announced-X
			// recomputation manaToPay makes after the announcement, applied at
			// the gate so the offer and the charge agree.
			mods = accepted
		} else if accepted, ok := e.offerNamedModsGated(p, id, ability, &base, &mods, statics, scope, tax, delve, hyp); ok {
			// A RaiseCost part counted by a named announcement (Explosive
			// Singularity's "tap any number of untapped creatures ... costs
			// {1} less for each creature tapped this way") can make the cast
			// payable at a nonzero announcement the 0 snapshot misprices --
			// the offerSacXMods sweep, for the named count.
			mods = accepted
		} else {
			return false
		}
	}
	// feasibleAny has established the mana half for a specific announced face
	// when a floor or Color$ reduction is face-sensitive. Do not re-check
	// that result against the unresolved Cost: Color$ W can make the W half
	// of {W/U} free, while applying it before that half is chosen sees no W
	// pip at all. The remaining cost parts are face-independent, so this
	// shared tail preserves every Sac/Discard/counter/tap legality check.
	return e.nonManaCastable(p, id, e.composedOfferCost(p, id, base, mods, scope), ability, tapCostSAKind(scope.ab))
}

// offerSacXMods is the offer gate's announced-sacrifice-count affordability
// sweep. A cost carrying a Sac<X/Spec> part announces its count as X (CR
// 601.2b), and a ReduceCost static that reads the paid X (Dargo, the
// Shipwrecker's SVar:X:Count$xPaid over SVar:Y:SVar$X/Times.2) is evaluated
// with X=0 in the ordinary pre-announcement snapshot -- so a cast the player
// can only afford AFTER reducing the cost is never offered. This walks every
// legal announcement (1..the matching permanents the payer can sacrifice,
// the same bound xAsk computes) and reprices exactly the way manaToPay's
// post-announcement recomputation does: the announced cost (base folded at X)
// under the X-bound modifiers, then the commander tax. It returns the
// modifier snapshot of the first feasible announcement, so the caller's
// composed cost and payment agree; ok=false means no legal announcement is
// payable and the cast must stay withheld (the fail-closed direction, never
// a bypass of mana feasibility for every Sac<X> spell). X=0 is deliberately
// not swept here -- the caller already priced it, and an all-zero reduction
// would merely repeat that answer.
func (e *Engine) offerSacXMods(p state.PlayerID, id state.ObjID, ability bool, base Cost, statics costStaticViews, scope costScope, tax, delve int32, hyp *state.Mana) (costMods, bool) {
	if !costAnnouncesSacX(base) {
		return costMods{}, false
	}
	// Every announced Sac part pays the SAME X. The smallest candidate
	// pool bounds the search, but overlapping pools may require more
	// distinct objects than either pool alone can supply.
	maxX := int32(0)
	boundSet := false
	for _, part := range base.Sac {
		if !part.Announced {
			continue
		}
		n := int32(len(e.sacrificeCostCandidates(p, id, part, ability)))
		if !boundSet || n < maxX {
			maxX = n
			boundSet = true
		}
	}
	if maxX <= 0 {
		return costMods{}, false
	}
	// The potential-target variant is the same "does SOME legal announcement
	// exist" relaxation the ordinary retry above makes; target-dependent
	// raises and floors stay absent (they can only raise the price).
	targets := e.costPotentialTargets(p, id, scope)
	for x := int32(1); x <= maxX; x++ {
		if !e.sacrificeCostAssignable(p, id, base.Sac, ability, x) {
			continue
		}
		announced := base.WithX(x)
		mods := e.costModifiersWithTargetsXUsing(statics, p, id, scope, nil, false, x)
		if e.manaFeasiblePriced(p, id, ability, announced, mods, tax, delve, hyp) {
			return mods, true
		}
		if len(targets) == 0 {
			continue
		}
		if candidateMods, ok := e.potentialCostModsUsing(statics, p, id, scope, targets, x, func(m costMods) bool {
			return e.manaFeasiblePriced(p, id, ability, announced, m, tax, delve, hyp)
		}); ok {
			return candidateMods, true
		}
	}
	return costMods{}, false
}

// costPotentialTargets returns the legal target candidates that can make a
// target-conditional reduction available at offer time. The final selection
// is still repriced before payment; this is only the "does SOME legal
// announcement exist" half of CR 601.2. Modal spells have no selected mode
// yet and therefore conservatively contribute no potential discount.
func (e *Engine) costPotentialTargets(p state.PlayerID, id state.ObjID, scope costScope) []state.Target {
	sa := e.costTargetingSA(id, scope)
	if sa == nil || sa.Params["ValidTgts"] == "" || sa.Params["Choices"] != "" {
		return nil
	}
	var excludeSelf state.ObjID
	if scope.kind != "Ability" {
		excludeSelf = id
	}
	candidates := e.legalTargetCandidates(p, id, excludeSelf, sa)
	out := make([]state.Target, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.kind == "player" {
			out = append(out, state.Target{Player: candidate.player, IsPlayer: true})
		} else {
			out = append(out, state.Target{Obj: candidate.obj})
		}
	}
	return out
}

// costTargetingSA returns the spell or activated ability whose ValidTgts$ and
// TargetMin$/TargetMax$ a target-relative cost read shares. A spell's face
// carries the declaration while it is in hand; an activated ability is the
// scope's own SA. This is the ONE derivation costPotentialTargets and
// costAmountTargets use, so the offer census and the amount's legal-assignment
// size can never name different declarations.
func (e *Engine) costTargetingSA(id state.ObjID, scope costScope) *cards.SA {
	if scope.kind == "Static" {
		// A special action (specialActionScope) announces no targets.
		return nil
	}
	if scope.kind == "Ability" {
		return scope.ab
	}
	if o := e.G.Obj(id); o != nil && o.Face() != nil {
		return o.Face().SpellAbility()
	}
	return nil
}

// costAmountTargets trims a potential-target census to a COMPLETE LEGAL TARGET
// ASSIGNMENT before a target-relative Amount$ reads it. The offer gate's
// potential pass hands the whole costPotentialTargets census to the modifier
// composition so every ValidTarget$/ValidSpell$ rule can match against SOME
// candidate, but an Amount$ that counts the cast's targets (Battlefield
// Thaumaturge's TargetedObjectsDistinct, "for each creature it targets") must
// see only as many targets as the declaration actually announces -- pricing
// every legal candidate at once would reduce the cost by the census size and
// offer a cast the table can never complete. The size is resolvedTargetBounds'
// own maximum (the most targets a reduction reading the count can see, and so
// the reduction-favourable witness the existential offer gate wants), capped
// at the census. A resolved maximum of 0 (the "instead" idiom) yields the
// empty assignment, never a target the declaration may not announce. Only the
// potential pass calls this: a non-potential composition already carries the
// announced targets, which are a legal assignment by construction. The chosen
// targets are always repriced at CR 601.2c/h.
func (e *Engine) costAmountTargets(p state.PlayerID, id state.ObjID, scope costScope, targets []state.Target) []state.Target {
	if len(targets) == 0 {
		return targets
	}
	sa := e.costTargetingSA(id, scope)
	if sa == nil {
		return targets
	}
	_, max := e.resolvedTargetBounds(p, id, sa, 0)
	// A resolved maximum of 0 (the "instead" idiom) must yield the empty
	// assignment even when the census holds a single candidate: the bound is
	// resolved BEFORE the size fast path so one candidate can never stand in
	// for a target the declaration is not allowed to announce.
	if max == 0 {
		return nil
	}
	if max < 0 || max >= len(targets) {
		return targets
	}
	return targets[:max]
}

// SpellEffectiveCost returns id's non-X own spell cost after offer-time cost
// modifiers, in Forge notation. It is scoped to the hand -- the only zone the
// cast-offer walk (rules/legal.go) prices -- so a battlefield permanent or a
// graveyard flashback card never carries an "effective cast cost" it cannot
// use. It deliberately declines alternative/dynamic shapes rather than
// presenting an incomplete amount.
func (e *Engine) SpellEffectiveCost(p state.PlayerID, id state.ObjID) string {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil || o.Zone != state.ZHand {
		return ""
	}
	f := o.Face()
	if f.SpellAbility() == nil {
		return ""
	}
	base := e.parseCost(f.ManaCost)
	if base.X != 0 || base.XMin != 0 || base.WaterbendX {
		return ""
	}
	// The own-cost projection does not price alternate cast faces or extra
	// costs. The ordinary face spell shape is the only supported case here.
	if f.SpellAbility().Params["AlternativeCost"] != "" || f.SpellAbility().Params["Cost"] != "" {
		return ""
	}
	mods := e.costModifiersWithTargetsUsing(e.collectCostStatics(), p, id, spellScope(""), nil, false)
	if mods.hasExtra || mods.setFloor != 0 || mods.raiseX != 0 {
		return ""
	}
	cost := e.offerCostFor(p, id, base, spellScope(""))
	if cost.X != 0 {
		return ""
	}
	// Compare NORMALIZED costs on both sides: f.ManaCost is the raw printed
	// string, whose colour order / pip spelling formatCost may re-render
	// identically-in-meaning but differently in text ({2 G B} -> "2 B G").
	// A raw-vs-normalized compare would then report a spurious "effective"
	// cost for a card no static touches. Normalizing the base first makes an
	// unchanged card read unchanged however its printed string is spelled,
	// while a real RaiseCost/ReduceCost still differs after normalization.
	if formatCost(cost) == formatCost(base) {
		return ""
	}
	return formatCost(cost)
}

// AbilityCosts returns id's non-mana activated-ability costs after the same
// offer-time RaiseCost/ReduceCost composition legalActions applies. The order
// is the face's authored ability order. This is a projection helper: it emits
// no event and mutates no game state.
func (e *Engine) AbilityCosts(p state.PlayerID, id state.ObjID) []string {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return nil
	}
	var out []string
	for _, ab := range o.Face().Abilities {
		if ab.Kind != "AB" || isManaAbilityAPI(ab.API) {
			continue
		}
		out = append(out, e.abilityOfferCost(p, id, ab))
	}
	return out
}

// abilityOfferCost is one activated ability's offer-time cost in Forge
// notation: the printed Cost$ with its own ReduceCost$ and the applicable
// RaiseCost/ReduceCost statics composed. AbilityCosts projects it onto the
// card, and legalActionsPriced stamps it on the ability's "ability" option,
// so the two can never disagree about what an activation will charge.
func (e *Engine) abilityOfferCost(p state.PlayerID, id state.ObjID, ab *cards.SA) string {
	cost := e.parseCost(ab.Params["Cost"])
	// The ability's own ReduceCost$ (Otawara's Channel): the same
	// composition the offer gate and beginActivation's charge apply, so
	// the decision's displayed cost is the cost the payment will charge.
	if n := e.ownReduceCost(p, id, ab, nil, nil, 0); n > 0 && cost.Generic >= n {
		cost.Generic -= n
	} else if n > 0 {
		cost.Generic = 0
	}
	cost = e.powerUpReducedCost(id, ab, cost)
	return formatCost(e.offerCostFor(p, id, cost, abilityScope(ab)))
}

// formatCost writes the parsed cost back in the whitespace-delimited Forge
// notation understood by the client. Parsing first is intentional: malformed
// tokens retain the engine's real conservative one-generic interpretation,
// and every recognised non-mana component remains visible so a client that
// cannot price it can still fail closed.
func formatCost(c Cost) string {
	var parts []string
	if c.Generic > 0 {
		parts = append(parts, strconv.FormatInt(int64(c.Generic), 10))
	}
	const faces = "WUBRGC"
	for i, face := range []byte(faces) {
		for n := int32(0); n < c.Colored[i]; n++ {
			parts = append(parts, string(face))
		}
	}
	for range c.X {
		parts = append(parts, "X")
	}
	for _, h := range c.Hybrid {
		parts = append(parts, string([]byte{h.A, '/', h.B}))
	}
	for _, t := range c.Twobrid {
		parts = append(parts, strconv.FormatInt(int64(t.Generic), 10)+"/"+string(t.Col))
	}
	for _, p := range c.Phyrexian {
		parts = append(parts, string([]byte{p, 'P'}))
	}
	for _, hp := range c.HybridPhyrexian {
		parts = append(parts, string([]byte{hp.A, '/', hp.B, '/', 'P'}))
	}
	for n := c.Snow; n > 0; n-- {
		parts = append(parts, "S")
	}
	if c.Life > 0 {
		parts = append(parts, "PayLife<"+strconv.FormatInt(int64(c.Life), 10)+">")
	}
	for range c.LifeX {
		parts = append(parts, "PayLife<X>")
	}
	for _, part := range c.DamageYou {
		parts = append(parts, "DamageYou<"+strconv.FormatInt(int64(part.N), 10)+">")
	}
	for _, part := range c.GainLife {
		tok := "GainLife<" + strconv.FormatInt(int64(part.N), 10) + "/" + part.Spec
		if part.Each {
			tok += "/*"
		}
		parts = append(parts, tok+">")
	}
	if c.Tap {
		parts = append(parts, "T")
	}
	if c.Untap {
		parts = append(parts, "Q")
	}
	for _, part := range c.Energy {
		if part.Spec == "X" {
			parts = append(parts, "PayEnergy<X>")
		} else {
			parts = append(parts, "PayEnergy<"+strconv.FormatInt(int64(part.N), 10)+">")
		}
	}
	appendCostParts := func(kind string, costs []CostPart) {
		for _, part := range costs {
			n := strconv.FormatInt(int64(part.N), 10)
			if part.Dyn != "" {
				n = part.Dyn
			}
			parts = append(parts, kind+"<"+n+"/"+part.Spec+">")
		}
	}
	appendCostParts("Sac", c.Sac)
	appendCostParts("Discard", c.Discard)
	appendCostParts("Draw", c.Draw)
	appendCostParts("SubCounter", c.SubCounter)
	appendCostParts("AddCounter", c.AddCounter)
	for _, part := range c.Exile {
		var head string
		switch part.Zone {
		case state.ZBattlefield:
			head = "Exile"
		case state.ZGraveyard:
			head = "ExileFromGrave"
		default:
			head = "ExileFromHand"
		}
		n := strconv.FormatInt(int64(part.N), 10)
		if part.Announced {
			n = "X"
		}
		parts = append(parts, head+"<"+n+"/"+part.Spec+">")
	}
	for _, part := range c.ExileFromTop {
		parts = append(parts, "ExileFromTop<"+strconv.FormatInt(int64(part.N), 10)+"/"+part.Spec+">")
	}
	appendCostParts("Reveal", c.Reveal)
	// RevealOrChoose prints its own head so Compile/Decompile round-trips back
	// into the distinct slice (appendCostParts' generic head would print the
	// bare kind name and re-parse as the fallback). The optional trailing
	// description field is preserved when present, matching the parser's
	// three-field form.
	for _, part := range c.RevealOrChoose {
		head := "RevealOrChoose<" + strconv.FormatInt(int64(part.N), 10) + "/" + part.Spec
		if part.Desc != "" {
			head += "/" + part.Desc
		}
		parts = append(parts, head+">")
	}
	for _, part := range c.RevealChosen {
		// RevealChosen<Player> has no trailing field; RevealChosen<Type/...>
		// prints its description. Both are re-parseable by revealChosenCost.
		if part.Desc == "" {
			parts = append(parts, "RevealChosen<"+part.Spec+">")
		} else {
			parts = append(parts, "RevealChosen<"+part.Spec+"/"+part.Desc+">")
		}
	}
	appendCostParts("Behold", c.Behold)
	appendCostParts("ExiledMoveToGrave", c.MoveToGrave)
	for _, part := range c.Mill {
		parts = append(parts, "Mill<"+strconv.FormatInt(int64(part.N), 10)+">")
	}
	appendCostParts("tapXType", c.TapPermanent)
	for _, part := range c.Blight {
		if part.Announced {
			parts = append(parts, "Blight<X>")
			continue
		}
		parts = append(parts, "Blight<"+strconv.FormatInt(int64(part.N), 10)+">")
	}
	appendCostParts("Return", c.Return)
	for range c.Exert {
		parts = append(parts, "Exert<1/CARDNAME>")
	}
	if c.Forage {
		parts = append(parts, "Forage")
	}
	appendCostParts("PayEnergy", c.Energy)
	for _, part := range c.PutToLib {
		zone := "Battlefield"
		switch part.Zone {
		case state.ZHand:
			zone = "Hand"
		case state.ZGraveyard:
			zone = "Grave"
		}
		parts = append(parts, "PutCardToLibFrom"+zone+"<"+strconv.FormatInt(int64(part.N), 10)+"/"+
			strconv.FormatInt(int64(part.LibraryPos), 10)+"/"+part.Spec+">")
	}
	return strings.Join(parts, " ")
}

// costPhrase renders a parsed cost for a PLAYER-FACING prompt or option
// label: Forge's non-mana heads become English, and the mana half stays in
// the card's own Forge notation ("2 B", "U") so it matches the card view's
// mana_cost (view/view.go) and the web's mana.ts symbol renderer. It is the
// deliberate sibling of formatCost: formatCost writes the raw whitespace-
// delimited token back out ("Sac<1/Creature.Other>") and is retained for
// the machine wire field decision.Option.Cost (manaActivationCostMarker).
//
// Forge embeds a human-readable "/description" on most non-mana tokens
// (Sac<1/Creature.Other/another creature>); costPhrase uses it verbatim when
// present (it is the card's own wording) and otherwise synthesizes a
// reasonable noun phrase from the head and spec. The fallback is
// deliberately not an oracle-text generator -- see CostPart.Desc.
//
// An empty Cost (or one whose only tokens were the payment-mode marker
// "Mandatory") renders as the empty string, so a caller that would
// otherwise emit "pay ?" can detect the no-cost case instead.
func costPhrase(c Cost) string {
	var clauses []string
	if m := formatManaClause(c); m != "" {
		clauses = append(clauses, m)
	}
	if c.Tap {
		clauses = append(clauses, "tap this permanent")
	}
	if c.Untap {
		clauses = append(clauses, "untap this permanent")
	}
	if c.Life > 0 {
		clauses = append(clauses, "pay "+strconv.FormatInt(int64(c.Life), 10)+" life")
	}
	for range c.LifeX {
		clauses = append(clauses, "pay X life")
	}
	for _, part := range c.DamageYou {
		clauses = append(clauses, "take "+countPhrase(part.N)+" damage")
	}
	for _, part := range c.GainLife {
		clauses = append(clauses, gainLifeCostPhrase(part))
	}
	for _, part := range c.Sac {
		clauses = append(clauses, "sacrifice "+objectPhrase(part, "permanent"))
	}
	for _, part := range c.Discard {
		clauses = append(clauses, "discard "+objectPhrase(part, "card"))
	}
	for _, part := range c.Draw {
		if part.Dyn != "" {
			clauses = append(clauses, "draw X cards")
		} else {
			clauses = append(clauses, "draw "+countPhrase(part.N)+" card"+pluralSuffix(part.N))
		}
	}
	for _, part := range c.SubCounter {
		n := countPhrase(part.N)
		if part.Announced {
			n = "X"
		}
		clause := "remove " + n + " " + strings.ToUpper(part.Spec) + " counter" + pluralSuffix(part.N)
		if part.Target != "" && !subCounterTargetsSource(part.Target) && part.Desc != "" {
			clause += " from " + part.Desc
		}
		clauses = append(clauses, clause)
	}
	for _, part := range c.AddCounter {
		kind := "loyalty"
		if !strings.EqualFold(part.Spec, "LOYALTY") {
			kind = strings.ToUpper(part.Spec)
		}
		clauses = append(clauses, "add "+countPhrase(part.N)+" "+kind+" counter"+pluralSuffix(part.N))
	}
	if len(c.Exert) > 0 {
		clauses = append(clauses, "exert it")
	}
	for _, part := range c.Exile {
		clauses = append(clauses, "exile "+objectPhrase(part, "card"))
	}
	for _, part := range c.ExileFromTop {
		// Top-of-library payment: prose must not imply the payer picks any
		// card from the library, only the top N in order.
		clauses = append(clauses, "exile the top "+strconv.FormatInt(int64(part.N), 10)+" card"+pluralSuffix(part.N)+" of your library")
	}
	for _, part := range c.MoveToGrave {
		clauses = append(clauses, "put "+objectPhrase(part, "card")+" from exile into its owner's graveyard")
	}
	for _, part := range c.Mill {
		clauses = append(clauses, "mill "+countPhrase(part.N)+" card"+pluralSuffix(part.N))
	}
	for _, part := range c.Evidence {
		n := countPhrase(part.N)
		if part.Dyn != "" {
			n = part.Dyn
		}
		clauses = append(clauses, "exile cards with total mana value "+n+" or greater from your graveyard")
	}
	for _, part := range c.Reveal {
		clauses = append(clauses, "reveal "+objectPhrase(part, "card"))
	}
	for _, part := range c.RevealOrChoose {
		clauses = append(clauses, "reveal "+objectPhrase(part, "card")+" or choose "+objectPhrase(part, "permanent")+" you control")
	}
	for _, part := range c.RevealChosen {
		if strings.EqualFold(part.Spec, "Player") {
			clauses = append(clauses, "reveal the chosen player")
		} else {
			clauses = append(clauses, "reveal the chosen creature type")
		}
	}
	for _, part := range c.Behold {
		clauses = append(clauses, "behold "+objectPhrase(part, "card"))
	}
	for _, part := range c.TapPermanent {
		clauses = append(clauses, "tap "+objectPhrase(part, "permanent"))
	}
	for _, part := range c.Blight {
		if part.Announced {
			clauses = append(clauses, "blight X")
			continue
		}
		clauses = append(clauses, "blight "+countPhrase(part.N))
	}
	for _, part := range c.Return {
		clauses = append(clauses, "return "+objectPhrase(part, "permanent")+" to its owner's hand")
	}
	for _, part := range c.PutToLib {
		clauses = append(clauses, "put "+objectPhrase(part, "card")+" from your "+zoneNoun(part.Zone)+" into your library")
	}
	for _, part := range c.Energy {
		if part.Spec == "X" {
			clauses = append(clauses, "pay X energy")
		} else {
			clauses = append(clauses, "pay "+countPhrase(part.N)+" energy")
		}
	}
	if c.Forage {
		clauses = append(clauses, "forage")
	}
	return joinClauses(clauses)
}

// formatManaClause renders the mana half of a cost in Forge notation with a
// "pay " prefix ("pay 2 B"). It is empty when the cost has no mana
// component, so costPhrase can omit the clause entirely rather than emit a
// bare "pay".
func formatManaClause(c Cost) string {
	var parts []string
	if c.Generic > 0 {
		parts = append(parts, strconv.FormatInt(int64(c.Generic), 10))
	}
	const faces = "WUBRGC"
	for i, face := range []byte(faces) {
		for n := int32(0); n < c.Colored[i]; n++ {
			parts = append(parts, string(face))
		}
	}
	for range c.X {
		parts = append(parts, "X")
	}
	for _, h := range c.Hybrid {
		parts = append(parts, string([]byte{h.A, '/', h.B}))
	}
	for _, t := range c.Twobrid {
		parts = append(parts, strconv.FormatInt(int64(t.Generic), 10)+"/"+string(t.Col))
	}
	for _, p := range c.Phyrexian {
		parts = append(parts, string([]byte{p, 'P'}))
	}
	for _, hp := range c.HybridPhyrexian {
		parts = append(parts, string([]byte{hp.A, '/', hp.B, '/', 'P'}))
	}
	for n := c.Snow; n > 0; n-- {
		parts = append(parts, "S")
	}
	if len(parts) == 0 {
		return ""
	}
	return "pay " + strings.Join(parts, " ")
}

// objectPhrase renders one non-mana cost part's noun: Forge's embedded
// /description verbatim when the corpus supplies one (the card's own
// wording), else the count as digits plus a noun derived from the spec's
// leading type word or the head's default. An announced part (Sac<X>,
// SubCounter<X>) renders its "X" count. On the description path a count of
// one is dropped, so "another creature" is not prefixed with "1 ".
func objectPhrase(part CostPart, defNoun string) string {
	count := ""
	if part.Announced {
		count = "X "
	} else if part.N != 1 || part.Desc == "" {
		// The synthesized path keeps the count as digits (even 1), per the
		// brief; only the embedded-description path drops a count of one,
		// so "another creature" is not prefixed with "1 ".
		count = strconv.FormatInt(int64(part.N), 10) + " "
	}
	if part.Desc != "" {
		// The corpus description already names the object ("another
		// creature", "this artifact"); only an explicit count > 1 needs the
		// number in front of it.
		if part.N != 1 && !part.Announced {
			return count + part.Desc
		}
		return part.Desc
	}
	return count + specNoun(part.Spec, defNoun)
}

// specNoun derives a readable noun from a cost spec's leading type word
// ("Creature.Other" -> "creature"). It is the fallback only: the corpus's
// embedded /description wins wherever it exists (objectPhrase). An unknown
// base word falls back to the head's own default noun rather than echoing
// raw filter syntax at the player.
func specNoun(spec, defNoun string) string {
	base := spec
	if i := strings.IndexAny(base, ".+, ;"); i >= 0 {
		base = base[:i]
	}
	switch strings.ToLower(base) {
	case "creature":
		return "creature"
	case "artifact":
		return "artifact"
	case "enchantment":
		return "enchantment"
	case "land":
		return "land"
	case "planeswalker":
		return "planeswalker"
	case "permanent":
		return "permanent"
	case "card":
		return "card"
	case "token":
		return "token"
	default:
		return defNoun
	}
}

// zoneNoun names the origin zone a PutCardToLib part pays from, for the
// prose phrase.
func zoneNoun(z state.Zone) string {
	switch z {
	case state.ZHand:
		return "hand"
	case state.ZGraveyard:
		return "graveyard"
	default:
		return "battlefield"
	}
}

// countPhrase renders a cost part's count as digits ("2"), or "X" for an
// announced part. No number-to-word table: the brief pins digits.
func countPhrase(n int32) string {
	return strconv.FormatInt(int64(n), 10)
}

// gainLifeCostPhrase renders one GainLife<N/Player...> part as player-facing
// prose: "an opponent gains N life" for the bare Player.Opponent form and
// "each other player gains N life" for the /* marker (Reverent Silence,
// Skyshroud Cutter). An unrecognised player word falls back to the generic
// "a player" rather than echoing raw Forge filter syntax.
func gainLifeCostPhrase(part CostPart) string {
	who := "a player"
	switch strings.TrimSpace(part.Spec) {
	case "Player.Opponent":
		who = "an opponent"
	case "Player.Other":
		if part.Each {
			who = "each other player"
		} else {
			who = "another player"
		}
	}
	verb := "gains"
	if who == "each other player" {
		verb = "gain"
	}
	return who + " " + verb + " " + countPhrase(part.N) + " life"
}

// pluralSuffix returns "s" for a count that is not exactly one.
func pluralSuffix(n int32) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// joinClauses joins prose cost clauses the way a sentence reads: "a", "a
// and b", "a, b and c".
func joinClauses(clauses []string) string {
	switch len(clauses) {
	case 0:
		return ""
	case 1:
		return clauses[0]
	case 2:
		return clauses[0] + " and " + clauses[1]
	default:
		return strings.Join(clauses[:len(clauses)-1], ", ") + " and " + clauses[len(clauses)-1]
	}
}

// capitaliseFirst upper-cases the first rune, for an option label built from
// a costPhrase.
func capitaliseFirst(s string) string {
	if s == "" {
		return s
	}
	r, size := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[size:]
}

// manaCostBeyondTap reports whether paying this cost takes anything beyond
// a bare tap: any mana pip (generic, coloured, X, snow, hybrid, Phyrexian,
// twobrid) or any non-mana component (HasNonMana). A bare-tap mana ability
// (every plain land's) and a tapless free one report false — exactly the
// windows the client's empty-priority-window floor may pass away unseen.
func manaCostBeyondTap(c Cost) bool {
	c.Tap = false
	c.Untap = false
	if c.Generic > 0 || c.X > 0 || c.Snow > 0 || c.Colored != (state.Mana{}) {
		return true
	}
	if len(c.Hybrid) > 0 || len(c.Phyrexian) > 0 || len(c.Twobrid) > 0 || len(c.HybridPhyrexian) > 0 {
		return true
	}
	return c.HasNonMana()
}

// manaActivationCostMarker is the wire marker fb-led1 adds to a priority
// window's "activate" option (decision.Option.Cost): the formatCost rendering
// of the FIRST available mana ability (face order, deterministic) whose cost
// is more than a bare tap, "" when every available ability is a bare tap —
// so a plain land's option carries no field and every existing option list
// serialises byte-identically. The engine already gates the offer through
// manaAbilityPayable, which parses and prices the whole cost, so the marker
// costs no new rules knowledge at the offer site; it exists purely so a
// client that (rightly) does not count a bare tap as a play can still tell
// the Lion's Eye Diamond window it must show the player.
//
// The cost is the configured text's compiled parse (compiledCostOf, the same
// ParseCost result frozen), so the per-offer read neither re-parses nor
// copies it; only a marked ability's cost is formatted.
func (e *Engine) manaActivationCostMarker(abilities []*cards.SA) string {
	for _, ma := range abilities {
		if cc := e.compiledCostOf(ma.Params["Cost"]); cc.beyondTap {
			return formatCost(cc.Cost)
		}
	}
	return ""
}

// dynTapParts returns the dynamic (X/Any) TapPermanent parts of a cost --
// the tapXType heads the tap election pays (dynTapCost's doc). The literal
// tapXType<N/Spec> parts are ordinary fixed payments and never appear here.
func dynTapParts(c Cost) []CostPart {
	var out []CostPart
	for _, part := range c.TapPermanent {
		if part.Dyn != "" {
			out = append(out, part)
		}
	}
	return out
}

// costCarriesDynTap reports whether a cost carries a dynamic tapXType part:
// the trigger-cost window's arm condition and the announce carve-outs use
// it, so a cost whose only X is a tapXType<X/Spec> election is treated the
// way a printed {X} is everywhere the engine asks "does this announce an X".
func costCarriesDynTap(c Cost) bool {
	return len(dynTapParts(c)) > 0
}

// withoutDynTaps strips the dynamic tapXType parts from a cost, keeping the
// literal ones (so a composed cost carrying both keeps its unpriceable half
// unpriceable). The triggered-cost window prices the non-tap rest of a
// dyn-tap cost with it.
func withoutDynTaps(c Cost) Cost {
	if !costCarriesDynTap(c) {
		return c
	}
	kept := make([]CostPart, 0, len(c.TapPermanent))
	for _, part := range c.TapPermanent {
		if part.Dyn == "" {
			kept = append(kept, part)
		}
	}
	c.TapPermanent = kept
	return c
}

// costAnnouncesCastX reports whether paying this cost announces a value for
// {X} through an announce-bearing part OTHER than a tapXType<X/Spec>
// election: a printed {X} mana symbol, a PayEnergy<X> part, an announced
// Sac<X/Spec>, SubCounter<X/Kind> or PayLife<X> part. This mirrors xAsk's
// own guard (the two must agree: the tap ask defers its X-form parts exactly
// when this is true, and xAsk bounds the announced X by the tap candidates).
// It deliberately does not fold into legal.go's costAnnouncesX, which omits
// the announced-Sac clause -- the offer gate's carve-out and this defer
// decision answer different questions and changing the offer gate's answer
// for Sac<X> costs is not this work.
func costAnnouncesCastX(c Cost) bool {
	if c.X > 0 {
		return true
	}
	for _, part := range c.Energy {
		if part.Spec == "X" {
			return true
		}
	}
	for _, part := range c.Sac {
		if part.Announced {
			return true
		}
	}
	for _, part := range c.SubCounter {
		if part.Announced {
			return true
		}
	}
	for _, part := range c.Blight {
		if part.Announced {
			return true
		}
	}
	for _, part := range c.Exile {
		if part.Announced {
			return true
		}
	}
	return len(c.LifeX) > 0
}

// HasNonMana reports whether paying this cost takes more than mana.
// AddCounter counts (the part is settled by the cast flow beside SubCounter,
// even though it takes no payment), so a caller using this to skip the
// cast-flow stages is told the truth.
func (c Cost) HasNonMana() bool {
	return c.Life > 0 || c.Tap || c.Untap || len(c.Sac) > 0 || len(c.Discard) > 0 || len(c.SubCounter) > 0 || len(c.AddCounter) > 0 || len(c.Exile) > 0 || len(c.ExileFromTop) > 0 || len(c.Reveal) > 0 || len(c.RevealChosen) > 0 || len(c.Behold) > 0 || len(c.TapPermanent) > 0 || len(c.UntapPermanent) > 0 || len(c.Blight) > 0 || c.Forage || len(c.Energy) > 0 || len(c.Return) > 0 || len(c.PutToLib) > 0 || len(c.Draw) > 0 || len(c.LifeX) > 0 || len(c.DamageYou) > 0 || len(c.GainLife) > 0 || len(c.MoveToGrave) > 0 || len(c.Mill) > 0 || len(c.Exert) > 0
}

// Priceable reports whether payMana can actually charge every part of this
// cost. payMana charges Colored and Generic from the pool and fixed Life from
// the payer, but an {X} component that has not been folded into Generic, or a
// Tap/Sac/Discard/SubCounter component, still needs cast-flow handling. Such a cost
// is unpriceable by the mid-resolution payment API and must be DECLINED,
// never silently priced at zero. This is the predicate I-5 routes through:
// every component ParseCost collapses into a shape payMana cannot charge --
// an SVar-sourced X, a cast-time-chosen X, or a Tap/Sac/Discard/SubCounter part --
// travels through the same predicate rather than a `if cost == "X"` special
// case.
//
// Cost.Pay remains pool-only, while Priceable is the "is this chargeable by
// payMana with a payer" question the mid-resolution unless-pay answer asks
// before trusting the pool and life total.
func (c Cost) Priceable() bool {
	return c.X == 0 && !c.Tap && !c.Untap && len(c.Sac) == 0 && len(c.Discard) == 0 && len(c.SubCounter) == 0 &&
		len(c.Draw) == 0 && len(c.Exile) == 0 && len(c.ExileFromTop) == 0 && len(c.Reveal) == 0 && len(c.RevealChosen) == 0 && len(c.Behold) == 0 &&
		len(c.TapPermanent) == 0 && len(c.UntapPermanent) == 0 && len(c.Blight) == 0 && !c.Forage &&
		len(c.Hybrid) == 0 && len(c.Phyrexian) == 0 && len(c.Twobrid) == 0 && len(c.HybridPhyrexian) == 0 &&
		len(c.Energy) == 0 && len(c.Return) == 0 && len(c.PutToLib) == 0 && len(c.LifeX) == 0 && len(c.DamageYou) == 0 &&
		len(c.GainLife) == 0 &&
		len(c.MoveToGrave) == 0 && len(c.Mill) == 0
}

// energyCostTotal returns the fixed energy a cost's PayEnergy<N> parts demand:
// the SUM of every fixed part, so a composed cost carrying the part several
// times (a replicated cast re-pays its PayEnergy cost once per payment) draws
// the pool down once per part rather than each spending the whole total
// independently. A dynamic PayEnergy<X> part contributes nothing here -- its
// amount is the announced X, bounded at the X ask by the payer's energy total
// (createEnergyCostX's rule) and charged as that value.
func (c Cost) energyCostTotal() int32 {
	total := int32(0)
	for _, part := range c.Energy {
		if part.Spec == "X" {
			continue
		}
		total += part.N
	}
	return total
}

// energyCostX reports whether the cost carries a dynamic PayEnergy<X> part
// whose amount is the announced X rather than a fixed N.
func (c Cost) energyCostX() bool {
	for _, part := range c.Energy {
		if part.Spec == "X" {
			return true
		}
	}
	return false
}

// withoutEnergy returns the cost with its energy parts stripped, so a caller
// can judge the remaining mana/life/components by the ordinary rules (an
// energy part is charged by chargeEnergyCost, never by payMana).
func (c Cost) withoutEnergy() Cost {
	if len(c.Energy) == 0 {
		return c
	}
	c.Energy = nil
	return c
}

// energyPayable reports whether the payer's ENERGY counter total covers the
// cost's fixed energy parts (Forge CostPayEnergy.canPay reads the same total).
// A dynamic PayEnergy<X> part is bounded by that total at its own X ask, so
// this gate makes no assumption about the not-yet-chosen value.
func (e *Engine) energyPayable(p state.PlayerID, c Cost) bool {
	total := c.energyCostTotal()
	return total == 0 || e.G.Players[p].Counter("ENERGY") >= total
}

// pip is one flexible mana demand inside a cost's mana part, as a list of
// alternative payments tried in order. The alternative kinds are exactly the
// mana symbols CR 107.4 knows: one unit of a colour, N generic mana (a
// monocolour hybrid's "2" face), two life (a Phyrexian face), and snow mana
// (a {S} pip, payable only by a mana a snow permanent produced). A pip with
// one colour listed twice is just a strict colour pip.
type pip struct {
	alts []pipAlt
}

type pipAlt struct {
	color   byte  // one unit of this colour (0 = not a colour alternative)
	generic int32 // this many generic mana (0 = not a generic alternative)
	life    int32 // two life (0 = not a life alternative)
	snow    bool  // one snow mana unit
}

// pipRider carries the payer-side may-play riders a cost's pip alternatives
// expand under: anyColor is MayPlayIgnoreColor$ ("mana of any color",
// CR 401.5), anyType is MayPlayIgnoreType$ ("mana of any type", Rakdos, the
// Muscle). The zero value is the plain exact-colour payment.
type pipRider struct {
	anyColor bool
	anyType  bool
}

// costPips expands a cost's mana part into a flat pip list, in a fixed order
// (exact colours first — colourless included — then two-colour hybrids, then
// monocolour hybrids, then Phyrexians, then hybrid-Phyrexians, then snow).
// The order is a deterministic exploration order for resolveMana's search,
// not a payment schedule: the backtracking search tries alternatives in
// list order and each pip's alternatives in their own order, so the chosen
// assignment is stable run to run. Snow pips come last so the search prefers
// spending ordinary mana before touching a snow unit for generic.
//
// Two payer-side grants widen the alternatives: when bLifeOK is set, every
// plain {B} pip additionally accepts 2 life (K'rrik, Son of Yawgmoth's "For
// each {B} in a cost, you may pay 2 life rather than pay that mana"); when
// rider.anyColor is set, every coloured pip (plain, hybrid, twobrid,
// Phyrexian or hybrid-Phyrexian) is payable by ANY colour in the pool — the
// may-play grant's MayPlayIgnoreColor$ rider, "you may spend mana as though
// it were mana of any color to cast it" (CR 401.5). A {C} pip stays
// colourless-only under anyColor: CR 107.4c's "any color" never includes
// colourless. rider.anyType (MayPlayIgnoreType$, Rakdos, the Muscle's "mana
// of any type can be spent to cast those spells") is the wider reading: under
// it EVERY pip — coloured and {C} alike — accepts all six mana types
// (anyTypeAlts), since "any type" is every mana type, colourless included.
// hasPips reports whether costPips would return any pip. Every pip source
// costPips reads is listed here; a cost without one resolves against only
// its life and generic totals (resolveManaWith), whatever the rider, the
// B-life grant and the conversion set are.
func (c Cost) hasPips() bool {
	return c.Colored != (state.Mana{}) || len(c.Hybrid) > 0 || len(c.Twobrid) > 0 || len(c.Phyrexian) > 0 ||
		len(c.HybridPhyrexian) > 0 || c.Snow > 0
}

func (c Cost) costPips(bLifeOK bool, rider pipRider) []pip {
	// Size the list once: every pip source below contributes exactly one
	// pip per unit counted here.
	n := len(c.Hybrid) + len(c.Twobrid) + len(c.Phyrexian) + len(c.HybridPhyrexian)
	if c.Snow > 0 {
		n += int(c.Snow)
	}
	for _, letter := range pipLetters {
		if k := c.Colored[state.ManaIndex(letter)]; k > 0 {
			n += int(k)
		}
	}
	out := make([]pip, 0, n)
	// The coloured slots including the colourless one: a plain {C} pip is a
	// strict colourless requirement generic must not satisfy by stealing the
	// pool's only colourless, so it is reserved like any coloured pip.
	for _, letter := range pipLetters {
		for n := c.Colored[state.ManaIndex(letter)]; n > 0; n-- {
			// The strict one-colour alternative list is shared read-only
			// (every pip consumer only ranges alts); it is capped at its
			// length, so the K'rrik append below copies rather than
			// writing into the shared array.
			alts := strictColourAlts[state.ManaIndex(letter)][:1:1]
			if rider.anyType {
				alts = anyTypeAlts()
			} else if rider.anyColor && letter != 'C' {
				alts = anyColorAlts()
			}
			if bLifeOK && letter == 'B' {
				alts = append(alts, pipAlt{life: 2})
			}
			out = append(out, pip{alts: alts})
		}
	}
	for _, pair := range c.Hybrid {
		alts := []pipAlt{{color: pair.A}, {color: pair.B}}
		if rider.anyType {
			alts = anyTypeAlts()
		} else if rider.anyColor {
			alts = anyColorAlts()
		}
		out = append(out, pip{alts: alts})
	}
	for _, t := range c.Twobrid {
		var alts []pipAlt
		switch {
		case rider.anyType:
			alts = anyTypeAlts()
		case rider.anyColor:
			alts = anyColorAlts()
		default:
			alts = []pipAlt{{color: t.Col}}
		}
		if t.Generic > 0 {
			alts = append(alts, pipAlt{generic: t.Generic})
		}
		out = append(out, pip{alts: alts})
	}
	for _, letter := range c.Phyrexian {
		alts := []pipAlt{{color: letter}, {life: 2}}
		if rider.anyType {
			alts = append(anyTypeAlts(), pipAlt{life: 2})
		} else if rider.anyColor {
			alts = append(anyColorAlts(), pipAlt{life: 2})
		}
		out = append(out, pip{alts: alts})
	}
	for _, hp := range c.HybridPhyrexian {
		alts := []pipAlt{{color: hp.A}, {color: hp.B}, {life: 2}}
		if rider.anyType {
			alts = append(anyTypeAlts(), pipAlt{life: 2})
		} else if rider.anyColor {
			alts = append(anyColorAlts(), pipAlt{life: 2})
		}
		out = append(out, pip{alts: alts})
	}
	for n := c.Snow; n > 0; n-- {
		out = append(out, pip{alts: []pipAlt{{snow: true}}})
	}
	return out
}

// pipLetters is costPips' fixed exact-colour order (colourless last).
var pipLetters = [...]byte{'W', 'U', 'B', 'R', 'G', 'C'}

// strictColourAlts holds, per mana index, the one-element strict colour
// alternative list costPips hands every plain coloured pip (read-only).
var strictColourAlts = func() (t [len(pipLetters)][1]pipAlt) {
	for _, letter := range pipLetters {
		t[state.ManaIndex(letter)][0] = pipAlt{color: letter}
	}
	return t
}()

// anyColorAlts is the colour alternatives a coloured pip accepts under the
// may-play ignore-colour rider (MayPlayIgnoreColor$ True, CR 401.5): any of
// the five colours, tried in fixed WUBRG order. A {C} pip never reaches this
// helper: CR 107.4c's "any color" never includes colourless.
func anyColorAlts() []pipAlt {
	return []pipAlt{{color: 'W'}, {color: 'U'}, {color: 'B'}, {color: 'R'}, {color: 'G'}}
}

// anyTypeAlts is the MayPlayIgnoreType$ alternative set: ALL six mana types
// (Rakdos, the Muscle's "mana of any type can be spent to cast those spells"
// — "any type" is every mana type, colourless included, unlike "any color"
// which CR 107.4c keeps away from {C}). Used for every pip kind under the
// anyType rider; coloured pips, {C} pips, hybrids and Phyrexians all widen
// to it.
func anyTypeAlts() []pipAlt {
	return []pipAlt{{color: 'W'}, {color: 'U'}, {color: 'B'}, {color: 'R'}, {color: 'G'}, {color: 'C'}}
}

// manaPayment is what resolveMana found: the pool and its two parallel
// tallies after every pip and the generic requirement were paid, plus any
// Phyrexian face spent. Snow units are always consumed alongside their pool
// slot (Snow[i] never exceeds Pool[i]); typed units (task castfilter2) are
// consumed alongside theirs (TypedMana[k][i] never exceeds Pool[i]).
type manaPayment struct {
	pool      state.Mana
	snow      state.Mana
	typed     [7]state.Mana
	lifeSpent int32
}

// takeUnit consumes one mana unit from slot i of rem/sn/typed, preferring a
// PLAIN unit when one exists, then the typed units in their fixed
// Treasure > Cave > Desert order, and a SNOW unit LAST so a snow unit stays
// available for a later {S} pip (typed units have no pips of their own, so
// they go before snow but after plain); the backtracking search undoes the
// choice if the rest of the cost cannot be paid that way. plain is the
// slot's untagged remainder; each tally is <= the pool by construction.
func takeUnit(rem, sn *state.Mana, typed *[7]state.Mana, i int) {
	plain := (*rem)[i] - (*sn)[i]
	for t := range *typed {
		plain -= (*typed)[t][i]
	}
	if plain > 0 {
		(*rem)[i]--
		return
	}
	for t := range *typed {
		if (*typed)[t][i] > 0 {
			(*rem)[i]--
			(*typed)[t][i]--
			return
		}
	}
	(*rem)[i]--
	(*sn)[i]--
}

// resolveMana finds a concrete payment of the cost's mana and fixed-life
// parts from pool, the pool's parallel snow tally and the payer's life,
// preferring to spend coloured pool mana over life for a Phyrexian pip and
// the first alternative of each pip, so the assignment is deterministic. It
// returns the payment with the coloured pips and generic requirement spent,
// and whether the whole cost is payable. The generic requirement is paid
// last from whatever the pips left, so coloured mana is never spent on
// generic while a pip still needs it; a monocolour hybrid's generic face
// competes in the backtracking search as the pip's later alternative (its
// generic amount joins the requirement for the rest of the search).
//
// conv, when non-nil, is the stat:ManaConvert conversion set this payment is
// resolved under (rules/mana_convert.go): it WIDENS what a pip's colour
// alternatives accept -- the payer's converted mana may be spent as though
// it were another colour -- and onlyC may also NARROW it ("spend other mana
// only as though it were colorless"). A nil conv is the plain exact-colour
// match every pre-existing caller keeps, so games with no ManaConvert static
// on the battlefield resolve byte-identically.
func (c Cost) resolveMana(pool, snow state.Mana, typed [7]state.Mana, life int32, conv *manaConv) (manaPayment, bool) {
	return c.resolveManaWith(pool, snow, typed, life, false, pipRider{}, conv)
}

// resolveManaWith is resolveMana with the two payer-side grants applied:
// when bLifeOK is set, every plain {B} pip additionally accepts 2 life
// (K'rrik, Son of Yawgmoth's "For each {B} in a cost, you may pay 2 life
// rather than pay that mana"); when anyColor is set, every coloured pip
// (plain, hybrid, twobrid, Phyrexian or hybrid-Phyrexian) is payable by ANY
// colour in the pool -- the may-play grant's MayPlayIgnoreColor$ rider, "you
// may spend mana as though it were mana of any color to cast it" (CR 401.5).
// A {C} pip stays colourless-only under anyColor: CR 107.4c's "any color"
// never includes colourless. Both grants keep main search's deterministic
// first-alternative preference; the expanded alternatives are tried in fixed
// WUBRG order (see anyColorAlts).
//
// The life parameter is the payer's life total, which may already be 0 or
// less mid-cast (Ancient Tomb's own 2 damage landing between two planned
// activations): state-based actions are not checked until a player would
// receive priority (CR 704.3), and the payer may still finish paying. The
// gate therefore binds only the cost's FIXED life component (c.Life): paying
// N>0 life requires life >= N (CR 119.4), while a cost with no life
// component pays 0 life, which is always legal, whatever the life total. The
// Phyrexian/life pip alternatives inside the search still require life >= 2
// each, so a dead payer can never pay a pip with life.
func (c Cost) resolveManaWith(pool, snow state.Mana, typed [7]state.Mana, life int32, bLifeOK bool, rider pipRider, conv *manaConv) (manaPayment, bool) {
	if c.Life > 0 && life < c.Life {
		return manaPayment{}, false
	}
	pips := c.costPips(bLifeOK, rider)
	rem := pool
	sn := snow
	tp := typed
	life -= c.Life
	lifeSpent := c.Life
	// pipExact reports whether col is one of the pip's colour alternatives
	// (a strict colour pip lists its colour once; a hybrid lists two).
	pipExact := func(p pip, col byte) bool {
		for _, alt := range p.alts {
			if alt.color == col {
				return true
			}
		}
		return false
	}
	// pipAccepts reports whether one unit of pool colour col (manaLetters
	// index) may pay pip p under conv. Exact colours always match; conv
	// widens (wild/to) and narrows (onlyC) around that base. Non-colour
	// alternatives (generic, life, snow) are unaffected by conv.
	pipAccepts := func(p pip, col byte, di int) bool {
		isC := len(p.alts) == 1 && p.alts[0].color == 'C'
		if conv == nil {
			return pipExact(p, col)
		}
		if conv.onlyC[di] {
			// The <-C restriction: this pool colour may be spent ONLY as
			// colorless mana -- a {C} pip or generic (generic is handled
			// outside the pip loop and takes any mana), never a coloured
			// or hybrid pip, not even its own colour's.
			return isC
		}
		if pipExact(p, col) {
			return true
		}
		if conv.wild[di] {
			// "spend as mana of any color" widens to every coloured or
			// hybrid pip; the colourless-specific {C} pip is a TYPE, not a
			// colour (CR 107.4c), so it is covered only by the
			// AnyType->AnyType wording (conv.wildC).
			if isC {
				return conv.wildC
			}
			return true
		}
		for _, alt := range p.alts {
			if alt.color != 0 && conv.to[di][state.ManaIndex(alt.color)] {
				return true
			}
		}
		return false
	}
	// finalGeneric is the successful search path's generic requirement: the
	// cost's own Generic plus every monocolour-hybrid pip that paid its
	// generic face on that path. The closing deduction spends exactly it.
	finalGeneric := c.Generic
	var rec func(i int, generic int32) bool
	rec = func(i int, generic int32) bool {
		if i == len(pips) {
			if rem.Total() < generic {
				return false
			}
			finalGeneric = generic
			return true
		}
		p := pips[i]
		// Try each alternative in order: for a hybrid this prefers A over B;
		// for a single-colour pip there is one colour alternative, then the
		// pip's life face for a Phyrexian pip.
		for _, alt := range p.alts {
			switch {
			case alt.color != 0:
				di := state.ManaIndex(alt.color)
				if rem[di] > 0 && pipAccepts(p, alt.color, di) {
					beforeRem, beforeSn, beforeTyped := rem, sn, tp
					takeUnit(&rem, &sn, &tp, di)
					if rec(i+1, generic) {
						return true
					}
					rem, sn, tp = beforeRem, beforeSn, beforeTyped
				}
			case alt.generic > 0:
				// A monocolour hybrid's generic face: this pip joins the
				// generic requirement (tried after the colour face, so a
				// colour unit is preferred when the search can still pay).
				if rec(i+1, generic+alt.generic) {
					return true
				}
			case alt.life > 0:
				if life >= 2 {
					life -= 2
					lifeSpent += 2
					if rec(i+1, generic) {
						return true
					}
					lifeSpent -= 2
					life += 2
				}
			case alt.snow:
				// A {S} pip consumes an actual SNOW unit: both the pool slot
				// and the parallel snow tally, so the unit that leaves is the
				// unit that was snow (never a plain unit misattributed into
				// the tally).
				for di, s := range sn {
					if s > 0 {
						beforeRem, beforeSn, beforeTyped := rem, sn, tp
						rem[di]--
						sn[di]--
						if rec(i+1, generic) {
							return true
						}
						rem, sn, tp = beforeRem, beforeSn, beforeTyped
					}
				}
			}
		}
		// A conversion may let OTHER pool colours pay this pip too -- e.g.
		// "spend white mana as though it were red" offers the pool's white
		// mana for a red pip, which the exact-colour alternatives above
		// cannot see. The walk order is manaLetters (WUBRGC), so the
		// assignment stays deterministic; converted mana is tried only after
		// every exact alternative, so a conversion never displaces an exact
		// payment.
		if conv != nil {
			for di := range manaLetters {
				col := manaLetters[di][0]
				if !pipExact(p, col) && rem[di] > 0 && pipAccepts(p, col, di) {
					beforeRem, beforeSn, beforeTyped := rem, sn, tp
					takeUnit(&rem, &sn, &tp, di)
					if rec(i+1, generic) {
						return true
					}
					rem, sn, tp = beforeRem, beforeSn, beforeTyped
				}
			}
		}
		return false
	}
	if !rec(0, c.Generic) {
		return manaPayment{}, false
	}
	// The search found a pip assignment that leaves enough total mana; deduct
	// the generic requirement from that remainder, preferring colourless then
	// colours in fixed WUBRG order so payment is deterministic. Generic can
	// be paid by any leftover mana, so a total >= Generic always suffices.
	need := finalGeneric
	for _, i := range [...]int{state.MC, state.MW, state.MU, state.MB, state.MR, state.MG} {
		for need > 0 && rem[i] > 0 {
			takeUnit(&rem, &sn, &tp, i)
			need--
		}
	}
	return manaPayment{pool: rem, snow: sn, typed: tp, lifeSpent: lifeSpent}, true
}

// payable reports whether the cost's mana and fixed-life parts can be paid
// by pool, its parallel snow tally and the payer's current life (a Phyrexian
// pip may additionally be paid with two life; a {S} pip only by snow mana).
// This is the offering gate's feasibility question, and the real answer to
// "is there ANY way this cost can be paid right now" -- the same resolveMana
// the payment stage uses, so an offered cost and the cost it charges can
// never disagree.
func (c Cost) payable(pool, snow state.Mana, typed [7]state.Mana, life int32) bool {
	_, ok := c.resolveMana(pool, snow, typed, life, nil)
	return ok
}

func (c Cost) CanPay(p state.Mana) bool {
	// Pool-only feasibility, no life and no snow offered: a hybrid must be
	// paid by one of its colours in the pool, a Phyrexian pip by its colour,
	// a monocolour hybrid by its colour (its generic face is not offered
	// here) and a {S} pip is unpayable. This is the pure pricing question the
	// corpus invariants ask, and it never treats a hybrid as generic nor lets
	// colourless `pay` it.
	_, ok := c.resolveMana(p, state.Mana{}, [7]state.Mana{}, 0, nil)
	return ok
}

// Pay spends the cost from a pool and returns what is left. Coloured
// requirements come out first so generic can never strand a colour the cost
// still needs; hybrid pips take one of their pair and Phyrexian pips their
// colour (pool-only -- the cast flow's payMana handles the life half and
// passes a fully-resolved cost here). Mana-only: non-mana parts
// (Tap/Sac/Discard/SubCounter) are the cast flow's own job (rules/cast.go), never
// this function's.
func (c Cost) Pay(p state.Mana) (state.Mana, bool) {
	// Pool-only: no life and no snow are offered, so a Phyrexian pip is paid
	// by its colour (the cast flow's payMana handles the life half and passes
	// a fully resolved cost here). resolveMana already reserves the coloured
	// pips and deducts generic, so the returned pool is fully spent. A failed
	// search returns the input pool untouched.
	pay, ok := c.resolveMana(p, state.Mana{}, [7]state.Mana{}, 0, nil)
	if !ok {
		return p, false
	}
	return pay.pool, true
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
	for toks := (costTokenIter{s: s}); ; {
		sym, ok := toks.next()
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
				c.Generic = addClampedGeneric(c.Generic, int64(n))
				continue
			}
			if m := lifeCost.FindStringSubmatch(sym); m != nil {
				n, err := strconv.ParseInt(m[1], 10, 64)
				if err != nil || n < 0 || n > int64(math.MaxInt32) {
					return Cost{}, false
				}
				c.Life = addClampedGeneric(c.Life, n)
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

// payerGrantsPayLifeInsteadOfB reports whether p's side of the battlefield
// carries a Continuous static granting PayLifeInsteadOf:B to p (K'rrik's
// "Affected$ You | AddKeyword$ PayLifeInsteadOf:B"). Every mana payment and
// every cast/activation offer gate consults it, so a plain {B} pip is
// payable with 2 life anywhere K'rrik is in play under its controller.
func (e *Engine) payerGrantsPayLifeInsteadOfB(p state.PlayerID) bool {
	for _, sv := range e.activeStatics("Continuous") {
		// A member equal to the keyword needs the keyword as a substring, so
		// the allocation-free substring test rejects every other static
		// before the list is split.
		if raw := sv.Params["AddKeyword"]; !strings.Contains(raw, "PayLifeInsteadOf:B") ||
			!slices.Contains(cards.SplitKeywordList(raw), "PayLifeInsteadOf:B") {
			continue
		}
		if effects.MatchesPlayerSpec(e.G, sv.Params["Affected"], p, sv.Controller) {
			return true
		}
	}
	return false
}

// payerGrantsIgnoreColor reports whether an active may-play grant of p's
// carrying MayPlayIgnoreColor$ True selects the card id being cast: the
// grant is p's, its AffectedZone names the card's CURRENT zone (so a card
// being cast the ordinary way from hand never inherits an exile grant), and
// the Affected$ spec matches the card. The IsRemembered predicate inside a
// grant's spec is matched against the CONTINUOUS EFFECT's Remembered set
// (the cards the delivering Effect captured), through the SpecContext the
// ordinary filter grammar already carries -- the same direct-list reading
// restrictionApplies uses for Effect-delivered CantTarget/CantRegenerate.
func (e *Engine) payerGrantsIgnoreColor(p state.PlayerID, id state.ObjID) bool {
	return e.payerGrantsMayPlayRider(p, id, func(ce ContinuousEffect) bool { return ce.MayPlayIgnoreColor })
}

// payerGrantsIgnoreType is payerGrantsIgnoreColor for the MayPlayIgnoreType$
// rider (Rakdos, the Muscle's "mana of any type can be spent to cast those
// spells"): the same zone/Affects/remembered-set reading, keyed on the wider
// rider.
func (e *Engine) payerGrantsIgnoreType(p state.PlayerID, id state.ObjID) bool {
	return e.payerGrantsMayPlayRider(p, id, func(ce ContinuousEffect) bool { return ce.MayPlayIgnoreType })
}

// payerGrantsMayPlayRider is the shared body of the two may-play payment
// riders: it walks the active may-play grants of p's and reports whether one
// carrying the asked rider selects the card id being cast.
func (e *Engine) payerGrantsMayPlayRider(p state.PlayerID, id state.ObjID, rider func(ContinuousEffect) bool) bool {
	o := e.G.Obj(id)
	if o == nil {
		return false
	}
	ces := e.active()
	for i := range ces {
		ce := &ces[i]
		if !ce.MayPlay || !rider(*ce) || ce.Controller != p {
			continue
		}
		if ce.MayPlayPlayerTurn && e.G.Active != p {
			continue
		}
		zones, all, ok := effects.ParseZones(ce.AffectedZone)
		if !ok && !all {
			continue
		}
		if !all && !slices.Contains(zones, o.Zone) {
			continue
		}
		sc := e.withNames(effects.SpecContext{You: ce.Controller, Source: ce.Source,
			Remembered: rememberedTargets(ce.Remembered), Resolving: true})
		if e.matchesSpec(ce.Affects, id, sc) {
			return true
		}
	}
	return false
}

// rememberedTargets lifts a ContinuousEffect's Remembered object ids into
// the []state.Target shape the filter grammar's SpecContext carries.
func rememberedTargets(ids []state.ObjID) []state.Target {
	if len(ids) == 0 {
		return nil
	}
	out := make([]state.Target, 0, len(ids))
	for _, id := range ids {
		out = append(out, state.Target{Obj: id})
	}
	return out
}

// offerRetryFutile reports that offerCastableUsing's potential-target retry
// would re-ask exactly the mana question its first pass just failed: no
// cost static survived a target-independent gate (so the retry, whatever
// targets it binds, composes no static either), the composition is the zero
// costMods the retry's own empty composition is (no waterbend credit was
// folded on top), the scope is not an ability's (whose own ReduceCost$
// reads targets, ownManaReduction), and no ValidCard$ provenance capture is
// pending (the retry would leave it as the first pass did). The retry's
// accept is then manaFeasiblePriced over identical arguments, which failed.
func offerRetryFutile(scope costScope, mods costMods, mayApply, provenance bool) bool {
	if mayApply || provenance || (scope.kind == "Ability" && scope.ab != nil) {
		return false
	}
	return costModsZero(mods)
}

// costModsZero reports whether m is the zero composition in every field
// (TestCostModsZeroCoversEveryField pins the field list).
func costModsZero(m costMods) bool {
	return len(m.raises) == 0 && !m.hasExtra && m.raiseCol == (state.Mana{}) && m.raiseGen == 0 &&
		m.raiseLife == 0 && len(m.reduces) == 0 && m.setFloor == 0 && m.waterbend == 0 &&
		!m.waterbendX && m.waterbendPartX == 0 && m.raiseX == 0
}

// offerSacXModsGated is offerSacXMods behind its own first test
// (costAnnouncesSacX), read through the pointer so the common no-Sac<X> cost
// is refused without copying the cost and the statics into the call.
func (e *Engine) offerSacXModsGated(p state.PlayerID, id state.ObjID, ability bool, base *Cost, statics costStaticViews, scope costScope, tax, delve int32, hyp *state.Mana) (costMods, bool) {
	announced := false
	for i := range base.Sac {
		if base.Sac[i].Announced {
			announced = true
			break
		}
	}
	if !announced {
		return costMods{}, false
	}
	return e.offerSacXMods(p, id, ability, *base, statics, scope, tax, delve, hyp)
}

// offerNamedModsGated is offerNamedMods behind its own first test
// (costHasNamedCount over the composition's extra cost), read through the
// pointers for the same reason.
func (e *Engine) offerNamedModsGated(p state.PlayerID, id state.ObjID, ability bool, base *Cost, mods *costMods, statics costStaticViews, scope costScope, tax, delve int32, hyp *state.Mana) (costMods, bool) {
	named := false
	for i := range mods.extra.Exile {
		if isNamedCountPart(mods.extra.Exile[i]) {
			named = true
			break
		}
	}
	for i := 0; !named && i < len(mods.extra.TapPermanent); i++ {
		named = isNamedCountPart(mods.extra.TapPermanent[i])
	}
	if !named {
		return costMods{}, false
	}
	return e.offerNamedMods(p, id, ability, *base, *mods, statics, scope, tax, delve, hyp)
}
