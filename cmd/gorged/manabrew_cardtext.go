package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
)

// manabrewCardText implements internal/manabrew.CardText over the art
// cache's per-face facts sidecar (art.go: the six-field cardFacts record a
// card's Scryfall named response already writes for the web client's own
// /cards/named route). Wiring it to ManaBrew's Translator is MB-9/MB-10's
// job (the ManaBrew HTTP transport that builds a Translator per table);
// this type is the seam itself, exercised directly by the tests below.
//
// Q9's five rules (scoping spec Section 10.1) hold here:
//  1. Presentation only: nothing here touches events, the log, replay or a
//     bot input -- it is a read of a JSON sidecar on disk.
//  2. Keyed only by the name the caller passes (the redacted CardView's
//     name); a face-down card is never looked up because the translator
//     never calls Text with an empty name.
//  3. A miss is ("", false), and never blocks: a cold name queues a
//     background fill through the art cache's existing paced/limited fetch
//     path (fetchCard, via ensureText) and returns immediately either way.
//  4. No wall clock or network is read synchronously in Text itself.
//  5. The facts sidecar carries Oracle text (the same field the web
//     client's oracle.ts already reads), never Forge script text.
type manabrewCardText struct {
	ac *artCache
}

// newManabrewCardText builds the CardText seam over ac. ac may be nil (no
// art cache configured), in which case Text always misses.
func newManabrewCardText(ac *artCache) *manabrewCardText {
	return &manabrewCardText{ac: ac}
}

// Text implements internal/manabrew.CardText. name is exactly what the
// seat's redacted CardView shows (the face name for a DFC/split/adventure
// card); the caller never passes an empty name for a face-down or hidden
// card, but an empty name is treated as a miss here too, defensively.
//
// It reads only what is already on disk: a hit returns the sidecar's Oracle
// text; a miss returns ("", false) and, unless the name is already a known
// 404, queues a background fetch through the art cache's own singleflight
// and pacing (the same path the web client's /cards/named route uses) so a
// later call may hit. It never blocks on that fetch and never touches the
// network or a clock itself.
func (m *manabrewCardText) Text(name string) (string, bool) {
	if m == nil || m.ac == nil {
		return "", false
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", false
	}
	key := artKey(name)
	if facts, ok := m.ac.readFacts(key); ok {
		return facts.OracleText, true
	}
	if fileExists(m.ac.missPath(key)) {
		return "", false // a known 404; nothing to queue
	}
	m.ac.queueTextFill(name)
	return "", false
}

// readFacts reads and decodes key's facts sidecar, if present. Any read or
// decode failure is treated as absent rather than an error: the caller
// falls back to a miss, and a later fetch (background or client-driven)
// overwrites the sidecar atomically anyway.
func (a *artCache) readFacts(key string) (cardFacts, bool) {
	b, err := os.ReadFile(a.factsPath(key))
	if err != nil {
		return cardFacts{}, false
	}
	var f cardFacts
	if err := json.Unmarshal(b, &f); err != nil {
		return cardFacts{}, false
	}
	return f, true
}

// queueTextFill starts a detached background fetch of name's facts sidecar
// through the SAME single-flight, paced, retried path every other caller of
// ensureText uses (fetchCard, gated by a.sem and the shared pace limiter),
// so a burst of misses for the same cold name still costs one Scryfall
// request, not one per caller. It is fire-and-forget: the caller (Text)
// never waits on it, and its context is independent of any request context
// so a fetch a caller's original request has moved on from still finishes
// and settles the cache for whoever asks next.
func (a *artCache) queueTextFill(name string) {
	key := artKey(name)
	go func() {
		_, _ = a.ensureText(context.Background(), key, name)
	}()
}
