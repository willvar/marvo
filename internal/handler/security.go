package handler

import (
	"net"
	"net/http"
	"strings"
	"time"
)

func (d *Dependencies) allowAttempt(kind string, r *http.Request, limit int, window time.Duration) bool {
	d = d.sharedSecurityRoot()
	key := kind + ":" + directRemoteIP(r)
	now := time.Now()
	d.securityMu.Lock()
	defer d.securityMu.Unlock()
	if d.rateLimits == nil {
		d.rateLimits = make(map[string]rateWindow)
	}
	entry := d.rateLimits[key]
	if entry.Reset.IsZero() || !now.Before(entry.Reset) {
		entry = rateWindow{Reset: now.Add(window)}
	}
	if entry.Count >= limit {
		d.rateLimits[key] = entry
		return false
	}
	entry.Count++
	d.rateLimits[key] = entry
	if len(d.rateLimits) > 10_000 {
		for candidate, value := range d.rateLimits {
			if !now.Before(value.Reset) {
				delete(d.rateLimits, candidate)
			}
		}
	}
	return true
}

func (d *Dependencies) resetAttempts(kind string, r *http.Request) {
	d = d.sharedSecurityRoot()
	d.securityMu.Lock()
	delete(d.rateLimits, kind+":"+directRemoteIP(r))
	d.securityMu.Unlock()
}

func (d *Dependencies) rememberChallenge(challenge string, expiry int64) {
	d = d.sharedSecurityRoot()
	d.securityMu.Lock()
	if d.challenges == nil {
		d.challenges = make(map[string]int64)
	}
	now := time.Now().Unix()
	for value, deadline := range d.challenges {
		if deadline < now {
			delete(d.challenges, value)
		}
	}
	d.challenges[challenge] = expiry
	d.securityMu.Unlock()
}

func (d *Dependencies) consumeChallenge(challenge string, expiry int64) bool {
	d = d.sharedSecurityRoot()
	d.securityMu.Lock()
	defer d.securityMu.Unlock()
	stored, ok := d.challenges[challenge]
	if !ok || stored != expiry || time.Now().Unix() > stored {
		return false
	}
	delete(d.challenges, challenge)
	return true
}

func (d *Dependencies) hasChallenge(challenge string, expiry int64) bool {
	d = d.sharedSecurityRoot()
	d.securityMu.Lock()
	defer d.securityMu.Unlock()
	stored, ok := d.challenges[challenge]
	return ok && stored == expiry && time.Now().Unix() <= stored
}

func (d *Dependencies) sharedSecurityRoot() *Dependencies {
	if d.securityRoot != nil {
		return d.securityRoot
	}
	return d
}

func directRemoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		host = strings.TrimSpace(r.RemoteAddr)
	}
	if host == "" {
		return "unknown"
	}
	return host
}
