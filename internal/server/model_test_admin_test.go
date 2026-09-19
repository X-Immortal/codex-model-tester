package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"chatgpt-codex-proxy/internal/accountmanager"
	"chatgpt-codex-proxy/internal/accounts"
	"chatgpt-codex-proxy/internal/codex"
	"chatgpt-codex-proxy/internal/config"
	"chatgpt-codex-proxy/internal/models"
)

func newModelTestApp(t *testing.T, records ...*accounts.Record) *App {
	t.Helper()
	now := time.Now().UTC()
	for _, record := range records {
		if record.CreatedAt.IsZero() {
			record.CreatedAt = now
		}
		if record.UpdatedAt.IsZero() {
			record.UpdatedAt = now
		}
		if record.Status == "" {
			record.Status = accounts.StatusActive
		}
	}
	accountsSvc := newServerAccounts(t, records...)
	cfg := config.Config{RefreshSkew: time.Minute, CodexBaseURL: "https://example.invalid"}
	catalog := models.NewCatalog(models.BootstrapEntries())
	return &App{
		cfg: cfg, logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		accounts:   accountsSvc,
		accountMgr: accountmanager.NewAccountManager(cfg, accountsSvc, nil, nil, catalog.SupportsRecord),
		models:     catalog,
	}
}

func modelTestRequestContext(body string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/admin/model-test", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	return ctx, recorder
}

func TestAdminModelTestUsesOnlySelectedAccountAndPreservesRawModel(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	app := newModelTestApp(t,
		&accounts.Record{ID: "acct-a", AccountID: "upstream-a", Token: accounts.OAuthToken{AccessToken: "a", ExpiresAt: now.Add(time.Hour)}},
		&accounts.Record{ID: "acct-b", AccountID: "upstream-b", Token: accounts.OAuthToken{AccessToken: "b", ExpiresAt: now.Add(time.Hour)}},
	)
	var calls []string
	var captured codex.Request
	app.httpStream = func(_ context.Context, account accounts.Record, request codex.Request, _ string) (eventStream, error) {
		calls = append(calls, account.ID)
		captured = request
		return &fakeEventStream{events: []*codex.StreamEvent{
			{Type: "response.output_text.delta", Raw: map[string]any{"delta": "OK"}},
			{Type: "response.completed", Raw: map[string]any{"type": "response.completed", "response": map[string]any{
				"id": "resp_raw_1", "model": "gpt-raw-exact", "status": "completed", "output_text": "OK",
				"untranslated_field": "preserved",
			}}},
		}}, nil
	}
	ctx, recorder := modelTestRequestContext("{\"account_id\":\"acct-b\",\"model\":\"gpt-requested\"}")
	app.handleAdminModelTest(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if len(calls) != 1 || calls[0] != "acct-b" {
		t.Fatalf("calls = %#v, want only acct-b", calls)
	}
	if !fixedModelTestRequest(captured) {
		t.Fatalf("request = %#v, want fixed minimal model-test request", captured)
	}
	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["account_id"] != "acct-b" || response["requested_model"] != "gpt-requested" ||
		response["upstream_model"] != "gpt-raw-exact" || response["match"] != false || response["response_id"] != "resp_raw_1" {
		t.Fatalf("response = %#v, want strict raw model mismatch", response)
	}
	raw, _ := response["raw_event"].(map[string]any)
	if raw["type"] != "response.completed" {
		t.Fatalf("raw event = %#v, want original response.completed", raw)
	}
}

func TestAdminModelTestDoesNotFallbackWhenSelectedAccountFails(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	app := newModelTestApp(t,
		&accounts.Record{ID: "acct-a", AccountID: "upstream-a", Token: accounts.OAuthToken{AccessToken: "a", ExpiresAt: now.Add(time.Hour)}},
		&accounts.Record{ID: "acct-b", AccountID: "upstream-b", Token: accounts.OAuthToken{AccessToken: "b", ExpiresAt: now.Add(time.Hour)}},
	)
	var calls []string
	app.httpStream = func(_ context.Context, account accounts.Record, _ codex.Request, _ string) (eventStream, error) {
		calls = append(calls, account.ID)
		return nil, errors.New("selected account failed")
	}
	ctx, recorder := modelTestRequestContext("{\"account_id\":\"acct-a\",\"model\":\"gpt-test\"}")
	app.handleAdminModelTest(ctx)
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", recorder.Code)
	}
	if len(calls) != 1 || calls[0] != "acct-a" {
		t.Fatalf("calls = %#v, want only acct-a", calls)
	}
	if strings.Contains(recorder.Body.String(), "acct-b") {
		t.Fatalf("failure response mentioned another account: %s", recorder.Body.String())
	}
}

func TestAdminAccountModelsAreFetchedForRequestedAccount(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	app := newModelTestApp(t,
		&accounts.Record{ID: "acct-a", AccountID: "upstream-a", Token: accounts.OAuthToken{AccessToken: "a", ExpiresAt: now.Add(time.Hour)}},
		&accounts.Record{ID: "acct-b", AccountID: "upstream-b", Token: accounts.OAuthToken{AccessToken: "b", ExpiresAt: now.Add(time.Hour)}},
	)
	app.fetchModels = func(_ context.Context, account accounts.Record) ([]codex.BackendModelEntry, error) {
		if account.ID == "acct-a" {
			return []codex.BackendModelEntry{{Slug: "model-a", DisplayName: "A"}}, nil
		}
		return []codex.BackendModelEntry{{Slug: "model-b", DisplayName: "B"}}, nil
	}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/admin/accounts/acct-b/models", nil)
	ctx.Params = gin.Params{{Key: "account_id", Value: "acct-b"}}
	app.handleAdminAccountModels(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	modelsValue, _ := response["models"].([]any)
	modelObject, _ := modelsValue[0].(map[string]any)
	if response["account_id"] != "acct-b" || len(modelsValue) != 1 || modelObject["id"] != "model-b" {
		t.Fatalf("response = %#v, want only acct-b model", response)
	}
}

func TestStrictModelMatchDoesNotNormalizeAliases(t *testing.T) {
	if strictModelMatch("gpt-5.6-sol", "gpt-5.6-sol-preview") {
		t.Fatal("strictModelMatch returned true for different strings")
	}
	if !strictModelMatch("gpt-5.6-sol", "gpt-5.6-sol") {
		t.Fatal("strictModelMatch returned false for identical strings")
	}
}

func TestAdminAccountDeleteRemovesAccountFromList(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	app := newModelTestApp(t, &accounts.Record{ID: "acct-logout", AccountID: "upstream-logout", Token: accounts.OAuthToken{AccessToken: "secret", ExpiresAt: now.Add(time.Hour)}})
	deleteRecorder := httptest.NewRecorder()
	deleteCtx, _ := gin.CreateTestContext(deleteRecorder)
	deleteCtx.Request = httptest.NewRequest(http.MethodDelete, "/admin/accounts/acct-logout", nil)
	deleteCtx.Params = gin.Params{{Key: "account_id", Value: "acct-logout"}}
	app.handleAdminAccountDelete(deleteCtx)
	if deleteCtx.Writer.Status() != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", deleteCtx.Writer.Status())
	}
	listRecorder := httptest.NewRecorder()
	listCtx, _ := gin.CreateTestContext(listRecorder)
	listCtx.Request = httptest.NewRequest(http.MethodGet, "/admin/accounts", nil)
	app.handleAdminAccounts(listCtx)
	if listRecorder.Code != http.StatusOK || strings.Contains(listRecorder.Body.String(), "acct-logout") {
		t.Fatalf("account still listed: status=%d body=%s", listRecorder.Code, listRecorder.Body.String())
	}
}

func TestAdminAccountLogoutRevokesBeforeRemovingAccount(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	token := accounts.OAuthToken{
		AccessToken:  "access-secret",
		RefreshToken: "refresh-secret",
		ExpiresAt:    now.Add(time.Hour),
	}
	app := newModelTestApp(t, &accounts.Record{
		ID: "acct-logout", AccountID: "upstream-logout", Token: token,
	})
	var revoked accounts.OAuthToken
	app.revokeOAuth = func(_ context.Context, got accounts.OAuthToken) error {
		if _, ok, err := app.accounts.Get("acct-logout"); err != nil || !ok {
			t.Fatalf("account was removed before revocation: ok=%v err=%v", ok, err)
		}
		revoked = got
		return nil
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/admin/accounts/acct-logout/logout", nil)
	ctx.Params = gin.Params{{Key: "account_id", Value: "acct-logout"}}
	app.handleAdminAccountLogout(ctx)

	if ctx.Writer.Status() != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", ctx.Writer.Status(), recorder.Body.String())
	}
	if revoked.AccessToken != token.AccessToken || revoked.RefreshToken != token.RefreshToken {
		t.Fatalf("revoked token = %#v, want stored OAuth token", revoked)
	}
	if _, ok, err := app.accounts.Get("acct-logout"); err != nil || ok {
		t.Fatalf("account remains after logout: ok=%v err=%v", ok, err)
	}
	listRecorder := httptest.NewRecorder()
	listCtx, _ := gin.CreateTestContext(listRecorder)
	listCtx.Request = httptest.NewRequest(http.MethodGet, "/admin/accounts", nil)
	app.handleAdminAccounts(listCtx)
	if listRecorder.Code != http.StatusOK || strings.Contains(listRecorder.Body.String(), "acct-logout") {
		t.Fatalf("logged-out account is still listed: status=%d body=%s", listRecorder.Code, listRecorder.Body.String())
	}
}

func TestAdminAccountLogoutFailureKeepsLocalCredentials(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	app := newModelTestApp(t, &accounts.Record{
		ID: "acct-logout", AccountID: "upstream-logout",
		Token: accounts.OAuthToken{
			AccessToken: "access-secret", RefreshToken: "refresh-secret", ExpiresAt: now.Add(time.Hour),
		},
	})
	app.revokeOAuth = func(context.Context, accounts.OAuthToken) error {
		return errors.New("revocation unavailable")
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/admin/accounts/acct-logout/logout", nil)
	ctx.Params = gin.Params{{Key: "account_id", Value: "acct-logout"}}
	app.handleAdminAccountLogout(ctx)

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502: %s", recorder.Code, recorder.Body.String())
	}
	if _, ok, err := app.accounts.Get("acct-logout"); err != nil || !ok {
		t.Fatalf("account was removed after failed revocation: ok=%v err=%v", ok, err)
	}
	for _, secret := range []string{"access-secret", "refresh-secret"} {
		if strings.Contains(recorder.Body.String(), secret) {
			t.Fatalf("logout failure exposed OAuth token: %s", recorder.Body.String())
		}
	}
}

func TestAdminModelTestClassifiesUpstreamFailures(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		code      string
		status    int
		eventCode string
	}{
		{name: "rate limited", code: "rate_limited", status: http.StatusTooManyRequests, eventCode: "rate_limit_exceeded"},
		{name: "quota exhausted", code: "quota_exhausted", status: http.StatusPaymentRequired, eventCode: "usage_limit_reached"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			now := time.Now().UTC()
			app := newModelTestApp(t, &accounts.Record{
				ID: "acct-error", AccountID: "upstream-error",
				Token: accounts.OAuthToken{AccessToken: "token", ExpiresAt: now.Add(time.Hour)},
			})
			app.httpStream = func(_ context.Context, _ accounts.Record, _ codex.Request, _ string) (eventStream, error) {
				return &fakeEventStream{events: []*codex.StreamEvent{{
					Type: "response.failed",
					Raw: map[string]any{"type": "response.failed", "response": map[string]any{
						"error": map[string]any{"code": tc.eventCode, "message": "quota failure", "resets_in_seconds": 30},
					}},
				}}}, nil
			}
			ctx, recorder := modelTestRequestContext(`{"account_id":"acct-error","model":"gpt-test"}`)
			app.handleAdminModelTest(ctx)
			if recorder.Code != tc.status {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, tc.status, recorder.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body["error"] != tc.code {
				t.Fatalf("error = %#v, want %q", body["error"], tc.code)
			}
			if body["retry_after_seconds"] != float64(30) {
				t.Fatalf("retry_after_seconds = %#v, want 30", body["retry_after_seconds"])
			}
		})
	}
}

func TestAdminModelTestRemovesUnauthorizedAccount(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	app := newModelTestApp(t, &accounts.Record{
		ID: "acct-expired", AccountID: "upstream-expired",
		Token: accounts.OAuthToken{AccessToken: "expired", ExpiresAt: now.Add(time.Hour)},
	})
	app.httpStream = func(_ context.Context, _ accounts.Record, _ codex.Request, _ string) (eventStream, error) {
		return nil, &codex.UpstreamError{
			Op:         "codex stream",
			StatusCode: http.StatusUnauthorized,
			Body:       "{\"error\":{\"code\":\"token_revoked\",\"message\":\"token expired\"}}",
		}
	}

	ctx, recorder := modelTestRequestContext("{\"account_id\":\"acct-expired\",\"model\":\"gpt-test\"}")
	app.handleAdminModelTest(ctx)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %s", recorder.Code, recorder.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["error"] != "upstream_unauthorized" || body["account_removed"] != true {
		t.Fatalf("body = %#v, want unauthorized account_removed response", body)
	}
	if _, ok, err := app.accounts.Get("acct-expired"); err != nil || ok {
		t.Fatalf("expired account still exists: ok=%v err=%v", ok, err)
	}
}

func TestAdminAccountModelsRemovesUnauthorizedAccount(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	app := newModelTestApp(t, &accounts.Record{
		ID: "acct-model-expired", AccountID: "upstream-model-expired",
		Token: accounts.OAuthToken{AccessToken: "expired", ExpiresAt: now.Add(time.Hour)},
	})
	app.fetchModels = func(_ context.Context, _ accounts.Record) ([]codex.BackendModelEntry, error) {
		return nil, &codex.UpstreamError{
			Op:         "codex models",
			StatusCode: http.StatusUnauthorized,
			Body:       "{\"error\":{\"code\":\"token_revoked\",\"message\":\"token expired\"}}",
		}
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/admin/accounts/acct-model-expired/models", nil)
	ctx.Params = gin.Params{{Key: "account_id", Value: "acct-model-expired"}}
	app.handleAdminAccountModels(ctx)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %s", recorder.Code, recorder.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["error"] != "upstream_unauthorized" || body["account_removed"] != true {
		t.Fatalf("body = %#v, want unauthorized account_removed response", body)
	}
	if _, ok, err := app.accounts.Get("acct-model-expired"); err != nil || ok {
		t.Fatalf("expired model account still exists: ok=%v err=%v", ok, err)
	}
}
