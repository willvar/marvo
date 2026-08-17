package scheduler

import (
	"container/heap"
	"time"
)

// wakeQueue keeps one wake time per user. Updating one user's schedule is
// O(log n), and finding the next user never scans every scheduled space.
type wakeQueue struct {
	items  []*wakeItem
	byUser map[string]*wakeItem
}

type wakeItem struct {
	userID string
	when   time.Time
	index  int
}

func newWakeQueue() *wakeQueue {
	queue := &wakeQueue{byUser: make(map[string]*wakeItem)}
	heap.Init(queue)
	return queue
}

func (q wakeQueue) Len() int { return len(q.items) }

func (q wakeQueue) Less(i, j int) bool {
	if !q.items[i].when.Equal(q.items[j].when) {
		return q.items[i].when.Before(q.items[j].when)
	}
	return q.items[i].userID < q.items[j].userID
}

func (q wakeQueue) Swap(i, j int) {
	q.items[i], q.items[j] = q.items[j], q.items[i]
	q.items[i].index = i
	q.items[j].index = j
}

func (q *wakeQueue) Push(value any) {
	item := value.(*wakeItem)
	item.index = len(q.items)
	q.items = append(q.items, item)
	q.byUser[item.userID] = item
}

func (q *wakeQueue) Pop() any {
	last := len(q.items) - 1
	item := q.items[last]
	q.items[last] = nil
	q.items = q.items[:last]
	item.index = -1
	delete(q.byUser, item.userID)
	return item
}

func (q *wakeQueue) Peek() *wakeItem {
	if len(q.items) == 0 {
		return nil
	}
	return q.items[0]
}

func (q *wakeQueue) PopNext() *wakeItem {
	if len(q.items) == 0 {
		return nil
	}
	return heap.Pop(q).(*wakeItem)
}

func (q *wakeQueue) SetEarlier(userID string, when time.Time) {
	if item := q.byUser[userID]; item != nil {
		if !when.Before(item.when) {
			return
		}
		item.when = when
		heap.Fix(q, item.index)
		return
	}
	heap.Push(q, &wakeItem{userID: userID, when: when})
}
