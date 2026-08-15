package handler

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"marvo/internal/runtimeevents"
)

const (
	runtimeEventInitialBackoff = 500 * time.Millisecond
	runtimeEventMaxBackoff     = 30 * time.Second
	runtimeEventMaxLineBytes   = 64 << 10
)

func (r *SpaceRegistry) StartRuntimeEvents() {
	r.eventsOnce.Do(func() {
		r.mu.Lock()
		if r.closed {
			r.mu.Unlock()
			return
		}
		r.backgroundWG.Add(1)
		r.mu.Unlock()
		go r.runRuntimeEvents()
	})
}

func (r *SpaceRegistry) runRuntimeEvents() {
	defer r.backgroundWG.Done()
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		ResponseHeaderTimeout: 15 * time.Second,
		IdleConnTimeout:       90 * time.Second,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	backoff := runtimeEventInitialBackoff
	reportedUnavailable := false
	for r.background.Err() == nil {
		connected, err := r.consumeRuntimeEvents(client)
		if r.background.Err() != nil || errors.Is(err, context.Canceled) {
			return
		}
		if err != nil && !reportedUnavailable {
			slog.Warn("runtime events unavailable; retrying", "error", err)
			reportedUnavailable = true
		}
		if connected {
			backoff = runtimeEventInitialBackoff
		} else if backoff < runtimeEventMaxBackoff {
			backoff *= 2
			if backoff > runtimeEventMaxBackoff {
				backoff = runtimeEventMaxBackoff
			}
		}
		select {
		case <-time.After(backoff):
		case <-r.background.Done():
			return
		case <-r.shuttingDown:
			return
		}
		if connected {
			reportedUnavailable = false
		}
	}
}

func (r *SpaceRegistry) consumeRuntimeEvents(client *http.Client) (bool, error) {
	request, err := http.NewRequestWithContext(r.background, http.MethodGet, r.config.Runtime.URL+"/events", nil)
	if err != nil {
		return false, err
	}
	request.Header.Set("Accept", "text/event-stream")
	request.Header.Set("Authorization", "Bearer "+r.config.Runtime.Token)
	response, err := client.Do(request)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.CopyN(io.Discard, response.Body, 4<<10)
		return false, fmt.Errorf("runtime event stream returned %s", response.Status)
	}

	// The Gateway subscribes before flushing the response headers. Refreshing
	// loaded spaces now closes the only loss window between stream attempts.
	r.resyncLoadedSpaces()
	if err := r.readRuntimeEventStream(response.Body); err != nil {
		return true, err
	}
	return true, errors.New("runtime event stream closed")
}

func (r *SpaceRegistry) readRuntimeEventStream(source io.Reader) error {
	scanner := bufio.NewScanner(source)
	scanner.Buffer(make([]byte, 4096), runtimeEventMaxLineBytes)
	eventName := ""
	data := make([]string, 0, 1)
	dispatch := func() {
		if eventName == "state_changed" && len(data) > 0 {
			var event runtimeevents.Event
			if json.Unmarshal([]byte(strings.Join(data, "\n")), &event) == nil {
				r.notifyRuntimeEvent(event)
			}
		}
		eventName = ""
		data = data[:0]
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			dispatch()
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, found := strings.Cut(line, ":")
		if !found {
			value = ""
		} else {
			value = strings.TrimPrefix(value, " ")
		}
		switch field {
		case "event":
			eventName = value
		case "data":
			data = append(data, value)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	dispatch()
	return nil
}

func (r *SpaceRegistry) resyncLoadedSpaces() {
	r.mu.Lock()
	type loadedSpace struct {
		userID string
		space  *UserSpace
	}
	loaded := make([]loadedSpace, 0, len(r.spaces))
	for userID, entry := range r.spaces {
		entry.leases++
		entry.lastUsed = r.now()
		loaded = append(loaded, loadedSpace{userID: userID, space: entry.space})
	}
	r.mu.Unlock()
	for _, current := range loaded {
		for _, kind := range []runtimeevents.Kind{
			runtimeevents.KindActivity,
			runtimeevents.KindSpace,
			runtimeevents.KindMemories,
			runtimeevents.KindAgentSettings,
			runtimeevents.KindDevices,
		} {
			current.space.broadcastRuntimeEvent(kind)
		}
		r.release(current.userID, current.space)
	}
}
