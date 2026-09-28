package v2shadow

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/spellbench/sbsearch"
)

// Names are the registered shadow policies (cmd/sbagent -policy shadow-*):
//
//	tactical              sb-tactical on the shadow
//	generic               route (a): the hint-free v1 policy (sb-generic) on v2 observations
//	search                sb-search's default budget (16 worlds, to game end)
//	search-fast[-atk]     8 worlds, 3-turn horizon (+ attack declarations searched)
//	search-lite[-atk]     4 worlds, 2-turn horizon (+ attacks)
//	search-wN-hM[-atk]    N worlds, M-turn horizon
//	...-blk               any search budget with block declarations searched too
//	fallback              the fallback policy only (a plumbing control)
var Names = []string{"tactical", "generic", "search", "search-fast", "search-fast-atk", "search-lite", "search-lite-atk", "fallback"}

// NamedConfig returns the Config of a registered shadow policy name (the
// part after "shadow-"). The registry, seed, clock and trace are the
// caller's.
func NamedConfig(name string, reg *cards.Registry) (Config, error) {
	cfg := Config{Reg: reg}
	switch {
	case name == "tactical":
		cfg.Mode = ModeTactical
		return cfg, nil
	case name == "fallback":
		cfg.Mode = ModeFallback
		return cfg, nil
	case name == "generic":
		cfg.Mode = ModeGeneric
		return cfg, nil
	case strings.HasPrefix(name, "search"):
		cfg.Mode = ModeSearch
		sc := sbsearch.DefaultConfig()
		rest := strings.TrimPrefix(name, "search")
		if r, ok := strings.CutSuffix(rest, "-blk"); ok {
			sc.Block = true
			rest = r
		}
		if r, ok := strings.CutSuffix(rest, "-atk"); ok {
			sc.Attack = true
			rest = r
		}
		switch {
		case rest == "":
		case rest == "-fast":
			sc.Worlds, sc.Horizon = 8, 3
		case rest == "-lite":
			sc.Worlds, sc.Horizon = 4, 2
		default:
			var w, h int
			if _, err := fmt.Sscanf(rest, "-w%d-h%d", &w, &h); err != nil || w < 0 || h < 0 {
				return cfg, fmt.Errorf("v2shadow: unknown search budget %q", name)
			}
			sc.Worlds, sc.Horizon = w, int32(h)
		}
		cfg.Search = sc
		return cfg, nil
	}
	return cfg, fmt.Errorf("v2shadow: unknown policy %q (want one of %s)", name, strings.Join(Names, ", "))
}
