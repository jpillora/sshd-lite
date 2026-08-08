package sshtest

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jpillora/sshd-lite/xssh"
	"github.com/pkg/sftp"
)

func TestSFTPDownloadCancellationOutcomeIsAtomic(t *testing.T) {
	localDir := t.TempDir()
	destination := filepath.Join(localDir, "destination.bin")
	preserved := []byte("preserve existing destination")
	if err := os.WriteFile(destination, preserved, 0o600); err != nil {
		t.Fatalf("write existing destination: %v", err)
	}

	env, reader, releaseReader := newControlledDownloadEnvironment(t)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	actionFinished := make(chan struct{})
	var actionErr error
	go func() {
		actionErr = SFTPDownload("remote.bin", destination).Execute(ctx, env, "test")
		close(actionFinished)
	}()
	t.Cleanup(func() {
		cancel()
		releaseReader()
		select {
		case <-actionFinished:
		case <-time.After(5 * time.Second):
			t.Errorf("download action did not stop during cleanup")
		}
	})
	waitForControlledDownload(t, reader, actionFinished, &actionErr)
	waitForPartialDownloads(t, localDir)

	cancel()
	releaseReader()
	select {
	case <-actionFinished:
	case <-time.After(3 * time.Second):
		t.Fatal("canceled download did not join its operation goroutine")
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatalf("read destination: %v", err)
	}
	if actionErr == nil {
		if !bytes.Equal(got, reader.data) {
			t.Fatalf("finalization won but destination was not completed: got %d bytes", len(got))
		}
	} else {
		if !errors.Is(actionErr, context.Canceled) {
			t.Fatalf("racing cancellation error = %v", actionErr)
		}
		if !bytes.Equal(got, preserved) {
			t.Fatalf("cancellation won but destination changed: %q", got)
		}
	}
	partials, err := filepath.Glob(filepath.Join(localDir, ".destination.bin.part-*"))
	if err != nil || len(partials) != 0 {
		t.Fatalf("partial downloads remain: %v, err = %v", partials, err)
	}
}

func TestSFTPDownloadCloseWinsCommit(t *testing.T) {
	localDir := t.TempDir()
	destination := filepath.Join(localDir, "destination.bin")
	preserved := []byte("preserve existing destination")
	if err := os.WriteFile(destination, preserved, 0o600); err != nil {
		t.Fatalf("write existing destination: %v", err)
	}
	env, reader, releaseReader := newControlledDownloadEnvironment(t)
	client, err := env.Client("test").SFTP()
	if err != nil {
		t.Fatalf("start SFTP session: %v", err)
	}
	done := make(chan struct{})
	var downloadErr error
	go func() {
		downloadErr = client.Download("remote.bin", destination)
		close(done)
	}()
	waitForControlledDownload(t, reader, done, &downloadErr)
	waitForPartialDownloads(t, localDir)
	closeDone := make(chan error, 1)
	go func() { closeDone <- client.Close() }()
	waitForSFTPClientClosed(t, client)
	releaseReader()
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("close SFTP client: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("SFTP Close did not return")
	}
	select {
	case <-done:
		if downloadErr == nil {
			t.Fatal("download succeeded after Close won")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("download did not stop after Close")
	}
	got, err := os.ReadFile(destination)
	if err != nil || !bytes.Equal(got, preserved) {
		t.Fatalf("destination after Close = %q, err = %v", got, err)
	}
	assertNoPartialDownloads(t, localDir)
}

func TestSFTPDownloadFinalizeWinsClose(t *testing.T) {
	localDir := t.TempDir()
	destination := filepath.Join(localDir, "destination.bin")
	if err := os.WriteFile(destination, []byte("old"), 0o600); err != nil {
		t.Fatalf("write existing destination: %v", err)
	}
	env, reader, releaseReader := newControlledDownloadEnvironment(t)
	client, err := env.Client("test").SFTP()
	if err != nil {
		t.Fatalf("start SFTP session: %v", err)
	}
	done := make(chan struct{})
	var downloadErr error
	go func() {
		downloadErr = client.Download("remote.bin", destination)
		close(done)
	}()
	waitForControlledDownload(t, reader, done, &downloadErr)
	releaseReader()
	select {
	case <-done:
		if downloadErr != nil {
			t.Fatalf("completed download: %v", downloadErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("completed download did not return")
	}
	if err := client.Close(); err != nil {
		t.Fatalf("close completed SFTP client: %v", err)
	}
	got, err := os.ReadFile(destination)
	if err != nil || !bytes.Equal(got, reader.data) {
		t.Fatalf("completed destination has %d bytes, err = %v", len(got), err)
	}
	assertNoPartialDownloads(t, localDir)
}

func newControlledDownloadEnvironment(t *testing.T) (*Environment, *controlledSFTPReaderAt, func()) {
	t.Helper()
	reader := &controlledSFTPReaderAt{
		data:          controlledDownloadData(),
		partialSent:   make(chan struct{}),
		secondStarted: make(chan struct{}),
		release:       make(chan struct{}),
	}
	releaseReader := sync.OnceFunc(func() { close(reader.release) })
	t.Cleanup(releaseReader)
	serverStarted := make(chan struct{})
	serverDone := make(chan error, 1)
	var startedOnce sync.Once
	env := New(t).
		WithServer(func(cfg *serverConfig) {
			handlers := sftp.InMemHandler()
			handlers.FileGet = reader
			handlers.FileList = reader
			cfg.SubsystemHandlers = map[string]xssh.SubsystemHandler{
				"sftp": func(sess *xssh.Session, req *xssh.Request) (retErr error) {
					startedOnce.Do(func() { close(serverStarted) })
					defer func() { serverDone <- retErr }()
					if req.WantReply {
						if err := req.Reply(true, nil); err != nil {
							return err
						}
					}
					server := sftp.NewRequestServer(sess.Channel, handlers)
					defer server.Close()
					return server.Serve()
				},
			}
		}).
		WithClient("test", ClientWithKeySeed("test")).
		Start()
	t.Cleanup(func() {
		// Close transports before waiting for the custom Serve loop. If the
		// subsystem never started, there is no server goroutine to join.
		var clientCloseDone chan struct{}
		if client := env.Client("test"); client != nil {
			clientCloseDone = make(chan struct{})
			go func() {
				_ = client.Close()
				close(clientCloseDone)
			}()
		}
		releaseReader()
		if clientCloseDone != nil {
			select {
			case <-clientCloseDone:
			case <-time.After(3 * time.Second):
				t.Errorf("controlled SSH client did not close")
			}
		}
		env.Stop()
		select {
		case <-serverStarted:
			select {
			case <-serverDone:
			case <-time.After(3 * time.Second):
				t.Errorf("controlled SFTP server did not stop")
			}
		default:
		}
	})
	if err := env.Client("test").Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	return env, reader, releaseReader
}

func waitForControlledDownload(t *testing.T, reader *controlledSFTPReaderAt, done <-chan struct{}, operationErr *error) {
	t.Helper()
	select {
	case <-reader.partialSent:
	case <-done:
		t.Fatalf("download ended before controlled partial response: %v", *operationErr)
	case <-time.After(5 * time.Second):
		t.Fatal("controlled SFTP reader never sent its partial response")
	}
	select {
	case <-reader.secondStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("controlled SFTP reader never blocked the follow-up request")
	}
}

func assertNoPartialDownloads(t *testing.T, localDir string) {
	t.Helper()
	partials, err := filepath.Glob(filepath.Join(localDir, ".destination.bin.part-*"))
	if err != nil || len(partials) != 0 {
		t.Fatalf("partial downloads remain: %v, err = %v", partials, err)
	}
}

func waitForSFTPClientClosed(t *testing.T, client *SFTPClient) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		client.lifecycleMu.Lock()
		closed := client.closed
		client.lifecycleMu.Unlock()
		if closed {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("SFTP Close did not win the lifecycle lock")
}

type controlledSFTPReaderAt struct {
	data          []byte
	partialSent   chan struct{}
	secondStarted chan struct{}
	release       chan struct{}
	partialOnce   sync.Once
	secondOnce    sync.Once
}

func (r *controlledSFTPReaderAt) Fileread(*sftp.Request) (io.ReaderAt, error) {
	return r, nil
}

func (r *controlledSFTPReaderAt) Filelist(*sftp.Request) (sftp.ListerAt, error) {
	return controlledSFTPLister{info: controlledSFTPFileInfo{size: int64(len(r.data))}}, nil
}

func (r *controlledSFTPReaderAt) ReadAt(p []byte, offset int64) (int, error) {
	if offset == 0 {
		n := copy(p, r.data)
		r.partialOnce.Do(func() { close(r.partialSent) })
		return n, nil
	}
	r.secondOnce.Do(func() { close(r.secondStarted) })
	<-r.release
	if offset >= int64(len(r.data)) {
		return 0, io.EOF
	}
	n := copy(p, r.data[offset:])
	if int(offset)+n == len(r.data) {
		return n, io.EOF
	}
	return n, nil
}

func controlledDownloadData() []byte {
	data := make([]byte, 40<<10)
	for i := range data {
		data[i] = byte(i)
	}
	return data
}

type controlledSFTPLister struct{ info os.FileInfo }

func (l controlledSFTPLister) ListAt(items []os.FileInfo, offset int64) (int, error) {
	if offset > 0 || len(items) == 0 {
		return 0, io.EOF
	}
	items[0] = l.info
	return 1, io.EOF
}

type controlledSFTPFileInfo struct{ size int64 }

func (i controlledSFTPFileInfo) Name() string       { return "remote.bin" }
func (i controlledSFTPFileInfo) Size() int64        { return i.size }
func (i controlledSFTPFileInfo) Mode() os.FileMode  { return 0o600 }
func (i controlledSFTPFileInfo) ModTime() time.Time { return time.Time{} }
func (i controlledSFTPFileInfo) IsDir() bool        { return false }
func (i controlledSFTPFileInfo) Sys() interface{}   { return nil }

func waitForPartialDownloads(t *testing.T, localDir string) []string {
	t.Helper()
	pattern := filepath.Join(localDir, ".destination.bin.part-*")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		partials, err := filepath.Glob(pattern)
		if err == nil && len(partials) == 1 {
			if info, statErr := os.Stat(partials[0]); statErr == nil && info.Size() > 0 {
				return partials
			}
		}
		time.Sleep(time.Millisecond)
	}
	entries, _ := os.ReadDir(localDir)
	for _, entry := range entries {
		if info, err := entry.Info(); err == nil {
			t.Logf("download directory entry before timeout: %s size=%d", entry.Name(), info.Size())
		}
	}
	t.Fatalf("partial download did not appear for %s", pattern)
	return nil
}
