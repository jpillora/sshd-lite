//go:build windows

package xssh

import "testing"

func TestWindowsPTYDimensionAndLifecycleBoundaries(t *testing.T) {
	if _, err := winsizeFromDimensions(32767, 32767); err != nil {
		t.Fatalf("signed int16 maximum rejected: %v", err)
	}
	if _, err := winsizeFromDimensions(32768, 24); err == nil {
		t.Fatal("dimension 32768 would wrap the backend's signed int16 COORD")
	}
	if err := SetWinsize(fakeFdHolder(0), 80, 24); err == nil {
		t.Fatal("SetWinsize allowed an uncoordinated running ConPTY resize")
	}
}
