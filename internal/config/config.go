package config

import "github.com/spf13/viper"

type Config struct {
	App struct {
		Env      string
		Timezone string
	} `mapstructure:"app"`

	Telegram struct {
		Token       string
		AdminChatID int64 `mapstructure:"admin_chat_id"`
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
	// Позже можно будет переопределять через ENV (APP_*), если надо
	v.SetEnvPrefix("APP")
	v.AutomaticEnv()

	var c Config
	if err := v.ReadInConfig(); err != nil {
		return c, err
	}
	if err := v.Unmarshal(&c); err != nil {
		return c, err
	}
	return c, nil
}
