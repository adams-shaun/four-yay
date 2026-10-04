package rules

import (
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// openingRound serializes "may begin the game" choices after every London
// mulligan and bottoming choice has fixed the opening hands, but before turn
// one. The source is an SVar name, rather than a card name, so every
// FromHand/FromOpeningHand script with the normal self-to-battlefield shape
// shares this flow.
// Existing chooseFor values occupy 0 through 9 (chooseManaExile is 9).
const chooseOpening chooseFor = iota + 10

type openingRound struct {
	start     state.PlayerID  `clone:"deep"`
	mulligans int             `clone:"deep"`
	effects   []openingEffect `clone:"deep"`
	index     int             `clone:"deep"`
	exile     state.ObjID     `clone:"deep"`
	// awaiting marks a round whose current effect's own work posed a
	// decision of its own -- the card entering the battlefield from the
	// opening hand asked an "as this enters" choice (Leyline of
	// Transformation's creature type) through Engine.Ask, or an ordinary
	// effect-registry opening action asked mid-resolution. The round must not
	// pose the next "begin the game with" ask on top of it (ask's overwrite
	// guard: the pending answer would be orphaned); Submit's tail steps the
	// round on (resumeOpening) once that decision and everything it handed
	// on to has been answered.
	awaiting bool `clone:"deep"`
	// exileAsk is the Gemstone-shape mandatory hand-exile ask held back
	// because the entry that precedes it posed a decision first; resumeOpening
	// poses it once that decision is answered.
	exileAsk *decision.Decision `clone:"deep"`
}

type openingEffect struct {
	player state.PlayerID
	card   state.ObjID
	svar   string
}

// openingActionSVar finds the SVar which performs the pregame action. Forge's
// keyword parameter is normally that action's name (FromHand, RevealCard),
// but some scripts name its delayed child instead: Chancellor of the Tangle
// says ManaOnMain while RevealCard is the actual reveal-and-register action.
// Prefer an explicit pregame action, then a SVar which chains into the named
// child. Sorting keeps the fallback deterministic if a future script has more
// than one descriptive opening action.
func openingActionSVar(f *cards.Face, raw string) string {
	name, _, _ := strings.Cut(raw, ":")
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if sa := cards.ResolveSVar(f.SVars, name); sa != nil &&
		(strings.HasPrefix(name, "From") || strings.HasPrefix(name, "Exile") ||
			strings.Contains(strings.ToLower(sa.ParamStr(cards.PKSpellDescription)), "opening hand")) {
		return name
	}
	keys := make([]string, 0, len(f.SVars))
	for key := range f.SVars {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		sa := cards.ResolveSVar(f.SVars, key)
		if sa != nil && sa.ParamStr(cards.PKSubAbility) == name &&
			strings.Contains(strings.ToLower(sa.ParamStr(cards.PKSpellDescription)), "opening hand") {
			return key
		}
	}
	for _, key := range keys {
		sa := cards.ResolveSVar(f.SVars, key)
		if sa != nil && strings.Contains(strings.ToLower(sa.ParamStr(cards.PKSpellDescription)), "opening hand") {
			return key
		}
	}
	// A parameter that is itself an SVar remains the final conservative
	// fallback for scripts without Forge's descriptive field.
	if cards.ResolveSVar(f.SVars, name) != nil {
		return name
	}
	return ""
}

func (e *Engine) newOpeningRound(start state.PlayerID, mulligans int) openingRound {
	r := openingRound{start: start, mulligans: mulligans}
	for _, p := range e.G.AliveFrom(start) {
		for _, id := range e.G.Zone(state.ZHand, p) {
			o := e.G.Obj(id)
			if o == nil || o.Face() == nil {
				continue
			}
			raw, ok := o.Face().KeywordParam("MayEffectFromOpeningHand")
			if !ok {
				continue
			}
			_, requirement, _ := strings.Cut(raw, ":")
			// !PlayFirst is Gemstone Caverns' and Impatient Iguana's gate.
			// Other FromHand shapes are unconditional; unsupported predicates
			// fail closed rather than accidentally granting a pregame effect.
			if strings.Contains(requirement, "!PlayFirst") && p == start {
				continue
			}
			svar := openingActionSVar(o.Face(), raw)
			if svar == "" {
				continue
			}
			r.effects = append(r.effects, openingEffect{player: p, card: id, svar: svar})
		}
	}
	return r
}

func (e *Engine) stepOpening() {
	if e.opening.exile != 0 {
		return
	}
	for e.opening.index < len(e.opening.effects) {
		ef := e.opening.effects[e.opening.index]
		o := e.G.Obj(ef.card)
		if o == nil || o.Zone != state.ZHand || o.Face() == nil {
			e.opening.index++
			continue
		}
		d := decision.New(ef.player, decision.KChoose, "Begin the game with "+o.Face().Name+"?", 1, 1,
			[]decision.Option{{Index: 0, Kind: "opening_yes", Obj: ef.card, Label: "Yes"},
				{Index: 1, Kind: "opening_no", Obj: ef.card, Label: "No"}})
		e.choosing = chooseOpening
		e.ask(d)
		return
	}
	e.finishOpening()
}

// applyOpeningEffect handles the common keyword expansion: move Self from the
// opening hand to the battlefield, then apply its immediate PutCounter sub.
// A following mandatory hand-to-exile ChangeZone is represented by a real
// KChoose continuation (Gemstone Caverns), rather than a deterministic card.
func (e *Engine) applyOpeningEffect(ef openingEffect) {
	o := e.G.Obj(ef.card)
	if o == nil || o.Face() == nil {
		return
	}
	sa := cards.ResolveSVar(o.Face().SVars, ef.svar)
	if sa == nil {
		return
	}
	if sa.API != "ChangeZone" || !effects.ChangeZoneOf(sa).OriginExactly(state.ZHand) || !effects.ChangeZoneOf(sa).DestinationIs(state.ZBattlefield) {
		// Reveal, exile, token and delayed-effect opening scripts use the
		// ordinary effect registry too. The FromHand battlefield shape below
		// is split out only because Gemstone's mandatory follow-up needs its
		// own pregame card-choice continuation.
		ctx := effects.NewCtxPtr(ef.card, ef.player, effects.CtxInit{})
		effects.SetSVars(ctx, o.Face().SVars)
		effects.Resolve(e, ctx, sa)
		e.registerOpeningEffectTriggers(ef, sa)
		return
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: ef.card, From: state.ZHand, To: state.ZBattlefield, Text: "opening hand effect"})
	sub := sa.Sub
	if sub != nil && sub.API == "PutCounter" {
		pc := effects.PutCounterOf(sub)
		n := pc.CounterNumValue(e, effects.NewCtxPtr(ef.card, ef.player, effects.CtxInit{}))
		// A pregame opening-hand counter is put by the effect's player, with
		// no stack cause: publish the adder for the AddCounter class.
		prevAdder := e.SetCounterAdder(ef.player)
		e.emit(events.Event{Kind: events.CounterChange, Obj: ef.card, Counter: pc.CounterType, Amount: n})
		e.SetCounterAdder(prevAdder)
		sa = sub
		sub = sub.Sub
	}
	if sub != nil && sub.API == "ChangeZone" && effects.ChangeZoneOf(sub).OriginExactly(state.ZHand) && effects.ChangeZoneOf(sub).DestinationIs(state.ZExile) {
		e.opening.exile = ef.card
		d := &decision.Decision{Player: ef.player, Kind: decision.KChoose, Min: 1, Max: 1,
			Prompt: "Exile a card from your hand", Source: ef.card}
		for _, id := range e.G.Zone(state.ZHand, ef.player) {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "opening_exile", Obj: id, Label: e.G.Obj(id).Face().Name})
		}
		if len(d.Options) > 0 {
			if e.pending != nil || e.Suspended() {
				e.opening.exileAsk = d
				return
			}
			e.choosing = chooseOpening
			e.ask(d)
		}
	}
}

func (e *Engine) handleOpening(d *decision.Decision, in decision.Intent) {
	chosen := d.Chosen(in)
	if len(chosen) == 0 {
		return
	}
	if e.opening.exile != 0 {
		e.emit(events.Event{Kind: events.MoveZone, Obj: chosen[0].Obj, From: state.ZHand, To: state.ZExile, Text: "opening hand effect"})
		e.opening.exile = 0
		e.opening.index++
		e.stepOpening()
		return
	}
	if chosen[0].Kind == "opening_yes" && e.opening.index < len(e.opening.effects) {
		ef := e.opening.effects[e.opening.index]
		e.applyOpeningEffect(ef)
		e.tape.ResolutionDone()
		// Impatient Iguana's opening-hand effect changes the player who takes
		// turn one. This round-local value is consumed by finishOpening.
		if o := e.G.Obj(ef.card); o != nil && o.Face() != nil {
			if sa := cards.ResolveSVar(o.Face().SVars, ef.svar); sa != nil && sa.ParamStr(cards.PKBecomeStartingPlayer) == "True" {
				e.opening.start = ef.player
			}
		}
		if e.opening.exile != 0 {
			return
		}
		if e.pending != nil || e.Suspended() {
			e.opening.awaiting = true
			return
		}
	}
	e.opening.index++
	e.stepOpening()
}

// resumeOpening steps an opening round that parked behind a decision its
// current effect posed (openingRound.awaiting) once the engine is idle
// again: nothing pending and no suspended resolution left to finish.
func (e *Engine) resumeOpening() {
	if (!e.opening.awaiting && e.opening.exileAsk == nil) || e.pending != nil || e.Suspended() {
		return
	}
	if d := e.opening.exileAsk; d != nil {
		e.opening.exileAsk = nil
		e.choosing = chooseOpening
		e.ask(d)
		return
	}
	e.opening.awaiting = false
	e.opening.index++
	e.stepOpening()
}

// registerOpeningEffectTriggers turns an opening Effect's one-off trigger
// children into the ordinary delayed-trigger mechanism. The source may remain
// in hand: delayed triggers intentionally survive their source moving zones,
// and the firing resolves the named SVar with the same normal stack/resume
// machinery as any other trigger. Phase children fire on the registered step
// (rules.checkDelayedTriggers); SpellCast children fire on the first matching
// spell cast (rules.checkEventDelayedTriggers), one registration per player
// the Effect's EffectOwner$ selector names -- Chancellor of the Annex's
// `EffectOwner$ Opponent` with `ValidActivatingPlayer$ You` is per opponent,
// exactly the oracle's "when each opponent casts their first spell". Any
// other trigger mode, a non-one-off body, an OptionalDecider$ ask (the
// DelayedPush path never poses one) or an unresolvable Execute$ fails closed:
// nothing is registered rather than silently pretending to work.
func (e *Engine) registerOpeningEffectTriggers(ef openingEffect, first *cards.SA) {
	for sa := first; sa != nil; {
		if sa.API == "Effect" {
			for name := range strings.FieldsSeq(effects.EffectOf(sa).Triggers) {
				o := e.G.Obj(ef.card)
				if o == nil || o.Face() == nil {
					return
				}
				t, ok := cards.ParseTriggerLine(o.Face().SVars[name])
				if !ok || t.ParamStr(cards.PKOneOff) != "True" || t.ParamStr(cards.PKOptionalDecider) != "" {
					continue
				}
				exec := t.ParamStr(cards.PKExecute)
				if exec == "" || cards.ResolveSVar(o.Face().SVars, exec) == nil {
					continue
				}
				switch t.ModeKind() {
				case cards.TriggerPhase:
					var step state.Step
					switch registerOpeningEffectTriggersCodes.Code(string(strings.TrimSpace(t.ParamStr(cards.PKPhase)))) {
					case registerOpeningEffectTriggersUpkeep:
						step = state.StepUpkeep
					case registerOpeningEffectTriggersMain1:
						step = state.StepMain1
					default:
						continue
					}
					if e.openingEffectAlreadyRegistered(ef.card, ef.player, t.Mode, exec) {
						continue
					}
					e.emit(events.Event{Kind: events.DelayedRegister, Obj: ef.card, Player: ef.player,
						Step: step, Counter: exec, Text: t.ParamStr(cards.PKPhase)})
				case cards.TriggerSpellCast:
					// Step carries the registration's decoding guard only
					// (events.Apply requires a valid Step); an event-matched
					// registration never fires on a step --
					// checkDelayedTriggers skips it.
					for _, p := range e.openingEffectOwners(sa, ef.player) {
						if e.openingEffectAlreadyRegistered(ef.card, p, t.Mode, exec) {
							continue
						}
						e.emit(events.Event{Kind: events.DelayedRegister, Obj: ef.card, Player: p,
							Step: e.G.Step, Counter: exec, Text: "SpellCast:" + name})
					}
				}
			}
		}
		next := sa.Sub
		if next == nil && sa.ParamStr(cards.PKSubAbility) != "" {
			if o := e.G.Obj(ef.card); o != nil && o.Face() != nil {
				next = cards.ResolveSVar(o.Face().SVars, sa.ParamStr(cards.PKSubAbility))
			}
		}
		sa = next
	}
}

// openingEffectOwners resolves an opening Effect's EffectOwner$ player
// selector to the players the effect (and its trigger) belongs to. The
// empty/You selector keeps the revealer; Opponent/Other fans out to every
// other surviving seat, which is what makes Chancellor of the Annex tax EACH
// opponent's first spell while the trigger body's own "You" resolves, per
// registration, to that opponent. The selector grammar has exactly one home
// (effects.EffectOwnerPlayers, the same resolver the generic effEffect path
// uses), so the pregame and generic registrations agree on the owner set and
// an opening body is never owned by two different seats. Unrecognized
// selectors fail closed (an empty list registers nothing) rather than
// guessing a player set.
func (e *Engine) openingEffectOwners(sa *cards.SA, you state.PlayerID) []state.PlayerID {
	ps, _ := effects.EffectOwnerPlayers(e, effects.NewCtxPtr(0, you, effects.CtxInit{}), effects.EffectOf(sa).EffectOwner)
	return ps
}

// openingEffectAlreadyRegistered reports whether the generic effEffect path --
// which runs before this pregame pass, inside applyOpeningEffect's
// effects.Resolve -- already minted the equivalent registration for an
// opening Effect's trigger body. The generic path now resolves EffectOwner$
// itself, so without this check the two paths would register the same body
// twice for the same owner and fire it twice. Matching on source, owner and
// Execute names the SAME promise: an event-matched registration stores its
// mode in EventMode, a Mode$ Phase one carries none. When the generic path
// did not register the body (e.g. the lifetime guard withheld a longer-lived
// Effect), this returns false and the pregame pass registers it, so the
// opening-hand case keeps working.
func (e *Engine) openingEffectAlreadyRegistered(card state.ObjID, owner state.PlayerID, mode, exec string) bool {
	for i := range e.G.Delayed {
		dt := &e.G.Delayed[i]
		if dt.Source != card || dt.Controller != owner || dt.Execute != exec {
			continue
		}
		if mode == "Phase" {
			if dt.EventMode == "" {
				return true
			}
			continue
		}
		if dt.EventMode == mode {
			return true
		}
	}
	return false
}

func (e *Engine) finishOpening() {
	start := e.opening.start
	e.opening = openingRound{}
	e.beginTurn(start)
}

func init() { effects.RegisterNonAPI("kw:MayEffectFromOpeningHand") }

type registerOpeningEffectTriggersCode uint16

const (
	registerOpeningEffectTriggersUpkeep registerOpeningEffectTriggersCode = iota + 1
	registerOpeningEffectTriggersMain1
)

var registerOpeningEffectTriggersCodes = state.NewStrCodes(
	state.StrEntry[registerOpeningEffectTriggersCode]{Key: "Upkeep", Val: registerOpeningEffectTriggersUpkeep},
	state.StrEntry[registerOpeningEffectTriggersCode]{Key: "Main1", Val: registerOpeningEffectTriggersMain1},
)
