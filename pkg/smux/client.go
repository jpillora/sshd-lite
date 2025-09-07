package smux

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	sshclient "github.com/jpillora/sshd-lite/pkg/client"
)

type Client struct {
	config    Config
	daemon    *Daemon
	port      int
	sshClient *sshclient.Client
}

func NewClient(config Config) *Client {
	if config.HTTPPort == 0 {
		config.HTTPPort = HTTPPort
	}
	return &Client{
		config:    config,
		daemon:    NewDaemon(config),
		port:      config.HTTPPort,
		sshClient: sshclient.NewClient(),
	}
}

func (c *Client) AttachToSession(sessionID string) error {
	if !c.isHTTPDaemonRunning() {
		log.Println("Daemon not running, starting in background...")
		if err := c.daemon.StartBackground(); err != nil {
			return fmt.Errorf("failed to start daemon: %v", err)
		}
		for range 10 {
			time.Sleep(500 * time.Millisecond)
			if c.isHTTPDaemonRunning() {
				break
			}
			log.Println("Waiting for daemon to start...")
		}
		if !c.isHTTPDaemonRunning() {
			return fmt.Errorf("daemon failed to start")
		}
	}

	sessions, err := c.getSessionList()
	if err != nil {
		return fmt.Errorf("failed to get session list: %v", err)
	}

	var targetSessionID string
	if sessionID == "" {
		sessionID = "1"
	}

	for _, session := range sessions {
		if session.ID == sessionID {
			targetSessionID = session.ID
			break
		}
	}

	if targetSessionID == "" {
		sessionIDResult, err := c.createSession(sessionID)
		if err != nil {
			return fmt.Errorf("failed to create session: %v", err)
		}
		targetSessionID = sessionIDResult
	}

	url := fmt.Sprintf("http://localhost:%d/attach/%s", c.port, targetSessionID)
	fmt.Printf("Opening browser to: %s\n", url)
	fmt.Printf("Or visit: http://localhost:%d\n", c.port)
	
	return nil
}

func (c *Client) ListSessions() error {
	if !c.isHTTPDaemonRunning() {
		fmt.Println("Daemon not running")
		return nil
	}

	sessions, err := c.getSessionList()
	if err != nil {
		return fmt.Errorf("failed to get session list: %v", err)
	}

	if len(sessions) == 0 {
		fmt.Println("No active sessions")
		return nil
	}

	fmt.Printf("Active sessions (%d):\n", len(sessions))
	for _, session := range sessions {
		fmt.Printf("  %s (%d clients, started: %s)\n",
			session.ID, session.ClientCount, session.StartTime)
	}
	fmt.Printf("\nWebUI available at: http://localhost:%d\n", c.port)

	return nil
}

func (c *Client) CreateNewSession(sessionID, initialCommand string) error {
	if !c.isHTTPDaemonRunning() {
		log.Println("Daemon not running, starting in background...")
		if err := c.daemon.StartBackground(); err != nil {
			return fmt.Errorf("failed to start daemon: %v", err)
		}
		for range 10 {
			time.Sleep(500 * time.Millisecond)
			if c.isHTTPDaemonRunning() {
				break
			}
			log.Println("Waiting for daemon to start...")
		}
		if !c.isHTTPDaemonRunning() {
			return fmt.Errorf("daemon failed to start")
		}
	}

	sessionIDResult, err := c.createSessionWithCommand(sessionID, initialCommand)
	if err != nil {
		return fmt.Errorf("failed to create session: %v", err)
	}

	fmt.Printf("Created session: %s\n", sessionIDResult)
	fmt.Printf("WebUI: http://localhost:%d/attach/%s\n", c.port, sessionIDResult)
	fmt.Printf("Or visit: http://localhost:%d\n", c.port)

	return nil
}

type sessionInfo struct {
	ID          string `json:"id"`
	StartTime   string `json:"start_time"`
	ClientCount int    `json:"client_count"`
}

func (c *Client) isHTTPDaemonRunning() bool {
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/api/sessions", c.port))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (c *Client) getSessionList() ([]sessionInfo, error) {
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/api/sessions", c.port))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var sessions []sessionInfo
	if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
		return nil, err
	}

	return sessions, nil
}

func (c *Client) createSession(id string) (string, error) {
	reqBody := map[string]string{}
	if id != "" {
		reqBody["id"] = id
	}
	
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	resp, err := http.Post(
		fmt.Sprintf("http://localhost:%d/api/sessions/create", c.port),
		"application/json",
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to create session: %s", resp.Status)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	sessionID, ok := result["id"].(string)
	if !ok {
		return "", fmt.Errorf("invalid response format")
	}

	return sessionID, nil
}

func (c *Client) createSessionWithCommand(id, command string) (string, error) {
	reqBody := map[string]string{}
	if id != "" {
		reqBody["id"] = id
	}
	
	if command != "" {
		reqBody["command"] = command
	}
	
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	resp, err := http.Post(
		fmt.Sprintf("http://localhost:%d/api/sessions/create", c.port),
		"application/json",
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to create session: %s", resp.Status)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	sessionID, ok := result["id"].(string)
	if !ok {
		return "", fmt.Errorf("invalid response format")
	}

	return sessionID, nil
}