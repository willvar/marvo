package scheduling

import (
	"errors"
	"fmt"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/robfig/cron/v3"
)

type Kind string

const (
	KindAt       Kind = "at"
	KindEvery    Kind = "every"
	KindCron     Kind = "cron"
	KindAdaptive Kind = "adaptive"

	MinimumInterval = time.Minute
	MaximumInterval = 10 * 365 * 24 * time.Hour
)

var ErrInvalidSchedule = errors.New("invalid schedule")

// Spec deliberately keeps calendar, interval and adaptive schedules distinct.
// Cron is one schedule kind, not an interchange format for every cadence.
type Spec struct {
	At             *time.Time `json:"at,omitempty"`
	EverySeconds   int64      `json:"every_seconds,omitempty"`
	Anchor         *time.Time `json:"anchor,omitempty"`
	Expression     string     `json:"expression,omitempty"`
	MinimumSeconds int64      `json:"minimum_seconds,omitempty"`
	MaximumSeconds int64      `json:"maximum_seconds,omitempty"`
	DefaultSeconds int64      `json:"default_seconds,omitempty"`
}

type Definition struct {
	Kind     Kind   `json:"kind"`
	Spec     Spec   `json:"spec"`
	Timezone string `json:"timezone,omitempty"`
}

func Normalize(definition Definition, now time.Time) (Definition, error) {
	now = now.UTC()
	definition.Timezone = strings.TrimSpace(definition.Timezone)
	definition.Spec.Expression = strings.TrimSpace(definition.Spec.Expression)
	if len(definition.Timezone) > 256 || len(definition.Spec.Expression) > 256 {
		return Definition{}, ErrInvalidSchedule
	}

	switch definition.Kind {
	case KindAt:
		if definition.Spec.At == nil || hasEveryFields(definition.Spec) || definition.Spec.Expression != "" ||
			hasAdaptiveFields(definition.Spec) || definition.Timezone != "" {
			return Definition{}, ErrInvalidSchedule
		}
		at := definition.Spec.At.UTC().Truncate(time.Millisecond)
		if !at.After(now) {
			return Definition{}, fmt.Errorf("%w: one-time execution must be in the future", ErrInvalidSchedule)
		}
		definition.Spec.At = &at
	case KindEvery:
		if definition.Spec.At != nil || definition.Spec.Expression != "" || hasAdaptiveFields(definition.Spec) || definition.Timezone != "" {
			return Definition{}, ErrInvalidSchedule
		}
		interval := secondsDuration(definition.Spec.EverySeconds)
		if interval < MinimumInterval || interval > MaximumInterval {
			return Definition{}, fmt.Errorf("%w: interval is outside the supported range", ErrInvalidSchedule)
		}
		if definition.Spec.Anchor == nil {
			anchor := now.Truncate(time.Millisecond)
			definition.Spec.Anchor = &anchor
		} else {
			anchor := definition.Spec.Anchor.UTC().Truncate(time.Millisecond)
			definition.Spec.Anchor = &anchor
		}
	case KindCron:
		if definition.Spec.At != nil || hasEveryFields(definition.Spec) || hasAdaptiveFields(definition.Spec) ||
			definition.Spec.Expression == "" || definition.Timezone == "" {
			return Definition{}, ErrInvalidSchedule
		}
		if _, err := time.LoadLocation(definition.Timezone); err != nil {
			return Definition{}, fmt.Errorf("%w: unknown timezone", ErrInvalidSchedule)
		}
		if _, err := cronParser.Parse(definition.Spec.Expression); err != nil {
			return Definition{}, fmt.Errorf("%w: invalid cron expression", ErrInvalidSchedule)
		}
	case KindAdaptive:
		if definition.Spec.At != nil || hasEveryFields(definition.Spec) || definition.Spec.Expression != "" || definition.Timezone != "" {
			return Definition{}, ErrInvalidSchedule
		}
		minimum := secondsDuration(definition.Spec.MinimumSeconds)
		maximum := secondsDuration(definition.Spec.MaximumSeconds)
		fallback := secondsDuration(definition.Spec.DefaultSeconds)
		if minimum < MinimumInterval || maximum < minimum || maximum > MaximumInterval || fallback < minimum || fallback > maximum {
			return Definition{}, fmt.Errorf("%w: adaptive intervals are invalid", ErrInvalidSchedule)
		}
	default:
		return Definition{}, ErrInvalidSchedule
	}
	return definition, nil
}

func First(definition Definition, now time.Time) (time.Time, error) {
	normalized, err := Normalize(definition, now)
	if err != nil {
		return time.Time{}, err
	}
	switch normalized.Kind {
	case KindAt:
		return normalized.Spec.At.UTC(), nil
	case KindEvery:
		return nextInterval(*normalized.Spec.Anchor, secondsDuration(normalized.Spec.EverySeconds), now), nil
	case KindCron:
		return nextCron(normalized, now)
	case KindAdaptive:
		return now.UTC().Add(secondsDuration(normalized.Spec.DefaultSeconds)).Truncate(time.Millisecond), nil
	default:
		return time.Time{}, ErrInvalidSchedule
	}
}

// Next calculates the next natural occurrence after a completed scheduled run.
// Missed recurring occurrences are collapsed instead of replayed one by one.
func Next(definition Definition, completedAt time.Time, adaptiveProposal time.Duration) (*time.Time, error) {
	normalized, err := normalizeExisting(definition)
	if err != nil {
		return nil, err
	}
	completedAt = completedAt.UTC()
	switch normalized.Kind {
	case KindAt:
		return nil, nil
	case KindEvery:
		next := nextInterval(*normalized.Spec.Anchor, secondsDuration(normalized.Spec.EverySeconds), completedAt)
		return &next, nil
	case KindCron:
		next, err := nextCron(normalized, completedAt)
		return &next, err
	case KindAdaptive:
		delay := adaptiveProposal
		minimum := secondsDuration(normalized.Spec.MinimumSeconds)
		maximum := secondsDuration(normalized.Spec.MaximumSeconds)
		if delay == 0 {
			delay = secondsDuration(normalized.Spec.DefaultSeconds)
		}
		if delay < minimum {
			delay = minimum
		}
		if delay > maximum {
			delay = maximum
		}
		next := completedAt.Add(delay).Truncate(time.Millisecond)
		return &next, nil
	default:
		return nil, ErrInvalidSchedule
	}
}

func ClampAdaptive(definition Definition, delay time.Duration) (time.Duration, error) {
	normalized, err := normalizeExisting(definition)
	if err != nil || normalized.Kind != KindAdaptive {
		return 0, ErrInvalidSchedule
	}
	minimum := secondsDuration(normalized.Spec.MinimumSeconds)
	maximum := secondsDuration(normalized.Spec.MaximumSeconds)
	if delay < minimum {
		delay = minimum
	}
	if delay > maximum {
		delay = maximum
	}
	return delay, nil
}

func normalizeExisting(definition Definition) (Definition, error) {
	// Existing one-shot timestamps are allowed to be in the past. Use a point
	// before the stored timestamp so the same structural validation still applies.
	now := time.Now().UTC()
	if definition.Kind == KindAt && definition.Spec.At != nil {
		now = definition.Spec.At.Add(-time.Millisecond)
	}
	return Normalize(definition, now)
}

func nextCron(definition Definition, after time.Time) (time.Time, error) {
	location, err := time.LoadLocation(definition.Timezone)
	if err != nil {
		return time.Time{}, ErrInvalidSchedule
	}
	schedule, err := cronParser.Parse(definition.Spec.Expression)
	if err != nil {
		return time.Time{}, ErrInvalidSchedule
	}
	next := schedule.Next(after.In(location)).UTC().Truncate(time.Millisecond)
	if next.IsZero() {
		return time.Time{}, ErrInvalidSchedule
	}
	return next, nil
}

func nextInterval(anchor time.Time, interval time.Duration, after time.Time) time.Time {
	anchorMillis := anchor.UTC().Truncate(time.Millisecond).UnixMilli()
	afterMillis := after.UTC().Truncate(time.Millisecond).UnixMilli()
	if anchorMillis > afterMillis {
		return time.UnixMilli(anchorMillis).UTC()
	}
	// time.Time.Sub saturates at roughly 290 years. Calendar-valid anchors can
	// be older than that, so calculate in milliseconds to keep the next result
	// strictly after `after` instead of accidentally returning another past time.
	intervalMillis := interval.Milliseconds()
	elapsedMillis := afterMillis - anchorMillis
	steps := elapsedMillis/intervalMillis + 1
	return time.UnixMilli(anchorMillis + steps*intervalMillis).UTC()
}

func secondsDuration(seconds int64) time.Duration {
	if seconds <= 0 || seconds > int64(MaximumInterval/time.Second) {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func hasEveryFields(spec Spec) bool {
	return spec.EverySeconds != 0 || spec.Anchor != nil
}

func hasAdaptiveFields(spec Spec) bool {
	return spec.MinimumSeconds != 0 || spec.MaximumSeconds != 0 || spec.DefaultSeconds != 0
}

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
