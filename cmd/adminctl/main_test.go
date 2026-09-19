package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"mywebsite/internal/app"
)

func TestRunVersionAndRejectsPasswordArgument(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run([]string{"version"}, CommandDeps{Out: &out, Err: &errOut}); code != 0 {
		t.Fatalf("version exit code = %d", code)
	}
	if got := strings.TrimSpace(out.String()); got != "dev" {
		t.Fatalf("version output = %q", got)
	}
	if code := Run([]string{"admin", "create", "--password", "secret"}, CommandDeps{Out: &out, Err: &errOut}); code != 2 {
		t.Fatalf("password argument exit code = %d", code)
	}
	if strings.Contains(out.String()+errOut.String(), "secret") {
		t.Fatal("password argument was echoed")
	}
}

func TestCreateAdminRejectsNonTTYAndMismatchedPasswords(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run([]string{"admin", "create"}, CommandDeps{
		In:         strings.NewReader("admin user\n"),
		Out:        &out,
		Err:        &errOut,
		IsTerminal: func() bool { return false },
	}); code != 2 {
		t.Fatalf("non-TTY exit code = %d", code)
	}
	if !strings.Contains(errOut.String(), "interactive terminal") {
		t.Fatalf("non-TTY error = %q", errOut.String())
	}

	out.Reset()
	errOut.Reset()
	passwords := [][]byte{[]byte("a sufficiently long password"), []byte("different password")}
	passwordIndex := 0
	if code := Run([]string{"admin", "create"}, CommandDeps{
		In:         strings.NewReader("admin user\n"),
		Out:        &out,
		Err:        &errOut,
		IsTerminal: func() bool { return true },
		ReadPassword: func() ([]byte, error) {
			value := passwords[passwordIndex]
			passwordIndex++
			return value, nil
		},
		LoadConfig: func() (app.Config, error) { return app.Config{}, errors.New("must not load config") },
	}); code != 2 {
		t.Fatalf("mismatched-password exit code = %d", code)
	}
	if !strings.Contains(errOut.String(), "do not match") {
		t.Fatalf("mismatched-password error = %q", errOut.String())
	}
	if strings.Contains(out.String()+errOut.String(), "sufficiently long password") {
		t.Fatal("password was echoed")
	}
}
