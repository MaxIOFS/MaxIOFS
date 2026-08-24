package s3compat

import (
	"context"
	"testing"
)

func TestBackgroundJobContextKeepsRequestValuesButUsesShutdownCancellation(t *testing.T) {
	type contextKey string

	h := NewHandler(nil, nil)
	serverCtx, stopServer := context.WithCancel(context.Background())
	defer stopServer()
	h.SetBackgroundRunner(func() context.Context { return serverCtx }, func(string, func()) bool { return true })

	reqCtx, cancelRequest := context.WithCancel(context.WithValue(context.Background(), contextKey("user"), "alice"))
	bgCtx := h.backgroundJobContext(reqCtx)

	if got := bgCtx.Value(contextKey("user")); got != "alice" {
		t.Fatalf("background context lost request value: got %v", got)
	}

	cancelRequest()
	select {
	case <-bgCtx.Done():
		t.Fatal("background context should not be cancelled by client/request cancellation")
	default:
	}

	stopServer()
	select {
	case <-bgCtx.Done():
	default:
		t.Fatal("background context should be cancelled by server shutdown")
	}
}

func TestRunBackgroundRejectsWhenConfiguredRunnerRejects(t *testing.T) {
	h := NewHandler(nil, nil)
	ran := false
	h.SetBackgroundRunner(func() context.Context { return context.Background() }, func(string, func()) bool {
		return false
	})

	if h.runBackground("test", func() { ran = true }) {
		t.Fatal("runBackground returned true after runner rejected the job")
	}
	if ran {
		t.Fatal("runBackground executed a rejected job")
	}
}
