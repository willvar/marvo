package scheduler

import (
	"testing"
	"time"
)

func TestWakeQueueKeepsEarliestTimePerUser(t *testing.T) {
	queue := newWakeQueue()
	base := time.Date(2026, time.August, 17, 8, 0, 0, 0, time.UTC)
	queue.SetEarlier("later", base.Add(time.Hour))
	queue.SetEarlier("first", base.Add(time.Minute))
	queue.SetEarlier("later", base.Add(2*time.Hour))
	queue.SetEarlier("later", base.Add(30*time.Second))

	first := queue.PopNext()
	if first == nil || first.userID != "later" || !first.when.Equal(base.Add(30*time.Second)) {
		t.Fatalf("first item = %#v", first)
	}
	second := queue.PopNext()
	if second == nil || second.userID != "first" || !second.when.Equal(base.Add(time.Minute)) {
		t.Fatalf("second item = %#v", second)
	}
	if queue.Peek() != nil {
		t.Fatalf("queue still has %#v", queue.Peek())
	}
}
