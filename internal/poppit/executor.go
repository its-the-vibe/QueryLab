package poppit

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/its-the-vibe/QueryLab/internal/config"
	"github.com/redis/go-redis/v9"
)

type Executor struct {
	redisClient       *redis.Client
	notificationList  string
	commandOutputChan string
	repo              string
	branch            string
	commandType       string
	dir               string
	source            string
	commandTimeout    time.Duration
}

type notification struct {
	Repo     string                 `json:"repo"`
	Branch   string                 `json:"branch"`
	Type     string                 `json:"type"`
	Dir      string                 `json:"dir"`
	Commands []string               `json:"commands"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

type commandOutput struct {
	Metadata   map[string]interface{} `json:"metadata"`
	Type       string                 `json:"type"`
	Command    string                 `json:"command"`
	Output     string                 `json:"output"`
	Stderr     string                 `json:"stderr"`
	StatusCode int                    `json:"status_code"`
}

// NewExecutor creates a Poppit command executor backed by Redis.
func NewExecutor(cfg config.Config) *Executor {
	return &Executor{
		redisClient: redis.NewClient(&redis.Options{
			Addr:     fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port),
			Password: cfg.Redis.Password,
		}),
		notificationList:  cfg.Poppit.NotificationList,
		commandOutputChan: cfg.Poppit.CommandOutputChannel,
		repo:              cfg.Poppit.Repo,
		branch:            cfg.Poppit.Branch,
		commandType:       cfg.Poppit.Type,
		dir:               cfg.Poppit.Dir,
		source:            cfg.Poppit.Source,
		commandTimeout:    time.Duration(cfg.Poppit.CommandTimeoutSeconds) * time.Second,
	}
}

// Close releases the executor's Redis connection.
func (e *Executor) Close() error {
	return e.redisClient.Close()
}

func (e *Executor) buildNotification(command, taskID string) notification {
	return notification{
		Repo:     e.repo,
		Branch:   e.branch,
		Type:     e.commandType,
		Dir:      e.dir,
		Commands: []string{command},
		Metadata: map[string]interface{}{
			"taskId": taskID,
			"source": e.source,
			"spam":   false,
		},
	}
}

// Execute sends a command through Poppit and waits for its matching output.
func (e *Executor) Execute(ctx context.Context, command string) ([]byte, error) {
	taskID := fmt.Sprintf("querylab-%d", time.Now().UnixNano())

	subCtx, subCancel := context.WithTimeout(ctx, 5*time.Second)
	defer subCancel()

	pubsub := e.redisClient.Subscribe(subCtx, e.commandOutputChan)
	if _, err := pubsub.Receive(subCtx); err != nil {
		_ = pubsub.Close()
		return nil, fmt.Errorf("subscribing to poppit output: %w", err)
	}
	defer pubsub.Close()

	payload, err := json.Marshal(e.buildNotification(command, taskID))
	if err != nil {
		return nil, fmt.Errorf("marshalling poppit notification: %w", err)
	}

	if err := e.redisClient.RPush(ctx, e.notificationList, payload).Err(); err != nil {
		return nil, fmt.Errorf("sending poppit notification: %w", err)
	}

	waitCtx, waitCancel := context.WithTimeout(ctx, e.commandTimeout)
	defer waitCancel()

	for {
		msg, err := pubsub.ReceiveMessage(waitCtx)
		if err != nil {
			return nil, fmt.Errorf("waiting for poppit command output: %w", err)
		}

		var output commandOutput
		if err := json.Unmarshal([]byte(msg.Payload), &output); err != nil {
			continue
		}
		outputTaskID, ok := output.Metadata["taskId"].(string)
		if !ok || outputTaskID != taskID || output.Command != command {
			continue
		}
		if output.StatusCode != 0 {
			return nil, fmt.Errorf("poppit command failed (%d): %s", output.StatusCode, strings.TrimSpace(output.Stderr))
		}
		return []byte(output.Output), nil
	}
}
