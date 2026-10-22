package query

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Executor runs a command and returns its output.
type Executor interface {
	Execute(ctx context.Context, command string) ([]byte, error)
}

// Service provides query execution and result parsing.
type Service struct {
	executor Executor
}

// QueryMetadata describes a query and the optional parameters it accepts.
type QueryMetadata struct {
	Name       string
	Parameters []QueryParameter
}

// QueryParameter describes an optional query parameter.
type QueryParameter struct {
	Name string
	Type string
}

// NewService creates a query service using the provided command executor.
func NewService(executor Executor) *Service {
	return &Service{executor: executor}
}

// DiscoverQueries returns the sorted set of available queries and their parameters.
func (s *Service) DiscoverQueries(ctx context.Context) ([]QueryMetadata, error) {
	output, err := s.executor.Execute(ctx, "./goquery --json list --verbose")
	if err != nil {
		output, err = s.executor.Execute(ctx, "./goquery --json list")
		if err != nil {
			return nil, fmt.Errorf("running goquery list via poppit: %w", err)
		}
	}

	queries, err := parseQueryList(output)
	if err != nil {
		return nil, fmt.Errorf("parsing goquery list output: %w", err)
	}

	sort.Slice(queries, func(i, j int) bool {
		return queries[i].Name < queries[j].Name
	})
	return queries, nil
}

// RunQuery executes the named GoQuery query through the configured executor.
func (s *Service) RunQuery(ctx context.Context, name string, parameters map[string]string) ([]byte, error) {
	command := fmt.Sprintf("./goquery --json query %s", shellQuote(name))
	parameterNames := make([]string, 0, len(parameters))
	for parameter := range parameters {
		parameterNames = append(parameterNames, parameter)
	}
	sort.Strings(parameterNames)
	for _, parameter := range parameterNames {
		if strings.TrimSpace(parameter) == "" {
			continue
		}
		command += " -p " + shellQuote(parameter+"="+parameters[parameter])
	}
	output, err := s.executor.Execute(ctx, command)
	if err != nil {
		return nil, fmt.Errorf("running goquery query %q via poppit: %w", name, err)
	}
	return output, nil
}

// RunSchema retrieves the schema for a dataset and table through the executor.
func (s *Service) RunSchema(ctx context.Context, dataset, table string) ([]byte, error) {
	command := fmt.Sprintf("./goquery --json schema %s %s", shellQuote(dataset), shellQuote(table))
	output, err := s.executor.Execute(ctx, command)
	if err != nil {
		return nil, fmt.Errorf("running goquery schema %q.%q via poppit: %w", dataset, table, err)
	}
	return output, nil
}

func parseQueryList(output []byte) ([]QueryMetadata, error) {
	var parsed any
	if err := json.Unmarshal(output, &parsed); err != nil {
		return nil, err
	}

	var out []QueryMetadata
	add := func(value any) {
		var query QueryMetadata
		switch item := value.(type) {
		case string:
			query.Name = item
		case map[string]any:
			if name, ok := item["name"].(string); ok {
				query.Name = name
			} else if name, ok := item["query"].(string); ok {
				query.Name = name
			}
			if parameters, ok := item["parameters"].([]any); ok {
				for _, parameter := range parameters {
					record, ok := parameter.(map[string]any)
					if !ok {
						continue
					}
					name, hasName := record["name"].(string)
					parameterType, hasType := record["type"].(string)
					if hasName && strings.TrimSpace(name) != "" && hasType && strings.TrimSpace(parameterType) != "" {
						query.Parameters = append(query.Parameters, QueryParameter{Name: name, Type: parameterType})
					}
				}
			}
		}
		query.Name = strings.TrimSpace(query.Name)
		if query.Name != "" {
			out = append(out, query)
		}
	}

	switch typed := parsed.(type) {
	case []any:
		for _, item := range typed {
			add(item)
		}
	case map[string]any:
		for _, key := range []string{"queries", "data", "results"} {
			entries, ok := typed[key].([]any)
			if !ok {
				continue
			}
			for _, item := range entries {
				add(item)
			}
		}
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("no executable queries found")
	}

	unique := make(map[string]struct{}, len(out))
	result := make([]QueryMetadata, 0, len(out))
	for _, query := range out {
		if _, ok := unique[query.Name]; ok {
			continue
		}
		unique[query.Name] = struct{}{}
		result = append(result, query)
	}
	return result, nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

// ParseRows decodes tabular output from GoQuery JSON.
func ParseRows(output []byte) ([]map[string]any, error) {
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

// ParseSchema decodes and validates schema fields from GoQuery JSON.
func ParseSchema(output []byte) ([]map[string]any, error) {
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
		fieldType, hasType := record["type"].(string)
		if !hasName || strings.TrimSpace(name) == "" || !hasType || strings.TrimSpace(fieldType) == "" {
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

// BuildTable returns sorted column names and their values in row order.
func BuildTable(rows []map[string]any) ([]string, [][]string) {
	columnSet := make(map[string]struct{})
	for _, row := range rows {
		for column := range row {
			columnSet[column] = struct{}{}
		}
	}

	columns := make([]string, 0, len(columnSet))
	for column := range columnSet {
		columns = append(columns, column)
	}
	sort.Strings(columns)

	tableRows := make([][]string, 0, len(rows))
	for _, row := range rows {
		line := make([]string, 0, len(columns))
		for _, column := range columns {
			line = append(line, fmt.Sprintf("%v", row[column]))
		}
		tableRows = append(tableRows, line)
	}
	return columns, tableRows
}
