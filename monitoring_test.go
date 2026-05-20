package toolctl

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMonitoringStatusConstants(t *testing.T) {
	if MACHINE_STATUS_PRE != -1 ||
		MACHINE_STATUS_IDLE != 0 ||
		MACHINE_STATUS_BUSY != 1 ||
		MACHINE_STATUS_FATAL != 2 ||
		MACHINE_STATUS_ERROR != 3 {
		t.Fatalf("unexpected machine status constants")
	}
	if MACHINE_TASK_STATUS_SUCCESS != 0 ||
		MACHINE_TASK_STATUS_FAIL != 1 ||
		MACHINE_TASK_STATUS_REPEATED != 2 ||
		MACHINE_TASK_STATUS_EXCEPT != 3 {
		t.Fatalf("unexpected machine task status constants")
	}
}

func TestSystemMetricsCollectorPayload(t *testing.T) {
	collector := NewSystemMetricsCollector(SystemMetricsCollectorOptions{
		ServiceName: "test-tool",
		InstanceID:  "instance-1",
		Labels:      map[string]string{"env": "test"},
	})

	payload := collector.Collect()

	if payload["id"] != "instance-1" {
		t.Fatalf("unexpected id: %#v", payload["id"])
	}
	if payload["machine_id"] != "instance-1" {
		t.Fatalf("unexpected machine_id: %#v", payload["machine_id"])
	}
	if payload["status"] != MACHINE_STATUS_IDLE {
		t.Fatalf("unexpected status: %#v", payload["status"])
	}
	if payload["category"] != "test-tool" {
		t.Fatalf("unexpected category: %#v", payload["category"])
	}
	if payload["instance_id"] != "instance-1" {
		t.Fatalf("unexpected instance_id: %#v", payload["instance_id"])
	}
	if payload["ip"] == "" || payload["hostname"] == "" {
		t.Fatalf("missing host fields: %#v", payload)
	}
	for _, key := range []string{"routes", "send_time", "cpu", "memory", "process"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("missing %s in payload", key)
		}
	}
	labels := payload["labels"].(map[string]string)
	if labels["env"] != "test" {
		t.Fatalf("unexpected labels: %#v", labels)
	}
}

func TestSystemMetricsCollectorStatusProvider(t *testing.T) {
	collector := NewSystemMetricsCollector(SystemMetricsCollectorOptions{
		ServiceName:    "test-tool",
		InstanceID:     "instance-1",
		StatusProvider: func() int { return MACHINE_STATUS_BUSY },
	})

	payload := collector.Collect()

	if payload["status"] != MACHINE_STATUS_BUSY {
		t.Fatalf("unexpected status: %#v", payload["status"])
	}
}

func TestResourceMonitorPublishesJSONPayload(t *testing.T) {
	publisher := &fakeMetricsPublisher{}
	monitor, err := NewResourceMonitor(ResourceMonitorOptions{
		Config: MonitoringConfig{
			ServiceName:        "test-tool",
			Interval:           10 * time.Millisecond,
			InstanceID:         "instance-1",
			PublishImmediately: true,
		},
		Publisher: publisher,
	})
	if err != nil {
		t.Fatalf("new monitor: %v", err)
	}

	monitor.Start(context.Background())
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && publisher.Len() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	monitor.Stop(time.Second)

	msg := publisher.First()
	if len(msg.data) == 0 {
		t.Fatalf("expected one published message")
	}
	var payload map[string]any
	if err := json.Unmarshal(msg.data, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload["category"] != "test-tool" {
		t.Fatalf("unexpected category: %#v", payload["category"])
	}
	if msg.attributes["event_type"] != "machine_status" {
		t.Fatalf("unexpected event_type: %#v", msg.attributes)
	}
	if msg.attributes["service"] != "test-tool" || msg.attributes["instance_id"] != "instance-1" {
		t.Fatalf("unexpected attributes: %#v", msg.attributes)
	}
}

func TestResourceMonitorCanBeDisabledWithoutPublisher(t *testing.T) {
	enabled := false
	monitor, err := NewResourceMonitor(ResourceMonitorOptions{
		Config: MonitoringConfig{
			ServiceName:        "disabled-tool",
			Enabled:            &enabled,
			PublishImmediately: true,
		},
	})
	if err != nil {
		t.Fatalf("new disabled monitor: %v", err)
	}

	monitor.Start(context.Background())
	payload, err := monitor.PublishOnce(context.Background())
	monitor.Stop(time.Second)

	if err != nil {
		t.Fatalf("publish disabled monitor: %v", err)
	}
	if monitor.Running() {
		t.Fatalf("disabled monitor should not be running")
	}
	if payload["category"] != "disabled-tool" {
		t.Fatalf("unexpected category: %#v", payload["category"])
	}
}

func TestToolAppResourceMonitoringStartsAndPublishes(t *testing.T) {
	var mu sync.Mutex
	var payloads []map[string]any
	app := Start(AppConfig{Title: "lifecycle-tool"})
	monitor, err := app.EnableResourceMonitoring(EnableResourceMonitoringOptions{
		Publish: func(payload map[string]any) error {
			mu.Lock()
			defer mu.Unlock()
			payloads = append(payloads, payload)
			return nil
		},
		Interval:           10 * time.Millisecond,
		PublishImmediately: true,
	})
	if err != nil {
		t.Fatalf("enable resource monitoring: %v", err)
	}
	defer monitor.Stop(time.Second)

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		count := len(payloads)
		mu.Unlock()
		if count > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(payloads) == 0 {
		t.Fatalf("expected published payload")
	}
	if payloads[0]["category"] != "lifecycle-tool" {
		t.Fatalf("unexpected category: %#v", payloads[0]["category"])
	}
}

func TestToolAppResourceMonitoringCanBeDisabled(t *testing.T) {
	enabled := false
	app := Start(AppConfig{Title: "disabled-lifecycle-tool"})
	monitor, err := app.EnableResourceMonitoring(EnableResourceMonitoringOptions{
		Enabled:            &enabled,
		Interval:           10 * time.Millisecond,
		PublishImmediately: true,
	})
	if err != nil {
		t.Fatalf("enable disabled resource monitoring: %v", err)
	}

	time.Sleep(30 * time.Millisecond)

	if monitor.Running() {
		t.Fatalf("disabled monitor should not be running")
	}
	if app.beginToolRequest() {
		t.Fatalf("disabled monitoring should not track requests")
	}
	if monitor.CollectOnce()["status"] != MACHINE_STATUS_IDLE {
		t.Fatalf("disabled monitor should collect idle status")
	}
}

func TestToolAppResourceMonitorStatusTracksActiveRequest(t *testing.T) {
	app := Start(AppConfig{Title: "status-tool"})
	monitor, err := app.EnableResourceMonitoring(EnableResourceMonitoringOptions{
		Publish: func(map[string]any) error { return nil },
	})
	if err != nil {
		t.Fatalf("enable resource monitoring: %v", err)
	}
	defer monitor.Stop(time.Second)

	if monitor.CollectOnce()["status"] != MACHINE_STATUS_IDLE {
		t.Fatalf("expected idle before request")
	}
	if !app.beginToolRequest() {
		t.Fatalf("expected request to be tracked")
	}
	defer app.endToolRequest()
	if monitor.CollectOnce()["status"] != MACHINE_STATUS_BUSY {
		t.Fatalf("expected busy during request")
	}
}

type fakeMetricsPublisher struct {
	mu       sync.Mutex
	messages []fakePublishedMessage
}

type fakePublishedMessage struct {
	data       []byte
	attributes map[string]string
}

func (p *fakeMetricsPublisher) Publish(_ context.Context, data []byte, attributes map[string]string, _ string) (any, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.messages = append(p.messages, fakePublishedMessage{
		data:       append([]byte(nil), data...),
		attributes: cloneStringMap(attributes),
	})
	return "message-1", nil
}

func (p *fakeMetricsPublisher) Len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.messages)
}

func (p *fakeMetricsPublisher) First() fakePublishedMessage {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.messages) == 0 {
		return fakePublishedMessage{}
	}
	return p.messages[0]
}

func TestNewPubSubMetricsPublisherRequiresTopic(t *testing.T) {
	_, err := NewPubSubMetricsPublisher(PubSubMetricsPublisherOptions{})
	if err == nil || !strings.Contains(err.Error(), "topic is required") {
		t.Fatalf("expected topic required error, got: %v", err)
	}
}

func TestNewPubSubMetricsPublisherRequiresProjectIDForShortTopic(t *testing.T) {
	_, err := NewPubSubMetricsPublisher(PubSubMetricsPublisherOptions{
		Topic: "my-topic",
	})
	if err == nil || !strings.Contains(err.Error(), "project_id is required") {
		t.Fatalf("expected project_id required error, got: %v", err)
	}
}

func TestNewPubSubMetricsPublisherFullTopicPath(t *testing.T) {
	pub, err := NewPubSubMetricsPublisher(PubSubMetricsPublisherOptions{
		Topic: "projects/my-project/topics/my-topic",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pub.topicPath != "projects/my-project/topics/my-topic" {
		t.Fatalf("unexpected topicPath: %s", pub.topicPath)
	}
}

func TestNewPubSubMetricsPublisherDecodesBase64CredentialsJSON(t *testing.T) {
	import_b64 := "eyJ0eXBlIjoic2VydmljZV9hY2NvdW50IiwicHJvamVjdF9pZCI6InRlc3QifQ=="
	pub, err := NewPubSubMetricsPublisher(PubSubMetricsPublisherOptions{
		Topic:           "projects/test/topics/t",
		CredentialsJSON: import_b64,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(pub.credentialsJSON, "{") {
		t.Fatalf("expected decoded JSON, got: %s", pub.credentialsJSON)
	}
}

func TestPubSubMetricsPublisherPublishesViaREST(t *testing.T) {
	// Fake PubSub publish endpoint
	var publishedBody map[string]any
	pubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-token" {
			http.Error(w, "unauthorized", 401)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&publishedBody)
		fmt.Fprintf(w, `{"messageIds":["msg-1"]}`)
	}))
	defer pubServer.Close()

	pub, err := NewPubSubMetricsPublisher(PubSubMetricsPublisherOptions{
		Topic: "projects/test-project/topics/test-topic",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Inject pre-fetched token and redirect to local test server
	pub.mu.Lock()
	pub.accessToken = "test-token"
	pub.tokenExpiry = time.Now().Add(time.Hour)
	pub.topicPath = strings.TrimPrefix(pubServer.URL, "http://") + "/v1/projects/test-project/topics/test-topic"
	pub.httpClient = pubServer.Client()
	pub.mu.Unlock()

	// Override publish URL to use http scheme matching test server
	origTopicPath := pub.topicPath
	_ = origTopicPath
	// Directly test via a custom http round-tripper that rewrites the URL
	pub.httpClient = &http.Client{
		Transport: &rewriteTransport{base: pubServer.URL},
	}
	pub.topicPath = "projects/test-project/topics/test-topic"

	_, err = pub.Publish(context.Background(), []byte(`{"hello":"world"}`), map[string]string{"k": "v"}, "")
	if err != nil {
		t.Fatalf("publish error: %v", err)
	}
	if publishedBody == nil {
		t.Fatal("expected publish body to be received")
	}
	msgs, ok := publishedBody["messages"].([]any)
	if !ok || len(msgs) == 0 {
		t.Fatalf("expected messages in body: %#v", publishedBody)
	}
}

type rewriteTransport struct {
	base string
}

func (rt *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Replace https://pubsub.googleapis.com/v1 with test server base
	newURL := strings.Replace(req.URL.String(), "https://pubsub.googleapis.com/v1", rt.base+"/v1", 1)
	newReq, err := http.NewRequestWithContext(req.Context(), req.Method, newURL, req.Body)
	if err != nil {
		return nil, err
	}
	for k, v := range req.Header {
		newReq.Header[k] = v
	}
	return http.DefaultTransport.RoundTrip(newReq)
}
