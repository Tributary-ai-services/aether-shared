package resilience

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAffinityZeroIsValidAndMeansOff(t *testing.T) {
	var a Affinity
	if err := a.Validate(); err != nil {
		t.Fatalf("zero affinity rejected: %v", err)
	}
	if !a.IsZero() {
		t.Fatal("zero affinity should report IsZero")
	}
	if (Affinity{KeySource: "conversation"}).IsZero() {
		t.Fatal("configured affinity reported as zero")
	}
}

func TestAffinityAcceptsTheExperimentRunnersKeys(t *testing.T) {
	// Reusing that enum is the point: a second vocabulary for the same idea,
	// with the same fallbacks, would drift.
	for _, k := range AffinityKeySources {
		if err := (Affinity{KeySource: k}).Validate(); err != nil {
			t.Errorf("key_source %q rejected: %v", k, err)
		}
	}
	if err := (Affinity{KeySource: "session"}).Validate(); err == nil {
		t.Fatal("an invented key source was accepted")
	}
}

func TestAffinityRejectsUnknownScopeAndOnBreak(t *testing.T) {
	if err := (Affinity{KeySource: "conversation", Scope: "vendor+region"}).Validate(); err == nil {
		t.Fatal("unknown scope accepted")
	}
	if err := (Affinity{KeySource: "conversation", OnBreak: "explode"}).Validate(); err == nil {
		t.Fatal("unknown on_break accepted")
	}
	for _, s := range AffinityScopes {
		if err := (Affinity{KeySource: "conversation", Scope: s}).Validate(); err != nil {
			t.Errorf("scope %q rejected: %v", s, err)
		}
	}
	for _, b := range AffinityOnBreakValues {
		if err := (Affinity{KeySource: "conversation", OnBreak: b}).Validate(); err != nil {
			t.Errorf("on_break %q rejected: %v", b, err)
		}
	}
}

// Beyond an hour affinity is no longer protecting a vendor cache — the longest
// vendor TTL available is an hour — it is just pinning traffic.
func TestAffinityTTLIsBounded(t *testing.T) {
	if err := (Affinity{KeySource: "conversation", TTLSeconds: MaxAffinityTTLSeconds + 1}).Validate(); err == nil {
		t.Fatal("an over-long TTL was accepted")
	}
	if err := (Affinity{KeySource: "conversation", TTLSeconds: -1}).Validate(); err == nil {
		t.Fatal("a negative TTL was accepted")
	}
	if err := (Affinity{KeySource: "conversation", TTLSeconds: 300}).Validate(); err != nil {
		t.Fatalf("a normal TTL was rejected: %v", err)
	}
}

// Settings that only take effect alongside key_source are refused rather than
// silently ignored: someone who set them clearly intended affinity to be on.
func TestSettingsWithoutKeySourceAreRefused(t *testing.T) {
	err := (Affinity{Scope: "vendor+model", TTLSeconds: 300}).Validate()
	if err == nil {
		t.Fatal("affinity settings without key_source were silently accepted")
	}
	if !strings.Contains(err.Error(), "none of them apply") {
		t.Fatalf("error %q should explain that the settings have no effect", err)
	}
}

func TestAffinityOmitsUnsetFields(t *testing.T) {
	b, err := json.Marshal(Affinity{KeySource: "conversation"})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"key_source":"conversation"}` {
		t.Fatalf("partial affinity marshalled as %s; unset fields must be omitted", b)
	}
}
