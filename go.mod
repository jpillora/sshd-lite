module github.com/jpillora/sshd-lite

go 1.15

replace github.com/creack/pty => ./pty

require (
	github.com/creack/pty v1.1.11
	golang.org/x/crypto v0.0.0-20201116153603-4be66e5b6582
)
