// Package envfile loads KEY=VALUE files into the process environment and persists generated secrets.
package envfile

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"
)

// Load sets variables from a .env file without overriding the real environment.
// A missing file is not an error.
func Load(path string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for line := range strings.SplitSeq(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(strings.TrimPrefix(k, "export "))
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		if _, exists := os.LookupEnv(k); !exists {
			_ = os.Setenv(k, v)
		}
	}
}

// EnsureSecrets generates a random 32-byte hex value for every key that is not set, exports it and
// appends it to path, so the values stay stable across restarts. It returns the generated keys.
func EnsureSecrets(path string, keys ...string) ([]string, error) {
	var lines, generated []string
	for _, k := range keys {
		if os.Getenv(k) != "" {
			continue
		}
		buf := make([]byte, 32)
		if _, err := rand.Read(buf); err != nil {
			return nil, fmt.Errorf("generate %s: %w", k, err)
		}
		v := hex.EncodeToString(buf)
		_ = os.Setenv(k, v)
		generated = append(generated, k)
		lines = append(lines, k+"="+v)
	}
	if len(lines) == 0 {
		return nil, nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("secrets file: %w", err)
	}
	defer func() { _ = f.Close() }()
	_, err = fmt.Fprintf(f, "\n# Generated %s. Keep this file: ENCRYPTION_KEY protects stored project secrets.\n%s\n",
		time.Now().Format(time.RFC3339), strings.Join(lines, "\n"))
	if err != nil {
		return nil, fmt.Errorf("secrets file: %w", err)
	}
	return generated, nil
}
