package server

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"chatgpt-codex-proxy/internal/accountmanager"
	"chatgpt-codex-proxy/internal/accounts"
)

type adminAccountResponse struct {
	ID            string                  `json:"id"`
	UpstreamID    string                  `json:"upstream_account_id"`
	UserID        string                  `json:"user_id,omitempty"`
	Email         string                  `json:"email,omitempty"`
	PlanType      string                  `json:"plan_type,omitempty"`
	Label         string                  `json:"label,omitempty"`
	Status        accounts.Status         `json:"status"`
	EligibleNow   bool                    `json:"eligible_now"`
	CooldownUntil *time.Time              `json:"cooldown_until,omitempty"`
	LastError     string                  `json:"last_error,omitempty"`
	CachedQuota   *accounts.QuotaSnapshot `json:"cached_quota,omitempty"`
	OauthExpires  time.Time               `json:"oauth_expires"`
	CreatedAt     time.Time               `json:"created_at"`
	UpdatedAt     time.Time               `json:"updated_at"`
}

type adminAccountUsageResponse struct {
	AccountID      string                  `json:"account_id"`
	UpstreamID     string                  `json:"upstream_account_id"`
	UserID         string                  `json:"user_id,omitempty"`
	Status         accounts.Status         `json:"status"`
	EligibleNow    bool                    `json:"eligible_now"`
	CooldownUntil  *time.Time              `json:"cooldown_until,omitempty"`
	LastError      string                  `json:"last_error,omitempty"`
	CachedQuota    *accounts.QuotaSnapshot `json:"cached_quota,omitempty"`
	QuotaRuntime   *accounts.QuotaSnapshot `json:"quota_runtime,omitempty"`
	QuotaSource    string                  `json:"quota_source,omitempty"`
	QuotaFetchedAt *time.Time              `json:"quota_fetched_at,omitempty"`
	OauthExpires   time.Time               `json:"oauth_expires"`
}

func (a *App) handleAdminAccounts(c *gin.Context) {
	records, err := a.accounts.List()
	if err != nil {
		a.writeAdminError(c, http.StatusInternalServerError, "accounts_list_failed", err.Error())
		return
	}
	items := make([]adminAccountResponse, 0, len(records))
	for _, record := range records {
		item, err := a.adminAccountView(record)
		if err != nil {
			a.writeAdminError(c, http.StatusInternalServerError, "accounts_list_failed", err.Error())
			return
		}
		items = append(items, item)
	}
	c.JSON(http.StatusOK, gin.H{"accounts": items})
}

func (a *App) adminAccountView(record accounts.Record) (adminAccountResponse, error) {
	eligible, err := a.accounts.EligibleNow(record.ID)
	if err != nil {
		return adminAccountResponse{}, err
	}
	return adminAccountResponse{
		ID: record.ID, UpstreamID: record.AccountID, UserID: record.UserID, Email: record.Email,
		PlanType: record.PlanType, Label: record.Label, Status: record.Status, EligibleNow: eligible,
		CooldownUntil: record.CooldownUntil, LastError: record.LastError, CachedQuota: record.CachedQuota,
		OauthExpires: record.Token.ExpiresAt, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
	}, nil
}

func (a *App) handleAdminDeviceLoginStart(c *gin.Context) {
	record, err := a.deviceLogins.Start(c.Request.Context())
	if err != nil {
		a.writeAdminError(c, http.StatusBadGateway, "device_login_start_failed", err.Error())
		return
	}
	c.JSON(http.StatusOK, record)
}

func (a *App) handleAdminDeviceLoginGet(c *gin.Context) {
	record, ok := a.deviceLogins.Get(c.Param("login_id"))
	if !ok {
		a.writeAdminError(c, http.StatusNotFound, "login_not_found", "device login not found")
		return
	}
	c.JSON(http.StatusOK, record)
}

func (a *App) handleAdminDeviceLoginCancel(c *gin.Context) {
	if !a.deviceLogins.Cancel(c.Param("login_id")) {
		a.writeAdminError(c, http.StatusNotFound, "login_not_found", "device login not found")
		return
	}
	c.Status(http.StatusNoContent)
}

func (a *App) handleAdminAccountDelete(c *gin.Context) {
	accountID := c.Param("account_id")
	if err := a.accounts.Remove(accountID); err != nil {
		a.writeAdminError(c, http.StatusNotFound, "account_not_found", err.Error())
		return
	}
	a.removeHeartbeatsForAccount(accountID)
	c.Status(http.StatusNoContent)
}

func (a *App) handleAdminAccountLogout(c *gin.Context) {
	accountID := c.Param("account_id")
	record, ok, err := a.accounts.Get(accountID)
	if err != nil {
		a.writeAdminError(c, http.StatusInternalServerError, "account_lookup_failed", err.Error())
		return
	}
	if !ok {
		a.writeAdminError(c, http.StatusNotFound, "account_not_found", "account not found")
		return
	}
	if a.revokeOAuth == nil {
		a.writeAdminError(c, http.StatusInternalServerError, "logout_unavailable", "OAuth logout is not configured")
		return
	}
	if err := a.revokeOAuth(c.Request.Context(), record.Token); err != nil {
		if a.logger != nil {
			a.logger.Warn("revoke account OAuth session failed", "account_id", accountID, "error", err.Error())
		}
		a.writeAdminError(c, http.StatusBadGateway, "logout_failed", "failed to revoke the upstream OAuth session; local credentials were kept")
		return
	}
	if err := a.accounts.Remove(accountID); err != nil {
		a.writeAdminError(c, http.StatusInternalServerError, "logout_cleanup_failed", "OAuth session was revoked but local credentials could not be removed")
		return
	}
	a.removeHeartbeatsForAccount(accountID)
	c.Status(http.StatusNoContent)
}

func (a *App) handleAdminAccountPatch(c *gin.Context) {
	var body struct {
		Label  *string `json:"label"`
		Status *string `json:"status"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		a.writeAdminError(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	var statusPtr *accounts.Status
	if body.Status != nil {
		status := accounts.Status(strings.TrimSpace(*body.Status))
		switch status {
		case accounts.StatusActive, accounts.StatusDisabled:
			statusPtr = &status
		default:
			a.writeAdminError(c, http.StatusBadRequest, "invalid_status", "status must be active or disabled")
			return
		}
	}

	record, err := a.accounts.Patch(c.Param("account_id"), body.Label, statusPtr)
	if err != nil {
		a.writeAdminError(c, http.StatusNotFound, "account_not_found", err.Error())
		return
	}
	view, err := a.adminAccountView(record)
	if err != nil {
		a.writeAdminError(c, http.StatusInternalServerError, "account_lookup_failed", err.Error())
		return
	}
	c.JSON(http.StatusOK, view)
}

func (a *App) handleAdminAccountUsage(c *gin.Context) {
	record, quota, err := a.accountMgr.GetUsage(c.Request.Context(), c.Param("account_id"), c.Query("cached") == "true")
	if err != nil {
		if errors.Is(err, accountmanager.ErrAccountNotFound) {
			a.writeAdminError(c, http.StatusNotFound, "account_not_found", err.Error())
			return
		}
		if isAdminCredentialFailure(err) {
			a.writeAdminUpstreamError(c, c.Param("account_id"), err)
		} else {
			a.writeAdminAccountError(c, c.Param("account_id"), http.StatusBadGateway, "usage_lookup_failed", err.Error(), err)
		}
		return
	}
	eligibleNow, err := a.accounts.EligibleNow(record.ID)
	if err != nil {
		a.writeAdminError(c, http.StatusInternalServerError, "usage_lookup_failed", err.Error())
		return
	}
	effectiveQuota := quota
	if effectiveQuota == nil {
		effectiveQuota = record.CachedQuota
	}
	quotaSource := ""
	var quotaFetchedAt *time.Time
	if effectiveQuota != nil {
		quotaSource = effectiveQuota.Source
		if !effectiveQuota.FetchedAt.IsZero() {
			ts := effectiveQuota.FetchedAt.UTC()
			quotaFetchedAt = &ts
		}
	}
	c.JSON(http.StatusOK, adminAccountUsageResponse{
		AccountID:      record.ID,
		UpstreamID:     record.AccountID,
		UserID:         record.UserID,
		Status:         record.Status,
		EligibleNow:    eligibleNow,
		CooldownUntil:  record.CooldownUntil,
		LastError:      record.LastError,
		CachedQuota:    record.CachedQuota,
		QuotaRuntime:   quota,
		QuotaSource:    quotaSource,
		QuotaFetchedAt: quotaFetchedAt,
		OauthExpires:   record.Token.ExpiresAt,
	})
}

func (a *App) handleAdminAccountRefresh(c *gin.Context) {
	record, err := a.accountMgr.Refresh(c.Request.Context(), c.Param("account_id"))
	if err != nil {
		if errors.Is(err, accountmanager.ErrAccountNotFound) {
			a.writeAdminError(c, http.StatusNotFound, "account_not_found", err.Error())
			return
		}
		a.writeAdminError(c, http.StatusBadGateway, "refresh_failed", err.Error())
		return
	}
	view, err := a.adminAccountView(record)
	if err != nil {
		a.writeAdminError(c, http.StatusInternalServerError, "account_lookup_failed", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"account": view})
}

func (a *App) handleAdminRotationGet(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"strategy": a.accounts.RotationStrategy()})
}

func (a *App) handleAdminRotationPut(c *gin.Context) {
	var body struct {
		Strategy string `json:"strategy"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		a.writeAdminError(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	strategy := accounts.RotationStrategy(strings.TrimSpace(body.Strategy))
	switch strategy {
	case accounts.RotationLeastUsed, accounts.RotationRoundRobin, accounts.RotationSticky:
	default:
		a.writeAdminError(c, http.StatusBadRequest, "invalid_strategy", "strategy must be least_used, round_robin, or sticky")
		return
	}
	if err := a.accounts.SetRotationStrategy(strategy); err != nil {
		a.writeAdminError(c, http.StatusInternalServerError, "rotation_update_failed", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"strategy": strategy})
}
