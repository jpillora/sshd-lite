# sshd-lite

A feature-light Secure Shell Daemon `sshd(8)` written in Go (Golang). A slightly more practical version of the SSH daemon described in this blog post http://blog.gopheracademy.com/go-and-ssh/. **Warning, this is beta software**.

### Install

**Binaries**

See [the latest release](https://github.com/jpillora/sshd-lite/releases/latest)

One-line-download and install `curl https://i.jpillora.com/sshd-lite! | sh`

**Source**

``` sh
$ go get -v github.com/jpillora/sshd-lite
```

### Features

* Cross platform binaries with no dependencies
* Remote shells (`bash` in linux/mac and `powershell` in windows)
* Authentication (`user:pass`, `~/.ssh/authorized_keys`, `github.com/foobar`, or `none`)
* Seed server-key generation
* Enable SFTP support with `--sftp` (allows `scp` and other SFTP clients)
* Enable TCP forwarding with `--tcp-forwarding` (both local and reverse forwarding)

### Quick use

Server

``` sh
$ curl https://i.jpillora.com/sshd-lite! | sh
Downloading: sshd-lite_1.1.0_darwin_amd64
######################################### 100.0%
$ sshd-lite john:doe
2020/12/09 23:55:08 Key from system rng
2020/12/09 23:55:08 RSA key fingerprint is SHA256:kLK6RD2tCqSfvYxdMPa3YRNwUJS09njfE1hXoqOYXG4.
2020/12/09 23:55:08 Authentication enabled (user 'john')
2020/12/09 23:55:08 Listening on 0.0.0.0:2200...
```

Client

```sh
$ ssh john@localhost -p 2200
The authenticity of host '[localhost]:2200 ([::1]:2200)' can't be established.
RSA key fingerprint is SHA256:kLK6RD2tCqSfvYxdMPa3YRNwUJS09njfE1hXoqOYXG4.
Are you sure you want to continue connecting (yes/no/[fingerprint])? yes # note fingerprint matches
john@localhost's password: *** # enter password from above
bash-3.2$ date
Wed  9 Dec 2020 23:57:22 AEDT
```

### Usage

```
$ sshd-lite --help
```

<!-- regenerate help with: go install -v github.com/jpillora/md-tmpl@latest && md-tmpl -w README.md -->
<!--tmpl,code=plain:echo "$ sshd-lite --help" && go run main.go --help 2>/dev/null | sed 's#0.0.0-src#X.Y.Z#' -->
``` plain 
$ sshd-lite --help

  Usage: sshd-lite [options] <auth>

  Version: X.Y.Z

  Options:
    --host, listening interface (defaults to all)
    --port -p, listening port (defaults to 22, then fallsback to 2200)
    --shell, the type of to use shell for remote sessions (defaults to $SHELL, then bash/powershell)
    --keyfile, a filepath to an private key (for example, an 'id_rsa' file)
    --keyseed, a string to use to seed key generation
    --noenv, ignore environment variables provided by the client
    --keepalive, server keep alive interval seconds (defaults to 60, 0 to disable)
    --sftp -s, enable the SFTP subsystem (disabled by default)
    --tcp-forwarding -t, enable TCP forwarding (both local and reverse, disabled by default)
    --version, display version
    --verbose -v, verbose logs

  <auth> must be set to one of:
    1. a username and password string separated by a colon ("myuser:mypass")
    2. a path to an ssh authorized keys file ("~/.ssh/authorized_keys")
    3. an authorized github user ("github.com/myuser") public keys from .keys
    4. "none" to disable client authentication :WARNING: very insecure

  Notes:
    * if no keyfile and no keyseed are set, a random RSA2048 key is used
    * authorized_key files are automatically reloaded on change
    * once authenticated, clients will have access to a shell of the
    current user. sshd-lite does not lookup system users.
    * sshd-lite only supports remotes shells, sftp, and tcp forwarding. command
    execution are not currently supported.
    * sftp working directory is the home directory of the user

  Read more: https://github.com/jpillora/sshd-lite

```
<!--/tmpl-->

### Programmatic Usage

[![GoDoc](https://godoc.org/github.com/jpillora/sshd-lite/server?status.svg)](https://godoc.org/github.com/jpillora/sshd-lite/server)

#### MIT License

Copyright © 2020 Jaime Pillora &lt;dev@jpillora.com&gt;

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