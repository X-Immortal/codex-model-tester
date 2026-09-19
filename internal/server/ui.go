package server

import (
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/base64"
	"net"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"chatgpt-codex-proxy/internal/middleware"
)

const adminUISessionCookieName = "codex_admin_session"

var (
	//go:embed web/index.html
	adminUIIndex []byte
	//go:embed web/app.js
	adminUIAppJS []byte
	//go:embed web/style.css
	adminUIStyleCSS []byte
	//go:embed web/openai-logomark.svg
	adminUIOpenAILogomark []byte
	//go:embed web/character-bg-calm.jpg
	adminUICharacterBGCalm []byte
)

func newAdminUISessionToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func isLoopbackRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		host = strings.Trim(strings.TrimSpace(r.RemoteAddr), "[]")
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (a *App) hasValidAdminUISession(r *http.Request) bool {
	if a.adminUISession == "" || !isLoopbackRequest(r) {
		return false
	}
	cookie, err := r.Cookie(adminUISessionCookieName)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(a.adminUISession)) == 1
}

func (a *App) adminUIAuthentication() gin.HandlerFunc {
	apiKeyAuth := middleware.APIKeyWithUnauthorized(a.cfg.ProxyAPIKey, func(c *gin.Context) {
		middleware.SetRequestError(c, "admin_session_required", "admin UI session expired")
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
			"error":   "admin_session_required",
			"message": "管理会话已失效，请刷新页面。",
		})
	})
	return func(c *gin.Context) {
		if a.hasValidAdminUISession(c.Request) {
			c.Next()
			return
		}
		apiKeyAuth(c)
	}
}

func (a *App) handleAdminUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	switch r.URL.Path {
	case "/", "/admin/ui":
		if !isLoopbackRequest(r) {
			http.Error(w, "admin UI is only available on localhost", http.StatusForbidden)
			return
		}
		if a.adminUISession == "" {
			http.Error(w, "admin UI session is unavailable", http.StatusServiceUnavailable)
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     adminUISessionCookieName,
			Value:    a.adminUISession,
			Path:     "/admin",
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
		})
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(adminUIIndex)
	case "/admin-ui/app.js":
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = w.Write(adminUIAppJS)
	case "/admin-ui/style.css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		_, _ = w.Write(adminUIStyleCSS)
	case "/admin-ui/openai-logomark.svg":
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Header().Set("Content-Type", "image/svg+xml")
		_, _ = w.Write(adminUIOpenAILogomark)
	case "/admin-ui/character-bg-calm.jpg":
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(adminUICharacterBGCalm)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}
