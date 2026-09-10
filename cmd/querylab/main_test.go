package main

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"
)

type fakeExecutor struct {
	commandOutput map[string][]byte
	err           error
	lastCommand   string
}

func (f *fakeExecutor) Execute(_ context.Context, command string) ([]byte, error) {
	f.lastCommand = command
	if f.err != nil {
		return nil, f.err
	}
	output, ok := f.commandOutput[command]
	if !ok {
		return nil, errors.New("unknown command")
	}
	return output, nil
}

func TestParseQueryList(t *testing.T) {
	t.Parallel()

	output := []byte(`{"queries":[{"name":"beta"},{"name":"alpha"},"gamma"]}`)
	queries, err := parseQueryList(output)
	if err != nil {
		t.Fatalf("parseQueryList() error = %v", err)
	}

	expected := []string{"beta", "alpha", "gamma"}
	if !reflect.DeepEqual(queries, expected) {
		t.Fatalf("parseQueryList() = %v, want %v", queries, expected)
	}
}

func TestDiscoverQueriesUsesPoppitCommandAndSorts(t *testing.T) {
	t.Parallel()

	exec := &fakeExecutor{
		commandOutput: map[string][]byte{
			"./goquery --json list": []byte(`["zeta","alpha","zeta"]`),
		},
	}

	queries, err := discoverQueries(context.Background(), exec)
	if err != nil {
		t.Fatalf("discoverQueries() error = %v", err)
	}

	expected := []string{"alpha", "zeta"}
	if !reflect.DeepEqual(queries, expected) {
		t.Fatalf("discoverQueries() = %v, want %v", queries, expected)
	}
	if exec.lastCommand != "./goquery --json list" {
		t.Fatalf("last command = %q, want %q", exec.lastCommand, "./goquery --json list")
	}
}

func TestRunQueryBuildsQuotedCommand(t *testing.T) {
	t.Parallel()

	expectedCommand := "./goquery --json query " + shellQuote("team's")
	exec := &fakeExecutor{
		commandOutput: map[string][]byte{
			expectedCommand: []byte(`[]`),
		},
	}

	if _, err := runQuery(context.Background(), exec, "team's"); err != nil {
		t.Fatalf("runQuery() error = %v", err)
	}

	if exec.lastCommand != expectedCommand {
		t.Fatalf("last command = %q, want %q", exec.lastCommand, expectedCommand)
	}
}

func TestRunSchemaBuildsQuotedCommand(t *testing.T) {
	t.Parallel()

	expectedCommand := "./goquery --json schema " + shellQuote("analytics-prod") + " " + shellQuote("team's_table")
	exec := &fakeExecutor{
		commandOutput: map[string][]byte{
			expectedCommand: []byte(`[]`),
		},
	}

	if _, err := runSchema(context.Background(), exec, "analytics-prod", "team's_table"); err != nil {
		t.Fatalf("runSchema() error = %v", err)
	}

	if exec.lastCommand != expectedCommand {
		t.Fatalf("last command = %q, want %q", exec.lastCommand, expectedCommand)
	}
}

func TestParseRowsAndBuildTable(t *testing.T) {
	t.Parallel()

	output := []byte(`{"rows":[{"name":"alice","count":2},{"name":"bob","count":5}]}`)
	rows, err := parseRows(output)
	if err != nil {
		t.Fatalf("parseRows() error = %v", err)
	}

	columns, tableRows := buildTable(rows)
	expectedColumns := []string{"count", "name"}
	expectedRows := [][]string{{"2", "alice"}, {"5", "bob"}}

	if !reflect.DeepEqual(columns, expectedColumns) {
		t.Fatalf("columns = %v, want %v", columns, expectedColumns)
	}
	if !reflect.DeepEqual(tableRows, expectedRows) {
		t.Fatalf("tableRows = %v, want %v", tableRows, expectedRows)
	}
}

func TestParseSchema(t *testing.T) {
	t.Parallel()

	output := []byte(`[
		{"name":"date","type":"STRING","mode":"NULLABLE","description":"The date"},
		{"name":"count","type":"INTEGER","mode":"REQUIRED"}
	]`)

	schemaFields, err := parseSchema(output)
	if err != nil {
		t.Fatalf("parseSchema() error = %v", err)
	}
	if len(schemaFields) != 2 {
		t.Fatalf("parseSchema() len = %d, want 2", len(schemaFields))
	}
	expected := []map[string]any{
		{"name": "date", "type": "STRING", "mode": "NULLABLE", "description": "The date"},
		{"name": "count", "type": "INTEGER", "mode": "REQUIRED"},
	}
	if !reflect.DeepEqual(schemaFields, expected) {
		t.Fatalf("parseSchema() = %v, want %v", schemaFields, expected)
	}
}

func TestParseSchemaRejectsInvalidOutput(t *testing.T) {
	t.Parallel()

	invalidOutputs := [][]byte{
		[]byte(`{}`),
		[]byte(`[{"name":"date"}]`),
		[]byte(`[{"name":"date","type":"STRING","mode":123}]`),
	}

	for _, output := range invalidOutputs {
		if _, err := parseSchema(output); err == nil {
			t.Fatalf("parseSchema(%s) expected error, got nil", string(output))
		}
	}
}

func TestParseSchemaAllowsEmptyArray(t *testing.T) {
	t.Parallel()

	schemaFields, err := parseSchema([]byte(`[]`))
	if err != nil {
		t.Fatalf("parseSchema() error = %v", err)
	}
	if len(schemaFields) != 0 {
		t.Fatalf("parseSchema() len = %d, want 0", len(schemaFields))
	}
}

func TestBuildAllowedSchemaTablesAndMatch(t *testing.T) {
	t.Parallel()

	allowed := buildAllowedSchemaTables([]struct {
		Dataset string `mapstructure:"dataset"`
		Table   string `mapstructure:"table"`
	}{
		{Dataset: "analytics", Table: "events"},
		{Dataset: "analytics", Table: "users"},
		{Dataset: " reporting ", Table: " daily "},
		{Dataset: " ", Table: "ignored"},
		{Dataset: "analytics", Table: " "},
	})

	if !isAllowedSchemaTable(allowed, "analytics", "events") {
		t.Fatalf("expected analytics.events to be allowed")
	}
	if isAllowedSchemaTable(allowed, "analytics", "missing") {
		t.Fatalf("expected analytics.missing to be rejected")
	}
	if isAllowedSchemaTable(allowed, "missing", "events") {
		t.Fatalf("expected missing.events to be rejected")
	}
	if !isAllowedSchemaTable(allowed, "reporting", "daily") {
		t.Fatalf("expected trimmed reporting.daily to be allowed")
	}
}

func TestIsTrustedRequestOrigin(t *testing.T) {
	t.Parallel()

	allowedOrigins := buildAllowedOrigins([]string{
		"https://querylab.local",
		"https://app.querylab.local:8443",
	})

	tests := []struct {
		name     string
		origin   string
		referer  string
		expected bool
	}{
		{name: "matching origin", origin: "https://querylab.local", expected: true},
		{name: "matching origin with configured port", origin: "https://app.querylab.local:8443", expected: true},
		{name: "wrong scheme for allowed host", origin: "http://querylab.local", expected: false},
		{name: "non-http scheme", origin: "file://querylab.local/tmp", expected: false},
		{name: "mismatched origin", origin: "https://evil.example", expected: false},
		{name: "matching referer", referer: "https://querylab.local/schema", expected: true},
		{name: "mismatched referer", referer: "https://evil.example/schema", expected: false},
		{name: "no origin or referer", expected: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req, err := http.NewRequest(http.MethodPost, "http://querylab.local/schema", nil)
			if err != nil {
				t.Fatalf("http.NewRequest() error = %v", err)
			}
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			if tt.referer != "" {
				req.Header.Set("Referer", tt.referer)
			}
			if got := isTrustedRequestOrigin(req, allowedOrigins); got != tt.expected {
				t.Fatalf("isTrustedRequestOrigin() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestBuildAllowedOrigins(t *testing.T) {
	t.Parallel()

	origins := buildAllowedOrigins([]string{
		"https://querylab.local",
		" https://QUERYLAB.local ",
		"ftp://querylab.local",
		"invalid",
		"",
	})

	if len(origins) != 1 {
		t.Fatalf("len(origins) = %d, want 1", len(origins))
	}
	if _, ok := origins["https://querylab.local"]; !ok {
		t.Fatalf("expected https://querylab.local in allowed origins")
	}
}
