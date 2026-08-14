package userspace

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"marvo/internal/control"
)

type Layout struct {
	root string
}

type Paths struct {
	Root      string
	App       string
	Workspace string
	Agent     string
}

func OpenLayout(root string) (*Layout, error) {
	root = filepath.Clean(root)
	if !filepath.IsAbs(root) {
		return nil, errors.New("state root must be absolute")
	}
	if err := ensurePrivateDirectory(root); err != nil {
		return nil, fmt.Errorf("initialize state root: %w", err)
	}
	for _, name := range []string{"control", "users"} {
		if err := ensurePrivateDirectory(filepath.Join(root, name)); err != nil {
			return nil, fmt.Errorf("initialize %s directory: %w", name, err)
		}
	}
	return &Layout{root: root}, nil
}

func (l *Layout) ControlDatabase() string {
	return filepath.Join(l.root, "control", "platform.sqlite")
}

// AndroidReleaseDirectory is the global Android distribution boundary. APKs
// are platform artifacts, not user content, so they live beside the control
// database instead of inside any user's isolated space.
func (l *Layout) AndroidReleaseDirectory() string {
	return filepath.Join(l.root, "control", "android")
}

func (l *Layout) UserPaths(userID string) (Paths, error) {
	if l == nil || !control.ValidateUserID(userID) {
		return Paths{}, errors.New("invalid user id")
	}
	root := filepath.Join(l.root, "users", userID)
	return Paths{
		Root:      root,
		App:       filepath.Join(root, "app"),
		Workspace: filepath.Join(root, "workspace"),
		Agent:     filepath.Join(root, "agent"),
	}, nil
}

func (l *Layout) EnsureUser(userID string) (Paths, error) {
	paths, err := l.UserPaths(userID)
	if err != nil {
		return Paths{}, err
	}
	usersRoot := filepath.Join(l.root, "users")
	if err := requirePrivateDirectory(usersRoot); err != nil {
		return Paths{}, fmt.Errorf("inspect users directory: %w", err)
	}
	for _, path := range []string{paths.Root, paths.App, paths.Workspace, paths.Agent} {
		if err := ensurePrivateDirectory(path); err != nil {
			return Paths{}, fmt.Errorf("initialize user directory: %w", err)
		}
	}
	return paths, nil
}

// UserUsage returns the bytes occupied by regular files inside one complete
// user boundary. WalkDir does not follow symbolic links, so a link cannot make
// another user's or a host directory's contents count toward this space.
func (l *Layout) UserUsage(userID string) (int64, error) {
	paths, err := l.UserPaths(userID)
	if err != nil {
		return 0, err
	}
	var used int64
	err = filepath.WalkDir(paths.Root, func(_ string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, os.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if info.Mode().IsRegular() {
			used += info.Size()
		}
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	return used, err
}

func ensurePrivateDirectory(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(path, 0700); err != nil {
			return err
		}
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("path is not a regular directory")
	}
	return os.Chmod(path, 0700)
}

func requirePrivateDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("path is not a regular directory")
	}
	return nil
}
