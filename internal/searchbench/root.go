package searchbench

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// RootRecord is the stable, rerunnable locator for one source-labelled native
// decision. Recorded preserves payment witnesses, which option-index labels
// alone cannot represent.
type RootRecord struct {
	GameID            string          `json:"game_id"`
	Ordinal           int             `json:"ordinal"`
	Seat              state.PlayerID  `json:"seat"`
	Turn              int32           `json:"turn"`
	Sequence          uint64          `json:"sequence"`
	PrefixDigest      string          `json:"prefix_digest"`
	PublicStateDigest string          `json:"public_state_digest"`
	DecisionDigest    string          `json:"decision_digest"`
	Recorded          decision.Intent `json:"recorded"`
}

func NewRootRecord(gameID string, ordinal int, root *rules.Engine, d *decision.Decision, recorded decision.Intent) (RootRecord, error) {
	if root == nil || d == nil || gameID == "" || ordinal < 0 {
		return RootRecord{}, fmt.Errorf("searchbench: invalid root record input")
	}
	v := view.Project(root.G, root, d.Player, d)
	pub, err := json.Marshal(v)
	if err != nil {
		return RootRecord{}, err
	}
	db, err := json.Marshal(d)
	if err != nil {
		return RootRecord{}, err
	}
	digest := func(b []byte) string { x := sha256.Sum256(b); return hex.EncodeToString(x[:]) }
	return RootRecord{GameID: gameID, Ordinal: ordinal, Seat: d.Player, Turn: root.G.Turn, Sequence: d.Seq, PrefixDigest: root.L.Head(), PublicStateDigest: digest(pub), DecisionDigest: digest(db), Recorded: decision.CloneIntent(recorded)}, nil
}
