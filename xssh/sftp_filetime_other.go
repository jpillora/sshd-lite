//go:build !linux && !darwin && !openbsd && !windows

package xssh

import (
	"errors"
	"os"
	"time"
)

func setFileTimes(_ *os.Root, _ *os.File, _ string, _, _ time.Time) error {
	return errors.ErrUnsupported
}
