package tasevents

// Severity levels for compliance and security events. These are sent as the
// CloudEvents extension attribute "severity" (lowercase, no underscore).
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
	SeverityInfo     Severity = "info"
)

func (s Severity) IsValid() bool {
	switch s {
	case SeverityCritical, SeverityHigh, SeverityMedium, SeverityLow, SeverityInfo, "":
		return true
	}
	return false
}
