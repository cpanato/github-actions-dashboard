package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/palantir/go-githubapp/githubapp"
	"github.com/palantir/go-githubapp/oauth2"
)

func TestLoginAuthorization(t *testing.T) {
	for _, tt := range []struct {
		name            string
		policy          AccessPolicy
		membership      string
		apiStatus, want int
	}{
		{name: "explicit user", policy: AccessPolicy{AllowedUsers: []string{"ALICE"}}, want: 302},
		{name: "active member", policy: AccessPolicy{AllowedOrganizations: []string{"acme"}}, membership: "active", want: 302},
		{name: "pending member", policy: AccessPolicy{AllowedOrganizations: []string{"acme"}}, membership: "pending", want: 403},
		{name: "nonmember", policy: AccessPolicy{AllowedOrganizations: []string{"acme"}}, apiStatus: 404, want: 403},
		{name: "API outage", policy: AccessPolicy{AllowedOrganizations: []string{"acme"}}, apiStatus: 500, want: 503},
		{name: "unlisted user", policy: AccessPolicy{AllowedUsers: []string{"bob"}}, want: 403},
		{name: "empty policy", want: 403},
	} {
		t.Run(tt.name, func(t *testing.T) {
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/user":
					if err := json.NewEncoder(w).Encode(map[string]string{"login": "alice"}); err != nil {
						t.Error(err)
					}
				case "/user/memberships/orgs/acme":
					if tt.apiStatus != 0 {
						w.WriteHeader(tt.apiStatus)
						return
					}
					if err := json.NewEncoder(w).Encode(map[string]string{"state": tt.membership}); err != nil {
						t.Error(err)
					}
				default:
					t.Errorf("unexpected API path %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer api.Close()
			sessions := NewSessionManager(3600, true)
			callback := LoginCallback(githubapp.Config{V3APIURL: api.URL}, "", sessions, tt.policy)
			var authorized bool
			h := sessions.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sessions.Put(r.Context(), SessionKeyRedirect, "/repository/acme/repo")
				callback(w, r, &oauth2.Login{Client: api.Client()})
				authorized = sessions.GetBool(r.Context(), SessionKeyAuthorized)
			}))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/login/callback", nil))
			if w.Code != tt.want {
				t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
			}
			if authorized != (tt.want == 302) {
				t.Fatalf("authorized=%v", authorized)
			}
			if tt.want == 302 {
				if w.Header().Get("Location") != "/repository/acme/repo" {
					t.Fatal("lost return target")
				}
				cookies := w.Result().Cookies()
				if len(cookies) == 0 || !cookies[0].Secure {
					t.Fatal("missing secure session cookie")
				}
			}
		})
	}
}

func TestRequireLoginRequiresAuthorizedSession(t *testing.T) {
	for _, authorized := range []bool{false, true} {
		sessions := NewSessionManager(3600, false)
		protected := RequireLogin(sessions, "")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
		h := sessions.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sessions.Put(r.Context(), SessionKeyUsername, "alice")
			sessions.Put(r.Context(), SessionKeyAuthorized, authorized)
			protected.ServeHTTP(w, r)
		}))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/workflows", nil))
		want := 302
		if authorized {
			want = 204
		}
		if w.Code != want {
			t.Fatalf("authorized=%v, status=%d", authorized, w.Code)
		}
	}
}

func TestAccessPolicyValidation(t *testing.T) {
	for _, p := range []AccessPolicy{{}, {AllowedUsers: []string{" "}}, {AllowedOrganizations: []string{""}}} {
		if err := p.Validate(); err == nil {
			t.Fatalf("accepted invalid policy %+v", p)
		}
	}
	if err := (AccessPolicy{AllowedUsers: []string{"alice"}}).Validate(); err != nil {
		t.Fatal(err)
	}
}
