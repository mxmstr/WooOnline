package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseDotEnvQuotedValues(t *testing.T) {
	values, err := parseDotEnv(strings.NewReader(`
# comment
STRANGLEHOLD_ACCESS_KEY="eightkey"
STRANGLEHOLD_RC4_KEY='five!'
STRANGLEHOLD_PUBLIC_HOST=192.168.4.199 # LAN address
`))
	if err != nil {
		t.Fatal(err)
	}
	if got := values["STRANGLEHOLD_ACCESS_KEY"]; got != "eightkey" {
		t.Fatalf("access key %q", got)
	}
	if got := values["STRANGLEHOLD_RC4_KEY"]; got != "five!" {
		t.Fatalf("RC4 key %q", got)
	}
	if got := values["STRANGLEHOLD_PUBLIC_HOST"]; got != "192.168.4.199" {
		t.Fatalf("public host %q", got)
	}
}

func TestLoadDotEnvDoesNotOverrideEnvironment(t *testing.T) {
	const name = "STRANGLEHOLD_DOTENV_PRECEDENCE_TEST"
	t.Setenv(name, "process")
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte(name+"=file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := LoadDotEnv(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv(name); got != "process" {
		t.Fatalf("value %q, want process", got)
	}
}

func TestValidateRejectsEmptyTransportKeys(t *testing.T) {
	cfg := Config{
		BindHost: "127.0.0.1", PublicHost: "127.0.0.1",
		AuthPort: 1, SecurePort: 2, NATPort: 3, DatabasePath: "server.db",
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate accepted empty transport keys")
	}
}
