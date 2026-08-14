//go:build !windows
// +build !windows

package xssh

// baseEnvNames are the process environment variables a session inherits when
// Config.InheritEnv is false: what a shell needs to start and find things.
// Everything else is the operator's business, not the client's.
var baseEnvNames = []string{
	"HOME",
	"LANG",
	"LOGNAME",
	"PATH",
	"SHELL",
	"TZ",
	"USER",
}
