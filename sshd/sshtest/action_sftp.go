package sshtest

import (
	"context"
	"errors"
	"fmt"
)

// sftpUploadAction uploads a file via SFTP.
type sftpUploadAction struct {
	localPath  string
	remotePath string
}

// SFTPUpload returns an action that uploads a file via SFTP.
func SFTPUpload(localPath, remotePath string) Action {
	return actionAdapter{&sftpUploadAction{localPath: localPath, remotePath: remotePath}}
}

func (a *sftpUploadAction) execute(ctx context.Context, env *Environment, clientName string) error {
	return executeSFTPOperation(ctx, env, clientName, "upload", func(client *SFTPClient) error {
		if err := client.Upload(a.localPath, a.remotePath); err != nil {
			return fmt.Errorf("upload local path %q to remote path %q: %w", a.localPath, a.remotePath, err)
		}
		return nil
	})
}

func (a *sftpUploadAction) String() string {
	return fmt.Sprintf("SFTPUpload(%q, %q)", a.localPath, a.remotePath)
}

// sftpDownloadAction downloads a file via SFTP.
type sftpDownloadAction struct {
	remotePath string
	localPath  string
}

// SFTPDownload returns an action that downloads a file via SFTP.
func SFTPDownload(remotePath, localPath string) Action {
	return actionAdapter{&sftpDownloadAction{remotePath: remotePath, localPath: localPath}}
}

func (a *sftpDownloadAction) execute(ctx context.Context, env *Environment, clientName string) error {
	return executeSFTPOperation(ctx, env, clientName, "download", func(client *SFTPClient) error {
		if err := client.Download(a.remotePath, a.localPath); err != nil {
			return fmt.Errorf("download remote path %q to local path %q: %w", a.remotePath, a.localPath, err)
		}
		return nil
	})
}

func (a *sftpDownloadAction) String() string {
	return fmt.Sprintf("SFTPDownload(%q, %q)", a.remotePath, a.localPath)
}

func executeSFTPOperation(ctx context.Context, e *Environment, clientName, operation string, fn func(*SFTPClient) error) error {
	client := e.clientByName(clientName)
	if client == nil {
		return fmt.Errorf("client %q not found", clientName)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("SFTP %s canceled before session start: %w", operation, err)
	}

	sftpClient, err := client.SFTP()
	if err != nil {
		return fmt.Errorf("start SFTP session for %s: %w", operation, err)
	}
	done := make(chan error, 1)
	go func() { done <- fn(sftpClient) }()

	select {
	case err := <-done:
		if closeErr := sftpClient.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close SFTP session after %s: %w", operation, closeErr))
		}
		return err
	case <-ctx.Done():
	}

	// pkg/sftp Close interrupts outstanding requests. Close synchronously, then
	// join the one operation goroutine: the action never abandons goroutines it
	// owns. Real controlled-transfer tests enforce the practical time bound.
	closeErr := sftpClient.Close()
	operationErr := <-done
	// The operation may have committed its result immediately before Close won
	// the lifecycle lock. In that case completion, not cancellation, is the
	// observable outcome even if this select happened to receive ctx.Done.
	if operationErr == nil {
		if closeErr != nil {
			return fmt.Errorf("close completed SFTP %s session: %w", operation, closeErr)
		}
		return nil
	}
	if operationErr != nil {
		operationErr = fmt.Errorf("SFTP %s stopped after cancellation: %w", operation, operationErr)
	}
	if closeErr != nil {
		closeErr = fmt.Errorf("close canceled SFTP %s session: %w", operation, closeErr)
	}
	return errors.Join(fmt.Errorf("SFTP %s canceled: %w", operation, ctx.Err()), operationErr, closeErr)
}
