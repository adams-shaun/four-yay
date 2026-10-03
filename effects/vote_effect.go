package effects

import (
	"strconv"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// api:Vote's resolution (effVote, effCardVote), moved out of charm.go so that file holds only the modal family
// whose parameters compileCharm reads (charm_params.go).

// effVote records one Note per voting player. Two shapes:
//
//   - the fixed-list shape ("Will of the Planeswalkers", Expropriate):
//     Choices$ names an SVar per ballot option, each player votes for the
//     first (the deterministic stand-in), Notes record it, and the WINNING
//     option's SVar runs. A tie runs VoteTiedAbility$ when the SA carries
//     one (the path cycle's DBChaos), else the first tied option's SVar.
//     Before this the fixed-list shape resolved nothing at all, so a Path
//     of the Ghosthunter vote recorded its Notes and then did nothing --
//     the "chosen outcome" the brief expected to hit Planeswalk/
//     ChaosEnsues never ran. The tie branch takes its tally from Ctx.Votes
//     when a caller has answered one (the seam a real per-player ask fills,
//     and what lets the tie be pinned against a real compiled SA); absent,
//     the deterministic stand-in applies.
//   - the card-ballot shape (Council's Judgment): VoteCard$ is a permanent
//     filter, so the ballot is the battlefield permanents matching it
//     (matched from the spell's controller: "a nonland permanent YOU don't
//     control"), each Defined$ player votes, and every permanent with the
//     most votes or tied for most lands in the resolution's Remembered set
//     for VoteSubAbility$ (DBExile's ChangeZone Defined$ Remembered).
//
// Fixed and card ballots use the real per-voter ask path below; a host that
// cannot answer retains the R-9 first-option fallback. Both VoteCard$ and
// VoteSubAbility$ are genuinely read on the ballot path.
func effVote(h Host, c *Ctx, sa *cards.SA) {
	vp := VoteOf(sa)

	// Once per call: an answered ballot re-entry already noted on its
	// first pass.
	noteUnreadParams(h, c, "Vote", vp.Unread)

	if vp.Card != "" {
		effCardVote(h, c, sa, vp, vp.Card)
		return
	}
	// The PLAYER ballot (task votepb1): VotePlayer$ with no Choices$ list
	// names the ballot entries as players (Mob Verdict's `VotePlayer$ Other`).
	// Choices$ keeps its precedence -- Forge's VoteEffect reads Choices first,
	// then VoteCard$, then VotePlayer$ -- so this fires only when the vote
	// carries no fixed option list.
	if vp.Player != "" && len(vp.Choices) == 0 {
		effPlayerVote(h, c, sa, vp)
		return
	}
	choices := vp.Choices
	voters := definedPlayers(h, c, sa)

	picks, complete := askFixedVote(h, c, sa, vp, choices, voters)
	if !complete {
		return
	}
	for i, t := range voters {
		label := ""
		if i < len(picks) && picks[i].Obj > 0 && int(picks[i].Obj-1) < len(choices) {
			label = choices[picks[i].Obj-1]
		}
		// Secret ballots are revealed here too: every vote is already in
		// (secret council: "then those votes are revealed").
		h.Emit(events.Event{Kind: events.Note, Player: t, Text: "votes for " + label})
	}
	counts := make([]int, len(choices))
	for _, p := range picks {
		if p.Obj > 0 && int(p.Obj-1) < len(choices) {
			counts[p.Obj-1]++
		}
	}
	if len(choices) > 0 && len(voters) > 0 {
		resolveVoteOutcomes(h, c, vp, choices, counts)
	}
	ballots := make([]VoteBallot, len(voters))
	for i, t := range voters {
		ballots[i] = VoteBallot{Player: t, Pick: int(picks[i].Obj) - 1}
	}
	emitVoteFinished(h, c, ballots, len(choices) > 0, vp.Secretly)
	return

}

// resolveVoteOutcomes executes the winning option normally. StoreVoteNum$ is
// the multi-outcome form: publish each option's tally as VoteNum in a private
// copy of the source SVar table, then resolve every option body so its numeric
// effects consume that option's count (including zero).
func resolveVoteOutcomes(h Host, c *Ctx, vp *VoteParams, choices []string, counts []int) {
	if vp.StoreVoteNum {
		for i, name := range choices {
			count := 0
			if i < len(counts) {
				count = counts[i]
			}
			svars := make(map[string]string, len(c.SVars)+1)
			for key, body := range c.SVars {
				svars[key] = body
			}
			// Forge reuses the SVar name VoteNum for each outcome body, binding
			// that option's tally while resolving it.
			svars["VoteNum"] = "Number$" + strconv.Itoa(count)
			cc := *c
			cc.SVars = svars
			if sub := cards.ResolveSVar(cc.SVars, name); sub != nil {
				Resolve(h, &cc, sub)
			}
		}
		return
	}
	best, tied := voteWinner(counts)
	name := choices[best]
	if tied {
		if alt := vp.TiedAbility; alt != "" {
			name = alt
		}
	}
	if sub := cards.ResolveSVar(c.SVars, name); sub != nil {
		Resolve(h, c, sub)
	}
}

// askFixedVote poses one private KChoose per voter. The answer is encoded as
// ObjID(index+1), avoiding a second answer channel while keeping ResumeChoices
// decision-scoped. A host that cannot answer takes option zero (R-9).
func askFixedVote(h Host, c *Ctx, sa *cards.SA, vp *VoteParams, choices []string, voters []state.PlayerID) ([]state.Target, bool) {
	picks := append([]state.Target(nil), ([]state.Target)(nil)...)
	i := int(0)

	for ; i < len(voters); i++ {
		voter := voters[i]
		min := 1
		if vp.UpTo {
			min = 0
		}
		prompt := vp.Message
		if prompt == "" {
			prompt = "Vote for an option"
		}
		d := &decision.Decision{Player: voter, Kind: decision.KChoose, Source: c.Source,
			Min: min, Max: 1, ResumeKind: "vote", ResumeSA: sa, ResumeTarget: i,
			Prompt: prompt}
		for j, name := range choices {
			d.Options = append(d.Options, decision.Option{Index: j, Kind: "vote", Label: name, Obj: state.ObjID(j + 1)})
		}
		if len(d.Options) == 0 {
			picks = append(picks, state.Target{})
			continue
		}
		if ans, ok := AskTape(h, d); ok {
			// The resolution kernel's answer in hand (the "vote" resume
			// arm's VoteAnswer): option j is encoded as ObjID(j+1).
			pick := state.Target{}
			if len(ans) > 0 {
				pick = state.Target{Obj: ans[0].Obj}
			}
			picks = append(picks, pick)
			continue
		}

		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "vote resolved as the first ballot entry (no engine host to ask)"})
		picks = append(picks, state.Target{Obj: 1})
	}

	return picks, true
}

// voteWinner returns the index of the highest count and whether that count is
// shared by more than one option. It is a separate function (rather than
// inline in effVote) so the tie branch is testable on its own: the current
// deterministic stand-in gives every vote to option 0, so a real tie cannot
// arise from a live resolution yet, and an untested branch would be dead code
// waiting to rot. The first highest index wins the tie, matching the
// oracle's "if X gets more votes" over "or the vote is tied" ordering.
func voteWinner(counts []int) (int, bool) {
	if len(counts) == 0 {
		return 0, false
	}
	best := 0
	for i, n := range counts {
		if n > counts[best] {
			best = i
		}
	}
	tied := 0
	for _, n := range counts {
		if n == counts[best] {
			tied++
		}
	}
	return best, tied > 1
}

// effCardVote is effVote's card-ballot half: the battlefield permanents
// VoteCard$ admits are the options, each voting player answers a private ask,
// and the most-voted -- every
// member of the tie -- is remembered for VoteSubAbility$, which runs once
// at the end (Council's Judgment's "exile each permanent with the most
// votes or tied for most votes").
func askCardVote(h Host, c *Ctx, sa *cards.SA, vp *VoteParams, options []state.ObjID, voters []state.PlayerID) ([]state.ObjID, bool) {
	picks := append([]state.Target(nil), ([]state.Target)(nil)...)
	i := int(0)

	for ; i < len(voters); i++ {
		voter := voters[i]
		min := 1
		if vp.UpTo {
			min = 0
		}
		prompt := vp.Message
		if prompt == "" {
			prompt = "Vote for a permanent"
		}
		d := &decision.Decision{Player: voter, Kind: decision.KChoose, Source: c.Source, Min: min, Max: 1,
			ResumeKind: "vote", ResumeSA: sa, ResumeTarget: i, Prompt: prompt}
		for j, id := range options {
			label := "permanent"
			var controller state.PlayerID
			if o := h.Game().Obj(id); o != nil && o.Face() != nil {
				label = o.Face().Name
				// The subject's controller is public information (CR 400.2) and
				// the one fact the voter's policy needs to prefer a foreign
				// permanent over its own: Council's Judgment's ballot excludes
				// only the CASTER's permanents, so a 3+ seat ballot offers a
				// voter both its own and an opponent's permanents. Option.Player
				// already carries exactly this subject-controller convention for
				// player targets, so no new wire field is needed.
				controller = o.Controller
			}
			d.Options = append(d.Options, decision.Option{Index: j, Kind: "vote_card", Label: label, Obj: id, Player: controller})
		}
		if len(d.Options) == 0 {
			picks = append(picks, state.Target{})
			continue
		}
		if ans, ok := AskTape(h, d); ok {
			// The resolution kernel's answer in hand (the "vote" resume
			// arm's VoteAnswer).
			pick := state.Target{}
			if len(ans) > 0 && ans[0].Obj != 0 {
				pick = state.Target{Obj: ans[0].Obj}
			}
			picks = append(picks, pick)
			continue
		}

		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "card vote resolved as the first ballot entry (no engine host to ask)"})
		picks = append(picks, state.Target{Obj: options[0]})
	}

	out := make([]state.ObjID, len(picks))
	for j, p := range picks {
		out[j] = p.Obj
	}
	return out, true
}

func effCardVote(h Host, c *Ctx, sa *cards.SA, vp *VoteParams, ballot string) {
	g := h.Game()
	var options []state.ObjID
	for i := range g.Players {
		for _, id := range g.Zone(state.ZBattlefield, state.PlayerID(i)) {
			if o := g.Obj(id); o != nil && c.MatchSpec(g, ballot, id, c.Controller) {
				options = append(options, id)
			}
		}
	}
	counts := map[state.ObjID]int{}
	max := 0
	voters := definedPlayers(h, c, sa)
	var picks []int

	answered, complete := askCardVote(h, c, sa, vp, options, voters)
	if !complete {
		return
	}
	picks = make([]int, len(answered))
	for i, id := range answered {
		picks[i] = -1
		if id != 0 {
			for j, option := range options {
				if option == id {
					picks[i] = j
					break
				}
			}
		}
	}

	for i, t := range voters {
		label := "nothing"
		if i < len(picks) && picks[i] >= 0 && picks[i] < len(options) {
			id := options[picks[i]]
			if o := g.Obj(id); o != nil && o.Face() != nil {
				label = o.Face().Name
			}
			counts[id]++
			if counts[id] > max {
				max = counts[id]
			}
		}
		// Secret ballots are revealed here too: every vote is already in
		// (secret council: "then those votes are revealed").
		h.Emit(events.Event{Kind: events.Note, Player: t, Text: "votes for " + label})
	}
	// The card ballot's per-subject tally, for the chained AmountFromVotes$
	// reader (task votepb1): one entry per ballot permanent, published behind
	// StoreVoteNum$ True -- the parameter Forge requires before it stores its
	// VoteNum<card> SVars. Forge's StoreVoteNum branch (a card ballot has no
	// Choices$) is authoritative on what the resolution REMEMBERS as well:
	// when the vote stores its per-subject tallies, the most-votes remember
	// path does not run at all, and the only remember is
	// RememberVotedObjects$'s `host.addRemembered(votes.keySet())` -- exactly
	// the objects that RECEIVED a vote, each once. Without StoreVoteNum$ the
	// most-votes append stands (Council's Judgment's "exile each permanent
	// with the most votes or tied for most votes", feeding VoteSubAbility$);
	// there a bare RememberVotedObjects$ beside it dedupes against the
	// most-votes set instead of duplicating it (no corpus carrier combines
	// the two without StoreVoteNum$, so the dedupe is the structural guard,
	// not a behaviour change any carrier can see).
	storeVoteNum := vp.StoreVoteNum
	rememberVoted := vp.RememberVoted
	if storeVoteNum {
		publishVoteCounts(c, voteCountsForObjects(options, counts))
	} else if max > 0 {
		for _, id := range options {
			if counts[id] == max {
				c.Remembered = append(c.Remembered, state.Target{Obj: id})
			}
		}
	}
	if rememberVoted {
		// Exactly the objects that received a vote, each once: on the
		// non-StoreVoteNum path the most-votes append above may already hold a
		// voted object, so the voted set never duplicates it.
		mostVoted := map[state.ObjID]bool{}
		if !storeVoteNum && max > 0 {
			for _, id := range options {
				if counts[id] == max {
					mostVoted[id] = true
				}
			}
		}
		for _, id := range options {
			if counts[id] > 0 && !mostVoted[id] {
				c.Remembered = append(c.Remembered, state.Target{Obj: id})
			}
		}
	}
	// VoteSubAbility$ resolves AFTER the tally publish and the remember, so a
	// chained body sees Votes bound and the remembered set complete (fx42's
	// consumers read before their own re-entries).
	if sub := vp.SubAbility; sub != "" {
		if resolved := cards.ResolveSVar(c.SVars, sub); resolved != nil {
			Resolve(h, c, resolved)
		}
	}
	// The canonical vote-finished carrier (trig:Vote, effects/vote.go),
	// emitted after VoteSubAbility$ ran -- the same after-the-vote point the
	// fixed-list shape emits at. Like the fixed-list shape it carries the RAW
	// ballots and the rules side re-splits against the carrier controller.
	// The deterministic stand-in gives every voter the ballot's FIRST option,
	// so a controller who voted sees every other voter in the same set. A
	// vote with no ballot option at all (an empty battlefield) had nobody
	// vote for anything, so ballotExisted=false binds neither set -- the
	// trigger still fires and its same/diff bodies act on nobody, the same
	// always-fire reading the fixed-list shape takes.
	ballots := make([]VoteBallot, len(voters))
	for i, t := range voters {
		ballots[i] = VoteBallot{Player: t, Pick: picks[i]}
	}
	emitVoteFinished(h, c, ballots, len(options) > 0, vp.Secretly)
}

// effBecomeMonarch records the game-level designation as an event so a
// conditional trigger observes it identically in the live game and on replay.
//
// The event is a TRANSITION (CR 720.2: a player "becomes" the monarch only
// when the designation moves to them), so a resolution that names the
// reigning monarch as its target is a no-op: the designation does not move
// and no "whenever a player becomes the monarch" trigger may fire. This is
// load-bearing for events.MonarchChange's one reader, rules'
// trigmatch.BecomeMonarchMatches -- it sees only the post-fold designation, so an
// unconditional emit here would queue trig:BecomeMonarch for a repeat
// BecomeMonarch (Custodi Lich resolving twice, two Peacekeeper Colossi, etc.).
// Suppressing at the source rather than inventing a previous-monarch field
// keeps events.Event's encoding untouched and replay-exact.
