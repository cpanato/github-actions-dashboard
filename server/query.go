package server

import (
	"fmt"
	"net/http"
	"strconv"
	"time"
)

func parseRepositoryID(r *http.Request) (int64, error) {
	value := r.URL.Query().Get("repository_id")
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 0 {
		return 0, fmt.Errorf("repository_id must be a nonnegative integer")
	}
	return parsed, nil
}

func parseQueryInt(r *http.Request, key string, fallback, minimum, maximum int) (int, error) {
	value := r.URL.Query().Get(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < minimum || parsed > maximum {
		return 0, fmt.Errorf("%s must be an integer between %d and %d", key, minimum, maximum)
	}
	return parsed, nil
}

func parseMetricQuery(r *http.Request) (int64, int, error) {
	repoID, err := parseRepositoryID(r)
	if err != nil {
		return 0, 0, err
	}
	days, err := parseQueryInt(r, "days", 30, 1, 365)
	return repoID, days, err
}

func parseWorkflowQuery(r *http.Request, defaultWindow string) (WorkflowRunOptions, error) {
	opts := WorkflowRunOptions{Status: r.URL.Query().Get("status"), Conclusion: r.URL.Query().Get("conclusion")}
	var err error
	if opts.RepositoryID, err = parseRepositoryID(r); err != nil {
		return opts, err
	}
	if opts.Limit, err = parseQueryInt(r, "limit", 50, 1, 500); err != nil {
		return opts, err
	}
	if opts.Offset, err = parseQueryInt(r, "offset", 0, 0, 100000); err != nil {
		return opts, err
	}
	switch opts.Status {
	case "", "completed", "queued", "in_progress", "waiting", "pending", "requested":
	default:
		return opts, fmt.Errorf("invalid status filter")
	}
	switch opts.Conclusion {
	case "", "success", "failure", "neutral", "cancelled", "skipped", "timed_out", "action_required", "stale", "startup_failure":
	default:
		return opts, fmt.Errorf("invalid conclusion filter")
	}
	window := r.URL.Query().Get("since")
	if window == "" {
		window = defaultWindow
	}
	duration, err := time.ParseDuration(window)
	if err != nil || duration <= 0 {
		return opts, fmt.Errorf("since must be a positive Go duration, such as 24h")
	}
	opts.Since = time.Now().Add(-duration)
	return opts, nil
}
