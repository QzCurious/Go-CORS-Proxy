// Package conflatedstream provides single-producer, single-consumer streams
// that retain only the newest pending value.
package conflatedstream

// New constructs producer and consumer capabilities sharing one stream.
func New[T any]() (Publisher[T], Stream[T]) {
	updates := make(chan T, 1)
	return Publisher[T]{updates: updates}, Stream[T]{updates: updates}
}

// Stream is the consumer capability for a stream that conflates pending
// values. When a published value has not yet been received, the next
// publication replaces it.
//
// A Stream must be constructed with New and consumed by only one consumer.
type Stream[T any] struct {
	updates <-chan T
}

// Updates returns the stream's receive-only channel.
func (s Stream[T]) Updates() <-chan T { return s.updates }

// Publisher is the producer capability for a Stream. Its owner must call
// Close at most once and must not call Publish afterward.
type Publisher[T any] struct {
	updates chan T
}

// Publish makes value the pending value without waiting for the consumer.
func (p Publisher[T]) Publish(value T) {
	select {
	case p.updates <- value:
		return
	default:
	}

	select {
	case <-p.updates:
	default:
	}

	p.updates <- value
}

// Close closes the stream. The producer must call Close at most once and must
// not call Publish afterward.
func (p Publisher[T]) Close() { close(p.updates) }
