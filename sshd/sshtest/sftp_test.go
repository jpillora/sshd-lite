package sshtest_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jpillora/sshd-lite/sshd/sshtest"
	"github.com/jpillora/sshd-lite/sshd/sshtest/scenario"
)

func TestSFTPTransfersAndSessionCleanup(t *testing.T) {
	workDir := t.TempDir()
	localDir := t.TempDir()
	env := sshtest.New(t).
		WithServer(
			sshtest.ServerWithSFTP(true),
			sshtest.ServerWithWorkDir(workDir),
		).
		WithClient("test", sshtest.ClientWithKeySeed("test"))
	env.Start()
	t.Cleanup(env.Stop)
	client := env.Client("test")
	if err := client.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}

	longContent := []byte("a longer payload that must be truncated")
	shortContent := []byte{0, 1, 2, 's', 'h', 'o', 'r', 't', 0xff}
	localUpload := filepath.Join(localDir, "upload.bin")
	if err := os.WriteFile(localUpload, longContent, 0o600); err != nil {
		t.Fatalf("write upload fixture: %v", err)
	}

	sftpClient, err := client.SFTP()
	if err != nil {
		t.Fatalf("start first SFTP session: %v", err)
	}
	if err := sftpClient.Upload(localUpload, "remote.bin"); err != nil {
		t.Fatalf("first upload: %v", err)
	}
	if err := os.WriteFile(localUpload, shortContent, 0o600); err != nil {
		t.Fatalf("replace upload fixture: %v", err)
	}
	if err := sftpClient.Upload(localUpload, "remote.bin"); err != nil {
		t.Fatalf("overwrite upload: %v", err)
	}
	if err := sftpClient.Close(); err != nil {
		t.Fatalf("close first SFTP session: %v", err)
	}
	if err := sftpClient.Close(); err != nil {
		t.Fatalf("idempotent SFTP close: %v", err)
	}
	assertFileBytes(t, filepath.Join(workDir, "remote.bin"), shortContent)

	download := filepath.Join(localDir, "download.bin")
	if err := os.WriteFile(download, []byte("old destination"), 0o600); err != nil {
		t.Fatalf("write old download: %v", err)
	}
	for i := 0; i < 3; i++ {
		sftpClient, err := client.SFTP()
		if err != nil {
			t.Fatalf("start SFTP session %d: %v", i+2, err)
		}
		if err := sftpClient.Download("remote.bin", download); err != nil {
			t.Fatalf("download in session %d: %v", i+2, err)
		}
		if err := sftpClient.Close(); err != nil {
			t.Fatalf("close SFTP session %d: %v", i+2, err)
		}
		assertFileBytes(t, download, shortContent)
	}

	missingDestination := filepath.Join(localDir, "missing.bin")
	if err := os.WriteFile(missingDestination, []byte("preserve me"), 0o600); err != nil {
		t.Fatalf("write protected destination: %v", err)
	}
	sftpClient, err = client.SFTP()
	if err != nil {
		t.Fatalf("start error-path SFTP session: %v", err)
	}
	if err := sftpClient.Download("does-not-exist", missingDestination); err == nil || !strings.Contains(err.Error(), "does-not-exist") {
		t.Fatalf("missing remote error = %v", err)
	}
	assertFileBytes(t, missingDestination, []byte("preserve me"))
	if err := sftpClient.Upload(localUpload, "missing/parent/remote.bin"); err == nil || !strings.Contains(err.Error(), "missing/parent/remote.bin") {
		t.Fatalf("nested remote error = %v", err)
	}
	if err := sftpClient.Close(); err != nil {
		t.Fatalf("close error-path SFTP session: %v", err)
	}
}

func TestSFTPActionsAndYAML(t *testing.T) {
	workDir := t.TempDir()
	localDir := t.TempDir()
	upload := filepath.Join(localDir, "action-upload.txt")
	download := filepath.Join(localDir, "action-download.txt")
	want := []byte("scenario SFTP content\x00with binary")
	if err := os.WriteFile(upload, want, 0o600); err != nil {
		t.Fatalf("write upload fixture: %v", err)
	}

	env := sshtest.New(t).
		WithServer(sshtest.ServerWithSFTP(true), sshtest.ServerWithWorkDir(workDir)).
		WithClient("test", sshtest.ClientWithKeySeed("test")).
		Start()
	t.Cleanup(env.Stop)
	yaml := "name: SFTP YAML\nsteps:\n  - client: test\n    actions:\n" +
		"      - connect\n" +
		"      - sftp_upload:\n" +
		"          local: " + yamlQuote(upload) + "\n" +
		"          remote: action-remote.txt\n" +
		"      - sftp_download:\n" +
		"          remote: action-remote.txt\n" +
		"          local: " + yamlQuote(download) + "\n"
	if err := env.RunYAML(yaml); err != nil {
		t.Fatalf("run SFTP YAML: %v", err)
	}
	assertFileBytes(t, filepath.Join(workDir, "action-remote.txt"), want)
	assertFileBytes(t, download, want)
}

func TestSFTPDisabledAndActionValidation(t *testing.T) {
	env := sshtest.New(t).
		WithServer(sshtest.ServerWithSFTP(false)).
		WithClient("test", sshtest.ClientWithKeySeed("test")).
		Start()
	t.Cleanup(env.Stop)
	client := env.Client("test")
	if err := client.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := client.SFTP(); err == nil || !strings.Contains(err.Error(), "subsystem") {
		t.Fatalf("disabled SFTP error = %v", err)
	}

	action := sshtest.SFTPUpload("local", "remote")
	if err := action.Execute(context.Background(), struct{}{}, "test"); err == nil || !strings.Contains(err.Error(), "invalid test environment") {
		t.Fatalf("invalid environment error = %v", err)
	}
	if err := action.Execute(context.Background(), env, "missing"); err == nil || !strings.Contains(err.Error(), `client "missing" not found`) {
		t.Fatalf("missing client error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := action.Execute(canceled, env, "test"); err == nil || !strings.Contains(err.Error(), "canceled before session start") {
		t.Fatalf("pre-canceled action error = %v", err)
	}
}

func TestSFTPActionSpecs(t *testing.T) {
	upload := scenario.SFTPUploadSpec("local-a", "remote-a")
	if upload.Type != scenario.ActionSFTPUpload || upload.LocalPath() != "local-a" || upload.RemotePath() != "remote-a" {
		t.Fatalf("upload spec = %#v", upload)
	}
	if upload.String() != "sftp_upload" {
		t.Fatalf("upload spec String = %q", upload.String())
	}
	download := scenario.SFTPDownloadSpec("remote-b", "local-b")
	if download.Type != scenario.ActionSFTPDownload || download.LocalPath() != "local-b" || download.RemotePath() != "remote-b" {
		t.Fatalf("download spec = %#v", download)
	}
	if download.String() != "sftp_download" {
		t.Fatalf("download spec String = %q", download.String())
	}
	if got := sshtest.SFTPUpload("local-a", "remote-a").String(); got != `SFTPUpload("local-a", "remote-a")` {
		t.Fatalf("upload String = %q", got)
	}
	if got := sshtest.SFTPDownload("remote-b", "local-b").String(); got != `SFTPDownload("remote-b", "local-b")` {
		t.Fatalf("download String = %q", got)
	}
}

func assertFileBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != string(want) {
		t.Fatalf("content of %s = %q, want %q", path, got, want)
	}
}

func yamlQuote(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}
