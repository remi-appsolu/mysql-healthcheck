package main

import (
	"os"
	"runtime"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

const (
	defaultDatabasePort = 3306
	defaultHTTPPort     = 5678
)

// CreateConfig creates a new config instance.
func CreateConfig() *viper.Viper {
	config := viper.New()
	config.SetConfigName(AppName)

	workingDir, err := os.Getwd()
	if err != nil {
		logrus.Fatal(err)
	}

	config.AddConfigPath(workingDir)

	if runtime.GOOS == "windows" {
		config.AddConfigPath(os.Getenv("PROGRAMFILES"))
		config.AddConfigPath(os.Getenv("LOCALAPPDATA"))
	} else {
		config.AddConfigPath("/etc/sysconfig")
		config.AddConfigPath("/etc/default")
		config.AddConfigPath("/etc")
		config.AddConfigPath("$HOME/.config")
	}

	if err := config.ReadInConfig(); err != nil { // Handle errors reading the config file.
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			logrus.Fatal(err)
		}
	}

	if len(config.ConfigFileUsed()) == 0 {
		logrus.Warn("No config file found.  Using default configuration!")
	} else if logrus.IsLevelEnabled(logrus.DebugLevel) {
		logrus.Debugf("Config loaded from %s", config.ConfigFileUsed())
	}

	config.SetDefault("connection.host", "localhost")
	config.SetDefault("connection.port", defaultDatabasePort)
	config.SetDefault("connection.tls.enforced", false)
	config.SetDefault("connection.tls.skip-verify", false)
	config.SetDefault("http.addr", "::")
	config.SetDefault("http.port", defaultHTTPPort)
	config.SetDefault("http.path", "/")
	config.SetDefault("options.available_when_donor", false)
	config.SetDefault("options.available_when_readonly", false)

	// HTTP path must contain leading slash.
	if config.GetString("http.path") != "/" {
		pathRune := []rune(config.GetString("http.path"))
		if string(pathRune[0:1]) != "/" {
			// Provided path does not begin with leading slash.
			config.Set("http.path", "/"+config.GetString("http.path"))
		}
	}

	return config
}
