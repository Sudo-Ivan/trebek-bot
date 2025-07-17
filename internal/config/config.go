package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Config holds the application's configuration.
type Config struct {
	BotName      string
	IRCServer    string
	IRCServerTLS string
	IRCChannel   string
	LogFilePath  string
	LogLevel     string
}

// LoadConfig loads configuration from the specified file path.
func LoadConfig(filePath string) (*Config, error) {
	cfg := &Config{}

	// Load from file first
	file, err := os.Open(filePath) // #nosec G304
	if err == nil {
		defer file.Close()
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue // Skip empty lines and comments
			}

			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				return nil, fmt.Errorf("invalid config line: %s", line)
			}

			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])

			switch key {
			case "BOT_NAME":
				cfg.BotName = value
			case "IRC_SERVER":
				cfg.IRCServer = value
			case "IRC_SERVER_TLS":
				cfg.IRCServerTLS = value
			case "IRC_CHANNEL":
				cfg.IRCChannel = value
			case "LOG_FILE_PATH":
				cfg.LogFilePath = value
			case "LOG_LEVEL":
				cfg.LogLevel = value
			default:
				fmt.Printf("Warning: Unknown config key '%s'\n", key)
			}
		}

		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("error reading config file: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to open config file: %w", err)
	}

	// Override with environment variables if set
	if env := os.Getenv("BOT_NAME"); env != "" {
		cfg.BotName = env
	}
	if env := os.Getenv("IRC_SERVER"); env != "" {
		cfg.IRCServer = env
	}
	if env := os.Getenv("IRC_SERVER_TLS"); env != "" {
		cfg.IRCServerTLS = env
	}
	if env := os.Getenv("IRC_CHANNEL"); env != "" {
		cfg.IRCChannel = env
	}
	if env := os.Getenv("LOG_FILE_PATH"); env != "" {
		cfg.LogFilePath = env
	}
	if env := os.Getenv("LOG_LEVEL"); env != "" {
		cfg.LogLevel = env
	}

	// Basic validation
	if cfg.BotName == "" {
		return nil, fmt.Errorf("BOT_NAME is not set in config or environment")
	}
	if cfg.IRCServer == "" && cfg.IRCServerTLS == "" {
		return nil, fmt.Errorf("at least one of IRC_SERVER or IRC_SERVER_TLS must be set in config or environment")
	}
	if cfg.IRCChannel == "" {
		return nil, fmt.Errorf("IRC_CHANNEL is not set in config or environment")
	}

	return cfg, nil
}
