package cmd

import (
	"io"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/filebrowser/filebrowser/v2/auth"
	"github.com/filebrowser/filebrowser/v2/settings"
)

func TestPrintSettingsDoesNotExposeAuthenticatorSecret(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	original := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = original }()

	const secret = "test-only-captcha-secret"
	err = printSettings(&settings.Server{}, &settings.Settings{}, auth.JSONAuth{
		ReCaptcha: &auth.ReCaptcha{Secret: secret},
	})
	w.Close()
	if err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(output), secret) || !strings.Contains(string(output), "Auth Method:") {
		t.Fatal("configuration output must show the auth method without exposing the authenticator secret")
	}
}

func TestParseUsernameOrIDUsesNativeUintRange(t *testing.T) {
	max := strconv.FormatUint(uint64(^uint(0)), 10)
	if username, id := parseUsernameOrID(max); username != "" || id != ^uint(0) {
		t.Fatalf("maximum native ID parsed as (%q, %d)", username, id)
	}
	outOfRange := "18446744073709551616"
	if strconv.IntSize == 32 {
		outOfRange = "4294967296"
	}
	if username, id := parseUsernameOrID(outOfRange); username != outOfRange || id != 0 {
		t.Fatalf("out-of-range ID parsed as (%q, %d)", username, id)
	}
}
