package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/tesserix/crossplane-validation/pkg/plan"
)

// TeamsNotifier sends plan summaries via Microsoft Teams incoming webhooks.
type TeamsNotifier struct {
	WebhookURL string
}

// NewTeamsNotifier creates a Teams notifier with the given webhook URL.
func NewTeamsNotifier(webhookURL string) *TeamsNotifier {
	return &TeamsNotifier{WebhookURL: webhookURL}
}

func (t *TeamsNotifier) Send(result *plan.Result) error {
	if t.WebhookURL == "" {
		return nil
	}

	card := buildTeamsCard(result)

	body, err := json.Marshal(card)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(t.WebhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("sending teams notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("teams webhook returned %d", resp.StatusCode)
	}

	return nil
}

func buildTeamsCard(result *plan.Result) map[string]interface{} {
	summary := "No changes detected."
	if result.StructuralDiff != nil {
		d := result.StructuralDiff
		summary = fmt.Sprintf("%d to add, %d to change, %d to destroy",
			d.Summary.ToAdd, d.Summary.ToChange, d.Summary.ToDelete)
	}

	return map[string]interface{}{
		"@type":      "MessageCard",
		"@context":   "https://schema.org/extensions",
		"themeColor": "0076D7",
		"summary":    "Crossplane Validation Plan",
		"sections": []map[string]interface{}{
			{
				"activityTitle": "Crossplane Validation Plan",
				"facts": []map[string]string{
					{"name": "Plan", "value": summary},
				},
				"markdown": true,
			},
		},
	}
}
