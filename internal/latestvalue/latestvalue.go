// Package latestvalue contains the small latest-value channel primitive used
// by single-producer state pipelines.
package latestvalue

// Publish places value in a capacity-one channel, replacing an older pending
// value when necessary. The channel must be owned by one producer and have
// capacity one.
func Publish[T any](ch chan T, value T) {
	select {
	case ch <- value:
		return
	default:
	}

	select {
	case <-ch:
	default:
	}

	ch <- value
}
