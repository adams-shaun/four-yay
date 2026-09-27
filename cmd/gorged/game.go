package main

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/adams-shaun/gorge/host"
	"github.com/adams-shaun/gorge/host/httpapi"
	"github.com/adams-shaun/gorge/view"
)

// createGame returns the Options.CreateGame closure gorged arms so the
// browser can seat a human against a bot on demand (Task ui11). It builds a
// fresh single-shot 2-seat table in the requested format — seat 0 human,
// seat 1 bot. Each seat may request a deck from that format's pool; an
// omitted choice is assigned from the same seeded random order as before.
//
// Seeding and replayability are the whole point of the "random" here: the
// game's seed is minted once, at create time, and it drives BOTH the deck
// order (host.SeededShuffle over the pool) and the match itself (via
// MatchSeed). The shuffled deck order is persisted verbatim in the
// TableConfig, and the seed is returned in the response and on the match's
// own wire records, so a viewer can replay the game from its configuration
// — the engine's deterministic-replay contract is preserved, not
// undercut, by a randomly chosen starting point.
//
// The closure is serialized: it allocates a table id (host.nextGameID,
// which observes the registry) and mints a token, and two concurrent
// requests must not hand each other the same id.
func (c config) createGame(r *host.Registry, gate *seatGate, cmdPool, conPool []string, vis view.Visibility) func(httpapi.CreateGameOptions) (httpapi.CreateGameResponse, error) {
	var mu sync.Mutex
	return func(req httpapi.CreateGameOptions) (httpapi.CreateGameResponse, error) {
		mu.Lock()
		defer mu.Unlock()
		policy, err := host.NormalizeBotPolicy(req.BotPolicy)
		if err != nil {
			return httpapi.CreateGameResponse{}, err
		}
		pool, otherPool := conPool, cmdPool
		otherFormat := host.FormatCommander
		if req.Format == host.FormatCommander {
			pool, otherPool = cmdPool, conPool
			otherFormat = host.FormatConstructed
		}
		if len(pool) == 0 {
			return httpapi.CreateGameResponse{}, fmt.Errorf("no %s deck is available to deal (decks dir supplies none)", req.Format)
		}
		for _, pick := range []struct {
			role string
			id   string
		}{{"human", req.HumanDeck}, {"bot", req.BotDeck}} {
			if pick.id == "" {
				continue
			}
			if containsDeck(pool, pick.id) {
				continue
			}
			if containsDeck(otherPool, pick.id) {
				return httpapi.CreateGameResponse{}, fmt.Errorf("%s deck %q belongs to %s, not requested format %s", pick.role, pick.id, otherFormat, req.Format)
			}
			return httpapi.CreateGameResponse{}, fmt.Errorf("unknown %s deck %q", pick.role, pick.id)
		}
		seed, err := randomSeed()
		if err != nil {
			return httpapi.CreateGameResponse{}, err
		}
		shuffled := host.SeededShuffle(seed, pool)
		decks := shuffled
		if req.HumanDeck != "" || req.BotDeck != "" {
			human, bot := req.HumanDeck, req.BotDeck
			if human == "" {
				human = randomOpponent(shuffled, bot)
			}
			if bot == "" {
				bot = randomOpponent(shuffled, human)
			}
			// Match 1 rotates TableConfig.Decks by one: with this two-item
			// order seat 0 receives human and seat 1 receives bot.
			decks = []string{bot, human}
		}
		id := host.NextGameID(r)
		tok, err := gate.mint(id, 0)
		if err != nil {
			return httpapi.CreateGameResponse{}, err
		}
		// The request may set its own London allowance (finding
		// fb-20260914T114629Z-6c81e4d6); an omitted field keeps the server
		// default. host.TableConfig.Validate still rejects a negative, so a
		// caller that skips the handler's 0..6 cap fails at AddTable.
		mulligans := c.mulligans
		if req.Mulligans != nil {
			mulligans = *req.Mulligans
		}
		cfg := host.TableConfig{
			ID: id, Name: fmt.Sprintf("Play vs bot (%s)", req.Format), Seats: 2, Decks: decks,
			Seed: seed, PlayerNames: []string{"You", "Bot"}, Mulligans: mulligans,
			Spectator: vis, Perpetual: false, Humans: []int{0}, Format: req.Format,
			BotPolicy: policy, BotAutoPayMana: c.botAutoPayMana, AutoMana: c.autoMana, OnDemand: true,
		}
		if err := r.AddTable(cfg); err != nil {
			return httpapi.CreateGameResponse{}, err
		}
		if err := r.Start(id); err != nil {
			return httpapi.CreateGameResponse{}, err
		}
		return httpapi.CreateGameResponse{Table: string(id), Match: 1, Seed: seed, Seat: 0,
			Token: tok, Join: fmt.Sprintf("/t/%s?seat=0&token=%s", id, tok), BotPolicy: policy}, nil
	}
}

func containsDeck(pool []string, id string) bool {
	for _, candidate := range pool {
		if candidate == id {
			return true
		}
	}
	return false
}

// randomOpponent returns the first shuffled deck different from avoid when
// one exists. A one-deck pool necessarily produces a mirror match, matching
// the host's existing modulo assignment rather than making random unusable.
func randomOpponent(shuffled []string, avoid string) string {
	for _, id := range shuffled {
		if id != avoid {
			return id
		}
	}
	return shuffled[0]
}

// randomSeed mints a fresh 64-bit game seed with crypto/rand, the one
// source of novelty in an otherwise fully deterministic game: everything
// after it is a pure function of the recorded configuration.
func randomSeed() (uint64, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(b[:]), nil
}
