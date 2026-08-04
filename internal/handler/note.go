package handler

import (
	"errors"
	"io"
	"marvo/internal/media"
	"marvo/internal/store"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const maxNoteJSONBytes = 32 << 20

func noteTitle(r *http.Request) (string, error) {
	title, err := url.PathUnescape(r.PathValue("title"))
	if err != nil {
		return "", errors.New("invalid title")
	}
	if err := store.ValidateTitle(title); err != nil {
		return "", err
	}
	return title, nil
}

func (d *Dependencies) ListNotes(w http.ResponseWriter, _ *http.Request) {
	notes, err := d.NoteStore.List()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to list notes"})
		return
	}
	writeJSON(w, http.StatusOK, notes)
}

func (d *Dependencies) CreateNote(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxNoteJSONBytes)
	var body struct {
		Title   string   `json:"title"`
		Content string   `json:"content"`
		Tags    []string `json:"tags"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request"})
		return
	}
	body.Title = strings.TrimSpace(body.Title)
	if err := store.ValidateTitle(body.Title); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	snapshot, err := d.NoteStore.CreateNote(body.Title, body.Content, body.Tags)
	if err != nil {
		if errors.Is(err, store.ErrNoteAlreadyExists) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "note already exists", "code": "title_conflict"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to create note"})
		return
	}
	writeJSON(w, http.StatusCreated, snapshot)
}

func (d *Dependencies) GetNote(w http.ResponseWriter, r *http.Request) {
	title, err := noteTitle(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	snapshot, err := d.NoteStore.Snapshot(title)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "note not found"})
		return
	}
	w.Header().Set("ETag", `"`+snapshot.ContentRevision+`"`)
	writeJSON(w, http.StatusOK, snapshot)
}

func (d *Dependencies) UpdateNoteContent(w http.ResponseWriter, r *http.Request) {
	title, err := noteTitle(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxNoteJSONBytes)
	var body struct {
		Content       string `json:"content"`
		BaseRevision  string `json:"base_revision"`
		InstanceToken string `json:"instance_token"`
	}
	if err := readJSON(r, &body); err != nil || body.BaseRevision == "" || body.InstanceToken == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "content, base_revision and instance_token are required"})
		return
	}
	snapshot, err := d.NoteStore.UpdateContentCAS(title, body.InstanceToken, body.BaseRevision, body.Content)
	if err != nil {
		d.writeNoteError(w, err)
		return
	}
	if d.Media != nil {
		d.Media.ReconcileNote(title, body.InstanceToken)
	}
	w.Header().Set("ETag", `"`+snapshot.ContentRevision+`"`)
	writeJSON(w, http.StatusOK, snapshot)
}

func (d *Dependencies) UpdateNoteMeta(w http.ResponseWriter, r *http.Request) {
	title, err := noteTitle(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body struct {
		Tags          *[]string `json:"tags"`
		BaseRevision  string    `json:"base_revision"`
		InstanceToken string    `json:"instance_token"`
	}
	if err := readJSON(r, &body); err != nil || body.BaseRevision == "" || body.InstanceToken == "" || body.Tags == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "metadata patch, base_revision and instance_token are required"})
		return
	}
	snapshot, err := d.NoteStore.UpdateMetaCAS(title, body.InstanceToken, body.BaseRevision, store.MetaUpdate{Tags: body.Tags})
	if err != nil {
		d.writeNoteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (d *Dependencies) RenameNote(w http.ResponseWriter, r *http.Request) {
	oldTitle, err := noteTitle(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	var body struct {
		NewTitle      string `json:"new_title"`
		InstanceToken string `json:"instance_token"`
	}
	if err := readJSON(r, &body); err != nil || body.NewTitle == "" || body.InstanceToken == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "new_title and instance_token are required"})
		return
	}
	if d.Media != nil && d.Media.HasBusyAssets(oldTitle, body.InstanceToken) {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": "media upload or conversion is still running",
			"code":  "media_busy",
		})
		return
	}
	snapshot, err := d.NoteStore.RenameCAS(oldTitle, body.NewTitle, body.InstanceToken)
	if err != nil {
		d.writeNoteError(w, err)
		return
	}
	d.Hub.MoveNote(oldTitle, body.NewTitle)
	d.Hub.BroadcastToNote(body.NewTitle, "", mustJSON(map[string]any{
		"action":         "note_moved",
		"old_title":      oldTitle,
		"new_title":      body.NewTitle,
		"instance_token": snapshot.InstanceToken,
		"note":           snapshot.Note,
	}))
	writeJSON(w, http.StatusOK, snapshot)
}

func (d *Dependencies) DeleteNote(w http.ResponseWriter, r *http.Request) {
	title, err := noteTitle(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	var body struct {
		InstanceToken string `json:"instance_token"`
	}
	if err := readJSON(r, &body); err != nil || body.InstanceToken == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "instance_token is required"})
		return
	}
	if d.Media != nil && d.Media.HasBusyAssets(title, body.InstanceToken) {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": "media upload or conversion is still running; remove its placeholder first",
			"code":  "media_busy",
		})
		return
	}
	entry, err := d.NoteStore.TrashCAS(title, body.InstanceToken)
	if err != nil {
		d.writeNoteError(w, err)
		return
	}
	d.Hub.BroadcastAll(mustJSON(map[string]any{"action": "note_trashed", "title": title, "trash": entry}))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "trash": entry})
}

func (d *Dependencies) ListTrash(w http.ResponseWriter, _ *http.Request) {
	entries, err := d.NoteStore.ListTrash()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to list trash"})
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func (d *Dependencies) RestoreTrash(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		NewTitle string `json:"new_title"`
	}
	if err := readJSON(r, &body); err != nil || body.NewTitle == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "new_title is required"})
		return
	}
	snapshot, err := d.NoteStore.RestoreTrash(id, body.NewTitle)
	if err != nil {
		d.writeNoteError(w, err)
		return
	}
	d.Hub.BroadcastAll(mustJSON(map[string]any{"action": "note_restored", "title": body.NewTitle}))
	writeJSON(w, http.StatusOK, snapshot)
}

func (d *Dependencies) PermanentlyDeleteTrash(w http.ResponseWriter, r *http.Request) {
	if err := d.NoteStore.PermanentlyDeleteTrash(r.PathValue("id")); err != nil {
		if errors.Is(err, store.ErrNoteNotFound) || errors.Is(err, os.ErrNotExist) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "trash entry not found"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "failed to permanently delete trash entry"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (d *Dependencies) EmptyTrash(w http.ResponseWriter, _ *http.Request) {
	removed, err := d.NoteStore.EmptyTrash()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to empty trash"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "removed": removed})
}

func (d *Dependencies) writeNoteError(w http.ResponseWriter, err error) {
	var conflict *store.ConflictError
	if errors.As(err, &conflict) {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":    "note changed",
			"code":     conflict.Kind,
			"moved_to": conflict.MovedTo,
			"current":  conflict.Current,
		})
		return
	}
	switch {
	case errors.Is(err, store.ErrNoteNotFound):
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "note not found"})
	case errors.Is(err, store.ErrNoteAlreadyExists):
		writeJSON(w, http.StatusConflict, map[string]any{"error": "title already exists", "code": "title_conflict"})
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
}

func (d *Dependencies) GetAttachment(w http.ResponseWriter, r *http.Request) {
	title, err := noteTitle(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid asset path"})
		return
	}
	filename := r.PathValue("filename")
	if !validAttachmentFilename(filename) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid asset filename"})
		return
	}
	file, info, err := d.NoteStore.OpenAttachment(title, filename)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "asset not found"})
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", guessContentType(filename))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, filename, info.ModTime(), file)
}

func (d *Dependencies) ReserveMediaAsset(w http.ResponseWriter, r *http.Request) {
	title, err := noteTitle(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid note title"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var body struct {
		AssetID       string `json:"asset_id"`
		OriginalName  string `json:"original_name"`
		ContentType   string `json:"content_type"`
		InstanceToken string `json:"instance_token"`
	}
	if err := readJSON(r, &body); err != nil || body.OriginalName == "" || body.InstanceToken == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "original_name and instance_token are required"})
		return
	}
	asset, err := d.Media.Reserve(title, body.InstanceToken, body.AssetID, body.OriginalName, body.ContentType)
	if err != nil {
		d.writeMediaError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, mediaAssetPayload(title, *asset))
}

func (d *Dependencies) ListMediaAssets(w http.ResponseWriter, r *http.Request) {
	title, token, ok := mediaRequestIdentity(w, r)
	if !ok {
		return
	}
	assets, err := d.Media.List(title, token)
	if err != nil {
		d.writeMediaError(w, err)
		return
	}
	payload := make([]map[string]any, 0, len(assets))
	for _, asset := range assets {
		payload = append(payload, mediaAssetPayload(title, asset))
	}
	writeJSON(w, http.StatusOK, payload)
}

func (d *Dependencies) GetMediaAsset(w http.ResponseWriter, r *http.Request) {
	title, token, ok := mediaRequestIdentity(w, r)
	if !ok {
		return
	}
	asset, err := d.Media.Get(title, token, r.PathValue("assetID"))
	if err != nil {
		d.writeMediaError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, mediaAssetPayload(title, *asset))
}

func (d *Dependencies) UploadMediaAsset(w http.ResponseWriter, r *http.Request) {
	title, token, ok := mediaRequestIdentity(w, r)
	if !ok {
		return
	}
	deadlineReader := &uploadDeadlineReader{
		source:     r.Body,
		controller: http.NewResponseController(w),
	}
	asset, err := d.Media.Upload(r.Context(), title, token, r.PathValue("assetID"), r.ContentLength, deadlineReader)
	_ = deadlineReader.controller.SetReadDeadline(time.Time{})
	if err != nil {
		d.writeMediaError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, mediaAssetPayload(title, *asset))
}

func (d *Dependencies) AbandonMediaAsset(w http.ResponseWriter, r *http.Request) {
	title, token, ok := mediaRequestIdentity(w, r)
	if !ok {
		return
	}
	asset, err := d.Media.Abandon(title, token, r.PathValue("assetID"))
	if err != nil {
		d.writeMediaError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, mediaAssetPayload(title, *asset))
}

func mediaRequestIdentity(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	title, err := noteTitle(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid note title"})
		return "", "", false
	}
	token := strings.TrimSpace(r.Header.Get("X-Marvo-Instance-Token"))
	if token == "" {
		token = strings.TrimSpace(r.URL.Query().Get("instance_token"))
	}
	if token == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "instance token is required"})
		return "", "", false
	}
	return title, token, true
}

func mediaAssetPayload(title string, asset media.Asset) map[string]any {
	payload := map[string]any{
		"id":            asset.ID,
		"kind":          asset.Kind,
		"state":         asset.State,
		"original_name": asset.OriginalName,
		"content_type":  asset.ContentType,
		"filename":      asset.Filename,
		"error":         asset.Error,
		"created_at":    asset.CreatedAt,
		"updated_at":    asset.UpdatedAt,
	}
	if asset.State == media.StateReady && asset.Filename != "" {
		payload["url"] = attachmentURL(title, asset.Filename)
	}
	return payload
}

func (d *Dependencies) writeMediaError(w http.ResponseWriter, err error) {
	var conflict *store.ConflictError
	switch {
	case errors.As(err, &conflict):
		d.writeNoteError(w, err)
	case errors.Is(err, os.ErrNotExist):
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "media asset not found"})
	case errors.Is(err, media.ErrUnsupportedMedia):
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]any{"error": err.Error()})
	case errors.Is(err, media.ErrInsufficientStorage):
		writeJSON(w, http.StatusInsufficientStorage, map[string]any{"error": err.Error()})
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
}

type uploadDeadlineReader struct {
	source     io.Reader
	controller *http.ResponseController
}

func (r *uploadDeadlineReader) Read(p []byte) (int, error) {
	if err := r.controller.SetReadDeadline(time.Now().Add(2 * time.Minute)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return 0, err
	}
	return r.source.Read(p)
}

func attachmentURL(title, filename string) string {
	return "/api/notes/" + url.PathEscape(title) + "/assets/" + url.PathEscape(filename)
}

func validAttachmentFilename(filename string) bool { return store.ValidAssetFilename(filename) }

func guessContentType(filename string) string {
	if contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(filename))); contentType != "" {
		return contentType
	}
	return "application/octet-stream"
}

func (d *Dependencies) SearchNotes(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, []store.NoteInfo{})
		return
	}
	notes, err := d.NoteStore.List()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "search failed"})
		return
	}
	lowerQ := strings.ToLower(q)
	type ranked struct {
		info  store.NoteInfo
		score int
	}
	results := make([]ranked, 0)
	for _, note := range notes {
		score := 0
		title := strings.ToLower(note.Title)
		if title == lowerQ {
			score += 100
		} else if strings.Contains(title, lowerQ) {
			score += 50
		}
		if strings.Contains(strings.ToLower(strings.Join(note.Tags, " ")), lowerQ) {
			score += 30
		}
		_, content, getErr := d.NoteStore.Get(note.Title)
		if getErr == nil {
			lowerContent := strings.ToLower(content)
			if strings.Contains(lowerContent, lowerQ) {
				score += 20 + strings.Count(lowerContent, lowerQ)
			}
		}
		if score > 0 {
			results = append(results, ranked{info: note, score: score})
		}
	}
	sort.SliceStable(results, func(i, j int) bool { return results[i].score > results[j].score })
	out := make([]store.NoteInfo, len(results))
	for i := range results {
		out[i] = results[i].info
	}
	writeJSON(w, http.StatusOK, out)
}
