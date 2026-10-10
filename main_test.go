package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func postDashboard(ctx context.Context) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("owner=team&repo=project")).WithContext(ctx)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

func TestHandlerValidatesRequests(t *testing.T) {
	client := testGitHubClient(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("invalid request reached GitHub")
	})
	handler, err := newHandler("kodata", client)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, method, path, body string
		status                   int
	}{
		{"home", "GET", "/", "", http.StatusOK},
		{"missing", "GET", "/missing", "", http.StatusNotFound},
		{"method", "PUT", "/", "", http.StatusMethodNotAllowed},
		{"empty", "POST", "/", "", http.StatusBadRequest},
		{"invalid_owner", "POST", "/", "owner=team%2Fother&repo=project", http.StatusBadRequest},
		{"invalid_repo", "POST", "/", "owner=team&repo=..", http.StatusBadRequest},
		{"malformed", "POST", "/", "owner=%zz&repo=project", http.StatusBadRequest},
		{"oversized", "POST", "/", "owner=team&repo=" + strings.Repeat("a", 5000), http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("got %d, want %d: %s", w.Code, tc.status, w.Body.String())
			}
			if tc.status == http.StatusMethodNotAllowed && w.Header().Get("Allow") != "GET, POST" {
				t.Fatal("missing Allow header")
			}
		})
	}
}

func TestHandlerRendersEscapedDashboard(t *testing.T) {
	client := testGitHubClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeAPIResponse(t, w, `{"workflow_runs":[{"name":"<script>alert(1)</script>","status":"completed","conclusion":"success","event":"push"}]}`)
	})
	handler, err := newHandler("kodata", client)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, postDashboard(t.Context()))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "team/project") || !strings.Contains(w.Body.String(), "&lt;script&gt;") {
		t.Fatalf("unexpected response: %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "<script>alert(1)</script>") {
		t.Fatal("workflow name was not escaped")
	}
}

func TestHandlerUpstreamErrors(t *testing.T) {
	for _, tc := range []struct {
		name       string
		apiStatus  int
		wantStatus int
		contextErr bool
	}{
		{"upstream", http.StatusInternalServerError, http.StatusBadGateway, false},
		{"rate_limit", http.StatusForbidden, http.StatusServiceUnavailable, false},
		{"secondary_rate_limit", http.StatusTooManyRequests, http.StatusServiceUnavailable, false},
		{"cancellation", http.StatusOK, http.StatusGatewayTimeout, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := testGitHubClient(t, func(w http.ResponseWriter, _ *http.Request) {
				if tc.contextErr {
					t.Error("canceled request reached GitHub")
				}
				if tc.name == "rate_limit" {
					w.Header().Set("X-RateLimit-Remaining", "0")
					w.Header().Set("X-RateLimit-Reset", fmt.Sprint(time.Now().Add(time.Hour).Unix()))
				}
				if tc.name == "secondary_rate_limit" {
					w.Header().Set("Retry-After", "60")
				}
				w.WriteHeader(tc.apiStatus)
				writeAPIResponse(t, w, `{"message":"private upstream details","documentation_url":"https://docs.github.com/rest/using-the-rest-api/rate-limits-for-the-rest-api#about-secondary-rate-limits"}`)
			})
			handler, err := newHandler("kodata", client)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.contextErr {
				cancel()
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, postDashboard(ctx))
			if w.Code != tc.wantStatus {
				t.Fatalf("got %d, want %d: %s", w.Code, tc.wantStatus, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "private upstream details") {
				t.Fatal("upstream error leaked to response")
			}
			if (tc.name == "rate_limit" || tc.name == "secondary_rate_limit") && w.Header().Get("Retry-After") == "" {
				t.Fatal("missing Retry-After header")
			}
		})
	}
}

func templateDataPath(t *testing.T, dashboard string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "templates"), 0o750); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"input.html": "home", "dashboard.html": dashboard} {
		if err := os.WriteFile(filepath.Join(dir, "templates", name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestHandlerTemplateFailures(t *testing.T) {
	client := testGitHubClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeAPIResponse(t, w, `{"workflow_runs":[]}`)
	})
	for _, body := range []string{`{{define "dashboard"}}{{end`, `no dashboard definition`} {
		if _, err := newHandler(templateDataPath(t, body), client); err == nil {
			t.Fatal("expected template initialization error")
		}
	}
	if _, err := newHandler(t.TempDir(), client); err == nil {
		t.Fatal("expected missing template error")
	}
	// Execution must finish before committing a successful response.
	handler, err := newHandler(templateDataPath(t, `{{define "dashboard"}}partial output{{.MissingField}}{{end}}`), client)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, postDashboard(t.Context()))
	if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "partial output") {
		t.Fatalf("partial successful response: %d %s", w.Code, w.Body.String())
	}
}

func TestHandlerUsesPostForm(t *testing.T) {
	client := testGitHubClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/team/project/actions/runs" {
			t.Errorf("unexpected repository: %s", r.URL.Path)
		}
		writeAPIResponse(t, w, `{"workflow_runs":[]}`)
	})
	handler, err := newHandler("kodata", client)
	if err != nil {
		t.Fatal(err)
	}
	r := postDashboard(t.Context())
	r.URL.RawQuery = url.Values{"owner": {"other"}, "repo": {"other"}}.Encode()
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("unexpected response: %d", w.Code)
	}
}
