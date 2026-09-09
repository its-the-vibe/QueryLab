package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Server struct {
		Addr string
	}
	Poppit struct {
		Directory string
	}
}

func loadConfig() (*Config, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("/")

	viper.SetDefault("server.addr", ":8080")
	viper.SetDefault("poppit.directory", ".")

	viper.AutomaticEnv()
	viper.BindEnv("server.addr", "SERVER_ADDR")
	viper.BindEnv("poppit.directory", "POPPIT_DIRECTORY")

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

func discoverQueries(ctx context.Context, directory string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "./goquery", "--json", "list")
	cmd.Dir = directory
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("running goquery list: %w: %s", err, strings.TrimSpace(string(output)))
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

func runQuery(ctx context.Context, directory, query string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "./goquery", "--json", "query", query)
	cmd.Dir = directory
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("running goquery query %q: %w: %s", query, err, strings.TrimSpace(string(output)))
	}
	return output, nil
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

	startupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	queries, err := discoverQueries(startupCtx, cfg.Poppit.Directory)
	if err != nil {
		log.Fatalf("failed to discover queries: %v", err)
	}

	tmpl := template.Must(template.New("page").Parse(pageTemplate))
	available := make(map[string]struct{}, len(queries))
	for _, query := range queries {
		available[query] = struct{}{}
	}

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

		output, err := runQuery(ctx, cfg.Poppit.Directory, query)
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
