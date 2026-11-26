package auth

import (
	"errors"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"golang.org/x/net/publicsuffix"
)

const WebAuthnTimeout = 2 * time.Minute

var dnsLabel = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func NormalizeWebAuthnOrigin(raw string) (origin, rpID string, err error) {
	invalid := errors.New("WebAuthn origin must be a single HTTPS origin (or HTTP localhost), without a path")
	u, err := url.Parse(raw)
	if err != nil || u == nil || u.Host == "" || u.User != nil || u.Path != "" ||
		strings.ContainsAny(raw, "?#[]") || strings.HasSuffix(u.Host, ":") {
		return "", "", invalid
	}
	host := strings.ToLower(u.Hostname())
	if u.Scheme != "https" && (u.Scheme != "http" || host != "localhost") {
		return "", "", invalid
	}
	if host != "localhost" {
		if len(host) > 253 || net.ParseIP(host) != nil {
			return "", "", invalid
		}
		for _, label := range strings.Split(host, ".") {
			if !dnsLabel.MatchString(label) {
				return "", "", invalid
			}
		}
		if _, err := publicsuffix.EffectiveTLDPlusOne(host); err != nil {
			return "", "", invalid
		}
	}
	address := host
	if port := u.Port(); port != "" {
		n, err := strconv.ParseUint(port, 10, 16)
		if err != nil || n == 0 {
			return "", "", invalid
		}
		if u.Scheme == "https" && n != 443 || u.Scheme == "http" && n != 80 {
			address += ":" + strconv.FormatUint(n, 10)
		}
	}
	return u.Scheme + "://" + address, host, nil
}

func NewWebAuthn(raw string) (*webauthn.WebAuthn, error) {
	origin, rpID, err := NormalizeWebAuthnOrigin(raw)
	if err != nil {
		return nil, err
	}
	timeout := webauthn.TimeoutConfig{Enforce: true, Timeout: WebAuthnTimeout}
	return webauthn.New(&webauthn.Config{
		RPDisplayName: "File Browser",
		RPID:          rpID,
		RPOrigins:     []string{origin},
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			AuthenticatorAttachment: protocol.CrossPlatform,
			ResidentKey:             protocol.ResidentKeyRequirementDiscouraged,
			UserVerification:        protocol.VerificationRequired,
		},
		AttestationPreference: protocol.PreferNoAttestation,
		Timeouts:              webauthn.TimeoutsConfig{Login: timeout, Registration: timeout},
	})
}
