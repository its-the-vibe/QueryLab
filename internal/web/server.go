package web

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/its-the-vibe/QueryLab/internal/config"
)

// CommandExecutor runs commands requested by the web interface.
type CommandExecutor interface {
	Execute(ctx context.Context, command string) ([]byte, error)
}

func discoverQueries(ctx context.Context, executor CommandExecutor) ([]string, error) {
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

func runQuery(ctx context.Context, executor CommandExecutor, query string) ([]byte, error) {
	command := fmt.Sprintf("./goquery --json query %s", shellQuote(query))
	output, err := executor.Execute(ctx, command)
	if err != nil {
		return nil, fmt.Errorf("running goquery query %q via poppit: %w", query, err)
	}
	return output, nil
}

func runSchema(ctx context.Context, executor CommandExecutor, dataset, table string) ([]byte, error) {
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
	if !ok {
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

func buildAllowedSchemaTables(entries []config.AllowedTable) map[string]map[string]struct{} {
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

func buildAllowedOrigins(origins []string) map[string]struct{} {
	allowed := make(map[string]struct{})
	for _, origin := range origins {
		origin = strings.TrimSpace(origin)
		if origin == "" {
			continue
		}
		u, err := url.Parse(origin)
		if err != nil {
			continue
		}
		if !u.IsAbs() || strings.TrimSpace(u.Host) == "" {
			continue
		}
		scheme := strings.ToLower(u.Scheme)
		if scheme != "http" && scheme != "https" {
			continue
		}
		allowed[scheme+"://"+strings.ToLower(u.Host)] = struct{}{}
	}
	return allowed
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
	Queries               []string
	SelectedQuery         string
	SchemaDatasets        []string
	SchemaTablesByDataset map[string][]string
	SchemaTablesJSON      template.JS
	SelectedDataset       string
	SelectedTable         string
	Columns               []string
	Rows                  [][]string
	Error                 string
}

func buildSchemaSelections(allowedSchemaTables map[string]map[string]struct{}) ([]string, map[string][]string) {
	datasets := make([]string, 0, len(allowedSchemaTables))
	schemaTablesByDataset := make(map[string][]string, len(allowedSchemaTables))
	for dataset, tables := range allowedSchemaTables {
		datasets = append(datasets, dataset)
		tableNames := make([]string, 0, len(tables))
		for table := range tables {
			tableNames = append(tableNames, table)
		}
		sort.Strings(tableNames)
		schemaTablesByDataset[dataset] = tableNames
	}
	sort.Strings(datasets)
	return datasets, schemaTablesByDataset
}

func selectSchemaDatasetAndTable(schemaDatasets []string, schemaTablesByDataset map[string][]string, selectedDataset, selectedTable string) (string, string) {
	selectedDataset = strings.TrimSpace(selectedDataset)
	selectedTable = strings.TrimSpace(selectedTable)

	if selectedDataset == "" {
		if len(schemaDatasets) == 0 {
			return "", ""
		}
		selectedDataset = schemaDatasets[0]
	}

	tables := schemaTablesByDataset[selectedDataset]
	if len(tables) == 0 {
		return selectedDataset, ""
	}
	if selectedTable == "" {
		return selectedDataset, tables[0]
	}
	for _, table := range tables {
		if table == selectedTable {
			return selectedDataset, selectedTable
		}
	}
	return selectedDataset, tables[0]
}

// NewServer constructs the HTTP server and discovers the available queries.
func NewServer(cfg *config.Config, executor CommandExecutor) (*http.Server, error) {
	startupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	queries, err := discoverQueries(startupCtx, executor)
	if err != nil {
		return nil, fmt.Errorf("discovering queries: %w", err)
	}

	tmpl := template.Must(template.New("page").Parse(pageTemplate))
	available := make(map[string]struct{}, len(queries))
	for _, query := range queries {
		available[query] = struct{}{}
	}
	allowedSchemaTables := buildAllowedSchemaTables(cfg.Schema.AllowedTables)
	allowedSchemaOrigins := buildAllowedOrigins(cfg.Schema.AllowedOrigins)
	schemaDatasets, schemaTablesByDataset := buildSchemaSelections(allowedSchemaTables)
	schemaTablesJSONBytes, err := json.Marshal(schemaTablesByDataset)
	if err != nil {
		return nil, fmt.Errorf("marshalling schema table options: %w", err)
	}
	schemaTablesJSON := template.JS(string(schemaTablesJSONBytes))

	newPageData := func() pageData {
		selectedDataset, selectedTable := selectSchemaDatasetAndTable(schemaDatasets, schemaTablesByDataset, "", "")
		return pageData{
			Queries:               queries,
			SchemaDatasets:        schemaDatasets,
			SchemaTablesByDataset: schemaTablesByDataset,
			SchemaTablesJSON:      schemaTablesJSON,
			SelectedDataset:       selectedDataset,
			SelectedTable:         selectedTable,
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		_ = tmpl.Execute(w, newPageData())
	})

	mux.HandleFunc("/execute", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		query := strings.TrimSpace(r.FormValue("query"))
		data := newPageData()
		data.SelectedQuery = query
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

	mux.HandleFunc("/schema/execute", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form data", http.StatusBadRequest)
			return
		}

		dataset := strings.TrimSpace(r.PostForm.Get("dataset"))
		table := strings.TrimSpace(r.PostForm.Get("table"))
		data := newPageData()
		data.SelectedDataset, data.SelectedTable = selectSchemaDatasetAndTable(schemaDatasets, schemaTablesByDataset, dataset, table)

		if !isAllowedSchemaTable(allowedSchemaTables, dataset, table) {
			data.Error = "invalid dataset/table selected"
			_ = tmpl.Execute(w, data)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), time.Duration(cfg.Poppit.CommandTimeoutSeconds)*time.Second)
		defer cancel()

		output, err := runSchema(ctx, executor, dataset, table)
		if err != nil {
			data.Error = err.Error()
			_ = tmpl.Execute(w, data)
			return
		}

		schemaFields, err := parseSchema(output)
		if err != nil {
			log.Printf("failed to parse schema result for %s.%s: %v", dataset, table, err)
			data.Error = "failed to parse schema result"
			_ = tmpl.Execute(w, data)
			return
		}

		data.Columns, data.Rows = buildTable(schemaFields)
		_ = tmpl.Execute(w, data)
	})

	mux.HandleFunc("/schema", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !isTrustedRequestOrigin(r, allowedSchemaOrigins) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form data", http.StatusBadRequest)
			return
		}
		dataset := strings.TrimSpace(r.PostForm.Get("dataset"))
		table := strings.TrimSpace(r.PostForm.Get("table"))
		if !isAllowedSchemaTable(allowedSchemaTables, dataset, table) {
			http.Error(w, "dataset/table is not allowed", http.StatusBadRequest)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), time.Duration(cfg.Poppit.CommandTimeoutSeconds)*time.Second)
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

	return &http.Server{
		Addr:    cfg.Server.Addr,
		Handler: mux,
	}, nil
}

func isTrustedRequestOrigin(r *http.Request, allowedOrigins map[string]struct{}) bool {
	if len(allowedOrigins) == 0 {
		return false
	}

	if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
		return originAllowed(origin, allowedOrigins)
	}

	if referer := strings.TrimSpace(r.Header.Get("Referer")); referer != "" {
		return originAllowed(referer, allowedOrigins)
	}

	return false
}

func originAllowed(value string, allowedOrigins map[string]struct{}) bool {
	u, err := url.Parse(value)
	if err != nil {
		return false
	}
	if !u.IsAbs() || strings.TrimSpace(u.Host) == "" {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return false
	}
	_, ok := allowedOrigins[scheme+"://"+strings.ToLower(u.Host)]
	return ok
}
