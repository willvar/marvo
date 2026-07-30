package handler

import (
	"io"
	"marvo/internal/store"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/gofiber/fiber/v2"
)

func noteTitle(c *fiber.Ctx) string {
	t, _ := url.PathUnescape(c.Params("title"))
	return t
}

func (d *Dependencies) ListNotes(c *fiber.Ctx) error {
	notes, err := d.NoteStore.List()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	if notes == nil {
		notes = []store.NoteInfo{}
	}
	return c.JSON(notes)
}

func (d *Dependencies) CreateNote(c *fiber.Ctx) error {
	var body struct {
		Title   string   `json:"title"`
		Content string   `json:"content"`
		Tags    []string `json:"tags"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request"})
	}

	if err := store.ValidateTitle(body.Title); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}

	if d.NoteStore.Exists(body.Title) {
		return c.Status(409).JSON(fiber.Map{"error": "note already exists"})
	}

	if err := d.NoteStore.Create(body.Title, body.Content, body.Tags); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	d.Hub.OT.InitDocument(body.Title, body.Content)

	note, content, err := d.NoteStore.Get(body.Title)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	d.Search.IndexAsync(body.Title, body.Content, func(err error) {
		logAction("failed to index note", "title", body.Title, "error", err)
	})

	return c.Status(201).JSON(fiber.Map{
		"note":    note,
		"content": content,
		"version": d.Hub.OT.GetDocument(body.Title).Version,
	})
}

func (d *Dependencies) GetNote(c *fiber.Ctx) error {
	title := noteTitle(c)
	note, content, err := d.NoteStore.Get(title)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "note not found"})
	}
	doc := d.Hub.OT.InitDocument(title, content)
	return c.JSON(fiber.Map{
		"note":    note,
		"content": content,
		"version": doc.Version,
	})
}

func (d *Dependencies) UpdateNoteContent(c *fiber.Ctx) error {
	title := noteTitle(c)

	var body struct {
		Content string `json:"content"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request"})
	}

	if !d.NoteStore.Exists(title) {
		return c.Status(404).JSON(fiber.Map{"error": "note not found"})
	}

	if err := d.NoteStore.UpdateContent(title, body.Content); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	doc := d.Hub.OT.ResetDocument(title, body.Content)

	d.Search.IndexAsync(title, body.Content, func(err error) {
		logAction("failed to update search index", "title", title, "error", err)
	})

	d.Hub.BroadcastToNote(title, "", store.MustJSON(fiber.Map{
		"action":  "ot_snapshot",
		"title":   title,
		"content": body.Content,
		"version": doc.Version,
	}))

	return c.JSON(fiber.Map{"ok": true})
}

func (d *Dependencies) UpdateNoteMeta(c *fiber.Ctx) error {
	title := noteTitle(c)

	var body struct {
		Tags []string `json:"tags"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request"})
	}

	if err := d.NoteStore.UpdateMeta(title, body.Tags); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"ok": true})
}

func (d *Dependencies) RenameNote(c *fiber.Ctx) error {
	oldTitle := noteTitle(c)

	var body struct {
		NewTitle string `json:"new_title"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request"})
	}

	if body.NewTitle == "" {
		return c.Status(400).JSON(fiber.Map{"error": "new_title is required"})
	}

	content, err := func() (string, error) {
		_, content, err := d.NoteStore.Get(oldTitle)
		if err != nil {
			return "", err
		}
		return content, nil
	}()
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "note not found"})
	}

	if err := d.NoteStore.Rename(oldTitle, body.NewTitle); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	d.Search.RunAsync(func() {
		if err := d.Search.Delete(oldTitle); err != nil {
			logAction("failed to delete old search index", "title", oldTitle, "error", err)
		}
		if err := d.Search.Index(body.NewTitle, content); err != nil {
			logAction("failed to index renamed note", "title", body.NewTitle, "error", err)
		}
	})

	return c.JSON(fiber.Map{"ok": true, "new_title": body.NewTitle})
}

func (d *Dependencies) DeleteNote(c *fiber.Ctx) error {
	title := noteTitle(c)

	if err := d.NoteStore.Delete(title); err != nil {
		return c.Status(404).JSON(fiber.Map{"error": err.Error()})
	}

	d.Search.DeleteAsync(title, func(err error) {
		logAction("failed to remove from search index", "title", title, "error", err)
	})

	return c.JSON(fiber.Map{"ok": true})
}

func (d *Dependencies) GetAttachment(c *fiber.Ctx) error {
	title := noteTitle(c)
	filename := c.Params("filename")

	if !validAttachmentFilename(filename) {
		return c.Status(400).JSON(fiber.Map{"error": "invalid filename"})
	}

	data, err := d.NoteStore.ReadAttachment(title, filename)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "attachment not found"})
	}

	contentType := guessContentType(filename)
	c.Set("Content-Type", contentType)
	return c.Send(data)
}

func (d *Dependencies) UploadAttachment(c *fiber.Ctx) error {
	title := noteTitle(c)

	if !d.NoteStore.Exists(title) {
		return c.Status(404).JSON(fiber.Map{"error": "note not found"})
	}

	file, err := c.FormFile("file")
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "no file provided"})
	}
	if !validAttachmentFilename(file.Filename) {
		return c.Status(400).JSON(fiber.Map{"error": "invalid filename"})
	}

	f, err := file.Open()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	if err := d.NoteStore.WriteAttachment(title, file.Filename, data); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{
		"filename": file.Filename,
		"url":      attachmentURL(title, file.Filename),
	})
}

func attachmentURL(title string, filename string) string {
	return "/api/notes/" + url.PathEscape(title) + "/assets/" + url.PathEscape(filename)
}

func validAttachmentFilename(filename string) bool {
	return filename != "" &&
		filename == filepath.Base(filename) &&
		!strings.Contains(filename, "..") &&
		!strings.ContainsAny(filename, `/\`) &&
		!filepath.IsAbs(filename)
}

func guessContentType(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	case ".webp":
		return "image/webp"
	case ".pdf":
		return "application/pdf"
	default:
		return "application/octet-stream"
	}
}
