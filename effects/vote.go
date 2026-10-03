package effects

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The canonical vote-finished carrier (trig:Vote). api:Vote's two shapes
// (the fixed-list effVote and the card-ballot effCardVote) already recorded
// one Note per voting player; this file adds the ONE canonical "players
// finished voting" Note a Mode$ Vote trigger fires on, the same shape
// api:FlipCoin's FlipCoinNote/FlipNoteResult pair serves for
// trig:FlippedCoin (rules/trigmatch/actions.go's flippedCoinMatches).
//
// Encoding (no events.Event schema change). A public vote carrier carries raw
// ballots; a secret carrier contains only the completion marker, while the
// synchronous trigger matcher receives private ballots. The two List$ sets
// are relative to the TRIGGER SOURCE'S CONTROLLER, unknowable when the vote resolves
// (the vote's caster and the carrier permanent's controller are frequently
// different seats in multiplayer). So Player is the vote's caster (for the
// transcript), Obj the resolving vote spell, and Pairs one entry per voter:
// [PlayerRef(voter), ObjID(pick+1)], pick -1 ("voted for nothing") encoded as
// ObjID(0) so a self-contained pair never collides with a valid pick. A
// non-zero Amount marks that a real ballot existed (Choices$/VoteCard$
// admitted at least one option); an empty-ballot vote carries Amount 0 and
// binds neither set. events.Apply folds Note as an inert marker, so the pairs
// are never re-interpreted; the log keeps them, and a replay re-derives the
// trigger's referents from the same event bytes.
const VoteNotePrefix = "vote finished"

// VoteBallot is one voter's answered ballot: the voting player and the option
// index they chose (-1 when they voted for nothing -- an out-of-range answer
// or an empty ballot).
type VoteBallot struct {
	Player state.PlayerID
	Pick   int
}

// VoteFinishedNote builds the canonical vote-finished event. caster is the
// vote's caster (transcript/log only); ballots is every voter's raw answer in
// voter order; ballotExisted reports whether the vote offered any option at
// all (false for an empty ballot, which binds neither referent set).
func VoteFinishedNote(caster state.PlayerID, source state.ObjID, ballots []VoteBallot, ballotExisted bool) events.Event {
	ev := events.Event{Kind: events.Note, Player: caster, Obj: source, Text: VoteNotePrefix}
	if ballotExisted {
		ev.Amount = 1
	}
	for _, b := range ballots {
		ev.Pairs = append(ev.Pairs, [2]state.ObjID{state.PlayerRef(b.Player), state.ObjID(b.Pick + 1)})
	}
	return ev
}

// VoteFinishedResult decodes the canonical vote-finished Note: the caster
// (ev.Player), the raw ballots in voter order, whether a real ballot existed,
// and whether ev is one. Any other Note -- in particular the per-voter
// "votes for ..." notes, which carry no Pairs -- is not one.
func VoteFinishedResult(ev events.Event) (caster state.PlayerID, ballots []VoteBallot, ballotExisted bool, ok bool) {
	if ev.Kind != events.Note || ev.Text != VoteNotePrefix {
		return 0, nil, false, false
	}
	for _, pr := range ev.Pairs {
		p, isPlayer := pr[0].PlayerRef()
		if !isPlayer {
			continue
		}
		ballots = append(ballots, VoteBallot{Player: p, Pick: int(pr[1]) - 1})
	}
	return ev.Player, ballots, ev.Amount != 0, true
}

// VoteSplit splits the voting players who are not the ANCHOR into the two
// List$ sets, in voter order: the same set holds those who voted for a choice
// the anchor also voted for, the diff set everyone else. The anchor is the
// controller of the permanent whose Mode$ Vote trigger is being bound, NOT
// the vote's caster -- "a choice you voted for" means the carrier
// controller's own ballot, so the rules-side capture re-splits the raw
// ballots against e.controllerOf(source).
//
// ballotExisted is the empty-ballot case: the vote offered no option at all
// (a fixed-list Vote with no Choices$, or a card ballot whose VoteCard$
// matched no permanent). Then NO voter cast a real vote, so neither set is
// bound -- no one voted for a choice the anchor voted for, and no one voted
// for a different one, because there was no choice. Without this, every
// pick is -1 and the anchorPick != -1 guard below would route every voting
// opponent into the DIFF set, making Grudge Keeper drain life from every
// opponent and Erestor scry the whole table after a vote in which nobody
// voted for anything.
//
// Otherwise the anchor's own pick anchors "a choice you voted for"; an anchor
// who is not among the voters (or voted for nothing while others voted)
// anchors nothing, so the same set is empty and every voting opponent lands
// in the diff set -- "a choice you didn't vote for" covers every choice when
// you made none.
func VoteSplit(anchor state.PlayerID, ballots []VoteBallot, ballotExisted bool) (same, diff []state.PlayerID) {
	if !ballotExisted || len(ballots) == 0 {
		return nil, nil
	}
	anchorPick := -1
	for _, b := range ballots {
		if b.Player == anchor {
			anchorPick = b.Pick
			break
		}
	}
	for _, b := range ballots {
		if b.Player == anchor {
			continue
		}
		if anchorPick != -1 && b.Pick == anchorPick {
			same = append(same, b.Player)
		} else {
			diff = append(diff, b.Player)
		}
	}
	return same, diff
}

// battlefieldHasVoteTrigger is the carrier's emit gate: the canonical
// vote-finished Note is proposed only when some battlefield permanent's face
// carries a Mode$ Vote trigger. A vote nobody can observe finished is
// indistinguishable from a canonical Note nobody matches -- and emitting it
// unconditionally would append a new event to every existing recorded game
// that resolves a vote (the golden acceptance games resolve Council's
// Judgment votes), moving every chain head. The gate is deterministic board
// state, so replay takes the same branch and no recorded game changes.
//
// The scan is BATTLEFIELD-only, matching where the engine's ordinary trigger
// scan looks for a permanent's face: a Mode$ Vote trigger carried by a card
// in another zone would not be found, so its carrier would stay silent. No
// corpus Vote carrier declares a non-battlefield TriggerZones$ today; if one
// ever does, this scan has to widen with it.
func battlefieldHasVoteTrigger(g *state.Game) bool {
	if g == nil {
		return false
	}
	for _, p := range g.AliveFrom(0) {
		for _, id := range g.Zone(state.ZBattlefield, p) {
			o := g.Obj(id)
			if o == nil || o.Face() == nil {
				continue
			}
			for _, tg := range o.Face().Triggers {
				if strings.EqualFold(tg.Mode, "Vote") {
					return true
				}
			}
		}
	}
	return false
}

// privateVoteEmitter supplies the trigger matcher with secret ballots during
// the synchronous emission, without storing them in the public event stream.
// The answer is replay-derived from the recorded private voter intents.
type privateVoteEmitter interface {
	EmitSecretVoteFinished(events.Event, []VoteBallot)
}

// emitVoteFinished proposes the canonical vote-finished Note for one ballot.
func emitVoteFinished(h Host, c *Ctx, ballots []VoteBallot, ballotExisted, secretly bool) {
	if !battlefieldHasVoteTrigger(h.Game()) {
		return
	}
	if secretly {
		// No raw ballot may enter events.Event: log readers see the same
		// completion marker, but only the synchronous trigger scan sees picks.
		public := VoteFinishedNote(c.Controller, c.Source, nil, ballotExisted)
		if private, ok := h.(privateVoteEmitter); ok {
			private.EmitSecretVoteFinished(public, ballots)
		} else {
			h.Emit(public)
		}
		return
	}
	h.Emit(VoteFinishedNote(c.Controller, c.Source, ballots, ballotExisted))
}

// effPlayerVote is api:Vote's PLAYER-ballot shape (task votepb1): the third
// ballot kind beside the fixed Choices$ list and the VoteCard$ permanent
// ballot, spelled `VotePlayer$ <selector>` (Mob Verdict's `VotePlayer$ Other`,
// Círdan the Shipwright's `VotePlayer$ Player`). It is Forge's VoteEffect
// branch
//
//	else if (sa.hasParam("VotePlayer")) { String param = other ? "Player" : ...; }
//
// made real: every voting player is asked, mid-resolution, to vote for one
// entry of the selector's ballot, and the per-entry tally is published on the
// resolution Ctx (Ctx.VoteCounts) for the chained AmountFromVotes$ reader.
//
// The ballot universe is built ONCE per resolution, in the deterministic
// APNAP seat order from the resolving controller (g.AliveFrom(c.Controller)):
// every living player the selector admits, or -- for the `Other` spelling --
// every living player, whose per-voter option list then drops the voter
// themselves (Forge's `voteOpts.remove(realVoter)`). A voter with an empty
// option list casts no vote at all (Forge's `if (voteOpts.isEmpty()) continue;`),
// recorded as a zero Target so the picks stay aligned with the voter list.
//
// SECRECY: the pending decision belongs only to its voter, and no ballot is
// revealed until every voter has answered; then each is revealed as a Note
// (secret council: "then those votes are revealed"). A Secretly$ ballot's
// completion marker still carries no picks. The canonical
// vote-finished carrier (emitVoteFinished) is emitted last.
//
// The tally is published behind StoreVoteNum$ True, the parameter Forge
// requires before it stores VoteNum<SVar>s; an AmountFromVotes$ RepeatEach on
// a vote without it therefore reads 0, matching Forge's unset read.
func effPlayerVote(h Host, c *Ctx, sa *cards.SA, vp *VoteParams) {
	g := h.Game()
	spec := vp.Player
	other := vp.PlayerOther
	var universe []state.PlayerID
	for _, p := range g.AliveFrom(c.Controller) {
		if other || MatchesPlayerSpec(g, spec, p, c.Controller) {
			universe = append(universe, p)
		}
	}
	voters := definedPlayers(h, c, sa)
	picks := append([]state.Target(nil), ([]state.Target)(nil)...)
	i := int(0)

	for ; i < len(voters); i++ {
		voter := voters[i]
		opts := playerBallotOptions(universe, voter, other)
		if len(opts) == 0 {
			picks = append(picks, state.Target{})
			continue
		}
		d := &decision.Decision{Player: voter, Kind: decision.KChoose, Source: c.Source,
			Min: 1, Max: 1, ResumeKind: "vote", ResumeSA: sa, ResumeTarget: i,
			Prompt: "Vote for a player"}
		for j, p := range opts {
			d.Options = append(d.Options, decision.Option{Index: j, Kind: "player", Player: p,
				Label: votePlayerLabel(g, p)})
		}
		if ans, ok := AskTape(h, d); ok {
			// The resolution kernel's answer in hand: the pick the "vote"
			// resume arm's VoteAnswer carries.
			pick := state.Target{}
			if len(ans) > 0 {
				if ans[0].Kind == "player" {
					pick = state.Target{Player: ans[0].Player, IsPlayer: true}
				} else if ans[0].Obj != 0 {
					pick = state.Target{Obj: ans[0].Obj}
				}
			}
			picks = append(picks, pick)
			continue
		}

		// No host to ask (the R-9 fuzz/test contract): the deterministic first
		// admissible entry -- the same pick the pre-ask stand-in made -- under
		// the R-9 degradation Note every other asking site records.
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "player vote resolved as the first ballot entry (no engine host to ask)"})
		picks = append(picks, state.Target{Player: opts[0], IsPlayer: true})
	}

	// Every ballot publishes the entry's deck identity once everyone has
	// voted -- secret ones too: secret council votes are cast privately,
	// "then those votes are revealed".
	ballots := make([]VoteBallot, len(voters))
	for k, t := range voters {
		voter := t
		pick := ballotPickIndex(universe, picks, k)
		label := "nothing"
		if pick >= 0 {
			label = votePlayerLabel(g, universe[pick])
		}
		h.Emit(events.Event{Kind: events.Note, Player: voter, Text: "votes for " + label})
		ballots[k] = VoteBallot{Player: voter, Pick: pick}
	}
	if vp.StoreVoteNum {
		publishVoteCounts(c, voteCountsForPlayers(universe, picks))
	}
	emitVoteFinished(h, c, ballots, len(universe) > 0, vp.Secretly)
}

// playerBallotOptions is the ballot entry list one voter may pick from: the
// universe, minus the voter themselves on the `Other` spelling.
func playerBallotOptions(universe []state.PlayerID, voter state.PlayerID, other bool) []state.PlayerID {
	out := make([]state.PlayerID, 0, len(universe))
	for _, p := range universe {
		if other && p == voter {
			continue
		}
		out = append(out, p)
	}
	return out
}

// ballotPickIndex maps the k-th voter's accumulated pick onto its index in
// the universe; -1 is "voted for nothing" (no answer recorded, an
// out-of-universe answer, or a voter with no admissible entry).
func ballotPickIndex(universe []state.PlayerID, picks []state.Target, k int) int {
	if k >= len(picks) || !picks[k].IsPlayer {
		return -1
	}
	for i, p := range universe {
		if p == picks[k].Player {
			return i
		}
	}
	return -1
}

// voteCountsForPlayers tallies the player ballot: one entry per universe seat
// (including a seat no one voted for, so an AmountFromVotes$ loop can still
// bind Votes 0 for it -- Forge stores VoteNum<player> for every ballot entry).
func voteCountsForPlayers(universe []state.PlayerID, picks []state.Target) []VoteCount {
	out := make([]VoteCount, 0, len(universe))
	for _, p := range universe {
		n := 0
		for _, pick := range picks {
			if pick.IsPlayer && pick.Player == p {
				n++
			}
		}
		out = append(out, VoteCount{Subject: state.Target{Player: p, IsPlayer: true}, Count: n})
	}
	return out
}

// voteCountsForObjects is the card-ballot twin of voteCountsForPlayers: one
// entry per ballot permanent (Forge stores VoteNum<card> for every VoteCard$
// entry), so RememberVotedObjects$ and an AmountFromVotes$ loop share one
// tally.
func voteCountsForObjects(options []state.ObjID, counts map[state.ObjID]int) []VoteCount {
	out := make([]VoteCount, 0, len(options))
	for _, id := range options {
		out = append(out, VoteCount{Subject: state.Target{Obj: id}, Count: counts[id]})
	}
	return out
}

// publishVoteCounts installs a ballot's tally on the resolution Ctx for the
// chained AmountFromVotes$ reader. A nil/empty tally (a vote with no ballot)
// clears the field rather than leaving a previous vote's stale one in place.
func publishVoteCounts(c *Ctx, counts []VoteCount) {
	if len(counts) == 0 {
		c.VoteCounts = nil
		return
	}
	c.VoteCounts = counts
}

// voteCountFor resolves an AmountFromVotes$ loop subject's tally. ok=false
// when the most recent vote published no entry for it -- the caller then binds
// Votes 0 (Forge's unset VoteNum<subject> read), never a stale SVar.
func voteCountFor(c *Ctx, subject state.Target) (int, bool) {
	if c == nil {
		return 0, false
	}
	return VoteCountForTarget(c.VoteCounts, subject)
}

// VoteCountForTarget resolves a subject's tally out of a published
// Ctx.VoteCounts table. It is the ONE home for the subject match, shared by
// voteCountFor's per-iteration binding and rules' rebuilt-Ctx restoration of a
// suspended AmountFromVotes$ loop (the tally is a prior chain link, so a
// resume restores the table and re-derives the scalar from it). ok=false when
// the table holds no entry for the subject.
func VoteCountForTarget(counts []VoteCount, subject state.Target) (int, bool) {
	for _, vc := range counts {
		if vc.Subject == subject {
			return vc.Count, true
		}
	}
	return 0, false
}

// votePlayerLabel is a ballot entry's event-text identity: the seat's deck
// Name, else "seat N" -- the same F3-safe identity the toss Note uses. The
// display PlayerName never reaches the chain.
func votePlayerLabel(g *state.Game, p state.PlayerID) string {
	if g != nil && int(p) < len(g.Players) {
		if name := g.Players[p].Name; name != "" {
			return name
		}
	}
	return "seat " + strconv.Itoa(int(p))
}
