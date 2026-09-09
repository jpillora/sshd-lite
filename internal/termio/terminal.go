package termio

import (
	"os"

	"golang.org/x/term"
)

func Size(f *os.File) (int, int) {
	cols, rows, err := term.GetSize(int(f.Fd()))
	if err != nil || cols < 1 || rows < 1 {
		return 80, 24
	}
	if cols > 1000 {
		cols = 1000
	}
	if rows > 100 {
		rows = 100
	}
	return cols, rows
}

func Raw(f *os.File) (func(), error) {
	state, err := term.MakeRaw(int(f.Fd()))
	if err != nil {
		return nil, err
	}
	return func() { _ = term.Restore(int(f.Fd()), state) }, nil
}
