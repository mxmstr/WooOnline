package config

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	BindHost       string
	PublicHost     string
	AuthPort       int
	SecurePort     int
	NATPort        int
	DatabasePath   string
	AccountsFile   string
	AccessKey      string
	RC4Key         string
	KeepOldMatches bool
}

func (c Config) Validate() error {
	if c.BindHost == "" {
		return fmt.Errorf("bind host is empty")
	}
	if c.PublicHost == "" {
		return fmt.Errorf("public host is empty")
	}
	for name, port := range map[string]int{
		"auth": c.AuthPort, "secure": c.SecurePort, "nat": c.NATPort,
	} {
		if port < 1 || port > 65535 {
			return fmt.Errorf("%s port %d is outside 1..65535", name, port)
		}
	}
	if c.AuthPort == c.SecurePort || c.AuthPort == c.NATPort || c.SecurePort == c.NATPort {
		return fmt.Errorf("auth, secure, and NAT ports must be distinct")
	}
	if c.DatabasePath == "" {
		return fmt.Errorf("database path is empty")
	}
	if c.AccessKey == "" {
		return fmt.Errorf("PRUDP access key is empty; set STRANGLEHOLD_ACCESS_KEY in .env or the process environment")
	}
	if c.RC4Key == "" {
		return fmt.Errorf("PRUDP RC4 key is empty; set STRANGLEHOLD_RC4_KEY in .env or the process environment")
	}
	return nil
}

// LoadDotEnv loads KEY=VALUE pairs without overwriting variables already
// present in the process environment. A missing file is not an error.
func LoadDotEnv(path string) error {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	values, err := parseDotEnv(file)
	if err != nil {
		return err
	}
	for name, value := range values {
		if _, exists := os.LookupEnv(name); exists {
			continue
		}
		if err := os.Setenv(name, value); err != nil {
			return fmt.Errorf("set %s: %w", name, err)
		}
	}
	return nil
}

func parseDotEnv(input io.Reader) (map[string]string, error) {
	values := make(map[string]string)
	scanner := bufio.NewScanner(input)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		name, raw, found := strings.Cut(line, "=")
		if !found {
			return nil, fmt.Errorf("line %d: expected KEY=VALUE", lineNumber)
		}
		name = strings.TrimSpace(name)
		raw = strings.TrimSpace(raw)
		if name == "" {
			return nil, fmt.Errorf("line %d: variable name is empty", lineNumber)
		}
		for _, char := range name {
			if !(char == '_' || char >= 'a' && char <= 'z' ||
				char >= 'A' && char <= 'Z' || char >= '0' && char <= '9') {
				return nil, fmt.Errorf("line %d: invalid variable name %q", lineNumber, name)
			}
		}
		value, err := parseDotEnvValue(raw)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNumber, err)
		}
		values[name] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func parseDotEnvValue(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	if raw[0] == '"' {
		value, err := strconv.Unquote(raw)
		if err != nil {
			return "", fmt.Errorf("invalid quoted value: %w", err)
		}
		return value, nil
	}
	if raw[0] == '\'' {
		if len(raw) < 2 || raw[len(raw)-1] != '\'' {
			return "", fmt.Errorf("unterminated single-quoted value")
		}
		return raw[1 : len(raw)-1], nil
	}
	if comment := strings.Index(raw, " #"); comment >= 0 {
		raw = raw[:comment]
	}
	return strings.TrimSpace(raw), nil
}

func Env(name, fallback string) string {
	if value, ok := os.LookupEnv(name); ok {
		return value
	}
	return fallback
}

func EnvInt(name string, fallback int) int {
	value, ok := os.LookupEnv(name)
	if !ok {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func EnvBool(name string, fallback bool) bool {
	value, ok := os.LookupEnv(name)
	if !ok {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}
