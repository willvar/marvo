package userspace

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const legacyMigrationMarker = ".legacy-migration.json"

var (
	ErrLegacyUnavailable = errors.New("legacy data is unavailable")
	ErrMigrationConflict = errors.New("migration destination conflicts with legacy data")
)

type LegacySources struct {
	Workspace string
	AgentHome string
}

type LegacyStatus struct {
	Available     bool   `json:"available"`
	NoteCount     int    `json:"note_count"`
	HasTrash      bool   `json:"has_trash"`
	HasSettings   bool   `json:"has_settings"`
	HasDevices    bool   `json:"has_devices"`
	HasAgentState bool   `json:"has_agent_state"`
	MigratedTo    string `json:"migrated_to,omitempty"`
}

type MigrationResult struct {
	UserID      string    `json:"user_id"`
	NoteCount   int       `json:"note_count"`
	FilesCopied int       `json:"files_copied"`
	BytesCopied int64     `json:"bytes_copied"`
	CompletedAt time.Time `json:"completed_at"`
}

type migrationMarker struct {
	Version int `json:"version"`
	MigrationResult
}

type copyFile struct {
	source      string
	destination string
	relative    string
	size        int64
	mode        os.FileMode
}

func (l *Layout) InspectLegacy(sources LegacySources) (LegacyStatus, error) {
	status := LegacyStatus{}
	workspace, workspaceAvailable, err := inspectOptionalDirectory(sources.Workspace)
	if err != nil {
		return status, err
	}
	if workspaceAvailable {
		entries, err := os.ReadDir(workspace)
		if err != nil {
			return status, fmt.Errorf("read legacy workspace: %w", err)
		}
		for _, entry := range entries {
			name := entry.Name()
			switch name {
			case ".trash":
				status.HasTrash = directoryHasEntries(filepath.Join(workspace, name))
			case ".agent-settings.json":
				status.HasSettings = entry.Type().IsRegular()
			case ".devices.json":
				status.HasDevices = entry.Type().IsRegular()
			default:
				if !strings.HasPrefix(name, ".") && entry.IsDir() && isLegacyNote(filepath.Join(workspace, name)) {
					status.NoteCount++
				}
			}
		}
	}
	agentHome, agentAvailable, err := inspectOptionalDirectory(sources.AgentHome)
	if err != nil {
		return status, err
	}
	if agentAvailable {
		status.HasAgentState = directoryHasEntries(filepath.Join(agentHome, ".local", "share", "opencode")) ||
			regularFileExists(filepath.Join(agentHome, ".config", "opencode", "AGENTS.md"))
	}
	status.Available = status.NoteCount > 0 || status.HasTrash || status.HasSettings || status.HasDevices || status.HasAgentState ||
		regularFileExists(filepath.Join(workspace, "theme.json")) ||
		regularFileExists(filepath.Join(workspace, ".agent-personalization.json"))
	status.MigratedTo, err = l.legacyMigrationTarget()
	return status, err
}

func (l *Layout) MigrateLegacy(userID string, sources LegacySources) (MigrationResult, error) {
	paths, err := l.EnsureUser(userID)
	if err != nil {
		return MigrationResult{}, err
	}
	status, err := l.InspectLegacy(sources)
	if err != nil {
		return MigrationResult{}, err
	}
	if !status.Available {
		return MigrationResult{}, ErrLegacyUnavailable
	}
	if status.MigratedTo != "" && status.MigratedTo != userID {
		return MigrationResult{}, fmt.Errorf("%w: legacy data was already migrated", ErrMigrationConflict)
	}
	if existing, ok := readMigrationMarker(paths.Root); ok {
		return existing.MigrationResult, nil
	}

	files, directories, err := collectLegacyFiles(paths, sources)
	if err != nil {
		return MigrationResult{}, err
	}
	if len(files) == 0 {
		return MigrationResult{}, ErrLegacyUnavailable
	}
	if err := preflightMigrationTargets(files, directories, paths); err != nil {
		return MigrationResult{}, err
	}

	stage, err := createMigrationStage(paths.Root)
	if err != nil {
		return MigrationResult{}, err
	}
	stageComplete := false
	defer func() {
		if !stageComplete {
			_ = os.RemoveAll(stage)
		}
	}()

	var copiedBytes int64
	for _, file := range files {
		stagedPath := filepath.Join(stage, file.relative)
		if err := copyVerifiedFile(file.source, stagedPath, file.mode, file.size); err != nil {
			return MigrationResult{}, err
		}
		copiedBytes += file.size
	}
	for _, file := range files {
		stagedPath := filepath.Join(stage, file.relative)
		if err := installWithoutOverwrite(stagedPath, file.destination); err != nil {
			return MigrationResult{}, err
		}
	}

	result := MigrationResult{
		UserID: userID, NoteCount: status.NoteCount, FilesCopied: len(files),
		BytesCopied: copiedBytes, CompletedAt: time.Now().UTC().Truncate(time.Millisecond),
	}
	marker := migrationMarker{Version: 1, MigrationResult: result}
	if err := writeMigrationMarker(paths.Root, marker); err != nil {
		return MigrationResult{}, err
	}
	stageComplete = true
	if err := os.RemoveAll(stage); err != nil {
		return MigrationResult{}, fmt.Errorf("remove migration staging directory: %w", err)
	}
	return result, nil
}

func collectLegacyFiles(paths Paths, sources LegacySources) ([]copyFile, []string, error) {
	files := make([]copyFile, 0)
	directories := make([]string, 0)
	workspace, available, err := inspectOptionalDirectory(sources.Workspace)
	if err != nil {
		return nil, nil, err
	}
	if available {
		entries, err := os.ReadDir(workspace)
		if err != nil {
			return nil, nil, fmt.Errorf("read legacy workspace: %w", err)
		}
		for _, entry := range entries {
			name := entry.Name()
			source := filepath.Join(workspace, name)
			switch {
			case !strings.HasPrefix(name, ".") && entry.IsDir() && isLegacyNote(source):
				err = collectRegularTree(source, filepath.Join(paths.Workspace, name), filepath.Join("workspace", name), &files, &directories)
			case name == ".trash" && entry.IsDir():
				err = collectRegularTree(source, filepath.Join(paths.Workspace, name), filepath.Join("workspace", name), &files, &directories)
			case name == "theme.json" || name == ".agent-personalization.json":
				err = collectRegularFile(source, filepath.Join(paths.Workspace, name), filepath.Join("workspace", name), &files)
			case name == ".agent-settings.json" || name == ".devices.json":
				err = collectRegularFile(source, filepath.Join(paths.Workspace, name), filepath.Join("workspace", name), &files)
			default:
				continue
			}
			if err != nil {
				return nil, nil, err
			}
		}
	}

	agentHome, available, err := inspectOptionalDirectory(sources.AgentHome)
	if err != nil {
		return nil, nil, err
	}
	if available {
		for _, relative := range []string{
			filepath.Join(".local", "share", "opencode"),
			filepath.Join(".config", "opencode", "AGENTS.md"),
		} {
			source := filepath.Join(agentHome, relative)
			info, statErr := os.Lstat(source)
			if errors.Is(statErr, os.ErrNotExist) {
				continue
			}
			if statErr != nil {
				return nil, nil, fmt.Errorf("inspect legacy Agent state: %w", statErr)
			}
			destination := filepath.Join(paths.AgentHome, relative)
			stageRelative := filepath.Join("agent", "home", relative)
			if info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
				err = collectRegularTree(source, destination, stageRelative, &files, &directories)
			} else {
				err = collectRegularFile(source, destination, stageRelative, &files)
			}
			if err != nil {
				return nil, nil, err
			}
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].relative < files[j].relative })
	sort.Strings(directories)
	return files, directories, nil
}

func collectRegularTree(sourceRoot, destinationRoot, stageRoot string, files *[]copyFile, directories *[]string) error {
	rootInfo, err := os.Lstat(sourceRoot)
	if err != nil || rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return fmt.Errorf("legacy source %q is not a regular directory", sourceRoot)
	}
	return filepath.WalkDir(sourceRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return err
		}
		if relative == "." {
			*directories = append(*directories, destinationRoot)
			return nil
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("legacy source contains a symbolic link: %s", path)
		}
		if info.IsDir() {
			*directories = append(*directories, filepath.Join(destinationRoot, relative))
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("legacy source contains an unsupported file: %s", path)
		}
		*files = append(*files, copyFile{
			source: path, destination: filepath.Join(destinationRoot, relative),
			relative: filepath.Join(stageRoot, relative), size: info.Size(), mode: info.Mode().Perm(),
		})
		return nil
	})
}

func collectRegularFile(source, destination, stageRelative string, files *[]copyFile) error {
	info, err := os.Lstat(source)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("legacy source %q is not a regular file", source)
	}
	*files = append(*files, copyFile{
		source: source, destination: destination, relative: stageRelative,
		size: info.Size(), mode: info.Mode().Perm(),
	})
	return nil
}

func preflightMigrationTargets(files []copyFile, directories []string, paths Paths) error {
	roots := []string{paths.Workspace, paths.Agent}
	for _, root := range roots {
		if err := requirePrivateDirectory(root); err != nil {
			return err
		}
	}
	for _, directory := range directories {
		if err := validateMigrationAncestors(directory, roots); err != nil {
			return err
		}
		info, err := os.Lstat(directory)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("%w: %s", ErrMigrationConflict, directory)
		}
	}
	for _, file := range files {
		if err := validateMigrationAncestors(file.destination, roots); err != nil {
			return err
		}
		info, err := os.Lstat(file.destination)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || !filesEqual(file.source, file.destination) {
			return fmt.Errorf("%w: %s", ErrMigrationConflict, file.destination)
		}
	}
	return nil
}

func validateMigrationAncestors(destination string, roots []string) error {
	var root string
	var relative string
	for _, candidate := range roots {
		value, err := filepath.Rel(candidate, destination)
		if err == nil && value != ".." && !strings.HasPrefix(value, ".."+string(filepath.Separator)) {
			root = candidate
			relative = value
			break
		}
	}
	if root == "" {
		return fmt.Errorf("%w: destination escapes user space", ErrMigrationConflict)
	}
	current := root
	parts := strings.Split(filepath.Dir(relative), string(filepath.Separator))
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("%w: %s", ErrMigrationConflict, current)
		}
	}
	return nil
}

func copyVerifiedFile(source, destination string, mode os.FileMode, expectedSize int64) error {
	before, err := os.Lstat(source)
	if err != nil || before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() || before.Size() != expectedSize {
		return fmt.Errorf("legacy source changed before copy: %s", source)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode(mode))
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(output, input)
	if copyErr == nil {
		copyErr = output.Sync()
	}
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	after, err := os.Lstat(source)
	if err != nil || written != expectedSize || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		return fmt.Errorf("legacy source changed during copy: %s", source)
	}
	if !filesEqual(source, destination) {
		return fmt.Errorf("verify migrated file: %s", source)
	}
	return nil
}

func installWithoutOverwrite(staged, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return err
	}
	if _, err := os.Lstat(destination); errors.Is(err, os.ErrNotExist) {
		if err := os.Rename(staged, destination); err != nil {
			return err
		}
		return nil
	} else if err != nil {
		return err
	}
	if !filesEqual(staged, destination) {
		return fmt.Errorf("%w: %s", ErrMigrationConflict, destination)
	}
	return nil
}

func filesEqual(left, right string) bool {
	leftInfo, leftErr := os.Lstat(left)
	rightInfo, rightErr := os.Lstat(right)
	if leftErr != nil || rightErr != nil || !leftInfo.Mode().IsRegular() || !rightInfo.Mode().IsRegular() || leftInfo.Size() != rightInfo.Size() {
		return false
	}
	leftDigest, leftOK := fileDigest(left)
	rightDigest, rightOK := fileDigest(right)
	return leftOK && rightOK && leftDigest == rightDigest
}

func fileDigest(path string) ([32]byte, bool) {
	file, err := os.Open(path)
	if err != nil {
		return [32]byte{}, false
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return [32]byte{}, false
	}
	var result [32]byte
	copy(result[:], hash.Sum(nil))
	return result, true
}

func createMigrationStage(userRoot string) (string, error) {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	path := filepath.Join(userRoot, ".legacy-stage-"+hex.EncodeToString(random))
	if err := os.Mkdir(path, 0700); err != nil {
		return "", err
	}
	return path, nil
}

func writeMigrationMarker(userRoot string, marker migrationMarker) error {
	data, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path := filepath.Join(userRoot, legacyMigrationMarker)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func readMigrationMarker(userRoot string) (migrationMarker, bool) {
	path := filepath.Join(userRoot, legacyMigrationMarker)
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > 64<<10 {
		return migrationMarker{}, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return migrationMarker{}, false
	}
	var marker migrationMarker
	if json.Unmarshal(raw, &marker) != nil || marker.Version != 1 || marker.UserID == "" {
		return migrationMarker{}, false
	}
	return marker, true
}

func (l *Layout) legacyMigrationTarget() (string, error) {
	usersRoot := filepath.Join(l.root, "users")
	entries, err := os.ReadDir(usersRoot)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		paths, err := l.UserPaths(entry.Name())
		if err != nil {
			continue
		}
		if marker, ok := readMigrationMarker(paths.Root); ok {
			return marker.UserID, nil
		}
		// Compatibility for user spaces that have not yet been opened since
		// the app-directory layout was retired.
		if marker, ok := readMigrationMarker(filepath.Join(paths.Root, "app")); ok {
			return marker.UserID, nil
		}
	}
	return "", nil
}

func inspectOptionalDirectory(path string) (string, bool, error) {
	path = filepath.Clean(path)
	if path == "." || !filepath.IsAbs(path) {
		return "", false, errors.New("legacy source must be an absolute path")
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return path, false, nil
	}
	if err != nil {
		return "", false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", false, fmt.Errorf("legacy source %q is not a regular directory", path)
	}
	return path, true, nil
}

func isLegacyNote(path string) bool {
	return regularFileExists(filepath.Join(path, "index.md")) && regularFileExists(filepath.Join(path, "meta.json"))
}

func regularFileExists(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSymlink == 0 && info.Mode().IsRegular()
}

func directoryHasEntries(path string) bool {
	entries, err := os.ReadDir(path)
	return err == nil && len(entries) > 0
}

func fileMode(mode os.FileMode) os.FileMode {
	if mode&0111 != 0 {
		return 0700
	}
	return 0600
}
