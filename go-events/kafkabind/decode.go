package kafkabind

import (
	"encoding/json"
	"fmt"

	"github.com/segmentio/kafka-go"

	tasevents "github.com/Tributary-ai-services/aether-shared/go-events"
)

// IsCloudEvent checks whether a Kafka message carries a CloudEvents structured-mode
// payload by inspecting the content-type header.
func IsCloudEvent(headers []kafka.Header) bool {
	for _, h := range headers {
		if h.Key == "content-type" && string(h.Value) == ContentType {
			return true
		}
	}
	return false
}

// Decode parses a structured-mode CloudEvents JSON message.
func Decode(value []byte, headers []kafka.Header) (tasevents.Event, error) {
	var e tasevents.Event
	if err := json.Unmarshal(value, &e); err != nil {
		return e, fmt.Errorf("unmarshal event: %w", err)
	}
	if err := e.Validate(); err != nil {
		return e, fmt.Errorf("invalid event: %w", err)
	}
	return e, nil
}
