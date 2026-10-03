package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() { Register("Investigate", effInvestigate) }

// clueTokenKey is the corpus token script investigate mints
// (.cards/tokenscripts/c_a_clue_draw.txt: an Artifact Clue with the "{2},
// Sacrifice this token: Draw a card" activated ability).
const clueTokenKey = "c_a_clue_draw"

// effInvestigate implements the Investigate primitive (CR 701.36a; 142
// corpus files -- 120 DB$, 16 AB$, 9 SP$): create Num$ Clue tokens for each
// investigating player.
//
// Count: Num$ (default 1) resolves through the shared Num evaluator -- a
// literal, an SVar name, an inline Count$ body or the bare X of a
// Count$xPaid cast; a present-but-unresolvable value degrades to 0 (the
// card resolves to nothing rather than doing something arbitrary), exactly
// the Num contract.
//
// Players: the investigating player(s) resolve through actingPlayers -- the
// shared "Defined$ else ValidTgts$-targeted else the resolving controller"
// selector every player-acting primitive uses. Defined$ Player.withMostType*
// and the Targeted/Remembered referents resolve inside Defined().
//
// Each Clue is one real TokenCreate event (owner = the investigating
// player), so the mint rides events.Apply and fires trig:TokenCreated /
// trig:TokenCreatedOnce (Mirkwood Bats et al.) exactly like effToken's
// mints, and each investigate also emits one events.Investigate marker
// (trig:Investigated matches it — Erdwal Illuminator; a plain Clue-token
// creation never fires an investigate trigger). No direct state.Game writes.
//
// Optional$ True (2 files: will_the_wise, nick_valentine_private_eye) is a
// real per-player may-investigate election: every affected player is posed
// their own KChoose yes/no (the draw_optional ask shape; the ask goes to the
// AFFECTED seat, never the trigger controller), a yes performs the ordinary
// Clue creation below and a no performs nothing. The answer rides rules'
// "investigate_optional" resume arm into Ctx.InvestigateOpt/Idx, which this
// body consumes and clears at the point of application (fx42 scoping) and
// resets when the walk finishes, so a chained optional Investigate poses its
// own elections. A host that cannot ask keeps the pre-ask mandatory
// investigate (the R-9 degradation the effDraw optional arms take), one
// loud Note per gated resolution.
//
// RememberInvestigatingPlayers$ (1 file, will_the_wise) remembers the
// players who ACTUALLY investigated into the resolution's Remembered (the
// Ctx.Remembered set PlayerCountRemembered$Amount and Defined$ Remembered
// read) AND onto the source object's persistent player-remembered list (the
// same event-backed write RememberChosen$ makes), which is what the
// Opponent.!IsRemembered player filter clause reads -- Will the Wise's
// DBLoseLife "each opponent who doesn't" chains on it. Decliners stay
// unremembered. Absent, the primitive remembers nothing, exactly as before.
func effInvestigate(h Host, c *Ctx, sa *cards.SA) {
	if rest := resumingMint(c, sa); rest != nil {
		// A Clue parked behind a CR 616.1 order ask and the answer has
		// minted it: its marker, the player's remaining Clues, and the
		// players after it resume here with the values the first pass
		// resolved (tokenRest's continuation).
		players, idx, n := rest.Players, int(rest.Count), rest.Amount
		if idx < 0 || idx >= len(players) {
			return
		}
		if investigateFor(h, c, sa, players, idx, n, int32(rest.Next), true, rest.Script) {
			return
		}
		if rest.Script == investigateMandatory {
			for j := idx + 1; j < len(players); j++ {
				if investigateFor(h, c, sa, players, j, n, 0, false, rest.Script) {
					return
				}
			}
			return
		}
		c.InvestigateOpt, c.InvestigateOptIdx = "", int32(idx+1)
		investigateOptionalWalk(h, c, sa, players, n,
			strings.EqualFold(strings.TrimSpace(sa.Params["RememberInvestigatingPlayers"]), "True"))
		return
	}
	g := h.Game()
	n := Num(h, c, sa, "Num", 1)
	if n <= 0 {
		return
	}
	def, ok := g.Tokens[clueTokenKey]
	if !ok || def == nil {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "Investigate: unknown token script " + clueTokenKey})
		return
	}
	remember := strings.EqualFold(strings.TrimSpace(sa.Params["RememberInvestigatingPlayers"]), "True")
	players := actingPlayers(h, c, sa)
	if !strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKOptional)), "True") {
		if remember {
			rememberInvestigatingPlayers(h, c, players)
		}
		for j := range players {
			if investigateFor(h, c, sa, players, j, n, 0, false, investigateMandatory) {
				return
			}
		}
		return
	}
	investigateOptionalWalk(h, c, sa, players, n, remember)
}

// investigateOptionalWalk is an Optional$ Investigate's per-player election
// walk from the cursor c.InvestigateOptIdx.
func investigateOptionalWalk(h Host, c *Ctx, sa *cards.SA, players []state.PlayerID, n int32, remember bool) {
	// The per-player election walk (the draw_upto cursor shape): each
	// affected player poses their own ask, the answered election is consumed
	// at the point of application, and the cursor advances to the next
	// player. A no-host ask keeps the mandatory investigate (R-9), one Note
	// per gated resolution.
	noted := false
	for idx := int(c.InvestigateOptIdx); idx < len(players); idx++ {
		p := players[idx]
		if c.InvestigateOpt == "" {
			d := &decision.Decision{Player: p, Kind: decision.KChoose, Min: 1, Max: 1,
				ResumeKind: "investigate_optional", ResumeSA: sa, ResumeTarget: idx,
				// The walk's Remembered rides the ask (the draw_upto
				// precedent): the re-entered body re-derives the player list
				// from Defined$, and a Remembered-valued selector must see
				// the set the first pass read, or the cursor would index a
				// DIFFERENT list than the one the election was posed for.
				ResumeRemembered: append([]state.Target(nil), c.Remembered...),
				Source:           c.Source,
				Prompt:           "Investigate?"}
			d.Options = []decision.Option{
				{Index: 0, Kind: "yes", Label: "Yes — investigate", Player: p},
				{Index: 1, Kind: "no", Label: "No", Player: p},
			}
			if Ask(h, d) == AskAsked {
				return
			}
			c.InvestigateOpt = "yes"
			if !noted {
				noted = true
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
					Text: "Investigate: no host to ask the election; investigates (R-9 stand-in)"})
			}
		}
		accepted := c.InvestigateOpt == "yes"
		c.InvestigateOpt = "" // fx42 scoping: consumed once; the next player poses its own ask
		c.InvestigateOptIdx = int32(idx + 1)
		if accepted {
			if remember {
				rememberInvestigatingPlayers(h, c, []state.PlayerID{p})
			}
			if investigateFor(h, c, sa, players, idx, n, 0, false, investigateOptional) {
				return
			}
		}
	}
	// The walk finished every player: reset the cursor so a chained optional
	// Investigate in the same resolution poses its own elections (fx42
	// scoping). The marker is already cleared above.
	c.InvestigateOptIdx = 0
}

// investigateFor is the one Clue-mint path: n real TokenCreate events plus
// the matching events.Investigate markers for player p. The marker is what
// trig:Investigated matches; emitting it only here (not on a plain DB$
// Token Clue mint) keeps "create a Clue token" from firing "whenever you
// investigate" triggers.
//
// Each marker follows ITS Clue: a mint that parks the resolution behind a
// CR 616.1 order ask (the Clue's entry-counter order, a CreateToken order)
// hands the rest -- that Clue's marker, the player's later Clues, and the
// players after it -- to the host (TokenRest) and reports true; the caller
// stops. resumed marks the re-entry, whose Clue start the answer minted.
// mode is investigateMandatory or investigateOptional, the walk to resume.
func investigateFor(h Host, c *Ctx, sa *cards.SA, players []state.PlayerID, idx int, n, start int32, resumed bool, mode string) bool {
	p := players[idx]
	for i := start; i < n; i++ {
		if !resumed || i != start {
			wasSuspended := h.Suspended()
			h.EmitTokenCreate(events.Event{Kind: events.TokenCreate, Player: p, Text: clueTokenKey})
			if !wasSuspended && h.Suspended() &&
				suspendMint(h, c, TokenRest{SA: sa, Next: int(i), Players: players, Count: int32(idx),
					Amount: n, Script: mode}) {
				return true
			}
		}
		// One investigate record per created Clue (CR 701.36a: each
		// investigate is one Clue token, so Num$ 2 is two investigates).
		h.Emit(events.Event{Kind: events.Investigate, Obj: c.Source, Player: p})
	}
	return false
}

// The walks a parked Clue's TokenRest resumes (TokenRest.Script).
const (
	investigateMandatory = "investigate"
	investigateOptional  = "investigate_optional"
)

// rememberInvestigatingPlayers records the players who actually
// investigated: into the resolution's Remembered (the Ctx.Remembered set
// PlayerCountRemembered$Amount and Defined$ Remembered read, deduped so a
// repeated Investigate in one resolution cannot inflate the count) and, via
// the event-backed persistent remember write, onto the source object's
// player-remembered list -- the set the IsRemembered player-filter clause
// (Will the Wise's Defined$ Opponent.!IsRemembered) reads.
func rememberInvestigatingPlayers(h Host, c *Ctx, ps []state.PlayerID) {
	for _, p := range ps {
		// Shared with RememberDiscardingPlayers$: each player lands on the
		// resolution's transient set once and on the source's event-backed
		// persistent list once, with the two dedup checks independent.
		rememberPlayerBothHalves(h, c, p)
	}
}
