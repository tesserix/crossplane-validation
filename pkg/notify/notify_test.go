package notify

import (
	"fmt"
	"testing"

	"github.com/tesserix/crossplane-validation/pkg/diff"
	"github.com/tesserix/crossplane-validation/pkg/plan"
)

type mockNotifier struct {
	called bool
	err    error
}

func (m *mockNotifier) Send(result *plan.Result) error {
	m.called = true
	return m.err
}

func TestMultiNotifierCallsAll(t *testing.T) {
	n1 := &mockNotifier{}
	n2 := &mockNotifier{}

	multi := NewMultiNotifier(n1, n2)

	result := &plan.Result{
		StructuralDiff: &diff.DiffResult{
			Summary: diff.DiffSummary{ToAdd: 1},
		},
	}

	err := multi.Send(result)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !n1.called {
		t.Error("expected first notifier to be called")
	}
	if !n2.called {
		t.Error("expected second notifier to be called")
	}
}

func TestMultiNotifierReturnsLastError(t *testing.T) {
	n1 := &mockNotifier{err: fmt.Errorf("first error")}
	n2 := &mockNotifier{err: fmt.Errorf("second error")}

	multi := NewMultiNotifier(n1, n2)

	err := multi.Send(&plan.Result{})
	if err == nil {
		t.Error("expected error from multi notifier")
	}
	if !n1.called || !n2.called {
		t.Error("both notifiers should be called even on errors")
	}
}

func TestMultiNotifierPartialError(t *testing.T) {
	n1 := &mockNotifier{}
	n2 := &mockNotifier{err: fmt.Errorf("fail")}

	multi := NewMultiNotifier(n1, n2)

	err := multi.Send(&plan.Result{})
	if err == nil {
		t.Error("expected error when one notifier fails")
	}
	if !n1.called {
		t.Error("first notifier should still be called")
	}
}

func TestSlackNotifierSkipsEmptyURL(t *testing.T) {
	s := NewSlackNotifier("", "")
	err := s.Send(&plan.Result{})
	if err != nil {
		t.Errorf("expected nil error for empty webhook URL, got %v", err)
	}
}

func TestTeamsNotifierSkipsEmptyURL(t *testing.T) {
	te := NewTeamsNotifier("")
	err := te.Send(&plan.Result{})
	if err != nil {
		t.Errorf("expected nil error for empty webhook URL, got %v", err)
	}
}
