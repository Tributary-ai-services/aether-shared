package payloads

const (
	TypeMCPToolInvoked = "com.tas.activity.mcp.tool_invoked"
)

type MCPToolInvoked struct {
	ServerID   string `json:"server_id"`
	ServerName string `json:"server_name,omitempty"`
	ToolName   string `json:"tool_name"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	Success    bool   `json:"success"`
	Error      string `json:"error,omitempty"`
}
