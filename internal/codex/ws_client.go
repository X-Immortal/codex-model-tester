package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/gorilla/websocket"

	"chatgpt-codex-proxy/internal/httpbody"
)

// WSStream wraps a websocket connection and exposes the event-stream interface used by the server package.
type WSStream struct {
	conn    *websocket.Conn
	headers http.Header
}

func ConnectWS(ctx context.Context, endpoint string, headers http.Header, body any) (*WSStream, error) {
	return ConnectWSWithProxy(ctx, endpoint, headers, body, "")
}

func ConnectWSWithProxy(ctx context.Context, endpoint string, headers http.Header, body any, proxyURL string) (*WSStream, error) {
	dialer := websocket.Dialer{}
	if proxyURL != "" {
		parsed, err := url.Parse(proxyURL)
		if err != nil {
			return nil, fmt.Errorf("parse upstream proxy: %w", err)
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return nil, fmt.Errorf("websocket proxy scheme %q is not supported; use an HTTP proxy", parsed.Scheme)
		}
		dialer.Proxy = http.ProxyURL(parsed)
	}
	conn, resp, err := dialer.DialContext(ctx, endpoint, headers)
	if err != nil {
		if resp != nil {
			payload := httpbody.ReadLimitedErrorBody(resp.Body)
			resp.Body.Close()
			return nil, NewUpstreamError("websocket dial", resp.StatusCode, payload, resp.Header)
		}
		return nil, err
	}
	if err := conn.WriteJSON(body); err != nil {
		conn.Close()
		return nil, err
	}
	return &WSStream{
		conn:    conn,
		headers: resp.Header.Clone(),
	}, nil
}

func (s *WSStream) Close() error {
	return s.conn.Close()
}

func (s *WSStream) Headers() http.Header {
	return s.headers.Clone()
}

func (s *WSStream) SendJSON(body any) error {
	return s.conn.WriteJSON(body)
}

func (s *WSStream) NextEvent() (*StreamEvent, error) {
	_, message, err := s.conn.ReadMessage()
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := json.Unmarshal(message, &raw); err != nil {
		return nil, err
	}
	eventType, _ := raw["type"].(string)
	if strings.TrimSpace(eventType) == "" {
		return nil, io.EOF
	}
	return &StreamEvent{Type: eventType, Raw: raw}, nil
}
