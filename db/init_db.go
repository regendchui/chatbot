package db

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

var DB *pgxpool.Pool

func InitDB() {
	dsn := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=disable",
		MustGetEnv("DB_USER", "postgres"),
		MustGetEnv("DB_PASSWORD", "postgres"),
		MustGetEnv("DB_HOST", "postgres"),
		MustGetEnv("DB_PORT", "5432"),
		MustGetEnv("DB_NAME", "wa_db"),
	)

	var err error
	DB, err = pgxpool.New(context.Background(), dsn)
	if err != nil {
		panic(fmt.Errorf("create pgx pool: %w", err))
	}

	if err := DB.Ping(context.Background()); err != nil {
		panic(fmt.Errorf("ping postgres: %w", err))
	}

	if err := CreateConversationTable(); err != nil {
		panic(fmt.Errorf("ensure conversation table: %w", err))
	}
	if err := EnsureMetaTableExists(); err != nil {
		panic(fmt.Errorf("ensure meta table: %w", err))
	}
	if err := EnsureBlacklistTableExists(); err != nil {
		panic(fmt.Errorf("ensure blacklist table: %w", err))
	}
	if err := EnsureRoleTableExists(); err != nil {
		panic(fmt.Errorf("ensure role table: %w", err))
	}
	if err := EnsureRAGTableExists(); err != nil {
		panic(fmt.Errorf("ensure RAG table: %w", err))
	}
	if err := EnsureLoginHistoryTableExists(); err != nil {
		panic(fmt.Errorf("ensure login_history table: %w", err))
	}
	if err := EnsureConfigUpdateHistoryTableExists(); err != nil {
		panic(fmt.Errorf("ensure config_update_history table: %w", err))
	}
}

func MustGetEnv(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}
