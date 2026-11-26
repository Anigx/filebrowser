package auth

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeWebAuthnOrigin(t *testing.T) {
	for _, test := range []struct{ input, origin, rp string }{
		{"https://FILES.example:443", "https://files.example", "files.example"},
		{"https://files.example:8443", "https://files.example:8443", "files.example"},
		{"http://localhost:8000", "http://localhost:8000", "localhost"},
	} {
		origin, rp, err := NormalizeWebAuthnOrigin(test.input)
		require.NoError(t, err)
		require.Equal(t, test.origin, origin)
		require.Equal(t, test.rp, rp)
		_, err = NewWebAuthn(test.input)
		require.NoError(t, err)
	}
	for _, input := range []string{
		"http://files.example", "http://127.0.0.1:8000", "https://user@files.example",
		"https://*.example", "https://files.example/", "https://files.example/a",
		"https://files.example?", "https://files.example#", "https://files.example:99999",
		"https://files.example:", "https://[::1]", "https://com",
	} {
		t.Run(input, func(t *testing.T) {
			_, _, err := NormalizeWebAuthnOrigin(input)
			require.Error(t, err)
		})
	}
}
