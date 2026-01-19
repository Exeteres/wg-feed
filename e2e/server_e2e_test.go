package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/exeteres/wg-feed/internal/etcd"
	"github.com/exeteres/wg-feed/internal/model"
	"github.com/exeteres/wg-feed/internal/server/httpapi"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func startEtcd(t *testing.T) (endpoint string, cleanup func()) {
	t.Helper()

	ctx := context.Background()
	req := testcontainers.ContainerRequest{
		Image:        "quay.io/coreos/etcd:v3.5.13",
		ExposedPorts: []string{"2379/tcp"},
		Cmd: []string{
			"/usr/local/bin/etcd",
			"--name", "default",
			"--data-dir", "/etcd-data",
			"--advertise-client-urls", "http://0.0.0.0:2379",
			"--listen-client-urls", "http://0.0.0.0:2379",
			"--listen-peer-urls", "http://0.0.0.0:2380",
			"--initial-advertise-peer-urls", "http://0.0.0.0:2380",
			"--initial-cluster", "default=http://0.0.0.0:2380",
			"--initial-cluster-state", "new",
		},
		WaitingFor: wait.ForListeningPort("2379/tcp").WithStartupTimeout(30 * time.Second),
	}

	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("start etcd container: %v", err)
	}

	host, err := c.Host(ctx)
	if err != nil {
		_ = c.Terminate(ctx)
		t.Fatalf("get container host: %v", err)
	}
	port, err := c.MappedPort(ctx, "2379/tcp")
	if err != nil {
		_ = c.Terminate(ctx)
		t.Fatalf("get mapped port: %v", err)
	}

	endpoint = fmt.Sprintf("http://%s:%s", host, port.Port())
	cleanup = func() {
		_ = c.Terminate(context.Background())
	}
	return endpoint, cleanup
}

func startServer(t *testing.T, etcdEndpoints []string) (baseURL string, shutdown func()) {
	t.Helper()

	etcdClient, err := etcd.NewClient(etcdEndpoints)
	if err != nil {
		t.Fatalf("create etcd client: %v", err)
	}

	logger := log.New(io.Discard, "test ", log.LstdFlags)
	st := etcd.NewStore(etcdClient)
	h := httpapi.NewHandler(st, logger)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = etcdClient.Close()
		t.Fatalf("listen: %v", err)
	}

	srv := &http.Server{Handler: h}
	go func() {
		_ = srv.Serve(ln)
	}()

	baseURL = "http://" + ln.Addr().String()
	shutdown = func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		_ = ln.Close()
		_ = etcdClient.Close()
	}
	return baseURL, shutdown
}

func putKey(t *testing.T, endpoint, key string, value []byte) {
	t.Helper()

	cli, err := etcd.NewClient([]string{endpoint})
	if err != nil {
		t.Fatalf("create etcd client: %v", err)
	}
	defer cli.Close()

	st := etcd.NewStore(cli)
	err = st.Put(context.Background(), key, value)
	if err != nil {
		t.Fatalf("put key: %v", err)
	}
}

func TestWGFeedServer_200(t *testing.T) {
	endpoint, cleanupEtcd := startEtcd(t)
	defer cleanupEtcd()

	feedPath := "client-a"
	key := "wg-feed/feeds/" + feedPath
	value := []byte(`{
	"revision": "test-rev-1",
	"ttl_seconds": 60,
	"encrypted": false,
	"data": {
		"id": "11111111-1111-4111-8111-111111111111",
		"endpoints": ["https://example.invalid/client-a"],
		"display_info": {"title": "Example"},
		"tunnels": [
			{
				"id": "t1",
				"name": "home",
				"display_info": {"title": "Home"},
				"wg_quick_config": "[Interface]\\nPrivateKey = x\\nAddress = 10.0.0.2/32\\n"
			}
		]
	}
}`)

	putKey(t, endpoint, key, value)

	baseURL, shutdown := startServer(t, []string{endpoint})
	defer shutdown()

	req, err := http.NewRequest(http.MethodGet, baseURL+"/"+feedPath, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 got %d body=%s", resp.StatusCode, string(b))
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("expected json content-type got %q", ct)
	}

	b, _ := io.ReadAll(resp.Body)
	if strings.TrimSpace(string(b)) == "" {
		t.Fatalf("expected non-empty body")
	}
	var sr model.SuccessResponse
	if err := json.Unmarshal(b, &sr); err != nil {
		t.Fatalf("unmarshal response: %v body=%s", err, string(b))
	}
	if err := sr.Validate(); err != nil {
		t.Fatalf("validate response: %v", err)
	}
}

func TestWGFeedServer_404(t *testing.T) {
	endpoint, cleanupEtcd := startEtcd(t)
	defer cleanupEtcd()

	baseURL, shutdown := startServer(t, []string{endpoint})
	defer shutdown()

	req, err := http.NewRequest(http.MethodGet, baseURL+"/does-not-exist", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 404 got %d body=%s", resp.StatusCode, string(b))
	}
}

func TestWGFeedServer_304_IfNoneMatch(t *testing.T) {
	endpoint, cleanupEtcd := startEtcd(t)
	defer cleanupEtcd()

	feedPath := "client-304"
	key := "wg-feed/feeds/" + feedPath
	value := []byte(`{
	"revision": "rev-304-test",
	"ttl_seconds": 60,
	"encrypted": false,
	"data": {
		"id": "22222222-2222-4222-8222-222222222222",
		"endpoints": ["https://example.invalid/client-304"],
		"display_info": {"title": "Example"},
		"tunnels": [
			{
				"id": "t1",
				"name": "home",
				"display_info": {"title": "Home"},
				"wg_quick_config": "[Interface]\\nPrivateKey = x\\nAddress = 10.0.0.2/32\\n"
			}
		]
	}
}`)
	putKey(t, endpoint, key, value)

	baseURL, shutdown := startServer(t, []string{endpoint})
	defer shutdown()

	req, err := http.NewRequest(http.MethodGet, baseURL+"/"+feedPath, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("If-None-Match", "rev-304-test")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotModified {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 304 got %d body=%s", resp.StatusCode, string(b))
	}
}

func TestWGFeedServer_SSE_StreamsUpdates(t *testing.T) {
	endpoint, cleanupEtcd := startEtcd(t)
	defer cleanupEtcd()

	feedPath := "client-sse"
	key := "wg-feed/feeds/" + feedPath
	value1 := []byte(`{"revision":"rev-sse-1","ttl_seconds":60,"encrypted":false,"data":{"id":"33333333-3333-4333-8333-333333333333","endpoints":["https://example.invalid/client-sse"],"display_info":{"title":"Example"},"tunnels":[{"id":"t1","name":"home","display_info":{"title":"Home"},"wg_quick_config":"[Interface]\\nPrivateKey = x\\nAddress = 10.0.0.2/32\\n"}]}}`)
	value2 := []byte(`{"revision":"rev-sse-2","ttl_seconds":60,"encrypted":false,"data":{"id":"33333333-3333-4333-8333-333333333333","endpoints":["https://example.invalid/client-sse"],"display_info":{"title":"Example"},"tunnels":[{"id":"t1","name":"home","display_info":{"title":"Home"},"wg_quick_config":"[Interface]\\nPrivateKey = x\\nAddress = 10.0.0.3/32\\n"}]}}`)

	putKey(t, endpoint, key, value1)

	baseURL, shutdown := startServer(t, []string{endpoint})
	defer shutdown()

	req, err := http.NewRequest(http.MethodGet, baseURL+"/"+feedPath, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 got %d body=%s", resp.StatusCode, string(b))
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("expected event-stream content-type got %q", ct)
	}

	r := bufio.NewReader(resp.Body)

	data1 := readOneSSEDataEvent(t, r, 5*time.Second)
	var sr1 model.SuccessResponse
	if err := json.Unmarshal([]byte(data1), &sr1); err != nil {
		t.Fatalf("unmarshal event1: %v data=%q", err, data1)
	}
	if err := sr1.Validate(); err != nil {
		t.Fatalf("validate event1: %v data=%q", err, data1)
	}
	if sr1.Revision != "rev-sse-1" {
		t.Fatalf("expected revision rev-sse-1 got %q", sr1.Revision)
	}

	// Trigger an update after the first event is observed.
	putKey(t, endpoint, key, value2)

	data2 := readOneSSEDataEvent(t, r, 5*time.Second)
	var sr2 model.SuccessResponse
	if err := json.Unmarshal([]byte(data2), &sr2); err != nil {
		t.Fatalf("unmarshal event2: %v data=%q", err, data2)
	}
	if err := sr2.Validate(); err != nil {
		t.Fatalf("validate event2: %v data=%q", err, data2)
	}
	if sr2.Revision != "rev-sse-2" {
		t.Fatalf("expected revision rev-sse-2 got %q", sr2.Revision)
	}
}

func readOneSSEDataEvent(t *testing.T, r *bufio.Reader, timeout time.Duration) string {
	t.Helper()

	ch := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		var data string
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				errCh <- err
				return
			}
			trimmed := strings.TrimRight(line, "\r\n")
			if trimmed == "" {
				// end of event
				if data != "" {
					ch <- data
					return
				}
				continue
			}
			if strings.HasPrefix(trimmed, "data: ") {
				data = strings.TrimPrefix(trimmed, "data: ")
			}
		}
	}()

	select {
	case s := <-ch:
		return s
	case err := <-errCh:
		t.Fatalf("read sse event: %v", err)
		return ""
	case <-time.After(timeout):
		t.Fatalf("timeout waiting for sse event")
		return ""
	}
}

func TestWGFeedServer_500_OnValidationError(t *testing.T) {
	endpoint, cleanupEtcd := startEtcd(t)
	defer cleanupEtcd()

	feedPath := "bad"
	key := "wg-feed/feeds/" + feedPath
	value := []byte(`{
	"revision": "test-rev-2",
	"ttl_seconds": 60,
	"encrypted": false,
	"data": {
		"id": "not-a-uuid",
		"endpoints": ["https://example.invalid/bad"],
		"display_info": {"title": "Bad"},
		"tunnels": [
			{
				"id": "t1",
				"name": "home",
				"display_info": {"title": "Home"},
				"wg_quick_config": "[Interface]\\nPrivateKey = x\\n"
			}
		]
	}
}`)

	putKey(t, endpoint, key, value)

	// Ensure the process accepts these env vars (even though the server is started in-process for testing).
	os.Setenv("ETCD_ENDPOINTS", endpoint)
	os.Setenv("SERVER_PORT", "0")
	t.Cleanup(func() {
		os.Unsetenv("ETCD_ENDPOINTS")
		os.Unsetenv("SERVER_PORT")
	})

	baseURL, shutdown := startServer(t, []string{endpoint})
	defer shutdown()

	req, err := http.NewRequest(http.MethodGet, baseURL+"/"+feedPath, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 500 got %d body=%s", resp.StatusCode, string(b))
	}
}
