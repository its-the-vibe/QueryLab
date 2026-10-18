package poppit

import (
	"encoding/json"
	"testing"
)

func TestBuildNotificationIncludesSpamMetadata(t *testing.T) {
	t.Parallel()

	executor := &Executor{
		repo:        "its-the-vibe/QueryLab",
		branch:      "refs/heads/main",
		commandType: "querylab-web",
		dir:         "/tmp",
		source:      "querylab",
	}
	message := executor.buildNotification("./goquery --json list", "task-123")
	if message.Metadata["spam"] != false || message.Metadata["taskId"] != "task-123" {
		t.Fatalf("notification metadata = %#v, want boolean spam false and taskId", message.Metadata)
	}

	payload, err := json.Marshal(message)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var decoded struct {
		Metadata map[string]interface{} `json:"metadata"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if spam, ok := decoded.Metadata["spam"].(bool); !ok || spam {
		t.Fatalf("marshalled spam metadata = %v, want boolean false", decoded.Metadata["spam"])
	}
}

func TestCommandOutputAllowsBooleanMetadata(t *testing.T) {
	t.Parallel()

	var output commandOutput
	err := json.Unmarshal([]byte(`{"metadata":{"taskId":"task-123","spam":false},"command":"query","output":"ok"}`), &output)
	if err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if output.Metadata["taskId"] != "task-123" {
		t.Fatalf("taskId metadata = %v, want %q", output.Metadata["taskId"], "task-123")
	}
	if spam, ok := output.Metadata["spam"].(bool); !ok || spam {
		t.Fatalf("spam metadata = %v, want boolean false", output.Metadata["spam"])
	}
}
