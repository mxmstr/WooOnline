package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestAccountAndCareerPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	account, err := database.FindOrCreateAccount(ctx, "player")
	if err != nil {
		t.Fatal(err)
	}
	again, err := database.FindOrCreateAccount(ctx, "player")
	if err != nil {
		t.Fatal(err)
	}
	if again.PID != account.PID {
		t.Fatalf("second lookup pid %d, want %d", again.PID, account.PID)
	}
	vector := make([]float32, CareerVectorLength)
	vector[5] = 12345
	rows := map[uint32]LeaderboardRow{
		301: {Cash: 12345, ModeStat: 7, MapID: 2},
	}
	if err := database.PersistMatch(ctx, account.PID, vector, rows); err != nil {
		t.Fatal(err)
	}
	got, ok, err := database.Career(ctx, account.PID)
	if err != nil || !ok {
		t.Fatalf("career ok=%v err=%v", ok, err)
	}
	if got[5] != vector[5] {
		t.Fatalf("cash %v, want %v", got[5], vector[5])
	}
	ranked, err := database.Ranked(ctx, 301)
	if err != nil {
		t.Fatal(err)
	}
	if len(ranked) != 1 || ranked[0].PID != account.PID {
		t.Fatalf("ranked %#v", ranked)
	}
	password := []byte{1, 2, 3, 4}
	if err := database.SetAccountCredential(ctx, account.PID, password, true); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	database, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	reopened, err := database.FindOrCreateAccount(ctx, "player")
	if err != nil {
		t.Fatal(err)
	}
	if reopened.PID != account.PID {
		t.Fatalf("reopened pid %d, want %d", reopened.PID, account.PID)
	}
	credential, ok, err := database.AccountCredential(ctx, reopened.PID)
	if err != nil || !ok {
		t.Fatalf("credential ok=%v err=%v", ok, err)
	}
	if string(credential.Password) != string(password) || !credential.PreHashed {
		t.Fatalf("credential %#v, want password %v pre-hashed", credential, password)
	}
}
