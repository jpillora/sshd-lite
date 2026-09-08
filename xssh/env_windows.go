//go:build windows
// +build windows

package xssh

// envNamesCaseInsensitive is true: Windows treats environment names
// case-insensitively, and os.LookupEnv follows suit.
const envNamesCaseInsensitive = true

// baseEnvNames are the process environment variables a session inherits when
// Config.NoInheritEnv is true. Windows needs considerably more than unix to
// produce a working process: without SystemRoot a child cannot load core DLLs,
// and both cmd.exe and PowerShell rely on ComSpec and PATHEXT to resolve
// commands at all.
//
// PSModulePath earns its place the hard way. Without it PowerShell does not
// fail fast — it spends 30 seconds looking for its modules and then exits 1
// having produced no output, which reads exactly like a hung session. cmd.exe
// is unaffected, so the gap only shows on the default Windows shell.
var baseEnvNames = []string{
	"ALLUSERSPROFILE",
	"APPDATA",
	"COMPUTERNAME",
	"CommonProgramFiles",
	"CommonProgramFiles(x86)",
	"ComSpec",
	"DriverData",
	"HOMEDRIVE",
	"HOMEPATH",
	"LOCALAPPDATA",
	"LOGONSERVER",
	"NUMBER_OF_PROCESSORS",
	"OS",
	"PATHEXT",
	"PROCESSOR_ARCHITECTURE",
	"PROCESSOR_IDENTIFIER",
	"PROCESSOR_LEVEL",
	"PROCESSOR_REVISION",
	"PSModulePath",
	"PUBLIC",
	"Path",
	"ProgramData",
	"ProgramFiles",
	"ProgramFiles(x86)",
	"ProgramW6432",
	"SESSIONNAME",
	"SystemDrive",
	"SystemRoot",
	"TEMP",
	"TMP",
	"USERDOMAIN",
	"USERNAME",
	"USERPROFILE",
	"windir",
}

// systemEnvFile supplies session defaults when present.
const systemEnvFile = ""
