module github.com/jpillora/sshd-lite

go 1.15

# Gettung this reified version of an upstream pty pull request
# until it is merged into the main project.
# At that point, we will remove this "replace" statement.
replace github.com/creack/pty => github.com/fusion/pty v1.1.13

require (
	github.com/creack/pty v1.1.13
	golang.org/x/crypto v0.0.0-20210921155107-089bfa567519
)
