module github.com/jpillora/sshd-lite

go 1.24

// This replace provides powershell support
replace github.com/creack/pty => github.com/photostorm/pty v1.1.19-0.20230903182454-31354506054b

require (
	github.com/creack/pty v1.1.18
	github.com/pkg/sftp v1.13.9
	golang.org/x/crypto v0.41.0
)

require (
	github.com/hashicorp/errwrap v1.0.0 // indirect
	github.com/hashicorp/go-multierror v1.0.0 // indirect
	github.com/posener/complete v1.2.2-0.20190308074557-af07aa5181b3 // indirect
)

require (
	github.com/jpillora/opts v1.2.3
	github.com/kr/fs v0.1.0 // indirect
	golang.org/x/sys v0.35.0 // indirect
)
