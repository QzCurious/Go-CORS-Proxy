package conflatedstream_test

import (
	"testing"
	"time"

	"github.com/QzCurious/seamless-cors/internal/lib/conflatedstream"
)

func TestPublishMakesValueAvailable(t *testing.T) {
	publisher, stream := conflatedstream.New[int]()
	publisher.Publish(1)
	if got := <-stream.Updates(); got != 1 {
		t.Fatalf("value = %d, want 1", got)
	}
}

func TestPublishReplacesPendingValue(t *testing.T) {
	publisher, stream := conflatedstream.New[int]()
	publisher.Publish(1)
	publisher.Publish(2)
	if got := <-stream.Updates(); got != 2 {
		t.Fatalf("value = %d, want 2", got)
	}
}

func TestPublishAfterReceiveMakesNextValueAvailable(t *testing.T) {
	publisher, stream := conflatedstream.New[int]()
	publisher.Publish(1)
	if got := <-stream.Updates(); got != 1 {
		t.Fatalf("first value = %d, want 1", got)
	}
	publisher.Publish(2)
	if got := <-stream.Updates(); got != 2 {
		t.Fatalf("second value = %d, want 2", got)
	}
}

func TestPublishDoesNotBlockWithPendingValue(t *testing.T) {
	publisher, stream := conflatedstream.New[int]()
	publisher.Publish(1)
	done := make(chan struct{})
	go func() {
		publisher.Publish(2)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish blocked")
	}
	if got := <-stream.Updates(); got != 2 {
		t.Fatalf("value = %d, want 2", got)
	}
}

func TestCloseClosesUpdates(t *testing.T) {
	publisher, stream := conflatedstream.New[int]()
	publisher.Close()
	if _, ok := <-stream.Updates(); ok {
		t.Fatal("updates remained open")
	}
}
