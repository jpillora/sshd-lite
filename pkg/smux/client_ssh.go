package smux

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
)

func (c *Client) isSSHDaemonRunning() bool {
	_, err := os.Stat(c.config.SocketPath)
	return err == nil
}

func (c *Client) trySSHAttach(sessionID string) bool {
	if !c.isSSHDaemonRunning() {
		return false
	}
	
	if err := c.sshClient.ConnectUnixSocket(c.config.SocketPath); err != nil {
		log.Printf("Failed to connect via SSH: %v", err)
		return false
	}
	
	session, err := c.sshClient.NewSession()
	if err != nil {
		log.Printf("Failed to create SSH session: %v", err)
		return false
	}
	defer session.Close()
	
	if sessionID == "" {
		sessionID = "1"
	}
	
	log.Printf("Attaching to session %s via SSH", sessionID)
	
	session.Stdin = os.Stdin
	session.Stdout = os.Stdout  
	session.Stderr = os.Stderr
	
	if err := session.RequestPty("xterm", 80, 24, nil); err != nil {
		log.Printf("Failed to request PTY: %v", err)
		return false
	}
	
	if err := session.Shell(); err != nil {
		log.Printf("Failed to start shell: %v", err)
		return false
	}
	
	if err := session.Wait(); err != nil {
		log.Printf("SSH session ended: %v", err)
	}
	
	return true
}

func (c *Client) trySSHListSessions() bool {
	if !c.isSSHDaemonRunning() {
		return false
	}
	
	if err := c.sshClient.ConnectUnixSocket(c.config.SocketPath); err != nil {
		log.Printf("Failed to connect via SSH: %v", err)
		return false
	}
	
	session, err := c.sshClient.NewSession()
	if err != nil {
		log.Printf("Failed to create SSH session: %v", err)
		return false
	}
	defer session.Close()
	
	output, err := session.Output("echo 'list-sessions' | nc -U " + c.config.SocketPath)
	if err != nil {
		log.Printf("Failed to list sessions via SSH: %v", err)
		return false
	}
	
	var sessions []sessionInfo
	if err := json.Unmarshal(output, &sessions); err != nil {
		log.Printf("Failed to parse session list: %v", err)
		return false
	}
	
	if len(sessions) == 0 {
		fmt.Println("No active sessions")
		return true
	}
	
	fmt.Printf("Active sessions (%d):\n", len(sessions))
	for _, session := range sessions {
		fmt.Printf("  %s (%d clients, started: %s)\n",
			session.ID, session.ClientCount, session.StartTime)
	}
	
	return true
}

func (c *Client) trySSHCreateSession(sessionID, initialCommand string) (string, bool) {
	if !c.isSSHDaemonRunning() {
		return "", false
	}
	
	if err := c.sshClient.ConnectUnixSocket(c.config.SocketPath); err != nil {
		log.Printf("Failed to connect via SSH: %v", err)
		return "", false
	}
	
	session, err := c.sshClient.NewSession()
	if err != nil {
		log.Printf("Failed to create SSH session: %v", err)
		return "", false
	}
	defer session.Close()
	
	reqData := map[string]string{}
	if sessionID != "" {
		reqData["id"] = sessionID
	}
	if initialCommand != "" {
		reqData["command"] = initialCommand
	}
	
	jsonData, _ := json.Marshal(reqData)
	cmd := fmt.Sprintf("echo '%s' | nc -U %s", string(jsonData), c.config.SocketPath)
	output, err := session.Output(cmd)
	if err != nil {
		log.Printf("Failed to create session via SSH: %v", err)
		return "", false
	}
	
	var result map[string]any
	if err := json.Unmarshal(output, &result); err != nil {
		log.Printf("Failed to parse create session response: %v", err)
		return "", false
	}
	
	id, ok := result["id"].(string)
	if !ok {
		return "", false
	}
	
	return id, true
}