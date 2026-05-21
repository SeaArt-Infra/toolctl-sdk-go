package toolctl

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	MACHINE_STATUS_PRE   = -1
	MACHINE_STATUS_IDLE  = 0
	MACHINE_STATUS_BUSY  = 1
	MACHINE_STATUS_FATAL = 2
	MACHINE_STATUS_ERROR = 3

	MACHINE_TASK_STATUS_SUCCESS  = 0
	MACHINE_TASK_STATUS_FAIL     = 1
	MACHINE_TASK_STATUS_REPEATED = 2
	MACHINE_TASK_STATUS_EXCEPT   = 3
)

type StatusProvider func() int

type PublishFn func(map[string]any) error

type MetricsPublisher interface {
	Publish(ctx context.Context, data []byte, attributes map[string]string, orderingKey string) (any, error)
}

type MonitoringConfig struct {
	ServiceName        string
	Interval           time.Duration
	InstanceID         string
	Labels             map[string]string
	PublishImmediately bool
	StatusProvider     StatusProvider
	Enabled            *bool
}

type ResourceMonitorOptions struct {
	Config    MonitoringConfig
	Publisher MetricsPublisher
	Publish   PublishFn
	Logger    *log.Logger
}

type StartResourceMonitorOptions struct {
	ServiceName        string
	Publisher          MetricsPublisher
	Publish            PublishFn
	Enabled            *bool
	Interval           time.Duration
	InstanceID         string
	Labels             map[string]string
	PublishImmediately bool
	StatusProvider     StatusProvider
	Logger             *log.Logger
}

type ResourceMonitor struct {
	config    MonitoringConfig
	collector *SystemMetricsCollector
	publisher MetricsPublisher
	publish   PublishFn
	logger    *log.Logger

	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
	doneCh  chan struct{}
}

func NewResourceMonitor(opts ResourceMonitorOptions) (*ResourceMonitor, error) {
	config := normalizeMonitoringConfig(opts.Config)
	if config.ServiceName == "" {
		return nil, fmt.Errorf("service_name is required")
	}
	if config.Interval <= 0 {
		return nil, fmt.Errorf("interval must be greater than zero")
	}
	if configEnabled(config) && opts.Publisher == nil && opts.Publish == nil {
		return nil, fmt.Errorf("either publisher or publish must be provided")
	}
	logger := opts.Logger
	if logger == nil {
		logger = log.Default()
	}
	return &ResourceMonitor{
		config: config,
		collector: NewSystemMetricsCollector(SystemMetricsCollectorOptions{
			ServiceName:    config.ServiceName,
			InstanceID:     config.InstanceID,
			Labels:         config.Labels,
			StatusProvider: config.StatusProvider,
		}),
		publisher: opts.Publisher,
		publish:   opts.Publish,
		logger:    logger,
	}, nil
}

func StartResourceMonitor(ctx context.Context, opts StartResourceMonitorOptions) (*ResourceMonitor, error) {
	monitor, err := NewResourceMonitor(ResourceMonitorOptions{
		Config: MonitoringConfig{
			ServiceName:        opts.ServiceName,
			Enabled:            opts.Enabled,
			Interval:           opts.Interval,
			InstanceID:         opts.InstanceID,
			Labels:             opts.Labels,
			PublishImmediately: opts.PublishImmediately,
			StatusProvider:     opts.StatusProvider,
		},
		Publisher: opts.Publisher,
		Publish:   opts.Publish,
		Logger:    opts.Logger,
	})
	if err != nil {
		return nil, err
	}
	monitor.Start(ctx)
	return monitor, nil
}

func (m *ResourceMonitor) Running() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

func (m *ResourceMonitor) Enabled() bool {
	return configEnabled(m.config)
}

func (m *ResourceMonitor) Start(ctx context.Context) {
	if !m.Enabled() {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	m.stopCh = make(chan struct{})
	m.doneCh = make(chan struct{})
	m.running = true
	stopCh := m.stopCh
	doneCh := m.doneCh
	m.mu.Unlock()

	go m.run(ctx, stopCh, doneCh)
}

func (m *ResourceMonitor) Stop(timeout time.Duration) {
	m.mu.Lock()
	if !m.running || m.stopCh == nil {
		m.mu.Unlock()
		return
	}
	stopCh := m.stopCh
	doneCh := m.doneCh
	m.stopCh = nil
	m.mu.Unlock()

	close(stopCh)
	if timeout <= 0 {
		<-doneCh
		return
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-doneCh:
	case <-timer.C:
	}
}

func (m *ResourceMonitor) CollectOnce() map[string]any {
	return m.collector.Collect()
}

func (m *ResourceMonitor) PublishOnce(ctx context.Context) (map[string]any, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	payload := m.CollectOnce()
	if !m.Enabled() {
		return payload, nil
	}
	return payload, m.publishPayload(ctx, payload)
}

func (m *ResourceMonitor) run(ctx context.Context, stopCh <-chan struct{}, doneCh chan<- struct{}) {
	defer func() {
		m.mu.Lock()
		m.running = false
		m.mu.Unlock()
		close(doneCh)
	}()

	if m.config.PublishImmediately {
		m.safePublishOnce(ctx)
	}

	ticker := time.NewTicker(m.config.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-stopCh:
			return
		case <-ticker.C:
			m.safePublishOnce(ctx)
		}
	}
}

func (m *ResourceMonitor) safePublishOnce(ctx context.Context) {
	if _, err := m.PublishOnce(ctx); err != nil && m.logger != nil {
		m.logger.Printf("resource monitor publish failed: %v", err)
	}
}

func (m *ResourceMonitor) publishPayload(ctx context.Context, payload map[string]any) error {
	if m.publish != nil {
		return m.publish(payload)
	}
	if m.publisher == nil {
		return nil
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = m.publisher.Publish(ctx, data, map[string]string{
		"event_type":  "machine_status",
		"service":     m.config.ServiceName,
		"instance_id": m.config.InstanceID,
	}, "")
	return err
}

type SystemMetricsCollectorOptions struct {
	ServiceName    string
	InstanceID     string
	Labels         map[string]string
	StatusProvider StatusProvider
}

type SystemMetricsCollector struct {
	serviceName    string
	instanceID     string
	labels         map[string]string
	statusProvider StatusProvider
	startedAt      time.Time
	mu             sync.Mutex
	lastCPUIdle    uint64
	lastCPUTotal   uint64
	hasLastCPU     bool
}

func NewSystemMetricsCollector(opts SystemMetricsCollectorOptions) *SystemMetricsCollector {
	instanceID := opts.InstanceID
	if instanceID == "" {
		instanceID = newInstanceID()
	}
	labels := cloneLabels(opts.Labels)
	idle, total, hasCPU := readLinuxCPUTotals()
	return &SystemMetricsCollector{
		serviceName:    opts.ServiceName,
		instanceID:     instanceID,
		labels:         labels,
		statusProvider: opts.StatusProvider,
		startedAt:      time.Now(),
		lastCPUIdle:    idle,
		lastCPUTotal:   total,
		hasLastCPU:     hasCPU,
	}
}

func (c *SystemMetricsCollector) Collect() map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	ip := c.label([]string{"ip", "machine_ip"}, hostIP())
	machineID := c.label([]string{"id", "machine_id"}, c.instanceID)
	category := c.label([]string{"category"}, c.serviceName)
	port := c.label([]string{"port"}, "8080")
	api := c.label([]string{"api", "start_api", "task_api"}, "tools")
	modeAPI := c.label([]string{"mode_api", "change_partition_api"}, "change_partition")
	actionAPI := c.label([]string{"action_api"}, "activity")
	taskURL := c.label([]string{"task_url", "start_task_url"}, routeURL(ip, port, api))
	taskExpressURL := c.label([]string{"task_express_url", "change_partition_url"}, routeURL(ip, port, modeAPI))
	hostname, _ := os.Hostname()

	cpuPercent := c.systemCPUPercent()
	var memoryPercent any
	if metrics, ok := linuxMemoryMetrics(); ok {
		memoryPercent = metrics["used_percent"]
	}

	payload := map[string]any{
		"id": machineID,
		"ip": ip,
		"routes": map[string]any{
			"start_task":           taskURL,
			"change_partition_url": taskExpressURL,
			"action_url":           c.label([]string{"action_url"}, routeURL(ip, port, actionAPI)),
		},
		"send_time":        now.UnixMilli(),
		"region":           c.label([]string{"region"}, ""),
		"machine_id":       machineID,
		"status":           c.machineStatus(),
		"category":         category,
		"task_url":         taskURL,
		"instance_group":   c.label([]string{"instance_group"}, c.serviceName),
		"express":          c.label([]string{"express", "mix_cluster", "tool"}, c.serviceName),
		"task_express_url": taskExpressURL,
		"cloud":            c.label([]string{"cloud"}, ""),
		"host_id":          c.label([]string{"host_id"}, ""),
		"partition":        c.label([]string{"partition"}, ""),
		"cpu_percent":      cpuPercent,
		"memory_percent":   memoryPercent,
		"instance_id":      c.instanceID,
		"hostname":         hostname,
	}
	return payload
}

func (c *SystemMetricsCollector) label(keys []string, fallback string) string {
	for _, key := range keys {
		if value := c.labels[key]; value != "" {
			return value
		}
	}
	return fallback
}

func (c *SystemMetricsCollector) machineStatus() int {
	if c.statusProvider != nil {
		return c.statusProvider()
	}
	raw := c.labels["status"]
	if raw == "" {
		return MACHINE_STATUS_IDLE
	}
	status, err := strconv.Atoi(raw)
	if err != nil {
		return MACHINE_STATUS_IDLE
	}
	return status
}

func (c *SystemMetricsCollector) systemCPUPercent() any {
	idle, total, ok := readLinuxCPUTotals()
	if !ok || !c.hasLastCPU {
		c.lastCPUIdle = idle
		c.lastCPUTotal = total
		c.hasLastCPU = ok
		return nil
	}
	totalDelta := total - c.lastCPUTotal
	idleDelta := idle - c.lastCPUIdle
	c.lastCPUIdle = idle
	c.lastCPUTotal = total
	if totalDelta == 0 || idleDelta > totalDelta {
		return nil
	}
	return round2((1 - float64(idleDelta)/float64(totalDelta)) * 100)
}

func normalizeMonitoringConfig(config MonitoringConfig) MonitoringConfig {
	if config.Interval == 0 {
		config.Interval = 5 * time.Second
	}
	if config.InstanceID == "" {
		config.InstanceID = newInstanceID()
	}
	config.Labels = cloneLabels(config.Labels)
	return config
}

func configEnabled(config MonitoringConfig) bool {
	return config.Enabled == nil || *config.Enabled
}

func newInstanceID() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), os.Getpid())
}

func cloneLabels(input map[string]string) map[string]string {
	if input == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func hostIP() string {
	addrs, err := net.InterfaceAddrs()
	if err == nil {
		for _, addr := range addrs {
			var ip net.IP
			switch value := addr.(type) {
			case *net.IPNet:
				ip = value.IP
			case *net.IPAddr:
				ip = value.IP
			}
			if ip != nil && ip.To4() != nil && !ip.IsLoopback() {
				return ip.String()
			}
		}
		for _, addr := range addrs {
			var ip net.IP
			switch value := addr.(type) {
			case *net.IPNet:
				ip = value.IP
			case *net.IPAddr:
				ip = value.IP
			}
			if ip != nil && ip.To4() != nil {
				return ip.String()
			}
		}
	}
	return "127.0.0.1"
}

func routeURL(ip, port, path string) string {
	cleanPath := strings.Trim(path, "/")
	portSegment := ""
	if port != "" {
		portSegment = ":" + port
	}
	if cleanPath == "" {
		return "http://" + ip + portSegment
	}
	return "http://" + ip + portSegment + "/" + cleanPath
}

func memoryMetrics() map[string]any {
	if metrics, ok := linuxMemoryMetrics(); ok {
		return metrics
	}
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	return map[string]any{
		"total_bytes":     nil,
		"available_bytes": nil,
		"used_bytes":      int64(mem.Sys),
		"used_percent":    nil,
	}
}

func processMetrics(now, startedAt time.Time) map[string]any {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	return map[string]any{
		"pid":            os.Getpid(),
		"cpu_percent":    nil,
		"rss_bytes":      int64(mem.Sys),
		"uptime_seconds": now.Sub(startedAt).Seconds(),
		"thread_count":   runtime.NumGoroutine(),
	}
}

func linuxMemoryMetrics() (map[string]any, bool) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return nil, false
	}
	values := map[string]int64{}
	for _, line := range strings.Split(string(data), "\n") {
		key, raw, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		parts := strings.Fields(raw)
		if len(parts) == 0 {
			continue
		}
		value, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			continue
		}
		values[key] = value * 1024
	}
	total, ok := values["MemTotal"]
	if !ok {
		return nil, false
	}
	available := values["MemAvailable"]
	used := total - available
	var usedPercent any
	if total > 0 {
		usedPercent = round2(float64(used) / float64(total) * 100)
	}
	return map[string]any{
		"total_bytes":     total,
		"available_bytes": available,
		"used_bytes":      used,
		"used_percent":    usedPercent,
	}, true
}

func readLinuxCPUTotals() (uint64, uint64, bool) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0, false
	}
	lines := strings.SplitN(string(data), "\n", 2)
	fields := strings.Fields(lines[0])
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0, 0, false
	}
	var total uint64
	var values []uint64
	for _, field := range fields[1:] {
		value, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return 0, 0, false
		}
		values = append(values, value)
		total += value
	}
	idle := values[3]
	if len(values) > 4 {
		idle += values[4]
	}
	return idle, total, true
}

func loadAverage() any {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return nil
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return nil
	}
	one, err1 := strconv.ParseFloat(fields[0], 64)
	five, err2 := strconv.ParseFloat(fields[1], 64)
	fifteen, err3 := strconv.ParseFloat(fields[2], 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return nil
	}
	return map[string]float64{"1m": one, "5m": five, "15m": fifteen}
}

func round2(value float64) float64 {
	return float64(int(value*100+0.5)) / 100
}

// PubSubMetricsPublisher publishes metrics to Google Cloud Pub/Sub via the REST API.
// Authenticates with a service account credentials JSON (raw JSON or base64-encoded).
// No external dependencies — uses only the Go standard library.
type PubSubMetricsPublisher struct {
	topicPath             string
	credentialsJSON       string
	publishTimeoutSeconds float64
	httpClient            *http.Client
	mu                    sync.Mutex
	accessToken           string
	tokenExpiry           time.Time
}

// PubSubMetricsPublisherOptions configures a PubSubMetricsPublisher.
type PubSubMetricsPublisherOptions struct {
	// Topic is a full Pub/Sub path ("projects/PROJECT/topics/TOPIC")
	// or a short topic name when ProjectID is also set.
	Topic string
	// CredentialsJSON is the service account JSON as a raw or base64-encoded string.
	// If empty, the GCE metadata server is used for token acquisition.
	CredentialsJSON string
	// ProjectID is only needed when Topic is not a full path.
	ProjectID string
	// PublishTimeoutSeconds is the per-request timeout. Defaults to 30.
	PublishTimeoutSeconds float64
}

type pubsubServiceAccount struct {
	ProjectID   string `json:"project_id"`
	PrivateKey  string `json:"private_key"`
	ClientEmail string `json:"client_email"`
	TokenURI    string `json:"token_uri"`
}

// NewPubSubMetricsPublisher creates a PubSubMetricsPublisher.
func NewPubSubMetricsPublisher(opts PubSubMetricsPublisherOptions) (*PubSubMetricsPublisher, error) {
	if opts.Topic == "" {
		return nil, fmt.Errorf("topic is required for PubSubMetricsPublisher")
	}
	timeout := opts.PublishTimeoutSeconds
	if timeout <= 0 {
		timeout = 30
	}
	topicPath := opts.Topic
	if !strings.HasPrefix(topicPath, "projects/") {
		if opts.ProjectID == "" {
			return nil, fmt.Errorf("project_id is required when topic is not a full path")
		}
		topicPath = fmt.Sprintf("projects/%s/topics/%s", opts.ProjectID, opts.Topic)
	}
	credJSON := opts.CredentialsJSON
	if credJSON != "" {
		payload := strings.TrimSpace(credJSON)
		if !strings.HasPrefix(payload, "{") {
			decoded, err := base64.StdEncoding.DecodeString(payload)
			if err != nil {
				return nil, fmt.Errorf("credentials_json is not valid base64: %w", err)
			}
			credJSON = string(decoded)
		}
	}
	return &PubSubMetricsPublisher{
		topicPath:             topicPath,
		credentialsJSON:       credJSON,
		publishTimeoutSeconds: timeout,
		httpClient:            &http.Client{Timeout: time.Duration(timeout * float64(time.Second))},
	}, nil
}

// Publish implements MetricsPublisher.
func (p *PubSubMetricsPublisher) Publish(ctx context.Context, data []byte, attributes map[string]string, orderingKey string) (any, error) {
	token, err := p.getAccessToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("pubsub: get access token: %w", err)
	}
	msg := map[string]any{
		"data":       base64.StdEncoding.EncodeToString(data),
		"attributes": attributes,
	}
	if orderingKey != "" {
		msg["orderingKey"] = orderingKey
	}
	body, err := json.Marshal(map[string]any{"messages": []any{msg}})
	if err != nil {
		return nil, err
	}
	url := fmt.Sprintf("https://pubsub.googleapis.com/v1/%s:publish", p.topicPath)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("pubsub publish failed [%d]: %s", resp.StatusCode, string(respBody))
	}
	var result map[string]any
	_ = json.Unmarshal(respBody, &result)
	return result, nil
}

func (p *PubSubMetricsPublisher) getAccessToken(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.accessToken != "" && time.Now().Before(p.tokenExpiry) {
		return p.accessToken, nil
	}
	if p.credentialsJSON != "" {
		token, expiry, err := p.fetchServiceAccountToken(ctx)
		if err != nil {
			return "", err
		}
		p.accessToken = token
		p.tokenExpiry = expiry
		return token, nil
	}
	token, expiry, err := fetchMetadataToken(ctx, p.httpClient)
	if err != nil {
		return "", err
	}
	p.accessToken = token
	p.tokenExpiry = expiry
	return token, nil
}

func (p *PubSubMetricsPublisher) fetchServiceAccountToken(ctx context.Context) (string, time.Time, error) {
	var sa pubsubServiceAccount
	if err := json.Unmarshal([]byte(p.credentialsJSON), &sa); err != nil {
		return "", time.Time{}, fmt.Errorf("parse service account JSON: %w", err)
	}
	tokenURI := sa.TokenURI
	if tokenURI == "" {
		tokenURI = "https://oauth2.googleapis.com/token"
	}
	now := time.Now()
	exp := now.Add(3600 * time.Second)
	headerJSON, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT"})
	claimsJSON, _ := json.Marshal(map[string]any{
		"iss":   sa.ClientEmail,
		"sub":   sa.ClientEmail,
		"scope": "https://www.googleapis.com/auth/pubsub",
		"aud":   tokenURI,
		"iat":   now.Unix(),
		"exp":   exp.Unix(),
	})
	sigInput := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(claimsJSON)
	sig, err := rsaSign([]byte(sigInput), sa.PrivateKey)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign JWT: %w", err)
	}
	jwt := sigInput + "." + base64.RawURLEncoding.EncodeToString(sig)
	payload := "grant_type=urn%3Aietf%3Aparams%3Aoauth%3Agrant-type%3Ajwt-bearer&assertion=" + jwt
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURI, strings.NewReader(payload))
	if err != nil {
		return "", time.Time{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", time.Time{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", time.Time{}, fmt.Errorf("token fetch failed [%d]: %s", resp.StatusCode, string(body))
	}
	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", time.Time{}, fmt.Errorf("parse token response: %w", err)
	}
	expiry := time.Now().Add(time.Duration(tokenResp.ExpiresIn-60) * time.Second)
	return tokenResp.AccessToken, expiry, nil
}

func fetchMetadataToken(ctx context.Context, client *http.Client) (string, time.Time, error) {
	url := "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", time.Time{}, err
	}
	req.Header.Set("Metadata-Flavor", "Google")
	resp, err := client.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("metadata server unavailable: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", time.Time{}, fmt.Errorf("metadata token fetch failed [%d]", resp.StatusCode)
	}
	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", time.Time{}, fmt.Errorf("parse metadata token response: %w", err)
	}
	expiry := time.Now().Add(time.Duration(tokenResp.ExpiresIn-60) * time.Second)
	return tokenResp.AccessToken, expiry, nil
}

func rsaSign(data []byte, privateKeyPEM string) ([]byte, error) {
	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block from private key")
	}
	var key *rsa.PrivateKey
	switch block.Type {
	case "RSA PRIVATE KEY":
		k, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		key = k
	case "PRIVATE KEY":
		k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		rsaKey, ok := k.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("PKCS8 key is not an RSA key")
		}
		key = rsaKey
	default:
		return nil, fmt.Errorf("unsupported PEM key type: %s", block.Type)
	}
	hash := sha256.Sum256(data)
	return rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
}
