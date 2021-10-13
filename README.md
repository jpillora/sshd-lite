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
* Authentication (`user:pass` and `authorized_keys`)
* Seed server-key generation

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

<!--tmpl,code=plain:echo "$ sshd-lite --help" && go run main.go --help | sed 's#0.0.0-src#X.Y.Z#' -->
``` plain 
$ sshd-lite --help
exit status 1

  Usage: sshd-lite [options] <auth>

  Version: X.Y.Z

  Options:
    --host, listening interface (defaults to all)
    --port -p, listening port (defaults to 22, then fallsback to 2200)
    --shell, the type of to use shell for remote sessions (defaults to $SHELL, then bash/powershell)
    --keyfile, a filepath to an private key (for example, an 'id_rsa' file)
    --keyseed, a string to use to seed key generation
    --noenv, ignore environment variables provided by the client
    --version, display version
    -v, verbose logs

  <auth> must be set to one of:
    1. a username and password string separated by a colon ("user:pass")
    2. a path to an ssh authorized keys file ("~/.ssh/authorized_keys")
    3. "none" to disable client authentication :WARNING: very insecure

  Notes:
    * if no keyfile and no keyseed are set, a random RSA2048 key is used
    * once authenticated, clients will login to a shell as the
    sshd-lite user. sshd-lite does not lookup system users.
    * sshd-lite only supports remotes shells. tunnelling and command
    execution are not currently supported.

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