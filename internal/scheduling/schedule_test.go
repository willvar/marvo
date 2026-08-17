package scheduling

import (
	"errors"
	"testing"
	"time"
)

func TestScheduleKindsRemainDistinct(t *testing.T) {
	now := time.Date(2026, time.August, 17, 2, 0, 0, 0, time.UTC)
	at := now.Add(30 * time.Minute)
	for _, test := range []struct {
		name       string
		definition Definition
		want       time.Time
	}{
		{name: "at", definition: Definition{Kind: KindAt, Spec: Spec{At: &at}}, want: at},
		{name: "every", definition: Definition{Kind: KindEvery, Spec: Spec{EverySeconds: 3600}}, want: now.Add(time.Hour)},
		{name: "cron", definition: Definition{Kind: KindCron, Spec: Spec{Expression: "30 9 * * *"}, Timezone: "Asia/Hong_Kong"}, want: time.Date(2026, 8, 17, 1, 30, 0, 0, time.UTC).Add(24 * time.Hour)},
		{name: "adaptive", definition: Definition{Kind: KindAdaptive, Spec: Spec{MinimumSeconds: 900, MaximumSeconds: 14400, DefaultSeconds: 3600}}, want: now.Add(time.Hour)},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := First(test.definition, now)
			if err != nil || !got.Equal(test.want) {
				t.Fatalf("First() = %v, %v; want %v", got, err, test.want)
			}
		})
	}
}

func TestRecurringSchedulesSkipMissedOccurrences(t *testing.T) {
	anchor := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	completed := anchor.Add(10*time.Hour + 10*time.Minute)
	next, err := Next(Definition{Kind: KindEvery, Spec: Spec{EverySeconds: 3600, Anchor: &anchor}}, completed, 0)
	if err != nil || next == nil || !next.Equal(anchor.Add(11*time.Hour)) {
		t.Fatalf("Next() = %v, %v", next, err)
	}
}

func TestIntervalWithAncientAnchorStillAdvancesPastNow(t *testing.T) {
	anchor := time.Date(1000, time.January, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, time.August, 17, 0, 0, 0, 0, time.UTC)
	next, err := First(Definition{Kind: KindEvery, Spec: Spec{EverySeconds: 86400, Anchor: &anchor}}, now)
	want := now.Add(24 * time.Hour)
	if err != nil || !next.Equal(want) {
		t.Fatalf("First() = %v, %v; want %v", next, err, want)
	}
}

func TestAdaptiveProposalIsClamped(t *testing.T) {
	definition := Definition{Kind: KindAdaptive, Spec: Spec{MinimumSeconds: 900, MaximumSeconds: 7200, DefaultSeconds: 3600}}
	completed := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	next, err := Next(definition, completed, time.Minute)
	if err != nil || next == nil || !next.Equal(completed.Add(15*time.Minute)) {
		t.Fatalf("minimum clamp = %v, %v", next, err)
	}
	next, err = Next(definition, completed, 12*time.Hour)
	if err != nil || next == nil || !next.Equal(completed.Add(2*time.Hour)) {
		t.Fatalf("maximum clamp = %v, %v", next, err)
	}
}

func TestScheduleValidationRejectsCrossKindFields(t *testing.T) {
	now := time.Now().UTC()
	if _, err := Normalize(Definition{Kind: KindEvery, Spec: Spec{EverySeconds: 60}, Timezone: "UTC"}, now); !errors.Is(err, ErrInvalidSchedule) {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := Normalize(Definition{Kind: KindCron, Spec: Spec{Expression: "invalid"}, Timezone: "UTC"}, now); !errors.Is(err, ErrInvalidSchedule) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCalendarScheduleKeepsLocalTimeAcrossDST(t *testing.T) {
	definition := Definition{
		Kind: KindCron, Spec: Spec{Expression: "0 9 * * *"}, Timezone: "America/New_York",
	}
	after := time.Date(2026, time.March, 7, 15, 0, 0, 0, time.UTC)
	next, err := First(definition, after)
	want := time.Date(2026, time.March, 8, 13, 0, 0, 0, time.UTC)
	if err != nil || !next.Equal(want) {
		t.Fatalf("DST next = %v, %v; want %v", next, err, want)
	}
}
