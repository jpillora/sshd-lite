package sshd

//Config is ...
type Config struct {
	Host       string
	Port       string
	Shell      string
	KeyFile    string
	KeySeed    string
	AuthType   string
	LogVerbose bool
}

//NewConfig creates a new Config
func NewConfig(keyFile string, keySeed string) *Config {
	return &Config{
		KeyFile: keyFile,
		KeySeed: keySeed,
	}
}
