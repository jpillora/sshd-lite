module github.com/jpillora/sshd-lite

go 1.15

// Getting this reified version of an upstream pty pull request
// until it is merged into the main project.
// At that point, we will remove this "replace" statement.
replace github.com/creack/pty => github.com/fusion/pty v1.1.13

require (
	github.com/creack/pty v1.1.16
	golang.org/x/crypto v0.0.0-20210921155107-089bfa567519
	golang.org/x/sys v0.0.0-20211002104244-808efd93c36d // indirect
	golang.org/x/term v0.0.0-20210927222741-03fcf44c2211 // indirect
)
