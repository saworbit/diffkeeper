package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func TestCaptureDurationRecordsObservation(t *testing.T) {
	label := "file_test"
	start := time.Now()
	time.Sleep(5 * time.Millisecond)
	ObserveCapture(start, label, "success")

	mfs, err := Registry.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	found := false
	for _, mf := range mfs {
		if mf.GetName() != "diffkeeper_capture_duration_ms" {
			continue
		}
		found = true
		if len(mf.Metric) == 0 {
			t.Fatalf("capture_duration_ms metric has no samples")
		}
		if got := mf.Metric[0].GetHistogram().GetSampleCount(); got == 0 {
			t.Fatalf("expected histogram sample count > 0, got %d", got)
		}
	}
	if !found {
		t.Fatalf("diffkeeper_capture_duration_ms not found")
	}
}

func TestMetricsEndpointExposesCoreMetrics(t *testing.T) {
	ObserveCapture(time.Now(), "file_test_endpoint", "success")

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	promhttp.HandlerFor(Registry, promhttp.HandlerOpts{EnableOpenMetrics: true}).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status code: %d", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "diffkeeper_capture_duration_ms_bucket") {
		t.Fatalf("expected capture_duration_ms histogram buckets, body: %s", body)
	}
	if !strings.Contains(body, "diffkeeper_up") {
		t.Fatalf("expected up gauge, body: %s", body)
	}
}
