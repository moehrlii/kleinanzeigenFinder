package config

import (
	"os"
	"path/filepath"

	"github.com/spf13/viper"
	"github.com/subosito/gotenv"
)

type AppSettings struct {
	Database string `mapstructure:"database"`
}

type TelegramSettings struct {
	Enabled               bool   `mapstructure:"enabled"`
	BotTokenEnv           string `mapstructure:"bot_token_env"`
	ChatIDEnv             string `mapstructure:"chat_id_env"`
	DisableWebPagePreview bool   `mapstructure:"disable_web_page_preview"`
}

func (t TelegramSettings) BotToken() string {
	if t.BotTokenEnv != "" {
		return os.Getenv(t.BotTokenEnv)
	}
	return ""
}

func (t TelegramSettings) ChatID() string {
	if t.ChatIDEnv != "" {
		return os.Getenv(t.ChatIDEnv)
	}
	return ""
}

type DiscordSettings struct {
	Enabled           bool     `mapstructure:"enabled"`
	TokenEnv          string   `mapstructure:"token_env"`
	GuildIDEnv        string   `mapstructure:"guild_id_env"`
	AdminRoleIDs      []string `mapstructure:"admin_role_ids"`
	CategoryName      string   `mapstructure:"category_name"`
	ReviewChannelName string   `mapstructure:"review_channel_name"`
	ChannelPrefix     string   `mapstructure:"channel_prefix"`
	SyncCommands      bool     `mapstructure:"sync_commands"`
}

func (d DiscordSettings) Token() string {
	if d.TokenEnv != "" {
		return os.Getenv(d.TokenEnv)
	}
	return ""
}

func (d DiscordSettings) GuildID() string {
	if d.GuildIDEnv != "" {
		return os.Getenv(d.GuildIDEnv)
	}
	return ""
}

type ScraperSettings struct {
	Headless           bool    `mapstructure:"headless"`
	TimeoutMS          int     `mapstructure:"timeout_ms"`
	DetailDelaySeconds float64 `mapstructure:"detail_delay_seconds"`
	DetailConcurrency  int     `mapstructure:"detail_concurrency"`
	UserAgent          string  `mapstructure:"user_agent"`
}

type ImmoScoutSettings struct {
	APIBaseURL string `mapstructure:"api_base_url"`
	UserAgent  string `mapstructure:"user_agent"`
	PageSize   int    `mapstructure:"page_size"`
}

type LLMSettings struct {
	MaxImages                 int     `mapstructure:"max_images"`
	ImageDetail               string  `mapstructure:"image_detail"`
	Enabled                   bool    `mapstructure:"enabled"`
	APIKeyEnv                 string  `mapstructure:"api_key_env"`
	BaseURL                   string  `mapstructure:"base_url"`
	Model                     string  `mapstructure:"model"`
	MinScore                  int     `mapstructure:"min_score"`
	TimeoutSeconds            float64 `mapstructure:"timeout_seconds"`
	MaxRetries                int     `mapstructure:"max_retries"`
	RetryDelaySeconds         float64 `mapstructure:"retry_delay_seconds"`
	ClassificationConcurrency int     `mapstructure:"classification_concurrency"`
	MaxDescriptionChars       int     `mapstructure:"max_description_chars"`
	SystemPrompt              string  `mapstructure:"system_prompt"`
}

func (l LLMSettings) APIKey() string {
	if l.APIKeyEnv != "" {
		return os.Getenv(l.APIKeyEnv)
	}
	return ""
}

func (l LLMSettings) ModelName() string {
	if override := os.Getenv("OPENAI_MODEL"); override != "" {
		return override
	}
	return l.Model
}

type LoggingSettings struct {
	Level     string `mapstructure:"level"`
	File      string `mapstructure:"file"`
	Rotation  string `mapstructure:"rotation"`
	Retention string `mapstructure:"retention"`
	Backtrace bool   `mapstructure:"backtrace"`
	Diagnose  bool   `mapstructure:"diagnose"`
}

type RulesConfig struct {
	IncludeKeywords []string `mapstructure:"include_keywords"`
	IncludeAny      []string `mapstructure:"include_any"`
	ExcludeKeywords []string `mapstructure:"exclude_keywords"`
	MinPrice        *float64 `mapstructure:"min_price"`
	MaxPrice        *float64 `mapstructure:"max_price"`
}

type MonitorConfig struct {
	Platform                string      `mapstructure:"platform"`
	Name                    string      `mapstructure:"name"`
	SearchURL               string      `mapstructure:"search_url"`
	Criteria                string      `mapstructure:"criteria"`
	Mode                    string      `mapstructure:"mode"`
	IntervalSeconds         int         `mapstructure:"interval_seconds"`
	BackfillIntervalSeconds int         `mapstructure:"backfill_interval_seconds"`
	WatchIntervalSeconds    int         `mapstructure:"watch_interval_seconds"`
	WatchPages              int         `mapstructure:"watch_pages"`
	MaxResults              int         `mapstructure:"max_results"`
	FetchDetails            bool        `mapstructure:"fetch_details"`
	MaxDetailPages          int         `mapstructure:"max_detail_pages"`
	Enabled                 bool        `mapstructure:"enabled"`
	Rules                   RulesConfig `mapstructure:"rules"`
}

type WhatsAppSettings struct {
	Enabled     bool   `mapstructure:"enabled"`
	SessionDB   string `mapstructure:"session_db"`
	TargetPhone string `mapstructure:"target_phone"`
}

type AppConfig struct {
	ImmoScout ImmoScoutSettings `mapstructure:"immoscout"`
	App       AppSettings       `mapstructure:"app"`
	Logging   LoggingSettings   `mapstructure:"logging"`
	Scraper   ScraperSettings   `mapstructure:"scraper"`
	Telegram  TelegramSettings  `mapstructure:"telegram"`
	Discord   DiscordSettings   `mapstructure:"discord"`
	WhatsApp  WhatsAppSettings  `mapstructure:"whatsapp"`
	LLM       LLMSettings       `mapstructure:"llm"`
}

func LoadConfig(path string) (*AppConfig, error) {
	envPath := filepath.Join(filepath.Dir(path), ".env")
	_ = gotenv.Load(envPath)

	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("toml")
	v.SetDefault("immoscout.api_base_url", "https://api.mobile.immobilienscout24.de")
	v.SetDefault("immoscout.user_agent", "ImmoScout_27.12_26.2_._")
	v.SetDefault("immoscout.page_size", 50)
	v.SetDefault("llm.api_key_env", "OPENAI_API_KEY")
	v.SetDefault("llm.base_url", "https://api.openai.com/v1")
	v.SetDefault("llm.model", "gpt-6-astra")
	v.SetDefault("llm.max_images", 8)
	v.SetDefault("llm.image_detail", "auto")
	v.SetDefault("llm.timeout_seconds", 90)

	if err := v.ReadInConfig(); err != nil {
		return nil, err
	}

	var cfg AppConfig

	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	// Fallbacks
	if cfg.App.Database == "" {
		cfg.App.Database = "data/seen.sqlite3"
	}
	if !filepath.IsAbs(cfg.App.Database) {
		cfg.App.Database = filepath.Join(filepath.Dir(path), cfg.App.Database)
	}

	if cfg.Logging.File != "" && !filepath.IsAbs(cfg.Logging.File) {
		cfg.Logging.File = filepath.Join(filepath.Dir(path), cfg.Logging.File)
	}

	if cfg.LLM.SystemPrompt == "" {
		cfg.LLM.SystemPrompt = "Assess marketplace listings against the user requirements and return the requested structured assessment."
	}

	if cfg.LLM.MaxDescriptionChars == 0 {
		cfg.LLM.MaxDescriptionChars = 3500
	}
	if cfg.Scraper.DetailConcurrency <= 0 {
		cfg.Scraper.DetailConcurrency = 1
	}
	if cfg.LLM.ClassificationConcurrency <= 0 {
		cfg.LLM.ClassificationConcurrency = 2
	}

	return &cfg, nil
}
