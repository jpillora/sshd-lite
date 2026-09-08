//go:build !windows
// +build !windows

package xssh

// envNamesCaseInsensitive is false: unix environment names are case-sensitive.
const envNamesCaseInsensitive = false

// baseEnvNames are the process environment variables a session inherits when
// Config.NoInheritEnv is true: what a shell needs to start and find things.
var baseEnvNames = []string{
	"HOME",
	"LANG",
	"LOGNAME",
	"PATH",
	"SHELL",
	"TZ",
	"USER",
}

// systemEnvFile supplies session defaults when present.
const systemEnvFile = "/etc/environment"
