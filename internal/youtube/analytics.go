package youtube

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"time"
)

// AnalyticsBase is the YouTube Analytics API, a variable for the tests.
var AnalyticsBase = "https://youtubeanalytics.googleapis.com"

// Stats is how one video is doing (#123).
type Stats struct {
	VideoID     string `json:"videoId"`
	Title       string `json:"title"`
	PublishedAt string `json:"publishedAt"`
	// Views, Likes and Comments are the Data API's counts, which are live.
	Views    int64 `json:"views"`
	Likes    int64 `json:"likes"`
	Comments int64 `json:"comments"`
	// The rest is the Analytics API's report from publication to today. It
	// lags by two or three days, so a video uploaded this morning reports
	// nothing here yet while Views already counts.
	MinutesWatched     int64  `json:"minutesWatched"`
	AverageViewSeconds int64  `json:"averageViewSeconds"`
	SubscribersGained  int64  `json:"subscribersGained"`
	AnalyticsThrough   string `json:"analyticsThrough"`
	AnalyticsAvailable bool   `json:"analyticsAvailable"`
	// AnalyticsError says why the report could not be read when the counts
	// could — the counts are still worth showing, so this is not a failure.
	AnalyticsError string `json:"analyticsError"`
}

// VideoStats reads one video's counts and its analytics report.
func VideoStats(ctx context.Context, access, videoID string) (Stats, error) {
	var listed struct {
		Items []struct {
			ID      string `json:"id"`
			Snippet struct {
				Title       string `json:"title"`
				PublishedAt string `json:"publishedAt"`
			} `json:"snippet"`
			Statistics struct {
				ViewCount    string `json:"viewCount"`
				LikeCount    string `json:"likeCount"`
				CommentCount string `json:"commentCount"`
			} `json:"statistics"`
		} `json:"items"`
	}
	q := url.Values{"part": {"snippet,statistics"}, "id": {videoID}}
	if err := getJSON(ctx, access, APIBase+"/youtube/v3/videos?"+q.Encode(), &listed); err != nil {
		return Stats{}, err
	}
	if len(listed.Items) == 0 {
		return Stats{}, errors.New("YouTube has no video with that id on this channel — it may have been deleted")
	}
	item := listed.Items[0]
	stats := Stats{
		VideoID:     item.ID,
		Title:       item.Snippet.Title,
		PublishedAt: item.Snippet.PublishedAt,
		Views:       atoi(item.Statistics.ViewCount),
		Likes:       atoi(item.Statistics.LikeCount),
		Comments:    atoi(item.Statistics.CommentCount),
	}

	start := time.Now().AddDate(0, 0, -28)
	if published, err := time.Parse(time.RFC3339, item.Snippet.PublishedAt); err == nil {
		start = published
	}
	today := time.Now().UTC().Format("2006-01-02")
	report := url.Values{
		"ids":       {"channel==MINE"},
		"startDate": {start.UTC().Format("2006-01-02")},
		"endDate":   {today},
		"metrics":   {"estimatedMinutesWatched,averageViewDuration,subscribersGained"},
		"filters":   {"video==" + videoID},
	}
	var body struct {
		ColumnHeaders []struct {
			Name string `json:"name"`
		} `json:"columnHeaders"`
		Rows [][]any `json:"rows"`
	}
	if err := getJSON(ctx, access, AnalyticsBase+"/v2/reports?"+report.Encode(), &body); err != nil {
		stats.AnalyticsError = err.Error()
		return stats, nil
	}
	stats.AnalyticsThrough = today
	if len(body.Rows) == 0 {
		return stats, nil
	}
	stats.AnalyticsAvailable = true
	for i, header := range body.ColumnHeaders {
		if i >= len(body.Rows[0]) {
			break
		}
		n := number(body.Rows[0][i])
		switch header.Name {
		case "estimatedMinutesWatched":
			stats.MinutesWatched = n
		case "averageViewDuration":
			stats.AverageViewSeconds = n
		case "subscribersGained":
			stats.SubscribersGained = n
		}
	}
	return stats, nil
}

// atoi reads the Data API's counts, which it sends as strings.
func atoi(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

// number reads a report cell, which JSON decodes as a float.
func number(v any) int64 {
	if f, ok := v.(float64); ok {
		return int64(f)
	}
	return 0
}
