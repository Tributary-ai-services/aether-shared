package payloads

const (
	TypeComplianceFindingDetected = "com.tas.compliance.finding.detected"
	TypeComplianceActionExecuted  = "com.tas.compliance.action.executed"
	TypeComplianceAuditRecorded   = "com.tas.compliance.audit.recorded"
)

type ComplianceFinding struct {
	FindingID   string `json:"finding_id"`
	PatternID   string `json:"pattern_id"`
	PatternType string `json:"pattern_type"`
	Source      string `json:"source"`
	ValueHash   string `json:"value_hash"`
	ValuePreview string `json:"value_preview,omitempty"`
	ActionTaken string `json:"action_taken,omitempty"`
	Redacted    bool   `json:"redacted"`
	Tokenized   bool   `json:"tokenized"`
}

type ComplianceAction struct {
	ActionID   string `json:"action_id"`
	FindingID  string `json:"finding_id"`
	RuleID     string `json:"rule_id"`
	ActionType string `json:"action_type"`
	Success    bool   `json:"success"`
	Error      string `json:"error,omitempty"`
}

type ComplianceAudit struct {
	AuditID  string                 `json:"audit_id"`
	Action   string                 `json:"action"`
	Resource string                 `json:"resource"`
	Details  map[string]interface{} `json:"details,omitempty"`
}
