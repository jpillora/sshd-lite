package sshd

// Config is the configuration for the server
type Config struct {
	Host       string
	Port       string
	Shell      string
	KeyFile    string
	KeySeed    string
	AuthType   string
	KeepAlive  int
	IgnoreEnv  bool
	LogVerbose bool
}

// NewConfig creates a new Config
func NewConfig(keyFile string, keySeed string) *Config {
	return &Config{
		KeyFile: keyFile,
		KeySeed: keySeed,
	}
}
