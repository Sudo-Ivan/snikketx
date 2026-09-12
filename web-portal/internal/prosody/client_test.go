package prosody

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sudo-ivan/snikketx/web-portal/internal/xmpp"
)

func TestCallJSONDecodesResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "example.test" {
			t.Errorf("Host = %q, want example.test", r.Host)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("Authorization = %q, want bearer token", got)
		}
		_, _ = w.Write([]byte(`{"name":"Family"}`))
	}))
	t.Cleanup(server.Close)

	c := New(server.URL, "example.test", "test")
	var out struct {
		Name string `json:"name"`
	}
	if err := c.callJSON(context.Background(), http.MethodGet, c.adminEndpoint("groups", "g1"), "tok", nil, &out); err != nil {
		t.Fatal(err)
	}
	if out.Name != "Family" {
		t.Fatalf("got %q", out.Name)
	}
}

func TestCallJSONSendsPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"name":"Family"`) {
			t.Errorf("body %s", body)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q", got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	c := New(server.URL, "example.test", "test")
	err := c.callJSON(context.Background(), http.MethodPost, c.Endpoint+pathAdminAPI+"/groups", "tok",
		map[string]any{"name": "Family"}, nil)
	if err != nil {
		t.Fatal(err)
	}
}

func TestCallJSONNoTokenOmitsAuthorization(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization = %q, want empty", got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	c := New(server.URL, "example.test", "test")
	if err := c.callJSON(context.Background(), http.MethodGet, c.Endpoint+pathREST, "", nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestErrorFromResponse(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		body      string
		wantAPI   bool
		wantError bool
	}{
		{name: "success passes through", status: http.StatusOK, body: "ignored"},
		{name: "api error envelope", status: http.StatusConflict, body: `{"error":{"type":"modify","condition":"conflict","text":"exists"}}`, wantAPI: true, wantError: true},
		{name: "envelope without condition is plain", status: http.StatusBadRequest, body: `{"error":{"text":"nope"}}`, wantError: true},
		{name: "plain body", status: http.StatusBadGateway, body: "upstream broke", wantError: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := &http.Response{
				StatusCode: tc.status,
				Status:     http.StatusText(tc.status),
				Body:       io.NopCloser(strings.NewReader(tc.body)),
			}
			err := errorFromResponse(resp)
			if !tc.wantError {
				if err != nil {
					t.Fatalf("got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected an error")
			}
			var apiErr *APIError
			if tc.wantAPI != errors.As(err, &apiErr) {
				t.Fatalf("apiErr match = %v, want %v (err %v)", errors.As(err, &apiErr), tc.wantAPI, err)
			}
			var httpErr *HTTPError
			if !tc.wantAPI && (!errors.As(err, &httpErr) || httpErr.Status != tc.status) {
				t.Fatalf("got %v, want HTTPError status %d", err, tc.status)
			}
		})
	}
}

func TestStatusOf(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, 0},
		{"http error", &HTTPError{Status: http.StatusForbidden}, http.StatusForbidden},
		{"wrapped http error", errors.Join(errors.New("ctx"), &HTTPError{Status: http.StatusNotFound}), http.StatusNotFound},
		{"iq error", &xmpp.IQError{Status: http.StatusConflict, Condition: "conflict"}, http.StatusConflict},
		{"other error", errors.New("dial tcp"), 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := StatusOf(tc.err); got != tc.want {
				t.Fatalf("StatusOf = %d, want %d", got, tc.want)
			}
		})
	}
}

// newOAuthStub returns a server that handles dynamic client registration and
// the password grant.
func newOAuthStub(t *testing.T, tokenHandler http.HandlerFunc) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc(pathOAuth2+"/register", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"client_id":"cid","client_secret":"secret"}`))
	})
	mux.HandleFunc(pathOAuth2+"/token", tokenHandler)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func TestBearerToken(t *testing.T) {
	server := newOAuthStub(t, func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "cid" || pass != "secret" {
			t.Errorf("client auth = %q/%q ok=%v", user, pass, ok)
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if got := r.PostForm.Get("username"); got != "alice" {
			t.Errorf("username = %q, want the localpart only", got)
		}
		if got := r.PostForm.Get("grant_type"); got != "password" {
			t.Errorf("grant_type = %q", got)
		}
		_, _ = w.Write([]byte(`{"access_token":"tok","token_type":"bearer","scope":"prosody:registered prosody:admin"}`))
	})

	c := New(server.URL, "example.test", "test")
	info, err := c.Login(context.Background(), "alice@example.test", "secretsecret")
	if err != nil {
		t.Fatal(err)
	}
	if info.Token != "tok" || !info.IsAdmin() {
		t.Fatalf("got %#v", info)
	}
	if !c.IsClientRegistered() {
		t.Fatal("client registration was not cached")
	}
}

func TestBearerTokenInvalidGrant(t *testing.T) {
	server := newOAuthStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	})

	c := New(server.URL, "example.test", "test")
	if _, err := c.Login(context.Background(), "alice", "wrong"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("got %v, want ErrInvalidCredentials", err)
	}
}

func TestBearerTokenRejectedWithoutGrant(t *testing.T) {
	server := newOAuthStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized_client"}`))
	})

	c := New(server.URL, "example.test", "test")
	_, err := c.Login(context.Background(), "alice", "wrong")
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.Status != http.StatusUnauthorized {
		t.Fatalf("got %v, want HTTPError 401", err)
	}
}

func TestBearerTokenUnexpectedReply(t *testing.T) {
	server := newOAuthStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream broke"))
	})

	c := New(server.URL, "example.test", "test")
	_, err := c.Login(context.Background(), "alice", "wrong")
	if StatusOf(err) != http.StatusBadGateway {
		t.Fatalf("got %v, want status 502", err)
	}
}

func TestBearerTokenBadTokenType(t *testing.T) {
	server := newOAuthStub(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"tok","token_type":"mac"}`))
	})

	c := New(server.URL, "example.test", "test")
	if _, err := c.Login(context.Background(), "alice", "secretsecret"); err == nil {
		t.Fatal("expected an error for a non-bearer token type")
	}
}
