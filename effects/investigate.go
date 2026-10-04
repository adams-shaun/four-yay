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
// Clue creation below and a no performs nothing. A host that cannot ask keeps the pre-ask mandatory
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
	remember := strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKRememberInvestigatingPlayers)), "True")
	players := actingPlayers(h, c, sa)
	if !strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKOptional)), "True") {
		if remember {
			rememberInvestigatingPlayers(h, c, players)
		}
		for _, p := range players {
			investigateFor(h, c, p, n)
		}
		return
	}
	investigateOptionalWalk(h, c, sa, players, n, remember)
}

// investigateOptionalWalk is an Optional$ Investigate's per-player election
// walk.
func investigateOptionalWalk(h Host, c *Ctx, sa *cards.SA, players []state.PlayerID, n int32, remember bool) {
	// Each affected player poses their own ask. A no-host ask keeps the
	// mandatory investigate (R-9), one Note per gated resolution.
	noted := false
	for idx, p := range players {
		var accepted bool
		d := &decision.Decision{Player: p, Kind: decision.KChoose, Min: 1, Max: 1,
			ResumeKind: "investigate_optional", ResumeSA: sa, ResumeTarget: idx,
			Source: c.Source,
			Prompt: "Investigate?"}
		d.Options = []decision.Option{
			{Index: 0, Kind: "yes", Label: "Yes — investigate", Player: p},
			{Index: 1, Kind: "no", Label: "No", Player: p},
		}
		if ans, ok := AskTape(h, d); ok {
			// The answer in hand: a malformed or empty answer keeps the
			// decline, as the arm reads it.
			accepted = len(ans) > 0 && ans[0].Kind == "yes"
		} else {
			accepted = true
			if !noted {
				noted = true
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
					Text: "Investigate: no host to ask the election; investigates (R-9 stand-in)"})
			}
		}

		if accepted {
			if remember {
				rememberInvestigatingPlayers(h, c, []state.PlayerID{p})
			}
			investigateFor(h, c, p, n)
		}
	}
}

// investigateFor is the one Clue-mint path: n real TokenCreate events plus
// the matching events.Investigate markers for player p. The marker is what
// trig:Investigated matches; emitting it only here (not on a plain DB$
// Token Clue mint) keeps "create a Clue token" from firing "whenever you
// investigate" triggers.
func investigateFor(h Host, c *Ctx, p state.PlayerID, n int32) {
	for i := int32(0); i < n; i++ {
		h.EmitTokenCreate(events.Event{Kind: events.TokenCreate, Player: p, Text: clueTokenKey})
		// One investigate record per created Clue (CR 701.36a: each
		// investigate is one Clue token, so Num$ 2 is two investigates).
		h.Emit(events.Event{Kind: events.Investigate, Obj: c.Source, Player: p})
	}
}

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
