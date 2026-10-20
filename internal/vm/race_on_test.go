//go:build race

package vm

// raceEnabled: under the race detector sync.Pool drops a share of its Puts on
// purpose, so allocation counts that rely on pooling are not meaningful.
const raceEnabled = true
