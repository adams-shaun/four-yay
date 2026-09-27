package seat

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// Bot is a deterministic policy with its own RNG, independent of the
// engine's (rules/rng.go), so a match is reproducible from (engine seed, bot
// seed). It picks from the options the engine offered and nothing else,
// which is the same contract a human client has (Ruling P8).
//
// The policy itself lives in botpolicy (botpolicy/policy.go), shared with
// the rules package's fuzz testBot (rules/testbot_test.go): rules cannot
// import seat without running the dependency order backwards (Ruling F7),
// so both sides build a botpolicy.Board from what they can see and call the
// same botpolicy.Decide. This file is the view-shaped half of that -- it
// converts the projected View into the Board and seeds the bot's own rng.
// There is no second copy of the policy to keep in step; seat/integration_test.go's
// TestBotAdaptersAgree* pins the two halves to the same Board for the same
// game facts.
type Bot struct {
	r              *rand.Rand
	lethalPressure bool
	combinedLethal bool
	blocksAssign   bool
	explore        bool
	// autoPayMana makes priority use an offered payment plan instead of
	// manually activating mana abilities. It is configured once by the hosted
	// table and does not add state or RNG to the match.
	autoPayMana bool
	// cast/castSet are the cast-profile policy's weights: when castSet is
	// true every decision's Board gets brd.Cast = cast before the policy
	// runs, so the cast scorer (cardWorth/castScore/chooseCast) dots its
	// features with the profile instead of the default. Set once at
	// construction from a parsed profile; the Board refill (BoardFromGame /
	// BoardFromView) never touches Board.Cast, so the profile survives the
	// reuse contract untouched. With the embedded default profile (whose
	// weights equal DefaultCastWeights, pinned in botpolicy/profile_test.go)
	// the decisions are identical to NewBot's by the L1 equivalence table.
	cast    botpolicy.CastWeights
	castSet bool
}

// M4: a compile-time assertion that Bot keeps satisfying Seat, since
// seat.go and bot.go otherwise never reference each other.
var _ Seat = (*Bot)(nil)

// M4: Bot also satisfies BoardSeat — the game-shaped half of the adapter
// pair, answered without a projected View. host builds the botpolicy.Board
// (via BoardFromGame, the same Board BoardFromView would lift off the View)
// under the match's exclusive lock and calls this instead of Decide, so a
// bot seat never forces cardViews' string round-trip.
var _ BoardSeat = (*Bot)(nil)

// NewBot seeds the bot's own PCG source. Never math/rand's global functions
// and never the engine's rng: a match's outcome must be a pure function of
// (engine seed, bot seed), nothing else.
func NewBot(seed uint64) *Bot {
	return &Bot{r: rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))}
}

// EnableAutoPayMana configures this bot to select payment-plan witnesses at
// priority. It returns b so every hosted policy constructor can retain its
// existing deterministic policy shape and seed derivation.
func (b *Bot) EnableAutoPayMana() *Bot {
	b.autoPayMana = true
	return b
}

// NewLethalPressureBot returns the measured opt-in AR7 policy. Both Seat
// adapters use the same variant, preserving the Board/View parity contract.
func NewLethalPressureBot(seed uint64) *Bot {
	return &Bot{r: rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)), lethalPressure: true}
}

// NewCombinedLethalBot returns the opt-in AR8 bench policy: the AR7
// per-attacker lethal test plus the combined-attacker subset search
// (botpolicy.CombinedLethalDecide). It is constructed only by cmd/botbench's
// "ar8" policy entry -- it is deliberately absent from the hosted policy
// vocabulary (host.NormalizeBotPolicy), so it can never reach a live table.
func NewCombinedLethalBot(seed uint64) *Bot {
	return &Bot{r: rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)), lethalPressure: true, combinedLethal: true}
}

// NewBlocksBot returns the opt-in BLK bench policy: the default policy with
// KBlockers answered by the whole-assignment heuristic (botpolicy.
// BlocksDecide). It is constructed only by cmd/botbench's "blocks" policy
// entry -- it is deliberately absent from the hosted policy vocabulary
// (host.NormalizeBotPolicy), so it can never reach a live table.
func NewBlocksBot(seed uint64) *Bot {
	return &Bot{r: rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)), lethalPressure: true, blocksAssign: true}
}

// NewExploreBot returns the opt-in coverage-exploration policy
// (botpolicy.ExploreDecide). It is constructed only by cmd/cardfuzz -- it is
// deliberately absent from the hosted policy vocabulary
// (host.NormalizeBotPolicy), so it can never reach a live table.
func NewExploreBot(seed uint64) *Bot {
	return &Bot{r: rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)), lethalPressure: true, explore: true}
}

// NewCastProfileBot returns the cast-profile policy playing the named
// embedded profile (today: the default one). The only error is an embedded
// profile that fails its own strict loader -- never reachable for a valid
// committed file (pinned by botpolicy's profile tests), surfaced as an error
// rather than a panic so the host factory can report it the same way it
// reports an unknown policy name.
func NewCastProfileBot(seed uint64) (*Bot, error) {
	w, err := botpolicy.LoadCastProfile(botpolicy.DefaultCastProfileName)
	if err != nil {
		return nil, fmt.Errorf("seat: %w", err)
	}
	return NewCastProfileBotWithWeights(seed, w), nil
}

// NewCastProfileBotWithWeights returns the cast-profile policy playing the
// given weights -- the shape botbench's -profile flag builds after parsing a
// candidate file, so a profile is benched without a rebuild. Same PCG
// derivation as NewBot, so a profile's RNG consumption matches the
// production bot's exactly.
func NewCastProfileBotWithWeights(seed uint64, w botpolicy.CastWeights) *Bot {
	return &Bot{r: rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)), cast: w, castSet: true}
}

func (b *Bot) decide(brd botpolicy.Board, d *decision.Decision) decision.Intent {
	if b.castSet {
		brd.Cast = b.cast
	}
	if b.autoPayMana {
		if in, ok := b.paymentIntent(brd, d); ok {
			return in
		}
	}
	return b.decideWithoutAutoPay(brd, d)
}

// paymentIntent lets the ordinary casting policy rank offered payment-plan
// casts without ever manually floating mana. It constructs a private priority
// decision in which mana activations are absent and each payable cast missing
// from legacy Options is represented as a normal cast candidate. Thus the
// existing policy still takes a land drop first, keeps all non-payment
// decisions unchanged, and selects the same preferred card among payable
// spells. The submitted witness is copied from the exact offered plan.
func (b *Bot) paymentIntent(brd botpolicy.Board, d *decision.Decision) (decision.Intent, bool) {
	if d == nil || d.Kind != decision.KPriority || len(d.PaymentActions) == 0 {
		return decision.Intent{}, false
	}
	payable := make(map[state.ObjID]decision.PaymentAction, len(d.PaymentActions))
	for _, a := range d.PaymentActions {
		if len(a.Plans) != 0 {
			payable[a.Cast.Object] = a
		}
	}
	if len(payable) == 0 {
		return decision.Intent{}, false
	}

	candidate := d.Clone()
	candidate.Options = make([]decision.Option, 0, len(d.Options)+len(payable))
	candidateToOriginal := make(map[int]int, len(d.Options))
	legacyCast := make(map[state.ObjID]bool, len(d.Options))
	for _, o := range d.Options {
		// These are precisely legalActions' mana abilities. A payment plan
		// performs the required activations atomically, so exposing one here
		// would defeat auto-pay before cast ranking runs.
		if o.Kind == "activate" {
			continue
		}
		originalIndex := o.Index
		o.Index = len(candidate.Options)
		candidateToOriginal[o.Index] = originalIndex
		candidate.Options = append(candidate.Options, o)
		if o.Kind == "cast" {
			legacyCast[o.Obj] = true
		}
	}
	for _, a := range d.PaymentActions {
		if len(a.Plans) == 0 || legacyCast[a.Cast.Object] {
			continue
		}
		candidate.Options = append(candidate.Options, decision.Option{
			Index: len(candidate.Options), Kind: "cast", Obj: a.Cast.Object, Label: a.Label,
		})
	}

	// Use the exact policy variant (including a cast profile) on the private
	// candidate list. Priority choices consume no RNG for hosted policies, so
	// this neither changes subsequent choices nor asks the policy twice.
	in := b.decideWithoutAutoPay(brd, candidate)
	if len(in.Choices) != 1 {
		return decision.Intent{}, false
	}
	for _, o := range candidate.Options {
		if o.Index != in.Choices[0] || o.Kind != "cast" {
			continue
		}
		a, ok := payable[o.Obj]
		if ok {
			return decision.Intent{Seq: d.Seq, Player: d.Player, Payment: &decision.PaymentSelection{
				ActionID: a.ID, Plan: decision.ClonePaymentPlan(a.Plans[0]),
			}}, true
		}
		break
	}
	// The private policy may have preferred a land drop, ability, or pass.
	// Translate that choice back to the original option index so removing mana
	// activations never changes the decision's public index contract.
	if original, ok := candidateToOriginal[in.Choices[0]]; ok {
		return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{original}}, true
	}
	return decision.Intent{}, false
}

// decideWithoutAutoPay is the existing policy dispatch, factored so the
// payment wrapper can ask it to rank a private candidate decision once.
func (b *Bot) decideWithoutAutoPay(brd botpolicy.Board, d *decision.Decision) decision.Intent {
	if b.explore {
		return botpolicy.ExploreDecide(brd, d, b.r)
	}
	if b.combinedLethal {
		return botpolicy.CombinedLethalDecide(brd, d, b.r)
	}
	if b.blocksAssign {
		return botpolicy.BlocksDecide(brd, d, b.r)
	}
	if b.lethalPressure {
		return botpolicy.LethalPressureDecide(brd, d, b.r)
	}
	return botpolicy.Decide(brd, d, b.r)
}

// Decide answers d with the combat-aware policy in botpolicy. v is read
// for two things -- whether it is currently a main phase, and the public
// battlefield/life facts the combat heuristic reads (both halves of
// BoardFromView below) -- the reason Decide takes a View at all rather
// than acting on d alone. rules/testbot_test.go's testBot has no View and
// gets the same facts from the engine (e.G.Step.IsMain, botpolicy.BoardFromGame)
// instead -- the game-shaped half of the same adapter pair;
// TestBotAdaptersAgree* (integration_test.go) pins the two halves to the
// same Board for the same game facts.
func (b *Bot) Decide(_ context.Context, v view.View, d decision.Decision) (decision.Intent, error) {
	return b.decide(BoardFromView(v), &d), nil
}

// DecideBoard is the game-shaped half of Decide: the Board is already built
// (under the match lock, from botpolicy.BoardFromGame reading state.Game and
// the engine's derived P/T/keywords) and handed in as a value, so the bot
// answers without the view->Board string round-trip. The engine the Board
// was built from derives exactly the facts the projected View would have
// carried (TestBotAdaptersAgreeOverWholeGame pins the two halves).
func (b *Bot) DecideBoard(_ context.Context, brd botpolicy.Board, d decision.Decision) (decision.Intent, error) {
	return b.decide(brd, &d), nil
}

// BoardFromView is the view-shaped adapter: the Board the policy reads,
// lifted off the projected View a real client would receive. The combat
// half (Creatures, Life) is every public battlefield creature and life
// total the seat can see -- exactly the facts botpolicy.BoardFromGame
// derives from the engine's state.Game (same zones, same derived P/T and
// keywords, same filters: cardless ability objects and off-battlefield
// ephemerals are dropped by the View itself, and the creature test below
// mirrors cards/face.go's hasType membership check on the joined type
// list). The rules test host computes the same Board from the engine;
// seat/integration_test.go's TestBotAdaptersAgreeOverWholeGame pins the
// two halves to the same facts over a whole game.
func BoardFromView(v view.View) botpolicy.Board {
	b := botpolicy.Board{
		IsMain: v.Phase == "main1" || v.Phase == "main2",
		// The cast scorer's two board-half features (botpolicy/cast.go):
		// FirstMain is the FIRST main phase (the Precombat feature), MyTurn
		// whether the deciding seat is the active player (the
		// InstantOnOwnTurn feature's "own main phase" half — a main phase can
		// belong to another seat, so IsMain alone cannot say it). Same facts
		// the game half derives from g.Step == state.StepMain1 and
		// g.Active == me.
		FirstMain:  v.Phase == "main1",
		MyTurn:     v.Active == v.Viewer,
		Creatures:  make(map[state.ObjID]botpolicy.Creature, 32),
		Life:       make(map[state.PlayerID]int32, len(v.Players)),
		Cards:      make(map[state.ObjID]botpolicy.Card, 16),
		Commanders: make(map[state.ObjID]botpolicy.Commander, 8),
	}
	// The public stack census (C8's facts): the projected StackView list is
	// the stack's own bottom-to-top order, so the census slice is that same
	// order and every read of it is deterministic. IsSpell mirrors the
	// game-shaped half's o.Ability == nil test exactly: the View's Kind is
	// "spell" precisely for the objects whose Ability is nil (an ability
	// object projects as "trigger" or "ability", view/view.go's
	// stackViews).
	b.Stack = make([]botpolicy.StackEntry, 0, len(v.Stack))
	for _, sv := range v.Stack {
		var cmc int32
		var manaCost string
		// A trigger can include its source card for display, but the stack
		// ability itself has no face or mana cost. Only spells have a CMC
		// in the game-shaped adapter (BoardFromGameInto).
		if sv.Kind == "spell" && sv.Card != nil {
			cmc = botpolicy.CmcOf(sv.Card.ManaCost)
			manaCost = sv.Card.ManaCost
		}
		b.Stack = append(b.Stack, botpolicy.StackEntry{ID: sv.ID, Controller: sv.Controller, IsSpell: sv.Kind == "spell", CMC: cmc, ManaCost: manaCost})
	}
	for _, p := range v.Players {
		b.Life[p.ID] = p.Life
		if p.ID == v.Viewer {
			b.LibrarySize = int32(p.LibrarySize)
			b.HandSize = int32(p.HandSize)
			// The tap gate's pool (tap.go): the projecting viewer's own mana
			// pool, the same numbers poolView lifted off the engine state
			// that BoardFromGameInto reads directly, so the two halves agree
			// (pinned non-vacuously by integration_test.go's pool agreement).
			for sym, n := range p.Pool {
				b.Pool[state.ManaIndex(sym[0])] = n
			}
			// The restricted share of that pool (Board.PoolRestricted): the
			// View names exactly the batches with a spend limit, carrying the
			// producing counter verbatim, the game half's source too.
			for _, r := range p.PoolRestrictions {
				if r.Amount > 0 {
					b.PoolRestricted[state.ManaSlot(r.Color)] += r.Amount
				}
			}
		}
		for _, cv := range p.Battlefield {
			if !isCreatureView(cv) {
				continue
			}
			b.Creatures[cv.ID] = botpolicy.Creature{
				Power:      cv.Power,
				Toughness:  cv.Toughness,
				Damage:     cv.Damage,
				Keywords:   cv.Keywords,
				Tapped:     cv.Tapped,
				Controller: cv.Controller,
			}
		}
		// The commander bookkeeping: the projected roster (p.Commanders,
		// never shrunk as commanders are cast) carries identity and the CR
		// 903.8 cast counts; p.Command's zone-list membership is
		// InCommandZone's exact mirror of the game half's
		// g.Zone(ZCommand, owner) read; every player's CmdDamage re-keys
		// the CR 903.10 clock by commander object id exactly as the game
		// half's dense-index transpose does.
		for k, cv := range p.Commanders {
			var casts int32
			if k < len(p.CommanderCasts) {
				casts = p.CommanderCasts[k]
			}
			cmdr := botpolicy.Commander{Casts: casts}
			for _, cz := range p.Command {
				if cz.ID == cv.ID {
					cmdr.InCommandZone = true
					break
				}
			}
			b.Commanders[cv.ID] = cmdr
		}
	}
	// The CR 903.10 clock, filled from every player's damage keys. The map
	// iteration order is irrelevant: each entry lands in b.Commanders[id]'s
	// own Damage map keyed by the player who took it, and no policy branch
	// reads anything in order this fill could disturb (ties break on ObjID
	// or option index). A commander a damaged player names is always
	// already in b.Commanders (damage only ever accrues to a commander) —
	// the nil-map read would be a zero entry otherwise, never a crash.
	for _, q := range v.Players {
		for id, tally := range q.CmdDamage {
			cmdr := b.Commanders[id]
			if cmdr.Damage == nil {
				cmdr.Damage = make(map[state.PlayerID]int32, 2)
			}
			cmdr.Damage[q.ID] = tally
			b.Commanders[id] = cmdr
		}
	}
	// The casting Card census: the viewer's own hand, graveyard, battlefield
	// and command-zone CardViews — the deciding seat's own legally-seen
	// zones — mirroring exactly what BoardFromGame fills from state.Game for
	// that seat (cast.go's CmcOf and the type-word check on the same printed
	// fields, and the engine's derived Power that the View already projects
	// as cv.Power), so the casting policy ranks the same card the same way
	// on both halves. The fill is per zone, because the tap gate (tap.go)
	// reads Castable from the zone a card sits in: a hand or command-zone
	// card is worth mana (its cast is offered as soon as the pool pays the
	// cost; a command-zone card is a commander the CR 903.8 tax prices), a
	// graveyard card only when the View's derived keyword list carries
	// Flashback (the same Derived list the engine's own flashback gate
	// reads), and a battlefield permanent never.
	//
	// The public battlefield census (vote_card1): another seat's battlefield
	// permanents are public CR 400.2 facts projected for every viewer, and
	// they are exactly what a ballot offers a voter (VoteCard$'s Council's
	// Judgment filter names only permanents the caster does not control),
	// so the loop below fills their WORTH facts (Creature/Power/Toughness/
	// CMC/Basic/ManaCost, what cardWorth prices) for every player, not only
	// the viewer — botpolicy.BoardFromGameInto's opponent-battlefield walk
	// is this call's exact mirror. The seat-relative facts stay ZERO on a
	// foreign entry (OnBattlefield/Produces/Tapped/Castable/Activated/
	// InstantSpeed/AttachedTo are the deciding seat's own-board facts; the
	// land-drop greedy's mana readers would otherwise count an opponent's
	// lands as the seat's own), so the foreign fill below is its own walk,
	// not a fillZone call. A facedown opponent permanent projects stripped
	// (no printed Types/Power), so its entry lands all-zero here — exactly
	// the zero-fact Card the game half writes for the same object.
	fillZone := func(zone []view.CardView, castable func(view.CardView) bool, battlefield bool) {
		for _, cv := range zone {
			var produces cards.ManaProduction
			if cv.Produces != nil {
				produces = *cv.Produces
			}
			b.Cards[cv.ID] = botpolicy.Card{
				Creature:      isCreatureView(cv),
				Power:         cv.Power,
				Toughness:     cv.Toughness,
				CMC:           botpolicy.CmcOf(cv.ManaCost),
				Basic:         hasBasicView(cv),
				AttachedTo:    cv.AttachedTo,
				Activated:     cv.ActivatedThisTurn,
				ManaCost:      cv.ManaCost,
				Castable:      castable(cv),
				OnBattlefield: battlefield,
				Tapped:        cv.Tapped,
				Sick:          cv.SummonSick,
				Produces:      produces,
				InstantSpeed:  instantSpeedView(cv),
				Counter:       cv.SpellAPI == "Counter",
			}
		}
	}
	aCastable := func(view.CardView) bool { return true }
	notCastable := func(view.CardView) bool { return false }
	for _, p := range v.Players {
		if p.ID != v.Viewer {
			for _, cv := range p.Battlefield {
				b.Cards[cv.ID] = botpolicy.Card{
					Creature:  isCreatureView(cv),
					Power:     cv.Power,
					Toughness: cv.Toughness,
					CMC:       botpolicy.CmcOf(cv.ManaCost),
					Basic:     hasBasicView(cv),
					ManaCost:  cv.ManaCost,
				}
			}
			continue
		}
		fillZone(p.Hand, aCastable, false)
		fillZone(p.Graveyard, hasFlashbackView, false)
		fillZone(p.Battlefield, notCastable, true)
		fillZone(p.Command, aCastable, false)
	}
	return b
}

// hasFlashbackView is the view-shaped half of the tap gate's
// graveyard-castability test (botpolicy.combat.go's game-shaped half): the
// projected keyword list is the engine's Derived() output, so checking the
// card's keywords for "Flashback" (cards.KeywordHead-stripped, the same
// head test the game half runs) agrees with the engine's own flashback
// gate read.
func hasFlashbackView(cv view.CardView) bool {
	for _, k := range cv.Keywords {
		if strings.EqualFold(cards.KeywordHead(k), "Flashback") {
			return true
		}
	}
	return false
}

// isCreatureView is the view-shaped half of "is this battlefield object a
// creature": the game-shaped half reads the object's own Face().IsCreature
// (cards/face.go's hasType, an EqualFold membership check), and the View
// carries only the same type words joined into one string, so this mirrors
// exactly that check on the joined list.
func isCreatureView(cv view.CardView) bool {
	for t := range strings.FieldsSeq(cv.Types) {
		if strings.EqualFold(t, "Creature") {
			return true
		}
	}
	return false
}

// hasBasicView is the view-shaped half of "is this a basic land": the
// game-shaped half reads the face's own type list (BoardFromGame's
// Card.Basic via hasTypeWord), and the View carries the same type words
// joined into one string, so this mirrors exactly that check on the
// joined list.
func hasBasicView(cv view.CardView) bool {
	for t := range strings.FieldsSeq(cv.Types) {
		if strings.EqualFold(t, "Basic") {
			return true
		}
	}
	return false
}

// instantSpeedView is the view-shaped half of "can this card be cast at
// instant speed" (botpolicy.Card.InstantSpeed): the card is an Instant, or
// it carries the Flash keyword. The game-shaped half (BoardFromGame) reads
// the face's own type list and the engine's derived keyword list, which the
// View carries as cv.Types and cv.Keywords, so the two halves agree.
func instantSpeedView(cv view.CardView) bool {
	for t := range strings.FieldsSeq(cv.Types) {
		if strings.EqualFold(t, "Instant") {
			return true
		}
	}
	for _, k := range cv.Keywords {
		if strings.EqualFold(cards.KeywordHead(k), "Flash") {
			return true
		}
	}
	return false
}
