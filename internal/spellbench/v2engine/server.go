package v2engine

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/deck"
	"github.com/adams-shaun/gorge/internal/spellbench"
	"github.com/adams-shaun/gorge/internal/spellbench/v2agent"
	"github.com/adams-shaun/gorge/state"
)

// Engine identity.
const (
	EngineName = "gorge-v2-reverse"
	Format     = "pauper-bo1"
)

// Options configures a Server.
type Options struct {
	Registry *cards.Registry
	// Mana is "manual" (every mana ability a priority candidate, the
	// default) or "autopay" (engine_autopay: planner-paid casts).
	Mana string
	// Version, SourceRevision and CardPool fill hello_ok.engine.
	Version        string
	SourceRevision string
	CardPool       string
	// Truth, when non-nil, receives the test-mode truth side channel
	// (truth.go). Never set in a rated run.
	Truth io.Writer
	// Log receives diagnostics; nil discards them.
	Log io.Writer
}

// Stats counts what the server did, by key (sorted when dumped).
type Stats struct {
	mu sync.Mutex
	m  map[string]int64
}

func (s *Stats) add(k string, n int64) {
	s.mu.Lock()
	if s.m == nil {
		s.m = map[string]int64{}
	}
	s.m[k] += n
	s.mu.Unlock()
}

// Snapshot returns a copy of the counters.
func (s *Stats) Snapshot() map[string]int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]int64, len(s.m))
	for k, v := range s.m {
		out[k] = v
	}
	return out
}

// Server is one engine process's state (spec 9): at most one active game.
type Server struct {
	opts    Options
	stats   *Stats
	catalog []catalogDeck
	game    *Game
	domain  map[string]bool // card_name_domain of the active game (lookup only)
	usedIDs map[string]bool // game ids seen (lookup only)
	// the retransmission cache (spec 4.1): payload hash per request id, and
	// the last response.
	reqHash  map[string]string
	reqOrder []string
	lastID   string
	lastResp []byte
	logLines int
	// seedOverride, when set (tests only), seeds gorge directly instead of
	// from the game secret, so a game can be replayed in-process.
	seedOverride *uint64
}

type catalogDeck struct {
	id    string
	rows  []map[string]any
	cards []*cards.Card
}

// New builds a server; the catalog is the pauper-kernel decks gorge serves.
func New(opts Options) (*Server, error) {
	if opts.Registry == nil {
		return nil, errors.New("v2engine: nil registry")
	}
	if opts.Mana == "" {
		opts.Mana = "manual"
	}
	if opts.Mana != "manual" && opts.Mana != "autopay" {
		return nil, fmt.Errorf("v2engine: mana %q (want manual or autopay)", opts.Mana)
	}
	if opts.Version == "" {
		opts.Version = "dev"
	}
	if opts.CardPool == "" {
		opts.CardPool = "gorge-forge-corpus"
	}
	s := &Server{opts: opts, stats: &Stats{}, usedIDs: map[string]bool{}, reqHash: map[string]string{}}
	ids, err := spellbench.CatalogIDs(spellbench.PauperKernel)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		f, err := spellbench.File(spellbench.PauperKernel, id)
		if err != nil {
			return nil, err
		}
		cs, err := f.Resolve(opts.Registry)
		if err != nil {
			return nil, err
		}
		cd := catalogDeck{id: f.Name, cards: cs}
		// Rows by Oracle full name ("A // B" for a multi-face card, spec
		// 4.4, 12.1), in the file's order, one row per distinct name.
		counts := map[string]int{} // lookup only
		var names []string
		for _, c := range cs {
			n := fullName(c)
			if counts[n] == 0 {
				names = append(names, n)
			}
			counts[n]++
		}
		for _, n := range names {
			cd.rows = append(cd.rows, map[string]any{"name": n, "count": counts[n]})
		}
		s.catalog = append(s.catalog, cd)
	}
	return s, nil
}

// Stats is the server's counters.
func (s *Server) Stats() *Stats { return s.stats }

func (s *Server) logf(format string, args ...any) {
	if s.opts.Log == nil || s.logLines > 2000 {
		return
	}
	s.logLines++
	fmt.Fprintf(s.opts.Log, "sbv2engine: "+format+"\n", args...)
}

func (s *Server) provenance() map[string]any {
	return map[string]any{"engine_name": EngineName, "engine_version": s.opts.Version,
		"rules_snapshot_id": "gorge/" + s.opts.Version, "card_pool_identity": s.opts.CardPool}
}

// Serve answers request lines until r reaches EOF.
func (s *Server) Serve(r io.Reader, w io.Writer) error {
	lr := v2agent.NewLineReader(r)
	bw := bufio.NewWriterSize(w, 256<<10)
	for {
		line, err := lr.ReadLine()
		var out []byte
		switch {
		case err == nil:
			out = s.HandleLine(line)
		case errors.Is(err, io.EOF):
			return nil
		case errors.Is(err, v2agent.ErrLineTooLong), errors.Is(err, v2agent.ErrUnterminated):
			out = errLine("", "malformed_json", err.Error())
		default:
			return err
		}
		if _, err := bw.Write(out); err != nil {
			return err
		}
		if err := bw.Flush(); err != nil {
			return err
		}
	}
}

type reqError struct{ code, msg string }

func (e *reqError) Error() string { return e.code + ": " + e.msg }

func fail(code, format string, args ...any) *reqError {
	return &reqError{code: code, msg: fmt.Sprintf(format, args...)}
}

// HandleLine answers one request line with one response line.
func (s *Server) HandleLine(line []byte) []byte {
	line = bytes.TrimRight(line, "\r\n")
	if !json.Valid(line) {
		s.stats.add("error:malformed_json", 1)
		return errLine("", "malformed_json", "line is not valid JSON")
	}
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.UseNumber()
	var top map[string]any
	if err := dec.Decode(&top); err != nil || top == nil {
		s.stats.add("error:malformed_request", 1)
		return errLine("", "malformed_request", "top-level value is not an object")
	}
	rid, ok := top["request_id"].(string)
	if !ok || rid == "" {
		s.stats.add("error:malformed_request", 1)
		return errLine("", "malformed_request", "request_id must be a nonempty string")
	}
	sum := sha256.Sum256(line)
	h := hex.EncodeToString(sum[:])
	if prev, seen := s.reqHash[rid]; seen {
		if prev != h {
			s.stats.add("error:request_id_reuse_mismatch", 1)
			return errLine(rid, "request_id_reuse_mismatch", "request_id reused with a different payload")
		}
		if rid == s.lastID && s.lastResp != nil {
			s.stats.add("retransmission", 1)
			return s.lastResp
		}
	}
	payload, rerr := s.handle(rid, top)
	var out []byte
	if rerr != nil {
		s.stats.add("error:"+rerr.code, 1)
		s.logf("request %s: %s: %s", rid, rerr.code, rerr.msg)
		// requests that fail parsing are never cached (spec 4.1)
		return errLine(rid, rerr.code, rerr.msg)
	}
	payload["protocol"] = v2agent.Protocol
	payload["request_id"] = rid
	out, err := v2agent.CanonicalLine(payload)
	if err != nil {
		s.stats.add("error:internal_encoding", 1)
		return errLine(rid, "malformed_request", "response encoding failed")
	}
	s.reqHash[rid] = h
	s.reqOrder = append(s.reqOrder, rid)
	if len(s.reqOrder) > 8192 {
		delete(s.reqHash, s.reqOrder[0])
		s.reqOrder = s.reqOrder[1:]
	}
	s.lastID, s.lastResp = rid, out
	return out
}

func errLine(rid, code, msg string) []byte {
	msg = strings.Join(strings.Fields(msg), " ")
	if len(msg) > 240 {
		msg = msg[:240]
	}
	out, _ := v2agent.CanonicalLine(map[string]any{"response_type": "error", "protocol": v2agent.Protocol,
		"request_id": rid, "error": map[string]any{"code": code, "message": msg}})
	return out
}

func exactKeys(m map[string]any, keys ...string) *reqError {
	if len(m) != len(keys) {
		for k := range m {
			if !contains(keys, k) {
				return fail("malformed_request", "unknown field %q", k)
			}
		}
		for _, k := range keys {
			if _, ok := m[k]; !ok {
				return fail("malformed_request", "missing field %q", k)
			}
		}
	}
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			return fail("malformed_request", "missing field %q", k)
		}
	}
	return nil
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

func asInt(v any) (int64, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	i, err := strconv.ParseInt(string(n), 10, 64)
	return i, err == nil
}

func (s *Server) handle(rid string, top map[string]any) (map[string]any, *reqError) {
	proto, ok := top["protocol"].(string)
	if !ok {
		return nil, fail("malformed_request", "protocol must be a string")
	}
	if proto != v2agent.Protocol {
		return nil, fail("protocol_mismatch", "protocol must be %q", v2agent.Protocol)
	}
	rt, _ := top["request_type"].(string)
	switch rt {
	case "hello":
		if e := exactKeys(top, "request_type", "protocol", "request_id", "protocol_minor"); e != nil {
			return nil, e
		}
		return s.hello(), nil
	case "reset":
		return s.reset(top)
	case "step":
		return s.step(top)
	case "validate_deck":
		if e := exactKeys(top, "request_type", "protocol", "request_id", "format", "deck"); e != nil {
			return nil, e
		}
		if f, _ := top["format"].(string); f != Format {
			return nil, fail("unsupported_format", "format %q", top["format"])
		}
		if _, err := s.resolveDeck(top["deck"], false); err != nil {
			return nil, err
		}
		return map[string]any{"response_type": "deck_ok"}, nil
	case "probe_resample":
		if s.game == nil {
			return nil, fail("step_before_reset", "no game")
		}
		return nil, fail("unsupported_request", "the noninterference probe is not implemented")
	}
	return nil, fail("malformed_request", "unknown request_type %q", rt)
}

var observationFlags = []string{"poison", "player_counters", "designations", "player_progress", "day_night",
	"passed_seats", "pending_triggers", "keywords", "full_name", "exiled_by", "stack_text", "permanent_details", "known_cards"}

// DecisionKinds are the v2.0 kinds this engine may emit.
func (s *Server) DecisionKinds() []string {
	k := []string{"pass", "play_land", "cast_spell"}
	if s.opts.Mana == "manual" {
		k = append(k, "activate_mana_ability")
	}
	return append(k, "activate_ability", "special_action", "choose_target", "finish_target_selection",
		"choose_cost_target", "choose_spell_mode", "choose_option", "choose_color", "choose_number",
		"choose_boolean", "choose_name", "select_object", "finish_selection", "optional_cost",
		"order_pick", "arrange_card", "declare_attack", "declare_block")
}

func (s *Server) hello() map[string]any {
	obsFlags := map[string]any{}
	for _, f := range observationFlags {
		obsFlags[f] = f == "keywords" || f == "full_name"
	}
	var catalog []any
	for _, c := range s.catalog {
		rows := make([]any, len(c.rows))
		for i, r := range c.rows {
			rows[i] = r
		}
		catalog = append(catalog, map[string]any{"catalog_id": c.id, "name": c.id, "decklist": rows})
	}
	var rev any
	if s.opts.SourceRevision != "" {
		rev = s.opts.SourceRevision
	}
	var mana any
	if s.opts.Mana == "autopay" {
		mana = "engine_autopay"
	}
	return map[string]any{
		"response_type":  "hello_ok",
		"protocol_minor": 0,
		"engine": map[string]any{"name": EngineName, "version": s.opts.Version, "source_revision": rev,
			"rules_snapshot_id": "gorge/" + s.opts.Version, "card_pool_identity": s.opts.CardPool},
		"formats":         []any{Format},
		"deck_sources":    []any{"catalog", "decklist"},
		"catalog":         catalog,
		"rules_supported": map[string]any{"mulligan": []any{"none"}, "starting_player": []any{"host_assigned"}},
		"observation":     obsFlags,
		"decision_kinds":  s.DecisionKinds(),
		"engine_defaults": map[string]any{"trigger_order": nil, "replacement_order": "engine_order",
			"combat_damage_assignment": "engine_order", "mana_payment": mana},
		"rewind":     false,
		"fairness":   map[string]any{"noninterference_probe": false},
		"extensions": []any{},
	}
}

// resolveDeck reads {deck_id, catalog_id} or {deck_id, decklist} (withID)
// or the validate_deck shape {catalog_id} / {decklist}.
func (s *Server) resolveDeck(v any, withID bool) ([]*cards.Card, *reqError) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fail("malformed_request", "deck must be an object")
	}
	if withID {
		if _, ok := m["deck_id"].(string); !ok || len(m) != 2 {
			return nil, fail("malformed_request", "deck must be {deck_id, catalog_id} or {deck_id, decklist}")
		}
	} else if len(m) != 1 {
		return nil, fail("malformed_request", "deck must be {catalog_id} or {decklist}")
	}
	if cid, ok := m["catalog_id"].(string); ok {
		for _, c := range s.catalog {
			if c.id == cid {
				return c.cards, nil
			}
		}
		return nil, fail("unsupported_deck", "unknown catalog_id %q", cid)
	}
	rows, ok := m["decklist"].([]any)
	if !ok {
		return nil, fail("malformed_request", "deck needs catalog_id or decklist")
	}
	var entries []deck.Entry
	for _, r := range rows {
		rm, ok := r.(map[string]any)
		if !ok {
			return nil, fail("malformed_request", "decklist row must be an object")
		}
		name, _ := rm["name"].(string)
		n, ok := asInt(rm["count"])
		if name == "" || !ok || n < 1 || len(rm) != 2 {
			return nil, fail("malformed_request", "decklist row must be {name, count}")
		}
		if _, ok := s.opts.Registry.Lookup(name); !ok {
			if front, _, multi := strings.Cut(name, " // "); multi {
				name = front // the registry keys a multi-face card by its front face
			}
		}
		entries = append(entries, deck.Entry{Name: name, Count: int(n)})
	}
	cs, err := deck.File{Name: "decklist", Cards: entries}.Resolve(s.opts.Registry)
	if err != nil {
		return nil, fail("unsupported_deck", "%v", err)
	}
	return cs, nil
}

func (s *Server) reset(top map[string]any) (map[string]any, *reqError) {
	if e := exactKeys(top, "request_type", "protocol", "request_id", "game_id", "format", "seats", "rules",
		"game_secret", "max_decisions", "max_steps"); e != nil {
		return nil, e
	}
	gid, ok := top["game_id"].(string)
	if !ok || gid == "" {
		return nil, fail("malformed_request", "game_id must be a nonempty string")
	}
	if s.usedIDs[gid] {
		return nil, fail("malformed_request", "game_id %q was already used", gid)
	}
	if s.game != nil && !s.game.over {
		return nil, fail("game_already_active", "a game is active")
	}
	if f, _ := top["format"].(string); f != Format {
		return nil, fail("unsupported_format", "format %q", top["format"])
	}
	seatsV, ok := top["seats"].([]any)
	if !ok || len(seatsV) != 2 {
		return nil, fail("malformed_request", "seats must list p0 then p1")
	}
	var decks [2][]*cards.Card
	for i, sv := range seatsV {
		sm, ok := sv.(map[string]any)
		if !ok || len(sm) != 2 || sm["seat"] != seats[i] {
			return nil, fail("malformed_request", "seats must list p0 then p1")
		}
		d, err := s.resolveDeck(sm["deck"], true)
		if err != nil {
			return nil, err
		}
		decks[i] = d
	}
	rules, ok := top["rules"].(map[string]any)
	if !ok {
		return nil, fail("malformed_request", "rules must be an object")
	}
	if e := exactKeys(rules, "opponent_decklist", "mulligan", "starting_player", "starting_seat", "card_name_domain",
		"extensions", "probe"); e != nil {
		return nil, e
	}
	if rules["mulligan"] != "none" {
		return nil, fail("unsupported_rule", "mulligan %v (supported: none)", rules["mulligan"])
	}
	if rules["starting_player"] != "host_assigned" {
		return nil, fail("unsupported_rule", "starting_player %v (supported: host_assigned)", rules["starting_player"])
	}
	start := state.PlayerID(0)
	switch rules["starting_seat"] {
	case "p0":
	case "p1":
		start = 1
	default:
		return nil, fail("malformed_request", "starting_seat must be p0 or p1 under host_assigned")
	}
	if ext, ok := rules["extensions"].([]any); !ok || len(ext) != 0 {
		return nil, fail("unsupported_rule", "no extensions are supported")
	}
	if rules["probe"] != false {
		return nil, fail("unsupported_rule", "the probe is not supported")
	}
	domain := map[string]bool{}
	if cnd, ok := rules["card_name_domain"].(map[string]any); ok {
		if names, ok := cnd["names"].([]any); ok {
			for _, n := range names {
				if ns, ok := n.(string); ok {
					domain[ns] = true
				}
			}
		}
	}
	secretHex, _ := top["game_secret"].(string)
	secret, err := hex.DecodeString(secretHex)
	if err != nil || len(secret) != 32 || strings.ToLower(secretHex) != secretHex {
		return nil, fail("malformed_request", "game_secret must be 64 lowercase hex characters")
	}
	maxD, ok1 := asInt(top["max_decisions"])
	maxS, ok2 := asInt(top["max_steps"])
	if !ok1 || !ok2 || maxD < 1 || maxS < 1 {
		return nil, fail("malformed_request", "max_decisions and max_steps must be positive integers")
	}
	s.usedIDs[gid] = true
	s.domain = domain
	s.stats.add("games", 1)
	s.game = newGame(s, gid, secret, decks, start, maxD, maxS)
	return s.game.advance(), nil
}

func (g *Game) inDomain(name string) bool { return g.srv.domain[name] }

func (s *Server) step(top map[string]any) (map[string]any, *reqError) {
	if e := exactKeys(top, "request_type", "protocol", "request_id", "game_id", "expected_step", "selection"); e != nil {
		return nil, e
	}
	g := s.game
	if g == nil {
		return nil, fail("step_before_reset", "no game has been reset")
	}
	if gid, _ := top["game_id"].(string); gid != g.id {
		return nil, fail("game_id_mismatch", "game_id names another game")
	}
	if g.over {
		return nil, fail("game_already_terminal", "the game is over")
	}
	exp, ok := asInt(top["expected_step"])
	if !ok {
		return nil, fail("malformed_request", "expected_step must be an integer")
	}
	sel, ok := top["selection"].(map[string]any)
	if !ok {
		return nil, fail("malformed_request", "selection must be an object")
	}
	if e := exactKeys(sel, "candidate_id", "semantic_echo"); e != nil {
		return nil, e
	}
	cid, ok := asInt(sel["candidate_id"])
	if !ok {
		return nil, fail("malformed_request", "candidate_id must be an integer")
	}
	if exp != g.step || g.posed == nil {
		return nil, fail("expected_step_mismatch", "pending step is %d", g.step)
	}
	if cid < 0 || int(cid) >= len(g.posed.cands) {
		return nil, fail("candidate_id_out_of_range", "candidate_id %d of %d", cid, len(g.posed.cands))
	}
	echo, err := v2agent.Canonical(sel["semantic_echo"])
	if err != nil || !bytes.Equal(echo, g.posed.semantics[cid]) {
		return nil, fail("semantic_echo_mismatch", "semantic_echo differs from candidate %d", cid)
	}
	return g.answer(int(cid)), nil
}

// DumpStats writes the counters sorted by key.
func (s *Server) DumpStats(w io.Writer) {
	m := s.stats.Snapshot()
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(w, "%s %d\n", k, m[k])
	}
}
