package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"chatgpt-codex-proxy/internal/accounts"
	"chatgpt-codex-proxy/internal/codex"
)

func TestAdminNotificationTest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var gotTitle, gotMessage string
	app := &App{notifyHeartbeat: func(title, message string) error {
		gotTitle = title
		gotMessage = message
		return nil
	}}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/admin/notifications/test", nil)

	app.handleAdminNotificationTest(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if gotTitle != "Codex 模型监测" || gotMessage == "" {
		t.Fatalf("notification = %q / %q", gotTitle, gotMessage)
	}
}

func TestHeartbeatManagerPersistsAndNotifiesOnlyOnStateChanges(t *testing.T) {
	now := time.Now().UTC()
	manager, err := newHeartbeatManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	monitor, err := manager.upsert("acct-a", "gpt-5.6-sol", int64(time.Hour/time.Second), now)
	if err != nil {
		t.Fatal(err)
	}

	updated, notify, err := manager.complete(monitor.ID, heartbeatOutcome{Status: heartbeatError, ErrorCode: "upstream_error", Error: "failed"}, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !notify || updated.LastStatus != heartbeatError || updated.ConsecutiveFailures != 1 {
		t.Fatalf("first failure = %#v notify=%v", updated, notify)
	}
	updated, notify, err = manager.complete(monitor.ID, heartbeatOutcome{Status: heartbeatError, ErrorCode: "upstream_error", Error: "failed again"}, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if notify || updated.ConsecutiveFailures != 2 {
		t.Fatalf("repeated failure = %#v notify=%v", updated, notify)
	}
	updated, notify, err = manager.complete(monitor.ID, heartbeatOutcome{Status: heartbeatError, ErrorCode: "rate_limited", Error: "limited"}, now.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !notify || updated.ConsecutiveFailures != 3 {
		t.Fatalf("changed failure = %#v notify=%v", updated, notify)
	}
	updated, notify, err = manager.complete(monitor.ID, heartbeatOutcome{Status: heartbeatHealthy, UpstreamModel: "gpt-5.6-sol"}, now.Add(4*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !notify || updated.LastStatus != heartbeatHealthy || updated.ConsecutiveFailures != 0 {
		t.Fatalf("recovery = %#v notify=%v", updated, notify)
	}

	reloaded, err := newHeartbeatManager(manager.path[:len(manager.path)-len("heartbeats.json")])
	if err != nil {
		t.Fatal(err)
	}
	items := reloaded.list()
	if len(items) != 1 || items[0].LastStatus != heartbeatHealthy || items[0].Model != "gpt-5.6-sol" {
		t.Fatalf("reloaded heartbeats = %#v", items)
	}
}

func TestHeartbeatUsesOnlyConfiguredAccountAndStrictRawModel(t *testing.T) {
	now := time.Now().UTC()
	app := newModelTestApp(t,
		&accounts.Record{ID: "acct-a", AccountID: "upstream-a", Token: accounts.OAuthToken{AccessToken: "a", ExpiresAt: now.Add(time.Hour)}},
		&accounts.Record{ID: "acct-b", AccountID: "upstream-b", Email: "b@example.test", Token: accounts.OAuthToken{AccessToken: "b", ExpiresAt: now.Add(time.Hour)}},
	)
	manager, err := newHeartbeatManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app.heartbeats = manager
	var calls []string
	app.httpStream = func(_ context.Context, account accounts.Record, request codex.Request, _ string) (eventStream, error) {
		calls = append(calls, account.ID)
		if len(request.Input) != 1 || len(request.Input[0].Content) != 1 {
			t.Fatalf("heartbeat request = %#v, want one text input", request)
		}
		prompt := request.Input[0].Content[0].Text
		if !modelProbeRequest(request, prompt) || !isHeartbeatPrompt(prompt) {
			t.Fatalf("heartbeat request = %#v, want a minimal request using the random prompt pool", request)
		}
		return &fakeEventStream{events: []*codex.StreamEvent{{Type: "response.completed", Raw: map[string]any{
			"type":     "response.completed",
			"response": map[string]any{"id": "resp-heartbeat", "model": "gpt-upstream-different", "status": "completed"},
		}}}}, nil
	}
	monitor, err := manager.upsert("acct-b", "gpt-5.6-sol", int64(time.Hour/time.Second), now)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := manager.claim(monitor.ID, true, now)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := app.executeHeartbeat(context.Background(), claimed)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0] != "acct-b" {
		t.Fatalf("calls = %#v, want only acct-b", calls)
	}
	if updated.LastStatus != heartbeatMismatch || updated.LastUpstreamModel != "gpt-upstream-different" {
		t.Fatalf("heartbeat result = %#v, want strict mismatch", updated)
	}
}

func TestHeartbeatFailureDoesNotFallbackAndNotificationIsDeduplicated(t *testing.T) {
	now := time.Now().UTC()
	app := newModelTestApp(t,
		&accounts.Record{ID: "acct-a", AccountID: "upstream-a", Token: accounts.OAuthToken{AccessToken: "a", ExpiresAt: now.Add(time.Hour)}},
		&accounts.Record{ID: "acct-b", AccountID: "upstream-b", Email: "b@example.test", Token: accounts.OAuthToken{AccessToken: "b", ExpiresAt: now.Add(time.Hour)}},
	)
	manager, err := newHeartbeatManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app.heartbeats = manager
	var calls []string
	var notifications []string
	app.notifyHeartbeat = func(title, message string) error {
		notifications = append(notifications, title+"\n"+message)
		return nil
	}
	app.httpStream = func(_ context.Context, account accounts.Record, _ codex.Request, _ string) (eventStream, error) {
		calls = append(calls, account.ID)
		return nil, errors.New("selected account failed")
	}
	monitor, err := manager.upsert("acct-b", "gpt-5.6-sol", int64(time.Hour/time.Second), now)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		claimed, err := manager.claim(monitor.ID, true, now)
		if err != nil {
			t.Fatal(err)
		}
		updated, err := app.executeHeartbeat(context.Background(), claimed)
		if err != nil {
			t.Fatal(err)
		}
		if updated.LastStatus != heartbeatError {
			t.Fatalf("status = %q, want error", updated.LastStatus)
		}
	}
	if len(calls) != 2 || calls[0] != "acct-b" || calls[1] != "acct-b" {
		t.Fatalf("calls = %#v, want only acct-b", calls)
	}
	if len(notifications) != 1 {
		t.Fatalf("notifications = %#v, want one deduplicated alert", notifications)
	}
}

func TestHeartbeatSupportsShortIntervals(t *testing.T) {
	for _, interval := range []time.Duration{time.Minute, 5 * time.Minute, 10 * time.Minute, 15 * time.Minute, 30 * time.Minute} {
		if !validHeartbeatInterval(int64(interval / time.Second)) {
			t.Fatalf("interval %s should be supported", interval)
		}
	}
}

func TestHeartbeatRejectsUnsupportedInterval(t *testing.T) {
	manager, err := newHeartbeatManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.upsert("acct-a", "gpt-5.6-sol", int64(2*time.Minute/time.Second), time.Now().UTC()); err == nil {
		t.Fatal("upsert error = nil, want unsupported interval")
	}
}

func TestHeartbeatPromptPoolIsLargeAndUnique(t *testing.T) {
	if len(heartbeatPromptTemplates) < 32 {
		t.Fatalf("prompt count = %d, want at least 32", len(heartbeatPromptTemplates))
	}
	seen := make(map[string]struct{}, len(heartbeatPromptTemplates))
	for _, prompt := range heartbeatPromptTemplates {
		if prompt == "" {
			t.Fatal("heartbeat prompt must not be empty")
		}
		if _, ok := seen[prompt]; ok {
			t.Fatalf("duplicate heartbeat prompt %q", prompt)
		}
		seen[prompt] = struct{}{}
	}
	for i := 0; i < 100; i++ {
		if prompt := randomHeartbeatPrompt(); !isHeartbeatPrompt(prompt) {
			t.Fatalf("random prompt %q is not in the prompt pool", prompt)
		}
	}
}

func TestRandomizedHeartbeatDelayStaysWithinConfiguredRange(t *testing.T) {
	for baseSeconds := range allowedHeartbeatIntervals {
		minimumSeconds, maximumSeconds := heartbeatDelayBounds(baseSeconds)
		minimum := time.Duration(minimumSeconds) * time.Second
		maximum := time.Duration(maximumSeconds) * time.Second
		for i := 0; i < 100; i++ {
			delay := randomizedHeartbeatDelay(baseSeconds)
			if delay < minimum || delay > maximum {
				t.Fatalf("delay %s for base %s is outside [%s, %s]", delay, time.Duration(baseSeconds)*time.Second, minimum, maximum)
			}
		}
	}
}

func TestHeartbeatShortIntervalRanges(t *testing.T) {
	tests := []struct {
		base time.Duration
		min  time.Duration
		max  time.Duration
	}{
		{base: time.Minute, min: time.Minute, max: 10 * time.Minute},
		{base: 5 * time.Minute, min: 5 * time.Minute, max: 15 * time.Minute},
		{base: 10 * time.Minute, min: 10 * time.Minute, max: 30 * time.Minute},
		{base: 15 * time.Minute, min: 15 * time.Minute, max: 45 * time.Minute},
		{base: 30 * time.Minute, min: 30 * time.Minute, max: 90 * time.Minute},
	}
	for _, test := range tests {
		minimum, maximum := heartbeatDelayBounds(int64(test.base / time.Second))
		if got := time.Duration(minimum) * time.Second; got != test.min {
			t.Fatalf("minimum for %s = %s, want %s", test.base, got, test.min)
		}
		if got := time.Duration(maximum) * time.Second; got != test.max {
			t.Fatalf("maximum for %s = %s, want %s", test.base, got, test.max)
		}
	}
}
