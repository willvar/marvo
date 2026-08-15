package store

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"
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

type deviceFile struct {
	Pending  map[string]*PendingRequest       `json:"pending"`
	Approved map[string]*approvedDeviceRecord `json:"approved"`
}

type DeviceStore struct {
	state  *StateDB
	secret string
}

const MaxDeviceNameRunes = 50

var (
	ErrTooManyPendingDevices = errors.New("too many pending device applications")
	ErrInvalidDeviceName     = errors.New("invalid device name")
	ErrDeviceNameConflict    = errors.New("device name already exists")
)

func NewDeviceStore(state *StateDB, sessionSecret string) *DeviceStore {
	return &DeviceStore{state: state, secret: sessionSecret}
}

func normalizeDeviceName(name string) string {
	return norm.NFC.String(strings.TrimSpace(name))
}

func deviceNameKey(name string) string {
	return strings.ToLower(normalizeDeviceName(name))
}

func validDeviceName(name string) bool {
	length := len([]rune(name))
	return length > 0 && length <= MaxDeviceNameRunes
}

func truncateDeviceName(name string, maximum int) string {
	characters := []rune(name)
	if len(characters) <= maximum {
		return name
	}
	if maximum <= 0 {
		return ""
	}
	return strings.TrimSpace(string(characters[:maximum]))
}

func nextUniqueDeviceName(name string, used func(string) bool) string {
	base := normalizeDeviceName(name)
	if base == "" {
		base = "未命名设备"
	}
	base = truncateDeviceName(base, MaxDeviceNameRunes)
	if !used(base) {
		return base
	}
	for index := 2; ; index++ {
		suffix := fmt.Sprintf(" (%d)", index)
		prefix := truncateDeviceName(base, MaxDeviceNameRunes-len([]rune(suffix)))
		candidate := prefix + suffix
		if !used(candidate) {
			return candidate
		}
	}
}

func (ds *DeviceStore) CreateRequest(localDeviceID string, deviceName string, info DeviceInfo) (*PendingRequest, error) {
	deviceName = normalizeDeviceName(deviceName)
	if !validDeviceName(deviceName) {
		return nil, ErrInvalidDeviceName
	}
	tx, err := ds.state.sql.BeginTx(context.Background(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM devices WHERE local_device_id = ?`, localDeviceID).Scan(&exists); err != nil {
		return nil, err
	}
	if exists > 0 {
		return nil, tx.Commit()
	}
	request, err := findPendingRequest(tx, localDeviceID)
	if err == nil {
		return request, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err := tx.QueryRow(`SELECT COUNT(*) FROM device_requests`).Scan(&exists); err != nil {
		return nil, err
	}
	if exists >= 1000 {
		return nil, ErrTooManyPendingDevices
	}
	id, err := randomID()
	if err != nil {
		return nil, err
	}
	request = &PendingRequest{ID: id, LocalDeviceID: localDeviceID, DeviceName: deviceName, DeviceInfo: info, CreatedAt: time.Now().UTC()}
	infoJSON, err := json.Marshal(info)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`
		INSERT INTO device_requests(id, local_device_id, device_name, info_json, created_at)
		VALUES(?, ?, ?, ?, ?)
	`, request.ID, request.LocalDeviceID, request.DeviceName, string(infoJSON), request.CreatedAt.UnixMilli()); err != nil {
		return nil, err
	}
	return request, tx.Commit()
}

func (ds *DeviceStore) FindByLocalDeviceID(localDeviceID string) (*ApprovedDevice, *PendingRequest) {
	device, err := findApprovedDevice(ds.state.sql, localDeviceID, true)
	if err == nil {
		return device, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		slog.Error("failed to find approved device", "error", err)
		return nil, nil
	}
	request, err := findPendingRequest(ds.state.sql, localDeviceID)
	if err == nil {
		return nil, request
	}
	if !errors.Is(err, sql.ErrNoRows) {
		slog.Error("failed to find pending device", "error", err)
	}
	return nil, nil
}

func (ds *DeviceStore) ListRequests() []*PendingRequest {
	rows, err := ds.state.sql.Query(`
		SELECT id, local_device_id, device_name, info_json, created_at
		FROM device_requests ORDER BY created_at DESC, id DESC
	`)
	if err != nil {
		slog.Error("failed to list device requests", "error", err)
		return []*PendingRequest{}
	}
	defer rows.Close()
	result := make([]*PendingRequest, 0)
	for rows.Next() {
		request, scanErr := scanPendingRequest(rows)
		if scanErr != nil {
			slog.Error("failed to read device request", "error", scanErr)
			return []*PendingRequest{}
		}
		result = append(result, request)
	}
	return result
}

func (ds *DeviceStore) ApproveRequest(id string) (*ApprovedDevice, error) {
	tx, err := ds.state.sql.BeginTx(context.Background(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	request, err := findPendingRequestByID(tx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, tx.Commit()
	}
	if err != nil {
		return nil, err
	}
	token, err := randomID()
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(`SELECT device_name_key FROM devices`)
	if err != nil {
		return nil, err
	}
	usedNames := make(map[string]struct{})
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			_ = rows.Close()
			return nil, err
		}
		usedNames[key] = struct{}{}
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	name := nextUniqueDeviceName(request.DeviceName, func(candidate string) bool {
		_, exists := usedNames[deviceNameKey(candidate)]
		return exists
	})
	device := &ApprovedDevice{
		ID: request.ID, LocalDeviceID: request.LocalDeviceID, DeviceName: name,
		DeviceInfo: request.DeviceInfo, Token: token, ApprovedAt: time.Now().UTC(),
	}
	infoJSON, err := json.Marshal(device.DeviceInfo)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`DELETE FROM device_requests WHERE id = ?`, id); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`
		INSERT INTO devices(local_device_id, id, device_name, device_name_key, info_json, token, approved_at)
		VALUES(?, ?, ?, ?, ?, ?, ?)
	`, device.LocalDeviceID, device.ID, device.DeviceName, deviceNameKey(device.DeviceName), string(infoJSON), device.Token,
		device.ApprovedAt.UnixMilli()); err != nil {
		return nil, err
	}
	return device, tx.Commit()
}

func (ds *DeviceStore) RejectRequest(id string) (bool, error) {
	result, err := ds.state.sql.Exec(`DELETE FROM device_requests WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected > 0, err
}

func (ds *DeviceStore) ListDevices() []*ApprovedDevice {
	rows, err := ds.state.sql.Query(`
		SELECT id, local_device_id, device_name, info_json, approved_at
		FROM devices ORDER BY approved_at DESC, id DESC
	`)
	if err != nil {
		slog.Error("failed to list approved devices", "error", err)
		return []*ApprovedDevice{}
	}
	defer rows.Close()
	result := make([]*ApprovedDevice, 0)
	for rows.Next() {
		device, scanErr := scanApprovedDevice(rows, false)
		if scanErr != nil {
			slog.Error("failed to read approved device", "error", scanErr)
			return []*ApprovedDevice{}
		}
		result = append(result, device)
	}
	return result
}

func (ds *DeviceStore) RenameDevice(localDeviceID string, deviceName string) (*ApprovedDevice, error) {
	deviceName = normalizeDeviceName(deviceName)
	if !validDeviceName(deviceName) {
		return nil, ErrInvalidDeviceName
	}
	tx, err := ds.state.sql.BeginTx(context.Background(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	device, err := findApprovedDevice(tx, localDeviceID, false)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, tx.Commit()
	}
	if err != nil {
		return nil, err
	}
	if device.DeviceName == deviceName {
		return device, tx.Commit()
	}
	var exists int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM devices WHERE device_name_key = ? AND local_device_id <> ?`, deviceNameKey(deviceName), localDeviceID).Scan(&exists); err != nil {
		return nil, err
	}
	if exists > 0 {
		return nil, ErrDeviceNameConflict
	}
	if _, err := tx.Exec(`UPDATE devices SET device_name = ?, device_name_key = ? WHERE local_device_id = ?`, deviceName, deviceNameKey(deviceName), localDeviceID); err != nil {
		return nil, err
	}
	device.DeviceName = deviceName
	return device, tx.Commit()
}

func (ds *DeviceStore) RevokeDevice(localDeviceID string) (bool, error) {
	result, err := ds.state.sql.Exec(`DELETE FROM devices WHERE local_device_id = ?`, localDeviceID)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected > 0, err
}

func (ds *DeviceStore) SignToken(token string) string {
	mac := hmac.New(sha256.New, []byte(ds.secret))
	_, _ = mac.Write([]byte(token))
	return hex.EncodeToString(mac.Sum(nil))
}

func (ds *DeviceStore) VerifyToken(token, signature string) bool {
	if token == "" || signature == "" || ds.secret == "" || !hmac.Equal([]byte(signature), []byte(ds.SignToken(token))) {
		return false
	}
	var stored string
	if err := ds.state.sql.QueryRow(`SELECT token FROM devices WHERE token = ?`, token).Scan(&stored); err != nil {
		return false
	}
	return hmac.Equal([]byte(stored), []byte(token))
}

type rowScanner interface {
	Scan(...any) error
}

type queryRower interface {
	QueryRow(string, ...any) *sql.Row
}

func findPendingRequest(source queryRower, localDeviceID string) (*PendingRequest, error) {
	return scanPendingRequest(source.QueryRow(`
		SELECT id, local_device_id, device_name, info_json, created_at
		FROM device_requests WHERE local_device_id = ?
	`, localDeviceID))
}

func findPendingRequestByID(source queryRower, id string) (*PendingRequest, error) {
	return scanPendingRequest(source.QueryRow(`
		SELECT id, local_device_id, device_name, info_json, created_at
		FROM device_requests WHERE id = ?
	`, id))
}

func scanPendingRequest(row rowScanner) (*PendingRequest, error) {
	var request PendingRequest
	var infoJSON string
	var createdAt int64
	if err := row.Scan(&request.ID, &request.LocalDeviceID, &request.DeviceName, &infoJSON, &createdAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(infoJSON), &request.DeviceInfo); err != nil {
		return nil, err
	}
	request.CreatedAt = time.UnixMilli(createdAt).UTC()
	return &request, nil
}

func findApprovedDevice(source queryRower, localDeviceID string, includeToken bool) (*ApprovedDevice, error) {
	columns := "id, local_device_id, device_name, info_json, approved_at"
	if includeToken {
		columns += ", token"
	}
	return scanApprovedDevice(source.QueryRow(`SELECT `+columns+` FROM devices WHERE local_device_id = ?`, localDeviceID), includeToken)
}

func scanApprovedDevice(row rowScanner, includeToken bool) (*ApprovedDevice, error) {
	var device ApprovedDevice
	var infoJSON string
	var approvedAt int64
	var err error
	if includeToken {
		err = row.Scan(&device.ID, &device.LocalDeviceID, &device.DeviceName, &infoJSON, &approvedAt, &device.Token)
	} else {
		err = row.Scan(&device.ID, &device.LocalDeviceID, &device.DeviceName, &infoJSON, &approvedAt)
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(infoJSON), &device.DeviceInfo); err != nil {
		return nil, err
	}
	device.ApprovedAt = time.UnixMilli(approvedAt).UTC()
	return &device, nil
}

func writePrivateFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(dir, ".marvo-state-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	cleanup := func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}
	if err := temporary.Chmod(0600); err != nil {
		cleanup()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := temporary.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		_ = os.Remove(temporaryPath)
		return err
	}
	if directory, err := os.Open(dir); err == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
	return nil
}

func randomID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}
