package main

import (
	"bytes"
	"context"
	"errors"
	"html/template"
	"log"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-github/v92/github"
	"github.com/patrickmn/go-cache"
)

const requestTimeout = 30 * time.Second

// Dashboard contains recent workflow runs for a repository.
type Dashboard struct {
	Owner          string
	Repo           string
	DateGenerated  string
	NextGeneration string
	Data           map[string][]Status
}

func main() {
	client, err := github.NewClient(
		github.WithAuthToken(os.Getenv("GITHUB_TOKEN")),
		github.WithTimeout(requestTimeout),
		github.WithEnvProxy(),
	)
	if err != nil {
		log.Fatal(err)
	}
	dataPath := os.Getenv("KO_DATA_PATH")
	if dataPath == "" {
		dataPath = "kodata"
	}
	handler, err := newHandler(dataPath, client)
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{
		Addr:              ":3000",
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      45 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Print("Listening on :3000...")
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

// Status describes a workflow run displayed on the dashboard.
type Status struct {
	SHA         string
	Status      string
	Event       string
	Conclusion  string
	TableStatus string
	JobHTML     string
	WorkflowID  int64
	CreatedAt   time.Time
	PRUrl       string
}

type dashboardHandler struct {
	client   *github.Client
	cache    *cache.Cache
	input    []byte
	template *template.Template
}

func newHandler(dataPath string, client *github.Client) (*dashboardHandler, error) {
	input, err := os.ReadFile(filepath.Join(dataPath, "templates/input.html")) //nolint:gosec // dataPath is trusted deployment configuration.
	if err != nil {
		return nil, err
	}
	tmpl, err := template.ParseFiles(filepath.Join(dataPath, "templates/dashboard.html"))
	if err != nil {
		return nil, err
	}
	if tmpl.Lookup("dashboard") == nil {
		return nil, errors.New("missing dashboard template")
	}
	return &dashboardHandler{
		client:   client,
		cache:    cache.New(cacheDuration, 2*cacheDuration),
		input:    input,
		template: tmpl,
	}, nil
}

var (
	ownerPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*$`)
	repoPattern  = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
)

func (h *dashboardHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeContent(w, r, "input.html", time.Time{}, bytes.NewReader(h.input))
	case http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Invalid form", http.StatusBadRequest)
			return
		}
		owner := strings.TrimSpace(r.PostFormValue("owner"))
		repo := strings.TrimSpace(r.PostFormValue("repo"))
		if !ownerPattern.MatchString(owner) || !repoPattern.MatchString(repo) || repo == "." || repo == ".." {
			http.Error(w, "A valid repository owner and name are required", http.StatusBadRequest)
			return
		}
		h.serveDashboard(w, r, owner, repo)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *dashboardHandler) serveDashboard(w http.ResponseWriter, r *http.Request, owner, repo string) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	data, err := getJobs(ctx, h.cache, h.client, owner, repo)
	if err != nil {
		slog.Error("fetch dashboard", "owner", owner, "repo", repo, "error", err)
		status := http.StatusBadGateway
		var rateLimit *github.RateLimitError
		var abuseLimit *github.AbuseRateLimitError
		switch {
		case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
			status = http.StatusGatewayTimeout
		case errors.As(err, &rateLimit):
			status = http.StatusServiceUnavailable
			if seconds := int(time.Until(rateLimit.Rate.Reset.Time).Seconds()) + 1; seconds > 0 {
				w.Header().Set("Retry-After", strconv.Itoa(seconds))
			}
		case errors.As(err, &abuseLimit):
			status = http.StatusServiceUnavailable
			if abuseLimit.RetryAfter != nil {
				w.Header().Set("Retry-After", strconv.Itoa(int(abuseLimit.RetryAfter.Seconds())+1))
			}
		}
		http.Error(w, http.StatusText(status), status)
		return
	}
	var body bytes.Buffer
	if err := h.template.ExecuteTemplate(&body, "dashboard", data); err != nil {
		slog.Error("render dashboard", "error", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := w.Write(body.Bytes()); err != nil {
		slog.Error("write dashboard", "error", err)
	}
}
