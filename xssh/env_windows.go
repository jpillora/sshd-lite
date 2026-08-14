//go:build windows
// +build windows

package xssh

// baseEnvNames are the process environment variables a session inherits when
// Config.InheritEnv is false. Windows needs considerably more than Unix to
// produce a working process: without SystemRoot a child cannot load core DLLs,
// and both cmd.exe and PowerShell rely on ComSpec and PATHEXT to resolve
// commands at all.
var baseEnvNames = []string{
	"ALLUSERSPROFILE",
	"APPDATA",
	"CommonProgramFiles",
	"CommonProgramFiles(x86)",
	"ComSpec",
	"HOMEDRIVE",
	"HOMEPATH",
	"LOCALAPPDATA",
	"NUMBER_OF_PROCESSORS",
	"OS",
	"PATHEXT",
	"PROCESSOR_ARCHITECTURE",
	"Path",
	"ProgramData",
	"ProgramFiles",
	"ProgramFiles(x86)",
	"ProgramW6432",
	"PUBLIC",
	"SystemDrive",
	"SystemRoot",
	"TEMP",
	"TMP",
	"USERDOMAIN",
	"USERNAME",
	"USERPROFILE",
	"windir",
}
