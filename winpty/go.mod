module github.com/jpillora/sshd-lite/winpty

go 1.25

// Use photostorm's fork for Windows PTY support
replace github.com/creack/pty => github.com/photostorm/pty v1.1.19-0.20230903182454-31354506054b

require github.com/creack/pty v1.1.24

require golang.org/x/sys v0.39.0 // indirect
