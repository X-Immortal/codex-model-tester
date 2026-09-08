//go:build live

package integration_test

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLiveCompactionRoundTrip(t *testing.T) {
	cfg := loadLiveConfig(t)
	history := []map[string]any{
		{"role": "user", "content": "Read the deployment notes and remember the launch code and target region for later."},
		{"type": "function_call", "call_id": "call_deployment_notes", "name": "read_deployment_notes", "arguments": "{}"},
		{"type": "function_call_output", "call_id": "call_deployment_notes", "output": "Target region: eu-west-3.\n" + strings.Repeat("Service health check successful; latency stable; no alerts.\n", 400)},
		{"role": "assistant", "content": "The launch code is MARIGOLD_742. I will remember the deployment details."},
	}
	original, err := json.Marshal(history)
	if err != nil {
		t.Fatal(err)
	}
	compactRequest := map[string]any{
		"model":        cfg.Model,
		"instructions": "Preserve deployment facts for future turns.",
		"reasoning":    map[string]string{"effort": "low"},
		"input":        history,
	}
	expected := []string{"MARIGOLD_742", "eu-west-3"}
	for round := range 2 {
		body := postJSON(t, cfg, "/responses/compact", compactRequest)
		var compact struct {
			ID     string           `json:"id"`
			Object string           `json:"object"`
			Output []map[string]any `json:"output"`
		}
		if err := json.Unmarshal(body, &compact); err != nil {
			t.Fatal(err)
		}
		if compact.ID == "" || compact.Object != "response.compaction" || len(compact.Output) == 0 {
			t.Fatalf("round %d: expected compacted output, got object=%q items=%d", round+1, compact.Object, len(compact.Output))
		}
		hasCompaction := false
		for _, item := range compact.Output {
			encrypted, _ := item["encrypted_content"].(string)
			if item["type"] == "compaction" && encrypted != "" {
				hasCompaction = true
			}
		}
		if !hasCompaction {
			t.Fatal("response did not contain encrypted compaction state")
		}
		if round == 0 {
			encoded, err := json.Marshal(compact.Output)
			if err != nil {
				t.Fatal(err)
			}
			if len(encoded) >= len(original) {
				t.Fatalf("compaction did not reduce context: %d -> %d bytes", len(original), len(encoded))
			}
			t.Logf("compacted history: %d -> %d bytes", len(original), len(encoded))
		}
		input := append(compact.Output, map[string]any{
			"role": "user", "content": "Repeat the launch code from your earlier answer, the target region from the deployment notes, and the deployment owner if supplied. Use the exact values from our conversation.",
		})
		continued := postJSON(t, cfg, "/responses", map[string]any{
			"model": cfg.Model, "input": input, "reasoning": map[string]string{"effort": "low"},
		})
		var response struct {
			ID     string `json:"id"`
			Output []struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"output"`
		}
		if err := json.Unmarshal(continued, &response); err != nil {
			t.Fatal(err)
		}
		var text strings.Builder
		for _, item := range response.Output {
			for _, part := range item.Content {
				text.WriteString(part.Text)
			}
		}
		for _, value := range expected {
			if !strings.Contains(text.String(), value) {
				t.Fatalf("round %d: compacted context lost %q: %q", round+1, value, text.String())
			}
		}
		if response.ID == "" {
			t.Fatal("continuation did not return a response id")
		}
		compactRequest = map[string]any{
			"previous_response_id": response.ID,
			"input":                []map[string]any{{"role": "user", "content": "The deployment owner is JUNIPER_19. Remember it along with the launch code and region."}},
			"reasoning":            map[string]string{"effort": "low"},
		}
		expected = append(expected, "JUNIPER_19")
	}
}
