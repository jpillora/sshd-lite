A simple golang SSH honeypot that records recevied username/password pairs.

Essentially created by stripping off parts of https://github.com/jpillora/sshd-lite

(bugs are my fault, not theirs)

I am very new to Go, so usage is very much at your own risk.

I am building on x64 Linux, I have no idea if it will work on other platforms.

Inspired by https://github.com/regit/pshitt but I want to play with Go. 

to build:

go build

create RSA keyfile (to avoid regenerating each time during testing) with

rsa_keygen -t

to run:

./gosshpot -p 2022 -v --keyfile id_rsa a:b 2> password.list
