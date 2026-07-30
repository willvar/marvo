package handler

import (
	"marvo/config"
	"marvo/internal/search"
	"marvo/internal/store"
	"marvo/internal/ws"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/websocket/v2"
)

type Dependencies struct {
	Config    *config.Config
	NoteStore *store.NoteStore
	Search    *search.Index
	Hub       *ws.Hub
}

func RegisterRoutes(app *fiber.App, deps *Dependencies) {
	api := app.Group("/api")

	api.Post("/auth/verify", deps.Verify)
	api.Post("/auth", deps.Login)
	api.Post("/auth/logout", deps.Logout)

	api.Use(deps.AuthMiddleware())

	api.Get("/notes", deps.ListNotes)
	api.Post("/notes", deps.CreateNote)
	api.Get("/notes/:title", deps.GetNote)
	api.Put("/notes/:title/content", deps.UpdateNoteContent)
	api.Put("/notes/:title/meta", deps.UpdateNoteMeta)
	api.Put("/notes/:title/rename", deps.RenameNote)
	api.Delete("/notes/:title", deps.DeleteNote)
	api.Get("/notes/:title/assets/:filename", deps.GetAttachment)
	api.Post("/notes/:title/assets", deps.UploadAttachment)

	api.Get("/search", deps.SearchNotes)

	api.Use("/ws", ws.UpgradeHandler())
	api.Get("/ws", websocket.New(deps.HandleWebSocket))
}
