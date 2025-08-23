package main

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/jpillora/sshd-lite/client"
	"github.com/jpillora/sshd-lite/server"
)

func runAttachCommand(sessionName string) error {
	if !isDaemonRunning() {
		log.Println("Daemon not running, starting in background...")
		if err := startDaemonBackground(); err != nil {
			return fmt.Errorf("failed to start daemon: %v", err)
		}
		// Give daemon time to start
		for i := 0; i < 10; i++ {
			time.Sleep(500 * time.Millisecond)
			if isDaemonRunning() {
				break
			}
			log.Println("Waiting for daemon to start...")
		}
		if !isDaemonRunning() {
			return fmt.Errorf("daemon failed to start")
		}
	}

	c := client.NewClient()
	if err := c.ConnectUnixSocket(defaultSocketPath); err != nil {
		return fmt.Errorf("failed to connect to daemon: %v", err)
	}
	defer c.Close()

	session, err := c.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create session: %v", err)
	}
	defer session.Close()

	return client.ReplaceTerminal(session)
}

func runListCommand() error {
	c := client.NewClient()
	if err := c.ConnectUnixSocket(defaultSocketPath); err != nil {
		return fmt.Errorf("failed to connect to daemon: %v", err)
	}
	defer c.Close()

	ok, data, err := c.SendRequest("list", true, nil)
	if err != nil {
		return fmt.Errorf("failed to send list request: %v", err)
	}
	
	if !ok {
		return fmt.Errorf("list request was rejected")
	}

	var sessions []sshd.SessionInfo
	if err := json.Unmarshal(data, &sessions); err != nil {
		return fmt.Errorf("failed to parse session list: %v", err)
	}

	if len(sessions) == 0 {
		fmt.Println("No active sessions")
		return nil
	}

	fmt.Printf("Active sessions (%d):\n", len(sessions))
	for _, session := range sessions {
		fmt.Printf("  %s: %s (PID: %d, started: %s)\n",
			session.ID, session.Name, session.PID, session.StartTime.Format("15:04:05"))
	}

	return nil
}