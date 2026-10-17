//go:build !unix

package client

import (
	"errors"
	"os"
)

func openSource(name string) (*os.File, error) {
	info, err := os.Stat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("ssh client: nonregular source")
	}
	return os.Open(name)
}
