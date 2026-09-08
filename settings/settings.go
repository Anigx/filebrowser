package settings

import (
	"crypto/rand"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/netip"
	"path/filepath"
	"strings"
	"time"

	"github.com/filebrowser/filebrowser/v2/rules"
)

const DefaultUsersHomeBasePath = "/users"
const DefaultLogoutPage = "/login"
const DefaultMinimumPasswordLength = 12
const DefaultFileMode = 0640
const DefaultDirMode = 0750

// AuthMethod describes an authentication method.
type AuthMethod string

const (
	defaultSandboxTimeout     = 30 * time.Second
	defaultSandboxOutputLimit = int64(1 << 20) // 1 MiB
)

// ExecutionSandbox configures a trusted launcher that isolates all File
// Browser-managed commands. Command must be an absolute launcher invocation
// ending in "--"; the executor appends the untrusted target after it.
//
// A launcher is intentionally external: it keeps this binary portable to the
// minimal BusyBox Docker image while allowing operators to use the sandbox
// mechanism available in their deployment (for example, bwrap or a dedicated
// namespace/chroot wrapper).
type ExecutionSandbox struct {
	Enabled        bool     `json:"enabled"`
	Command        []string `json:"command"`
	Timeout        string   `json:"timeout"`
	MaxOutputBytes int64    `json:"maxOutputBytes"`
}

// Validate rejects configurations that could cause enabled execution to bypass
// the sandbox launcher. It is also called immediately before each execution so
// a malformed value inserted outside the normal settings storage fails closed.
func (s ExecutionSandbox) Validate() error {
	if !s.Enabled {
		return nil
	}
	if len(s.Command) < 2 || !filepath.IsAbs(s.Command[0]) || s.Command[len(s.Command)-1] != "--" {
		return fmt.Errorf("execution sandbox requires an absolute launcher command ending with --")
	}
	if strings.TrimSpace(s.Command[0]) == "" {
		return fmt.Errorf("execution sandbox launcher cannot be empty")
	}
	if s.Timeout != "" {
		d, err := time.ParseDuration(s.Timeout)
		if err != nil || d <= 0 {
			return fmt.Errorf("execution sandbox timeout must be a positive duration")
		}
	}
	if s.MaxOutputBytes < 0 {
		return fmt.Errorf("execution sandbox maxOutputBytes cannot be negative")
	}
	return nil
}

// TimeoutDuration returns the safe deadline used for sandboxed processes.
func (s ExecutionSandbox) TimeoutDuration() time.Duration {
	if !s.Enabled {
		return 0
	}
	if s.Timeout == "" {
		return defaultSandboxTimeout
	}
	d, err := time.ParseDuration(s.Timeout)
	if err != nil || d <= 0 {
		return defaultSandboxTimeout
	}
	return d
}

// OutputLimit returns the safe output cap used for sandboxed processes.
func (s ExecutionSandbox) OutputLimit() int64 {
	if s.MaxOutputBytes == 0 {
		return defaultSandboxOutputLimit
	}
	return s.MaxOutputBytes
}

// Settings contain the main settings of the application.
type Settings struct {
	Key                   []byte              `json:"key"`
	Signup                bool                `json:"signup"`
	HideLoginButton       bool                `json:"hideLoginButton"`
	CreateUserDir         bool                `json:"createUserDir"`
	UserHomeBasePath      string              `json:"userHomeBasePath"`
	Defaults              UserDefaults        `json:"defaults"`
	AuthMethod            AuthMethod          `json:"authMethod"`
	LogoutPage            string              `json:"logoutPage"`
	Branding              Branding            `json:"branding"`
	Tus                   Tus                 `json:"tus"`
	Commands              map[string][]string `json:"commands"`
	Shell                 []string            `json:"shell"`
	ExecutionSandbox      ExecutionSandbox    `json:"executionSandbox"`
	Rules                 []rules.Rule        `json:"rules"`
	MinimumPasswordLength uint                `json:"minimumPasswordLength"`
	FileMode              fs.FileMode         `json:"fileMode"`
	DirMode               fs.FileMode         `json:"dirMode"`
	HideDotfiles          bool                `json:"hideDotfiles"`
}

// GetRules implements rules.Provider.
func (s *Settings) GetRules() []rules.Rule {
	return s.Rules
}

// Server specific settings.
type Server struct {
	Root                  string `json:"root"`
	BaseURL               string `json:"baseURL"`
	Socket                string `json:"socket"`
	TLSKey                string `json:"tlsKey"`
	TLSCert               string `json:"tlsCert"`
	Port                  string `json:"port"`
	Address               string `json:"address"`
	Log                   string `json:"log"`
	EnableThumbnails      bool   `json:"enableThumbnails"`
	ResizePreview         bool   `json:"resizePreview"`
	EnableExec            bool   `json:"enableExec"`
	TypeDetectionByHeader bool   `json:"typeDetectionByHeader"`
	ImageResolutionCal    bool   `json:"imageResolutionCalculation"`
	AuthHook              string `json:"authHook"`
	// TrustedProxyIPs is the explicit allowlist of reverse proxies permitted to
	// supply a proxy-auth identity header. CIDRs and individual IPs are accepted.
	// An empty list trusts no peer, which is the safe default.
	TrustedProxyIPs        []string `json:"trustedProxyIPs"`
	TokenExpirationTime    string   `json:"tokenExpirationTime"`
	FollowExternalSymlinks bool     `json:"followExternalSymlinks"`

	// CaseInsensitiveFs is detected from Root at startup rather than
	// configured, and tells the rule checker to match paths case-insensitively.
	// It is never persisted.
	CaseInsensitiveFs bool `json:"-"`
}

// Clean cleans any variables that might need cleaning.
func (s *Server) Clean() {
	s.BaseURL = strings.TrimSuffix(s.BaseURL, "/")
}

// IsTrustedProxy reports whether remoteAddr belongs to the configured proxy
// allowlist. Invalid configuration entries never grant trust.
func (s *Server) IsTrustedProxy(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	for _, raw := range s.TrustedProxyIPs {
		if prefix, err := netip.ParsePrefix(raw); err == nil && prefix.Contains(ip) {
			return true
		}
		if trusted, err := netip.ParseAddr(raw); err == nil && trusted == ip {
			return true
		}
	}
	return false
}

func (s *Server) GetTokenExpirationTime(fallback time.Duration) time.Duration {
	if s.TokenExpirationTime == "" {
		return fallback
	}

	duration, err := time.ParseDuration(s.TokenExpirationTime)
	if err != nil {
		log.Printf("[WARN] Failed to parse tokenExpirationTime: %v", err)
		return fallback
	}
	return duration
}

// GenerateKey generates a key of 512 bits.
func GenerateKey() ([]byte, error) {
	b := make([]byte, 64)
	_, err := rand.Read(b)
	// Note that err == nil only if we read len(b) bytes.
	if err != nil {
		return nil, err
	}

	return b, nil
}
