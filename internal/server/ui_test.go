package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"chatgpt-codex-proxy/internal/config"
)

func TestAdminUILocalSession(t *testing.T) {
	app := &App{adminUISession: "ui-session-secret"}
	req := httptest.NewRequest(http.MethodGet, "http://localhost/", nil)
	req.RemoteAddr = "127.0.0.1:41000"
	recorder := httptest.NewRecorder()

	app.handleAdminUI(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if strings.Contains(recorder.Body.String(), "api-key") || strings.Contains(recorder.Body.String(), "代理 API Key") {
		t.Fatal("admin UI still contains an API key input")
	}
	response := recorder.Result()
	var session *http.Cookie
	for _, cookie := range response.Cookies() {
		if cookie.Name == adminUISessionCookieName {
			session = cookie
			break
		}
	}
	if session == nil {
		t.Fatal("admin UI did not issue a session cookie")
	}
	if session.Value != "ui-session-secret" || session.Path != "/admin" || !session.HttpOnly || session.SameSite != http.SameSiteStrictMode {
		t.Fatalf("unexpected admin session cookie: %#v", session)
	}
}

func TestAdminUIRejectsRemotePage(t *testing.T) {
	app := &App{adminUISession: "ui-session-secret"}
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	req.RemoteAddr = "203.0.113.10:41000"
	recorder := httptest.NewRecorder()

	app.handleAdminUI(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
}

func TestAdminUIEmbeddedCharacterBackground(t *testing.T) {
	app := &App{}
	req := httptest.NewRequest(http.MethodGet, "http://localhost/admin-ui/character-bg-calm.jpg", nil)
	recorder := httptest.NewRecorder()

	app.handleAdminUI(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Header().Get("Content-Type"); got != "image/jpeg" {
		t.Fatalf("content type = %q, want image/jpeg", got)
	}
	if recorder.Body.Len() == 0 {
		t.Fatal("embedded character background is empty")
	}
}

func TestAdminUIEmbeddedOpenAILogomark(t *testing.T) {
	app := &App{}
	req := httptest.NewRequest(http.MethodGet, "http://localhost/admin-ui/openai-logomark.svg", nil)
	recorder := httptest.NewRecorder()

	app.handleAdminUI(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Header().Get("Content-Type"); got != "image/svg+xml" {
		t.Fatalf("content type = %q, want image/svg+xml", got)
	}
	if !strings.Contains(recorder.Body.String(), "<svg") {
		t.Fatal("embedded OpenAI logomark is not an SVG")
	}
}

func TestAdminAuthenticationAcceptsLocalSessionOrProxyKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := &App{
		cfg:            config.Config{ProxyAPIKey: "proxy-secret"},
		adminUISession: "ui-session-secret",
	}
	router := gin.New()
	router.Use(app.adminUIAuthentication())
	router.GET("/admin/ping", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	tests := []struct {
		name       string
		remoteAddr string
		cookie     string
		apiKey     string
		want       int
	}{
		{name: "local UI session", remoteAddr: "127.0.0.1:41000", cookie: "ui-session-secret", want: http.StatusNoContent},
		{name: "IPv6 local UI session", remoteAddr: "[::1]:41000", cookie: "ui-session-secret", want: http.StatusNoContent},
		{name: "remote cookie rejected", remoteAddr: "203.0.113.10:41000", cookie: "ui-session-secret", want: http.StatusUnauthorized},
		{name: "proxy API key remains supported", remoteAddr: "203.0.113.10:41000", apiKey: "proxy-secret", want: http.StatusNoContent},
		{name: "missing credentials", remoteAddr: "127.0.0.1:41000", want: http.StatusUnauthorized},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/admin/ping", nil)
			req.RemoteAddr = tc.remoteAddr
			if tc.cookie != "" {
				req.AddCookie(&http.Cookie{Name: adminUISessionCookieName, Value: tc.cookie})
			}
			if tc.apiKey != "" {
				req.Header.Set("X-API-Key", tc.apiKey)
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)
			if recorder.Code != tc.want {
				t.Fatalf("status = %d, want %d", recorder.Code, tc.want)
			}
		})
	}
}
