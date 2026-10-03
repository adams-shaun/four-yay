package oraclediff

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Cache holds XMage driver results keyed by scenario hash (the verdict
// row's scenario_sha). XMage's side of a scenario is a pure function of the
// scenario, XMAGE_REF and the driver source, so a cache directory is
// specific to one (XMAGE_REF, driver) pair: the pass driver
// (scripts/compliance-pass.sh) names it after both. Lives off-repo, under
// XMAGE_ORACLE_DIR, next to the XMage build.
type Cache struct{ Dir string }

func (c Cache) path(sha string) string {
	pre := "xx"
	if len(sha) >= 2 {
		pre = sha[:2]
	}
	return filepath.Join(c.Dir, pre, sha+".json")
}

// Get returns the cached result for a scenario hash.
func (c Cache) Get(sha string) (XResult, bool) {
	if c.Dir == "" {
		return XResult{}, false
	}
	raw, err := os.ReadFile(c.path(sha))
	if err != nil {
		return XResult{}, false
	}
	var x XResult
	if json.Unmarshal(raw, &x) != nil {
		return XResult{}, false
	}
	return x, true
}

// Put stores a result under a scenario hash.
func (c Cache) Put(sha string, x XResult) error {
	if c.Dir == "" {
		return errors.New("oraclediff: no cache directory")
	}
	p := c.path(sha)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(x)
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}
