// Package kafkabind provides CloudEvents structured-mode encoding for Kafka
// via segmentio/kafka-go.
package kafkabind

import (
	"encoding/json"
	"fmt"

	"github.com/segmentio/kafka-go"

	tasevents "github.com/Tributary-ai-services/aether-shared/go-events"
)

const ContentType = "application/cloudevents+json"

// Encode serializes a CloudEvent into structured-mode JSON (entire event in
// message value). Two headers are set:
//   - content-type: application/cloudevents+json (CE spec)
//   - ce_type: <type> (redundant, enables DLQ routing without body parse)
func Encode(e tasevents.Event) (value []byte, headers []kafka.Header, err error) {
	if err := e.Validate(); err != nil {
		return nil, nil, fmt.Errorf("invalid event: %w", err)
	}

	value, err = json.Marshal(e)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal event: %w", err)
	}

	headers = []kafka.Header{
		{Key: "content-type", Value: []byte(ContentType)},
		{Key: "ce_type", Value: []byte(e.Type)},
	}

	return value, headers, nil
}

// MessageKey returns the Kafka message key for an event. Format: "tenantid:subject".
// Falls back to event ID if both are empty.
func MessageKey(e tasevents.Event) []byte {
	switch {
	case e.TenantID != "" && e.Subject != "":
		return []byte(e.TenantID + ":" + e.Subject)
	case e.TenantID != "":
		return []byte(e.TenantID + ":" + e.ID)
	default:
		return []byte(e.ID)
	}
}
