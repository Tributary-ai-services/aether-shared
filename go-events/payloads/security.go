package payloads

const (
	TypeSecurityThreatDetected  = "com.tas.activity.security.threat_detected"
	TypeSecurityPolicyViolation = "com.tas.activity.security.policy_violation"
	TypeSecurityReviewCompleted = "com.tas.activity.security.review_completed"
)

type SecurityThreatDetected struct {
	ThreatType string `json:"threat_type"`
	Source     string `json:"source,omitempty"`
	Details    string `json:"details,omitempty"`
}

type SecurityPolicyViolation struct {
	PolicyID   string `json:"policy_id"`
	PolicyName string `json:"policy_name"`
	Resource   string `json:"resource,omitempty"`
	Details    string `json:"details,omitempty"`
}

type SecurityReviewCompleted struct {
	ReviewID string `json:"review_id"`
	Result   string `json:"result"`
	Details  string `json:"details,omitempty"`
}
