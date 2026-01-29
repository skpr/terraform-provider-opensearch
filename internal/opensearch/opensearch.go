package opensearch

const (
	TaskStateCompleted = "COMPLETED"
	TaskStateFailed    = "FAILED"
)

type ModelGroupCreateRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type ModelGroupCreateResponse struct {
	ModelGroupID string `json:"model_group_id,omitempty"`
}

type ConnectorCreateResponse struct {
	ConnectorID string `json:"connector_id,omitempty"`
}

type ModelRegisterResponse struct {
	TaskID string `json:"task_id"`
	Status string `json:"status"`
}

type TaskGetResponse struct {
	TaskID   string         `json:"task_id,omitempty"`
	State    string         `json:"state,omitempty"`
	ModelID  string         `json:"model_id,omitempty"`
	Response map[string]any `json:"response,omitempty"`
}

type ModelGetResponse struct {
	ModelID string `json:"model_id,omitempty"`
}
