package feed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/exeteres/wg-feed/internal/model"
)

func TestFetchAnyEndpoints_DoesNotStopOnNonRetriableIfAnotherEndpointSucceeds(t *testing.T) {
	errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(model.ErrorResponse{
			Version:   "wg-feed-00",
			Success:   false,
			Message:   "nope",
			Retriable: false,
		})
	}))
	defer errSrv.Close()

	successSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(model.SuccessResponse{
			Version:    "wg-feed-00",
			Success:    true,
			Revision:   "r1",
			TTLSeconds: 60,
			Data: &model.FeedDocument{
				ID:        "123e4567-e89b-12d3-a456-426614174000",
				Endpoints: []string{"https://example.invalid/sub"},
				DisplayInfo: model.DisplayInfo{
					Title: "t",
				},
				Tunnels: []model.Tunnel{},
			},
		})
	}))
	defer successSrv.Close()

	ctx := context.Background()
	res, used, err := FetchAnyEndpoints(ctx, []string{errSrv.URL, successSrv.URL}, successSrv.URL, "")
	if err != nil {
		t.Fatalf("expected success, got err=%v", err)
	}
	if used != successSrv.URL {
		t.Fatalf("expected used endpoint %q, got %q", successSrv.URL, used)
	}
	if res.Revision != "r1" {
		t.Fatalf("unexpected revision: %q", res.Revision)
	}
}

func TestFetchAnyEndpoints_AllNonRetriable_ReturnsNonRetriable(t *testing.T) {
	errSrv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(model.ErrorResponse{Version: "wg-feed-00", Success: false, Message: "nope", Retriable: false})
	}))
	defer errSrv1.Close()

	errSrv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(model.ErrorResponse{Version: "wg-feed-00", Success: false, Message: "nope", Retriable: false})
	}))
	defer errSrv2.Close()

	_, _, err := FetchAnyEndpoints(context.Background(), []string{errSrv1.URL, errSrv2.URL}, errSrv1.URL, "")
	if err == nil {
		t.Fatalf("expected error")
	}
	wf, ok := AsWGFeedError(err)
	if !ok {
		t.Fatalf("expected WGFeedError, got %T: %v", err, err)
	}
	if wf.Retriable {
		t.Fatalf("expected non-retriable")
	}
}
