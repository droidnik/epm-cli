package config

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Client secrets live in the macOS Keychain (service "epm-cli", account "profile:<name>").
// On other platforms they fall back to a 0600 JSON file next to the config.

const keychainService = "epm-cli"

func keychainAccount(profile string) string { return "profile:" + profile }

// KeychainAvailable reports whether the macOS `security` tool can be used.
func KeychainAvailable() bool {
	if runtime.GOOS != "darwin" || os.Getenv("EPM_CLI_NO_KEYCHAIN") != "" {
		return false
	}
	_, err := exec.LookPath("security")
	return err == nil
}

// SecretsFile is the fallback secret store.
func SecretsFile() string { return filepath.Join(Dir(), "secrets.json") }

func readSecretsFile() map[string]string {
	m := map[string]string{}
	b, err := os.ReadFile(SecretsFile())
	if err == nil {
		_ = json.Unmarshal(b, &m)
	}
	return m
}

func writeSecretsFile(m map[string]string) error {
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	return writeFile0600(SecretsFile(), b)
}

// StoreSecret saves a secret and returns the backend used ("keychain" or "file").
func StoreSecret(profile, secret string) (string, error) {
	if KeychainAvailable() {
		// `security -i` reads commands from stdin, and -X takes the password hex-encoded,
		// so the secret never appears in the process list. Profile names are validated
		// to [A-Za-z0-9._-], so they need no quoting.
		cmd := exec.Command("security", "-i")
		cmd.Stdin = strings.NewReader(fmt.Sprintf(
			"add-generic-password -U -s %s -a %s -l %s -X %s\n",
			keychainService, keychainAccount(profile), "epm-cli:"+profile, hex.EncodeToString([]byte(secret))))
		if out, err := cmd.CombinedOutput(); err == nil && !strings.Contains(string(out), "rror") {
			return "keychain", nil
		}
	}
	m := readSecretsFile()
	m[profile] = secret
	return "file", writeSecretsFile(m)
}

// GetSecret returns the stored secret or "".
func GetSecret(profile string) string {
	if KeychainAvailable() {
		out, err := exec.Command("security", "find-generic-password", "-s", keychainService,
			"-a", keychainAccount(profile), "-w").Output()
		if err == nil {
			return strings.TrimRight(string(out), "\n")
		}
	}
	return readSecretsFile()[profile]
}

// DeleteSecret removes a secret from all backends.
func DeleteSecret(profile string) {
	if KeychainAvailable() {
		_ = exec.Command("security", "delete-generic-password", "-s", keychainService,
			"-a", keychainAccount(profile)).Run()
	}
	m := readSecretsFile()
	if _, ok := m[profile]; ok {
		delete(m, profile)
		_ = writeSecretsFile(m)
	}
}
