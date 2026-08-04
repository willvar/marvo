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

type NoteChangeHandler func(title string)
type ListChangeHandler func()
type ThemeChangeHandler func()

type NoteWatcher struct {
	watcher       *fsnotify.Watcher
	dataDir       string
	mu            sync.Mutex
	tasks         map[string]*debounceTask
	closed        bool
	done          chan struct{}
	wg            sync.WaitGroup
	closeErr      error
	once          sync.Once
	onNoteChange  NoteChangeHandler
	onListChange  ListChangeHandler
	onThemeChange ThemeChangeHandler
}

type debounceTask struct {
	cancel context.CancelFunc
}

func WatchNotes(dataDir string, onNoteChange NoteChangeHandler, onListChange ListChangeHandler, onThemeChange ThemeChangeHandler) (*NoteWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	dataDir = filepath.Clean(dataDir)
	addDirs(dataDir, w)

	nw := &NoteWatcher{
		watcher:       w,
		dataDir:       dataDir,
		tasks:         make(map[string]*debounceTask),
		done:          make(chan struct{}),
		onNoteChange:  onNoteChange,
		onListChange:  onListChange,
		onThemeChange: onThemeChange,
	}

	go nw.run()
	return nw, nil
}

func (nw *NoteWatcher) run() {
	for {
		select {
		case <-nw.done:
			return
		case event, ok := <-nw.watcher.Events:
			if !ok {
				return
			}
			nw.handle(event)
		case err, ok := <-nw.watcher.Errors:
			if !ok {
				return
			}
			slog.Error("note watcher error", "error", err)
		}
	}
}

func (nw *NoteWatcher) handle(event fsnotify.Event) {
	clean := filepath.Clean(event.Name)
	if clean == filepath.Join(nw.dataDir, "theme.json") {
		if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) != 0 {
			nw.notifyThemeChange()
		}
		return
	}
	if isHiddenDataPath(nw.dataDir, clean) {
		return
	}

	if event.Op&fsnotify.Create != 0 {
		if info, err := os.Lstat(clean); err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			addDirs(clean, nw.watcher)
			nw.notifyListChange()
			// A restore or out-of-band rename can place a complete note
			// directory into dataDir in one operation. Existing files inside it
			// do not emit a second Create event after the watch is attached.
			if rel, relErr := filepath.Rel(nw.dataDir, clean); relErr == nil &&
				rel != "." && !strings.HasPrefix(rel, "..") &&
				!strings.Contains(filepath.ToSlash(rel), "/") {
				nw.schedule(filepath.ToSlash(rel))
			}
		}
	}
	if event.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
		nw.notifyListChange()
	}

	base := filepath.Base(clean)
	if base != "index.md" && base != "meta.json" {
		return
	}
	if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) == 0 {
		return
	}
	rel, err := filepath.Rel(nw.dataDir, filepath.Dir(clean))
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") || strings.Contains(filepath.ToSlash(rel), "/") {
		return
	}
	nw.schedule(filepath.ToSlash(rel))
	if base == "meta.json" || event.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
		nw.notifyListChange()
	}
}

func isHiddenDataPath(dataDir, path string) bool {
	rel, err := filepath.Rel(dataDir, path)
	if err != nil || rel == "." {
		return false
	}
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}

func (nw *NoteWatcher) schedule(title string) {
	nw.mu.Lock()
	if nw.closed {
		nw.mu.Unlock()
		return
	}
	if old := nw.tasks[title]; old != nil {
		old.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	task := &debounceTask{cancel: cancel}
	nw.tasks[title] = task
	nw.wg.Add(1)
	nw.mu.Unlock()

	go func() {
		defer nw.wg.Done()
		timer := time.NewTimer(200 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return
		}

		nw.mu.Lock()
		if nw.tasks[title] == task {
			delete(nw.tasks, title)
		}
		closed := nw.closed
		handler := nw.onNoteChange
		nw.mu.Unlock()
		if closed || handler == nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		default:
			handler(title)
		}
	}()
}

func (nw *NoteWatcher) notifyListChange() {
	nw.mu.Lock()
	if nw.closed {
		nw.mu.Unlock()
		return
	}
	handler := nw.onListChange
	nw.mu.Unlock()
	if handler != nil {
		handler()
	}
}

func (nw *NoteWatcher) notifyThemeChange() {
	nw.mu.Lock()
	if nw.closed {
		nw.mu.Unlock()
		return
	}
	handler := nw.onThemeChange
	nw.mu.Unlock()
	if handler != nil {
		handler()
	}
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

func addDirs(root string, watcher *fsnotify.Watcher) {
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if path != root && strings.HasPrefix(info.Name(), ".") {
				return filepath.SkipDir
			}
			_ = watcher.Add(path)
		}
		return nil
	})
}

func MustJSON(v any) []byte {
	data, _ := json.Marshal(v)
	return data
}
