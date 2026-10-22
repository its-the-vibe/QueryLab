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
	queryservice "github.com/its-the-vibe/QueryLab/internal/query"
)

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

type pageData struct {
	Queries               []queryservice.QueryMetadata
	SelectedQuery         string
	SelectedParameters    map[string]string
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
func NewServer(cfg *config.Config, executor queryservice.Executor) (*http.Server, error) {
	startupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	queryService := queryservice.NewService(executor)
	queries, err := queryService.DiscoverQueries(startupCtx)
	if err != nil {
		return nil, fmt.Errorf("discovering queries: %w", err)
	}

	queryTmpl := template.Must(template.New("queries").Funcs(template.FuncMap{
		"inputType": queryParameterInputType,
		"isBoolean": queryParameterIsBoolean,
	}).Parse(queriesPageTemplate))
	schemaTmpl := template.Must(template.New("schema").Parse(schemaPageTemplate))
	available := make(map[string]queryservice.QueryMetadata, len(queries))
	for _, query := range queries {
		available[query.Name] = query
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
		http.Redirect(w, r, "/queries", http.StatusSeeOther)
	})

	mux.HandleFunc("/queries", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		data := newPageData()
		if queryValues, exists := r.URL.Query()["query"]; exists {
			query := strings.TrimSpace(queryValues[0])
			data.SelectedQuery = query
			queryMetadata, ok := available[query]
			if !ok {
				data.Error = "invalid query selected"
				_ = queryTmpl.Execute(w, data)
				return
			}

			parameters := make(map[string]string)
			for _, parameter := range queryMetadata.Parameters {
				if values, exists := r.URL.Query()["param_"+parameter.Name]; exists && len(values) > 0 && values[0] != "" {
					parameters[parameter.Name] = values[0]
				}
			}
			data.SelectedParameters = parameters

			ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
			defer cancel()

			output, err := queryService.RunQuery(ctx, query, parameters)
			if err != nil {
				data.Error = err.Error()
				_ = queryTmpl.Execute(w, data)
				return
			}

			rows, err := queryservice.ParseRows(output)
			if err != nil {
				data.Error = fmt.Sprintf("failed to parse query result: %v", err)
				_ = queryTmpl.Execute(w, data)
				return
			}

			data.Columns, data.Rows = queryservice.BuildTable(rows)
		}
		_ = queryTmpl.Execute(w, data)
	})

	mux.HandleFunc("/schema", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			data := newPageData()
			if datasetValues, datasetSet := r.URL.Query()["dataset"]; datasetSet {
				table := r.URL.Query().Get("table")
				dataset := strings.TrimSpace(datasetValues[0])
				table = strings.TrimSpace(table)
				data.SelectedDataset, data.SelectedTable = selectSchemaDatasetAndTable(schemaDatasets, schemaTablesByDataset, dataset, table)

				if !isAllowedSchemaTable(allowedSchemaTables, dataset, table) {
					data.Error = "invalid dataset/table selected"
					_ = schemaTmpl.Execute(w, data)
					return
				}

				ctx, cancel := context.WithTimeout(r.Context(), time.Duration(cfg.Poppit.CommandTimeoutSeconds)*time.Second)
				defer cancel()

				output, err := queryService.RunSchema(ctx, dataset, table)
				if err != nil {
					data.Error = err.Error()
					_ = schemaTmpl.Execute(w, data)
					return
				}

				schemaFields, err := queryservice.ParseSchema(output)
				if err != nil {
					log.Printf("failed to parse schema result for %s.%s: %v", dataset, table, err)
					data.Error = "failed to parse schema result"
					_ = schemaTmpl.Execute(w, data)
					return
				}

				data.Columns, data.Rows = queryservice.BuildTable(schemaFields)
			}
			_ = schemaTmpl.Execute(w, data)
		case http.MethodPost:
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

			output, err := queryService.RunSchema(ctx, dataset, table)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}

			schemaFields, err := queryservice.ParseSchema(output)
			if err != nil {
				log.Printf("failed to parse schema result for %s.%s: %v", dataset, table, err)
				http.Error(w, "failed to parse schema result", http.StatusBadGateway)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(schemaFields); err != nil {
				http.Error(w, "failed to encode schema result", http.StatusInternalServerError)
			}
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	return &http.Server{
		Addr:    cfg.Server.Addr,
		Handler: mux,
	}, nil
}

func queryParameterIsBoolean(parameterType string) bool {
	switch strings.ToLower(strings.TrimSpace(parameterType)) {
	case "bool", "boolean":
		return true
	default:
		return false
	}
}

func queryParameterInputType(parameterType string) string {
	switch strings.ToLower(strings.TrimSpace(parameterType)) {
	case "int", "integer", "number", "float", "double", "decimal":
		return "number"
	case "date":
		return "date"
	default:
		return "text"
	}
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
