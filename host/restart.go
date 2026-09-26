package host

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/adams-shaun/gorge/protocol"
)

var onDemandTableID = regexp.MustCompile(`^g[1-9][0-9]*$`)

// legacyOnDemand recognizes the exact shape gorged wrote before
// TableConfig.OnDemand existed. The narrow check is a migration only: it
// prevents old g1..gN browser games from surviving one more deployment while
// never treating a normal human table as disposable.
func legacyOnDemand(c TableConfig) bool {
	return onDemandTableID.MatchString(string(c.ID)) &&
		strings.HasPrefix(c.Name, "Play vs bot (") && c.Seats == 2 &&
		!c.Perpetual && len(c.Humans) == 1 && c.Humans[0] == 0 &&
		len(c.PlayerNames) == 2 && c.PlayerNames[0] == "You" && c.PlayerNames[1] == "Bot"
}

// load reads tables.json and every sidecar. A match still marked live was
// cut off by a crash or kill: it is rewritten as aborted (spec: restart
// aborts in-progress matches; resume is M5). Perpetual tables are left
// ready for StartAll to begin match k+1.
func (r *Registry) load() error {
	raw, err := os.ReadFile(filepath.Join(r.opts.Dir, "tables.json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var tf tablesFile
	if err := json.Unmarshal(raw, &tf); err != nil {
		return fmt.Errorf("host: tables.json: %w", err)
	}
	// Keep the presence bit separate from TableConfig's bool: before payment
	// plans the key did not exist, while a current table may deliberately set
	// it false. TableConfig now serializes false explicitly, so this migration
	// runs only for genuinely pre-feature files.
	var shape struct {
		Tables []struct {
			Config map[string]json.RawMessage `json:"config"`
		} `json:"tables"`
	}
	if err := json.Unmarshal(raw, &shape); err != nil {
		return fmt.Errorf("host: tables.json: %w", err)
	}
	dropped := false
	for i, rec := range tf.Tables {
		if i < len(shape.Tables) {
			if _, present := shape.Tables[i].Config["bot_auto_pay_mana"]; !present {
				rec.Config.BotAutoPayMana = r.opts.DefaultBotAutoPayMana
			}
		}
		// Browser-created games have process-local credentials. A restart
		// cannot safely revive them (and historically aborts live games
		// anyway), so do not re-register them or keep accumulating their
		// table configs. Accept the prior exact gorged shape once as a
		// migration for deployments made before on_demand was recorded.
		if rec.Config.OnDemand || legacyOnDemand(rec.Config) {
			dropped = true
			continue
		}
		cfg, err := rec.Config.validated(r.opts.LoadDeck)
		if err != nil {
			return err
		}
		rec.Config = cfg
		t := newTable(cfg)
		t.k = rec.Match
		scs, err := readSidecars(r.opts.Dir, cfg.ID)
		if err != nil {
			return err
		}
		for _, sc := range scs {
			// Sidecars written before named host policies have no bot_policy.
			// They necessarily used the table's historical default controller,
			// so expose the restored table policy without rewriting their
			// replay-bearing archival record.
			if sc.BotPolicy == "" {
				sc.BotPolicy = cfg.BotPolicy
			}
			if sc.State == protocol.MatchLive {
				sc.State = protocol.MatchAborted
				if err := writeSidecar(r.opts.Dir, sc, r.opts.Sync); err != nil {
					return err
				}
				// Task M2c-1: a match the previous process was cut off in is
				// recorded here as aborted; an embedder that persists every
				// match needs to observe it as such, so OnMatchEnd fires with
				// the rewritten (aborted) MatchInfo, like any other terminal
				// transition. Sidecar-derived, not a live match — see
				// callOnMatchEnd.
				r.callOnMatchEnd(cfg.ID, sc.Match, sc.info())
			}
			// The index keeps no name list; loadArchived re-reads it from
			// N.json when it rebuilds the match (see archive).
			sc.NameUniverseNames = nil
			t.archived = append(t.archived, sc)
			if sc.Match > t.k {
				t.k = sc.Match
			}
		}
		r.tables[cfg.ID] = t
	}
	if dropped {
		// New has no concurrent callers yet, so saveLocked is safe here. This
		// rewrites tables.json without the disposable configs; their old log
		// files are intentionally retained as crash-era evidence and never
		// become reachable without a table record.
		if err := r.saveLocked(); err != nil {
			return err
		}
	}
	return nil
}
