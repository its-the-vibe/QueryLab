package main

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
