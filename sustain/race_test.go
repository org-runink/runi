//go:build race

package sustain

// raceScale shrinks the longest randomised tests under the race detector,
// which slows them about tenfold; the plain run keeps the full length.
const raceScale = 4
