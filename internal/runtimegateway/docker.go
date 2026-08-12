package runtimegateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var ErrDockerNotFound = errors.New("docker object not found")

type DockerClient struct {
	http *http.Client
	base string
}

type DockerContainer struct {
	ID     string `json:"Id"`
	Name   string `json:"Name"`
	Config struct {
		Image  string            `json:"Image"`
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	State struct {
		Running bool   `json:"Running"`
		Status  string `json:"Status"`
		Health  *struct {
			Status string `json:"Status"`
		} `json:"Health"`
	} `json:"State"`
}

type DockerContainerSummary struct {
	ID     string            `json:"Id"`
	Labels map[string]string `json:"Labels"`
	State  string            `json:"State"`
}

type DockerImage struct {
	ID string `json:"Id"`
}

type ContainerSpec struct {
	Image       string            `json:"Image"`
	Env         []string          `json:"Env"`
	WorkingDir  string            `json:"WorkingDir"`
	User        string            `json:"User"`
	Labels      map[string]string `json:"Labels"`
	Healthcheck *Healthcheck      `json:"Healthcheck,omitempty"`
	HostConfig  HostConfig        `json:"HostConfig"`
}

type Healthcheck struct {
	Test        []string `json:"Test"`
	Interval    int64    `json:"Interval"`
	Timeout     int64    `json:"Timeout"`
	StartPeriod int64    `json:"StartPeriod"`
	Retries     int      `json:"Retries"`
}

type HostConfig struct {
	Mounts         []Mount           `json:"Mounts"`
	NetworkMode    string            `json:"NetworkMode"`
	RestartPolicy  RestartPolicy     `json:"RestartPolicy"`
	ReadonlyRootfs bool              `json:"ReadonlyRootfs"`
	CapDrop        []string          `json:"CapDrop"`
	SecurityOpt    []string          `json:"SecurityOpt"`
	PidsLimit      int64             `json:"PidsLimit,omitempty"`
	Memory         int64             `json:"Memory,omitempty"`
	NanoCPUs       int64             `json:"NanoCpus,omitempty"`
	Tmpfs          map[string]string `json:"Tmpfs"`
	Init           *bool             `json:"Init,omitempty"`
}

type Mount struct {
	Type     string `json:"Type"`
	Source   string `json:"Source"`
	Target   string `json:"Target"`
	ReadOnly bool   `json:"ReadOnly"`
}

type RestartPolicy struct {
	Name string `json:"Name"`
}

func NewDockerClient(socketPath string) *DockerClient {
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", socketPath)
		},
		MaxIdleConns:          20,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}
	return &DockerClient{http: &http.Client{Transport: transport}, base: "http://docker"}
}

func (d *DockerClient) Ping(ctx context.Context) error {
	return d.do(ctx, http.MethodGet, "/_ping", nil, nil)
}

func (d *DockerClient) InspectNetwork(ctx context.Context, name string) error {
	return d.do(ctx, http.MethodGet, "/networks/"+url.PathEscape(name), nil, nil)
}

func (d *DockerClient) InspectImage(ctx context.Context, name string) (*DockerImage, error) {
	var image DockerImage
	if err := d.do(ctx, http.MethodGet, "/images/"+url.PathEscape(name)+"/json", nil, &image); err != nil {
		return nil, err
	}
	if image.ID == "" {
		return nil, errors.New("docker returned an empty image ID")
	}
	return &image, nil
}

func (d *DockerClient) InspectContainer(ctx context.Context, name string) (*DockerContainer, error) {
	var container DockerContainer
	if err := d.do(ctx, http.MethodGet, "/containers/"+url.PathEscape(name)+"/json", nil, &container); err != nil {
		return nil, err
	}
	return &container, nil
}

func (d *DockerClient) CreateContainer(ctx context.Context, name string, spec ContainerSpec) (string, error) {
	var result struct {
		ID string `json:"Id"`
	}
	path := "/containers/create?name=" + url.QueryEscape(name)
	if err := d.do(ctx, http.MethodPost, path, spec, &result); err != nil {
		return "", err
	}
	if result.ID == "" {
		return "", errors.New("docker returned an empty container ID")
	}
	return result.ID, nil
}

func (d *DockerClient) StartContainer(ctx context.Context, id string) error {
	return d.do(ctx, http.MethodPost, "/containers/"+url.PathEscape(id)+"/start", nil, nil)
}

func (d *DockerClient) StopContainer(ctx context.Context, id string) error {
	return d.do(ctx, http.MethodPost, "/containers/"+url.PathEscape(id)+"/stop?t=10", nil, nil)
}

func (d *DockerClient) ListContainersByLabel(ctx context.Context, label string) ([]DockerContainerSummary, error) {
	filters, err := json.Marshal(map[string][]string{"label": {label}})
	if err != nil {
		return nil, err
	}
	var containers []DockerContainerSummary
	path := "/containers/json?all=1&filters=" + url.QueryEscape(string(filters))
	if err := d.do(ctx, http.MethodGet, path, nil, &containers); err != nil {
		return nil, err
	}
	return containers, nil
}

func (d *DockerClient) RemoveContainer(ctx context.Context, id string) error {
	return d.do(ctx, http.MethodDelete, "/containers/"+url.PathEscape(id)+"?force=1&v=0", nil, nil)
}

func (d *DockerClient) do(ctx context.Context, method, path string, input, output any) error {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, d.base+path, body)
	if err != nil {
		return err
	}
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := d.http.Do(request)
	if err != nil {
		return fmt.Errorf("docker API request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		io.Copy(io.Discard, response.Body)
		return ErrDockerNotFound
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		limited, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		var failure struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(limited, &failure)
		if failure.Message == "" {
			failure.Message = strings.TrimSpace(string(limited))
		}
		return fmt.Errorf("docker API %s: %s", response.Status, failure.Message)
	}
	if output == nil {
		io.Copy(io.Discard, response.Body)
		return nil
	}
	if err := json.NewDecoder(response.Body).Decode(output); err != nil {
		return fmt.Errorf("decode Docker API response: %w", err)
	}
	return nil
}
