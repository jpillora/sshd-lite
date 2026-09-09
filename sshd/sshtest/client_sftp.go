package sshtest

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/jpillora/sshd-lite/sshd/sshtest/scenario"
	"github.com/pkg/sftp"
)

// SFTPClient owns one SFTP subsystem session. Upload and Download may be used
// concurrently (the underlying pkg/sftp client supports concurrent requests).
// Callers ordinarily close it after operations finish. Close atomically prevents
// a pending Download from replacing its destination, then closes the underlying
// session to interrupt in-flight requests. A Download already committing its
// rename wins that race and completes normally. Upload cancellation can leave a
// partial remote file. Close is idempotent and returns the first close result.
type SFTPClient struct {
	client      *sftp.Client
	lifecycleMu sync.Mutex
	closed      bool
	closeOnce   sync.Once
	closeErr    error
}

// Upload copies localPath to remotePath, creating or truncating the remote file.
func (c *SFTPClient) Upload(localPath, remotePath string) (retErr error) {
	if err := c.ensureOpen(); err != nil {
		return fmt.Errorf("start upload: %w", err)
	}
	local, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open local upload file %q: %w", localPath, err)
	}
	defer func() {
		if err := local.Close(); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("close local upload file %q: %w", localPath, err))
		}
	}()

	remote, err := c.client.OpenFile(remotePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return fmt.Errorf("open remote upload file %q: %w", remotePath, err)
	}
	defer func() {
		if err := remote.Close(); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("close remote upload file %q: %w", remotePath, err))
		}
	}()

	if _, err := io.Copy(remote, local); err != nil {
		return fmt.Errorf("copy local file %q to remote file %q: %w", localPath, remotePath, err)
	}
	return nil
}

// Download copies remotePath to localPath. Data is first written to a temporary
// sibling and atomically renamed where supported, so a failed transfer is never
// exposed at localPath as a successful complete download.
func (c *SFTPClient) Download(remotePath, localPath string) (retErr error) {
	if err := c.ensureOpen(); err != nil {
		return fmt.Errorf("start download: %w", err)
	}
	remote, err := c.client.Open(remotePath)
	if err != nil {
		return fmt.Errorf("open remote download file %q: %w", remotePath, err)
	}
	remoteClosed := false
	defer func() {
		if !remoteClosed {
			if err := remote.Close(); err != nil {
				retErr = errors.Join(retErr, fmt.Errorf("close remote download file %q: %w", remotePath, err))
			}
		}
	}()

	dir := filepath.Dir(localPath)
	temp, err := os.CreateTemp(dir, "."+filepath.Base(localPath)+".part-*")
	if err != nil {
		return fmt.Errorf("create temporary local download file for %q: %w", localPath, err)
	}
	tempPath := temp.Name()
	keepTemp := false
	tempClosed := false
	defer func() {
		if !tempClosed {
			if err := temp.Close(); err != nil {
				retErr = errors.Join(retErr, fmt.Errorf("close temporary local download file %q: %w", tempPath, err))
			}
		}
		if !keepTemp {
			if err := os.Remove(tempPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				retErr = errors.Join(retErr, fmt.Errorf("remove partial local download file %q: %w", tempPath, err))
			}
		}
	}()

	if _, err := io.Copy(temp, remote); err != nil {
		return fmt.Errorf("copy remote file %q to local file %q: %w", remotePath, localPath, err)
	}
	err = temp.Close()
	tempClosed = true
	if err != nil {
		return fmt.Errorf("close completed local download file %q: %w", tempPath, err)
	}
	err = remote.Close()
	remoteClosed = true
	if err != nil {
		return fmt.Errorf("close completed remote download file %q: %w", remotePath, err)
	}
	// Close and the destination commit have one explicit winner. Do not hold
	// lifecycleMu while doing protocol I/O: pkg/sftp Close may need those
	// requests to unwind.
	c.lifecycleMu.Lock()
	if c.closed {
		c.lifecycleMu.Unlock()
		return fmt.Errorf("commit local download file %q: SFTP session closed", localPath)
	}
	err = replaceFile(tempPath, localPath)
	if err == nil {
		keepTemp = true
	}
	c.lifecycleMu.Unlock()
	if err != nil {
		return fmt.Errorf("replace local download file %q: %w", localPath, err)
	}
	return nil
}

func (c *SFTPClient) ensureOpen() error {
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	if c.closed {
		return errors.New("SFTP session closed")
	}
	return nil
}

func replaceFile(source, destination string) error {
	return os.Rename(source, destination)
}

// Close prevents future download commits and closes this SFTP subsystem
// session. It is safe to call repeatedly. Close does not hold the lifecycle
// mutex while pkg/sftp shuts down, so interrupted requests can unwind.
func (c *SFTPClient) Close() error {
	c.closeOnce.Do(func() {
		c.lifecycleMu.Lock()
		c.closed = true
		c.lifecycleMu.Unlock()
		c.closeErr = c.client.Close()
	})
	return c.closeErr
}

// SFTP returns an SFTP client.
func (c *clientGo) SFTP() (*SFTPClient, error) {
	c.mu.Lock()
	if !c.connected {
		c.mu.Unlock()
		return nil, fmt.Errorf("not connected")
	}
	client := c.sshClient
	c.mu.Unlock()

	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return nil, fmt.Errorf("start SFTP subsystem: %w", err)
	}
	c.events.Emit(scenario.EventSFTPStarted, "client", c.config.name)
	return &SFTPClient{client: sftpClient}, nil
}
