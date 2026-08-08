// Package conflatedstream provides single-producer streams that retain only
// the newest pending value.
package conflatedstream

// Stream is a single-consumer stream that conflates pending values. When a
// published value has not yet been received, the next publication replaces it.
//
// A Stream must be constructed with New. Its producer owns Publish and Close;
// publishing after Close is invalid.
type Stream[T any] struct {
	updates chan T
}

// New constructs a Stream.
func New[T any]() *Stream[T] {
	return &Stream[T]{updates: make(chan T, 1)}
}

// Updates returns the stream's receive-only channel.
func (s *Stream[T]) Updates() <-chan T { return s.updates }

// Publish makes value the pending value without waiting for the consumer.
func (s *Stream[T]) Publish(value T) {
	select {
	case s.updates <- value:
		return
	default:
	}

	select {
	case <-s.updates:
	default:
	}

	s.updates <- value
}

// Close closes the stream. The producer must call Close at most once and must
// not call Publish afterward.
func (s *Stream[T]) Close() { close(s.updates) }
