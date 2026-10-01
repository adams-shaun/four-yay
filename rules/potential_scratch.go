package rules

// resizeCleared returns s with length n and every element zero, reusing its
// array when it is large enough.
func resizeCleared[T any](s []T, n int) []T {
	if cap(s) < n {
		return make([]T, n)
	}
	s = s[:n]
	clear(s)
	return s
}
