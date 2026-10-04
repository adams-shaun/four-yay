package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// playAnswerApply applies an answered Play ask (effPlay's "play" decision)
// into ctx, for the resolution kernel's tape record (playAnswerSettle).
func playAnswerApply(e *Engine, ctx *effects.Ctx, sa *cards.SA, chosen []decision.Option) {
	// A Play effect (Conduit of Worlds, Spinerock Knoll) was answered:
	// each chosen option's Obj is a card to play from its current zone,
	// in answer order. An empty answer is a DECLINE of an Optional$
	// Play (effPlay now offers Min 0) -- the answer is consumed with
	// nothing begun. Only the effect's own WithoutManaCost$ grants a
	// free cast: Conduit has no such parameter, while Spinerock Knoll
	// does. A PlayCost$ alternative (task playcost1: Amped Raptor's
	// PayEnergy<ConvertedManaCost>, Anrakyr's PayLife<ConvertedManaCost>,
	// Blue Mage's Cane's fixed {3}, Cruelclaw's Discard<1/Card>) is
	// priced per chosen card -- ConvertedManaCost substitutes the
	// card's own mana value -- inside beginPlay, which hard-declines an
	// unpriceable or unpayable alternative with a loud Note instead of
	// charging full mana. An Amount$ All / N answer may name several
	// cards; each is begun in turn, and the loop stops at the first cast
	// that cannot commit synchronously (an ask inside the cast
	// transaction -- an ETB choice, a target, a mana window -- parks the
	// resolution on that cast's question, and the not-yet-begun cards
	// are dropped with a Note rather than wedging; the corpus Amount$ All
	// shapes are without-mana-cost creature/spell plays, which commit
	// synchronously). ctx.Play/PlayDone are set so the continuing
	// effPlay sees the answer as consumed either way.
	free := strings.EqualFold(sa.ParamStr(cards.PKWithoutManaCost), "True")
	playCost := strings.TrimSpace(sa.ParamStr(cards.PKPlayCost))
	// ReplaceGraveyard$ Exile (task replplay1): the Play SA's own
	// rider — "if that spell would be put into your graveyard this
	// turn, exile it instead" — stamps the played spell's pay-time
	// CastInfo with state.FlagReplaceGraveyard so spellRestZone (and
	// spellFizzleZone for a fizzled/countered play) exiles it. The
	// conditional sibling ReplaceGraveyardValid$ (2 corpus files:
	// Bilbo, Thief in the Night; Scholar of the Lost Trove) restricts
	// the exile to named types and is unread — fail closed, keep the
	// graveyard resting place for those.
	replaceGraveyard := strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKReplaceGraveyard)), "Exile") &&
		strings.TrimSpace(sa.ParamStr(cards.PKReplaceGraveyardValid)) == ""
	// CopyCard$ True casts an event-minted copy of the selected card,
	// leaving the original in its source zone. False or absent keeps
	// the ordinary Play path.
	copyCard := strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKCopyCard)), "True")
	// ImprintPlayed$ True (task imprintplayed: Rashmi and Ragavan,
	// Kefka, Beseech the Mirror, Soundwave, Smuggler's Buggy — 5 corpus
	// files): every card the Play actually BEGINS to play is recorded
	// as imprinted on the resolution's source (events.Imprint, the
	// same association Chrome Mox's Imprint$ writes), so the chained
	// ConditionDefined$ Imprinted gate (DBEffect's "did you cast it
	// this way?" arm) reads a real answer. "Actually begins" is read
	// from the card's zone: a begun cast pushes the card onto the
	// stack (CR 601.2a) or moves it onward, while a declined Play — an
	// unpayable alternative, a stale answer, a reversed cast — leaves
	// it in its zone, and an aborted cast reverses it back to exactly
	// the zone it started in. The emission sits before the suspension
	// break so a cast suspended mid-transaction (a target ask inside
	// the free cast) is still recorded as played.
	imprintPlayed := strings.EqualFold(sa.ParamStr(cards.PKImprintPlayed), "True")
	// The play's own CONTROLLER rides the answer's options (effPlay set
	// each option's Player to the Controller$-resolved seat): a
	// Controller$ Play (Word of Command's TargetedPlayer, Wild
	// Evocation's TriggeredPlayer, Spell Queller's RememberedOwner) is
	// begun BY that seat, not by the resolving ability's controller.
	// An option carrying no seat (a hand-built context, or every
	// historical Controller$-absent game whose options named the
	// resolving controller anyway) keeps ctx.Controller, the pre-
	// Controller$ read.
	player := ctx.Controller
	if len(chosen) > 0 && int(chosen[0].Player) < len(e.G.Players) {
		player = chosen[0].Player
	}
	// ShowCards$ (Sunbird's Invocation -- the corpus's one carrier):
	// the play's public reveal rider. The population it names is a
	// card filter over the walk's remembered set ("Card.IsRemembered"
	// = the window the PeekAndReveal revealed and remembered); the
	// reveal is ONE public Note (the ids payload view.Describe
	// renders) emitted BEFORE the first cast begins, while the cards
	// still sit in their hidden zones. A decline plays nothing and
	// reveals nothing. The R-9 no-host path (effPlay's deterministic
	// first candidate) never reaches this arm and stays untouched --
	// the same boundary ForgetPlayed$ keeps.
	var toPlay []state.ObjID
	for _, ch := range chosen {
		if ch.Obj != 0 {
			toPlay = append(toPlay, ch.Obj)
		}
	}
	if show := strings.TrimSpace(sa.ParamStr(cards.PKShowCards)); show != "" && len(toPlay) > 0 {
		sc := ctx.SpecContext(player)
		var ids []state.ObjID
		seenShow := map[state.ObjID]bool{}
		for _, t := range ctx.Remembered {
			if t.IsPlayer || t.Obj == 0 || seenShow[t.Obj] {
				continue
			}
			if o := e.G.Obj(t.Obj); o != nil && e.matchesSpec(show, t.Obj, sc) {
				seenShow[t.Obj] = true
				ids = append(ids, t.Obj)
			}
		}
		if len(ids) > 0 {
			e.emit(events.Event{Kind: events.Note, Player: player, IDs: ids})
		}
	}
	ctx.PlayDone = true
	if len(toPlay) > 0 {
		ctx.Play = toPlay[0]
	}
	q := &queuedPlays{player: player, ids: toPlay, free: free, playCost: playCost,
		replaceGraveyard: replaceGraveyard, copyCard: copyCard}
	if imprintPlayed {
		q.imprintOn = ctx.Source
	}
	e.runPlays(q)
}

// playAnswerSettle is the tape-served Play answer's record: the plays begin
// in line. A begun cast that cannot commit synchronously (an ask inside the
// cast transaction) reaches the engine's ask choke point as a legacy ask
// after a tape ask and aborts the run; a cast still in flight or a parked
// play queue hands the resolution back to legacy too.
func playAnswerSettle(e *Engine, d *decision.Decision, chosen []decision.Option) {
	ctx := e.resolutionCtx
	if ctx == nil || d.ResumeSA == nil {
		panic("rules: a tape-served Play answer outside a resolution chain")
	}
	playAnswerApply(e, ctx, d.ResumeSA, chosen)
	if e.queuedPlays != nil || e.cast != nil || e.pending != nil || e.Suspended() {
		tapeUnservable(e, "play")
	}
}
