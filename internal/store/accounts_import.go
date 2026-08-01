package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type preloadAccount struct {
	Username string  `json:"username"`
	Password *string `json:"password"`
}

func ImportAccountsFile(ctx context.Context, database *DB, path string) (imported, skipped int, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, fmt.Errorf("read accounts file %q: %w", path, err)
	}
	var entries []preloadAccount
	if err := json.Unmarshal(data, &entries); err != nil {
		return 0, 0, fmt.Errorf("parse accounts file %q: %w", path, err)
	}
	for index, entry := range entries {
		username := strings.TrimSpace(entry.Username)
		if username == "" {
			return imported, skipped, fmt.Errorf("accounts file %q entry %d has an empty username", path, index+1)
		}
		var password []byte
		if entry.Password != nil {
			password = []byte(*entry.Password)
		}
		created, err := database.PreloadAccount(ctx, username, password)
		if err != nil {
			return imported, skipped, fmt.Errorf("preload account %q: %w", username, err)
		}
		if created {
			imported++
		} else {
			skipped++
		}
	}
	return imported, skipped, nil
}
