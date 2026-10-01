package rules

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/deck"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

func (e *Engine) Pending() *decision.Decision { return e.pending }

// OwnDeck returns a detached copy of p's genesis deck manifest, or nil when p
// is not a configured seat. The manifest is observer data, not game state.
func (e *Engine) OwnDeck(p state.PlayerID) *deck.Manifest {
	if e == nil || int(p) >= len(e.deckManifests) {
		return nil
	}
	m := e.deckManifests[p].Clone()
	return &m
}

// OwnDeckShared is OwnDeck without the copy: a pointer to p's genesis
// manifest in engine storage, or nil when p is not a configured seat. The
// manifest is immutable for the engine's whole life and is shared, not
// copied, by every Clone (cloneWith), so the pointer stays valid and
// unchanging for as long as the caller holds it -- but the caller must
// never write through it (or through its slices), because that write would
// reach this engine, every clone of it and every later OwnDeck copy.
// It is the per-decision read path (botpolicy.BoardFromGameInto, which the
// search loop runs at every bot decision of every simulation); anything
// that publishes or hands the manifest outside the process (view.View's
// OwnDeck) takes OwnDeck's detached copy instead.
func (e *Engine) OwnDeckShared(p state.PlayerID) *deck.Manifest {
	if e == nil || int(p) >= len(e.deckManifests) {
		return nil
	}
	return &e.deckManifests[p]
}

// EnsurePaymentActions lazily builds the payment extension for the current
// priority decision. It is a pure derived read: it emits no event and does
// not advance sequence or RNG. The built marker also caches an empty result.
func (e *Engine) EnsurePaymentActions() []decision.PaymentAction {
	d := e.pending
	if d == nil || d.Kind != decision.KPriority {
		return nil
	}
	if !d.PaymentActionsBuilt {
		// The pending Options are the list BaseOptionIndex indexes: ask built
		// them with the same legalActions walk, so the builder reuses them
		// instead of walking again.
		gen := e.derivedMemoGen
		// The build is a pure read of the board ask's offer walk read a
		// moment ago: when that walk's memo tail is still exact (no Submit,
		// no other scope, only ask's DecisionAsk marker logged since --
		// derivedmemo.go), RESUME its generation so the build's own walks
		// are served the Derived results and walk caches ask already built
		// instead of re-deriving the whole board. A dead tail opens a fresh
		// generation exactly as the build's own scope always did.
		e.BeginDerivedReads()
		actions := e.paymentActionsForPriority(d.Player, d.Seq, d.Options)
		e.EndDerivedReads()
		d.PaymentActions = (&decision.Decision{PaymentActions: actions}).Clone().PaymentActions
		d.PaymentActionsBuilt = true
		// A builder walk performs derived reads in its own memo generation.
		// Make that completed read the resumable tail so a later BoardSeat
		// build can still use BeginDerivedReads without reopening the walk.
		// A build that opened no walk (a declined pool) leaves ask's own
		// tail, which is still exact, in place.
		if e.derivedMemoGen != gen {
			e.recordDerivedMemoTail(d)
		}
	}
	return d.PaymentActions
}

// seatFacingName is the seat-facing identity for client-facing prompt and
// option-label text (the priority prompt, the keep/mulligan prompt, the
// attacker, cumulative-upkeep and target option labels). PlayerName is
// supplied by the table and identifies a human even when two players chose
// the same deck; Name is the deterministic fallback for bots or callers
// without display names. Decision prompts and option labels are NOT chain
// content (rules/engine.go's ask emits only DecisionAsk{Kind}), so
// composing them from PlayerName moves no chain head — but event text must
// stay on Name (the F3 invariant, rules/playername_test.go). The final
// fallback is defensive: decision seats originate from AliveFrom, but
// malformed state must not panic while constructing a client decision.
func seatFacingName(g *state.Game, p state.PlayerID) string {
	if g != nil && int(p) < len(g.Players) {
		if name := g.Players[p].PlayerName; name != "" {
			return name
		}
		if name := g.Players[p].Name; name != "" {
			return name
		}
	}
	return fmt.Sprintf("seat %d", p)
}

// BeginLibrarySearch marks the start of one library search's execution: the
// searcher's repl:Moved FoundSearchingLibrary$ replacements apply to exactly
// the moves the search emits.
func (e *Engine) BeginLibrarySearch(owner state.PlayerID) {
	e.searchDepth++
	e.searchingBy = owner
}

// EndLibrarySearch closes the innermost search scope.
func (e *Engine) EndLibrarySearch() {
	if e.searchDepth > 0 {
		e.searchDepth--
	}
	if e.searchDepth == 0 {
		e.searchingBy = 0
	}
}

// searchControlRedirect applies the ControlOpponentsSearchingLibrary$ static
// family (Opposition Agent's "You control your opponents while they're
// searching their libraries"): the search pick's decision is posed to the
// static's controller instead of the searching player. Scope: the pick (and
// any later ask the search flow poses) — the "control" of the searched
// player's every action is the wider grant Forge models; this build
// redirects the decisions the search itself asks, which is what a
// resolution can observe. The first static in activeStatics' deterministic
// APNAP order wins; an Affected$ spec that does not match the searching
// player leaves the static inert.
func (e *Engine) searchControlRedirect(d *decision.Decision) {
	if d.ResumeKind != "search" {
		return
	}
	for _, sv := range e.activeStatics("Continuous") {
		if strings.TrimSpace(sv.Params["ControlOpponentsSearchingLibrary"]) != "You" {
			continue
		}
		if sv.Controller == d.Player {
			continue
		}
		if spec := sv.Params["Affected"]; spec != "" &&
			!effects.MatchesPlayerSpec(e.G, spec, d.Player, sv.Controller) {
			continue
		}
		d.Player = sv.Controller
		return
	}
}

// controlPlayerRedirect is CR 720's decision-ownership rule: while one player
// controls another (api:ControlPlayer), every decision the CONTROLLED player
// would be offered is answered by the CONTROLLER instead. It rewrites the
// decision's Player, exactly the mechanism searchControlRedirect above uses,
// so Validate/Submit, the host's seat routing and the replay log all agree
// that the controller is the answering seat. The controlled player's own
// hidden information is not widened here (a later ticket's view concern);
// what this proves is the interval -- the redirect applies only while the
// grant is live and stops the turn it expires (rules/turn.go beginTurn).
func (e *Engine) controlPlayerRedirect(d *decision.Decision) {
	if d.Player < 0 || int(d.Player) >= len(e.G.Players) {
		return
	}
	ctl, ok := e.G.ControlledBy[d.Player]
	if !ok || ctl == d.Player {
		return
	}
	// CR 720.2 scopes the control to the controlled player's NEXT turn: the
	// controller answers only decisions that seat would be offered DURING
	// that turn, never a response priority on a turn that passes before it
	// begins (the controller's own turn the grant resolved on, or another
	// opponent's turn in between). This mirrors expirePlayerControl's
	// arithmetic exactly: the grant armed on turn T is live only while the
	// controlled seat is active on a strictly later turn (Turn > armed), the
	// same Turn > armed test that expires it at that turn's end. Without this
	// the redirect spilled across the whole interval -- the controlled seat's
	// on-controller-turn response priority was handed to the controller before
	// their controlled turn (findings-t2 MAJOR).
	if e.G.Active != d.Player || e.G.Turn <= e.G.ControlArmedTurn[d.Player] {
		return
	}
	// CR 720.6 keeps control acyclic; a corrupt or hand-built fold naming a
	// cycle must not move the decision back onto the controlled seat.
	if _, loop := e.G.ControlledBy[ctl]; loop {
		return
	}
	d.Player = ctl
}

func (e *Engine) GetCurrentEffectFrame() effects.EffectFrame {
	return e.currentEffectFrame
}

func (e *Engine) SetCurrentEffectFrame(frame effects.EffectFrame) {
	e.currentEffectFrame = frame
}
