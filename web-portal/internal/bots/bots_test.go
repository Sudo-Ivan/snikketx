package bots

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// stubAPI records the requests the client makes and answers with a
// canned JSON payload or status code.
type stubAPI struct {
	t        *testing.T
	server   *httptest.Server
	method   string
	path     string
	host     string
	auth     string
	payload  map[string]any
	response any
	status   int
}

func newStub(t *testing.T, response any, status int) *stubAPI {
	s := &stubAPI{t: t, response: response, status: status}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.method = r.Method
		s.path = r.URL.Path
		s.host = r.Host
		s.auth = r.Header.Get("Authorization")
		if r.Body != nil {
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err == nil {
				s.payload = payload
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(s.status)
		if s.response != nil {
			raw, _ := json.Marshal(s.response)
			_, _ = w.Write(raw)
		}
	}))
	t.Cleanup(s.server.Close)
	return s
}

func (s *stubAPI) client() *Client {
	return New(s.server.URL+"/bots", "bots.example.test", "test-admin-token")
}

func (s *stubAPI) checkCommon(t *testing.T) {
	t.Helper()
	if s.host != "bots.example.test" {
		t.Fatalf("Host header = %q, want bots.example.test", s.host)
	}
	if s.auth != "Bearer test-admin-token" {
		t.Fatalf("Authorization = %q", s.auth)
	}
}

func TestClientListFiltersByOwner(t *testing.T) {
	stub := newStub(t, map[string]any{
		"bots": []map[string]any{
			{"name": "mine", "jid": "mine@bots.x", "owner": "alice@x"},
			{"name": "theirs", "jid": "theirs@bots.x", "owner": "mallory@x"},
		},
	}, http.StatusOK)

	got, err := stub.client().List(context.Background(), "alice@x")
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	stub.checkCommon(t)
	if stub.method != http.MethodGet || stub.path != "/bots/" {
		t.Fatalf("%s %s, want GET /bots/", stub.method, stub.path)
	}
	if len(got) != 1 || got[0].Name != "mine" {
		t.Fatalf("List = %+v, want only the owned bot", got)
	}
}

func TestClientCreatePostsRecord(t *testing.T) {
	stub := newStub(t, map[string]any{
		"name": "echo", "owner": "alice@x", "token": "sxb_aaaaaaaa_secret",
	}, http.StatusCreated)

	bot, err := stub.client().Create(context.Background(), "echo", "alice@x", "Echo bot")
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}
	if stub.method != http.MethodPost || stub.path != "/bots/" {
		t.Fatalf("%s %s, want POST /bots/", stub.method, stub.path)
	}
	if stub.payload["name"] != "echo" || stub.payload["owner"] != "alice@x" {
		t.Fatalf("payload = %+v", stub.payload)
	}
	if bot.Token != "sxb_aaaaaaaa_secret" {
		t.Fatalf("Token = %q", bot.Token)
	}
}

func TestClientSetDisabledUsesPatch(t *testing.T) {
	stub := newStub(t, map[string]any{"name": "echo", "disabled": true}, http.StatusOK)

	err := stub.client().SetDisabled(context.Background(), "echo", true)
	if err != nil {
		t.Fatalf("SetDisabled error: %v", err)
	}
	if stub.method != http.MethodPatch || stub.path != "/bots/echo" {
		t.Fatalf("%s %s, want PATCH /bots/echo", stub.method, stub.path)
	}
	if stub.payload["disabled"] != true {
		t.Fatalf("payload = %+v", stub.payload)
	}
}

func TestClientTokenLifecycle(t *testing.T) {
	stub := newStub(t, map[string]any{"token": "sxb_bbbbbbbb_new", "id": "tok1"}, http.StatusCreated)

	token, id, err := stub.client().MintToken(context.Background(), "echo", "laptop", nil, 86400)
	if err != nil {
		t.Fatalf("MintToken error: %v", err)
	}
	if stub.method != http.MethodPost || stub.path != "/bots/echo/tokens" {
		t.Fatalf("%s %s, want POST /bots/echo/tokens", stub.method, stub.path)
	}
	if token != "sxb_bbbbbbbb_new" || id != "tok1" {
		t.Fatalf("token/id = %q/%q", token, id)
	}
	if stub.payload["ttl"] != float64(86400) || stub.payload["name"] != "laptop" {
		t.Fatalf("payload = %+v", stub.payload)
	}

	stub.response = map[string]any{"tokens": []map[string]any{
		{"id": "tok1", "scopes": []string{"read", "write"}, "created": 1},
	}}
	toks, err := stub.client().ListTokens(context.Background(), "echo")
	if err != nil {
		t.Fatalf("ListTokens error: %v", err)
	}
	if len(toks) != 1 || toks[0].ID != "tok1" || toks[0].Scopes[0] != "read" {
		t.Fatalf("tokens = %+v", toks)
	}

	stub.response = nil
	stub.status = http.StatusOK
	if err := stub.client().RevokeToken(context.Background(), "echo", "tok1"); err != nil {
		t.Fatalf("RevokeToken error: %v", err)
	}
	if stub.method != http.MethodDelete || stub.path != "/bots/echo/tokens/tok1" {
		t.Fatalf("%s %s, want DELETE /bots/echo/tokens/tok1", stub.method, stub.path)
	}
}

func TestClientErrorMapping(t *testing.T) {
	stub := newStub(t, map[string]any{"error": "owner-quota"}, http.StatusForbidden)

	_, err := stub.client().List(context.Background(), "alice@x")
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want APIError", err)
	}
	if apiErr.Err != "owner-quota" || apiErr.Code != http.StatusForbidden {
		t.Fatalf("APIError = %+v", apiErr)
	}
}

func TestValidName(t *testing.T) {
	for _, name := range []string{"echo", "a", "bot-1.2_3", "bot.name"} {
		if !ValidName(name) {
			t.Fatalf("ValidName(%q) = false", name)
		}
	}
	for _, name := range []string{"", "-lead", "_lead", ".lead", "UPPER", "a b", "audit",
		"has space", "ünïcode", "a/b"} {
		if ValidName(name) {
			t.Fatalf("ValidName(%q) = true", name)
		}
	}
	long := "a"
	for i := 0; i < 48; i++ {
		long += "a"
	}
	if ValidName(long) {
		t.Fatal("49 char name must be rejected")
	}
}
