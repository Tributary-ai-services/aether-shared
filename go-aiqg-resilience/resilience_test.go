package resilience

import (
	"encoding/json"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// The reason this module exists: one set of rules, applied at write time by
// the control plane and at run time by the gateway.
// ---------------------------------------------------------------------------

func TestZeroIsValidAndMeansUnset(t *testing.T) {
	if err := (Health{}).Validate(); err != nil {
		t.Fatalf("zero Health rejected: %v", err)
	}
	if err := (Budgets{}).Validate(); err != nil {
		t.Fatalf("zero Budgets rejected: %v", err)
	}
	if !(Health{}).IsZero() || !(Budgets{}).IsZero() {
		t.Fatal("zero values must report IsZero so callers can omit the block")
	}
	if (Health{EjectForSeconds: 30}).IsZero() {
		t.Fatal("a partially-set block is not zero")
	}
}

// A route rule that changes only one setting must be expressible; everything
// else inherits. Serialising all-defaults would read as deliberate config.
func TestPartialConfigOmitsUnsetFields(t *testing.T) {
	b, err := json.Marshal(Health{EjectForSeconds: 30})
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if got != `{"eject_for_seconds":30}` {
		t.Fatalf("partial Health marshalled as %s; unset fields must be omitted", got)
	}
}

// Durations cross this boundary as integer seconds. time.Duration would
// marshal as nanoseconds — unreadable in a JSONB row and silently wrong to
// anyone who assumes seconds.
func TestSecondsRoundTripLegibly(t *testing.T) {
	var h Health
	if err := json.Unmarshal([]byte(`{"window_seconds":30,"eject_for_seconds":60}`), &h); err != nil {
		t.Fatal(err)
	}
	if h.WindowSeconds != 30 || h.EjectForSeconds != 60 {
		t.Fatalf("got %+v, want plain seconds", h)
	}
}

func TestHealthValidate(t *testing.T) {
	cases := []struct {
		name    string
		h       Health
		wantErr bool
	}{
		{"typical", Health{ConsecutiveErrors: 5, ErrorRatePercent: 50, MinRequests: 20, WindowSeconds: 30, EjectForSeconds: 30}, false},
		{"negative consecutive", Health{ConsecutiveErrors: -1}, true},
		{"rate over 100", Health{ErrorRatePercent: 101}, true},
		{"negative rate", Health{ErrorRatePercent: -5}, true},
		{"window too long", Health{WindowSeconds: MaxWindowSeconds + 1}, true},
		{"eject too long", Health{EjectForSeconds: MaxEjectForSeconds + 1}, true},
		{"negative eject", Health{EjectForSeconds: -5}, true},
		// The silent trap: a rate rule acting on a sample of 1 or 2 ejects a
		// healthy target and gives the operator no reason to suspect why.
		{"rate on a meaningless sample", Health{ErrorRatePercent: 50, MinRequests: 2}, true},
		{"rate with a real sample", Health{ErrorRatePercent: 50, MinRequests: 20}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := c.h.Validate(); (err != nil) != c.wantErr {
				t.Fatalf("Validate() = %v, wantErr %v", err, c.wantErr)
			}
		})
	}
}

func TestBudgetsValidate(t *testing.T) {
	cases := []struct {
		name    string
		b       Budgets
		wantErr bool
	}{
		{"typical", Budgets{RetryRatio: 0.1, MinRetries: 3, MaxAttempts: 3}, false},
		{"ratio above 1", Budgets{RetryRatio: 1.5}, true},
		{"negative ratio", Budgets{RetryRatio: -0.1}, true},
		{"ratio of exactly 1 is allowed", Budgets{RetryRatio: 1}, false},
		{"negative min", Budgets{MinRetries: -1}, true},
		{"attempts over ceiling", Budgets{MaxAttempts: MaxAttemptsCeiling + 1}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := c.b.Validate(); (err != nil) != c.wantErr {
				t.Fatalf("Validate() = %v, wantErr %v", err, c.wantErr)
			}
		})
	}
}

// The most likely operator error is entering a percentage where a fraction is
// wanted. The message has to say so, or "invalid retry_ratio" sends them to
// the docs for something the error could have explained.
func TestRatioErrorExplainsTheUnit(t *testing.T) {
	err := Budgets{RetryRatio: 10}.Validate()
	if err == nil {
		t.Fatal("retry_ratio of 10 accepted")
	}
	if !strings.Contains(err.Error(), "fraction") {
		t.Fatalf("error %q does not explain that the field is a fraction, not a percentage", err)
	}
}

// An operator fixing a form wants the whole list, not one round trip per
// mistake.
func TestValidateReportsEveryProblemAtOnce(t *testing.T) {
	err := Health{ErrorRatePercent: 200, EjectForSeconds: -1, ConsecutiveErrors: -1}.Validate()
	if err == nil {
		t.Fatal("expected errors")
	}
	for _, want := range []string{"error_rate_percent", "eject_for_seconds", "consecutive_errors"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q omits %s", err, want)
		}
	}
}
