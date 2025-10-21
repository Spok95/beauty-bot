package config

import (
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
		AdminChatID       int64 `mapstructure:"admin_chat_id"`
		RequestTimeoutSec int   `mapstructure:"request_timeout_sec"`
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
}

func Load(path string) (Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	// ENV-переопределения: APP_* и TELEGRAM_TOKEN
	v.SetEnvPrefix("APP")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_")) // telegram.token -> APP_TELEGRAM_TOKEN
	v.AutomaticEnv()
	// Разрешаем также голое TELEGRAM_TOKEN без префикса APP_
	_ = v.BindEnv("telegram.token", "TELEGRAM_TOKEN")
	_ = v.BindEnv("telegram.admin_chat_id", "TELEGRAM_ADMIN_CHAT_ID")
	// Дефолты
	v.SetDefault("telegram.request_timeout_sec", 30)

	var c Config
	if err := v.ReadInConfig(); err != nil {
		return c, err
	}
	if err := v.Unmarshal(&c); err != nil {
		return c, err
	}
	return c, nil
}
