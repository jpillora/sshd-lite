module github.com/jpillora/sshd-lite

go 1.19

// Getting this reified version of an upstream pty pull request
// until it is merged into the main project.
// At that point, we will remove this "replace" statement.
replace github.com/creack/pty => github.com/fusion/pty v1.1.13

require (
	github.com/creack/pty v1.1.18
	golang.org/x/crypto v0.6.0
)

require golang.org/x/sys v0.5.0 // indirect
