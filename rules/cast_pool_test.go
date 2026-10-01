package rules

// The rules test binary runs with recycled pendingCast storage poisoned
// (cast_pool.go), so a reader that kept a cast past its Submit reads garbage.
func init() { castPoolPoison = true }
