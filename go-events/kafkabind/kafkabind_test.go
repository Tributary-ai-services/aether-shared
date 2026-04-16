package kafkabind

import (
	"encoding/json"
	"testing"

	"github.com/segmentio/kafka-go"

	tasevents "github.com/Tributary-ai-services/aether-shared/go-events"
)

func TestEncodeDecodeTroundTrip(t *testing.T) {
	original := tasevents.New("com.tas.activity.document.uploaded", "urn:tas:service:audimodal",
		tasevents.WithTenant("t-acme", "sp-default"),
		tasevents.WithSubject("file-abc123"),
		tasevents.WithData(map[string]string{"file_id": "file-abc123"}),
	)

	value, headers, err := Encode(original)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	if !IsCloudEvent(headers) {
		t.Error("IsCloudEvent should return true for encoded headers")
	}

	decoded, err := Decode(value, headers)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if decoded.SpecVersion != "1.0" {
		t.Errorf("specversion = %q, want 1.0", decoded.SpecVersion)
	}
	if decoded.ID != original.ID {
		t.Errorf("id mismatch: %q != %q", decoded.ID, original.ID)
	}
	if decoded.Source != original.Source {
		t.Errorf("source mismatch")
	}
	if decoded.Type != original.Type {
		t.Errorf("type mismatch")
	}
	if decoded.TenantID != "t-acme" {
		t.Errorf("tenantid = %q, want t-acme", decoded.TenantID)
	}
	if decoded.Subject != "file-abc123" {
		t.Errorf("subject = %q, want file-abc123", decoded.Subject)
	}
}

func TestIsCloudEvent(t *testing.T) {
	tests := []struct {
		name    string
		headers []kafka.Header
		want    bool
	}{
		{
			name:    "CE headers",
			headers: []kafka.Header{{Key: "content-type", Value: []byte(ContentType)}},
			want:    true,
		},
		{
			name:    "no content-type",
			headers: []kafka.Header{{Key: "ce_type", Value: []byte("test")}},
			want:    false,
		},
		{
			name:    "wrong content-type",
			headers: []kafka.Header{{Key: "content-type", Value: []byte("application/json")}},
			want:    false,
		},
		{
			name:    "empty headers",
			headers: nil,
			want:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsCloudEvent(tt.headers); got != tt.want {
				t.Errorf("IsCloudEvent() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEncodeInvalidEvent(t *testing.T) {
	e := tasevents.Event{}
	_, _, err := Encode(e)
	if err == nil {
		t.Error("Encode should fail for invalid event")
	}
}

func TestMessageKey(t *testing.T) {
	tests := []struct {
		name string
		e    tasevents.Event
		want string
	}{
		{
			name: "tenant and subject",
			e:    tasevents.Event{TenantID: "t-acme", Subject: "file-123", ID: "uuid-1"},
			want: "t-acme:file-123",
		},
		{
			name: "tenant only",
			e:    tasevents.Event{TenantID: "t-acme", ID: "uuid-1"},
			want: "t-acme:uuid-1",
		},
		{
			name: "no tenant",
			e:    tasevents.Event{ID: "uuid-1"},
			want: "uuid-1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(MessageKey(tt.e)); got != tt.want {
				t.Errorf("MessageKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDecodeRejectsNonCE(t *testing.T) {
	legacy := map[string]interface{}{
		"schema_version": "1",
		"event_id":       "abc",
		"event_type":     "document.uploaded",
		"source_service": "audimodal",
	}
	data, _ := json.Marshal(legacy)
	_, err := Decode(data, nil)
	if err == nil {
		t.Error("Decode should reject legacy envelope (missing specversion)")
	}
}
