module github.com/jpillora/sshd-lite

go 1.25

tool github.com/jpillora/md-tmpl

require (
	github.com/creack/pty v1.1.24
	github.com/jpillora/jplog v1.0.2
	github.com/jpillora/sshd-lite/winpty v0.0.0-20260106042502-3a28ff230268
	github.com/pkg/sftp v1.13.10
	golang.org/x/crypto v0.46.0
	google.golang.org/grpc v1.78.0
	gopkg.in/yaml.v3 v3.0.1
)

require github.com/jpillora/md-tmpl v1.3.0 // indirect

require (
	github.com/aymanbagabas/go-osc52/v2 v2.0.1 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/jpillora/opts v1.2.3
	github.com/kr/fs v0.1.0 // indirect
	github.com/lucasb-eyer/go-colorful v1.3.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/muesli/termenv v0.16.0 // indirect
	github.com/posener/complete v1.2.3 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	golang.org/x/sys v0.40.0 // indirect
)
