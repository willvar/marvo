package runtimeevents

import "marvo/internal/userid"

type Kind string

const (
	KindActivity      Kind = "activity"
	KindMemories      Kind = "memories"
	KindSpace         Kind = "space"
	KindAgentSettings Kind = "agent_settings"
	KindDevices       Kind = "devices"
	KindSchedules     Kind = "schedules"
)

type Event struct {
	UserID string `json:"user_id"`
	Kind   Kind   `json:"kind"`
}

func (e Event) Valid() bool {
	if !userid.Valid(e.UserID) {
		return false
	}
	switch e.Kind {
	case KindActivity, KindMemories, KindSpace, KindAgentSettings, KindDevices, KindSchedules:
		return true
	default:
		return false
	}
}
