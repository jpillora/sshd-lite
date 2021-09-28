# 
# WARNING
# THIS MAKEFILE CAN ONLY BE USED TO GENERATE A VERSION OF SSHD-LITE
# THAT IS BASED ON A PENDING PULL REQUEST OF THE PTY PROJECT
#
BUILD_VARS=-s -w
TRIM_FLAGS=-gcflags "all=-trimpath=${PWD}" -asmflags "all=-trimpath=${PWD}"

pty/go.mod:
	git clone https://github.com/jeffreystoke/pty.git

all: linux64 linuxarm64 darwin64 darwinarm64 win64

linux: linux64

linux64: pty/go.mod
	GOOS=linux GOARCH=amd64 go build ${TRIM_FLAGS} -ldflags "${BUILD_VARS}" -o bin/sshd-lite && cd bin && sha256sum sshd-lite > sshd-lite.sha256

linuxarm64: pty/go.mod
	GOOS=linux GOARCH=arm64 go build ${TRIM_FLAGS} -ldflags "${BUILD_VARS}" -o bin/sshd-lite-arm64  && cd bin && sha256sum sshd-lite-arm64 > sshd-lite-arm64.sha256

darwin64: pty/go.mod
	GOOS=darwin GOARCH=amd64 go build ${TRIM_FLAGS} -ldflags "${BUILD_VARS}" -o bin/sshd-lite-darwin && cd bin && sha256sum sshd-lite-darwin > sshd-lite-darwin.sha256

darwinarm64: pty/go.mod
	GOOS=darwin GOARCH=arm64 go build ${TRIM_FLAGS} -ldflags "${BUILD_VARS}" -o bin/sshd-lite-darwin-arm64 && cd bin && sha256sum sshd-lite-darwin-arm64 > sshd-lite-darwin-arm64.sha256

win64: pty/go.mod
	GOOS=windows GOARCH=amd64 go build ${TRIM_FLAGS} -ldflags "${BUILD_VARS}" -o bin/sshd-lite.exe && cd bin && sha256sum sshd-lite.exe > sshd-lite-win64.sha256

verify:
	cd bin && sha256sum *.sha256 -c && cd ../;
