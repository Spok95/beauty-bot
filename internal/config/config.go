package config

import (
	"strconv"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	App struct {
		Env      string
		Timezone string
	} `mapstructure:"app"`

	Telegram struct {
		Token             string
		AdminChatID       int64   `mapstructure:"admin_chat_id"`
		AdminIDs          []int64 `mapstructure:"-"`
		RequestTimeoutSec int     `mapstructure:"request_timeout_sec"`
	} `mapstructure:"telegram"`

	HTTP struct {
		Addr string
	} `mapstructure:"http"`

	Postgres struct {
		DSN string
	} `mapstructure:"postgres"`

	Metrics struct {
		Enabled bool
	} `mapstructure:"metrics"`

	Payments struct {
		BaseURL string `mapstructure:"base_url"`
	} `mapstructure:"payments"`
}

func Load(path string) (Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	// ENV-переопределения: APP_* и TELEGRAM_TOKEN
	v.SetEnvPrefix("APP")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_")) // telegram.token -> APP_TELEGRAM_TOKEN
	v.AutomaticEnv()
	_ = v.BindEnv("telegram.token", "TELEGRAM_TOKEN")
	_ = v.BindEnv("telegram.admin_chat_id", "TELEGRAM_ADMIN_CHAT_ID")
	_ = v.BindEnv("telegram.admin_ids", "TELEGRAM_ADMIN_IDS")
	_ = v.BindEnv("postgres.dsn", "POSTGRES_DSN", "APP_POSTGRES_DSN")

	// Дефолты
	v.SetDefault("telegram.request_timeout_sec", 30)
	v.SetDefault("telegram.admin_ids", "")

	var c Config
	if err := v.ReadInConfig(); err != nil {
		return c, err
	}
	if err := v.Unmarshal(&c); err != nil {
		return c, err
	}

	// парсим список админов
	raw := v.GetString("telegram.admin_ids")
	if raw == "" {
		// fallback: если список не задан, но задан adminChatID — считаем его единственным админом
		if c.Telegram.AdminChatID != 0 {
			c.Telegram.AdminIDs = []int64{c.Telegram.AdminChatID}
		}
		return c, nil
	}

	parts := strings.Split(raw, ",")
	var ids []int64
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		id, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			continue
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 && c.Telegram.AdminChatID != 0 {
		ids = []int64{c.Telegram.AdminChatID}
	}
	c.Telegram.AdminIDs = ids

	return c, nil
}
