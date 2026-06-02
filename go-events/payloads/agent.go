package payloads

const (
	TypeAgentCreated  = "com.tas.activity.agent.created"
	TypeAgentExecuted = "com.tas.activity.agent.executed"
	TypeAgentFailed   = "com.tas.activity.agent.failed"
)

type AgentCreated struct {
	AgentID   string `json:"agent_id"`
	AgentName string `json:"agent_name"`
	Template  string `json:"template,omitempty"`
}

type AgentExecuted struct {
	AgentID      string `json:"agent_id"`
	AgentName    string `json:"agent_name,omitempty"`
	DurationMS   int64  `json:"duration_ms,omitempty"`
	ToolCalls    int    `json:"tool_calls,omitempty"`
	LLMCalls     int    `json:"llm_calls,omitempty"`
	TokensUsed   int    `json:"tokens_used,omitempty"`
}

type AgentFailed struct {
	AgentID   string `json:"agent_id"`
	AgentName string `json:"agent_name,omitempty"`
	Stage     string `json:"stage,omitempty"`
	Error     string `json:"error"`
}
