package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestImportAccountsFileSkipsExistingUsernames(t *testing.T) {
	directory := t.TempDir()
	database, err := Open(filepath.Join(directory, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	ctx := context.Background()
	existing, err := database.FindOrCreateAccount(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "accounts.json")
	if err := database.SetAccountCredential(ctx, existing.PID, []byte("original"), false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(
		`[{"username":"alice","password":"replacement"},{"username":"bob","password":"secret"}]`,
	), 0o600); err != nil {
		t.Fatal(err)
	}

	imported, skipped, err := ImportAccountsFile(ctx, database, path)
	if err != nil {
		t.Fatal(err)
	}
	if imported != 1 || skipped != 1 {
		t.Fatalf("imported=%d skipped=%d, want 1 and 1", imported, skipped)
	}
	alice, err := database.FindOrCreateAccount(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if alice.PID != existing.PID {
		t.Fatalf("existing account PID changed from %d to %d", existing.PID, alice.PID)
	}
	aliceCredential, ok, err := database.AccountCredential(ctx, alice.PID)
	if err != nil || !ok || string(aliceCredential.Password) != "original" {
		t.Fatalf("existing credential changed: %#v, ok=%v, err=%v", aliceCredential, ok, err)
	}
	bob, err := database.FindOrCreateAccount(ctx, "bob")
	if err != nil {
		t.Fatal(err)
	}
	if bob.PID == existing.PID {
		t.Fatalf("new account reused existing PID %d", bob.PID)
	}
	bobCredential, ok, err := database.AccountCredential(ctx, bob.PID)
	if err != nil || !ok || string(bobCredential.Password) != "secret" || bobCredential.PreHashed {
		t.Fatalf("imported credential %#v, ok=%v, err=%v", bobCredential, ok, err)
	}
}
