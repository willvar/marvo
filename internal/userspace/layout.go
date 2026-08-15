package userspace

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"marvo/internal/agentcredentials"
	"marvo/internal/control"
)

type Layout struct {
	root string
}

type Paths struct {
	Root         string
	App          string
	Workspace    string
	Agent        string
	AgentHome    string
	OpenCodeData string
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
	agent := filepath.Join(root, "agent")
	agentHome := filepath.Join(agent, "home")
	return Paths{
		Root:         root,
		App:          filepath.Join(root, "app"),
		Workspace:    filepath.Join(root, "workspace"),
		Agent:        agent,
		AgentHome:    agentHome,
		OpenCodeData: filepath.Join(agentHome, ".local", "share", "opencode"),
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
	for _, path := range []string{
		paths.Root,
		paths.App,
		paths.Workspace,
		paths.Agent,
		paths.AgentHome,
		filepath.Join(paths.AgentHome, ".local"),
		filepath.Join(paths.AgentHome, ".local", "share"),
		paths.OpenCodeData,
	} {
		if err := ensurePrivateDirectory(path); err != nil {
			return Paths{}, fmt.Errorf("initialize user directory: %w", err)
		}
	}
	if err := migrateFileWithoutOverwrite(
		filepath.Join(paths.App, agentcredentials.LegacyFileName),
		filepath.Join(paths.OpenCodeData, agentcredentials.FileName),
	); err != nil {
		return Paths{}, fmt.Errorf("migrate Agent credentials: %w", err)
	}
	return paths, nil
}

func migrateFileWithoutOverwrite(source, destination string) error {
	sourceInfo, err := os.Lstat(source)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if sourceInfo.Mode()&os.ModeSymlink != 0 || !sourceInfo.Mode().IsRegular() {
		return errors.New("migration source is not a regular file")
	}

	destinationInfo, err := os.Lstat(destination)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Rename(source, destination); err != nil {
			return err
		}
		return syncDirectories(filepath.Dir(source), filepath.Dir(destination))
	}
	if err != nil {
		return err
	}
	if destinationInfo.Mode()&os.ModeSymlink != 0 || !destinationInfo.Mode().IsRegular() || !filesEqual(source, destination) {
		return errors.New("migration destination conflicts with source")
	}
	if err := os.Remove(source); err != nil {
		return err
	}
	return syncDirectories(filepath.Dir(source), filepath.Dir(destination))
}

func syncDirectories(paths ...string) error {
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		path = filepath.Clean(path)
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		directory, err := os.Open(path)
		if err != nil {
			return err
		}
		syncErr := directory.Sync()
		closeErr := directory.Close()
		if syncErr != nil {
			return syncErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
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
