package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sudo-ivan/snikketx/web-portal/internal/prosody"
)

// fakeProsody implements ProsodyClient by embedding the interface, so only the
// methods a test exercises need a real implementation.
type fakeProsody struct {
	ProsodyClient

	listUsers   func(ctx context.Context, token string) ([]prosody.AdminUserInfo, error)
	listGroups  func(ctx context.Context, token string) ([]prosody.AdminGroupInfo, error)
	listInvites func(ctx context.Context, token string) ([]prosody.AdminInviteInfo, error)
}

func (f *fakeProsody) IsClientRegistered() bool { return true }

func (f *fakeProsody) GetSystemMetrics(ctx context.Context, token string) (map[string]any, error) {
	return map[string]any{}, nil
}

func (f *fakeProsody) ListUsers(ctx context.Context, token string) ([]prosody.AdminUserInfo, error) {
	return f.listUsers(ctx, token)
}

func (f *fakeProsody) ListGroups(ctx context.Context, token string) ([]prosody.AdminGroupInfo, error) {
	if f.listGroups == nil {
		return nil, nil
	}
	return f.listGroups(ctx, token)
}

func (f *fakeProsody) ListInvites(ctx context.Context, token string) ([]prosody.AdminInviteInfo, error) {
	if f.listInvites == nil {
		return nil, nil
	}
	return f.listInvites(ctx, token)
}

func TestAdminUsersWithFakeBackend(t *testing.T) {
	users := []prosody.AdminUserInfo{
		{Localpart: "alice", DisplayName: "Alice", Roles: []string{prosody.ScopeAdmin}, Enabled: true},
		{Localpart: "bob", Enabled: false},
	}

	cases := []struct {
		name         string
		listUsers    func(ctx context.Context, token string) ([]prosody.AdminUserInfo, error)
		listGroups   func(ctx context.Context, token string) ([]prosody.AdminGroupInfo, error)
		wantStatus   int
		wantContains string
		wantLocation string
	}{
		{
			name: "lists accounts",
			listUsers: func(ctx context.Context, token string) ([]prosody.AdminUserInfo, error) {
				if token != "tok" {
					t.Errorf("token = %q, want the session token", token)
				}
				return users, nil
			},
			wantStatus:   http.StatusOK,
			wantContains: "alice@example.test",
		},
		{
			name: "missing circles still render",
			listUsers: func(ctx context.Context, token string) ([]prosody.AdminUserInfo, error) {
				return users, nil
			},
			listGroups: func(ctx context.Context, token string) ([]prosody.AdminGroupInfo, error) {
				return nil, errors.New("groups endpoint gone")
			},
			wantStatus:   http.StatusOK,
			wantContains: "alice@example.test",
		},
		{
			name: "expired token returns to login",
			listUsers: func(ctx context.Context, token string) ([]prosody.AdminUserInfo, error) {
				return nil, &prosody.HTTPError{Status: http.StatusUnauthorized}
			},
			wantStatus:   http.StatusSeeOther,
			wantLocation: pathLogin,
		},
		{
			name: "unreachable backend reports unavailable",
			listUsers: func(ctx context.Context, token string) ([]prosody.AdminUserInfo, error) {
				return nil, errors.New("dial tcp: connection refused")
			},
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name: "backend error page for other failures",
			listUsers: func(ctx context.Context, token string) ([]prosody.AdminUserInfo, error) {
				return nil, &prosody.HTTPError{Status: http.StatusInternalServerError}
			},
			wantStatus: http.StatusBadGateway,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := newTestApp(t, "http://prosody.invalid")
			app.Prosody = &fakeProsody{listUsers: tc.listUsers, listGroups: tc.listGroups}
			handler := app.Routes()
			cookie := adminCookie(t, app)

			req := httptest.NewRequest(http.MethodGet, pathAdminUsers, nil)
			req.AddCookie(cookie)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status %d, want %d\n%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantLocation != "" {
				if got := rec.Header().Get("Location"); got != tc.wantLocation {
					t.Fatalf("Location = %q, want %q", got, tc.wantLocation)
				}
			}
			if tc.wantContains != "" && !strings.Contains(rec.Body.String(), tc.wantContains) {
				t.Fatalf("body missing %q\n%s", tc.wantContains, rec.Body.String())
			}
		})
	}
}
