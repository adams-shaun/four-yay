package searchprobe

import (
	"crypto/sha256"
	"encoding"
	"encoding/json"
	"hash"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// Board is a captured frame's end-of-frame board in the owned, compact form
// its readers use. A frame used to keep the whole projected view.View as its
// JSON encoding and every reader decoded the few facts it needed back out of
// it; a long game's history held tens of megabytes of that text and a search
// re-parsed it at every decision. Now the capture hashes the view's
// canonical encoding (canon.go, whose equality is the JSON encoding's
// equality) into Sum and Stripped, the digests every equality check
// compares, and copies the facts below -- all any reader decoded -- straight
// from the typed view before it is dropped.
//
//   - Step: the stack-rejection context's step label (Sample).
//   - Players, in view order: seat, public library and hand sizes (the
//     known-card tracker, the arrange epoch), and the object refs of the
//     displayed zones (the tracker, the redeal's public pool and pins),
//     each with the printed-material inputs StaticChoice ranks by.
//   - Stack, bottom first: each entry's ref, source and target objects (the
//     tracker, the redeal's referenced-object check).
//
// A hand-built Board (a unit test's) carries facts only; its digests are
// zero. A Board with no players is not a board at all (KnownCardTracker
// refuses it).
type Board struct {
	Step    string
	Players []BoardPlayer
	Stack   []BoardStack
	// Sum is the SHA-256 of the board's canonical encoding (canon.go): two
	// boards share it exactly when their JSON encodings are equal. Stripped
	// is the same digest of the board with every seat's potential_actions
	// cleared -- what a scratch capture that skips the projection sees -- and
	// equals Sum when the board carries none.
	Sum, Stripped [sha256.Size]byte
	// raw is the board's JSON encoding, kept only when the capturing
	// collector has retainJSON set (in-package tests that inspect it).
	raw []byte
}

// BoardPlayer is one seat's slice of a Board. Hand is nil for a seat whose
// hand the viewer does not see (the view's own null hand).
type BoardPlayer struct {
	Seat                                         state.PlayerID
	LibrarySize, HandSize                        int
	Hand, Battlefield, Graveyard, Exile, Command []BoardCard
}

// BoardCard is one displayed card: its observation ref and the inputs of the
// frozen material score (rollout.go material).
type BoardCard struct {
	ID uint32
	// PowerToughness is Power+Toughness in the view's int32 arithmetic.
	PowerToughness   int32
	Land, Creature bool
}

// BoardStack is one stack entry: its ref, its source's ref (0 for a spell)
// and each target's object ref (0 for a player target), in view order.
type BoardStack struct {
	ID, Source uint32
	Targets    []uint32
}

// material is rollout.go's frozen material score of c.
func (c BoardCard) material() float64 { return materialScore(c.Land, c.Creature, c.PowerToughness) }

// boardFacts copies the facts Board documents out of a remapped view. Every
// card and every target list shares one backing array each, sliced with a
// capacity limit, so a board is a handful of allocations however many cards
// it shows.
func boardFacts(v *view.View) Board {
	cards, targets := 0, 0
	for i := range v.Players {
		p := &v.Players[i]
		cards += len(p.Hand) + len(p.Battlefield) + len(p.Graveyard) + len(p.Exile) + len(p.Command)
	}
	for i := range v.Stack {
		targets += len(v.Stack[i].Targets)
	}
	cardBuf := make([]BoardCard, 0, cards)
	zone := func(src []view.CardView) []BoardCard {
		if src == nil {
			return nil
		}
		lo := len(cardBuf)
		for i := range src {
			c := &src[i]
			cardBuf = append(cardBuf, BoardCard{
				ID:             uint32(c.ID),
				PowerToughness: c.Power + c.Toughness,
				Land:           strings.Contains(c.Types, "Land"),
				Creature:       strings.Contains(c.Types, "Creature"),
			})
		}
		return cardBuf[lo:len(cardBuf):len(cardBuf)]
	}
	b := Board{Step: v.Step, Players: make([]BoardPlayer, len(v.Players))}
	for i := range v.Players {
		p := &v.Players[i]
		b.Players[i] = BoardPlayer{
			Seat: p.ID, LibrarySize: p.LibrarySize, HandSize: p.HandSize,
			Hand: zone(p.Hand), Battlefield: zone(p.Battlefield), Graveyard: zone(p.Graveyard),
			Exile: zone(p.Exile), Command: zone(p.Command),
		}
	}
	if len(v.Stack) > 0 {
		targetBuf := make([]uint32, 0, targets)
		b.Stack = make([]BoardStack, len(v.Stack))
		for i := range v.Stack {
			s := &v.Stack[i]
			var ts []uint32
			if len(s.Targets) > 0 {
				lo := len(targetBuf)
				for _, t := range s.Targets {
					targetBuf = append(targetBuf, uint32(t.Obj))
				}
				ts = targetBuf[lo:len(targetBuf):len(targetBuf)]
			}
			b.Stack[i] = BoardStack{ID: uint32(s.ID), Source: uint32(s.Source), Targets: ts}
		}
	}
	return b
}

// legacyFrame is a Frame as it was encoded while Frame.Board held the board's
// JSON. Sample's seeds are rooted in the SHA-256 of the whole History's JSON
// (historyDigest), so the history chain reproduces that encoding frame by
// frame, byte for byte, without keeping any of it.
type legacyFrame struct {
	Board      json.RawMessage
	Identities []Identity
	Events     []ObservedEvent
	Decision   *ObservedDecision
}

// historyLink is one frame's place in its collector's history chain: the
// SHA-256 state after hashing
//
//	{"Actor":<actor>,"Frames":[<frame 0>,<frame 1>,...,<this frame>
//
// -- the prefix of json.Marshal(History{Actor, Frames: <the chain so far>})
// as it was encoded with a JSON board. prev is the previous frame's link (nil
// for the collector's first capture). A link is immutable once made; a
// collector Clone shares it and extends it independently.
type historyLink struct {
	actor state.PlayerID
	prev  *historyLink
	state []byte
	// buf backs state: a SHA-256 marshalled state is 108 bytes, so the link
	// and its state are one allocation.
	buf [112]byte
}

// hashState is the SHA-256 hash the chain resumes and marshals; one per
// collector, reset per use.
type hashState interface {
	hash.Hash
	encoding.BinaryAppender
	encoding.BinaryUnmarshaler
}

// extendChain hashes one frame's legacy encoding onto the collector's chain
// and returns the new link.
func (c *Collector) extendChain(frameJSON []byte) (*historyLink, error) {
	if c.hasher == nil {
		c.hasher = sha256.New().(hashState)
	}
	h := c.hasher
	h.Reset()
	if c.chain == nil {
		var num [3]byte
		h.Write([]byte(`{"Actor":`))
		h.Write(strconv.AppendUint(num[:0], uint64(c.actor), 10))
		h.Write([]byte(`,"Frames":[`))
	} else {
		if err := h.UnmarshalBinary(c.chain.state); err != nil {
			return nil, err
		}
		h.Write([]byte{','})
	}
	h.Write(frameJSON)
	link := &historyLink{actor: c.actor, prev: c.chain}
	st, err := h.AppendBinary(link.buf[:0])
	if err != nil {
		return nil, err
	}
	link.state = st
	c.chain = link
	return link, nil
}

// HistoryDigest is the SHA-256 Sample roots its seeds in (historyDigest).
func HistoryDigest(h History) ([sha256.Size]byte, error) { return historyDigest(h) }

// historyDigest is sha256(json.Marshal(h)) as it was computed while
// Frame.Board held the board's JSON -- the root of every seed Sample draws.
// A history whose frames are one unbroken collector chain (a Feed's, an
// experiment's, a prefix or copy of either) finishes the last frame's hash
// state with the Answers; that is the same byte stream the old encoding
// hashed. A history assembled by hand (unit tests) has no chain and is
// hashed through its own current encoding instead.
func historyDigest(h History) ([sha256.Size]byte, error) {
	if st := chainState(h); st != nil {
		hs := sha256.New()
		if err := hs.(encoding.BinaryUnmarshaler).UnmarshalBinary(st); err != nil {
			return [sha256.Size]byte{}, err
		}
		answers, err := json.Marshal(h.Answers)
		if err != nil {
			return [sha256.Size]byte{}, err
		}
		hs.Write([]byte(`],"Answers":`))
		hs.Write(answers)
		hs.Write([]byte{'}'})
		var out [sha256.Size]byte
		hs.Sum(out[:0])
		return out, nil
	}
	encoded, err := json.Marshal(h)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(encoded), nil
}

// chainState returns the hash state of h's last frame when h.Frames is
// exactly one chain from its first link, captured for h.Actor; otherwise nil.
func chainState(h History) []byte {
	if len(h.Frames) == 0 {
		return nil
	}
	var prev *historyLink
	for _, f := range h.Frames {
		if f.link == nil || f.link.prev != prev || f.link.actor != h.Actor {
			return nil
		}
		prev = f.link
	}
	return prev.state
}
