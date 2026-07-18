package eventhub

import (
	"context"
	"testing"
)

func TestRelayPublisherCloseIsJoinedAndTerminal(t *testing.T) {
	publisher := NewRelayPublisher(context.Background(), nil)
	publisher.Close()
	publisher.Close()

	select {
	case <-publisher.done:
	default:
		t.Fatal("RelayPublisher.Close returned before notifier loop exited")
	}
	// A post-close emission must be a harmless no-op even without a database.
	publisher.Emit(EventLog, LogPayload{Message: "ignored"})
}
