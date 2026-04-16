// Package tasevents provides a CloudEvents 1.0 envelope for TAS activity
// and compliance events. The envelope is hand-rolled (not cloudevents/sdk-go)
// because our only transport is Kafka via segmentio/kafka-go.
package tasevents

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

const SpecVersion = "1.0"

// Event is a CloudEvents 1.0 compliant envelope. Extension attribute names
// use lowercase-only per CE 1.0 §4.2 (no underscores, no uppercase).
type Event struct {
	// CE 1.0 REQUIRED
	SpecVersion string `json:"specversion"`
	ID          string `json:"id"`
	Source      string `json:"source"`
	Type        string `json:"type"`

	// CE 1.0 OPTIONAL
	DataContentType string    `json:"datacontenttype,omitempty"`
	DataSchema      string    `json:"dataschema,omitempty"`
	Subject         string    `json:"subject,omitempty"`
	Time            time.Time `json:"time,omitempty"`

	// TAS extension attributes (lowercase-only per CE spec)
	TenantID   string   `json:"tenantid,omitempty"`
	SpaceID    string   `json:"spaceid,omitempty"`
	UserID     string   `json:"userid,omitempty"`
	RequestID  string   `json:"requestid,omitempty"`
	Severity   Severity `json:"severity,omitempty"`
	Frameworks []string `json:"frameworks,omitempty"`

	// CE data field — raw JSON payload
	Data json.RawMessage `json:"data,omitempty"`
}

// Option configures an Event via functional options.
type Option func(*Event)

// New creates a CloudEvents 1.0 compliant Event with a fresh UUID and current
// timestamp. source must be a valid URI (e.g. "urn:tas:service:audimodal").
func New(eventType, source string, opts ...Option) Event {
	e := Event{
		SpecVersion:     SpecVersion,
		ID:              uuid.NewString(),
		Source:          source,
		Type:            eventType,
		Time:            time.Now().UTC(),
		DataContentType: "application/json",
	}
	for _, opt := range opts {
		opt(&e)
	}
	return e
}

// NewWithID creates an Event with a caller-supplied ID. Use this when the
// same event is published in two formats (e.g. dual-publish during migration)
// so consumers can deduplicate on ID.
func NewWithID(id, eventType, source string, opts ...Option) Event {
	e := Event{
		SpecVersion:     SpecVersion,
		ID:              id,
		Source:          source,
		Type:            eventType,
		Time:            time.Now().UTC(),
		DataContentType: "application/json",
	}
	for _, opt := range opts {
		opt(&e)
	}
	return e
}

func WithTenant(tenantID, spaceID string) Option {
	return func(e *Event) {
		e.TenantID = tenantID
		e.SpaceID = spaceID
	}
}

func WithUser(userID string) Option {
	return func(e *Event) { e.UserID = userID }
}

func WithRequest(requestID string) Option {
	return func(e *Event) { e.RequestID = requestID }
}

func WithSubject(subject string) Option {
	return func(e *Event) { e.Subject = subject }
}

func WithSeverity(s Severity) Option {
	return func(e *Event) { e.Severity = s }
}

func WithFrameworks(fws ...string) Option {
	return func(e *Event) { e.Frameworks = fws }
}

func WithTime(t time.Time) Option {
	return func(e *Event) { e.Time = t.UTC() }
}

// WithData marshals v to JSON and sets it as the event payload.
func WithData(v any) Option {
	return func(e *Event) {
		data, err := json.Marshal(v)
		if err != nil {
			return
		}
		e.Data = data
	}
}

// WithRawData sets pre-marshaled JSON as the event payload.
func WithRawData(data json.RawMessage) Option {
	return func(e *Event) { e.Data = data }
}

// Validate checks that the event meets CloudEvents 1.0 requirements.
func (e *Event) Validate() error {
	if e.SpecVersion != SpecVersion {
		return fmt.Errorf("specversion must be %q, got %q", SpecVersion, e.SpecVersion)
	}
	if e.ID == "" {
		return fmt.Errorf("id is required")
	}
	if e.Source == "" {
		return fmt.Errorf("source is required")
	}
	if _, err := url.Parse(e.Source); err != nil {
		return fmt.Errorf("source must be a valid URI: %w", err)
	}
	if e.Type == "" {
		return fmt.Errorf("type is required")
	}
	if strings.ContainsAny(e.Type, " \t\n") {
		return fmt.Errorf("type must not contain whitespace")
	}
	return nil
}
