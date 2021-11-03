A simple golang SSH honeypot that records recevied username/password pairs.

Essentially created by stripping off parts of https://github.com/jpillora/sshd-lite

(bugs are my fault, not theirs)

I am very new to Go, so usage is very much at your own risk.

Built on x64 Linux. Won't work on Windows because golang stdlib syslog doesn't support Windows. 

Inspired by https://github.com/regit/pshitt but I want to play with Go. 

to build:

    go build

create RSA keyfile (to avoid regenerating each time during testing) with

    ssh-keygen -t rsa

to run:

    ./gosshpot -p 2022 -v --keyfile id_rsa 


