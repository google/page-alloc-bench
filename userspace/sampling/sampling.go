// Package sampling has helpers for reservoir sampling.
package sampling

import (
	"math/rand"
	"time"
)

// Reservoir implements what is described in
// https://en.wikipedia.org/wiki/Reservoir_sampling
type Reservoir[T any] struct {
	// Length of this slice is the desired output sample size.
	outSamples   []T
	numInSamples int
	rand         *rand.Rand
}

// NewReservoir initializes a Reservoir that will take a sample of up to the
// given size of a stream of data.
func NewReservoir[T any](size int) *Reservoir[T] {
	return &Reservoir[T]{
		outSamples: make([]T, size),
		rand:       rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// Add adds an item to the reservoir.
func (r *Reservoir[T]) Add(datum T) {
	outIdx := r.numInSamples
	r.numInSamples++
	// https://en.wikipedia.org/wiki/Reservoir_sampling#Simple:_Algorithm_R
	// Until the sample is full we pick every input.
	if outIdx >= len(r.outSamples) {
		// After we have enough samples we drop samples with linearly
		// increasing probability.
		outIdx = r.rand.Int() % r.numInSamples
		if outIdx >= len(r.outSamples) {
			return
		}
	}
	r.outSamples[outIdx] = datum
}

// Samples returns, at any given time, a random sample of the data passed to
// Add. The result is read-only.
func (r *Reservoir[T]) Samples() []T {
	n := r.numInSamples
	if n > len(r.outSamples) {
		n = len(r.outSamples)
	}
	return r.outSamples[:n]
}
