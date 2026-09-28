package events

// The events test binary runs the arena-walk skips (state.Game.BlockersLive,
// GoadsSeen) in verify mode: every skipped walk runs anyway and panics on
// anything it would have had to write.
func init() { ArenaSkipVerify = true }
