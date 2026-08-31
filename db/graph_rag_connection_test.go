package db

import (
	"context"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

type recordingAGEExecutor struct {
	commands []string
}

func (executor *recordingAGEExecutor) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	executor.commands = append(executor.commands, sql)
	return pgconn.NewCommandTag(""), nil
}

func TestConfigureAGEConnectionLoadsExtensionAndOperatorSearchPath(t *testing.T) {
	executor := &recordingAGEExecutor{}
	if err := configureAGEConnection(context.Background(), executor); err != nil {
		t.Fatal(err)
	}
	want := []string{`LOAD 'age'`, `SET search_path = ag_catalog, "$user", public`}
	if !reflect.DeepEqual(executor.commands, want) {
		t.Fatalf("AGE connection commands=%q want %q", executor.commands, want)
	}
}
