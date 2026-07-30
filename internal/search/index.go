package search

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"marvo/internal/store"

	"github.com/blevesearch/bleve/v2"
)

var ErrClosed = errors.New("search index is closed")

type SearchResult struct {
	Title   string  `json:"title"`
	Content string  `json:"content"`
	Score   float64 `json:"score"`
}

type Index struct {
	bleve   bleve.Index
	dataDir string
	mu      sync.RWMutex
	jobs    sync.WaitGroup
	closing bool
	closed  bool
}

type NoteDocument struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

func NewIndex(dataDir string) (*Index, error) {
	idxPath := filepath.Join(filepath.Dir(dataDir), ".marvo_search_index")

	var idx bleve.Index
	var err error

	if _, statErr := os.Stat(idxPath); os.IsNotExist(statErr) {
		mapping := bleve.NewIndexMapping()
		idx, err = bleve.New(idxPath, mapping)
	} else {
		idx, err = bleve.Open(idxPath)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to open search index: %w", err)
	}

	return &Index{bleve: idx, dataDir: dataDir}, nil
}

func (i *Index) Index(title string, content string) error {
	i.mu.RLock()
	defer i.mu.RUnlock()
	if i.closed {
		return ErrClosed
	}

	doc := NoteDocument{Title: title, Content: content}
	return i.bleve.Index(title, doc)
}

func (i *Index) Delete(title string) error {
	i.mu.RLock()
	defer i.mu.RUnlock()
	if i.closed {
		return ErrClosed
	}

	return i.bleve.Delete(title)
}

func (i *Index) Search(query string, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 20
	}

	bleveQuery := bleve.NewQueryStringQuery(query)
	searchReq := bleve.NewSearchRequest(bleveQuery)
	searchReq.Size = limit
	searchReq.Fields = []string{"*"}

	i.mu.RLock()
	defer i.mu.RUnlock()
	if i.closed {
		return nil, ErrClosed
	}

	results, err := i.bleve.Search(searchReq)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	var searchResults []SearchResult
	for _, hit := range results.Hits {
		content := ""
		if c, ok := hit.Fields["Content"].(string); ok {
			content = c
			if len(content) > 200 {
				content = content[:200] + "..."
			}
		}
		searchResults = append(searchResults, SearchResult{
			Title:   hit.ID,
			Content: content,
			Score:   hit.Score,
		})
	}

	return searchResults, nil
}

func (i *Index) RunAsync(task func()) bool {
	i.mu.Lock()
	if i.closing || i.closed {
		i.mu.Unlock()
		return false
	}
	i.jobs.Add(1)
	i.mu.Unlock()

	go func() {
		defer i.jobs.Done()
		if task != nil {
			task()
		}
	}()

	return true
}

func (i *Index) IndexAsync(title string, content string, onError func(error)) bool {
	return i.RunAsync(func() {
		if err := i.Index(title, content); err != nil && onError != nil {
			onError(err)
		}
	})
}

func (i *Index) DeleteAsync(title string, onError func(error)) bool {
	return i.RunAsync(func() {
		if err := i.Delete(title); err != nil && onError != nil {
			onError(err)
		}
	})
}

func (i *Index) RebuildAll(noteStore *store.NoteStore) error {
	slog.Info("rebuilding search index...")
	notes, err := noteStore.List()
	if err != nil {
		return err
	}

	for _, note := range notes {
		_, content, err := noteStore.Get(note.Title)
		if err != nil {
			slog.Warn("failed to read note for indexing", "title", note.Title, "error", err)
			continue
		}
		if err := i.Index(note.Title, content); err != nil {
			slog.Warn("failed to index note", "title", note.Title, "error", err)
		}
	}

	i.mu.RLock()
	if i.closed {
		i.mu.RUnlock()
		return ErrClosed
	}
	count, _ := i.bleve.DocCount()
	i.mu.RUnlock()
	slog.Info("search index rebuilt", "documents", strconv.Itoa(int(count)))
	return nil
}

func (i *Index) Close() error {
	i.mu.Lock()
	if i.closed {
		i.mu.Unlock()
		return nil
	}
	i.closing = true
	i.mu.Unlock()

	i.jobs.Wait()

	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed {
		return nil
	}
	i.closed = true
	return i.bleve.Close()
}
