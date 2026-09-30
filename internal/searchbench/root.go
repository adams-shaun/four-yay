package searchbench

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// ReadRootRecords reads the append-only JSONL root index emitted by
// `searchbench source root-audit`.
func ReadRootRecords(path string) ([]RootRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	// A decision with a large payment witness can exceed Scanner's small
	// default token limit, while remaining bounded by this offline index.
	s.Buffer(make([]byte, 4096), 4<<20)
	var out []RootRecord
	for line := 1; s.Scan(); line++ {
		var record RootRecord
		if err := json.Unmarshal(s.Bytes(), &record); err != nil {
			return nil, fmt.Errorf("searchbench: root index line %d: %w", line, err)
		}
		out = append(out, record)
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, io.EOF
	}
	return out, nil
}

// RootRecord is the stable, rerunnable locator for one source-labelled native
// decision. Recorded preserves payment witnesses, which option-index labels
// alone cannot represent.
type RootRecord struct {
	GameID            string          `json:"game_id"`
	GenesisSeed       uint64          `json:"genesis_seed"`
	Ordinal           int             `json:"ordinal"`
	SourceKind        string          `json:"source_kind"`
	SourceCard        string          `json:"source_card"`
	Seat              state.PlayerID  `json:"seat"`
	Turn              int32           `json:"turn"`
	Sequence          uint64          `json:"sequence"`
	PrefixDigest      string          `json:"prefix_digest"`
	PublicStateDigest string          `json:"public_state_digest"`
	DecisionDigest    string          `json:"decision_digest"`
	Recorded          decision.Intent `json:"recorded"`
}

func NewRootRecord(gameID string, genesisSeed uint64, ordinal int, root *rules.Engine, d *decision.Decision, action RecordedAction) (RootRecord, error) {
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
	if action.Kind != "land" && action.Kind != "spell" || action.Card == "" {
		return RootRecord{}, fmt.Errorf("searchbench: invalid recorded root action")
	}
	return RootRecord{GameID: gameID, GenesisSeed: genesisSeed, Ordinal: ordinal, SourceKind: action.Kind, SourceCard: action.Card, Seat: d.Player, Turn: root.G.Turn, Sequence: d.Seq, PrefixDigest: root.L.Head(), PublicStateDigest: digest(pub), DecisionDigest: digest(db), Recorded: decision.CloneIntent(action.Intent)}, nil
}
