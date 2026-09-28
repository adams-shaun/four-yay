package v2engine

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strconv"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/spellbench/v2agent"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// Game is one v2 game over one gorge engine.
type Game struct {
	srv    *Server
	id     string
	secret []byte
	idKey  []byte
	e      *rules.Engine
	// decklists by seat, as names with counts (the truth side channel).
	decks [2]map[string]int

	maxDecisions, maxSteps int64

	step      int64 // binding step of the pending wire decision
	answered  int64 // answered wire decisions (step_count)
	completed int64 // completed groups (decision_count)
	seatStep  [2]int64
	nextGroup [2]int64
	curGroup  [2]int64 // group id of the seat's partial group, -1 when none

	zc       map[state.ObjID]uint32     // zone changes seen per gorge object
	lastZone map[state.ObjID]state.Zone // zone at the last sync
	evScan   int
	looks    map[lookKey]uint32
	idCache  map[string]string

	tr      translator
	trSeq   uint64
	trValid bool
	posed   *posedDecision
	// lastAction is each seat's last non-pass priority option's object: the
	// source a following cast-time choice (target, cost) belongs to when
	// gorge's Decision.Source is 0 (an ability mid-activation).
	lastAction [2]state.ObjID

	terminal map[string]any
	over     bool
	// submits counts the intents handed to gorge (tests compare it with an
	// in-process game's intent count).
	submits int
}

type lookKey struct {
	viewer state.PlayerID
	obj    state.ObjID
}

// posedDecision is the pending wire decision.
type posedDecision struct {
	seat       state.PlayerID
	cands      []cand
	semantics  [][]byte // canonical semantic per candidate
	substep    int
	count      int
	groupID    int64
	priority   bool
	gorgeKind  decision.Kind
	translator string
	obsBuild   *obsBuild
}

func newGame(srv *Server, gameID string, secret []byte, decks [2][]*cards.Card, startSeat state.PlayerID, maxDecisions, maxSteps int64) *Game {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("spellbench/v2/object-id"))
	idKey := mac.Sum(nil)
	g := &Game{srv: srv, id: gameID, secret: secret, idKey: idKey, maxDecisions: maxDecisions, maxSteps: maxSteps,
		zc: map[state.ObjID]uint32{}, lastZone: map[state.ObjID]state.Zone{}, looks: map[lookKey]uint32{},
		idCache: map[string]string{}, curGroup: [2]int64{-1, -1}}
	for s := 0; s < 2; s++ {
		g.decks[s] = map[string]int{}
		for _, c := range decks[s] {
			g.decks[s][c.Faces[0].Name]++
		}
	}
	seed := g.rngSeed("shared", "gorge-seed")
	if srv.seedOverride != nil {
		seed = *srv.seedOverride
	}
	cfg := rules.Config{Seed: seed, Names: []string{"p0", "p1"},
		Decks: [][]*cards.Card{decks[0], decks[1]}, Tokens: srv.opts.Registry.Tokens, NameUniverse: srv.opts.Registry.Cards}
	g.e = rules.NewStartingPlayerChoice(cfg)
	// host_assigned (spec 7.6): the engine answers CR 103.1's choice itself.
	if d := g.e.AskStartingPlayer(); d != nil {
		for _, o := range d.Options {
			if o.Player == startSeat {
				_ = g.e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}})
				break
			}
		}
	}
	g.e.Advance()
	return g
}

// rngSeed is spec 11.6's recommended stream seed, read as a uint64. gorge
// draws every random event from ONE stream (rules.Config.Seed), so the
// per-seat stream separation spec 11.6 asks for is not available: a
// documented deviation.
func (g *Game) rngSeed(who, purpose string) uint64 {
	mac := hmac.New(sha256.New, g.secret)
	mac.Write([]byte("spellbench/v2/rng:" + who + ":" + purpose + ":0"))
	return binary.BigEndian.Uint64(mac.Sum(nil)[:8])
}

// objectID is spec 5.3's recommended construction for viewer and gorge
// object id; suffix "" for a visible object, "look:<n>" for a hidden one.
func (g *Game) objectID(viewer state.PlayerID, id state.ObjID, suffix string) string {
	m := seatOf(viewer) + ":" + strconv.FormatUint(uint64(id), 10) + ":z" + strconv.FormatUint(uint64(g.zc[id]), 10)
	if suffix != "" {
		m += ":" + suffix
	}
	if v, ok := g.idCache[m]; ok {
		return v
	}
	mac := hmac.New(sha256.New, g.idKey)
	mac.Write([]byte(m))
	v := "o-" + hex.EncodeToString(mac.Sum(nil)[:8])
	g.idCache[m] = v
	return v
}

func (g *Game) lookN(viewer state.PlayerID, id state.ObjID) uint32 {
	return g.looks[lookKey{viewer, id}]
}

// syncZones advances the per-object zone-change counts from the event log
// (and, as a guard, from any zone that moved without a logged move).
func (g *Game) syncZones() {
	evs := g.e.L.Events
	bumped := map[state.ObjID]bool{} // lookup only
	for ; g.evScan < len(evs); g.evScan++ {
		ev := &evs[g.evScan]
		switch ev.Kind {
		case events.MoveZone, events.Draw, events.PutOnStack:
			if ev.Obj != 0 {
				g.zc[ev.Obj]++
				bumped[ev.Obj] = true
			}
		}
	}
	gs := g.e.G
	for i := range gs.Objs {
		o := &gs.Objs[i]
		if lz, ok := g.lastZone[o.ID]; ok && lz != o.Zone && !bumped[o.ID] {
			g.zc[o.ID]++
		}
		g.lastZone[o.ID] = o.Zone
	}
}

// advance runs until a wire decision is posed or the game ends; it returns
// the response payload (a decision or a terminal, without the envelope).
func (g *Game) advance() map[string]any {
	for {
		if g.over {
			return g.terminal
		}
		gs := g.e.G
		if gs.Over {
			return g.natural()
		}
		d := g.e.Pending()
		if d == nil {
			return g.halt("engine_contract_failure:no_pending_decision")
		}
		if !g.trValid || g.trSeq != d.Seq {
			if d.Kind == decision.KPriority {
				g.lastAction[d.Player] = 0
			}
			g.syncZones()
			g.tr = g.newTranslator(d)
			g.trSeq, g.trValid = d.Seq, true
			for _, lk := range g.tr.looks() {
				g.looks[lookKey{d.Player, lk.obj}]++
			}
		}
		p, err := g.pose(d)
		if err != nil {
			return g.halt("engine_contract_failure:" + err.Error())
		}
		if p != nil {
			g.posed = p
			return g.decisionPayload(p, d)
		}
		in, err := g.tr.intent()
		if err != nil {
			return g.halt("engine_contract_failure:" + err.Error())
		}
		g.trValid = false
		if err := g.submit(d, in); err != nil {
			return g.halt("engine_contract_failure:refused")
		}
	}
}

// submit hands an assembled intent to gorge. A refusal is retried with the
// repaired minimal answer (counted); a second refusal halts.
func (g *Game) submit(d *decision.Decision, in decision.Intent) (err error) {
	defer func() {
		if r := recover(); r != nil {
			g.srv.stats.add("engine_panic", 1)
			g.srv.logf("gorge panic at seq %d: %v", d.Seq, r)
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	in.Seq, in.Player = d.Seq, d.Player
	if in.Payment == nil && d.Kind == decision.KPriority && len(in.Choices) == 1 {
		if o := optionByIndex(d, in.Choices[0]); o != nil && o.Kind != "pass" {
			g.lastAction[d.Player] = o.Obj
		}
	}
	if in.Payment != nil {
		g.lastAction[d.Player] = 0
	}
	g.submits++
	if err := g.e.Submit(in); err != nil {
		g.srv.stats.add("gorge_refused", 1)
		g.srv.logf("gorge refused %s answer %v: %v", d.Kind, in.Choices, err)
		fb := repairedMinimal(d)
		if err2 := g.e.Submit(fb); err2 != nil {
			g.srv.logf("gorge refused the repaired answer too: %v", err2)
			return err2
		}
		g.srv.stats.add("gorge_refused_repaired", 1)
	}
	return nil
}

func optionByIndex(d *decision.Decision, i int) *decision.Option {
	for k := range d.Options {
		if d.Options[k].Index == i {
			return &d.Options[k]
		}
	}
	return nil
}

// pose asks the translator for its next wire decision (nil when the gorge
// decision is fully answered).
func (g *Game) pose(d *decision.Decision) (*posedDecision, error) {
	b := g.buildObservation(d.Player, d.Kind == decision.KPriority, g.tr.looks())
	sub, count, ok := g.tr.progress()
	if !ok {
		return nil, nil
	}
	cands, err := g.tr.candidates(b)
	if err != nil {
		return nil, err
	}
	if len(cands) == 0 {
		return nil, fmt.Errorf("no_candidates:%s", d.Kind)
	}
	if len(cands) > 4096 {
		return nil, fmt.Errorf("candidate_limit")
	}
	orderHidden(cands, b)
	p := &posedDecision{seat: d.Player, cands: cands, substep: sub, count: count,
		priority: g.tr.contextKind() == "priority", gorgeKind: d.Kind, translator: g.tr.name()}
	seen := map[string]int{} // lookup only
	for i := range cands {
		raw, err := v2agent.Canonical(cands[i].sem)
		if err != nil {
			return nil, fmt.Errorf("semantic_encoding")
		}
		if j, dup := seen[string(raw)]; dup {
			g.srv.logf("duplicate semantic %s (candidates %d and %d, %s)", raw, j, i, d.Kind)
			return nil, fmt.Errorf("duplicate_semantics")
		}
		seen[string(raw)] = i
		p.semantics = append(p.semantics, raw)
	}
	seat := int(d.Player)
	if sub == 0 {
		p.groupID = g.nextGroup[seat]
		g.curGroup[seat] = p.groupID
	} else {
		p.groupID = g.curGroup[seat]
	}
	p.obsBuild = b
	return p, nil
}

// decisionPayload is the decision response (spec 9.3) past the envelope.
func (g *Game) decisionPayload(p *posedDecision, d *decision.Decision) map[string]any {
	seat := int(p.seat)
	ctx := map[string]any{"kind": g.tr.contextKind(), "source": nil, "purpose": nil, "text": nil, "rewind": false}
	if src := g.tr.contextSource(p.obsBuild); src != nil {
		ctx["source"] = *src
	}
	if pur := g.tr.contextPurpose(); pur != "" {
		ctx["purpose"] = pur
	}
	cands := make([]any, len(p.cands))
	for i, c := range p.cands {
		var disp any
		if c.display != "" {
			disp = c.display
		}
		cands[i] = map[string]any{"candidate_id": i, "semantic": c.sem, "display_text": disp}
	}
	sd := map[string]any{
		"acting_seat": seatOf(p.seat),
		"seat_step":   g.seatStep[seat],
		"group":       map[string]any{"group_id": p.groupID, "substep_index": p.substep, "substep_count": p.count},
		"context":     ctx,
		"observation": p.obsBuild.obs,
		"candidates":  cands,
		"extensions":  map[string]any{},
	}
	g.srv.stats.add("posed", 1)
	g.srv.stats.add("posed:"+p.translator, 1)
	if g.srv.opts.Truth != nil {
		g.writeTruth(p, d)
	}
	return map[string]any{"response_type": "decision", "game_id": g.id, "step": g.step, "seat_decision": sd,
		"provenance": g.srv.provenance()}
}

// answer applies the candidate the host selected for the posed decision.
func (g *Game) answer(candidateID int) map[string]any {
	p := g.posed
	g.posed = nil
	seat := int(p.seat)
	g.seatStep[seat]++
	g.answered++
	g.step++
	if p.substep+1 == p.count {
		g.completed++
		g.nextGroup[seat] = p.groupID + 1
		g.curGroup[seat] = -1
	}
	if err := g.tr.answer(p.cands[candidateID].act); err != nil {
		return g.halt("engine_contract_failure:" + err.Error())
	}
	// Caps (spec 9.2): an answer that completes the gorge decision is
	// applied first, so a game it ends naturally is not truncated.
	if _, _, more := g.tr.progress(); !more {
		d := g.e.Pending()
		in, err := g.tr.intent()
		if err != nil {
			return g.halt("engine_contract_failure:" + err.Error())
		}
		g.trValid = false
		if err := g.submit(d, in); err != nil {
			return g.halt("engine_contract_failure:refused")
		}
		if g.e.G.Over {
			return g.natural()
		}
	}
	if g.answered >= g.maxSteps {
		return g.end("truncated", "truncated", nil, "max_steps")
	}
	if g.completed >= g.maxDecisions {
		return g.end("truncated", "truncated", nil, "max_decisions")
	}
	return g.advance()
}

func (g *Game) natural() map[string]any {
	gs := g.e.G
	if gs.Draw || !gs.Over {
		return g.end("draw", "natural", nil, "draw")
	}
	w := seatOf(gs.Winner)
	loser := 1 - int(gs.Winner)
	reason := seats[loser] + "_lost"
	if gs.Players[loser].Life <= 0 {
		reason = seats[loser] + "_life_zero"
	} else if len(g.cards(state.ZLibrary, state.PlayerID(loser))) == 0 {
		reason = seats[loser] + "_drew_from_empty_library"
	}
	return g.end(w+"_win", "natural", &w, reason)
}

func (g *Game) halt(reason string) map[string]any {
	g.srv.stats.add("halted", 1)
	g.srv.stats.add("halt:"+reason, 1)
	g.srv.logf("game %s halted: %s", g.id, reason)
	return g.end("halted", "halted", nil, reason)
}

func (g *Game) end(outcome, classification string, winner *string, reason string) map[string]any {
	var w any
	if winner != nil {
		w = *winner
	}
	g.over = true
	g.srv.stats.add("terminal:"+classification, 1)
	g.terminal = map[string]any{"response_type": "terminal", "game_id": g.id, "outcome": outcome,
		"classification": classification, "winner": w, "reason": reason, "step_count": g.answered,
		"decision_count": g.completed, "provenance": g.srv.provenance()}
	return g.terminal
}
