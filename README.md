# sshd-lite

A feature-light Secure Shell Daemon `sshd(8)` written in Go (Golang). A slightly more practical version of the SSH daemon described in this blog post http://blog.gopheracademy.com/go-and-ssh/. **Warning, this is beta software**.

### Install

**Binaries**

See [the latest release](https://github.com/jpillora/sshd-lite/releases/latest)

**Source**

``` sh
$ go get -v github.com/jpillora/sshd-lite
```

### Features

* Cross platform single binary
* No dependencies
* Remote shells
* Authentication (`user:pass` and `authorized_keys`)
* Seed server-key generation

### Usage

```
$ sshd-lite --help
```

<tmpl,code: go run main.go --help>
```

	Usage: sshd-lite [options] <auth>

	Version: 0.0.0

	Options:
	  --host, listening interface (defaults to all)
	  --port -p, listening port (defaults to 22, then fallsback to 2200)
	  --shell, the type of to use shell for remote sessions (defaults to bash)
	  --keyfile, a filepath to an private key (for example, an 'id_rsa' file)
	  --keyseed, a string to use to seed key generation
	  --version, display version
	  -v, verbose logs

	<auth> must be set to one of:
	  1. a username and password string separated by a colon ("user:pass")
	  2. a path to an ssh authorized keys file ("~/.ssh/authorized_keys")
	  3. "none" to disable client authentication :WARNING: very insecure

	Notes:
	  * if no keyfile and no keyseed are set, a random RSA2048 key is used
	  * once authenticated, clients will have access to a shell of the
	  current user. sshd-lite does not lookup system users.
	  * sshd-lite only supports remotes shells. tunnelling and command
	  execution are not currently supported.

	Read more: https://github.com/jpillora/sshd-lite

```
</tmpl>

### Programmatic Usage

[![GoDoc](https://godoc.org/github.com/jpillora/sshd-lite/server?status.svg)](https://godoc.org/github.com/jpillora/sshd-lite/server)

### Todo

* Add windows support using PowerShell?

#### MIT License

Copyright © 2015 Jaime Pillora &lt;dev@jpillora.com&gt;

Permission is hereby granted, free of charge, to any person obtaining
a copy of this software and associated documentation files (the
'Software'), to deal in the Software without restriction, including
without limitation the rights to use, copy, modify, merge, publish,
distribute, sublicense, and/or sell copies of the Software, and to
permit persons to whom the Software is furnished to do so, subject to
the following conditions:

The above copyright notice and this permission notice shall be
included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED 'AS IS', WITHOUT WARRANTY OF ANY KIND,
EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT.
IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY
CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT,
TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE
SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.