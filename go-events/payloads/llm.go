package payloads

const (
	TypeLLMRequest  = "com.tas.activity.llm.request"
	TypeLLMResponse = "com.tas.activity.llm.response"
)

type LLMRequest struct {
	Model       string `json:"model"`
	Provider    string `json:"provider,omitempty"`
	TokensIn    int    `json:"tokens_in,omitempty"`
	MessageCount int   `json:"message_count,omitempty"`
}

type LLMResponse struct {
	Model      string  `json:"model"`
	Provider   string  `json:"provider,omitempty"`
	TokensIn   int     `json:"tokens_in,omitempty"`
	TokensOut  int     `json:"tokens_out,omitempty"`
	DurationMS int64   `json:"duration_ms,omitempty"`
	FinishReason string `json:"finish_reason,omitempty"`
}
