package runtimegateway

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"marvo/internal/runtimeevents"
)

func TestEventBrokerBroadcastsWithoutBlockingOnSlowSubscriber(t *testing.T) {
	broker := newEventBroker()
	fast, cancelFast := broker.subscribe()
	defer cancelFast()
	_, cancelSlow := broker.subscribe()
	defer cancelSlow()
	event := runtimeevents.Event{UserID: gatewayTestUserID, Kind: runtimeevents.KindActivity}

	for index := 0; index <= eventSubscriberBuffer; index++ {
		broker.publish(event)
		if index < eventSubscriberBuffer {
			select {
			case received := <-fast:
				if received != event {
					t.Fatalf("event = %#v", received)
				}
			case <-time.After(time.Second):
				t.Fatal("fast subscriber did not receive an event")
			}
		}
	}

	broker.mu.Lock()
	subscriberCount := len(broker.subscribers)
	broker.mu.Unlock()
	if subscriberCount != 1 {
		t.Fatalf("subscribers after overflow = %d, want 1", subscriberCount)
	}
}

func TestGatewayEventStreamRequiresRuntimeTokenAndPublishesTypedEvents(t *testing.T) {
	target, _ := url.Parse("http://unused.invalid")
	server := NewServer("gateway-secret", &staticRuntimeProvider{target: &RuntimeTarget{URL: target}})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	unauthorized, err := http.Get(httpServer.URL + "/events")
	if err != nil {
		t.Fatal(err)
	}
	_ = unauthorized.Body.Close()
	if unauthorized.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.StatusCode)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, httpServer.URL+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer gateway-secret")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("stream response = %d %q", response.StatusCode, response.Header.Get("Content-Type"))
	}
	reader := bufio.NewReader(response.Body)
	ready, err := readSSEFrame(reader)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ready, "event: ready") {
		t.Fatalf("ready frame = %q", ready)
	}

	server.publishStateEvent(gatewayTestUserID, runtimeevents.KindMemories)
	frameResult := make(chan string, 1)
	go func() {
		frame, _ := readSSEFrame(reader)
		frameResult <- frame
	}()
	select {
	case frame := <-frameResult:
		if !strings.Contains(frame, "event: state_changed") ||
			!strings.Contains(frame, `"user_id":"`+gatewayTestUserID+`"`) ||
			!strings.Contains(frame, `"kind":"memories"`) {
			t.Fatalf("state frame = %q", frame)
		}
	case <-time.After(time.Second):
		t.Fatal("state event was not streamed")
	}
}

func readSSEFrame(reader *bufio.Reader) (string, error) {
	var frame strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return frame.String(), err
		}
		frame.WriteString(line)
		if line == "\n" {
			return frame.String(), nil
		}
	}
}
