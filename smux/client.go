package smux

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/jpillora/sshd-lite/client"
	"github.com/jpillora/sshd-lite/server"
)

func AttachToSession(sessionName string) error {
	if !IsDaemonRunning() {
		log.Println("Daemon not running, starting in background...")
		if err := StartDaemonBackground(); err != nil {
			return fmt.Errorf("failed to start daemon: %v", err)
		}
		// Give daemon time to start
		for i := 0; i < 10; i++ {
			time.Sleep(500 * time.Millisecond)
			if IsDaemonRunning() {
				break
			}
			log.Println("Waiting for daemon to start...")
		}
		if !IsDaemonRunning() {
			return fmt.Errorf("daemon failed to start")
		}
	}

	c := client.NewClient()
	if err := c.ConnectUnixSocket(DefaultSocketPath); err != nil {
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

func ListSessions() error {
	c := client.NewClient()
	if err := c.ConnectUnixSocket(DefaultSocketPath); err != nil {
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