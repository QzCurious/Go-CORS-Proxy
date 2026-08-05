package latestvalue

import (
	"testing"
	"time"
)

func TestPublishToEmptyChannel(t *testing.T) {
	values := make(chan int, 1)
	Publish(values, 1)
	if got := <-values; got != 1 {
		t.Fatalf("value = %d, want 1", got)
	}
}

func TestPublishReplacesPendingValue(t *testing.T) {
	values := make(chan int, 1)
	Publish(values, 1)
	Publish(values, 2)
	if got := <-values; got != 2 {
		t.Fatalf("value = %d, want 2", got)
	}
}

func TestPublishAfterConsumerReceivesOldValue(t *testing.T) {
	values := make(chan int, 1)
	Publish(values, 1)
	if got := <-values; got != 1 {
		t.Fatalf("first value = %d, want 1", got)
	}
	Publish(values, 2)
	if got := <-values; got != 2 {
		t.Fatalf("second value = %d, want 2", got)
	}
}

func TestPublishDoesNotBlockWithPendingValue(t *testing.T) {
	values := make(chan int, 1)
	Publish(values, 1)
	done := make(chan struct{})
	go func() {
		Publish(values, 2)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish blocked")
	}
	if got := <-values; got != 2 {
		t.Fatalf("value = %d, want 2", got)
	}
}
