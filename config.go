package main

import (
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

//LoadConfig config  and instantiate/return a Viper instance
func LoadConfig() {
	config = viper.New()
	config.SetConfigName("mysql-healthcheck")
	config.AddConfigPath("/etc/sysconfig/")
	config.AddConfigPath("/etc/default/")
	config.AddConfigPath("$HOME/.config/")
	config.AddConfigPath(".")

	if err := config.ReadInConfig(); err != nil { // Handle errors reading the config file
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			// Config file not found; ignore error if desired
		} else {
			logger.Fatal(err)
		}
	}
	if len(config.ConfigFileUsed()) == 0 {
		logger.Warn("No config file found.  Using default configuration!")
	} else if logger.IsLevelEnabled(logrus.DebugLevel) {
		logger.Debugf("Config loaded from %s", config.ConfigFileUsed())
	}

	// HTTP path must contain leading slash
	if config.IsSet("http.path") {
		pathRune := []rune(config.GetString("http.path"))
		if string(pathRune[0:1]) != "/" {
			// Provided path does not begin with leading slash
			config.Set("http.path", "/"+config.GetString("http.path"))
		}
	}

	config.SetDefault("connection.host", "localhost")
	config.SetDefault("connection.port", 3306)
	config.SetDefault("connection.tls.enforced", false)
	config.SetDefault("connection.tls.skip-verify", false)
	config.SetDefault("http.addr", "::")
	config.SetDefault("http.port", 5678)
	config.SetDefault("http.path", "/")
	config.SetDefault("options.available_when_donor", false)
	config.SetDefault("options.available_when_readonly", false)
}
