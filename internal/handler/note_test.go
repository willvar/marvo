package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"marvo/internal/store"
)

func TestValidAttachmentFilename(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     bool
	}{
		{name: "plain", filename: "image.png", want: true},
		{name: "spaces", filename: "my image.png", want: true},
		{name: "unicode", filename: "截图.png", want: true},
		{name: "empty", filename: "", want: false},
		{name: "parent path", filename: "../image.png", want: false},
		{name: "nested path", filename: "assets/image.png", want: false},
		{name: "windows path", filename: `assets\image.png`, want: false},
		{name: "parent marker", filename: "image..png", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validAttachmentFilename(tt.filename); got != tt.want {
				t.Fatalf("validAttachmentFilename(%q) = %v, want %v", tt.filename, got, tt.want)
			}
		})
	}
}

func TestAttachmentURLEscapesPathSegments(t *testing.T) {
	got := attachmentURL("b8c42977bc4e49779e04", "中文 note", "my image #1.png")
	want := "/api/user/b8c42977bc4e49779e04/notes/%E4%B8%AD%E6%96%87%20note/assets/my%20image%20%231.png"
	if got != want {
		t.Fatalf("attachmentURL() = %q, want %q", got, want)
	}
}

func TestBrowserContentWriteUsesRevision(t *testing.T) {
	noteStore := store.NewNoteStore(t.TempDir())
	snapshot, err := noteStore.CreateNote("busy-note", "Agent base", nil)
	if err != nil {
		t.Fatal(err)
	}
	deps := &Dependencies{NoteStore: noteStore}
	body, _ := json.Marshal(map[string]string{
		"content":        "browser overwrite",
		"base_revision":  snapshot.ContentRevision,
		"instance_token": snapshot.InstanceToken,
	})
	request := httptest.NewRequest(http.MethodPut, "/api/notes/busy-note/content", bytes.NewReader(body))
	request.SetPathValue("title", "busy-note")
	response := httptest.NewRecorder()
	deps.UpdateNoteContent(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("UpdateNoteContent() status = %d, body = %s", response.Code, response.Body.String())
	}
	current, err := noteStore.Snapshot("busy-note")
	if err != nil || current.Content != "browser overwrite" {
		t.Fatalf("content after accepted write = %q, error = %v", current.Content, err)
	}

	staleBody, _ := json.Marshal(map[string]string{
		"content":        "stale browser overwrite",
		"base_revision":  snapshot.ContentRevision,
		"instance_token": snapshot.InstanceToken,
	})
	staleRequest := httptest.NewRequest(http.MethodPut, "/api/notes/busy-note/content", bytes.NewReader(staleBody))
	staleRequest.SetPathValue("title", "busy-note")
	staleResponse := httptest.NewRecorder()
	deps.UpdateNoteContent(staleResponse, staleRequest)
	if staleResponse.Code != http.StatusConflict {
		t.Fatalf("stale UpdateNoteContent() status = %d, want 409", staleResponse.Code)
	}
	current, err = noteStore.Snapshot("busy-note")
	if err != nil || current.Content != "browser overwrite" {
		t.Fatalf("content after stale write = %q, error = %v", current.Content, err)
	}
}
