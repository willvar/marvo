package media

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"marvo/internal/store"
)

type State string

var (
	ErrUnsupportedMedia    = errors.New("only supported image and video files may be uploaded")
	ErrInsufficientStorage = errors.New("insufficient server storage")
)

const (
	StateReserved    State = "reserved"
	StateUploading   State = "uploading"
	StateQueued      State = "queued"
	StateProbing     State = "probing"
	StateTranscoding State = "transcoding"
	StateReady       State = "ready"
	StateAbandoned   State = "abandoned"
	StateFailed      State = "failed"
)

type Asset struct {
	ID            string    `json:"id"`
	Kind          string    `json:"kind"`
	State         State     `json:"state"`
	OriginalName  string    `json:"original_name"`
	ContentType   string    `json:"content_type,omitempty"`
	Filename      string    `json:"filename,omitempty"`
	Error         string    `json:"error,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	InstanceToken string    `json:"-"`
}

type job struct {
	Title         string
	InstanceToken string
	AssetID       string
}

type Manager struct {
	store  *store.NoteStore
	ctx    context.Context
	cancel context.CancelFunc
	queue  chan job
	wg     sync.WaitGroup

	mu        sync.Mutex
	queued    map[string]bool
	running   map[string]context.CancelFunc
	transfers map[string]context.CancelFunc
	onChange  func(string, Asset)
	ffmpeg    string
	ffprobe   string
}

var assetIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
var assetMetaPattern = regexp.MustCompile(`^\.asset-([0-9a-f-]{36})\.json$`)

const (
	minimumFreeBytes = uint64(512 << 20)
	copyBufferSize   = 1 << 20
	stallTimeout     = 3 * time.Minute
)

func NewManager(noteStore *store.NoteStore) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	manager := &Manager{
		store:     noteStore,
		ctx:       ctx,
		cancel:    cancel,
		queue:     make(chan job, 256),
		queued:    make(map[string]bool),
		running:   make(map[string]context.CancelFunc),
		transfers: make(map[string]context.CancelFunc),
	}
	manager.ffmpeg, _ = exec.LookPath("ffmpeg")
	manager.ffprobe, _ = exec.LookPath("ffprobe")
	manager.wg.Add(1)
	go manager.worker()
	manager.resumePending()
	return manager
}

func (m *Manager) SetChangeHandler(handler func(string, Asset)) {
	m.mu.Lock()
	m.onChange = handler
	m.mu.Unlock()
}

func (m *Manager) Close() {
	m.cancel()
	m.mu.Lock()
	for _, cancel := range m.running {
		cancel()
	}
	for _, cancel := range m.transfers {
		cancel()
	}
	m.mu.Unlock()
	m.wg.Wait()
}

// Idle reports whether closing the manager would leave no upload or transcode
// work behind. It is used by the user-space cache; active media work pins its
// space even when no HTTP request is currently using it.
func (m *Manager) Idle() bool {
	if m == nil {
		return true
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.queued) == 0 && len(m.running) == 0 && len(m.transfers) == 0
}

func ValidAssetID(id string) bool { return assetIDPattern.MatchString(id) }

func NewAssetID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	hexID := hex.EncodeToString(raw)
	return fmt.Sprintf("%s-%s-%s-%s-%s", hexID[0:8], hexID[8:12], hexID[12:16], hexID[16:20], hexID[20:32]), nil
}

func (m *Manager) Reserve(title, instanceToken, id, originalName, declaredType string) (*Asset, error) {
	if id == "" {
		var err error
		id, err = NewAssetID()
		if err != nil {
			return nil, err
		}
	}
	id = strings.ToLower(id)
	if !ValidAssetID(id) {
		return nil, errors.New("invalid asset id")
	}
	originalName = strings.TrimSpace(filepath.Base(originalName))
	if originalName == "" || originalName == "." || len([]rune(originalName)) > 255 {
		return nil, errors.New("invalid original filename")
	}
	kind, err := mediaKind(originalName, declaredType)
	if err != nil {
		return nil, err
	}
	assetsDir, err := m.store.AssetsDirCAS(title, instanceToken)
	if err != nil {
		return nil, err
	}
	if err := ensureDiskSpace(assetsDir, 0); err != nil {
		return nil, err
	}
	asset := Asset{
		ID: id, Kind: kind, State: StateReserved, OriginalName: originalName,
		ContentType: declaredType, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		InstanceToken: instanceToken,
	}
	m.mu.Lock()
	if _, err := os.Lstat(metaPath(assetsDir, id)); err == nil {
		m.mu.Unlock()
		return nil, errors.New("asset id already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		m.mu.Unlock()
		return nil, err
	}
	if err := writeAssetMeta(assetsDir, asset); err != nil {
		m.mu.Unlock()
		return nil, err
	}
	m.mu.Unlock()
	m.notify(title, asset)
	return &asset, nil
}

func (m *Manager) Upload(ctx context.Context, title, instanceToken, id string, size int64, src io.Reader) (*Asset, error) {
	if !ValidAssetID(id) {
		return nil, errors.New("invalid asset id")
	}
	assetsDir, err := m.store.AssetsDirCAS(title, instanceToken)
	if err != nil {
		return nil, err
	}
	if size > 0 {
		if err := ensureDiskSpace(assetsDir, uint64(size)); err != nil {
			return nil, err
		}
	}
	_, err = m.update(title, instanceToken, id, func(asset *Asset) error {
		if asset.State != StateReserved && asset.State != StateFailed {
			return fmt.Errorf("asset cannot be uploaded from state %s", asset.State)
		}
		asset.State = StateUploading
		asset.Error = ""
		return nil
	})
	if err != nil {
		return nil, err
	}
	uploadCtx, cancelUpload := context.WithCancel(ctx)
	key := jobKey(instanceToken, id)
	m.mu.Lock()
	if m.transfers[key] != nil {
		m.mu.Unlock()
		cancelUpload()
		return nil, errors.New("asset upload already in progress")
	}
	m.transfers[key] = cancelUpload
	m.mu.Unlock()
	defer func() {
		cancelUpload()
		m.mu.Lock()
		delete(m.transfers, key)
		m.mu.Unlock()
	}()

	source := uploadPath(assetsDir, id)
	file, err := os.OpenFile(source, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if errors.Is(err, os.ErrExist) {
		_ = os.Remove(source)
		file, err = os.OpenFile(source, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	}
	if err != nil {
		m.fail(title, instanceToken, id, "无法创建上传临时文件")
		return nil, err
	}
	copyErr := guardedCopy(uploadCtx, file, src, assetsDir)
	if syncErr := file.Sync(); copyErr == nil {
		copyErr = syncErr
	}
	if closeErr := file.Close(); copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		_ = os.Remove(source)
		if latest, getErr := m.Get(title, instanceToken, id); getErr == nil && latest.State != StateAbandoned {
			m.fail(title, instanceToken, id, "上传中断")
		}
		return nil, copyErr
	}

	asset, err := m.update(title, instanceToken, id, func(asset *Asset) error {
		if asset.State == StateAbandoned {
			return errors.New("asset was abandoned")
		}
		asset.State = StateQueued
		return nil
	})
	if err != nil {
		_ = os.Remove(source)
		return nil, err
	}
	m.enqueue(job{Title: title, InstanceToken: instanceToken, AssetID: id})
	return asset, nil
}

func (m *Manager) Get(title, instanceToken, id string) (*Asset, error) {
	if !ValidAssetID(id) {
		return nil, errors.New("invalid asset id")
	}
	assetsDir, err := m.store.AssetsDirCAS(title, instanceToken)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	asset, err := readAssetMeta(assetsDir, id)
	if err != nil {
		return nil, err
	}
	asset.InstanceToken = instanceToken
	return &asset, nil
}

func (m *Manager) List(title, instanceToken string) ([]Asset, error) {
	assetsDir, err := m.store.AssetsDirCAS(title, instanceToken)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	entries, err := os.ReadDir(assetsDir)
	if err != nil {
		return nil, err
	}
	result := make([]Asset, 0)
	for _, entry := range entries {
		match := assetMetaPattern.FindStringSubmatch(entry.Name())
		if len(match) != 2 || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		asset, readErr := readAssetMeta(assetsDir, match[1])
		if readErr != nil {
			continue
		}
		asset.InstanceToken = instanceToken
		result = append(result, asset)
	}
	return result, nil
}

func (m *Manager) Abandon(title, instanceToken, id string) (*Asset, error) {
	asset, err := m.update(title, instanceToken, id, func(asset *Asset) error {
		asset.State = StateAbandoned
		asset.Error = ""
		return nil
	})
	if err != nil {
		return nil, err
	}
	key := jobKey(instanceToken, id)
	m.mu.Lock()
	if cancel := m.running[key]; cancel != nil {
		cancel()
	}
	if cancel := m.transfers[key]; cancel != nil {
		cancel()
	}
	m.mu.Unlock()
	m.ReconcileNote(title, instanceToken)
	return asset, nil
}

func (m *Manager) HasBusyAssets(title, instanceToken string) bool {
	assets, err := m.List(title, instanceToken)
	if err != nil {
		return false
	}
	for _, asset := range assets {
		switch asset.State {
		case StateUploading, StateQueued, StateProbing, StateTranscoding:
			return true
		}
	}
	return false
}

func (m *Manager) ReconcileNote(title, instanceToken string) {
	assets, err := m.List(title, instanceToken)
	if err != nil {
		return
	}
	snapshot, err := m.store.Snapshot(title)
	if err != nil || snapshot.InstanceToken != instanceToken {
		return
	}
	assetsDir, err := m.store.AssetsDirCAS(title, instanceToken)
	if err != nil {
		return
	}
	for _, asset := range assets {
		if strings.Contains(snapshot.Content, asset.ID) {
			continue
		}
		if asset.State != StateAbandoned {
			updated, updateErr := m.update(title, instanceToken, asset.ID, func(current *Asset) error {
				current.State = StateAbandoned
				current.Error = ""
				return nil
			})
			if updateErr != nil {
				continue
			}
			asset = *updated
		}
		key := jobKey(instanceToken, asset.ID)
		m.mu.Lock()
		if cancel := m.running[key]; cancel != nil {
			cancel()
		}
		if cancel := m.transfers[key]; cancel != nil {
			cancel()
		}
		_ = removeAssetFiles(assetsDir, asset)
		m.mu.Unlock()
	}
}

func (m *Manager) update(title, instanceToken, id string, mutate func(*Asset) error) (*Asset, error) {
	assetsDir, err := m.store.AssetsDirCAS(title, instanceToken)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	asset, err := readAssetMeta(assetsDir, id)
	if err == nil {
		asset.InstanceToken = instanceToken
		err = mutate(&asset)
	}
	if err == nil {
		asset.UpdatedAt = time.Now().UTC()
		err = writeAssetMeta(assetsDir, asset)
	}
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	m.notify(title, asset)
	return &asset, nil
}

func (m *Manager) fail(title, instanceToken, id, message string) {
	_, err := m.update(title, instanceToken, id, func(asset *Asset) error {
		if asset.State == StateAbandoned {
			return nil
		}
		asset.State = StateFailed
		asset.Error = message
		return nil
	})
	if err != nil {
		slog.Warn("failed to persist media failure", "asset_id", id, "error", err)
	}
	assetsDir, dirErr := m.store.AssetsDirCAS(title, instanceToken)
	if dirErr == nil {
		for _, path := range []string{
			uploadPath(assetsDir, id),
			filepath.Join(assetsDir, ".transcode-"+id+".jpg"),
			filepath.Join(assetsDir, ".transcode-"+id+".mp4"),
		} {
			_ = os.Remove(path)
		}
	}
}

func (m *Manager) notify(title string, asset Asset) {
	m.mu.Lock()
	handler := m.onChange
	m.mu.Unlock()
	if handler != nil {
		handler(title, asset)
	}
}

func (m *Manager) enqueue(next job) {
	key := jobKey(next.InstanceToken, next.AssetID)
	m.mu.Lock()
	if m.queued[key] || m.running[key] != nil {
		m.mu.Unlock()
		return
	}
	m.queued[key] = true
	m.mu.Unlock()
	select {
	case m.queue <- next:
	case <-m.ctx.Done():
	}
}

func (m *Manager) worker() {
	defer m.wg.Done()
	for {
		select {
		case <-m.ctx.Done():
			return
		case next := <-m.queue:
			key := jobKey(next.InstanceToken, next.AssetID)
			ctx, cancel := context.WithCancel(m.ctx)
			m.mu.Lock()
			delete(m.queued, key)
			m.running[key] = cancel
			m.mu.Unlock()
			m.process(ctx, next)
			cancel()
			m.mu.Lock()
			delete(m.running, key)
			m.mu.Unlock()
		}
	}
}

func (m *Manager) process(ctx context.Context, next job) {
	title := next.Title
	if moved, ok := m.store.ResolveInstance(next.InstanceToken); ok {
		title = moved
	}
	asset, err := m.Get(title, next.InstanceToken, next.AssetID)
	if err != nil || asset.State == StateAbandoned || asset.State == StateReady {
		return
	}
	assetsDir, err := m.store.AssetsDirCAS(title, next.InstanceToken)
	if err != nil {
		return
	}
	source := uploadPath(assetsDir, asset.ID)
	if info, statErr := os.Lstat(source); statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		m.fail(title, next.InstanceToken, asset.ID, "上传文件不存在或无效")
		return
	}
	asset, err = m.update(title, next.InstanceToken, asset.ID, func(asset *Asset) error {
		asset.State = StateProbing
		return nil
	})
	if err != nil {
		return
	}

	ext := strings.ToLower(filepath.Ext(asset.OriginalName))
	if asset.Kind == "image" {
		if ext != ".heic" && ext != ".heif" {
			if err := validateCompatibleImage(source, ext); err != nil {
				m.fail(title, next.InstanceToken, asset.ID, "图片文件损坏或格式不匹配")
				return
			}
			finalExt := normalizeImageExtension(ext)
			if err := m.finishWithoutTranscode(title, next.InstanceToken, *asset, assetsDir, source, finalExt); err != nil {
				m.fail(title, next.InstanceToken, asset.ID, "图片入库失败")
			}
			return
		}
		if m.ffmpeg == "" {
			m.fail(title, next.InstanceToken, asset.ID, "服务器未安装 ffmpeg，无法转换 HEIC/HEIF")
			return
		}
		m.transcode(ctx, title, next.InstanceToken, *asset, assetsDir, source, ".jpg", []string{
			"-nostdin", "-hide_banner", "-loglevel", "error", "-i", source,
			"-frames:v", "1", "-q:v", "2",
		})
		return
	}

	probe, err := m.probe(ctx, source)
	if err != nil {
		m.fail(title, next.InstanceToken, asset.ID, "视频文件损坏或无法探测")
		return
	}
	if compatibleVideo(ext, probe) {
		finalExt := ext
		if finalExt == ".m4v" {
			finalExt = ".mp4"
		}
		if err := m.finishWithoutTranscode(title, next.InstanceToken, *asset, assetsDir, source, finalExt); err != nil {
			m.fail(title, next.InstanceToken, asset.ID, "视频入库失败")
		}
		return
	}
	if m.ffmpeg == "" {
		m.fail(title, next.InstanceToken, asset.ID, "服务器未安装 ffmpeg，无法转换该视频")
		return
	}
	m.transcode(ctx, title, next.InstanceToken, *asset, assetsDir, source, ".mp4", []string{
		"-nostdin", "-hide_banner", "-loglevel", "error", "-i", source,
		"-map", "0:v:0", "-map", "0:a?", "-c:v", "libx264", "-pix_fmt", "yuv420p",
		"-preset", "medium", "-crf", "23", "-c:a", "aac", "-b:a", "160k", "-movflags", "+faststart",
	})
}

func (m *Manager) finishWithoutTranscode(title, token string, asset Asset, assetsDir, source, extension string) error {
	finalName := asset.ID + extension
	finalPath := filepath.Join(assetsDir, finalName)
	if err := os.Rename(source, finalPath); err != nil {
		return err
	}
	_, err := m.update(title, token, asset.ID, func(current *Asset) error {
		if current.State == StateAbandoned {
			_ = os.Remove(finalPath)
			return errors.New("asset abandoned")
		}
		current.State = StateReady
		current.Filename = finalName
		current.Error = ""
		return nil
	})
	if err != nil {
		_ = os.Remove(finalPath)
	}
	return err
}

func (m *Manager) transcode(ctx context.Context, title, token string, asset Asset, assetsDir, source, extension string, args []string) {
	_, err := m.update(title, token, asset.ID, func(current *Asset) error {
		current.State = StateTranscoding
		return nil
	})
	if err != nil {
		return
	}
	output := filepath.Join(assetsDir, ".transcode-"+asset.ID+extension)
	_ = os.Remove(output)
	args = append(args, "-y", output)
	if err := runWithStallGuard(ctx, assetsDir, output, m.ffmpeg, args...); err != nil {
		_ = os.Remove(output)
		if latest, getErr := m.Get(title, token, asset.ID); getErr == nil && latest.State != StateAbandoned {
			m.fail(title, token, asset.ID, "媒体转换失败或长时间无进展")
		}
		return
	}
	if extension == ".jpg" {
		if err := validateCompatibleImage(output, extension); err != nil {
			_ = os.Remove(output)
			m.fail(title, token, asset.ID, "转换后的图片无效")
			return
		}
	} else {
		probe, probeErr := m.probe(ctx, output)
		if probeErr != nil || !compatibleVideo(".mp4", probe) {
			_ = os.Remove(output)
			m.fail(title, token, asset.ID, "转换后的视频无法在 Chromium 中播放")
			return
		}
	}
	if err := os.Chmod(output, 0600); err != nil {
		_ = os.Remove(output)
		m.fail(title, token, asset.ID, "无法保护转换后的媒体文件")
		return
	}
	finalName := asset.ID + extension
	finalPath := filepath.Join(assetsDir, finalName)
	if err := os.Rename(output, finalPath); err != nil {
		_ = os.Remove(output)
		m.fail(title, token, asset.ID, "转换结果入库失败")
		return
	}
	_ = os.Remove(source)
	_, err = m.update(title, token, asset.ID, func(current *Asset) error {
		if current.State == StateAbandoned {
			_ = os.Remove(finalPath)
			return errors.New("asset abandoned")
		}
		current.State = StateReady
		current.Filename = finalName
		current.Error = ""
		return nil
	})
	if err != nil {
		_ = os.Remove(finalPath)
	}
}

type probeResult struct {
	Format struct {
		FormatName string `json:"format_name"`
	} `json:"format"`
	Streams []struct {
		CodecType string `json:"codec_type"`
		CodecName string `json:"codec_name"`
	} `json:"streams"`
}

func (m *Manager) probe(parent context.Context, path string) (probeResult, error) {
	if m.ffprobe == "" {
		return probeResult{}, errors.New("ffprobe is not installed")
	}
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, m.ffprobe, "-v", "error", "-show_format", "-show_streams", "-of", "json", path)
	raw, err := cmd.Output()
	if err != nil {
		return probeResult{}, err
	}
	var result probeResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return probeResult{}, err
	}
	return result, nil
}

func compatibleVideo(extension string, probe probeResult) bool {
	var videoCodec string
	audioOK := true
	for _, stream := range probe.Streams {
		switch stream.CodecType {
		case "video":
			if videoCodec == "" {
				videoCodec = stream.CodecName
			}
		case "audio":
			if extension == ".webm" {
				audioOK = audioOK && (stream.CodecName == "opus" || stream.CodecName == "vorbis")
			} else {
				audioOK = audioOK && stream.CodecName == "aac"
			}
		}
	}
	if extension == ".webm" {
		return audioOK && (videoCodec == "vp8" || videoCodec == "vp9" || videoCodec == "av1")
	}
	return (extension == ".mp4" || extension == ".m4v") && audioOK && videoCodec == "h264"
}

func mediaKind(filename, declaredType string) (string, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".avif", ".heic", ".heif":
		return "image", nil
	case ".mp4", ".mov", ".m4v", ".webm":
		return "video", nil
	}
	if strings.HasPrefix(declaredType, "image/") || strings.HasPrefix(declaredType, "video/") {
		return "", fmt.Errorf("%w: unsupported file extension", ErrUnsupportedMedia)
	}
	return "", ErrUnsupportedMedia
}

func normalizeImageExtension(extension string) string {
	if extension == ".jpeg" {
		return ".jpg"
	}
	return extension
}

func validateCompatibleImage(path, extension string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	header := make([]byte, 512)
	n, err := io.ReadFull(file, header)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return err
	}
	detected := http.DetectContentType(header[:n])
	extension = normalizeImageExtension(extension)
	switch extension {
	case ".jpg":
		if detected != "image/jpeg" {
			return errors.New("not a JPEG")
		}
	case ".png":
		if detected != "image/png" {
			return errors.New("not a PNG")
		}
	case ".gif":
		if detected != "image/gif" {
			return errors.New("not a GIF")
		}
	case ".webp":
		if n < 12 || string(header[:4]) != "RIFF" || string(header[8:12]) != "WEBP" {
			return errors.New("not a WebP")
		}
	case ".avif":
		if n < 12 || string(header[4:8]) != "ftyp" || !bytes.Contains(header[8:n], []byte("avif")) {
			return errors.New("not an AVIF")
		}
	default:
		return errors.New("unsupported image extension")
	}
	return nil
}

func guardedCopy(ctx context.Context, destination *os.File, source io.Reader, dir string) error {
	buffer := make([]byte, copyBufferSize)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, readErr := source.Read(buffer)
		if n > 0 {
			if _, err := destination.Write(buffer[:n]); err != nil {
				return err
			}
			total += int64(n)
			if total%(64<<20) < int64(n) {
				if err := ensureDiskSpace(dir, 0); err != nil {
					return err
				}
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

func ensureDiskSpace(path string, incoming uint64) error {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return err
	}
	available := stat.Bavail * uint64(stat.Bsize)
	if available <= minimumFreeBytes || incoming > available-minimumFreeBytes {
		return fmt.Errorf("%w: 服务器磁盘剩余空间不足，上传已安全停止", ErrInsufficientStorage)
	}
	return nil
}

func runWithStallGuard(parent context.Context, dir, output, command string, args ...string) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	cmd := exec.CommandContext(ctx, command, args...)
	var stderr limitedBuffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	lastChange := time.Now()
	var lastSize int64 = -1
	for {
		select {
		case err := <-done:
			if err != nil {
				return fmt.Errorf("ffmpeg: %w: %s", err, stderr.String())
			}
			return nil
		case <-parent.Done():
			cancel()
			<-done
			return parent.Err()
		case <-ticker.C:
			if err := ensureDiskSpace(dir, 0); err != nil {
				cancel()
				<-done
				return err
			}
			if info, err := os.Stat(output); err == nil && info.Size() != lastSize {
				lastSize = info.Size()
				lastChange = time.Now()
			}
			if time.Since(lastChange) > stallTimeout {
				cancel()
				<-done
				return errors.New("transcode stalled")
			}
		}
	}
}

type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	const max = 64 << 10
	original := len(p)
	if b.Len() < max {
		remaining := max - b.Len()
		if len(p) > remaining {
			p = p[:remaining]
		}
		_, _ = b.Buffer.Write(p)
	}
	return original, nil
}

func (m *Manager) resumePending() {
	notes, err := m.store.List()
	if err != nil {
		return
	}
	for _, note := range notes {
		snapshot, snapshotErr := m.store.Snapshot(note.Title)
		if snapshotErr != nil {
			continue
		}
		assetsDir := filepath.Join(m.store.DataDir(), note.Title, "assets")
		entries, readErr := os.ReadDir(assetsDir)
		if readErr != nil {
			continue
		}
		for _, entry := range entries {
			match := assetMetaPattern.FindStringSubmatch(entry.Name())
			if len(match) != 2 || !ValidAssetID(match[1]) {
				continue
			}
			asset, metaErr := readAssetMeta(assetsDir, match[1])
			if metaErr != nil {
				continue
			}
			asset.InstanceToken = snapshot.InstanceToken
			if asset.State == StateUploading || asset.State == StateProbing || asset.State == StateTranscoding {
				asset.State = StateQueued
				asset.Error = ""
				asset.UpdatedAt = time.Now().UTC()
				_ = writeAssetMeta(assetsDir, asset)
			}
			if asset.State == StateQueued {
				m.enqueue(job{Title: note.Title, InstanceToken: snapshot.InstanceToken, AssetID: asset.ID})
			}
		}
	}
}

func readAssetMeta(assetsDir, id string) (Asset, error) {
	if !ValidAssetID(id) {
		return Asset{}, errors.New("invalid asset id")
	}
	path := metaPath(assetsDir, id)
	info, err := os.Lstat(path)
	if err != nil {
		return Asset{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return Asset{}, errors.New("invalid asset metadata")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Asset{}, err
	}
	var asset Asset
	if err := json.Unmarshal(raw, &asset); err != nil {
		return Asset{}, err
	}
	if asset.ID != id || !ValidAssetID(asset.ID) {
		return Asset{}, errors.New("asset metadata id mismatch")
	}
	return asset, nil
}

func writeAssetMeta(assetsDir string, asset Asset) error {
	if !ValidAssetID(asset.ID) {
		return errors.New("invalid asset id")
	}
	raw, err := json.MarshalIndent(asset, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp, err := os.CreateTemp(assetsDir, ".asset-meta-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}
	if err := tmp.Chmod(0600); err != nil {
		cleanup()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, metaPath(assetsDir, asset.ID)); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func removeAssetFiles(assetsDir string, asset Asset) error {
	if !ValidAssetID(asset.ID) {
		return errors.New("invalid asset id")
	}
	paths := []string{uploadPath(assetsDir, asset.ID), metaPath(assetsDir, asset.ID)}
	if asset.Filename != "" && store.ValidAssetFilename(asset.Filename) && strings.HasPrefix(asset.Filename, asset.ID+".") {
		paths = append(paths, filepath.Join(assetsDir, asset.Filename))
	}
	for _, extension := range []string{".jpg", ".mp4"} {
		paths = append(paths, filepath.Join(assetsDir, ".transcode-"+asset.ID+extension))
	}
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func metaPath(assetsDir, id string) string   { return filepath.Join(assetsDir, ".asset-"+id+".json") }
func uploadPath(assetsDir, id string) string { return filepath.Join(assetsDir, ".upload-"+id) }
func jobKey(token, id string) string         { return token + ":" + id }
