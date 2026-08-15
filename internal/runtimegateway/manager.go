package runtimegateway

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"marvo/internal/agentcredentials"
	"marvo/internal/userid"
)

const (
	labelRole                  = "com.marvo.role"
	labelUserID                = "com.marvo.user-id"
	labelGeneration            = "com.marvo.generation"
	labelNetwork               = "com.marvo.runtime-network"
	labelCredentialFingerprint = "com.marvo.agent-credentials"
)

type RuntimeTarget struct {
	URL      *url.URL
	Username string
	Password string
}

type RuntimeProvider interface {
	Ensure(context.Context, string) (*RuntimeTarget, error)
}

type RuntimeManager struct {
	config     Config
	docker     *DockerClient
	generation string
	readyCheck func(context.Context, *RuntimeTarget) error
	mu         sync.Mutex
	locks      map[string]*sync.Mutex
	activityMu sync.Mutex
	activity   map[string]*runtimeActivity
}

type runtimeActivity struct {
	active       int
	lastActivity time.Time
}

func NewRuntimeManager(config Config, dockerClient *DockerClient) *RuntimeManager {
	manager := &RuntimeManager{
		config: config, docker: dockerClient, locks: make(map[string]*sync.Mutex),
		activity: make(map[string]*runtimeActivity),
	}
	manager.setGeneration(config.AgentGeneration)
	manager.readyCheck = manager.waitUntilReady
	return manager
}

func (m *RuntimeManager) setGeneration(imageIdentity string) {
	generationPayload := strings.Join([]string{
		"v1", m.config.AgentImage, m.config.AgentGeneration, imageIdentity, m.config.Network,
		strconv.Itoa(m.config.RuntimeUID), strconv.Itoa(m.config.RuntimeGID),
		strconv.FormatInt(m.config.MemoryBytes, 10), strconv.FormatInt(m.config.NanoCPUs, 10),
		strconv.FormatInt(m.config.PidsLimit, 10), runtimePassword(m.config.Token, "generation"),
	}, "\x00")
	digest := sha256.Sum256([]byte(generationPayload))
	m.generation = hex.EncodeToString(digest[:16])
}

func (m *RuntimeManager) Validate(ctx context.Context) error {
	if err := m.docker.Ping(ctx); err != nil {
		return fmt.Errorf("connect to Docker: %w", err)
	}
	if err := m.docker.InspectNetwork(ctx, m.config.Network); err != nil {
		return fmt.Errorf("inspect Docker network %q: %w", m.config.Network, err)
	}
	image, err := m.docker.InspectImage(ctx, m.config.AgentImage)
	if err != nil {
		return fmt.Errorf("inspect Agent image %q: %w", m.config.AgentImage, err)
	}
	m.setGeneration(image.ID)
	return nil
}

func (m *RuntimeManager) Ensure(ctx context.Context, userID string) (*RuntimeTarget, error) {
	if !validUserID(userID) {
		return nil, errors.New("invalid user id")
	}
	lock := m.userLock(userID)
	lock.Lock()
	defer lock.Unlock()

	if err := m.validateUserDirectories(userID); err != nil {
		return nil, err
	}
	credentialStore, err := agentcredentials.NewStore(
		filepath.Join(m.config.StateDir, "users", userID, "agent", "home", ".local", "share", "opencode"),
		userID,
		m.config.Token,
	)
	if err != nil {
		return nil, fmt.Errorf("open Agent credentials: %w", err)
	}
	credentials, err := credentialStore.Load()
	if err != nil {
		return nil, fmt.Errorf("load Agent credentials: %w", err)
	}
	credentialFingerprint := credentialStore.Fingerprint(credentials)
	name := runtimeContainerName(userID)
	container, err := m.docker.InspectContainer(ctx, name)
	if err != nil && !errors.Is(err, ErrDockerNotFound) {
		return nil, err
	}
	if container != nil {
		if container.Config.Labels[labelRole] != "agent" || container.Config.Labels[labelUserID] != userID ||
			container.Config.Labels[labelNetwork] != m.config.Network {
			return nil, fmt.Errorf("container name %q is already used by an unmanaged container", name)
		}
		if container.Config.Labels[labelGeneration] != m.generation ||
			container.Config.Labels[labelCredentialFingerprint] != credentialFingerprint {
			if err := m.docker.RemoveContainer(ctx, container.ID); err != nil {
				return nil, fmt.Errorf("replace outdated Agent runtime: %w", err)
			}
			container = nil
		}
	}
	if container == nil {
		id, err := m.docker.CreateContainer(ctx, name, m.containerSpec(userID, credentials, credentialFingerprint))
		if err != nil {
			return nil, fmt.Errorf("create Agent runtime: %w", err)
		}
		container = &DockerContainer{ID: id}
	}
	startedNow := !container.State.Running
	if startedNow {
		if err := m.docker.StartContainer(ctx, container.ID); err != nil {
			return nil, fmt.Errorf("start Agent runtime: %w", err)
		}
	}
	target := &RuntimeTarget{
		URL:      &url.URL{Scheme: "http", Host: name + ":4096"},
		Username: "opencode",
		Password: runtimePassword(m.config.Token, userID),
	}
	if startedNow || container.State.Health == nil || container.State.Health.Status != "healthy" {
		if err := m.readyCheck(ctx, target); err != nil {
			return nil, err
		}
	}
	return target, nil
}

func (m *RuntimeManager) BeginUse(userID string) func() {
	m.activityMu.Lock()
	activity := m.activity[userID]
	if activity == nil {
		activity = &runtimeActivity{}
		m.activity[userID] = activity
	}
	activity.active++
	activity.lastActivity = time.Now()
	m.activityMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			m.activityMu.Lock()
			if current := m.activity[userID]; current != nil {
				if current.active > 0 {
					current.active--
				}
				current.lastActivity = time.Now()
			}
			m.activityMu.Unlock()
		})
	}
}

func (m *RuntimeManager) ReapIdle(ctx context.Context, now time.Time) error {
	if m.config.IdleTimeout <= 0 {
		return nil
	}
	containers, err := m.docker.ListContainersByLabel(ctx, labelRole+"=agent")
	if err != nil {
		return fmt.Errorf("list Agent runtimes: %w", err)
	}
	var firstErr error
	for _, container := range containers {
		userID := container.Labels[labelUserID]
		if container.State != "running" || !validUserID(userID) || container.Labels[labelRole] != "agent" ||
			container.Labels[labelNetwork] != m.config.Network {
			continue
		}
		m.activityMu.Lock()
		activity := m.activity[userID]
		if activity == nil {
			activity = &runtimeActivity{lastActivity: now}
			m.activity[userID] = activity
		}
		idle := activity.active == 0 && !activity.lastActivity.IsZero() && now.Sub(activity.lastActivity) >= m.config.IdleTimeout
		m.activityMu.Unlock()
		if !idle {
			continue
		}

		lock := m.userLock(userID)
		lock.Lock()
		m.activityMu.Lock()
		activity = m.activity[userID]
		idle = activity != nil && activity.active == 0 && now.Sub(activity.lastActivity) >= m.config.IdleTimeout
		m.activityMu.Unlock()
		if idle {
			inspected, inspectErr := m.docker.InspectContainer(ctx, container.ID)
			if inspectErr == nil && inspected.Config.Labels[labelRole] == "agent" && inspected.Config.Labels[labelUserID] == userID &&
				inspected.Config.Labels[labelNetwork] == m.config.Network && inspected.State.Running {
				if stopErr := m.docker.StopContainer(ctx, inspected.ID); stopErr != nil && firstErr == nil {
					firstErr = fmt.Errorf("stop idle Agent runtime for %s: %w", userID, stopErr)
				}
			} else if inspectErr != nil && !errors.Is(inspectErr, ErrDockerNotFound) && firstErr == nil {
				firstErr = inspectErr
			}
		}
		lock.Unlock()
	}
	return firstErr
}

func (m *RuntimeManager) RunIdleReaper(ctx context.Context) {
	if m.config.IdleTimeout <= 0 {
		return
	}
	interval := time.Minute
	if half := m.config.IdleTimeout / 2; half < interval {
		interval = half
	}
	if interval < time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if err := m.ReapIdle(ctx, now); err != nil && ctx.Err() == nil {
				slog.Error("runtime gateway: idle cleanup failed", "error", err)
			}
		}
	}
}

func (m *RuntimeManager) containerSpec(
	userID string,
	credentials agentcredentials.Credentials,
	credentialFingerprint string,
) ContainerSpec {
	userRoot := filepath.Join(m.config.HostStateDir, "users", userID)
	initEnabled := true
	environment := []string{
		"HOME=/home/marvo",
		"MARVO_USER_ID=" + userID,
		"MARVO_TOOL_URL=http://marvo-runtime:4097/tool/" + userID,
		"MARVO_TOOL_TOKEN=" + agentToolToken(m.config.Token, userID),
		"TZ=Asia/Hong_Kong",
		"OPENCODE_ENABLE_EXA=1",
		"OPENCODE_SERVER_USERNAME=opencode",
		"OPENCODE_SERVER_PASSWORD=" + runtimePassword(m.config.Token, userID),
	}
	if credentials.ExaAPIKey != "" {
		environment = append(environment, "EXA_API_KEY="+credentials.ExaAPIKey)
	}
	return ContainerSpec{
		Image:      m.config.AgentImage,
		Env:        environment,
		WorkingDir: "/workspace",
		User:       fmt.Sprintf("%d:%d", m.config.RuntimeUID, m.config.RuntimeGID),
		Labels: map[string]string{
			labelRole: "agent", labelUserID: userID, labelGeneration: m.generation, labelNetwork: m.config.Network,
			labelCredentialFingerprint: credentialFingerprint,
		},
		Healthcheck: &Healthcheck{
			Test:     []string{"CMD-SHELL", `curl -fsS --max-time 2 -u "${OPENCODE_SERVER_USERNAME}:${OPENCODE_SERVER_PASSWORD}" http://127.0.0.1:4096/global/health >/dev/null`},
			Interval: int64(5 * time.Second), Timeout: int64(3 * time.Second),
			StartPeriod: int64(10 * time.Second), Retries: 6,
		},
		HostConfig: HostConfig{
			Mounts: []Mount{
				{Type: "bind", Source: filepath.Join(userRoot, "workspace"), Target: "/workspace"},
				{Type: "bind", Source: filepath.Join(userRoot, "agent", "home"), Target: "/home/marvo"},
			},
			NetworkMode: m.config.Network, RestartPolicy: RestartPolicy{Name: "no"},
			ReadonlyRootfs: true, CapDrop: []string{"ALL"}, SecurityOpt: []string{"no-new-privileges"},
			PidsLimit: m.config.PidsLimit, Memory: m.config.MemoryBytes, NanoCPUs: m.config.NanoCPUs,
			Tmpfs: map[string]string{"/tmp": "rw,nosuid,nodev,size=536870912"}, Init: &initEnabled,
		},
	}
}

func (m *RuntimeManager) validateUserDirectories(userID string) error {
	root := filepath.Join(m.config.StateDir, "users", userID)
	for _, path := range []string{
		root,
		filepath.Join(root, "workspace"),
		filepath.Join(root, "agent"),
		filepath.Join(root, "agent", "home"),
		filepath.Join(root, "agent", "home", ".local", "share", "opencode"),
	} {
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("inspect runtime directory %q: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("runtime directory %q is not a regular directory", path)
		}
	}
	return nil
}

func (m *RuntimeManager) waitUntilReady(parent context.Context, target *RuntimeTarget) error {
	ctx, cancel := context.WithTimeout(parent, m.config.ReadinessTimeout)
	defer cancel()
	client := &http.Client{Timeout: 3 * time.Second}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	var lastErr error
	for {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.URL.String()+"/global/health", nil)
		if err == nil {
			request.SetBasicAuth(target.Username, target.Password)
			response, requestErr := client.Do(request)
			if requestErr == nil {
				response.Body.Close()
				if response.StatusCode == http.StatusOK {
					return nil
				}
				lastErr = fmt.Errorf("health status %s", response.Status)
			} else {
				lastErr = requestErr
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("agent runtime did not become ready: %w (last error: %v)", ctx.Err(), lastErr)
		case <-ticker.C:
		}
	}
}

func (m *RuntimeManager) userLock(userID string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	if lock := m.locks[userID]; lock != nil {
		return lock
	}
	lock := &sync.Mutex{}
	m.locks[userID] = lock
	return lock
}

func runtimeContainerName(userID string) string {
	return "marvo-agent-" + userID
}

func runtimePassword(secret, userID string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte("marvo-agent-runtime-v1\x00" + userID))
	return hex.EncodeToString(mac.Sum(nil))
}

func validUserID(id string) bool {
	return userid.Valid(id)
}
