package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/spf13/viper"
)

type Config struct {
	Server struct {
		Addr string
	}
	Redis struct {
		Host     string
		Port     int
		Password string
	}
	Poppit struct {
		Repo                 string
		Branch               string
		Type                 string
		Dir                  string
		Source               string
		NotificationList     string `mapstructure:"notification_list"`
		CommandOutputChannel string `mapstructure:"command_output_channel"`
		CommandTimeoutSecs   int    `mapstructure:"command_timeout_seconds"`
	}
	Schema struct {
		AllowedTables []struct {
			Dataset string `mapstructure:"dataset"`
			Table   string `mapstructure:"table"`
		} `mapstructure:"allowed_tables"`
	}
}

func loadConfig() (*Config, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("/")

	viper.SetDefault("server.addr", ":8080")
	viper.SetDefault("redis.host", "localhost")
	viper.SetDefault("redis.port", 6379)
	viper.SetDefault("poppit.repo", "its-the-vibe/QueryLab")
	viper.SetDefault("poppit.branch", "refs/heads/main")
	viper.SetDefault("poppit.type", "querylab-web")
	viper.SetDefault("poppit.dir", "/tmp")
	viper.SetDefault("poppit.source", "querylab")
	viper.SetDefault("poppit.notification_list", "poppit:notifications")
	viper.SetDefault("poppit.command_output_channel", "poppit:command-output")
	viper.SetDefault("poppit.command_timeout_seconds", 30)

	viper.AutomaticEnv()
	_ = viper.BindEnv("server.addr", "SERVER_ADDR")
	_ = viper.BindEnv("redis.host", "REDIS_HOST")
	_ = viper.BindEnv("redis.port", "REDIS_PORT")
	_ = viper.BindEnv("redis.password", "REDIS_PASSWORD")
	_ = viper.BindEnv("poppit.repo", "POPPIT_REPO")
	_ = viper.BindEnv("poppit.branch", "POPPIT_BRANCH")
	_ = viper.BindEnv("poppit.type", "POPPIT_TYPE")
	_ = viper.BindEnv("poppit.dir", "POPPIT_DIR")
	_ = viper.BindEnv("poppit.source", "POPPIT_SOURCE")
	_ = viper.BindEnv("poppit.notification_list", "POPPIT_SERVICE_REDIS_LIST_NAME")
	_ = viper.BindEnv("poppit.command_output_channel", "POPPIT_SERVICE_COMMAND_OUTPUT_CHANNEL")
	_ = viper.BindEnv("poppit.command_timeout_seconds", "POPPIT_COMMAND_TIMEOUT_SECONDS")

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("reading config: %w", err)
		}
		log.Println("No config file found, using defaults and environment variables")
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshalling config: %w", err)
	}
	return &cfg, nil
}

type commandExecutor interface {
	Execute(ctx context.Context, command string) ([]byte, error)
}

type poppitExecutor struct {
	redisClient         *redis.Client
	notificationList    string
	commandOutputChan   string
	repo                string
	branch              string
	commandType         string
	dir                 string
	source              string
	commandTimeout      time.Duration
}

type poppitNotification struct {
	Repo     string            `json:"repo"`
	Branch   string            `json:"branch"`
	Type     string            `json:"type"`
	Dir      string            `json:"dir"`
	Commands []string          `json:"commands"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type poppitCommandOutput struct {
	Metadata   map[string]string `json:"metadata"`
	Type       string            `json:"type"`
	Command    string            `json:"command"`
	Output     string            `json:"output"`
	Stderr     string            `json:"stderr"`
	StatusCode int               `json:"status_code"`
}

func newPoppitExecutor(cfg *Config) *poppitExecutor {
	return &poppitExecutor{
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
		commandTimeout:    time.Duration(cfg.Poppit.CommandTimeoutSecs) * time.Second,
	}
}

func (e *poppitExecutor) Close() error {
	return e.redisClient.Close()
}

func (e *poppitExecutor) Execute(ctx context.Context, command string) ([]byte, error) {
	taskID := fmt.Sprintf("querylab-%d", time.Now().UnixNano())

	subCtx, subCancel := context.WithTimeout(ctx, 5*time.Second)
	defer subCancel()

	pubsub := e.redisClient.Subscribe(subCtx, e.commandOutputChan)
	if _, err := pubsub.Receive(subCtx); err != nil {
		_ = pubsub.Close()
		return nil, fmt.Errorf("subscribing to poppit output: %w", err)
	}
	defer pubsub.Close()

	notification := poppitNotification{
		Repo:     e.repo,
		Branch:   e.branch,
		Type:     e.commandType,
		Dir:      e.dir,
		Commands: []string{command},
		Metadata: map[string]string{
			"taskId": taskID,
			"source": e.source,
		},
	}

	payload, err := json.Marshal(notification)
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

		var output poppitCommandOutput
		if err := json.Unmarshal([]byte(msg.Payload), &output); err != nil {
			continue
		}
		if output.Metadata["taskId"] != taskID {
			continue
		}
		if output.Command != command {
			continue
		}
		if output.StatusCode != 0 {
			return nil, fmt.Errorf("poppit command failed (%d): %s", output.StatusCode, strings.TrimSpace(output.Stderr))
		}
		return []byte(output.Output), nil
	}
}

func discoverQueries(ctx context.Context, executor commandExecutor) ([]string, error) {
	output, err := executor.Execute(ctx, "./goquery --json list")
	if err != nil {
		return nil, fmt.Errorf("running goquery list via poppit: %w", err)
	}

	queries, err := parseQueryList(output)
	if err != nil {
		return nil, fmt.Errorf("parsing goquery list output: %w", err)
	}

	sort.Strings(queries)
	return queries, nil
}

func parseQueryList(output []byte) ([]string, error) {
	var parsed any
	if err := json.Unmarshal(output, &parsed); err != nil {
		return nil, err
	}

	var out []string
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v != "" {
			out = append(out, v)
		}
	}

	switch typed := parsed.(type) {
	case []any:
		for _, item := range typed {
			switch t := item.(type) {
			case string:
				add(t)
			case map[string]any:
				if n, ok := t["name"].(string); ok {
					add(n)
				} else if q, ok := t["query"].(string); ok {
					add(q)
				}
			}
		}
	case map[string]any:
		for _, key := range []string{"queries", "data", "results"} {
			entries, ok := typed[key].([]any)
			if !ok {
				continue
			}
			for _, item := range entries {
				switch t := item.(type) {
				case string:
					add(t)
				case map[string]any:
					if n, ok := t["name"].(string); ok {
						add(n)
					} else if q, ok := t["query"].(string); ok {
						add(q)
					}
				}
			}
		}
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("no executable queries found")
	}

	unique := make(map[string]struct{}, len(out))
	result := make([]string, 0, len(out))
	for _, q := range out {
		if _, ok := unique[q]; ok {
			continue
		}
		unique[q] = struct{}{}
		result = append(result, q)
	}
	return result, nil
}

func runQuery(ctx context.Context, executor commandExecutor, query string) ([]byte, error) {
	command := fmt.Sprintf("./goquery --json query %s", shellQuote(query))
	output, err := executor.Execute(ctx, command)
	if err != nil {
		return nil, fmt.Errorf("running goquery query %q via poppit: %w", query, err)
	}
	return output, nil
}

func runSchema(ctx context.Context, executor commandExecutor, dataset, table string) ([]byte, error) {
	command := fmt.Sprintf("./goquery --json schema %s %s", shellQuote(dataset), shellQuote(table))
	output, err := executor.Execute(ctx, command)
	if err != nil {
		return nil, fmt.Errorf("running goquery schema %q.%q via poppit: %w", dataset, table, err)
	}
	return output, nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func parseRows(output []byte) ([]map[string]any, error) {
	var parsed any
	if err := json.Unmarshal(output, &parsed); err != nil {
		return nil, err
	}

	rows := convertRows(parsed)
	if len(rows) == 0 {
		return nil, fmt.Errorf("query output did not contain tabular rows")
	}
	return rows, nil
}

func parseSchema(output []byte) ([]map[string]any, error) {
	var parsed any
	if err := json.Unmarshal(output, &parsed); err != nil {
		return nil, err
	}

	fields, ok := parsed.([]any)
	if !ok || len(fields) == 0 {
		return nil, fmt.Errorf("schema output did not contain fields")
	}

	result := make([]map[string]any, 0, len(fields))
	for _, field := range fields {
		record, ok := field.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("schema output contained a non-object field")
		}
		name, hasName := record["name"].(string)
		typ, hasType := record["type"].(string)
		if !hasName || strings.TrimSpace(name) == "" || !hasType || strings.TrimSpace(typ) == "" {
			return nil, fmt.Errorf("schema output contained a field without required name/type")
		}
		if mode, ok := record["mode"]; ok {
			if _, modeIsString := mode.(string); !modeIsString {
				return nil, fmt.Errorf("schema output contained a field with non-string mode")
			}
		}
		if description, ok := record["description"]; ok {
			if _, descriptionIsString := description.(string); !descriptionIsString {
				return nil, fmt.Errorf("schema output contained a field with non-string description")
			}
		}
		result = append(result, record)
	}
	return result, nil
}

func buildAllowedSchemaTables(entries []struct {
	Dataset string `mapstructure:"dataset"`
	Table   string `mapstructure:"table"`
}) map[string]map[string]struct{} {
	allowed := make(map[string]map[string]struct{})
	for _, entry := range entries {
		dataset := strings.TrimSpace(entry.Dataset)
		table := strings.TrimSpace(entry.Table)
		if dataset == "" || table == "" {
			continue
		}
		if _, ok := allowed[dataset]; !ok {
			allowed[dataset] = make(map[string]struct{})
		}
		allowed[dataset][table] = struct{}{}
	}
	return allowed
}

func isAllowedSchemaTable(allowed map[string]map[string]struct{}, dataset, table string) bool {
	tables, datasetAllowed := allowed[dataset]
	if !datasetAllowed {
		return false
	}
	_, tableAllowed := tables[table]
	return tableAllowed
}

func convertRows(parsed any) []map[string]any {
	switch typed := parsed.(type) {
	case []any:
		rows := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			record, ok := item.(map[string]any)
			if ok {
				rows = append(rows, record)
				continue
			}
			rows = append(rows, map[string]any{"value": item})
		}
		return rows
	case map[string]any:
		for _, key := range []string{"rows", "results", "data"} {
			if nested, ok := typed[key]; ok {
				if rows := convertRows(nested); len(rows) > 0 {
					return rows
				}
			}
		}
	}
	return nil
}

func buildTable(rows []map[string]any) ([]string, [][]string) {
	columnSet := make(map[string]struct{})
	for _, row := range rows {
		for col := range row {
			columnSet[col] = struct{}{}
		}
	}

	columns := make([]string, 0, len(columnSet))
	for col := range columnSet {
		columns = append(columns, col)
	}
	sort.Strings(columns)

	tableRows := make([][]string, 0, len(rows))
	for _, row := range rows {
		line := make([]string, 0, len(columns))
		for _, col := range columns {
			line = append(line, fmt.Sprintf("%v", row[col]))
		}
		tableRows = append(tableRows, line)
	}
	return columns, tableRows
}

type pageData struct {
	Queries  []string
	Selected string
	Columns  []string
	Rows     [][]string
	Error    string
}

const pageTemplate = `
<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>QueryLab</title>
  <style>
    body { font-family: sans-serif; margin: 2rem; }
    table { border-collapse: collapse; margin-top: 1rem; min-width: 50%; }
    th, td { border: 1px solid #ccc; padding: 0.4rem 0.6rem; text-align: left; }
    .error { color: #b00020; margin-top: 1rem; }
  </style>
</head>
<body>
  <h1>QueryLab</h1>
  <form method="post" action="/execute">
    <label for="query">Available query</label>
    <select id="query" name="query" required>
      {{range .Queries}}
        <option value="{{.}}" {{if eq $.Selected .}}selected{{end}}>{{.}}</option>
      {{end}}
    </select>
    <button type="submit">Run</button>
  </form>
  {{if .Error}}<div class="error">{{.Error}}</div>{{end}}
  {{if .Rows}}
    <table>
      <thead><tr>{{range .Columns}}<th>{{.}}</th>{{end}}</tr></thead>
      <tbody>
        {{range .Rows}}
          <tr>{{range .}}<td>{{.}}</td>{{end}}</tr>
        {{end}}
      </tbody>
    </table>
  {{end}}
</body>
</html>
`

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	executor := newPoppitExecutor(cfg)
	defer executor.Close()

	startupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	queries, err := discoverQueries(startupCtx, executor)
	if err != nil {
		log.Fatalf("failed to discover queries: %v", err)
	}

	tmpl := template.Must(template.New("page").Parse(pageTemplate))
	available := make(map[string]struct{}, len(queries))
	for _, query := range queries {
		available[query] = struct{}{}
	}
	allowedSchemaTables := buildAllowedSchemaTables(cfg.Schema.AllowedTables)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		_ = tmpl.Execute(w, pageData{Queries: queries})
	})

	mux.HandleFunc("/execute", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		query := strings.TrimSpace(r.FormValue("query"))
		data := pageData{Queries: queries, Selected: query}
		if _, ok := available[query]; !ok {
			data.Error = "invalid query selected"
			_ = tmpl.Execute(w, data)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		output, err := runQuery(ctx, executor, query)
		if err != nil {
			data.Error = err.Error()
			_ = tmpl.Execute(w, data)
			return
		}

		rows, err := parseRows(output)
		if err != nil {
			data.Error = fmt.Sprintf("failed to parse query result: %v", err)
			_ = tmpl.Execute(w, data)
			return
		}

		data.Columns, data.Rows = buildTable(rows)
		_ = tmpl.Execute(w, data)
	})

	mux.HandleFunc("/schema", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		dataset := strings.TrimSpace(r.FormValue("dataset"))
		table := strings.TrimSpace(r.FormValue("table"))
		if !isAllowedSchemaTable(allowedSchemaTables, dataset, table) {
			http.Error(w, "dataset/table is not allowed", http.StatusBadRequest)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), time.Duration(cfg.Poppit.CommandTimeoutSecs)*time.Second)
		defer cancel()

		output, err := runSchema(ctx, executor, dataset, table)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}

		schemaFields, err := parseSchema(output)
		if err != nil {
			log.Printf("failed to parse schema result for %s.%s: %v", dataset, table, err)
			http.Error(w, "failed to parse schema result", http.StatusBadGateway)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(schemaFields); err != nil {
			http.Error(w, "failed to encode schema result", http.StatusInternalServerError)
		}
	})

	srv := &http.Server{
		Addr:    cfg.Server.Addr,
		Handler: mux,
	}

	go func() {
		log.Printf("QueryLab listening on %s", cfg.Server.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server failed: %v", err)
		}
	}()

	stopCtx, stop := signalContext()
	<-stopCtx.Done()
	stop()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}
}

func signalContext() (context.Context, context.CancelFunc) {
	return signalNotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

var signalNotifyContext = func(parent context.Context, signals ...os.Signal) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(parent, signals...)
}
