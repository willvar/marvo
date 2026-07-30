package handler

import (
	"marvo/internal/search"
	"strconv"

	"github.com/gofiber/fiber/v2"
)

func (d *Dependencies) SearchNotes(c *fiber.Ctx) error {
	query := c.Query("q")
	if query == "" {
		return c.Status(400).JSON(fiber.Map{"error": "query parameter 'q' is required"})
	}

	limit, _ := strconv.Atoi(c.Query("limit", "20"))

	results, err := d.Search.Search(query, limit)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	if results == nil {
		results = make([]search.SearchResult, 0)
	}

	return c.JSON(fiber.Map{"results": results})
}


