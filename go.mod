module github.com/jpillora/sshd-lite

go 1.19

// Getting this reified version of an upstream pty pull request
// until it is merged into the main project.
// At that point, we will remove this "replace" statement.
replace github.com/creack/pty => github.com/fusion/pty v1.1.14

require (
	github.com/creack/pty v1.1.18
	golang.org/x/crypto v0.31.0
)

require (
	github.com/kr/fs v0.1.0 // indirect
	github.com/pkg/sftp v1.13.9 // indirect
	golang.org/x/sys v0.28.0 // indirect
)
