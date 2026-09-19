package codexauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"chatgpt-codex-proxy/internal/accounts"
	"chatgpt-codex-proxy/internal/config"
)

func TestDeviceCodeResponseUnmarshalJSONAcceptsStringAndNumber(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want int
	}{
		{
			name: "number",
			body: `{"user_code":"ABC","device_auth_id":"dev_123","interval":15}`,
			want: 15,
		},
		{
			name: "string",
			body: `{"user_code":"ABC","device_auth_id":"dev_123","interval":"30"}`,
			want: 30,
		},
		{
			name: "padded string",
			body: `{"user_code":"ABC","device_auth_id":"dev_123","interval":" 45 "}`,
			want: 45,
		},
		{
			name: "blank string",
			body: `{"user_code":"ABC","device_auth_id":"dev_123","interval":""}`,
			want: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var decoded DeviceCodeResponse
			if err := json.Unmarshal([]byte(tc.body), &decoded); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}
			if decoded.Interval != tc.want {
				t.Fatalf("interval = %d, want %d", decoded.Interval, tc.want)
			}
		})
	}
}

func TestOAuthTokenResponseUnmarshalJSONAcceptsBlankAndPaddedExpiresIn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{
			name: "padded string",
			body: `{"access_token":"access","expires_in":" 7200 "}`,
		},
		{
			name: "blank string",
			body: `{"access_token":"access","expires_in":""}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var decoded oauthTokenResponse
			if err := json.Unmarshal([]byte(tc.body), &decoded); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}
		})
	}
}

func TestBuildOAuthTokenUsesTypedExpiresIn(t *testing.T) {
	t.Parallel()

	before := time.Now().UTC()
	var response oauthTokenResponse
	if err := json.Unmarshal([]byte(`{"access_token":"access","refresh_token":"refresh","expires_in":7200}`), &response); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	token := buildOAuthToken(response)

	if token.AccessToken != "access" || token.RefreshToken != "refresh" {
		t.Fatalf("token = %#v", token)
	}
	delta := token.ExpiresAt.Sub(before)
	if delta < 7190*time.Second || delta > 7210*time.Second {
		t.Fatalf("expires_at delta = %s, want about 2h", delta)
	}
}

func TestRefreshPreservesExistingRefreshTokenWhenResponseOmitsOne(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"next-access","expires_in":3600}`))
	}))
	defer server.Close()

	service := NewOAuthService(config.Config{AuthIssuer: server.URL})
	existing := accounts.OAuthToken{
		AccessToken:  "old-access",
		RefreshToken: "existing-refresh",
		ExpiresAt:    time.Now().UTC().Add(-time.Minute),
	}

	refreshed, _, err := service.Refresh(context.Background(), existing, "acct_123")
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if refreshed.RefreshToken != existing.RefreshToken {
		t.Fatalf("refresh token = %q, want existing token %q", refreshed.RefreshToken, existing.RefreshToken)
	}
}

func TestExtractAccountIDReadsJWTClaims(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  oauthTokenResponse
		want string
	}{
		{
			name: "top-level claim",
			raw: oauthTokenResponse{
				IDToken: makeJWT(`{"chatgpt_account_id":"acct_123"}`),
			},
			want: "acct_123",
		},
		{
			name: "nested claim",
			raw: oauthTokenResponse{
				AccessToken: makeJWT(`{"https://api.openai.com/auth":{"chatgpt_account_id":"acct_456"}}`),
			},
			want: "acct_456",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := extractAccountID(tc.raw); got != tc.want {
				t.Fatalf("extractAccountID() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRevokeRevokesRefreshThenAccessToken(t *testing.T) {
	t.Parallel()

	var calls []map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != oauthRevokePath {
			t.Errorf("path = %q, want %q", r.URL.Path, oauthRevokePath)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm() error = %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		calls = append(calls, map[string]string{
			"token":           r.Form.Get("token"),
			"token_type_hint": r.Form.Get("token_type_hint"),
			"client_id":       r.Form.Get("client_id"),
		})
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	service := NewOAuthService(config.Config{
		AuthIssuer:     server.URL,
		OAuthClientID:  "test-client",
		RequestTimeout: time.Second,
	})
	err := service.Revoke(context.Background(), accounts.OAuthToken{
		AccessToken:  "access-secret",
		RefreshToken: "refresh-secret",
	})
	if err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("revocation calls = %d, want 2", len(calls))
	}
	if calls[0]["token"] != "refresh-secret" || calls[0]["token_type_hint"] != "refresh_token" {
		t.Fatalf("first revocation = %#v, want refresh token", calls[0])
	}
	if calls[1]["token"] != "access-secret" || calls[1]["token_type_hint"] != "access_token" {
		t.Fatalf("second revocation = %#v, want access token", calls[1])
	}
	for _, call := range calls {
		if call["client_id"] != "test-client" {
			t.Fatalf("client_id = %q, want test-client", call["client_id"])
		}
	}
}

func TestRevokeReturnsErrorForUpstreamFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream failure", http.StatusBadGateway)
	}))
	defer server.Close()

	service := NewOAuthService(config.Config{AuthIssuer: server.URL, RequestTimeout: time.Second})
	err := service.Revoke(context.Background(), accounts.OAuthToken{RefreshToken: "refresh-secret"})
	if err == nil || !strings.Contains(err.Error(), "status 502") {
		t.Fatalf("Revoke() error = %v, want status 502", err)
	}
	if strings.Contains(err.Error(), "refresh-secret") {
		t.Fatal("Revoke() error exposed token")
	}
}

func makeJWT(payload string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	body := base64.RawURLEncoding.EncodeToString([]byte(payload))
	return strings.Join([]string{header, body, "signature"}, ".")
}
