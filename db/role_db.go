package db

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

type RoleUser struct {
	Username       string
	PermittedPages []string
}

func EnsureRoleTableExists() error {
	query := `
CREATE TABLE IF NOT EXISTS role (
    username TEXT PRIMARY KEY,
    password TEXT NOT NULL,
    permitted_pages JSONB NOT NULL DEFAULT '[]'::jsonb
);`
	if _, err := DB.Exec(context.Background(), query); err != nil {
		return fmt.Errorf("create role table: %w", err)
	}
	if _, err := DB.Exec(context.Background(), `ALTER TABLE role ADD COLUMN IF NOT EXISTS password TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("alter role add password: %w", err)
	}
	if _, err := DB.Exec(context.Background(), `ALTER TABLE role ADD COLUMN IF NOT EXISTS permitted_pages JSONB NOT NULL DEFAULT '[]'::jsonb`); err != nil {
		return fmt.Errorf("alter role add permitted_pages: %w", err)
	}
	return nil
}

func CreateRoleUser(username string, plainPassword string, permittedPages []string) error {
	name := strings.TrimSpace(username)
	pass := strings.TrimSpace(plainPassword)
	if name == "" {
		return fmt.Errorf("username is empty")
	}
	if pass == "" {
		return fmt.Errorf("password is empty")
	}
	if exists, err := roleUserExists(name); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("username already exists")
	}
	encryptedPassword, err := encryptAdminPanelPassword(pass)
	if err != nil {
		return fmt.Errorf("encrypt role password: %w", err)
	}
	normalizedPages := NormalizeRolePermittedPages(permittedPages)
	pagesJSON, err := json.Marshal(normalizedPages)
	if err != nil {
		return fmt.Errorf("marshal permitted pages: %w", err)
	}
	query := `INSERT INTO role (username, password, permitted_pages) VALUES ($1, $2, $3)`
	if _, err := DB.Exec(context.Background(), query, name, encryptedPassword, pagesJSON); err != nil {
		return fmt.Errorf("insert role user: %w", err)
	}
	return nil
}

func VerifyRoleCredentials(username string, password string) (bool, error) {
	name := strings.TrimSpace(username)
	pass := strings.TrimSpace(password)
	if name == "" || pass == "" {
		return false, nil
	}
	var encrypted string
	err := DB.QueryRow(
		context.Background(),
		`SELECT password FROM role WHERE username = $1`,
		name,
	).Scan(&encrypted)
	if err != nil {
		if err == pgx.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("load role credentials: %w", err)
	}
	plain, err := decryptAdminPanelPassword(strings.TrimSpace(encrypted))
	if err != nil {
		return false, err
	}
	return subtle.ConstantTimeCompare([]byte(plain), []byte(pass)) == 1, nil
}

func RoleUserCanAccessPath(username string, path string) (bool, error) {
	pages, err := RolePermittedPages(username)
	if err != nil {
		return false, err
	}
	return RoleAllowsPath(pages, path), nil
}

func RolePermittedPages(username string) ([]string, error) {
	name := strings.TrimSpace(username)
	if name == "" {
		return nil, nil
	}
	var raw []byte
	err := DB.QueryRow(
		context.Background(),
		`SELECT permitted_pages::text FROM role WHERE username = $1`,
		name,
	).Scan(&raw)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("load role permitted pages: %w", err)
	}
	pages := []string{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &pages); err != nil {
			return nil, fmt.Errorf("parse role permitted pages: %w", err)
		}
	}
	return NormalizeRolePermittedPages(pages), nil
}

func ListRoleUsers() ([]RoleUser, error) {
	rows, err := DB.Query(context.Background(), `SELECT username, permitted_pages::text FROM role ORDER BY username`)
	if err != nil {
		return nil, fmt.Errorf("list role users: %w", err)
	}
	defer rows.Close()

	out := make([]RoleUser, 0, 16)
	for rows.Next() {
		var username string
		var rawPages []byte
		if err := rows.Scan(&username, &rawPages); err != nil {
			return nil, fmt.Errorf("scan role user: %w", err)
		}
		pages := []string{}
		if len(rawPages) > 0 {
			if err := json.Unmarshal(rawPages, &pages); err != nil {
				return nil, fmt.Errorf("decode role permitted pages: %w", err)
			}
		}
		out = append(out, RoleUser{
			Username:       strings.TrimSpace(username),
			PermittedPages: NormalizeRolePermittedPages(pages),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate role users: %w", err)
	}
	return out, nil
}

func DeleteRoleUser(username string) (bool, error) {
	name := strings.TrimSpace(username)
	if name == "" {
		return false, fmt.Errorf("username is empty")
	}
	tag, err := DB.Exec(
		context.Background(),
		`DELETE FROM role WHERE username = $1`,
		name,
	)
	if err != nil {
		return false, fmt.Errorf("delete role user: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func UpdateRoleUserPermittedPages(username string, permittedPages []string) (bool, error) {
	name := strings.TrimSpace(username)
	if name == "" {
		return false, fmt.Errorf("username is empty")
	}
	normalizedPages := NormalizeRolePermittedPages(permittedPages)
	pagesJSON, err := json.Marshal(normalizedPages)
	if err != nil {
		return false, fmt.Errorf("marshal permitted pages: %w", err)
	}
	tag, err := DB.Exec(
		context.Background(),
		`UPDATE role SET permitted_pages = $2 WHERE username = $1`,
		name,
		pagesJSON,
	)
	if err != nil {
		return false, fmt.Errorf("update role user permitted pages: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func UpdateRoleUserPassword(username string, newPassword string) (bool, error) {
	name := strings.TrimSpace(username)
	pass := strings.TrimSpace(newPassword)
	if name == "" {
		return false, fmt.Errorf("username is empty")
	}
	if pass == "" {
		return false, fmt.Errorf("new password is empty")
	}
	encryptedPassword, err := encryptAdminPanelPassword(pass)
	if err != nil {
		return false, fmt.Errorf("encrypt role password: %w", err)
	}
	tag, err := DB.Exec(
		context.Background(),
		`UPDATE role SET password = $2 WHERE username = $1`,
		name,
		encryptedPassword,
	)
	if err != nil {
		return false, fmt.Errorf("update role user password: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func NormalizeRolePermittedPages(pages []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(pages))
	for _, page := range pages {
		normalized := normalizeRolePath(page)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

func RoleAllowsPath(permittedPages []string, path string) bool {
	target := normalizeRolePath(path)
	if target == "" {
		return false
	}
	for _, raw := range permittedPages {
		perm := normalizeRolePath(raw)
		if perm == "" {
			continue
		}
		if perm == "*" || perm == "/*" {
			return true
		}
		if strings.HasSuffix(perm, "*") {
			prefix := strings.TrimSuffix(perm, "*")
			if prefix == "" || strings.HasPrefix(target, prefix) {
				return true
			}
			continue
		}
		if target == perm {
			return true
		}
		if strings.HasPrefix(target, perm+"/") {
			return true
		}
	}
	return false
}

func roleUserExists(username string) (bool, error) {
	var count int
	if err := DB.QueryRow(
		context.Background(),
		`SELECT COUNT(1) FROM role WHERE username = $1`,
		strings.TrimSpace(username),
	).Scan(&count); err != nil {
		return false, fmt.Errorf("check role username exists: %w", err)
	}
	return count > 0, nil
}

func normalizeRolePath(path string) string {
	value := strings.TrimSpace(path)
	if value == "" {
		return ""
	}
	if value == "*" || value == "/*" {
		return value
	}
	if strings.HasSuffix(value, "*") {
		prefix := strings.TrimSpace(strings.TrimSuffix(value, "*"))
		if prefix == "" {
			return ""
		}
		if !strings.HasPrefix(prefix, "/") {
			prefix = "/" + prefix
		}
		prefix = strings.TrimRight(prefix, "/")
		if prefix == "" {
			return "/*"
		}
		return prefix + "/*"
	}
	if !strings.HasPrefix(value, "/") {
		value = "/" + value
	}
	if value != "/" {
		value = strings.TrimRight(value, "/")
	}
	return value
}
