package v2agent

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"strconv"
)

// AgentSeed is spec 11.6's agent_seed(i, s): the first 8 bytes of
// HMAC-SHA256(key = runSecret, message = "spellbench/v2/agent-seed:<i>:<s>"),
// big-endian, masked to its low 53 bits. Agents receive the value in
// game_start (spec 10.2) and never need to compute it; it is here for local
// harnesses that host games themselves and want the arena's seeds.
func AgentSeed(runSecret []byte, gameIndex uint64, seat string) uint64 {
	mac := hmac.New(sha256.New, runSecret)
	mac.Write([]byte("spellbench/v2/agent-seed:" + strconv.FormatUint(gameIndex, 10) + ":" + seat))
	return binary.BigEndian.Uint64(mac.Sum(nil)[:8]) & (1<<53 - 1)
}
