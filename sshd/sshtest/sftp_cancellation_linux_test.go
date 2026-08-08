//go:build linux

package sshtest_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jpillora/sshd-lite/sshd/sshtest"
	"github.com/jpillora/sshd-lite/sshd/sshtest/scenario"
	"golang.org/x/sys/unix"
)

func TestSFTPUploadCancellationJoinsControlledTransfer(t *testing.T) {
	workDir := t.TempDir()
	localDir := t.TempDir()
	fifo := filepath.Join(localDir, "upload.fifo")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Fatalf("create upload FIFO: %v", err)
	}
	keeper, err := os.OpenFile(fifo, os.O_RDWR|unix.O_NONBLOCK, 0)
	if err != nil {
		t.Fatalf("open upload FIFO keeper: %v", err)
	}
	closeKeeper := sync.OnceFunc(func() { _ = keeper.Close() })
	t.Cleanup(closeKeeper)
	env := newSFTPCancellationEnvironment(t, workDir)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	writerRelease := make(chan struct{})
	releaseWriter := sync.OnceFunc(func() { close(writerRelease) })
	t.Cleanup(releaseWriter)
	writerFinished := make(chan struct{})
	var writerErr error
	actionFinished := make(chan struct{})
	var actionErr error
	t.Cleanup(func() {
		cancel()
		releaseWriter()
		closeKeeper()
		select {
		case <-writerFinished:
		case <-time.After(5 * time.Second):
			t.Errorf("upload FIFO writer did not stop during cleanup")
		}
		select {
		case <-actionFinished:
		case <-time.After(5 * time.Second):
			t.Errorf("upload action did not stop during cleanup")
		}
	})
	payload := make([]byte, 1<<20)
	const firstChunk = 64 << 10
	for i := range payload {
		payload[i] = byte(i)
	}
	go func() {
		file, err := os.OpenFile(fifo, os.O_WRONLY, 0)
		if err == nil {
			_, err = file.Write(payload[:firstChunk])
		}
		if err == nil {
			<-writerRelease
		}
		if err == nil {
			_, err = file.Write(payload[firstChunk:])
		}
		if file != nil {
			err = errors.Join(err, file.Close())
		}
		writerErr = err
		close(writerFinished)
	}()
	go func() {
		actionErr = sshtest.SFTPUpload(fifo, "partial-upload.bin").Execute(ctx, env, "test")
		close(actionFinished)
	}()
	waitForSFTPStart(t, env)
	remote := filepath.Join(workDir, "partial-upload.bin")
	waitForFileSize(t, remote, 1)
	info, err := os.Stat(remote)
	if err != nil {
		t.Fatalf("stat partial remote upload: %v", err)
	}
	if info.Size() <= 0 || info.Size() >= int64(len(payload)) {
		t.Fatalf("remote was not genuinely partial before cancellation: size=%d", info.Size())
	}
	cancel()
	releaseWriter()
	closeKeeper()
	select {
	case <-writerFinished:
	case <-time.After(3 * time.Second):
		t.Fatal("upload FIFO writer did not join")
	}
	if writerErr != nil && !errors.Is(writerErr, unix.EPIPE) {
		t.Fatalf("upload FIFO writer: %v", writerErr)
	}
	assertCanceledActionReturns(t, actionFinished, &actionErr)
	got, err := os.ReadFile(remote)
	if err != nil {
		t.Fatalf("read partial remote upload: %v", err)
	}
	if len(got) == 0 || len(got) >= len(payload) {
		t.Fatalf("canceled upload size = %d, want a non-empty proper prefix of %d bytes", len(got), len(payload))
	}
}

func newSFTPCancellationEnvironment(t *testing.T, workDir string) *sshtest.Environment {
	t.Helper()
	env := sshtest.New(t).
		WithServer(sshtest.ServerWithSFTP(true), sshtest.ServerWithWorkDir(workDir)).
		WithClient("test", sshtest.ClientWithKeySeed("test")).
		Start()
	t.Cleanup(env.Stop)
	if err := env.Client("test").Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	return env
}

func waitForSFTPStart(t *testing.T, env *sshtest.Environment) {
	t.Helper()
	if _, err := env.Events().WaitTimeout(5*time.Second, scenario.EventSFTPStarted); err != nil {
		t.Fatalf("wait for SFTP start: %v", err)
	}
}

func waitForFileSize(t *testing.T, path string, minimum int64) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if info, err := os.Stat(path); err == nil && info.Size() >= minimum {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("file %s did not reach %d bytes", path, minimum)
}

func assertCanceledActionReturns(t *testing.T, done <-chan struct{}, actionErr *error) {
	t.Helper()
	select {
	case <-done:
		if !errors.Is(*actionErr, context.Canceled) {
			t.Fatalf("canceled SFTP action error = %v", *actionErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("canceled SFTP action did not join its operation goroutine")
	}
}
