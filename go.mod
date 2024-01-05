module github.com/jpillora/sshd-lite

go 1.21

// This replace provides powershell support
replace github.com/creack/pty => github.com/photostorm/pty v1.1.19-0.20230903182454-31354506054b

require (
	github.com/creack/pty v1.1.18
	golang.org/x/crypto v0.17.0
)

require golang.org/x/sys v0.15.0 // indirect
