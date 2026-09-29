package host

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// writeCrashReport is spec D15's evidence: what the match was doing when
// it died, enough to reproduce from the files beside it. Best effort — a
// failure to write the report must not mask the crash itself.
func (r *Registry) writeCrashReport(t *table, m *match, reason string) {
	if r.opts.Dir == "" {
		return
	}
	dir := filepath.Join(r.opts.Dir, "crash")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	m.mu.RLock()
	// head and seq are over the PERSISTED prefix, not the in-memory log
	// (Task 12 fix round 1): the in-memory tail can be ahead of disk
	// because the crashed Submit's events never fully reached the files, so
	// only the durable prefix is safe to name in a report meant to agree
	// with the log it points at.
	persisted := len(m.e.L.Events) - 1
	if m.files != nil {
		persisted = m.persisted - 1
	}
	body := fmt.Sprintf("table: %s\nmatch: %d\nseed: %d\nhead: %s\nseq: %d\nintents: %d\nturn: %d\nrefusals: %d\nfallbacks: %d\nreason:\n%s\n",
		t.cfg.ID, m.k, m.seed, m.head, persisted, m.intents, m.e.G.Turn, m.refusals, m.fallbacks, reason)
	m.mu.RUnlock()
	_ = os.WriteFile(filepath.Join(dir, string(t.cfg.ID)+"-"+strconv.Itoa(m.k)+".txt"), []byte(body), 0o644)
}
