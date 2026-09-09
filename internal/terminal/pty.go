// Package terminal owns platform PTYs and their child processes. It knows
// nothing about SSH channels, Mosh packets, or terminal screen emulation.
package terminal

import (
	"fmt"
	"io"
	"os/exec"
)

type FdHolder interface{ Fd() uintptr }
type PTY interface {
	io.ReadWriteCloser
	FdHolder
}
type Size struct{ Rows, Cols uint16 }

var startPTY func(*exec.Cmd, *Size) (PTY, error)
var setSize func(FdHolder, *Size) error

func SetSize(t FdHolder, size *Size) error { return setSize(t, size) }

const MaxDimension = uint32(1<<15 - 1)

func Dimensions(cols, rows uint32) (*Size, error) {
	if cols > MaxDimension || rows > MaxDimension {
		return nil, fmt.Errorf("terminal dimensions out of range: %dx%d (maximum %dx%d)", cols, rows, MaxDimension, MaxDimension)
	}
	if cols == 0 || rows == 0 {
		return nil, nil
	}
	return &Size{Cols: uint16(cols), Rows: uint16(rows)}, nil
}
