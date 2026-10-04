package youtube

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestVideoStats is #123 against a fake YouTube: the live counts from the
// Data API, the report from the Analytics API filtered to the one video from
// its publication date, and a report with no rows yet (the API lags) read as
// counts without analytics rather than as a failure.
func TestVideoStats(t *testing.T) {
	rows := [][]any{{float64(1234), float64(187), float64(9)}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/youtube/v3/videos":
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{map[string]any{
				"id":         "vid123",
				"snippet":    map[string]any{"title": "Night Drive (Full Album)", "publishedAt": "2026-09-30T12:00:00Z"},
				"statistics": map[string]any{"viewCount": "5000", "likeCount": "320", "commentCount": "41"},
			}}})
		case "/v2/reports":
			q := r.URL.Query()
			if q.Get("filters") != "video==vid123" || q.Get("startDate") != "2026-09-30" || q.Get("ids") != "channel==MINE" {
				t.Errorf("report asked for %v", q)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"columnHeaders": []any{
					map[string]any{"name": "estimatedMinutesWatched"},
					map[string]any{"name": "averageViewDuration"},
					map[string]any{"name": "subscribersGained"},
				},
				"rows": rows,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	prevAPI, prevAnalytics := APIBase, AnalyticsBase
	APIBase, AnalyticsBase = server.URL, server.URL
	t.Cleanup(func() { APIBase, AnalyticsBase = prevAPI, prevAnalytics })

	got, err := VideoStats(context.Background(), "access", "vid123")
	if err != nil {
		t.Fatal(err)
	}
	if got.Views != 5000 || got.Likes != 320 || got.Comments != 41 || !strings.HasPrefix(got.Title, "Night Drive") {
		t.Errorf("counts = %+v", got)
	}
	if !got.AnalyticsAvailable || got.MinutesWatched != 1234 || got.AverageViewSeconds != 187 || got.SubscribersGained != 9 {
		t.Errorf("analytics = %+v", got)
	}

	rows = nil
	got, err = VideoStats(context.Background(), "access", "vid123")
	if err != nil || got.AnalyticsAvailable || got.Views != 5000 {
		t.Errorf("a report with no rows yet: %+v, %v", got, err)
	}
}
