package runtimegateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"marvo/internal/agentcredentials"
)

func TestRuntimeManagerCreatesConstrainedPerUserContainer(t *testing.T) {
	stateRoot := t.TempDir()
	createRuntimeDirectories(t, stateRoot, gatewayTestUserID)
	var created ContainerSpec
	var started string
	dockerAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/containers/"+runtimeContainerName(gatewayTestUserID)+"/json":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"not found"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/containers/create":
			if got := r.URL.Query().Get("name"); got != runtimeContainerName(gatewayTestUserID) {
				t.Fatalf("container name = %q", got)
			}
			if err := json.NewDecoder(r.Body).Decode(&created); err != nil {
				t.Fatal(err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Id":"new-container"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/containers/new-container/start":
			started = "new-container"
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected Docker API request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer dockerAPI.Close()
	config := testRuntimeConfig(stateRoot)
	manager := NewRuntimeManager(config, testDockerClient(dockerAPI))
	manager.readyCheck = func(context.Context, *RuntimeTarget) error { return nil }
	target, err := manager.Ensure(context.Background(), gatewayTestUserID)
	if err != nil {
		t.Fatal(err)
	}
	if started != "new-container" || target.URL.Host != runtimeContainerName(gatewayTestUserID)+":4096" {
		t.Fatalf("started = %q, target = %s", started, target.URL)
	}
	if created.Image != config.AgentImage || created.User != "1000:1001" || created.WorkingDir != "/workspace" {
		t.Fatalf("container identity = %#v", created)
	}
	if created.Labels[labelRole] != "agent" || created.Labels[labelUserID] != gatewayTestUserID ||
		created.Labels[labelGeneration] == "" || created.Labels[labelNetwork] != config.Network {
		t.Fatalf("container labels = %#v", created.Labels)
	}
	if !created.HostConfig.ReadonlyRootfs || strings.Join(created.HostConfig.CapDrop, ",") != "ALL" || len(created.HostConfig.SecurityOpt) == 0 {
		t.Fatalf("container hardening = %#v", created.HostConfig)
	}
	if created.HostConfig.NetworkMode != config.Network || created.HostConfig.RestartPolicy.Name != "no" || len(created.HostConfig.Mounts) != 2 {
		t.Fatalf("container network/mounts = %#v", created.HostConfig)
	}
	wantWorkspace := filepath.Join(stateRoot, "users", gatewayTestUserID, "workspace")
	wantAgentHome := filepath.Join(stateRoot, "users", gatewayTestUserID, "agent", "home")
	if created.HostConfig.Mounts[0].Source != wantWorkspace || created.HostConfig.Mounts[0].Target != "/workspace" ||
		created.HostConfig.Mounts[1].Source != wantAgentHome || created.HostConfig.Mounts[1].Target != "/home/marvo" {
		t.Fatalf("container mounts = %#v", created.HostConfig.Mounts)
	}
	joinedEnv := strings.Join(created.Env, "\n")
	if strings.Contains(joinedEnv, config.Token) || !strings.Contains(joinedEnv, "OPENCODE_SERVER_PASSWORD=") {
		t.Fatal("runtime environment contains the gateway token or lacks an OpenCode password")
	}
	if !strings.Contains(joinedEnv, "OPENCODE_ENABLE_EXA=1") || strings.Contains(joinedEnv, "EXA_API_KEY=") {
		t.Fatalf("unexpected default Exa environment = %q", created.Env)
	}
}

func TestRuntimeManagerRefusesUnmanagedNameCollision(t *testing.T) {
	stateRoot := t.TempDir()
	createRuntimeDirectories(t, stateRoot, gatewayTestUserID)
	dockerAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("manager tried to mutate unmanaged container: %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Id":"foreign","Config":{"Labels":{}},"State":{"Running":true}}`))
	}))
	defer dockerAPI.Close()
	manager := NewRuntimeManager(testRuntimeConfig(stateRoot), testDockerClient(dockerAPI))
	if _, err := manager.Ensure(context.Background(), gatewayTestUserID); err == nil || !strings.Contains(err.Error(), "unmanaged") {
		t.Fatalf("collision error = %v", err)
	}
}

func TestRuntimeManagerReplacesContainerWhenExaCredentialChanges(t *testing.T) {
	stateRoot := t.TempDir()
	createRuntimeDirectories(t, stateRoot, gatewayTestUserID)
	config := testRuntimeConfig(stateRoot)
	credentialStore, err := agentcredentials.NewStore(
		filepath.Join(stateRoot, "users", gatewayTestUserID, "agent", "home", ".local", "share", "opencode"),
		gatewayTestUserID,
		config.Token,
	)
	if err != nil {
		t.Fatal(err)
	}
	const exaAPIKey = "exa-runtime-secret"
	if err := credentialStore.Save(agentcredentials.Credentials{ExaAPIKey: exaAPIKey}); err != nil {
		t.Fatal(err)
	}

	var manager *RuntimeManager
	var created ContainerSpec
	removed := false
	started := false
	dockerAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/containers/"+runtimeContainerName(gatewayTestUserID)+"/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Id":"old-container","Config":{"Labels":{"com.marvo.role":"agent","com.marvo.user-id":"` + gatewayTestUserID + `","com.marvo.generation":"` + manager.generation + `","com.marvo.runtime-network":"` + config.Network + `","com.marvo.agent-credentials":"old-fingerprint"}},"State":{"Running":true}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/containers/old-container":
			removed = true
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/containers/create":
			if err := json.NewDecoder(r.Body).Decode(&created); err != nil {
				t.Fatal(err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Id":"new-container"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/containers/new-container/start":
			started = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected Docker API request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer dockerAPI.Close()
	manager = NewRuntimeManager(config, testDockerClient(dockerAPI))
	manager.readyCheck = func(context.Context, *RuntimeTarget) error { return nil }
	if _, err := manager.Ensure(context.Background(), gatewayTestUserID); err != nil {
		t.Fatal(err)
	}
	if !removed || !started {
		t.Fatalf("removed = %t, started = %t", removed, started)
	}
	joinedEnvironment := strings.Join(created.Env, "\n")
	if !strings.Contains(joinedEnvironment, "EXA_API_KEY="+exaAPIKey) {
		t.Fatalf("runtime environment does not contain the configured Exa API key: %q", created.Env)
	}
	fingerprint := created.Labels[labelCredentialFingerprint]
	if fingerprint == "" || fingerprint == "old-fingerprint" || strings.Contains(fingerprint, exaAPIKey) {
		t.Fatalf("credential fingerprint = %q", fingerprint)
	}
	for key, value := range created.Labels {
		if strings.Contains(key, exaAPIKey) || strings.Contains(value, exaAPIKey) {
			t.Fatalf("container label exposes Exa API key: %s=%s", key, value)
		}
	}
}

func TestRuntimeManagerStopsOnlyIdleManagedContainers(t *testing.T) {
	stateRoot := t.TempDir()
	createRuntimeDirectories(t, stateRoot, gatewayTestUserID)
	stopped := false
	dockerAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/containers/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"Id":"managed","Labels":{"com.marvo.role":"agent","com.marvo.user-id":"` + gatewayTestUserID + `","com.marvo.runtime-network":"marvo-runtime-test"},"State":"running"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/containers/managed/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Id":"managed","Config":{"Labels":{"com.marvo.role":"agent","com.marvo.user-id":"` + gatewayTestUserID + `","com.marvo.runtime-network":"marvo-runtime-test"}},"State":{"Running":true}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/containers/managed/stop":
			stopped = true
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer dockerAPI.Close()
	config := testRuntimeConfig(stateRoot)
	config.IdleTimeout = time.Minute
	manager := NewRuntimeManager(config, testDockerClient(dockerAPI))
	base := time.Now()
	if err := manager.ReapIdle(context.Background(), base); err != nil {
		t.Fatal(err)
	}
	if stopped {
		t.Fatal("newly discovered runtime stopped immediately")
	}
	if err := manager.ReapIdle(context.Background(), base.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if !stopped {
		t.Fatal("idle managed runtime was not stopped")
	}
}

func TestRuntimeManagerKeepsContainerWhileRequestIsActive(t *testing.T) {
	stateRoot := t.TempDir()
	createRuntimeDirectories(t, stateRoot, gatewayTestUserID)
	stopCalls := 0
	dockerAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/containers/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"Id":"managed","Labels":{"com.marvo.role":"agent","com.marvo.user-id":"` + gatewayTestUserID + `","com.marvo.runtime-network":"marvo-runtime-test"},"State":"running"}]`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/stop"):
			stopCalls++
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer dockerAPI.Close()
	config := testRuntimeConfig(stateRoot)
	config.IdleTimeout = time.Second
	manager := NewRuntimeManager(config, testDockerClient(dockerAPI))
	release := manager.BeginUse(gatewayTestUserID)
	manager.activityMu.Lock()
	manager.activity[gatewayTestUserID].lastActivity = time.Now().Add(-time.Hour)
	manager.activityMu.Unlock()
	if err := manager.ReapIdle(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	release()
	if stopCalls != 0 {
		t.Fatalf("active runtime stop calls = %d", stopCalls)
	}
}

func testRuntimeConfig(stateRoot string) Config {
	return Config{
		Token: "gateway-runtime-test-token-with-enough-entropy", StateDir: stateRoot, HostStateDir: stateRoot,
		AgentImage: "marvo-opencode:test", Network: "marvo-runtime-test",
		AgentGeneration: "sha256:test-image",
		RuntimeUID:      1000, RuntimeGID: 1001, MemoryBytes: 1 << 30,
		NanoCPUs: 2_000_000_000, PidsLimit: 256, ReadinessTimeout: time.Second,
	}
}

func testDockerClient(server *httptest.Server) *DockerClient {
	return &DockerClient{http: server.Client(), base: strings.TrimRight(server.URL, "/")}
}

func createRuntimeDirectories(t *testing.T, stateRoot, userID string) {
	t.Helper()
	for _, path := range []string{
		filepath.Join(stateRoot, "users", userID, "workspace"),
		filepath.Join(stateRoot, "users", userID, "agent", "home", ".local", "share", "opencode"),
	} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
}
