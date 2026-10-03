package mzplay

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// game.yml is the file draft-zero's loop writes for every engine launch
// (loop.game_yml, yaml.safe_dump of its base configs/game.yml with the
// per-job keys set) and MageZero's Config.java reads. This reader takes
// exactly that shape: block mappings nested by indentation, one "key: value"
// or "key:" per line, plain or quoted scalars, comments. There are no
// sequences, flow collections, anchors, tags or multi-line scalars in the
// file, and each of those is refused by name rather than guessed at, so a
// change in the loop's output fails loudly here instead of running a game
// with a half-read configuration.
//
// A hand-written reader instead of a YAML library because the module takes
// no third-party dependency, and instead of a Python yml-to-JSON wrapper
// because the shim would then need a Python interpreter merely to start.

// ymlNode is a block mapping: key -> *ymlNode or ymlScalar.
type ymlNode struct {
	keys []string
	vals map[string]any
}

type ymlScalar struct {
	text   string
	quoted bool
	line   int
}

func parseYML(src string) (*ymlNode, error) {
	type frame struct {
		indent int
		node   *ymlNode
	}
	root := &ymlNode{vals: map[string]any{}}
	stack := []frame{{indent: -1, node: root}}
	// pending is the "key:" line whose block has not started yet.
	var pending *struct {
		parent *ymlNode
		key    string
		indent int
		line   int
	}
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	for ln, raw := range lines {
		lineNo := ln + 1
		if lead := raw[:len(raw)-len(strings.TrimLeft(raw, " \t"))]; strings.ContainsRune(lead, '\t') {
			return nil, fmt.Errorf("game.yml line %d: tab indentation", lineNo)
		}
		body := stripComment(raw)
		if strings.TrimSpace(body) == "" {
			continue
		}
		indent := len(body) - len(strings.TrimLeft(body, " "))
		text := strings.TrimSpace(body)
		switch {
		case text == "---":
			continue
		case strings.HasPrefix(text, "- ") || text == "-":
			return nil, fmt.Errorf("game.yml line %d: a sequence (\"- \"), which the game config never holds", lineNo)
		case strings.HasPrefix(text, "{") || strings.HasPrefix(text, "["):
			return nil, fmt.Errorf("game.yml line %d: a flow collection", lineNo)
		}
		key, val, ok := splitKey(text)
		if !ok {
			return nil, fmt.Errorf("game.yml line %d: %q is not \"key: value\" (a multi-line scalar is not supported)", lineNo, text)
		}
		if pending != nil {
			if indent > pending.indent {
				child := &ymlNode{vals: map[string]any{}}
				pending.parent.set(pending.key, child)
				stack = append(stack, frame{indent: indent, node: child})
			} else {
				// "key:" with nothing under it is a null scalar.
				pending.parent.set(pending.key, ymlScalar{text: "", line: pending.line})
			}
			pending = nil
		}
		for len(stack) > 1 && indent < stack[len(stack)-1].indent {
			stack = stack[:len(stack)-1]
		}
		top := stack[len(stack)-1]
		if len(stack) == 1 {
			if indent != 0 && top.indent == -1 && len(root.keys) > 0 {
				return nil, fmt.Errorf("game.yml line %d: unexpected indentation", lineNo)
			}
		} else if indent != top.indent {
			return nil, fmt.Errorf("game.yml line %d: indentation %d matches no open block", lineNo, indent)
		}
		if _, dup := top.node.vals[key]; dup {
			return nil, fmt.Errorf("game.yml line %d: duplicate key %q", lineNo, key)
		}
		if val == "" {
			pending = &struct {
				parent *ymlNode
				key    string
				indent int
				line   int
			}{top.node, key, indent, lineNo}
			continue
		}
		sc, err := parseScalar(val, lineNo)
		if err != nil {
			return nil, err
		}
		top.node.set(key, sc)
	}
	if pending != nil {
		pending.parent.set(pending.key, ymlScalar{text: "", line: pending.line})
	}
	return root, nil
}

func (n *ymlNode) set(key string, v any) {
	if _, ok := n.vals[key]; !ok {
		n.keys = append(n.keys, key)
	}
	n.vals[key] = v
}

// stripComment removes a "#" comment: a "#" at the start of the line or
// after whitespace, outside quotes.
func stripComment(s string) string {
	inS, inD := false, false
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\'' && !inD:
			inS = !inS
		case c == '"' && !inS:
			inD = !inD
		case c == '#' && !inS && !inD && (i == 0 || s[i-1] == ' '):
			return s[:i]
		}
	}
	return s
}

// splitKey splits "key: value" / "key:"; a quoted key is not supported.
func splitKey(text string) (key, val string, ok bool) {
	i := strings.Index(text, ":")
	for i >= 0 {
		if i+1 == len(text) || text[i+1] == ' ' {
			key = strings.TrimSpace(text[:i])
			if key == "" || strings.ContainsAny(key, "'\"{}[] ") {
				return "", "", false
			}
			return key, strings.TrimSpace(text[i+1:]), true
		}
		j := strings.Index(text[i+1:], ":")
		if j < 0 {
			break
		}
		i += 1 + j
	}
	return "", "", false
}

func parseScalar(val string, line int) (ymlScalar, error) {
	switch val[0] {
	case '\'':
		if len(val) < 2 || val[len(val)-1] != '\'' {
			return ymlScalar{}, fmt.Errorf("game.yml line %d: unterminated single-quoted scalar", line)
		}
		return ymlScalar{text: strings.ReplaceAll(val[1:len(val)-1], "''", "'"), quoted: true, line: line}, nil
	case '"':
		if len(val) < 2 || val[len(val)-1] != '"' {
			return ymlScalar{}, fmt.Errorf("game.yml line %d: unterminated double-quoted scalar", line)
		}
		s, err := strconv.Unquote(val)
		if err != nil {
			return ymlScalar{}, fmt.Errorf("game.yml line %d: double-quoted scalar: %v", line, err)
		}
		return ymlScalar{text: s, quoted: true, line: line}, nil
	case '|', '>':
		return ymlScalar{}, fmt.Errorf("game.yml line %d: a block scalar", line)
	case '&', '*', '!':
		return ymlScalar{}, fmt.Errorf("game.yml line %d: an anchor, alias or tag", line)
	case '{', '[':
		return ymlScalar{}, fmt.Errorf("game.yml line %d: a flow collection", line)
	}
	return ymlScalar{text: val, line: line}, nil
}

// --- typed access ---

// ymlGet walks a dotted path; ok is false when any key is missing.
func (n *ymlNode) get(path string) (any, bool) {
	var cur any = n
	for _, k := range strings.Split(path, ".") {
		m, isMap := cur.(*ymlNode)
		if !isMap {
			return nil, false
		}
		v, ok := m.vals[k]
		if !ok {
			return nil, false
		}
		cur = v
	}
	return cur, true
}

type ymlReader struct {
	root *ymlNode
	err  error
}

func (r *ymlReader) fail(path string, sc ymlScalar, want string) {
	if r.err == nil {
		r.err = fmt.Errorf("game.yml line %d: %s is %q, want %s", sc.line, path, sc.text, want)
	}
}

// scalar is the scalar at path; ok false when absent or null.
func (r *ymlReader) scalar(path string) (ymlScalar, bool) {
	v, ok := r.root.get(path)
	if !ok {
		return ymlScalar{}, false
	}
	sc, isScalar := v.(ymlScalar)
	if !isScalar {
		if r.err == nil {
			r.err = fmt.Errorf("game.yml: %s is a mapping, want a scalar", path)
		}
		return ymlScalar{}, false
	}
	if !sc.quoted {
		switch sc.text {
		case "", "~", "null", "Null", "NULL":
			return ymlScalar{}, false
		}
	}
	return sc, true
}

func (r *ymlReader) str(path, def string) string {
	sc, ok := r.scalar(path)
	if !ok {
		return def
	}
	return sc.text
}

func (r *ymlReader) boolean(path string, def bool) bool {
	sc, ok := r.scalar(path)
	if !ok {
		return def
	}
	if !sc.quoted {
		switch sc.text {
		case "true", "True", "TRUE":
			return true
		case "false", "False", "FALSE":
			return false
		}
	}
	r.fail(path, sc, "true or false")
	return def
}

func (r *ymlReader) integer(path string, def int) int {
	sc, ok := r.scalar(path)
	if !ok {
		return def
	}
	if !sc.quoted {
		if v, err := strconv.Atoi(sc.text); err == nil {
			return v
		}
		// Java reads a Number and takes intValue: 300.0 is 300.
		if f, err := strconv.ParseFloat(sc.text, 64); err == nil && f == float64(int(f)) {
			return int(f)
		}
	}
	r.fail(path, sc, "an integer")
	return def
}

func (r *ymlReader) number(path string, def float64) float64 {
	sc, ok := r.scalar(path)
	if !ok {
		return def
	}
	if !sc.quoted {
		if f, err := strconv.ParseFloat(sc.text, 64); err == nil {
			return f
		}
	}
	r.fail(path, sc, "a number")
	return def
}

// PlayerConfig is one player's half of game.yml, with the defaults of
// MageZero's Config.java (PlayerConfig, PriorsConfig, NoiseConfig,
// MctsConfig, GameplayConfig, HiddenInfoConfig).
type PlayerConfig struct {
	DeckPath     string
	DeckPool     string // a text file listing .dck paths, one per line
	DeckPoolMode string // "random" or "sequential"
	Type         string // "mcts" or "minimax"
	OutputFile   string

	PriorPriority, PriorTarget, PriorBinary, PriorOpponent bool
	PriorTemperature                                       float64
	NoiseEnabled                                           bool

	SearchBudget     int
	TimeoutMS        int
	TDDiscount       float64
	BackpropDiscount float64
	OfflineMode      bool
	// OpponentNodes is mcts.opponent_nodes, a gorge-only key (MageZero's
	// Config.java has none; draft-zero's runner passes every key it does not
	// set through untouched): the seat's tree also branches on the
	// opponent's searched decisions (azmcts.Options.OpponentNodes) instead
	// of answering them with the bot. Default false.
	OpponentNodes bool
	// ReuseTree is mcts.reuse_tree, a gorge-only key like opponent_nodes:
	// the seat keeps its search tree between decisions
	// (azmcts.Options.ReuseTree), as upstream always does. Default false.
	ReuseTree bool
	// UpstreamSearch is mcts.upstream_search, a gorge-only key like the two
	// above: every upstream-fidelity switch of the seat's search at once
	// (SeatSetup.SetUpstreamSearch). Default false.
	UpstreamSearch bool
	// ParentVisits, DeadlineBestChild, CombatSteps and MicroKinds are
	// mcts.parent_visits, mcts.deadline_best_child, mcts.combat_steps and
	// mcts.micro_kinds, gorge-only keys like the three above: one
	// upstream-fidelity switch each (the SeatSetup fields of the same
	// names), so a seat can run one part of upstream_search alone (an
	// ablation). Default false.
	ParentVisits, DeadlineBestChild, CombatSteps, MicroKinds bool

	Mulligans     bool
	ManualTapping bool

	SeeOpponentHand bool
}

// Config is game.yml.
type Config struct {
	GoesFirst string // "player_a", "player_b" or "random"
	GameMode  string
	A, B      PlayerConfig

	Games      int
	Threads    int
	MaxTurns   int
	MaxMinutes int

	Host         string
	Port         int
	OpponentPort int
}

// LoadConfig reads a game.yml file.
func LoadConfig(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("mzplay: game config: %w", err)
	}
	c, err := ParseConfig(string(raw))
	if err != nil {
		return Config{}, fmt.Errorf("mzplay: %s: %w", path, err)
	}
	return c, nil
}

// ParseConfig reads game.yml text. Absent keys take Config.java's defaults.
func ParseConfig(src string) (Config, error) {
	root, err := parseYML(src)
	if err != nil {
		return Config{}, err
	}
	r := &ymlReader{root: root}
	for _, need := range []string{"player_a", "player_b", "training", "server"} {
		if v, ok := root.vals[need]; !ok {
			return Config{}, fmt.Errorf("game.yml: no %s block", need)
		} else if _, isMap := v.(*ymlNode); !isMap {
			return Config{}, fmt.Errorf("game.yml: %s is not a mapping", need)
		}
	}
	player := func(p string) PlayerConfig {
		return PlayerConfig{
			DeckPath:          r.str(p+".deckPath", ""),
			DeckPool:          r.str(p+".deck_pool", ""),
			DeckPoolMode:      r.str(p+".deck_pool_mode", "random"),
			Type:              r.str(p+".type", "mcts"),
			OutputFile:        r.str(p+".output_file", ""),
			PriorPriority:     r.boolean(p+".priors.priority", false),
			PriorTarget:       r.boolean(p+".priors.target", false),
			PriorBinary:       r.boolean(p+".priors.binary", false),
			PriorOpponent:     r.boolean(p+".priors.opponent", false),
			PriorTemperature:  r.number(p+".priors.prior_temperature", 1.5),
			NoiseEnabled:      r.boolean(p+".noise.enabled", false),
			SearchBudget:      r.integer(p+".mcts.search_budget", 300),
			TimeoutMS:         r.integer(p+".mcts.timeout_ms", 4000),
			TDDiscount:        r.number(p+".mcts.td_discount", 0.95),
			BackpropDiscount:  r.number(p+".mcts.backprop_discount", 0.99),
			OfflineMode:       r.boolean(p+".mcts.offline_mode", false),
			OpponentNodes:     r.boolean(p+".mcts.opponent_nodes", false),
			ReuseTree:         r.boolean(p+".mcts.reuse_tree", false),
			UpstreamSearch:    r.boolean(p+".mcts.upstream_search", false),
			ParentVisits:      r.boolean(p+".mcts.parent_visits", false),
			DeadlineBestChild: r.boolean(p+".mcts.deadline_best_child", false),
			CombatSteps:       r.boolean(p+".mcts.combat_steps", false),
			MicroKinds:        r.boolean(p+".mcts.micro_kinds", false),
			Mulligans:         r.boolean(p+".gameplay.mulligans_enabled", true),
			ManualTapping:     r.boolean(p+".gameplay.manual_tapping", false),
			SeeOpponentHand:   r.boolean(p+".hiddenInfo.see_opponent_hand", true),
		}
	}
	c := Config{
		GoesFirst:    r.str("goes_first", "random"),
		GameMode:     r.str("game_mode", "normal"),
		A:            player("player_a"),
		B:            player("player_b"),
		Games:        r.integer("training.games", 1000),
		Threads:      r.integer("training.threads", 2),
		MaxTurns:     r.integer("training.max_turns", 50),
		MaxMinutes:   r.integer("training.max_minutes", 20),
		Host:         r.str("server.host", "localhost"),
		Port:         r.integer("server.port", 8080),
		OpponentPort: r.integer("server.opponent_port", 8081),
	}
	if r.err != nil {
		return Config{}, r.err
	}
	return c, nil
}

// Validate refuses every setting this engine does not implement. A refusal
// is an error, never a silent default: a run that asks for policy priors
// must not get a uniform-prior search under that name.
func (c Config) Validate() error {
	if c.GameMode != "normal" {
		return fmt.Errorf("game.yml: game_mode %q is not implemented (only two-player \"normal\")", c.GameMode)
	}
	switch c.GoesFirst {
	case "player_a", "player_b", "random":
	default:
		return fmt.Errorf("game.yml: goes_first %q: want player_a, player_b or random", c.GoesFirst)
	}
	if c.Games < 0 || c.Threads < 1 {
		return fmt.Errorf("game.yml: training.games %d / training.threads %d", c.Games, c.Threads)
	}
	if c.MaxTurns < 1 {
		return fmt.Errorf("game.yml: training.max_turns %d must be >= 1", c.MaxTurns)
	}
	for _, p := range []struct {
		name string
		pc   PlayerConfig
	}{{"player_a", c.A}, {"player_b", c.B}} {
		pc := p.pc
		switch {
		case pc.Type != "mcts":
			return fmt.Errorf("game.yml: %s.type %q is not implemented (only \"mcts\"; upstream's minimax player is ComputerPlayer8)", p.name, pc.Type)
		case pc.PriorPriority || pc.PriorTarget || pc.PriorBinary || pc.PriorOpponent:
			return fmt.Errorf("game.yml: %s.priors is switched on (priority %v, target %v, binary %v, opponent %v): no policy prior is implemented, the search is uniform-prior only",
				p.name, pc.PriorPriority, pc.PriorTarget, pc.PriorBinary, pc.PriorOpponent)
		case pc.NoiseEnabled:
			return fmt.Errorf("game.yml: %s.noise.enabled: root noise and sampled moves are not implemented", p.name)
		case pc.Mulligans:
			return fmt.Errorf("game.yml: %s.gameplay.mulligans_enabled: mulligans are not implemented (experiment #2a plays without them)", p.name)
		case pc.ManualTapping:
			return fmt.Errorf("game.yml: %s.gameplay.manual_tapping is not implemented (the search pays with the engine's payment plans)", p.name)
		case pc.SearchBudget < 1:
			return fmt.Errorf("game.yml: %s.mcts.search_budget %d must be >= 1", p.name, pc.SearchBudget)
		case pc.TDDiscount < 0 || pc.TDDiscount > 1 || pc.TDDiscount != pc.TDDiscount:
			return fmt.Errorf("game.yml: %s.mcts.td_discount %v must be in [0,1]", p.name, pc.TDDiscount)
		case pc.BackpropDiscount <= 0 || pc.BackpropDiscount > 1 || pc.BackpropDiscount != pc.BackpropDiscount:
			return fmt.Errorf("game.yml: %s.mcts.backprop_discount %v must be in (0,1]", p.name, pc.BackpropDiscount)
		case pc.DeckPool == "" && pc.DeckPath == "":
			return fmt.Errorf("game.yml: %s has neither deck_pool nor deckPath", p.name)
		case pc.DeckPoolMode != "random" && pc.DeckPoolMode != "sequential":
			return fmt.Errorf("game.yml: %s.deck_pool_mode %q: want random or sequential", p.name, pc.DeckPoolMode)
		}
	}
	return nil
}
