package store

import (
	"errors"
	"strings"
	"testing"
)

func TestAgentSettingsStorePersistsInUserState(t *testing.T) {
	state, _ := newTestStateDB(t)
	settingsStore, err := NewAgentSettingsStore(state)
	if err != nil {
		t.Fatal(err)
	}
	selection := &AgentModelSelection{ProviderID: " provider ", ModelID: " model/vision "}
	if err := settingsStore.Save(AgentSettings{Model: selection, Variant: " high ", GlobalPrompt: "默认使用中文"}); err != nil {
		t.Fatal(err)
	}
	if selection.ProviderID != " provider " || selection.ModelID != " model/vision " {
		t.Fatal("Save mutated its caller's model selection")
	}

	settings := settingsStore.Get()
	if settings.Model == nil || settings.Model.ProviderID != "provider" || settings.Model.ModelID != "model/vision" {
		t.Fatalf("stored model = %#v", settings.Model)
	}
	if settings.Variant != "high" || settings.GlobalPrompt != "默认使用中文" {
		t.Fatalf("stored settings = %#v", settings)
	}
	settings.Model.ModelID = "mutated"
	if got := settingsStore.Get().Model.ModelID; got != "model/vision" {
		t.Fatalf("Get returned shared mutable state: %q", got)
	}

	reloaded, err := NewAgentSettingsStore(state)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Get(); got.Model == nil || got.Model.ModelID != "model/vision" || got.Variant != "high" || got.GlobalPrompt != "默认使用中文" {
		t.Fatalf("reloaded settings = %#v", got)
	}
}

func TestAgentSettingsStoreRejectsInvalidContent(t *testing.T) {
	state, _ := newTestStateDB(t)
	settingsStore, err := NewAgentSettingsStore(state)
	if err != nil {
		t.Fatal(err)
	}
	for _, settings := range []AgentSettings{
		{Model: &AgentModelSelection{ProviderID: "", ModelID: "model"}},
		{Model: &AgentModelSelection{ProviderID: "provider", ModelID: ""}},
		{Variant: "high"},
		{Model: &AgentModelSelection{ProviderID: "provider", ModelID: "model"}, Variant: strings.Repeat("x", maxAgentVariantBytes+1)},
		{GlobalPrompt: strings.Repeat("界", MaxGlobalPromptBytes)},
	} {
		if err := settingsStore.Save(settings); !errors.Is(err, ErrInvalidAgentSettings) {
			t.Fatalf("Save(%#v) error = %v, want ErrInvalidAgentSettings", settings, err)
		}
	}
}
