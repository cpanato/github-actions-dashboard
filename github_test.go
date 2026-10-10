package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/google/go-github/v92/github"
	"github.com/patrickmn/go-cache"
)

func testGitHubClient(t *testing.T, handler http.HandlerFunc) *github.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	base := server.URL + "/"
	client, err := github.NewClient(github.WithURLs(&base, &base), github.WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func writeAPIResponse(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if _, err := fmt.Fprint(w, body); err != nil {
		t.Error(err)
	}
}

func TestGetJobsPaginationAndCache(t *testing.T) {
	var paths []string
	client := testGitHubClient(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/repos/team/project/actions/runs":
			if r.URL.Query().Get("per_page") != "100" || r.URL.Query().Get("created") == "" {
				t.Errorf("missing workflow query parameters: %s", r.URL.RawQuery)
			}
			if r.URL.Query().Get("page") == "2" {
				writeAPIResponse(t, w, `{"workflow_runs":[{"name":"ci","head_sha":"second","status":"queued","event":"push"}]}`)
				return
			}
			w.Header().Set("Link", fmt.Sprintf(`<http://%s/repos/team/project/actions/runs?page=2>; rel="next"`, r.Host))
			// Repository metadata must not overwrite the requested owner/repo.
			writeAPIResponse(t, w, `{"workflow_runs":[{"name":"ci","head_sha":"first","status":"in_progress","event":"pull_request","repository":{"name":"other","owner":{"login":"other"}}}]}`)
		case "/repos/team/project/commits/first/pulls":
			writeAPIResponse(t, w, `[{"html_url":"https://github.com/team/project/pull/1"}]`)
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	})
	c := cache.New(cacheDuration, 0)
	dash, err := getJobs(t.Context(), c, client, "team", "project")
	if err != nil {
		t.Fatal(err)
	}
	if dash.Owner != "team" || dash.Repo != "project" {
		t.Fatalf("wrong repository: %s/%s", dash.Owner, dash.Repo)
	}
	runs := dash.Data["ci"]
	if len(runs) != 2 || runs[0].PRUrl != "https://github.com/team/project/pull/1" || runs[0].Conclusion != "progress" || runs[1].Conclusion != "queued" {
		t.Fatalf("unexpected runs: %+v", runs)
	}
	cached, err := getJobs(t.Context(), c, client, "team", "project")
	if err != nil || !reflect.DeepEqual(cached, dash) || len(paths) != 3 {
		t.Fatalf("cache did not preserve dashboard: requests=%v error=%v", paths, err)
	}
}

func TestGetJobsCacheKeysDoNotCollide(t *testing.T) {
	requests := 0
	client := testGitHubClient(t, func(w http.ResponseWriter, _ *http.Request) {
		requests++
		writeAPIResponse(t, w, `{"workflow_runs":[]}`)
	})
	c := cache.New(cacheDuration, 0)
	for _, pair := range [][2]string{{"a-b", "c"}, {"a", "b-c"}} {
		dash, err := getJobs(t.Context(), c, client, pair[0], pair[1])
		if err != nil || dash.Owner != pair[0] || dash.Repo != pair[1] {
			t.Fatalf("wrong cached repository: %+v, %v", dash, err)
		}
	}
	if requests != 2 {
		t.Fatalf("expected distinct cache entries, got %d requests", requests)
	}
}

func TestGetJobsErrorsAreNotCached(t *testing.T) {
	for _, failPR := range []bool{false, true} {
		t.Run(fmt.Sprintf("pull_request=%t", failPR), func(t *testing.T) {
			fail := true
			client := testGitHubClient(t, func(w http.ResponseWriter, r *http.Request) {
				if fail && (!failPR || r.URL.Path == "/repos/team/project/commits/abc/pulls") {
					w.WriteHeader(http.StatusInternalServerError)
					writeAPIResponse(t, w, `{"message":"upstream unavailable"}`)
					return
				}
				if failPR && r.URL.Path == "/repos/team/project/actions/runs" {
					writeAPIResponse(t, w, `{"workflow_runs":[{"name":"ci","head_sha":"abc","event":"pull_request"}]}`)
					return
				}
				if failPR {
					writeAPIResponse(t, w, `[]`)
				} else {
					writeAPIResponse(t, w, `{"workflow_runs":[]}`)
				}
			})
			c := cache.New(cacheDuration, 0)
			if _, err := getJobs(t.Context(), c, client, "team", "project"); err == nil {
				t.Fatal("expected API error")
			}
			if c.ItemCount() != 0 {
				t.Fatal("failed fetch was cached")
			}
			fail = false
			if _, err := getJobs(t.Context(), c, client, "team", "project"); err != nil {
				t.Fatalf("next request should recover: %v", err)
			}
		})
	}
}

func TestGetJobsCancellation(t *testing.T) {
	started := make(chan struct{})
	client := testGitHubClient(t, func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := getJobs(ctx, cache.New(cacheDuration, 0), client, "team", "project")
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("GitHub request did not start")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected cancellation, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("GitHub request ignored cancellation")
	}
}

func TestGetJobsPullRequestPagination(t *testing.T) {
	client := testGitHubClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/team/project/actions/runs" {
			writeAPIResponse(t, w, `{"workflow_runs":[{"name":"ci","head_sha":"abc","event":"pull_request"}]}`)
			return
		}
		if r.URL.Query().Get("page") != "2" {
			w.Header().Set("Link", fmt.Sprintf(`<http://%s/repos/team/project/commits/abc/pulls?page=2>; rel="next"`, r.Host))
			writeAPIResponse(t, w, `[{"html_url":"https://github.com/team/project/pull/1"}]`)
			return
		}
		writeAPIResponse(t, w, `[{"html_url":"https://github.com/team/project/pull/2"}]`)
	})
	dash, err := getJobs(t.Context(), cache.New(cacheDuration, 0), client, "team", "project")
	if err != nil {
		t.Fatal(err)
	}
	if runs := dash.Data["ci"]; len(runs) != 1 || runs[0].PRUrl != "" {
		t.Fatalf("ambiguous pull request must not be linked: %+v", runs)
	}
}
