package server

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"chatgpt-codex-proxy/internal/accounts"
)

const (
	defaultHeartbeatSeconds = int64(6 * time.Hour / time.Second)
	heartbeatHealthy        = "healthy"
	heartbeatMismatch       = "mismatch"
	heartbeatError          = "error"
)

var (
	errHeartbeatNotFound      = errors.New("heartbeat not found")
	errHeartbeatRunning       = errors.New("heartbeat is already running")
	allowedHeartbeatIntervals = map[int64]struct{}{
		int64(time.Minute / time.Second):      {},
		int64(5 * time.Minute / time.Second):  {},
		int64(10 * time.Minute / time.Second): {},
		int64(15 * time.Minute / time.Second): {},
		int64(30 * time.Minute / time.Second): {},
		int64(time.Hour / time.Second):        {},
		int64(3 * time.Hour / time.Second):    {},
		int64(6 * time.Hour / time.Second):    {},
		int64(12 * time.Hour / time.Second):   {},
		int64(24 * time.Hour / time.Second):   {},
	}
	heartbeatPromptTemplates = []string{
		"Reply with exactly: OK",
		"Respond with exactly: OK",
		"Return exactly: OK",
		"Output exactly: OK",
		"Answer with exactly: OK",
		"Write exactly: OK",
		"Say exactly: OK",
		"Emit exactly: OK",
		"Reply only with: OK",
		"Respond only with: OK",
		"Return only: OK",
		"Output only: OK",
		"Answer only: OK",
		"Write only: OK",
		"Say only: OK",
		"Emit only: OK",
		"Your entire reply must be: OK",
		"Your entire response must be: OK",
		"Use only the uppercase text: OK",
		"Send only the uppercase text: OK",
		"Provide exactly the text: OK",
		"Produce exactly the text: OK",
		"No explanation; reply exactly: OK",
		"No punctuation; reply exactly: OK",
		"Do not add anything after: OK",
		"Do not add anything before: OK",
		"Confirm availability by replying exactly: OK",
		"Confirm this check with exactly: OK",
		"For this health check, reply exactly: OK",
		"For this model check, respond exactly: OK",
		"Complete this probe with exactly: OK",
		"Acknowledge this request with exactly: OK",
		"只回复两个大写字母：OK",
		"请准确回复：OK",
		"不要解释，只回复：OK",
		"本次检测仅输出：OK",
		"请用大写字母回复：OK",
		"确认服务可用，只回复：OK",
		"完成本次模型检测，仅回复：OK",
		"除 OK 外不要输出任何内容。",
	}
)

type heartbeatMonitor struct {
	ID                  string     `json:"id"`
	AccountID           string     `json:"account_id"`
	Model               string     `json:"model"`
	IntervalSeconds     int64      `json:"interval_seconds"`
	Enabled             bool       `json:"enabled"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
	NextRunAt           time.Time  `json:"next_run_at"`
	LastRunAt           *time.Time `json:"last_run_at,omitempty"`
	LastStatus          string     `json:"last_status,omitempty"`
	LastUpstreamModel   string     `json:"last_upstream_model,omitempty"`
	LastErrorCode       string     `json:"last_error_code,omitempty"`
	LastError           string     `json:"last_error,omitempty"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
}

type heartbeatDiskState struct {
	Monitors []heartbeatMonitor `json:"monitors"`
}

type heartbeatOutcome struct {
	Status        string
	UpstreamModel string
	ErrorCode     string
	Error         string
}

type heartbeatManager struct {
	mu       sync.Mutex
	path     string
	monitors map[string]heartbeatMonitor
	running  map[string]bool
}

func newHeartbeatManager(dataDir string) (*heartbeatManager, error) {
	manager := &heartbeatManager{
		path:     filepath.Join(dataDir, "heartbeats.json"),
		monitors: make(map[string]heartbeatMonitor),
		running:  make(map[string]bool),
	}
	raw, err := os.ReadFile(manager.path)
	if err != nil {
		if os.IsNotExist(err) {
			return manager, nil
		}
		return nil, fmt.Errorf("read heartbeat store: %w", err)
	}
	var state heartbeatDiskState
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, fmt.Errorf("decode heartbeat store: %w", err)
	}
	now := time.Now().UTC()
	for _, monitor := range state.Monitors {
		if strings.TrimSpace(monitor.ID) == "" || strings.TrimSpace(monitor.AccountID) == "" || strings.TrimSpace(monitor.Model) == "" {
			continue
		}
		if !validHeartbeatInterval(monitor.IntervalSeconds) {
			monitor.IntervalSeconds = defaultHeartbeatSeconds
		}
		if monitor.NextRunAt.IsZero() {
			monitor.NextRunAt = now.Add(randomizedHeartbeatDelay(monitor.IntervalSeconds))
		}
		manager.monitors[monitor.ID] = monitor
	}
	return manager, nil
}

func validHeartbeatInterval(seconds int64) bool {
	_, ok := allowedHeartbeatIntervals[seconds]
	return ok
}

func heartbeatMonitorID(accountID, model string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(accountID) + "\x00" + strings.TrimSpace(model)))
	return "hb_" + hex.EncodeToString(digest[:8])
}

func randomHeartbeatPrompt() string {
	return heartbeatPromptTemplates[randomHeartbeatIndex(len(heartbeatPromptTemplates))]
}

func isHeartbeatPrompt(prompt string) bool {
	for _, candidate := range heartbeatPromptTemplates {
		if prompt == candidate {
			return true
		}
	}
	return false
}

func randomizedHeartbeatDelay(baseSeconds int64) time.Duration {
	minimum, maximum := heartbeatDelayBounds(baseSeconds)
	span := maximum - minimum + 1
	seconds := minimum + int64(randomHeartbeatIndex(int(span)))
	return time.Duration(seconds) * time.Second
}

func heartbeatDelayBounds(baseSeconds int64) (int64, int64) {
	switch {
	case baseSeconds <= int64(time.Minute/time.Second):
		return baseSeconds, int64(10 * time.Minute / time.Second)
	case baseSeconds <= int64(time.Hour/time.Second):
		return baseSeconds, baseSeconds * 3
	default:
		return baseSeconds, baseSeconds * 2
	}
}

func randomHeartbeatIndex(size int) int {
	if size <= 1 {
		return 0
	}
	value, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(size)))
	if err == nil {
		return int(value.Int64())
	}
	return int(time.Now().UnixNano() % int64(size))
}

func (m *heartbeatManager) list() []heartbeatMonitor {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := make([]heartbeatMonitor, 0, len(m.monitors))
	for _, monitor := range m.monitors {
		items = append(items, monitor)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	return items
}

func (m *heartbeatManager) upsert(accountID, model string, intervalSeconds int64, now time.Time) (heartbeatMonitor, error) {
	if !validHeartbeatInterval(intervalSeconds) {
		return heartbeatMonitor{}, fmt.Errorf("unsupported heartbeat interval")
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	id := heartbeatMonitorID(accountID, model)
	monitor, exists := m.monitors[id]
	if !exists {
		monitor = heartbeatMonitor{ID: id, AccountID: accountID, Model: model, CreatedAt: now}
	}
	monitor.IntervalSeconds = intervalSeconds
	monitor.Enabled = true
	monitor.UpdatedAt = now
	monitor.NextRunAt = now.Add(randomizedHeartbeatDelay(intervalSeconds))
	m.monitors[id] = monitor
	if err := m.saveLocked(); err != nil {
		return heartbeatMonitor{}, err
	}
	return monitor, nil
}

func (m *heartbeatManager) remove(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.monitors[id]; !ok {
		return errHeartbeatNotFound
	}
	delete(m.monitors, id)
	delete(m.running, id)
	return m.saveLocked()
}

func (m *heartbeatManager) removeByAccount(accountID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	changed := false
	for id, monitor := range m.monitors {
		if monitor.AccountID == accountID {
			delete(m.monitors, id)
			delete(m.running, id)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return m.saveLocked()
}

func (m *heartbeatManager) claim(id string, force bool, now time.Time) (heartbeatMonitor, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	monitor, ok := m.monitors[id]
	if !ok {
		return heartbeatMonitor{}, errHeartbeatNotFound
	}
	if m.running[id] {
		return heartbeatMonitor{}, errHeartbeatRunning
	}
	if !monitor.Enabled || (!force && now.Before(monitor.NextRunAt)) {
		return heartbeatMonitor{}, errHeartbeatNotFound
	}
	m.running[id] = true
	return monitor, nil
}

func (m *heartbeatManager) claimDue(now time.Time) []heartbeatMonitor {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := make([]heartbeatMonitor, 0)
	for id, monitor := range m.monitors {
		if monitor.Enabled && !m.running[id] && !now.Before(monitor.NextRunAt) {
			m.running[id] = true
			items = append(items, monitor)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].NextRunAt.Before(items[j].NextRunAt) })
	return items
}

func (m *heartbeatManager) release(id string) {
	m.mu.Lock()
	delete(m.running, id)
	m.mu.Unlock()
}

func (m *heartbeatManager) complete(id string, outcome heartbeatOutcome, now time.Time) (heartbeatMonitor, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.running, id)
	monitor, ok := m.monitors[id]
	if !ok {
		return heartbeatMonitor{}, false, errHeartbeatNotFound
	}
	previousStatus := monitor.LastStatus
	previousNotificationKey := heartbeatNotificationKey(monitor.LastStatus, monitor.LastErrorCode, monitor.LastUpstreamModel)
	lastRun := now
	monitor.LastRunAt = &lastRun
	monitor.LastStatus = outcome.Status
	monitor.LastUpstreamModel = outcome.UpstreamModel
	monitor.LastErrorCode = outcome.ErrorCode
	monitor.LastError = outcome.Error
	monitor.UpdatedAt = now
	monitor.NextRunAt = now.Add(randomizedHeartbeatDelay(monitor.IntervalSeconds))
	if outcome.Status == heartbeatHealthy {
		monitor.ConsecutiveFailures = 0
	} else {
		monitor.ConsecutiveFailures++
	}
	m.monitors[id] = monitor
	nextNotificationKey := heartbeatNotificationKey(outcome.Status, outcome.ErrorCode, outcome.UpstreamModel)
	shouldNotify := nextNotificationKey != previousNotificationKey && (outcome.Status != heartbeatHealthy || previousStatus != "")
	if err := m.saveLocked(); err != nil {
		return monitor, false, err
	}
	return monitor, shouldNotify, nil
}

func heartbeatNotificationKey(status, errorCode, upstreamModel string) string {
	switch status {
	case heartbeatError:
		return status + ":" + errorCode
	case heartbeatMismatch:
		return status + ":" + upstreamModel
	default:
		return status
	}
}

func (m *heartbeatManager) saveLocked() error {
	items := make([]heartbeatMonitor, 0, len(m.monitors))
	for _, monitor := range m.monitors {
		items = append(items, monitor)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	payload, err := json.MarshalIndent(heartbeatDiskState{Monitors: items}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode heartbeat store: %w", err)
	}
	payload = append(payload, '\n')
	if err := os.MkdirAll(filepath.Dir(m.path), 0o755); err != nil {
		return fmt.Errorf("create heartbeat store dir: %w", err)
	}
	tmpPath := m.path + ".tmp"
	if err := os.WriteFile(tmpPath, payload, 0o600); err != nil {
		return fmt.Errorf("write heartbeat store: %w", err)
	}
	if err := os.Rename(tmpPath, m.path); err != nil {
		return fmt.Errorf("rename heartbeat store: %w", err)
	}
	return nil
}

func (a *App) handleAdminHeartbeats(c *gin.Context) {
	if a.heartbeats == nil {
		a.writeAdminError(c, http.StatusServiceUnavailable, "heartbeats_unavailable", "heartbeat monitoring is unavailable")
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"heartbeats":   a.heartbeats.list(),
		"prompt_mode":  "random",
		"prompt_count": len(heartbeatPromptTemplates),
	})
}

func (a *App) handleAdminNotificationTest(c *gin.Context) {
	if a.notifyHeartbeat == nil {
		a.writeAdminError(c, http.StatusServiceUnavailable, "notifications_unavailable", "desktop notifications are unavailable")
		return
	}
	if err := a.notifyHeartbeat("Codex 模型监测", "这是一条系统通知测试。如果你看到了它，通知功能可以正常工作。"); err != nil {
		a.writeAdminError(c, http.StatusInternalServerError, "notification_failed", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"sent": true})
}

func (a *App) handleAdminHeartbeatUpsert(c *gin.Context) {
	if a.heartbeats == nil {
		a.writeAdminError(c, http.StatusServiceUnavailable, "heartbeats_unavailable", "heartbeat monitoring is unavailable")
		return
	}
	var request struct {
		AccountID       string `json:"account_id"`
		Model           string `json:"model"`
		IntervalSeconds int64  `json:"interval_seconds"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		a.writeAdminError(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	request.AccountID = strings.TrimSpace(request.AccountID)
	request.Model = strings.TrimSpace(request.Model)
	if request.IntervalSeconds == 0 {
		request.IntervalSeconds = defaultHeartbeatSeconds
	}
	if request.AccountID == "" || request.Model == "" || !validHeartbeatInterval(request.IntervalSeconds) {
		a.writeAdminError(c, http.StatusBadRequest, "invalid_heartbeat", "account_id, model, and a supported interval are required")
		return
	}
	record, ok, err := a.accounts.Get(request.AccountID)
	if err != nil {
		a.writeAdminError(c, http.StatusInternalServerError, "account_lookup_failed", err.Error())
		return
	}
	if !ok {
		a.writeAdminError(c, http.StatusNotFound, "account_not_found", "account not found")
		return
	}
	if record.Status != accounts.StatusActive {
		a.writeAdminError(c, http.StatusConflict, "account_not_active", "account is not active")
		return
	}
	if !a.modelCatalog().SupportsRecord(record, request.Model) {
		a.writeAdminError(c, http.StatusBadRequest, "model_not_available", "model is not available for the selected account")
		return
	}
	monitor, err := a.heartbeats.upsert(request.AccountID, request.Model, request.IntervalSeconds, time.Now().UTC())
	if err != nil {
		a.writeAdminError(c, http.StatusInternalServerError, "heartbeat_save_failed", err.Error())
		return
	}
	c.JSON(http.StatusOK, monitor)
}

func (a *App) handleAdminHeartbeatDelete(c *gin.Context) {
	if a.heartbeats == nil {
		a.writeAdminError(c, http.StatusServiceUnavailable, "heartbeats_unavailable", "heartbeat monitoring is unavailable")
		return
	}
	if err := a.heartbeats.remove(strings.TrimSpace(c.Param("heartbeat_id"))); err != nil {
		if errors.Is(err, errHeartbeatNotFound) {
			a.writeAdminError(c, http.StatusNotFound, "heartbeat_not_found", "heartbeat not found")
		} else {
			a.writeAdminError(c, http.StatusInternalServerError, "heartbeat_delete_failed", err.Error())
		}
		return
	}
	c.Status(http.StatusNoContent)
}

func (a *App) handleAdminHeartbeatRun(c *gin.Context) {
	if a.heartbeats == nil {
		a.writeAdminError(c, http.StatusServiceUnavailable, "heartbeats_unavailable", "heartbeat monitoring is unavailable")
		return
	}
	monitor, err := a.heartbeats.claim(strings.TrimSpace(c.Param("heartbeat_id")), true, time.Now().UTC())
	if err != nil {
		if errors.Is(err, errHeartbeatRunning) {
			a.writeAdminError(c, http.StatusConflict, "heartbeat_running", "heartbeat is already running")
		} else {
			a.writeAdminError(c, http.StatusNotFound, "heartbeat_not_found", "heartbeat not found")
		}
		return
	}
	updated, err := a.executeHeartbeat(c.Request.Context(), monitor)
	if err != nil {
		a.writeAdminError(c, http.StatusInternalServerError, "heartbeat_run_failed", err.Error())
		return
	}
	c.JSON(http.StatusOK, updated)
}

func (a *App) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			for _, monitor := range a.heartbeats.claimDue(now.UTC()) {
				if _, err := a.executeHeartbeat(ctx, monitor); err != nil && a.logger != nil {
					a.logger.Warn("heartbeat model test failed", "heartbeat_id", monitor.ID, "error", err.Error())
				}
			}
		}
	}
}

func (a *App) executeHeartbeat(parent context.Context, monitor heartbeatMonitor) (heartbeatMonitor, error) {
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	defer cancel()
	accountLabel := "所选账号"
	var outcome heartbeatOutcome
	var cause error

	record, ok, err := a.accounts.Get(monitor.AccountID)
	if err != nil {
		outcome = heartbeatOutcome{Status: heartbeatError, ErrorCode: "account_lookup_failed", Error: err.Error()}
	} else if !ok {
		outcome = heartbeatOutcome{Status: heartbeatError, ErrorCode: "account_not_found", Error: "账号不存在"}
	} else if record.Status != accounts.StatusActive {
		accountLabel = heartbeatAccountName(record)
		outcome = heartbeatOutcome{Status: heartbeatError, ErrorCode: "account_not_active", Error: "账号当前不可用"}
	} else {
		accountLabel = heartbeatAccountName(record)
		ready := record
		if a.accountMgr != nil {
			ready, cause = a.accountMgr.EnsureReady(ctx, monitor.AccountID)
		}
		if cause == nil {
			var result adminModelTestResponse
			result, cause = a.runModelProbe(ctx, ready, monitor.Model, randomHeartbeatPrompt())
			if cause == nil {
				switch {
				case result.Status != "completed":
					outcome = heartbeatOutcome{Status: heartbeatError, ErrorCode: "upstream_status", Error: "上游状态为 " + result.Status, UpstreamModel: result.UpstreamModel}
				case !result.Match:
					outcome = heartbeatOutcome{Status: heartbeatMismatch, UpstreamModel: result.UpstreamModel}
				default:
					outcome = heartbeatOutcome{Status: heartbeatHealthy, UpstreamModel: result.UpstreamModel}
				}
			}
		}
		if cause != nil {
			if ctx.Err() != nil && errors.Is(ctx.Err(), context.Canceled) {
				a.heartbeats.release(monitor.ID)
				return heartbeatMonitor{}, ctx.Err()
			}
			_, code, message := a.classifyUpstreamError(monitor.AccountID, cause)
			outcome = heartbeatOutcome{Status: heartbeatError, ErrorCode: code, Error: message}
		}
	}

	updated, shouldNotify, err := a.heartbeats.complete(monitor.ID, outcome, time.Now().UTC())
	if err != nil {
		return heartbeatMonitor{}, err
	}
	if shouldNotify {
		a.notifyHeartbeatChange(accountLabel, updated)
	}
	if cause != nil && isAdminCredentialFailure(cause) {
		a.removeAdminCredentialFailure(monitor.AccountID, cause)
	}
	return updated, nil
}

func heartbeatAccountName(record accounts.Record) string {
	for _, value := range []string{record.Label, record.Email} {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return "所选账号"
}

func (a *App) notifyHeartbeatChange(accountLabel string, monitor heartbeatMonitor) {
	if a.notifyHeartbeat == nil {
		return
	}
	title := "Codex 模型监测异常"
	message := accountLabel + " · " + monitor.Model
	switch monitor.LastStatus {
	case heartbeatHealthy:
		title = "Codex 模型监测已恢复"
		message += "\n模型响应已恢复正常。"
	case heartbeatMismatch:
		message += "\n请求模型与上游模型不一致：" + monitor.LastUpstreamModel
	default:
		message += "\n" + monitor.LastError
	}
	if err := a.notifyHeartbeat(title, message); err != nil && a.logger != nil {
		a.logger.Warn("send heartbeat notification failed", "heartbeat_id", monitor.ID, "error", err.Error())
	}
}

func (a *App) removeHeartbeatsForAccount(accountID string) {
	if a.heartbeats == nil {
		return
	}
	if err := a.heartbeats.removeByAccount(accountID); err != nil && a.logger != nil {
		a.logger.Warn("remove account heartbeats failed", "account_id", accountID, "error", err.Error())
	}
}
