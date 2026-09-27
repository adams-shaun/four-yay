package view

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Describe renders one event as one line of rules transcript, the same
// line on every replay: it reads names from g and fields from ev and
// nothing else — no clock, no map iteration, no engine. Call it with the
// game as of the last event in ev's batch, the RedactEvents convention, so
// a redacted Obj (0) reads as "a card" and a visible one by name. The
// client never composes rules text; this is where the words come from.
//
// ClockTick describes as "" (the client hides empty lines); an unknown
// Kind as "unknown event" rather than a panic.
// alterAttributeGrantPhrase carries the AlterAttribute attributes whose GRANT
// narration reads better as a gain than as "becomes <attribute>" (Suspend is
// a keyword, not a designation). Attributes absent from the map keep the
// designation phrase; the removal narration stays generic for every
// attribute -- no corpus emitter clears any of these with Amount < 1.
var alterAttributeGrantPhrase = map[string]string{
	"Suspend": "gains suspend",
}

func firstID(ids []state.ObjID) state.ObjID {
	if len(ids) > 0 {
		return ids[0]
	}
	return 0
}

func Describe(g *state.Game, ev events.Event) string {
	switch ev.Kind {
	case events.GameStart:
		return "Game starts with " + itoa(int64(ev.Amount)) + " players"
	case events.Shuffle:
		return player(g, ev.Player) + " shuffles their library"
	case events.LibraryOrder:
		return player(g, ev.Player) + " rearranges the top of their library"
	case events.ExtraTurn:
		if ev.Amount < 0 {
			return ""
		}
		if ev.Amount == 1 {
			return player(g, ev.Player) + " takes an extra turn"
		}
		return player(g, ev.Player) + " takes " + itoa(int64(ev.Amount)) + " extra turns"
	case events.ExtraPhase:
		// Only the grant is narrated; the consume (-1) and complete (-2)
		// messages are the turn structure's own bookkeeping, the same silence
		// the ExtraTurn consumption keeps.
		if ev.Amount <= 0 {
			return ""
		}
		what := "extra phase"
		if len(ev.IDs) > 0 {
			switch state.Step(ev.IDs[0]) {
			case state.StepBeginCombat:
				what = "additional combat phase"
			case state.StepUntap:
				what = "additional beginning phase"
			case state.StepUpkeep:
				what = "additional upkeep step"
			case state.StepEnd:
				what = "additional end-of-turn step"
			default:
				what = "additional " + state.Step(ev.IDs[0]).String() + " step"
			}
		}
		if ev.Amount > 1 {
			return player(g, ev.Player) + " gets " + itoa(int64(ev.Amount)) + " " + what + "s"
		}
		return player(g, ev.Player) + " gets an " + what
	case events.DoorUnlock:
		return obj(g, ev.Obj) + "'s locked door is unlocked"
	case events.SpeedChange:
		verb, n := "gains", ev.Amount
		if n < 0 {
			verb, n = "loses", -n
		}
		speed := int32(0)
		if g != nil && int(ev.Player) < len(g.Players) {
			speed = g.Players[ev.Player].Speed
		}
		return player(g, ev.Player) + " " + verb + " " + itoa(int64(n)) + " speed (speed " + itoa(int64(speed)) + ")"
	case events.MonarchChange:
		return player(g, ev.Player) + " becomes the monarch"
	case events.BlessingChange:
		return player(g, ev.Player) + " gets the city's blessing"
	case events.EnduringStoryChange:
		return player(g, ev.Player) + " has an enduring story"
	case events.TurnFaceDown:
		return obj(g, ev.Obj) + " is turned face down"
	case events.TurnFaceUp:
		// CR 708.6: Obj is the permanent that revealed its printed face.
		return obj(g, ev.Obj) + " is turned face up"
	case events.Specialize:
		// A permanent specializes into one of its alternate faces: Obj is the
		// specializing permanent, Amount the destination face index. Name the
		// chosen face the way the board reads it.
		if g != nil {
			if o := g.Obj(ev.Obj); o != nil && o.Card != nil && int(ev.Amount) >= 0 && int(ev.Amount) < len(o.Card.Faces) {
				if f := o.Card.Faces[ev.Amount]; f.Name != "" {
					return obj(g, ev.Obj) + " specializes into " + f.Name
				}
			}
		}
		return obj(g, ev.Obj) + " specializes"
	case events.PhaseOut:
		// CR 702.25: Amount 1 is a permanent phasing out, -1 phasing in.
		if ev.Amount >= 1 {
			return obj(g, ev.Obj) + " phases out"
		}
		return obj(g, ev.Obj) + " phases in"
	case events.SearchedLibrary:
		// CR 701.23: the completed-search marker (trig:SearchedLibrary).
		// Player is the seat whose library was searched; the resolving source
		// (ev.Obj) is not named -- the search's card moves carry their own
		// events and lines.
		return player(g, ev.Player) + " searches their library"
	case events.RingTemptsYou:
		// CR 701.54: the temptation and the designation it made. A bearer of
		// 0 is CR 701.54d's impossible-choice shape (no creature controlled):
		// the temptation still happened, so the line still records it.
		if ev.Obj == 0 {
			return "The Ring tempts " + player(g, ev.Player)
		}
		return "The Ring tempts " + player(g, ev.Player) + " (" + obj(g, ev.Obj) + " is the Ring-bearer)"
	case events.RingEmblemPush:
		// CR 701.54c: the Ring emblem's level abilities have no card and no
		// object, so the line names the level's rules text (ringEmblemLabel's
		// wording, duplicated here because view cannot import rules).
		switch ev.Amount {
		case 1:
			return player(g, ev.Player) + " is tempted: the Ring emblem draws a card (Ring-bearer attacks)"
		case 2:
			return player(g, ev.Player) + " is tempted: the Ring emblem discards (Ring-bearer blocked)"
		case 3:
			return player(g, ev.Player) + " is tempted: the Ring emblem sacrifices its Ring-bearer (combat damage)"
		case 4:
			return player(g, ev.Player) + " is tempted: the Ring emblem drains each opponent (the Ring tempts you)"
		}
		return player(g, ev.Player) + " is tempted: a Ring emblem ability"
	case events.StartingPlayerChange:
		return player(g, ev.Player) + " becomes the starting player"
	case events.ControlChange:
		return player(g, ev.Player) + " gains control of " + obj(g, ev.Obj)
	case events.Goad:
		return obj(g, ev.Obj) + " is goaded by " + player(g, ev.Player)
	case events.PlayerCounterChange:
		verb, n := "gets", ev.Amount
		if n < 0 {
			verb, n = "loses", -n
		}
		return player(g, ev.Player) + " " + verb + " " + itoa(int64(n)) + " " + strings.ToLower(ev.Counter) + " counter(s)"
	case events.Imprint:
		if ev.Text == "clear" {
			return obj(g, ev.Obj) + " clears imprinted cards"
		}
		if ev.Text == "exiled-with" {
			return obj(g, ev.Obj) + " exiles " + objs(g, ev.IDs) + " with it"
		}
		return obj(g, ev.Obj) + " imprints " + objs(g, ev.IDs)
	case events.Pair:
		return obj(g, ev.Obj) + " pairs with " + obj(g, firstID(ev.IDs))
	case events.MyriadCopy:
		return obj(g, ev.Obj) + " creates a Myriad copy attacking " + player(g, state.PlayerID(firstID(ev.IDs)))
	case events.MyriadCleanup:
		return "Myriad tokens are exiled at end of combat"
	case events.TokenAttacks:
		// A token that entered tapped and attacking (Mobilize, Kari Zev):
		// Obj is the minted token, IDs[0] the player it is attacking.
		if len(ev.IDs) > 0 {
			return obj(g, ev.Obj) + " attacks " + player(g, state.PlayerID(firstID(ev.IDs)))
		}
		return obj(g, ev.Obj) + " attacks"
	case events.CopyToken:
		// DB$ CopyPermanent's mint (Flamerush Rider, Molten Echoes, populate):
		// Obj is the COPIED card, so the line reads the copy's provenance;
		// the entry itself is the follow-up MoveZone's own line. The
		// entry-state riders (the Amount bitmask) are named when set.
		text := obj(g, ev.Obj) + " creates a token copy"
		if ev.Amount&events.CopyTokenTapped != 0 {
			text += ", tapped"
		}
		if ev.Amount&events.CopyTokenAttacking != 0 {
			if len(ev.IDs) > 0 {
				text += " and attacking " + player(g, state.PlayerID(firstID(ev.IDs)))
			} else {
				text += " and attacking"
			}
		}
		if ev.Amount&events.CopyTokenExileCombat != 0 {
			text += " (exiled at end of combat)"
		}
		return text
	case events.CloneStatic:
		return obj(g, ev.Obj) + " gains a copy static ability"
	case events.DamageProvenance:
		// Game-long damage-by-source provenance (the_fallen, diseased_vermin):
		// Obj is the damage SOURCE and IDs[0] the recipient -- PlayerRef-
		// encoded for a seat, a plain ObjID for an object. This is the record
		// fact the damage predicates read, not a second damage announcement
		// (the Damage event already says "Bob takes N damage"), so the line
		// names the pair and the amount that landed. A hostile/fuzz event
		// with no recipient (or a stale id) degrades rather than panics.
		if len(ev.IDs) == 0 {
			return obj(g, ev.Obj) + " damaged something"
		}
		if p, isPlayer := ev.IDs[0].PlayerRef(); isPlayer {
			return obj(g, ev.Obj) + " damaged " + player(g, p) + " this game"
		}
		return obj(g, ev.Obj) + " damaged " + obj(g, ev.IDs[0]) + " this game"
	case events.ClonePermanent:
		// CR 613.1a's layer-1 copy basis (api:Clone, task api-clone): Obj is
		// the object that becomes the copy and IDs[0] the object copied from;
		// a zero/absent id is the expiry/cleanup clear.
		if len(ev.IDs) == 0 || ev.IDs[0] == 0 {
			return obj(g, ev.Obj) + " stops being a copy"
		}
		return obj(g, ev.Obj) + " becomes a copy of " + obj(g, ev.IDs[0])
	case events.Exert:
		// CR 702.100 (task exert1): the exert itself, and the consume marker
		// the untap-step scan emits as it passes an exerted permanent -- the
		// window that made it skip that untap closes there.
		if ev.Amount < 0 {
			return obj(g, ev.Obj) + " skips its untap step (exerted)"
		}
		return obj(g, ev.Obj) + " is exerted"
	case events.Enlist:
		// CR 702.160 (task enlist1): the enlist action record. Obj is the
		// ATTACKING creature that enlisted; IDs[0] the nonattacking creature
		// it tapped (its own Tap event is a separate line) and Player the
		// attacker's controller. The +X/+0 pump is a continuous effect, not
		// a line of its own.
		if len(ev.IDs) == 0 {
			return obj(g, ev.Obj) + " enlists a creature"
		}
		return obj(g, ev.Obj) + " enlists " + obj(g, ev.IDs[0])
	case events.PlanarRoll:
		// CR 901.3 (task rollplanar1): the roll record. The per-die faces ride
		// the die-roll Notes rules emits beside this event; Amount > 1 names
		// the post-replacement count (never render the face list here — the
		// Describe-coverage fuzz carries arbitrary IDs values that are not
		// results, so the faces are only ever read off the Notes). The ignored
		// count rides Counter as its decimal; never say "ignoring 0".
		if ev.Amount > 1 {
			text := player(g, ev.Player) + " rolls " + itoa(int64(ev.Amount)) + " planar dice"
			if n, err := strconv.Atoi(ev.Counter); err == nil && n > 0 {
				text += " (ignoring " + itoa(int64(n)) + ")"
			}
			return text
		}
		return player(g, ev.Player) + " rolls the planar die"
	case events.PlanarDeckShuffle:
		return player(g, ev.Player) + " shuffles the planar deck"
	case events.PlanarReveal:
		return obj(g, ev.Obj) + " is revealed as the current plane"
	case events.PlanarWalk:
		return "Planeswalk to the next plane"
	case events.ChaosEnsues:
		// CR 901.9 (task planar-verbs): the chaos-ensues marker. Obj is the
		// plane it erupts on when the emitter resolved one (the roll path and
		// the DB$ ChaosEnsues verb both do; the Describe-coverage fuzz may
		// carry a zero Obj, so the bare form stays grammatical).
		if ev.Obj != 0 {
			return obj(g, ev.Obj) + " erupts in chaos"
		}
		return "Chaos ensues"
	case events.RollDice:
		// The roll-action PROPOSAL (task rolldice-repl) is never logged -- the
		// per-die Notes are the roll's transcript lines -- but the Describe-
		// coverage walk visits every Kind, so the held proposal describes as
		// the pending roll action it is. Amount is the proposal's dice count.
		if ev.Amount > 1 {
			return player(g, ev.Player) + " would roll " + itoa(int64(ev.Amount)) + " dice"
		}
		return player(g, ev.Player) + " would roll a die"
	case events.Cascade:
		// The cascade-instruction replacement PROPOSAL (Averna, the Chaos
		// Bloom's R:Event$ Cascade) is never logged -- the rules tier holds
		// it out to the replacement matcher exactly as Engine.Scry holds
		// events.Scry -- but the Describe-coverage walk visits every Kind,
		// so the held proposal describes as the pending cascade
		// replacement it is. IDs is the ordered exiled batch it carries.
		if len(ev.IDs) == 1 {
			return player(g, ev.Player) + " would cascade into 1 exiled card"
		}
		return player(g, ev.Player) + " would cascade into " + itoa(int64(len(ev.IDs))) + " exiled cards"
	case events.NoteNumber:
		return obj(g, ev.Obj) + " notes " + itoa(int64(ev.Amount))
	case events.PlayerNoted:
		return player(g, ev.Player) + " is noted for " + ev.Text
	case events.PlayerNoteCleared:
		return player(g, ev.Player) + " is no longer noted for " + ev.Text
	case events.CardNoted:
		// The card-notation sibling of PlayerNoted (NoteCards$ Remembered /
		// TriggeredSource with NoteCardsFor$): Obj is the noted card, Text the
		// label a later `Card.NotedFor<label>` filter read selects on.
		return obj(g, ev.Obj) + " is noted for " + ev.Text
	case events.Mutate:
		// CR 702.140d: one mutating card merges into the surviving permanent.
		// Text is "top" or "under" (CR 702.140b's placement).
		place := "under"
		if ev.Text == "top" {
			place = "on top of"
		}
		return obj(g, ev.Obj) + " mutates with a card " + place + " it"
	case events.MoveZone:
		return obj(g, ev.Obj) + " moves from " + zone(ev.From) + " to " + zone(ev.To)
	case events.Draw:
		if ev.Obj == 0 {
			return player(g, ev.Player) + " draws a card"
		}
		return player(g, ev.Player) + " draws " + obj(g, ev.Obj)
	case events.LifeChange:
		verb, n := "gains", ev.Amount
		if n < 0 {
			verb, n = "loses", -n
		}
		return player(g, ev.Player) + " " + verb + " " + itoa(int64(n)) + " life (" + life(g, ev.Player) + ")"
	case events.Damage:
		if g != nil && g.Obj(ev.Obj) != nil {
			return obj(g, ev.Obj) + " takes " + itoa(int64(ev.Amount)) + " damage"
		}
		return player(g, ev.Player) + " takes " + itoa(int64(ev.Amount)) + " damage"
	case events.Tap:
		return obj(g, ev.Obj) + " taps"
	case events.Untap:
		return obj(g, ev.Obj) + " untaps"
	case events.StepChange:
		return "Step: " + ev.Step.String()
	case events.TurnChange:
		return "Turn " + itoa(int64(ev.Amount)) + ": " + player(g, ev.Player)
	case events.Priority:
		return player(g, ev.Player) + " has priority"
	case events.PutOnStack:
		return player(g, ev.Player) + " casts " + obj(g, ev.Obj)
	case events.Resolve:
		return obj(g, ev.Obj) + " resolves"
	case events.ManaAdd:
		if ev.Amount < 0 {
			return player(g, ev.Player) + " spends " + mana(ev.Counter, -ev.Amount)
		}
		return player(g, ev.Player) + " adds " + mana(ev.Counter, ev.Amount)
	case events.ManaClear:
		return player(g, ev.Player) + "'s mana pool empties"
	case events.CounterChange:
		verb, n := "gets", ev.Amount
		if n < 0 {
			verb, n = "loses", -n
		}
		s := obj(g, ev.Obj) + " " + verb + " " + itoa(int64(n)) + " " + ev.Counter + " counter"
		if n != 1 {
			s += "s"
		}
		if ev.Counter == "STUN" && ev.Amount < 0 {
			if ev.Text == events.UntapReplacedByStunNotice {
				return obj(g, ev.Obj) + " would untap, but a stun counter is removed instead"
			}
		}
		return s
	case events.Explore:
		// The explore record (task explore1): the revealed card is already
		// public (the reveal Note that precedes the record), so the line
		// names only the explorer.
		return obj(g, ev.Obj) + " explores"
	case events.Investigate:
		// The investigate record (CR 701.36a, task investtrig1) is a pure
		// marker: the Clue-token mint is its own TokenCreate line, so this
		// line names only the investigating seat (Player; Obj is the source
		// permanent, which may be 0 for a game-rule investigate).
		return player(g, ev.Player) + " investigates"
	case events.GiftPromise:
		// CR 702.168: the promise is attached to the cast object; Amount
		// distinguishes a decline from the promised-opponent seat in Player.
		if ev.Amount != 0 {
			return player(g, objController(g, ev.Obj)) + " promises a gift to " + player(g, ev.Player)
		}
		return player(g, objController(g, ev.Obj)) + " declines to promise a gift"
	case events.GiveGift:
		// The completed gift marker follows the gift action's own events.
		return player(g, ev.Player) + " gives a gift"
	case events.Clash:
		// The completed clash marker (CR 701.31, task clash1), one per
		// clashing player: Player is the clashing seat, Amount 1 when that
		// player WON the clash and 0 when they lost or tied (CR 701.31
		// leaves a tie with no winner). The reveal and the top/bottom
		// placements are their own preceding Note/LibraryOrder lines, so
		// this line names only the seat and the outcome; Obj is the
		// resolving source permanent and may be 0 for a source-less body.
		if ev.Amount == 1 {
			return player(g, ev.Player) + " wins the clash"
		}
		return player(g, ev.Player) + " loses the clash"
	case events.Evolved:
		// The completed evolve marker (CR 702.99b, task trig:Evolved) is a
		// pure no-op like GiveGift/Investigate: the +1/+1 counter placement
		// is its own CounterChange line, so this line names only the
		// evolving permanent (Obj is the Evolve trigger's own source).
		return obj(g, ev.Obj) + " evolves"
	case events.Exploit:
		// The exploit record (CR 702.58a, task exploit1): Obj is the
		// exploiting creature, IDs[0] the exploited (sacrificed) one. The
		// sacrifice's own MoveZone line already named the creature, so this
		// line names both halves of the action the way the oracle reads.
		s := obj(g, ev.Obj) + " exploits"
		if len(ev.IDs) > 0 {
			s += " " + obj(g, ev.IDs[0])
		}
		return s
	case events.Discover, events.Seek, events.Surveil, events.Scry, events.Proliferate:
		// The discover (CR 701.57), seek (task trigdisc1), surveil
		// (CR 701.42) and scry (CR 701.18, task scrybottom) records are pure
		// markers: the action's own state changes (the exiles/reveals and the
		// sought card's move, the surveil's or scry's KArrange answer) are
		// their own lines, so these lines name only the acting seat (Player;
		// Obj is the source permanent, which may be 0 for a source-less body).
		if ev.Kind == events.Seek {
			return player(g, ev.Player) + " seeks"
		}
		if ev.Kind == events.Surveil {
			return player(g, ev.Player) + " surveils"
		}
		if ev.Kind == events.Scry {
			return player(g, ev.Player) + " scries"
		}
		if ev.Kind == events.Proliferate {
			// The completed proliferate marker (CR 701.27, task
			// trig-proliferate) follows the counter batch's own lines and
			// names only the proliferating seat; Obj is the source permanent.
			return player(g, ev.Player) + " proliferates"
		}
		return player(g, ev.Player) + " discovers"
	case events.StoreSVar:
		// An api:StoreSVar write (Minion of the Wastes' LifePaidOnETB): Obj
		// the object whose runtime SVar table was written, Text the SVar
		// name, Amount the stored value.
		return obj(g, ev.Obj) + " records " + ev.Text + " = " + itoa(int64(ev.Amount))
	case events.Connive:
		// The connive record (task connive1): the draws and discards are
		// already their own lines (Draw/Discard events), so this line names
		// only the conniving permanent.
		return obj(g, ev.Obj) + " connives"
	case events.AlterAttribute:
		// Attribute changes name the designation carried on the event; keep
		// both grant and removal narration aligned with the event fold.
		// The default phrase ("becomes <attribute>") reads for the designation
		// attributes (Suspected/Monstrous/Renowned/Plotted); the map carries the
		// attribute-shaped riders whose grant reads better as a gain -- Suspend
		// is a keyword, not a designation.
		if ev.Amount >= 1 {
			if phrase, ok := alterAttributeGrantPhrase[ev.Text]; ok {
				return obj(g, ev.Obj) + " " + phrase
			}
			return obj(g, ev.Obj) + " becomes " + strings.ToLower(ev.Text)
		}
		return obj(g, ev.Obj) + " is no longer " + strings.ToLower(ev.Text)
	case events.CombatRetarget:
		// api:ChangeCombatants's reselect: Obj the attacker, Player the new
		// defender. The old defender needs no line (the re-pointed attack is
		// unblocked, and the next combat-damage line shows where it went).
		return obj(g, ev.Obj) + " now attacks " + player(g, ev.Player)
	case events.DeclareAttackers:
		if len(ev.IDs) == 0 {
			return "No attackers"
		}
		return objs(g, ev.IDs) + " " + plural(len(ev.IDs), "attacks", "attack") + " " + player(g, ev.Player)
	case events.DeclareBlockers:
		if len(ev.Pairs) == 0 {
			return "No blocks"
		}
		parts := make([]string, 0, len(ev.Pairs))
		for _, p := range ev.Pairs {
			parts = append(parts, obj(g, p[1])+" blocks "+obj(g, p[0]))
		}
		return strings.Join(parts, "; ")
	case events.PlayerLost:
		return player(g, ev.Player) + " loses the game"
	case events.GameOver:
		if ev.Amount == 1 {
			return "The game is a draw"
		}
		return player(g, ev.Player) + " wins the game"
	case events.DecisionAsk:
		return player(g, ev.Player) + " is asked: " + ev.Text
	case events.DecisionMade:
		return player(g, ev.Player) + " answers " + ev.Text
	case events.Note:
		if ev.Text == "" && len(ev.IDs) > 0 && !ev.Secret {
			// effReveal's reveal (task fb-3f1cc033): the Note carries the
			// revealed cards' ids and no text of its own, so Describe renders
			// them — the transcript line names WHAT was revealed ("player 0
			// reveals Mountain #82"), which is the only data path a client
			// has for hidden-zone ids in a Note. Rule 3 of view.RedactEvents
			// (Ruling T23-w) passes a non-Secret Note through unchanged, so
			// these ids are public by contract on every viewer's line.
			return player(g, ev.Player) + " reveals " + objs(g, ev.IDs)
		}
		if ev.Secret && ev.Text == "" && len(ev.IDs) > 0 {
			// A private look recorded by effects' emitLook: only the looker's
			// own copy of the Secret Note still carries the ids (rule 1 of
			// view.RedactEvents strips them from every other viewer, whose
			// line falls to the generic "looks at hidden cards" below), so
			// this branch is the looker's line alone. The looked-at player is
			// derived from the cards' owner — ownership never changes, so the
			// derivation is stable — and the looked-at zone from From, which
			// rule 1 keeps on every copy and which the looker's cards may
			// since have left. The names are on the line because the
			// transcript is the client's only data path for hidden-zone ids
			// in a Note, exactly as for the reveal line above: a look whose
			// line named nothing would show the looker nothing.
			return player(g, ev.Player) + " " + lookClause(g, ev)
		}
		if ev.Text != "" {
			// The genesis toss Note is the second event: GameStart is always
			// sequence zero and rules.New emits the toss before any deal event.
			// Match that event position AND its complete, seat-bound deck-identity
			// text, rather than a loose phrase: a later card-effect Note is allowed
			// to say the same words and must remain verbatim. The transcript renders
			// this one subject through player(), so PlayerName remains visible
			// without entering the event chain.
			if ev.Seq == 1 && g != nil && ev.Text == tossNoteText(g, ev.Player) {
				return player(g, ev.Player) + " won the toss"
			}
			return ev.Text
		}
		if ev.Secret {
			return player(g, ev.Player) + " looks at hidden cards"
		}
		// Neither a message nor Secret: a malformed or defensively-tested
		// Note (TestDescribeCoversEveryKind's generic fuzz event hits this
		// exact shape). "" is reserved for ClockTick alone, so this still
		// needs a word.
		return "Note"
	case events.LandPlayed:
		return player(g, ev.Player) + " plays a land"
	case events.TargetsChosen:
		if ev.Amount == 1 {
			return obj(g, ev.Obj) + " targets " + player(g, ev.Player)
		}
		return obj(g, ev.Obj) + " targets " + objs(g, ev.IDs)
	case events.FlipFace:
		return obj(g, ev.Obj) + " turns to face " + itoa(int64(ev.Amount))
	case events.ClockTick:
		return ""
	case events.TriggerPush:
		return obj(g, ev.Obj) + " triggers"
	case events.EndCombatReset:
		return "Combat ends"
	case events.CastInfo:
		// Records how a spell was cast, right before the PutOnStack line
		// (Task 4): Amount is the value chosen for {X}, Counter the comma-
		// separated mode flags ("kicked", ...). Described, not "": X and
		// the kicker are the caster's own decisions and nothing else in the
		// transcript shows them (mana-spend lines record totals, not what
		// they paid for), and the line is self-contained, so a DVR scrub
		// landing on it needs no neighbouring line. It carries no Player
		// field, so the caster is the card's controller. The two lines read
		// as one cast: the how, then the fact. The same event also records an
		// activated ability's paid {X} on the ability's stack object
		// (rules/cast.go commitCast's ability arm), and that object must not
		// read as a cast: the verb follows the object -- "activates" for an
		// ability stack object minted by AbilityPush, "casts" for a real
		// spell. The triggered/activated split is state.TriggerOf, the one
		// classifier StackView.Kind (view.go) also uses, so the transcript
		// and the stack view cannot disagree.
		verb := "casts"
		if g != nil {
			if o := g.Obj(ev.Obj); o != nil && o.Ability != nil {
				if _, triggered := state.TriggerOf(g, o); !triggered {
					verb = "activates"
				}
			}
		}
		cast := player(g, objController(g, ev.Obj)) + " " + verb + " " + obj(g, ev.Obj)
		how := make([]string, 0, 2)
		if ev.Amount != 0 {
			how = append(how, "X = "+itoa(int64(ev.Amount)))
		}
		if ev.Counter != "" {
			how = append(how, ev.Counter)
		}
		if len(how) == 0 {
			return cast
		}
		return cast + " (" + strings.Join(how, ", ") + ")"
	case events.Choose:
		// Records an "as this enters, choose ..." answer (etbAsk/etbAnswer):
		// Counter discriminates the shape ("name", "type", "number"; the
		// chosen name/type rides on Text, the number on Amount). The
		// "protector" shape (CR 310.10) instead carries the chosen
		// opponent's seat on Player, so it names that seat rather than the
		// card's controller. Every other shape carries no Player field, so
		// the chooser is the card's controller. An unrecognized Counter (a
		// fuzz event, a future shape) degrades to a generic "chooses a
		// value" line rather than inventing a field.
		if ev.Counter == "protector" {
			return player(g, ev.Player) + " protects " + obj(g, ev.Obj) +
				" (chosen by " + player(g, objController(g, ev.Obj)) + ")"
		}
		what := "a value"
		switch ev.Counter {
		case "name":
			what = "the name " + ev.Text
		case "type":
			what = "the type " + ev.Text
		case "number":
			what = "the number " + itoa(int64(ev.Amount))
		}
		return player(g, objController(g, ev.Obj)) + " chooses " + what + " for " + obj(g, ev.Obj)
	case events.TokenCreate:
		// A token minted onto the battlefield (Player is its owner and
		// controller). The event names the token by its script key -- the
		// minted object's id is assigned inside Apply, so there is nothing
		// to read from ev.Obj -- and the display name comes from the game's
		// token table; an unresolvable key (a hand-built game, the fuzz)
		// degrades to a plain "creates a token".
		if name := tokenName(g, ev.Text); name != "" {
			return player(g, ev.Player) + " creates a " + name + " token"
		}
		return player(g, ev.Player) + " creates a token"
	case events.CardToken:
		// A battlefield token that is a copy of the CARD object Obj names
		// (encore). The minted copy's own id is assigned inside Apply, so
		// the line names the original it duplicates -- the StackCopy shape.
		return player(g, ev.Player) + " creates a token copy of " + obj(g, ev.Obj)
	case events.StackCopy:
		// A copy of the stack object Obj, controlled by Player (CR
		// 707.10a), placed on top of the stack. The copy itself gets a new
		// id Apply assigns, so the line names the original it duplicates.
		return player(g, ev.Player) + " copies " + obj(g, ev.Obj)
	case events.Attach:
		// Aura/Equipment permanent Obj attaches to (IDs[0]) or detaches
		// from (empty IDs) its bearer. rules/attach.go's one detach-with-
		// reason site carries the why on Text; when present it is appended,
		// because "detaches" alone cannot explain an Equipment floating
		// free of its bearer mid-combat.
		if len(ev.IDs) > 0 {
			return obj(g, ev.Obj) + " attaches to " + obj(g, ev.IDs[0])
		}
		s := obj(g, ev.Obj) + " detaches"
		if ev.Text != "" {
			s += " (" + ev.Text + ")"
		}
		return s
	case events.Unattached:
		// CR 701.3b: Obj became unattached from the former bearer (IDs[0]).
		// The attachment stays on the battlefield (its own departure is a
		// MoveZone), so this is the detach half of Attach's line; Text carries
		// the reason the same way Attach's detach shape does.
		s := obj(g, ev.Obj) + " becomes unattached"
		if len(ev.IDs) > 0 {
			s += " from " + obj(g, ev.IDs[0])
		}
		if ev.Text != "" {
			s += " (" + ev.Text + ")"
		}
		return s
	case events.AbilityPush:
		// An activated ability minted onto the stack (the same shape
		// TriggerPush uses for triggers, Ruling T20-a): Player is the
		// activator, Obj the source permanent. The mirror of "X triggers":
		// the IR carries no ability names, so the source permanent is what
		// a line can name.
		return player(g, ev.Player) + " activates " + obj(g, ev.Obj)
	case events.GrantAbilityPush:
		// A cross-object ability grant's activation (CR 613.1f, the
		// printed-Continuous AddAbility$ fix): Player is the activator and
		// Obj is the RECIPIENT permanent -- the granted ability's own
		// source -- so naming it reads the same way AbilityPush does. The
		// parenthetical marks that another object granted it.
		return player(g, ev.Player) + " activates " + obj(g, ev.Obj) + " (granted)"
	case events.ModeChosen:
		// A cast/placement mode announcement or mid-resolution modal answer.
		// Player chose; Text carries the chosen option labels as csv. Mirrors
		// DecisionMade's "answers" shape; an empty Text (a fuzz event)
		// degrades to "a mode".
		labels := strings.ReplaceAll(ev.Text, ",", ", ")
		if labels == "" {
			labels = "a mode"
		}
		return player(g, ev.Player) + " chooses " + labels
	case events.CmdDamage:
		// Commander combat damage to a player (CR 903.10, task m33): Obj
		// is the source commander, Player the damaged player, Amount what
		// actually landed. The ordinary Damage event already says "Bob
		// takes N damage"; this line is the second clock -- the
		// per-commander cumulative tally and its lethal threshold -- the
		// thing this event adds and the only reason it exists. The tally
		// is read as of g (the batch's last Apply already folded this hit
		// in); it is unknown in a non-Commander game or when the source is
		// not on any roster, so the parenthetical drops then.
		s := obj(g, ev.Obj) + " deals " + itoa(int64(ev.Amount)) + " commander damage to " + player(g, ev.Player)
		if total, ok := cmdDamageTally(g, ev.Player, ev.Obj); ok {
			s += " (" + itoa(int64(total)) + " total; 21 is lethal)"
		}
		return s
	case events.DelayedRemove:
		return "delayed trigger registration removed"
	case events.DelayedForget:
		// One remembered card was dropped from a delayed registration (the
		// ForgetOnMoved$/ForgetCounter$ trim), not the whole promise -- so it
		// reads as a forget, not the DelayedRemove line above it.
		return obj(g, ev.Obj) + " is forgotten by a delayed trigger"
	case events.DelayedRegister:
		// A delayed trigger being registered (CR 603.7, dt1): Obj is the
		// source that created it, Text the phase it waits for. The line says
		// only that the promise was made -- what it will DO is the Execute$
		// sub-ability, which is named by the DelayedPush line below when it
		// actually happens, so saying it twice here would double-report an
		// effect that may never fire (the source can leave, the controller
		// can lose, the game can end first).
		if ev.Text == "" {
			return obj(g, ev.Obj) + " sets up a delayed trigger"
		}
		// A ValidPlayer$-gated registration's Text carries the "|VP=<value>"
		// suffix the rules side decodes; the display keeps the phase only.
		phase := ev.Text
		if i := strings.Index(phase, "|VP="); i >= 0 {
			phase = phase[:i]
		}
		return obj(g, ev.Obj) + " sets up a delayed trigger for " + phase
	case events.DelayedPush:
		// The registered phase arrived and the delayed ability went on the
		// stack. Obj is the minted stack object; its source name is what a
		// reader recognises, so prefer it and fall back to the minted id.
		return obj(g, ev.Obj) + " triggers (delayed)"
	case events.KeywordTriggerPush:
		return obj(g, ev.Obj) + " triggers (" + strings.TrimPrefix(ev.Counter, "__kw") + ")"
	case events.GrantTriggerPush:
		// A static-grant's trigger went on the stack (AddTrigger$, the
		// STATION 8+ shape): the same "triggers" phrasing DelayedPush uses --
		// the granted body's own text is the resolving ability's line, not
		// the push's, so saying what it will do twice would double-report it.
		return obj(g, ev.Obj) + " triggers (granted)"
	case events.MergedTriggerPush:
		// A mutated pile's under-card trigger went on the stack (CR 702.140d):
		// the same "triggers" phrasing -- the resolving ability's own line is
		// what carries what it does.
		return obj(g, ev.Obj) + " triggers (merged)"
	case events.GainedAbilityPush:
		// A has-all-abilities-of activated ability went on the stack (Forge's
		// GainsAbilitiesOf$): Obj is the minted stack object.
		return obj(g, ev.Obj) + " activates (gained)"
	case events.KeywordAbilityPush:
		// A keyword-granted activated ability went on the stack (a layer-6
		// AddKeyword$ Cycling grant, CR 613.1f): the same "activates"
		// phrasing the other grant pushes use -- the resolving ability's own
		// line carries what it does.
		return obj(g, ev.Obj) + " activates (keyword-granted)"
	case events.GainedTriggerPush:
		// A has-all-abilities-of triggered ability went on the stack (Forge's
		// GainsTriggerAbsOf$): the same "triggers" phrasing the other grant
		// pushes use -- the resolving ability's own line carries what it does.
		return obj(g, ev.Obj) + " triggers (gained)"
	case events.ManaActivate:
		// The ActivationLimit$ scan marker for a mana ability's activation
		// (events.ManaActivate's own comment). Obj is the source permanent.
		return obj(g, ev.Obj) + " is activated for mana"
	case events.XChange:
		// A mid-resolution effect rewrote the {X} a stack object was paid
		// with (events.XChange's own comment): Obj the stack object, Amount
		// the new value.
		return obj(g, ev.Obj) + " has its X set to " + itoa(int64(ev.Amount))
	}
	return "unknown event"
}

// obj names an object as "Name #id", "<Name>'s ability #id" for a faceless
// stack object whose source card is resolvable, "an ability #id" for a
// faceless object whose source is not (0, unresolvable, or itself faceless),
// "a card" for the redacted id 0, and "#id" for an id the game cannot
// resolve (nil g, stale or tampered data).
func obj(g *state.Game, id state.ObjID) string {
	if id == 0 {
		return "a card"
	}
	tag := "#" + strconv.FormatUint(uint64(id), 10)
	if g == nil {
		return tag
	}
	o := g.Obj(id)
	if o == nil {
		return tag
	}
	if f := o.Face(); f != nil && f.Name != "" {
		return f.Name + " " + tag
	}
	if src := abilitySource(g, o); src != "" {
		return src + "'s ability " + tag
	}
	return "an ability " + tag
}

// abilitySource resolves a faceless ability object's source (state.Object.
// Source, set by events.Apply's TriggerPush/AbilityPush/DelayedPush minting
// paths) to the name of the card it came from, or "" when that path cannot
// produce one: a zero source id, an id the game no longer holds, or a source
// that is itself faceless (a copied or minted ability) — a source whose own
// name is unknowable must not yield an empty possessive, so it degrades to
// the bare "an ability #id" instead. It mirrors obj's own care about nil g,
// id == 0 and stale ids, because this is called on log lines for tampered
// and historical data.
func abilitySource(g *state.Game, o *state.Object) string {
	if o.Source == 0 {
		return ""
	}
	src := g.Obj(o.Source)
	if src == nil {
		return ""
	}
	if f := src.Face(); f != nil && f.Name != "" {
		return f.Name
	}
	return ""
}

func objs(g *state.Game, ids []state.ObjID) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, obj(g, id))
	}
	return strings.Join(parts, ", ")
}

// objController is the player who controls id, or seat 0 when g cannot
// resolve it. CastInfo and Choose carry no Player field of their own (a
// card's controller is its caster/chooser at cast time), so their lines
// derive it from the object rather than reading the event's zero Player.
func objController(g *state.Game, id state.ObjID) state.PlayerID {
	if g == nil {
		return 0
	}
	if o := g.Obj(id); o != nil {
		return o.Controller
	}
	return 0
}

// player is the seat's display name (an independent PlayerName when one was
// configured, else the deck identity the engine hashes), or "seat N" when g
// cannot resolve it. Transcript lines read the same name the player box
// shows, so the log and the box never disagree about who is who.
func player(g *state.Game, p state.PlayerID) string {
	if g != nil && int(p) < len(g.Players) {
		if g.Players[p].PlayerName != "" {
			return g.Players[p].PlayerName
		}
		if g.Players[p].Name != "" {
			return g.Players[p].Name
		}
	}
	return "seat " + strconv.Itoa(int(p))
}

// tossNoteText is the deterministic chain text rules.New emits for its
// genesis toss Note. It deliberately follows player()'s deck-name fallback
// but excludes PlayerName, which is display-only and must not reach the hash
// chain. Keeping the fallback here makes an empty deck identity render through
// PlayerName rather than leaking the raw "seat N" chain text to the transcript.
func tossNoteText(g *state.Game, p state.PlayerID) string {
	name := ""
	if g != nil && int(p) < len(g.Players) {
		name = g.Players[p].Name
	}
	if name == "" {
		name = "seat " + strconv.Itoa(int(p))
	}
	return name + " won the toss"
}

// life is the seat's life total as of g, or "?" when unresolvable.
func life(g *state.Game, p state.PlayerID) string {
	if g != nil && int(p) < len(g.Players) {
		return itoa(int64(g.Players[p].Life))
	}
	return "?"
}

// tokenName is the display name of the token definition a TokenCreate
// event's script key names ("Goblin" for "r_1_1_goblin"), or "" when the
// game has no such definition (a hand-built game, an unknown key, the
// fuzz).
func tokenName(g *state.Game, key string) string {
	if g == nil {
		return ""
	}
	c, ok := g.Tokens[key]
	if !ok || c == nil || len(c.Faces) == 0 {
		return ""
	}
	return c.Faces[0].Name
}

// cmdDamageTally returns player p's cumulative commander damage as of g
// from the commander id -- CR 903.10's second clock, the tally 21 is
// lethal against -- and whether id is on any roster. It mirrors the
// match-wide dense index events.Apply folds CmdDamage into (seat order,
// then each seat's genesis Commanders order; never a map), so the total
// reads the same slot a log-only reconstruction would rebuild.
func cmdDamageTally(g *state.Game, p state.PlayerID, id state.ObjID) (int32, bool) {
	if g == nil || int(p) >= len(g.Players) {
		return 0, false
	}
	idx := 0
	for s := range g.Players {
		for _, c := range g.Players[s].Commanders {
			if c == id {
				if idx < len(g.Players[p].CmdDamage) {
					return g.Players[p].CmdDamage[idx], true
				}
				return 0, false
			}
			idx++
		}
	}
	return 0, false
}

// zone is the zone's name, total over out-of-range values.
func zone(z state.Zone) string {
	if !z.Valid() {
		return "nowhere"
	}
	return z.String()
}

// lookClause is the body of a looker's own copy of a private-look Note
// (effects.emitLook): "looks at <target>'s hand: <cards>", or the own-zone
// form when the looker looked at their own hidden zone (a Dig or Scry window).
func lookClause(g *state.Game, ev events.Event) string {
	noun := "cards"
	if ev.From.Valid() {
		noun = zone(ev.From)
	}
	target := ev.Player
	if g != nil {
		if o := g.Obj(ev.IDs[0]); o != nil {
			target = o.Owner
			if !ev.From.Valid() {
				noun = zone(o.Zone)
			}
		}
	}
	if target == ev.Player {
		return "looks at the cards in their own " + noun + ": " + objs(g, ev.IDs)
	}
	return "looks at " + player(g, target) + "'s " + noun + ": " + objs(g, ev.IDs)
}

// mana renders n symbols of one colour: "{G}{G}". An empty symbol is
// colourless.
func mana(sym string, n int32) string {
	if sym == "" {
		sym = "C"
	}
	// A typed unit ("TreasureC", "ArtifactW") is provenance bookkeeping, not
	// a symbol: the transcript names only its colour.
	if _, slot, ok := state.TypedManaCounter(sym); ok {
		sym = string("WUBRGC"[slot])
	}
	if n <= 0 {
		return ""
	}
	if n > 20 {
		return itoa(int64(n)) + " {" + sym + "}"
	}
	return strings.Repeat("{"+sym+"}", int(n))
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
