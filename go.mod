module github.com/jpillora/sshd-lite

go 1.24.0

// This replace provides powershell support
replace github.com/creack/pty => github.com/photostorm/pty v1.1.19-0.20230903182454-31354506054b

require (
	github.com/creack/pty v1.1.18
	github.com/pkg/sftp v1.13.9
	golang.org/x/crypto v0.41.0
)

require (
	github.com/kr/fs v0.1.0 // indirect
	golang.org/x/sys v0.36.0 // indirect
)
