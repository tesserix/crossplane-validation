// Package notify sends plan summaries to external channels like Slack and Teams.
package notify

import (
	"github.com/tesserix/crossplane-validation/pkg/plan"
)

// Notifier sends plan results to an external notification channel.
type Notifier interface {
	Send(result *plan.Result) error
}

// MultiNotifier dispatches to multiple notification channels.
type MultiNotifier struct {
	notifiers []Notifier
}

// NewMultiNotifier creates a notifier that dispatches to all provided notifiers.
func NewMultiNotifier(notifiers ...Notifier) *MultiNotifier {
	return &MultiNotifier{notifiers: notifiers}
}

func (m *MultiNotifier) Send(result *plan.Result) error {
	var lastErr error
	for _, n := range m.notifiers {
		if err := n.Send(result); err != nil {
			lastErr = err
		}
	}
	return lastErr
}
