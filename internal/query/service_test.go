package query

import (
	"context"
	"errors"
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

	queries, err := parseQueryList([]byte(`{"queries":[{"name":"beta","parameters":[{"name":"date","type":"string"}]},{"name":"alpha"},"gamma"]}`))
	if err != nil {
		t.Fatalf("parseQueryList() error = %v", err)
	}
	expected := []QueryMetadata{
		{Name: "beta", Parameters: []QueryParameter{{Name: "date", Type: "string"}}},
		{Name: "alpha"},
		{Name: "gamma"},
	}
	if !reflect.DeepEqual(queries, expected) {
		t.Fatalf("parseQueryList() = %v, want %v", queries, expected)
	}
}

func TestDiscoverQueriesUsesPoppitCommandAndSorts(t *testing.T) {
	t.Parallel()

	executor := &fakeExecutor{commandOutput: map[string][]byte{
		"./goquery --json list --verbose": []byte(`["zeta","alpha","zeta"]`),
	}}
	queries, err := NewService(executor).DiscoverQueries(context.Background())
	if err != nil {
		t.Fatalf("DiscoverQueries() error = %v", err)
	}
	if expected := []QueryMetadata{{Name: "alpha"}, {Name: "zeta"}}; !reflect.DeepEqual(queries, expected) {
		t.Fatalf("DiscoverQueries() = %v, want %v", queries, expected)
	}
	if executor.lastCommand != "./goquery --json list --verbose" {
		t.Fatalf("last command = %q, want %q", executor.lastCommand, "./goquery --json list --verbose")
	}
}

func TestDiscoverQueriesFallsBackWithoutVerboseSupport(t *testing.T) {
	t.Parallel()

	executor := &fakeExecutor{commandOutput: map[string][]byte{
		"./goquery --json list": []byte(`[{"name":"legacy"}]`),
	}}
	queries, err := NewService(executor).DiscoverQueries(context.Background())
	if err != nil {
		t.Fatalf("DiscoverQueries() error = %v", err)
	}
	if expected := []QueryMetadata{{Name: "legacy"}}; !reflect.DeepEqual(queries, expected) {
		t.Fatalf("DiscoverQueries() = %v, want %v", queries, expected)
	}
	if executor.lastCommand != "./goquery --json list" {
		t.Fatalf("last command = %q, want %q", executor.lastCommand, "./goquery --json list")
	}
}

func TestRunQueryBuildsQuotedCommand(t *testing.T) {
	t.Parallel()

	expectedCommand := "./goquery --json query " + shellQuote("team's")
	executor := &fakeExecutor{commandOutput: map[string][]byte{expectedCommand: []byte(`[]`)}}
	if _, err := NewService(executor).RunQuery(context.Background(), "team's", nil); err != nil {
		t.Fatalf("RunQuery() error = %v", err)
	}
	if executor.lastCommand != expectedCommand {
		t.Fatalf("last command = %q, want %q", executor.lastCommand, expectedCommand)
	}
}

func TestRunQueryBuildsSortedParameterFlags(t *testing.T) {
	t.Parallel()

	expectedCommand := "./goquery --json query 'starling-breakdown' -p 'date=2026-10-19' -p 'limit=5'"
	executor := &fakeExecutor{commandOutput: map[string][]byte{expectedCommand: []byte(`[]`)}}
	parameters := map[string]string{"limit": "5", "date": "2026-10-19"}
	if _, err := NewService(executor).RunQuery(context.Background(), "starling-breakdown", parameters); err != nil {
		t.Fatalf("RunQuery() error = %v", err)
	}
	if executor.lastCommand != expectedCommand {
		t.Fatalf("last command = %q, want %q", executor.lastCommand, expectedCommand)
	}
}

func TestRunSchemaBuildsQuotedCommand(t *testing.T) {
	t.Parallel()

	expectedCommand := "./goquery --json schema " + shellQuote("analytics-prod") + " " + shellQuote("team's_table")
	executor := &fakeExecutor{commandOutput: map[string][]byte{expectedCommand: []byte(`[]`)}}
	if _, err := NewService(executor).RunSchema(context.Background(), "analytics-prod", "team's_table"); err != nil {
		t.Fatalf("RunSchema() error = %v", err)
	}
	if executor.lastCommand != expectedCommand {
		t.Fatalf("last command = %q, want %q", executor.lastCommand, expectedCommand)
	}
}

func TestParseRowsAndBuildTable(t *testing.T) {
	t.Parallel()

	rows, err := ParseRows([]byte(`{"rows":[{"name":"alice","count":2},{"name":"bob","count":5}]}`))
	if err != nil {
		t.Fatalf("ParseRows() error = %v", err)
	}
	columns, tableRows := BuildTable(rows)
	if expected := []string{"count", "name"}; !reflect.DeepEqual(columns, expected) {
		t.Fatalf("columns = %v, want %v", columns, expected)
	}
	if expected := [][]string{{"2", "alice"}, {"5", "bob"}}; !reflect.DeepEqual(tableRows, expected) {
		t.Fatalf("tableRows = %v, want %v", tableRows, expected)
	}
}

func TestParseSchema(t *testing.T) {
	t.Parallel()

	output := []byte(`[{"name":"date","type":"STRING","mode":"NULLABLE","description":"The date"},{"name":"count","type":"INTEGER","mode":"REQUIRED"}]`)
	fields, err := ParseSchema(output)
	if err != nil {
		t.Fatalf("ParseSchema() error = %v", err)
	}
	expected := []map[string]any{
		{"name": "date", "type": "STRING", "mode": "NULLABLE", "description": "The date"},
		{"name": "count", "type": "INTEGER", "mode": "REQUIRED"},
	}
	if !reflect.DeepEqual(fields, expected) {
		t.Fatalf("ParseSchema() = %v, want %v", fields, expected)
	}
}

func TestParseSchemaRejectsInvalidOutput(t *testing.T) {
	t.Parallel()

	for _, output := range [][]byte{
		[]byte(`{}`),
		[]byte(`[{"name":"date"}]`),
		[]byte(`[{"name":"date","type":"STRING","mode":123}]`),
	} {
		if _, err := ParseSchema(output); err == nil {
			t.Fatalf("ParseSchema(%s) expected error, got nil", output)
		}
	}
}

func TestParseSchemaAllowsEmptyArray(t *testing.T) {
	t.Parallel()

	fields, err := ParseSchema([]byte(`[]`))
	if err != nil {
		t.Fatalf("ParseSchema() error = %v", err)
	}
	if len(fields) != 0 {
		t.Fatalf("ParseSchema() len = %d, want 0", len(fields))
	}
}
