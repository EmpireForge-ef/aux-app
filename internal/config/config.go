// Package config loads application configuration via viper from (in order of
// precedence) command-line flags, AUX_-prefixed environment variables, and an
// optional aux.yaml config file.
package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

type Spotify struct {
	ClientID     string `mapstructure:"client_id"`
	ClientSecret string `mapstructure:"client_secret"`
	RedirectURL  string `mapstructure:"redirect_url"`
}

type Anthropic struct {
	APIKey    string `mapstructure:"api_key"`
	Model     string `mapstructure:"model"`
	MaxTokens int64  `mapstructure:"max_tokens"`
	// ContextLimit is the model's usable input-context size in tokens. When a
	// chat's estimated history approaches it, older turns are summarised so the
	// request stays under the limit. Default 200000.
	ContextLimit int64 `mapstructure:"context_limit"`
}

type Admin struct {
	// Password protects the app and its admin settings. When empty, and no
	// OIDC provider is configured, authentication is disabled (local
	// development only).
	Password string `mapstructure:"password"`
}

// OIDC configures optional single-sign-on login against an OpenID Connect
// provider (e.g. Keycloak). It is enabled when IssuerURL and ClientID are
// both set.
type OIDC struct {
	// IssuerURL is the provider's base URL, e.g.
	// https://keycloak.example.com/realms/aux — its /.well-known/openid-
	// configuration document is fetched for discovery.
	IssuerURL    string `mapstructure:"issuer_url"`
	ClientID     string `mapstructure:"client_id"`
	ClientSecret string `mapstructure:"client_secret"`
	// RedirectURL defaults to <public_url>/api/admin/oidc/callback and must
	// be registered as a valid redirect URI in the provider.
	RedirectURL string `mapstructure:"redirect_url"`
	// Scopes is a space-separated list; defaults to "openid profile email".
	Scopes string `mapstructure:"scopes"`
	// AllowedEmails, when non-empty, is a comma-separated allowlist: only
	// those (verified) email addresses may log in. Empty means any user the
	// provider authenticates is allowed.
	AllowedEmails string `mapstructure:"allowed_emails"`
}

// Enabled reports whether OIDC login should be offered.
func (o OIDC) Enabled() bool {
	return o.IssuerURL != "" && o.ClientID != ""
}

// Listening configures the passive listening-profile collector.
type Listening struct {
	// Enabled turns the background poller on (default true). It no-ops until
	// the user connects Spotify.
	Enabled bool `mapstructure:"enabled"`
	// PollInterval is how often recent plays are recorded (default 20m).
	PollInterval time.Duration `mapstructure:"poll_interval"`
}

type Config struct {
	Addr      string `mapstructure:"addr"`
	StaticDir string `mapstructure:"static_dir"`
	PublicURL string `mapstructure:"public_url"`
	TokenFile string `mapstructure:"token_file"`
	// DatabaseURL is the PostgreSQL connection string, e.g.
	// postgres://user:pass@host:5432/aux?sslmode=disable. Required.
	DatabaseURL string `mapstructure:"database_url"`
	// The *File / *Dir paths below are only used for the one-time import of
	// pre-database JSON data on first startup; new data lives in PostgreSQL.
	SettingsFile    string `mapstructure:"settings_file"`
	ChatsDir        string `mapstructure:"chats_dir"`
	PreferencesFile string `mapstructure:"preferences_file"`
	TempPlaylists   string `mapstructure:"temp_playlists_file"`
	HistoryFile     string `mapstructure:"history_file"`
	PlaylistCache   string `mapstructure:"playlist_cache_file"`
	// Timezone is an IANA name (e.g. "Europe/Berlin") used to render the
	// current time given to the AI each turn. Empty means the server's local
	// zone. The admin settings UI can override it at runtime.
	Timezone string `mapstructure:"timezone"`
	// Location is a "lat,lon" pair or a place name used for weather tagging in
	// the listening profile. Empty disables the weather dimension. Also
	// settable in the admin UI.
	Location  string    `mapstructure:"location"`
	Spotify   Spotify   `mapstructure:"spotify"`
	Anthropic Anthropic `mapstructure:"anthropic"`
	Admin     Admin     `mapstructure:"admin"`
	OIDC      OIDC      `mapstructure:"oidc"`
	Listening Listening `mapstructure:"listening"`
}

// New builds a viper instance with defaults, env bindings, and an optional
// config file. cfgFile may be empty, in which case aux.yaml is searched for
// in the working directory and /etc/aux.
func New(cfgFile string, flags *pflag.FlagSet) (*Config, error) {
	v := viper.New()

	v.SetDefault("addr", ":8080")
	v.SetDefault("static_dir", "frontend/dist")
	// Spotify forbids "localhost" in redirect URIs: use the explicit loopback
	// IP for local development, HTTPS everywhere else.
	v.SetDefault("public_url", "http://127.0.0.1:8080")
	v.SetDefault("token_file", "spotify-token.json")
	v.SetDefault("database_url", "")
	v.SetDefault("settings_file", "aux-settings.json")
	v.SetDefault("chats_dir", "chats")
	v.SetDefault("preferences_file", "aux-preferences.json")
	v.SetDefault("temp_playlists_file", "aux-temp-playlists.json")
	v.SetDefault("history_file", "aux-history.json")
	v.SetDefault("playlist_cache_file", "aux-playlist-cache.json")
	v.SetDefault("timezone", "")
	v.SetDefault("location", "")
	v.SetDefault("listening.enabled", true)
	v.SetDefault("listening.poll_interval", "20m")
	v.SetDefault("admin.password", "")
	v.SetDefault("anthropic.model", "claude-opus-4-8")
	v.SetDefault("anthropic.max_tokens", 8192)
	v.SetDefault("anthropic.context_limit", 200000)
	v.SetDefault("oidc.scopes", "openid profile email")
	// Keys without a meaningful default still need to be registered, or
	// viper's Unmarshal won't see values that come only from the environment.
	v.SetDefault("anthropic.api_key", "")
	v.SetDefault("spotify.client_id", "")
	v.SetDefault("spotify.client_secret", "")
	v.SetDefault("spotify.redirect_url", "")
	v.SetDefault("oidc.issuer_url", "")
	v.SetDefault("oidc.client_id", "")
	v.SetDefault("oidc.client_secret", "")
	v.SetDefault("oidc.redirect_url", "")
	v.SetDefault("oidc.allowed_emails", "")

	v.SetEnvPrefix("AUX")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if cfgFile != "" {
		v.SetConfigFile(cfgFile)
	} else {
		v.SetConfigName("aux")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("/etc/aux")
	}
	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if cfgFile != "" || !errorsAs(err, &notFound) {
			return nil, fmt.Errorf("read config: %w", err)
		}
	}

	if flags != nil {
		if err := v.BindPFlags(flags); err != nil {
			return nil, fmt.Errorf("bind flags: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	base := strings.TrimSuffix(cfg.PublicURL, "/")
	if cfg.Spotify.RedirectURL == "" {
		cfg.Spotify.RedirectURL = base + "/api/auth/callback"
	}
	if cfg.OIDC.RedirectURL == "" {
		cfg.OIDC.RedirectURL = base + "/api/admin/oidc/callback"
	}
	return &cfg, nil
}

// errorsAs is a tiny indirection so the viper sentinel check reads cleanly above.
func errorsAs(err error, target *viper.ConfigFileNotFoundError) bool {
	if e, ok := err.(viper.ConfigFileNotFoundError); ok {
		*target = e
		return true
	}
	return false
}
