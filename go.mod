module github.com/jpillora/sshd-lite

go 1.24.0

// This replace provides powershell support
replace github.com/creack/pty => github.com/photostorm/pty v1.1.19-0.20230903182454-31354506054b

require (
	github.com/creack/pty v1.1.18
	github.com/jpillora/jplog v1.0.2
	github.com/pkg/sftp v1.13.10
	golang.org/x/crypto v0.43.0
)

require (
	github.com/aymanbagabas/go-osc52/v2 v2.0.1 // indirect
	github.com/kr/fs v0.1.0 // indirect
	github.com/lucasb-eyer/go-colorful v1.3.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/muesli/termenv v0.16.0 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	golang.org/x/sys v0.38.0 // indirect
)
