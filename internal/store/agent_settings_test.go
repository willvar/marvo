package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentSettingsStorePersistsPrivateCopy(t *testing.T) {
	dataDir := t.TempDir()
	settingsStore, err := NewAgentSettingsStore(dataDir)
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
	if settings.Variant != "high" {
		t.Fatalf("stored variant = %q, want high", settings.Variant)
	}
	settings.Model.ModelID = "mutated"
	if got := settingsStore.Get().Model.ModelID; got != "model/vision" {
		t.Fatalf("Get returned shared mutable state: %q", got)
	}

	path := filepath.Join(dataDir, agentSettingsFilename)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("settings mode = %o, want 600", info.Mode().Perm())
	}

	reloaded, err := NewAgentSettingsStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Get(); got.Model == nil || got.Model.ModelID != "model/vision" || got.Variant != "high" || got.GlobalPrompt != "默认使用中文" {
		t.Fatalf("reloaded settings = %#v", got)
	}
}

func TestAgentSettingsStoreRejectsInvalidContent(t *testing.T) {
	settingsStore, err := NewAgentSettingsStore(t.TempDir())
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

func TestAgentSettingsStoreRejectsSymlinkAndUnknownFields(t *testing.T) {
	dataDir := t.TempDir()
	target := filepath.Join(dataDir, "target.json")
	if err := os.WriteFile(target, []byte(`{"global_prompt":"untouched"}`), 0600); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(dataDir, agentSettingsFilename)
	if err := os.Symlink(target, settingsPath); err != nil {
		t.Fatal(err)
	}
	if _, err := NewAgentSettingsStore(dataDir); !errors.Is(err, ErrInvalidAgentSettings) {
		t.Fatalf("symlink load error = %v, want ErrInvalidAgentSettings", err)
	}

	if err := os.Remove(settingsPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, []byte(`{"global_prompt":"","unknown":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewAgentSettingsStore(dataDir); !errors.Is(err, ErrInvalidAgentSettings) {
		t.Fatalf("unknown field load error = %v, want ErrInvalidAgentSettings", err)
	}
}
