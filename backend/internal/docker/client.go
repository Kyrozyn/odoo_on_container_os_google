package docker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client communicates with the Docker Engine daemon over a Unix socket.
type Client struct {
	socketPath string
	httpClient *http.Client
	baseURL    string
}

// NewClient initializes a Unix-socket Docker Engine API client.
func NewClient(socketPath string) *Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socketPath)
		},
	}
	return &Client{
		socketPath: socketPath,
		baseURL:    "http://docker/v1.43",
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   15 * time.Minute,
		},
	}
}

type VersionInfo struct {
	Version    string `json:"Version"`
	APIVersion string `json:"ApiVersion"`
	Os         string `json:"Os"`
	Arch       string `json:"Arch"`
}

type ContainerSummary struct {
	ID      string            `json:"Id"`
	Names   []string          `json:"Names"`
	Image   string            `json:"Image"`
	State   string            `json:"State"`
	Status  string            `json:"Status"`
	Created int64             `json:"Created"`
	Labels  map[string]string `json:"Labels"`
	Ports   []PortBinding     `json:"Ports"`
}

type PortBinding struct {
	IP          string `json:"IP,omitempty"`
	PrivatePort int    `json:"PrivatePort"`
	PublicPort  int    `json:"PublicPort,omitempty"`
	Type        string `json:"Type"`
}

type ContainerInspect struct {
	ID    string `json:"Id"`
	Name  string `json:"Name"`
	State struct {
		Status     string `json:"Status"`
		Running    bool   `json:"Running"`
		Restarting bool   `json:"Restarting"`
		ExitCode   int    `json:"ExitCode"`
		Error      string `json:"Error"`
		StartedAt  string `json:"StartedAt"`
	} `json:"State"`
	Config struct {
		Image  string            `json:"Image"`
		Env    []string          `json:"Env"`
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
}

type ContainerStats struct {
	CPUPercent    float64 `json:"cpuPercent"`
	MemoryUsageMB float64 `json:"memoryUsageMb"`
	MemoryLimitMB float64 `json:"memoryLimitMb"`
	MemoryPercent float64 `json:"memoryPercent"`
}

type CreateContainerSpec struct {
	Name          string
	Image         string
	Cmd           []string
	Env           []string
	Labels        map[string]string
	Binds         []string
	PortMappings  map[string]string // containerPort/tcp -> hostPort (e.g. "8069/tcp": "8069")
	Network       string
	RestartPolicy string // "unless-stopped", "no", "always"
	MemoryLimit   int64  // bytes, optional
}

// Ping checks Docker daemon connectivity.
func (c *Client) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/_ping", nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("docker ping returned %d", resp.StatusCode)
	}
	return nil
}

// Version fetches Docker daemon version details.
func (c *Client) Version(ctx context.Context) (*VersionInfo, error) {
	var info VersionInfo
	if err := c.doJSON(ctx, http.MethodGet, "/version", nil, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// EnsureNetwork creates a bridge network if it does not already exist.
func (c *Client) EnsureNetwork(ctx context.Context, name string) error {
	var existing map[string]any
	if err := c.doJSON(ctx, http.MethodGet, "/networks/"+url.PathEscape(name), nil, &existing); err == nil {
		return nil
	}
	payload := map[string]any{
		"Name":           name,
		"CheckDuplicate": true,
		"Driver":         "bridge",
		"Labels": map[string]string{
			"runodoo.managed": "true",
		},
	}
	return c.doJSON(ctx, http.MethodPost, "/networks/create", payload, nil)
}

// PullImage pulls a Docker image from registry.
func (c *Client) PullImage(ctx context.Context, image string) error {
	return c.PullImageWithProgress(ctx, image, nil)
}

// PullImageWithProgress pulls a Docker image and streams formatted progress lines to onLine.
func (c *Client) PullImageWithProgress(ctx context.Context, image string, onLine func(string)) error {
	u := "/images/create?fromImage=" + url.QueryEscape(image)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+u, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("docker pull %s failed (%d): %s", image, resp.StatusCode, string(body))
	}

	dec := json.NewDecoder(resp.Body)
	lastStatusByID := map[string]string{}
	for {
		var evt struct {
			Status   string `json:"status"`
			ID       string `json:"id"`
			Progress string `json:"progress"`
			Error    string `json:"error"`
		}
		if err := dec.Decode(&evt); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		if evt.Error != "" {
			return fmt.Errorf("docker pull %s error: %s", image, evt.Error)
		}
		if onLine == nil || evt.Status == "" {
			continue
		}
		// Avoid flooding logs with duplicate per-byte progress events; log status transitions per layer
		key := evt.ID
		if key != "" {
			if lastStatusByID[key] == evt.Status {
				continue
			}
			lastStatusByID[key] = evt.Status
			onLine(fmt.Sprintf("[%s] %s", evt.ID, evt.Status))
		} else {
			onLine(evt.Status)
		}
	}
	return nil
}

// ImageExists checks whether an image is already present locally.
func (c *Client) ImageExists(ctx context.Context, image string) bool {
	err := c.doJSON(ctx, http.MethodGet, "/images/"+url.PathEscape(image)+"/json", nil, nil)
	return err == nil
}

// ListContainers lists containers, optionally filtered by label (e.g. "runodoo.managed=true").
func (c *Client) ListContainers(ctx context.Context, all bool, labelFilter string) ([]ContainerSummary, error) {
	q := url.Values{}
	if all {
		q.Set("all", "true")
	}
	if labelFilter != "" {
		filters, _ := json.Marshal(map[string][]string{"label": {labelFilter}})
		q.Set("filters", string(filters))
	}
	var list []ContainerSummary
	if err := c.doJSON(ctx, http.MethodGet, "/containers/json?"+q.Encode(), nil, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// InspectContainer returns detailed state of a container by name or ID.
func (c *Client) InspectContainer(ctx context.Context, idOrName string) (*ContainerInspect, error) {
	var out ContainerInspect
	if err := c.doJSON(ctx, http.MethodGet, "/containers/"+url.PathEscape(idOrName)+"/json", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateContainer creates a container and returns its ID.
func (c *Client) CreateContainer(ctx context.Context, spec CreateContainerSpec) (string, error) {
	exposedPorts := map[string]struct{}{}
	portBindings := map[string][]map[string]string{}
	for containerPort, hostPort := range spec.PortMappings {
		exposedPorts[containerPort] = struct{}{}
		portBindings[containerPort] = []map[string]string{
			{"HostIp": "0.0.0.0", "HostPort": hostPort},
		}
	}

	restartName := spec.RestartPolicy
	if restartName == "" {
		restartName = "unless-stopped"
	}

	hostConfig := map[string]any{
		"Binds":        spec.Binds,
		"PortBindings": portBindings,
		"RestartPolicy": map[string]any{
			"Name": restartName,
		},
	}
	if spec.MemoryLimit > 0 {
		hostConfig["Memory"] = spec.MemoryLimit
	}

	body := map[string]any{
		"Image":        spec.Image,
		"Env":          spec.Env,
		"Labels":       spec.Labels,
		"ExposedPorts": exposedPorts,
		"HostConfig":   hostConfig,
	}
	if len(spec.Cmd) > 0 {
		body["Cmd"] = spec.Cmd
	}
	if spec.Network != "" {
		body["NetworkingConfig"] = map[string]any{
			"EndpointsConfig": map[string]any{
				spec.Network: map[string]any{},
			},
		}
	}

	path := "/containers/create"
	if spec.Name != "" {
		path += "?name=" + url.QueryEscape(spec.Name)
	}

	var res struct {
		ID string `json:"Id"`
	}
	if err := c.doJSON(ctx, http.MethodPost, path, body, &res); err != nil {
		return "", err
	}
	return res.ID, nil
}

// StartContainer starts a stopped container.
func (c *Client) StartContainer(ctx context.Context, idOrName string) error {
	return c.doEmpty(ctx, http.MethodPost, "/containers/"+url.PathEscape(idOrName)+"/start")
}

// StopContainer stops a running container.
func (c *Client) StopContainer(ctx context.Context, idOrName string, timeoutSec int) error {
	if timeoutSec <= 0 {
		timeoutSec = 15
	}
	return c.doEmpty(ctx, http.MethodPost, fmt.Sprintf("/containers/%s/stop?t=%d", url.PathEscape(idOrName), timeoutSec))
}

// RestartContainer restarts a container.
func (c *Client) RestartContainer(ctx context.Context, idOrName string, timeoutSec int) error {
	if timeoutSec <= 0 {
		timeoutSec = 15
	}
	return c.doEmpty(ctx, http.MethodPost, fmt.Sprintf("/containers/%s/restart?t=%d", url.PathEscape(idOrName), timeoutSec))
}

// RemoveContainer deletes a container (optionally force-removing running containers).
func (c *Client) RemoveContainer(ctx context.Context, idOrName string, force bool) error {
	q := ""
	if force {
		q = "?force=true"
	}
	return c.doEmpty(ctx, http.MethodDelete, "/containers/"+url.PathEscape(idOrName)+q)
}

// ContainerLogs returns recent stdout+stderr logs for a container.
func (c *Client) ContainerLogs(ctx context.Context, idOrName string, tail int) (string, error) {
	if tail <= 0 {
		tail = 200
	}
	path := fmt.Sprintf("/containers/%s/logs?stdout=true&stderr=true&tail=%d&timestamps=true", url.PathEscape(idOrName), tail)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("docker logs failed (%d): %s", resp.StatusCode, string(raw))
	}
	return demuxDockerStream(raw), nil
}

// ContainerStats fetches a single snapshot of container CPU and RAM usage.
func (c *Client) ContainerStats(ctx context.Context, idOrName string) (*ContainerStats, error) {
	var raw struct {
		CPUStats struct {
			CPUUsage struct {
				TotalUsage uint64 `json:"total_usage"`
			} `json:"cpu_usage"`
			SystemCPUUsage uint64 `json:"system_cpu_usage"`
			OnlineCPUs     uint64 `json:"online_cpus"`
		} `json:"cpu_stats"`
		PreCPUStats struct {
			CPUUsage struct {
				TotalUsage uint64 `json:"total_usage"`
			} `json:"cpu_usage"`
			SystemCPUUsage uint64 `json:"system_cpu_usage"`
		} `json:"precpu_stats"`
		MemoryStats struct {
			Usage uint64 `json:"usage"`
			Limit uint64 `json:"limit"`
			Stats struct {
				Cache uint64 `json:"cache"`
			} `json:"stats"`
		} `json:"memory_stats"`
	}

	if err := c.doJSON(ctx, http.MethodGet, "/containers/"+url.PathEscape(idOrName)+"/stats?stream=false", nil, &raw); err != nil {
		return nil, err
	}

	cpuDelta := float64(raw.CPUStats.CPUUsage.TotalUsage - raw.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(raw.CPUStats.SystemCPUUsage - raw.PreCPUStats.SystemCPUUsage)
	cpus := float64(raw.CPUStats.OnlineCPUs)
	if cpus == 0 {
		cpus = 1
	}
	cpuPct := 0.0
	if sysDelta > 0 && cpuDelta > 0 {
		cpuPct = (cpuDelta / sysDelta) * cpus * 100.0
	}

	memUsage := raw.MemoryStats.Usage
	if memUsage > raw.MemoryStats.Stats.Cache {
		memUsage -= raw.MemoryStats.Stats.Cache
	}
	memLimit := raw.MemoryStats.Limit
	memPct := 0.0
	if memLimit > 0 {
		memPct = float64(memUsage) / float64(memLimit) * 100.0
	}

	return &ContainerStats{
		CPUPercent:    float64(int(cpuPct*100)) / 100,
		MemoryUsageMB: float64(int(float64(memUsage)/(1024*1024)*10)) / 10,
		MemoryLimitMB: float64(int(float64(memLimit)/(1024*1024)*10)) / 10,
		MemoryPercent: float64(int(memPct*100)) / 100,
	}, nil
}

// Exec runs a command inside a running container and returns combined output and exit code.
func (c *Client) Exec(ctx context.Context, idOrName string, cmd []string, env []string) (string, int, error) {
	createPayload := map[string]any{
		"AttachStdout": true,
		"AttachStderr": true,
		"Cmd":          cmd,
		"Env":          env,
	}
	var createRes struct {
		ID string `json:"Id"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/containers/"+url.PathEscape(idOrName)+"/exec", createPayload, &createRes); err != nil {
		return "", -1, err
	}

	startBody, _ := json.Marshal(map[string]any{"Detach": false, "Tty": false})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/exec/"+createRes.ID+"/start", bytes.NewReader(startBody))
	if err != nil {
		return "", -1, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", -1, err
	}
	defer resp.Body.Close()
	rawOut, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", -1, err
	}

	var inspectRes struct {
		ExitCode int `json:"ExitCode"`
	}
	_ = c.doJSON(ctx, http.MethodGet, "/exec/"+createRes.ID+"/json", nil, &inspectRes)
	return demuxDockerStream(rawOut), inspectRes.ExitCode, nil
}

// ExecStream runs a command inside a container and writes raw stdout to dst (useful for pg_dump).
func (c *Client) ExecStream(ctx context.Context, idOrName string, cmd []string, env []string, dst io.Writer) error {
	var stderrBuf bytes.Buffer
	code, err := c.execStream(ctx, idOrName, cmd, env, dst, &stderrBuf)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("exec exited with %d: %s", code, strings.TrimSpace(stderrBuf.String()))
	}
	return nil
}

// ExecStreamCombined runs a command inside a container and streams both stdout and stderr to dst.
func (c *Client) ExecStreamCombined(ctx context.Context, idOrName string, cmd []string, env []string, dst io.Writer) (int, error) {
	return c.execStream(ctx, idOrName, cmd, env, dst, dst)
}

func (c *Client) execStream(ctx context.Context, idOrName string, cmd []string, env []string, stdout, stderr io.Writer) (int, error) {
	createPayload := map[string]any{
		"AttachStdout": true,
		"AttachStderr": true,
		"Cmd":          cmd,
		"Env":          env,
	}
	var createRes struct {
		ID string `json:"Id"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/containers/"+url.PathEscape(idOrName)+"/exec", createPayload, &createRes); err != nil {
		return -1, err
	}

	startBody, _ := json.Marshal(map[string]any{"Detach": false, "Tty": false})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/exec/"+createRes.ID+"/start", bytes.NewReader(startBody))
	if err != nil {
		return -1, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return -1, err
	}
	defer resp.Body.Close()

	if err := demuxToWriters(resp.Body, stdout, stderr); err != nil {
		return -1, err
	}

	var inspectRes struct {
		ExitCode int `json:"ExitCode"`
	}
	_ = c.doJSON(ctx, http.MethodGet, "/exec/"+createRes.ID+"/json", nil, &inspectRes)
	return inspectRes.ExitCode, nil
}

// ExecInteractive starts an interactive TTY exec session inside a container and returns the raw bidirectional connection.
func (c *Client) ExecInteractive(ctx context.Context, idOrName string, cmd []string, env []string, rows, cols int) (string, net.Conn, *bufio.Reader, error) {
	createPayload := map[string]any{
		"AttachStdin":  true,
		"AttachStdout": true,
		"AttachStderr": true,
		"Tty":          true,
		"Cmd":          cmd,
		"Env":          env,
	}
	var createRes struct {
		ID string `json:"Id"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/containers/"+url.PathEscape(idOrName)+"/exec", createPayload, &createRes); err != nil {
		return "", nil, nil, err
	}

	conn, err := net.DialTimeout("unix", c.socketPath, 10*time.Second)
	if err != nil {
		return "", nil, nil, err
	}

	startBody, _ := json.Marshal(map[string]any{"Detach": false, "Tty": true})
	rawReq := fmt.Sprintf(
		"POST /v1.43/exec/%s/start HTTP/1.1\r\nHost: docker\r\nContent-Type: application/json\r\nConnection: Upgrade\r\nUpgrade: tcp\r\nContent-Length: %d\r\n\r\n%s",
		createRes.ID, len(startBody), startBody,
	)
	if _, err := io.WriteString(conn, rawReq); err != nil {
		_ = conn.Close()
		return "", nil, nil, err
	}

	br := bufio.NewReader(conn)
	req, _ := http.NewRequest(http.MethodPost, c.baseURL+"/exec/"+createRes.ID+"/start", nil)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		_ = conn.Close()
		return "", nil, nil, err
	}
	if resp.StatusCode != http.StatusSwitchingProtocols && resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		_ = conn.Close()
		return "", nil, nil, fmt.Errorf("docker exec upgrade failed with status %d", resp.StatusCode)
	}

	if rows > 0 && cols > 0 {
		_ = c.ResizeExec(ctx, createRes.ID, rows, cols)
	}
	return createRes.ID, conn, br, nil
}

// ResizeExec resizes the TTY of a running docker exec session.
func (c *Client) ResizeExec(ctx context.Context, execID string, rows, cols int) error {
	path := fmt.Sprintf("/exec/%s/resize?h=%d&w=%d", url.PathEscape(execID), rows, cols)
	return c.doJSON(ctx, http.MethodPost, path, nil, nil)
}

// RunOneShot creates, starts, waits for, captures logs, and removes a short-lived container (e.g. Certbot).
func (c *Client) RunOneShot(ctx context.Context, spec CreateContainerSpec) (string, int, error) {
	spec.RestartPolicy = "no"
	if !c.ImageExists(ctx, spec.Image) {
		if err := c.PullImage(ctx, spec.Image); err != nil {
			return "", -1, err
		}
	}
	id, err := c.CreateContainer(ctx, spec)
	if err != nil {
		return "", -1, err
	}
	defer func() {
		_ = c.RemoveContainer(context.Background(), id, true)
	}()

	if err := c.StartContainer(ctx, id); err != nil {
		return "", -1, err
	}

	var waitRes struct {
		StatusCode int `json:"StatusCode"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/containers/"+id+"/wait", nil, &waitRes); err != nil {
		return "", -1, err
	}
	logs, _ := c.ContainerLogs(ctx, id, 200)
	return logs, waitRes.StatusCode, nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, payload any, out any) error {
	var bodyReader io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("docker API %s %s returned %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) doEmpty(ctx context.Context, method, path string) error {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode != http.StatusNotModified {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("docker API %s %s returned %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

// demuxDockerStream parses Docker's 8-byte multiplexed log stream headers.
func demuxDockerStream(raw []byte) string {
	var out bytes.Buffer
	if err := demuxToWriters(bytes.NewReader(raw), &out, &out); err != nil {
		// Fallback for TTY / non-multiplexed output
		return string(raw)
	}
	return out.String()
}

func demuxToWriters(r io.Reader, stdout, stderr io.Writer) error {
	hdr := make([]byte, 8)
	for {
		_, err := io.ReadFull(r, hdr)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		streamType := hdr[0]
		size := binary.BigEndian.Uint32(hdr[4:8])
		if streamType > 2 || size > 64*1024*1024 {
			return fmt.Errorf("not a multiplexed docker stream")
		}
		target := stdout
		if streamType == 2 {
			target = stderr
		}
		if _, err := io.CopyN(target, r, int64(size)); err != nil {
			return err
		}
	}
}
