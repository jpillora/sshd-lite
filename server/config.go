package gosshpot

//Config is ...
type Config struct {
	Host       string
	Port       string
	Shell      string
	KeyFile    string
	KeySeed    string
	AuthType   string
	IgnoreEnv  bool
	LogVerbose bool
	MaxAuthTries int
}

//NewConfig creates a new Config
func NewConfig(keyFile string, keySeed string) *Config {
	return &Config{
		KeyFile: keyFile,
		KeySeed: keySeed,
	}
}
