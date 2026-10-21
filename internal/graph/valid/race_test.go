//go:build race

package valid

// raceOn is set under the race detector, which slows checking too much
// for the timing tests to say anything.
const raceOn = true
