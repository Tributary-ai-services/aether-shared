package tasevents

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	e := New("com.tas.activity.document.uploaded", "urn:tas:service:audimodal",
		WithTenant("t-acme", "sp-default"),
		WithUser("u-alice"),
		WithRequest("r-xyz"),
		WithSubject("file-abc123"),
		WithSeverity(SeverityInfo),
		WithData(map[string]string{"file_id": "file-abc123"}),
	)

	if e.SpecVersion != "1.0" {
		t.Errorf("specversion = %q, want %q", e.SpecVersion, "1.0")
	}
	if e.ID == "" {
		t.Error("id should not be empty")
	}
	if e.Source != "urn:tas:service:audimodal" {
		t.Errorf("source = %q, want urn:tas:service:audimodal", e.Source)
	}
	if e.Type != "com.tas.activity.document.uploaded" {
		t.Errorf("type = %q, want com.tas.activity.document.uploaded", e.Type)
	}
	if e.TenantID != "t-acme" {
		t.Errorf("tenantid = %q, want t-acme", e.TenantID)
	}
	if e.SpaceID != "sp-default" {
		t.Errorf("spaceid = %q, want sp-default", e.SpaceID)
	}
	if e.UserID != "u-alice" {
		t.Errorf("userid = %q, want u-alice", e.UserID)
	}
	if e.Subject != "file-abc123" {
		t.Errorf("subject = %q, want file-abc123", e.Subject)
	}
	if e.Severity != SeverityInfo {
		t.Errorf("severity = %q, want info", e.Severity)
	}
	if e.DataContentType != "application/json" {
		t.Errorf("datacontenttype = %q, want application/json", e.DataContentType)
	}
	if e.Time.IsZero() {
		t.Error("time should not be zero")
	}
}

func TestNewWithID(t *testing.T) {
	e := NewWithID("fixed-uuid", "com.tas.activity.llm.request", "urn:tas:service:llm-router")
	if e.ID != "fixed-uuid" {
		t.Errorf("id = %q, want fixed-uuid", e.ID)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		event   Event
		wantErr bool
	}{
		{
			name: "valid event",
			event: Event{
				SpecVersion: "1.0",
				ID:          "abc",
				Source:      "urn:tas:service:test",
				Type:        "com.tas.test.event",
			},
		},
		{
			name: "missing specversion",
			event: Event{
				ID:     "abc",
				Source: "urn:tas:service:test",
				Type:   "com.tas.test.event",
			},
			wantErr: true,
		},
		{
			name: "wrong specversion",
			event: Event{
				SpecVersion: "2.0",
				ID:          "abc",
				Source:      "urn:tas:service:test",
				Type:        "com.tas.test.event",
			},
			wantErr: true,
		},
		{
			name: "missing id",
			event: Event{
				SpecVersion: "1.0",
				Source:      "urn:tas:service:test",
				Type:        "com.tas.test.event",
			},
			wantErr: true,
		},
		{
			name: "missing source",
			event: Event{
				SpecVersion: "1.0",
				ID:          "abc",
				Type:        "com.tas.test.event",
			},
			wantErr: true,
		},
		{
			name: "missing type",
			event: Event{
				SpecVersion: "1.0",
				ID:          "abc",
				Source:      "urn:tas:service:test",
			},
			wantErr: true,
		},
		{
			name: "type with whitespace",
			event: Event{
				SpecVersion: "1.0",
				ID:          "abc",
				Source:      "urn:tas:service:test",
				Type:        "com.tas.test event",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.event.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestJSONRoundTrip(t *testing.T) {
	original := New("com.tas.activity.document.uploaded", "urn:tas:service:audimodal",
		WithTenant("t-acme", "sp-default"),
		WithUser("u-alice"),
		WithRequest("r-xyz"),
		WithSubject("file-abc123"),
		WithSeverity(SeverityCritical),
		WithFrameworks("HIPAA", "PCI_DSS"),
		WithTime(time.Date(2026, 4, 16, 18, 0, 0, 0, time.UTC)),
		WithData(map[string]string{"file_id": "file-abc123"}),
	)

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Verify CE field names on the wire (not Go struct names)
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}

	requiredFields := []string{"specversion", "id", "source", "type"}
	for _, f := range requiredFields {
		if _, ok := raw[f]; !ok {
			t.Errorf("missing required CE field %q in JSON", f)
		}
	}

	// Extension attrs must be lowercase, no underscore
	extensionFields := []string{"tenantid", "spaceid", "userid", "requestid", "severity", "frameworks"}
	for _, f := range extensionFields {
		if _, ok := raw[f]; !ok {
			t.Errorf("missing extension field %q in JSON", f)
		}
	}

	// Must NOT have snake_case variants
	forbidden := []string{"tenant_id", "space_id", "user_id", "request_id", "event_id", "event_type", "source_service", "schema_version"}
	for _, f := range forbidden {
		if _, ok := raw[f]; ok {
			t.Errorf("found forbidden snake_case field %q in JSON — CE extension attrs cannot contain underscores", f)
		}
	}

	// Round-trip
	var decoded Event
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.SpecVersion != original.SpecVersion {
		t.Errorf("specversion = %q, want %q", decoded.SpecVersion, original.SpecVersion)
	}
	if decoded.ID != original.ID {
		t.Errorf("id mismatch")
	}
	if decoded.TenantID != original.TenantID {
		t.Errorf("tenantid = %q, want %q", decoded.TenantID, original.TenantID)
	}
	if decoded.Severity != original.Severity {
		t.Errorf("severity = %q, want %q", decoded.Severity, original.Severity)
	}
	if len(decoded.Frameworks) != 2 {
		t.Errorf("frameworks length = %d, want 2", len(decoded.Frameworks))
	}
}

func TestSeverityIsValid(t *testing.T) {
	tests := []struct {
		s    Severity
		want bool
	}{
		{SeverityCritical, true},
		{SeverityHigh, true},
		{SeverityMedium, true},
		{SeverityLow, true},
		{SeverityInfo, true},
		{"", true},
		{"unknown", false},
	}
	for _, tt := range tests {
		if got := tt.s.IsValid(); got != tt.want {
			t.Errorf("Severity(%q).IsValid() = %v, want %v", tt.s, got, tt.want)
		}
	}
}
