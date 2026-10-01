package view

import (
	"github.com/adams-shaun/gorge/deck"
	"github.com/adams-shaun/gorge/state"
)

// ownDeckSharer is the optional read-only manifest capability
// (rules.Engine.OwnDeckShared): a pointer to the engine's own immutable
// genesis manifest, which must never be written through.
type ownDeckSharer interface {
	OwnDeckShared(state.PlayerID) *deck.Manifest
}

// ownDeckInto is View.OwnDeck for viewer: an independently-owned copy of
// the viewer's genesis manifest, exactly what Chars.OwnDeck's Clone
// returns. prev is the View's previous OwnDeck (the destination's own
// storage from its last projection, or nil): when ch offers the shared
// manifest, the copy is made into prev's storage, so a View refilled per
// decision copies its manifest without allocating. The engine's manifest
// is never published itself -- a View escapes to clients, JSON and seats
// -- only copied. A Chars without the capability (a wrapper, a test
// double) keeps its own OwnDeck.
func ownDeckInto(prev *deck.Manifest, ch Chars, viewer state.PlayerID) *deck.Manifest {
	s, ok := ch.(ownDeckSharer)
	if !ok {
		return ch.OwnDeck(viewer)
	}
	src := s.OwnDeckShared(viewer)
	if src == nil {
		return nil
	}
	if prev == nil {
		prev = new(deck.Manifest)
	}
	copyManifestInto(prev, src)
	return prev
}

// copyManifestInto makes *dst equal to src.Clone(), reusing dst's slices.
// Clone's shape is kept exactly: Main is never nil, and Sideboard,
// Commanders and Curve are nil when empty.
func copyManifestInto(dst, src *deck.Manifest) {
	main, side, cmd, curve := dst.Main[:0], dst.Sideboard[:0], dst.Commanders[:0], dst.Curve[:0]
	*dst = *src
	dst.Main = append(main, src.Main...)
	if dst.Main == nil {
		dst.Main = []deck.ManifestRow{}
	}
	dst.Sideboard, dst.Commanders, dst.Curve = nil, nil, nil
	if len(src.Sideboard) > 0 {
		dst.Sideboard = append(side, src.Sideboard...)
	}
	if len(src.Commanders) > 0 {
		dst.Commanders = append(cmd, src.Commanders...)
	}
	if len(src.Curve) > 0 {
		dst.Curve = append(curve, src.Curve...)
	}
}
