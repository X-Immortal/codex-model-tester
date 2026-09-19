package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"chatgpt-codex-proxy/internal/accounts"
	"chatgpt-codex-proxy/internal/codex"
	"chatgpt-codex-proxy/internal/jsonutil"
	"chatgpt-codex-proxy/internal/models"
)

type adminModelTestRequest struct {
	AccountID string `json:"account_id"`
	Model     string `json:"model"`
}

const modelTestPrompt = "Reply with exactly: OK"

type adminModelTestResponse struct {
	AccountID      string         `json:"account_id"`
	RequestedModel string         `json:"requested_model"`
	UpstreamModel  string         `json:"upstream_model"`
	Match          bool           `json:"match"`
	ResponseID     string         `json:"response_id"`
	Status         string         `json:"status"`
	Timestamp      time.Time      `json:"timestamp"`
	RawEvent       map[string]any `json:"raw_event"`
}

func (a *App) handleAdminAccountModels(c *gin.Context) {
	accountID := strings.TrimSpace(c.Param("account_id"))
	record, ok, err := a.accounts.Get(accountID)
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

	ready := record
	if a.accountMgr != nil {
		ready, err = a.accountMgr.EnsureReady(c.Request.Context(), accountID)
		if err != nil {
			a.writeAdminAccountError(c, accountID, http.StatusBadGateway, "account_not_ready", err.Error(), err)
			return
		}
	}

	fetch := a.fetchModels
	if fetch == nil && a.httpClient != nil {
		fetch = a.httpClient.GetCodexModels
	}
	if fetch == nil {
		a.writeAdminError(c, http.StatusInternalServerError, "models_unavailable", "account model fetcher is not configured")
		return
	}
	rawEntries, err := fetch(c.Request.Context(), ready)
	if err != nil {
		a.writeAdminUpstreamError(c, accountID, err)
		return
	}
	entries := models.NormalizeBackendEntries(rawEntries)
	if len(entries) == 0 {
		a.writeAdminError(c, http.StatusBadGateway, "models_lookup_failed", "upstream returned no usable models")
		return
	}
	a.modelCatalog().ApplyRouteModels(models.RoutingKeyForRecord(ready), entries)
	c.JSON(http.StatusOK, gin.H{
		"account_id": accountID,
		"models":     entries,
	})
}

func (a *App) handleAdminModelTest(c *gin.Context) {
	var request adminModelTestRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		a.writeAdminError(c, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	accountID := strings.TrimSpace(request.AccountID)
	modelID := strings.TrimSpace(request.Model)
	if accountID == "" || modelID == "" {
		a.writeAdminError(c, http.StatusBadRequest, "invalid_model_test", "account_id and model are required")
		return
	}

	record, ok, err := a.accounts.Get(accountID)
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

	ready := record
	if a.accountMgr != nil {
		ready, err = a.accountMgr.EnsureReady(c.Request.Context(), accountID)
		if err != nil {
			a.writeAdminAccountError(c, accountID, http.StatusBadGateway, "account_not_ready", err.Error(), err)
			return
		}
	}

	result, err := a.runFixedModelTest(c.Request.Context(), ready, modelID)
	if err != nil {
		a.writeAdminModelTestError(c, accountID, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (a *App) writeAdminModelTestError(c *gin.Context, accountID string, cause error) {
	a.writeAdminUpstreamError(c, accountID, cause)
}

func (a *App) runFixedModelTest(ctx context.Context, account accounts.Record, modelID string) (adminModelTestResponse, error) {
	return a.runModelProbe(ctx, account, modelID, modelTestPrompt)
}

func (a *App) runModelProbe(ctx context.Context, account accounts.Record, modelID, prompt string) (adminModelTestResponse, error) {
	request := codex.Request{
		Model:     modelID,
		Reasoning: &codex.Reasoning{Effort: "low"},
		Input: []codex.InputItem{{
			Role: "user",
			Content: []codex.ContentPart{{
				Type: "input_text",
				Text: prompt,
			}},
		}},
		Stream: true,
		Store:  false,
	}

	var stream eventStream
	var err error
	if a.httpStream != nil {
		stream, err = a.httpStream(ctx, account, request, "")
	} else if a.httpClient != nil {
		stream, err = a.httpClient.StreamResponse(ctx, account, request, "")
	} else {
		err = fmt.Errorf("codex stream client is not configured")
	}
	if err != nil {
		return adminModelTestResponse{}, err
	}
	if stream == nil {
		return adminModelTestResponse{}, fmt.Errorf("codex stream client returned a nil stream")
	}
	defer stream.Close()

	for {
		event, nextErr := stream.NextEvent()
		if nextErr != nil {
			if nextErr == io.EOF {
				return adminModelTestResponse{}, fmt.Errorf("upstream stream ended before response.completed")
			}
			return adminModelTestResponse{}, nextErr
		}
		if a.observeQuotaEvent(account, event) {
			continue
		}
		if upstreamErr := codex.StreamEventError(event); upstreamErr != nil {
			return adminModelTestResponse{}, upstreamErr
		}
		if event == nil || event.Type != "response.completed" {
			continue
		}

		rawEvent := jsonutil.CloneMap(event.Raw)
		response := jsonutil.MapValue(rawEvent, "response")
		if response == nil {
			return adminModelTestResponse{}, fmt.Errorf("response.completed is missing response")
		}
		responseID := jsonutil.StringValue(response["id"])
		if responseID == "" {
			return adminModelTestResponse{}, fmt.Errorf("response.completed is missing response.id")
		}
		upstreamModel := jsonutil.StringValue(response["model"])
		status := jsonutil.StringValue(response["status"])
		if status == "" {
			status = "completed"
		}
		return adminModelTestResponse{
			AccountID:      account.ID,
			RequestedModel: modelID,
			UpstreamModel:  upstreamModel,
			Match:          strictModelMatch(modelID, upstreamModel),
			ResponseID:     responseID,
			Status:         status,
			Timestamp:      time.Now().UTC(),
			RawEvent:       rawEvent,
		}, nil
	}
}

func fixedModelTestRequest(request codex.Request) bool {
	return modelProbeRequest(request, modelTestPrompt)
}

func modelProbeRequest(request codex.Request, prompt string) bool {
	return request.Model != "" && request.Stream && !request.Store &&
		request.Reasoning != nil && request.Reasoning.Effort == "low" &&
		len(request.Input) == 1 &&
		request.Input[0].Role == "user" &&
		len(request.Input[0].Content) == 1 &&
		request.Input[0].Content[0].Text == prompt
}

func strictModelMatch(requested, upstream string) bool {
	return requested == upstream
}
