package rules

import "github.com/adams-shaun/gorge/effects"

// *Engine is effects.Host's one production implementation. Its methods are
// spread over many files, so without this assertion a signature drift on
// either side surfaces only where an Engine is first passed as a Host.
var _ effects.Host = (*Engine)(nil)
