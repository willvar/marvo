package store

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

type ChangeHandler func(title string, content string)

type NoteWatcher struct {
	watcher  *fsnotify.Watcher
	mu       sync.Mutex
	tasks    map[string]*debounceTask
	closed   bool
	done     chan struct{}
	wg       sync.WaitGroup
	closeErr error
	once     sync.Once
}

type debounceTask struct {
	cancel context.CancelFunc
}

func WatchNotes(dataDir string, handler ChangeHandler) (*NoteWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	addDirs(dataDir, w)

	nw := &NoteWatcher{
		watcher: w,
		tasks:   make(map[string]*debounceTask),
		done:    make(chan struct{}),
	}

	go func() {
		for {
			select {
			case <-nw.done:
				return
			case event, ok := <-w.Events:
				if !ok {
					return
				}
				if event.Op&fsnotify.Create != 0 {
					if fi, err := os.Stat(event.Name); err == nil && fi.IsDir() {
						_ = nw.add(event.Name)
					}
				}
				if !strings.HasSuffix(event.Name, "index.md") {
					continue
				}
				if event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
					continue
				}

				nw.schedule(event.Name, dataDir, handler)

			case err, ok := <-w.Errors:
				if !ok {
					return
				}
				slog.Error("watcher error", "error", err)
			}
		}
	}()

	return nw, nil
}

func (nw *NoteWatcher) Close() error {
	nw.once.Do(func() {
		nw.mu.Lock()
		nw.closed = true
		for _, task := range nw.tasks {
			task.cancel()
		}
		nw.mu.Unlock()

		close(nw.done)
		nw.closeErr = nw.watcher.Close()
		nw.wg.Wait()
	})

	return nw.closeErr
}

func (nw *NoteWatcher) add(path string) error {
	nw.mu.Lock()
	defer nw.mu.Unlock()
	if nw.closed {
		return nil
	}
	return nw.watcher.Add(path)
}

func (nw *NoteWatcher) schedule(path string, dataDir string, handler ChangeHandler) {
	nw.mu.Lock()
	if nw.closed {
		nw.mu.Unlock()
		return
	}
	if task, ok := nw.tasks[path]; ok {
		task.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	task := &debounceTask{cancel: cancel}
	nw.tasks[path] = task
	nw.wg.Add(1)
	nw.mu.Unlock()

	go func() {
		defer nw.wg.Done()

		timer := time.NewTimer(300 * time.Millisecond)
		defer timer.Stop()

		select {
		case <-timer.C:
		case <-ctx.Done():
			return
		}

		nw.mu.Lock()
		if nw.tasks[path] == task {
			delete(nw.tasks, path)
		}
		closed := nw.closed
		nw.mu.Unlock()
		if closed {
			return
		}

		select {
		case <-ctx.Done():
			return
		default:
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return
		}
		rel, _ := filepath.Rel(dataDir, filepath.Dir(path))
		title := filepath.ToSlash(rel)

		select {
		case <-ctx.Done():
			return
		default:
		}

		handler(title, string(content))
	}()
}

func addDirs(root string, w *fsnotify.Watcher) {
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			_ = w.Add(path)
		}
		return nil
	})
}

func MustJSON(v interface{}) []byte {
	data, _ := json.Marshal(v)
	return data
}
