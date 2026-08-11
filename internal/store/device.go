package store

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type DeviceInfo struct {
	UserAgent                 string `json:"user_agent"`
	Platform                  string `json:"platform"`
	Language                  string `json:"language"`
	Screen                    string `json:"screen"`
	PixelRatio                string `json:"pixel_ratio"`
	Timezone                  string `json:"timezone"`
	Cores                     int    `json:"cores"`
	TouchPoints               int    `json:"touch_points"`
	ColorDepth                int    `json:"color_depth"`
	GPUVendor                 string `json:"gpu_vendor"`
	GPURenderer               string `json:"gpu_renderer"`
	GPUTextureSize            int    `json:"gpu_texture_size"`
	GPURenderbufferSize       int    `json:"gpu_renderbuffer_size"`
	GPUCubeMapSize            int    `json:"gpu_cube_map_size"`
	GPUViewportDims           string `json:"gpu_viewport_dims"`
	GPUVertexTextureUnits     int    `json:"gpu_vertex_texture_units"`
	GPUCombinedTextureUnits   int    `json:"gpu_combined_texture_units"`
	GPUVaryingVectors         int    `json:"gpu_varying_vectors"`
	GPUFragmentUniformVectors int    `json:"gpu_fragment_uniform_vectors"`
	GPUShadingLangVersion     string `json:"gpu_shading_lang_version"`
	WGPUArchitecture          string `json:"wgpu_architecture"`
	WGPUDevice                string `json:"wgpu_device"`
	WGPUDescription           string `json:"wgpu_description"`
	IPAddress                 string `json:"ip_address"`
}

type PendingRequest struct {
	ID            string     `json:"id"`
	LocalDeviceID string     `json:"local_device_id"`
	DeviceName    string     `json:"device_name"`
	DeviceInfo    DeviceInfo `json:"device_info"`
	CreatedAt     time.Time  `json:"created_at"`
}

type ApprovedDevice struct {
	ID            string     `json:"id"`
	LocalDeviceID string     `json:"local_device_id"`
	DeviceName    string     `json:"device_name"`
	DeviceInfo    DeviceInfo `json:"device_info"`
	Token         string     `json:"-"`
	ApprovedAt    time.Time  `json:"approved_at"`
}

type approvedDeviceRecord struct {
	ID            string     `json:"id"`
	LocalDeviceID string     `json:"local_device_id"`
	DeviceName    string     `json:"device_name"`
	DeviceInfo    DeviceInfo `json:"device_info"`
	Token         string     `json:"token"`
	ApprovedAt    time.Time  `json:"approved_at"`
}

type DeviceStore struct {
	mu       sync.RWMutex
	pending  map[string]*PendingRequest
	approved map[string]*ApprovedDevice
	path     string
	secret   string
}

type deviceFile struct {
	Pending  map[string]*PendingRequest       `json:"pending"`
	Approved map[string]*approvedDeviceRecord `json:"approved"`
}

var ErrTooManyPendingDevices = errors.New("too many pending device applications")

func NewDeviceStore(dataDir string, sessionSecret string) *DeviceStore {
	ds := &DeviceStore{
		pending:  make(map[string]*PendingRequest),
		approved: make(map[string]*ApprovedDevice),
		path:     dataDir,
		secret:   sessionSecret,
	}
	ds.load()
	return ds
}

func (ds *DeviceStore) load() {
	file := ds.devicesFile()
	if info, err := os.Lstat(file); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			slog.Warn("devices file is not a regular file", "path", file)
			return
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		slog.Warn("failed to inspect devices file", "error", err)
		return
	}
	data, err := os.ReadFile(file)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("failed to read devices file", "error", err)
		}
		return
	}
	var df deviceFile
	if err := json.Unmarshal(data, &df); err != nil {
		slog.Warn("failed to parse devices file", "error", err)
		ds.pending = make(map[string]*PendingRequest)
		ds.approved = make(map[string]*ApprovedDevice)
		return
	}
	if df.Pending != nil {
		ds.pending = df.Pending
	}
	if df.Approved != nil {
		ds.approved = make(map[string]*ApprovedDevice, len(df.Approved))
		for key, record := range df.Approved {
			if record == nil || record.Token == "" {
				continue
			}
			ds.approved[key] = &ApprovedDevice{
				ID: record.ID, LocalDeviceID: record.LocalDeviceID,
				DeviceName: record.DeviceName, DeviceInfo: record.DeviceInfo,
				Token: record.Token, ApprovedAt: record.ApprovedAt,
			}
		}
	}
}

func (ds *DeviceStore) saveLocked() error {
	approved := make(map[string]*approvedDeviceRecord, len(ds.approved))
	for key, dev := range ds.approved {
		approved[key] = &approvedDeviceRecord{
			ID: dev.ID, LocalDeviceID: dev.LocalDeviceID,
			DeviceName: dev.DeviceName, DeviceInfo: dev.DeviceInfo,
			Token: dev.Token, ApprovedAt: dev.ApprovedAt,
		}
	}
	df := deviceFile{Pending: ds.pending, Approved: approved}
	data, err := json.MarshalIndent(df, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writePrivateFileAtomic(ds.devicesFile(), data)
}

func (ds *DeviceStore) devicesFile() string {
	return filepath.Join(ds.path, ".devices.json")
}

func (ds *DeviceStore) CreateRequest(localDeviceID string, deviceName string, info DeviceInfo) (*PendingRequest, error) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	if _, ok := ds.approved[localDeviceID]; ok {
		return nil, nil
	}

	for _, r := range ds.pending {
		if r.LocalDeviceID == localDeviceID {
			return r, nil
		}
	}
	if len(ds.pending) >= 1000 {
		return nil, ErrTooManyPendingDevices
	}

	id, err := randomID()
	if err != nil {
		return nil, err
	}
	req := &PendingRequest{
		ID:            id,
		LocalDeviceID: localDeviceID,
		DeviceName:    deviceName,
		DeviceInfo:    info,
		CreatedAt:     time.Now(),
	}
	ds.pending[id] = req
	if err := ds.saveLocked(); err != nil {
		delete(ds.pending, id)
		return nil, err
	}
	return req, nil
}

func (ds *DeviceStore) FindByLocalDeviceID(localDeviceID string) (*ApprovedDevice, *PendingRequest) {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	if dev, ok := ds.approved[localDeviceID]; ok {
		return dev, nil
	}

	for _, r := range ds.pending {
		if r.LocalDeviceID == localDeviceID {
			return nil, r
		}
	}
	return nil, nil
}

func (ds *DeviceStore) ListRequests() []*PendingRequest {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	result := make([]*PendingRequest, 0, len(ds.pending))
	for _, r := range ds.pending {
		result = append(result, r)
	}
	return result
}

func (ds *DeviceStore) ApproveRequest(id string) (*ApprovedDevice, error) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	req, ok := ds.pending[id]
	if !ok {
		return nil, nil
	}

	token, err := randomID()
	if err != nil {
		return nil, err
	}
	dev := &ApprovedDevice{
		ID:            req.ID,
		LocalDeviceID: req.LocalDeviceID,
		DeviceName:    req.DeviceName,
		DeviceInfo:    req.DeviceInfo,
		Token:         token,
		ApprovedAt:    time.Now(),
	}
	delete(ds.pending, id)
	ds.approved[req.LocalDeviceID] = dev
	if err := ds.saveLocked(); err != nil {
		delete(ds.approved, req.LocalDeviceID)
		ds.pending[id] = req
		return nil, err
	}
	return dev, nil
}

func (ds *DeviceStore) RejectRequest(id string) (bool, error) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	req, exists := ds.pending[id]
	if !exists {
		return false, nil
	}
	delete(ds.pending, id)
	if err := ds.saveLocked(); err != nil {
		ds.pending[id] = req
		return false, err
	}
	return true, nil
}

func (ds *DeviceStore) ListDevices() []*ApprovedDevice {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	result := make([]*ApprovedDevice, 0, len(ds.approved))
	for _, d := range ds.approved {
		copy := *d
		copy.Token = ""
		result = append(result, &copy)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ApprovedAt.Equal(result[j].ApprovedAt) {
			return result[i].ID < result[j].ID
		}
		return result[i].ApprovedAt.After(result[j].ApprovedAt)
	})
	return result
}

func (ds *DeviceStore) RevokeDevice(localDeviceID string) (bool, error) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	if device, ok := ds.approved[localDeviceID]; ok {
		delete(ds.approved, localDeviceID)
		if err := ds.saveLocked(); err != nil {
			ds.approved[localDeviceID] = device
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func (ds *DeviceStore) SignToken(token string) string {
	mac := hmac.New(sha256.New, []byte(ds.secret))
	mac.Write([]byte(token))
	return hex.EncodeToString(mac.Sum(nil))
}

func (ds *DeviceStore) VerifyToken(token, sig string) bool {
	if token == "" || sig == "" || ds.secret == "" || !hmac.Equal([]byte(sig), []byte(ds.SignToken(token))) {
		return false
	}
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	for _, dev := range ds.approved {
		if dev != nil && hmac.Equal([]byte(dev.Token), []byte(token)) {
			return true
		}
	}
	return false
}

func writePrivateFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".marvo-devices-*")
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
	if _, err := tmp.Write(data); err != nil {
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
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if dirFile, err := os.Open(dir); err == nil {
		_ = dirFile.Sync()
		_ = dirFile.Close()
	}
	return nil
}

func randomID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
