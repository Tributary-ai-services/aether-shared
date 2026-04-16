// Package topics is the single source of truth for TAS Kafka topic names.
package topics

// Activity topics — one per event family.
const (
	ActivityDocuments = "tas.activity.documents"
	ActivityLLM       = "tas.activity.llm"
	ActivityAgents    = "tas.activity.agents"
	ActivityWorkflows = "tas.activity.workflows"
	ActivityMCP       = "tas.activity.mcp"
	ActivitySecurity  = "tas.activity.security"
)

// Compliance topics — Gatekeeper fan-out.
const (
	ComplianceFindings        = "tas.compliance.findings"
	ComplianceFindingsCritical = "tas.compliance.findings.critical"
	ComplianceFindingsHIPAA   = "tas.compliance.findings.hipaa"
	ComplianceFindingsPCI     = "tas.compliance.findings.pci"
	ComplianceFindingsNIST    = "tas.compliance.findings.nist"
	ComplianceFindingsSOC2    = "tas.compliance.findings.soc2"
	ComplianceFindingsEUAI    = "tas.compliance.findings.eu_ai"
	ComplianceFindingsISO27001 = "tas.compliance.findings.iso27001"
	ComplianceActions         = "tas.compliance.actions"
	ComplianceAudit           = "tas.compliance.audit"
)

// AllTopics returns every topic name for declarative creation.
func AllTopics() []string {
	return []string{
		ActivityDocuments,
		ActivityLLM,
		ActivityAgents,
		ActivityWorkflows,
		ActivityMCP,
		ActivitySecurity,
		ComplianceFindings,
		ComplianceFindingsCritical,
		ComplianceFindingsHIPAA,
		ComplianceFindingsPCI,
		ComplianceFindingsNIST,
		ComplianceFindingsSOC2,
		ComplianceFindingsEUAI,
		ComplianceFindingsISO27001,
		ComplianceActions,
		ComplianceAudit,
	}
}

// ConsumerTopics returns topics the aether-be live-feed consumer should
// subscribe to. Framework-specific fan-out topics are excluded because
// they duplicate findings already received via ComplianceFindings.
func ConsumerTopics() []string {
	return []string{
		ActivityDocuments,
		ActivityLLM,
		ActivityAgents,
		ActivityWorkflows,
		ActivityMCP,
		ActivitySecurity,
		ComplianceFindings,
		ComplianceActions,
	}
}
