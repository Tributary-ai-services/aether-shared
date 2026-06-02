package payloads

const (
	TypeWorkflowStarted       = "com.tas.activity.workflow.started"
	TypeWorkflowStepCompleted = "com.tas.activity.workflow.step_completed"
	TypeWorkflowCompleted     = "com.tas.activity.workflow.completed"
	TypeWorkflowFailed        = "com.tas.activity.workflow.failed"
)

type WorkflowStarted struct {
	WorkflowID   string `json:"workflow_id"`
	WorkflowName string `json:"workflow_name"`
	StepCount    int    `json:"step_count,omitempty"`
}

type WorkflowStepCompleted struct {
	WorkflowID string `json:"workflow_id"`
	StepName   string `json:"step_name"`
	StepIndex  int    `json:"step_index"`
	DurationMS int64  `json:"duration_ms,omitempty"`
}

type WorkflowCompleted struct {
	WorkflowID   string `json:"workflow_id"`
	WorkflowName string `json:"workflow_name,omitempty"`
	DurationMS   int64  `json:"duration_ms,omitempty"`
	StepsRun     int    `json:"steps_run,omitempty"`
}

type WorkflowFailed struct {
	WorkflowID   string `json:"workflow_id"`
	WorkflowName string `json:"workflow_name,omitempty"`
	FailedStep   string `json:"failed_step,omitempty"`
	Error        string `json:"error"`
}
