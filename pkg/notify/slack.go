package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/tesserix/crossplane-validation/pkg/plan"
)

// SlackNotifier sends plan summaries via Slack incoming webhooks.
type SlackNotifier struct {
	WebhookURL string
	Channel    string
}

// NewSlackNotifier creates a Slack notifier with the given webhook URL.
func NewSlackNotifier(webhookURL, channel string) *SlackNotifier {
	return &SlackNotifier{
		WebhookURL: webhookURL,
		Channel:    channel,
	}
}

func (s *SlackNotifier) Send(result *plan.Result) error {
	if s.WebhookURL == "" {
		return nil
	}

	text := formatSlackMessage(result)

	payload := map[string]interface{}{
		"text": text,
	}
	if s.Channel != "" {
		payload["channel"] = s.Channel
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(s.WebhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("sending slack notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("slack webhook returned %d", resp.StatusCode)
	}

	return nil
}

func formatSlackMessage(result *plan.Result) string {
	if result.StructuralDiff == nil {
		return "Crossplane Validation: No changes detected."
	}

	d := result.StructuralDiff
	var parts []string

	parts = append(parts, "*Crossplane Validation Plan*")
	parts = append(parts, fmt.Sprintf("Plan: %d to add, %d to change, %d to destroy",
		d.Summary.ToAdd, d.Summary.ToChange, d.Summary.ToDelete))

	if d.Summary.ToDelete > 0 {
		parts = append(parts, fmt.Sprintf(":warning: %d resource(s) will be destroyed", d.Summary.ToDelete))
	}

	return strings.Join(parts, "\n")
}
