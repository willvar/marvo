package collab

import "testing"

func TestFirstPollMessageHasReplayableSequence(t *testing.T) {
	hub := NewHub()
	defer hub.Close()
	id := hub.RegisterPoll()
	if id == "" {
		t.Fatal("RegisterPoll() returned an empty id")
	}
	hub.SendToPollClient(id, []byte(`{"action":"subscribed"}`))
	messages := hub.PollReplaySince(id, 0)
	if len(messages) != 1 || messages[0].Seq != 1 {
		t.Fatalf("PollReplaySince() = %#v, want first message at sequence 1", messages)
	}
}
