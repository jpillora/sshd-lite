package smux

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	sshclient "github.com/jpillora/sshd-lite/pkg/client"
	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh/terminal"
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
func (c *Client) AttachToSessionSSH(target, sessionName string) error {
	fmt.Printf("Attaching to session '%s' via target '%s'\n", sessionName, target)
	
	// Parse the connection target
	u, err := url.Parse(target)
	if err != nil {
		return fmt.Errorf("invalid target format: %v", err)
	}
	
	fmt.Printf("Parsed target: scheme=%s, path=%s, host=%s\n", u.Scheme, u.Path, u.Host)
	
	switch u.Scheme {
	case "unix":
		return c.attachToSessionUnixSocket(u.Path, sessionName)
	case "tcp":
		return c.attachToSessionTCP(u.Host, sessionName)
	case "http", "https":
		return c.attachToSessionHTTP(target, sessionName)
	default:
		return fmt.Errorf("unsupported scheme: %s (use unix://, tcp://, or http://)", u.Scheme)
	}
}

func (c *Client) attachToSessionUnixSocket(socketPath, sessionName string) error {
	fmt.Printf("Checking socket at: %s\n", socketPath)
	
	// Check if socket exists
	if _, err := os.Stat(socketPath); os.IsNotExist(err) {
		return fmt.Errorf("socket not found: %s (is smux daemon running?)", socketPath)
	}
	
	fmt.Printf("Socket found, setting username to: %s\n", sessionName)
	// Set username BEFORE connecting
	c.sshClient.SetUser(sessionName)
	
	fmt.Printf("Connecting to Unix socket: %s\n", socketPath)
	// Connect to the socket
	if err := c.sshClient.ConnectUnixSocket(socketPath); err != nil {
		return fmt.Errorf("failed to connect to socket: %v", err)
	}
	
	fmt.Printf("Connected successfully, starting SSH session\n")
	return c.attachToSessionViaSSH(sessionName)
}

func (c *Client) attachToSessionTCP(hostPort, sessionName string) error {
	fmt.Printf("Setting username to: %s\n", sessionName)
	// Set username BEFORE connecting
	c.sshClient.SetUser(sessionName)
	
	fmt.Printf("Connecting to TCP: %s\n", hostPort)
	if err := c.sshClient.Connect(hostPort); err != nil {
		return fmt.Errorf("failed to connect to %s: %v", hostPort, err)
	}
	
	fmt.Printf("Connected successfully, starting SSH session\n")
	return c.attachToSessionViaSSH(sessionName)
}

func (c *Client) attachToSessionViaSSH(sessionName string) error {
	fmt.Printf("Creating SSH session for: %s\n", sessionName)
	// Create an SSH session
	session, err := c.sshClient.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create SSH session: %v", err)
	}
	defer session.Close()
	
	// Set up terminal if we're in a terminal
	if terminal.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Printf("Terminal detected, setting up PTY\n")
		// Get terminal size
		width, height, err := terminal.GetSize(int(os.Stdin.Fd()))
		if err != nil {
			width, height = 80, 24 // defaults
		}
		
		fmt.Printf("Terminal size: %dx%d\n", width, height)
		
		// Request PTY
		if err := session.RequestPty("xterm-256color", height, width, nil); err != nil {
			return fmt.Errorf("failed to request PTY: %v", err)
		}
		
		fmt.Printf("PTY requested successfully, setting raw mode\n")
		// Set raw mode
		oldState, err := terminal.MakeRaw(int(os.Stdin.Fd()))
		if err != nil {
			return fmt.Errorf("failed to set raw mode: %v", err)
		}
		defer func() {
			fmt.Printf("Restoring terminal state\n")
			terminal.Restore(int(os.Stdin.Fd()), oldState)
			fmt.Printf("Terminal state restored\n")
		}()
		
		fmt.Printf("Raw mode set, terminal ready\n")
	} else {
		fmt.Printf("Not a terminal, skipping PTY setup\n")
	}
	
	// Connect stdin/stdout/stderr
	session.Stdin = os.Stdin
	session.Stdout = os.Stdout
	session.Stderr = os.Stderr
	
	fmt.Printf("Starting shell session\n")
	// Start shell
	if err := session.Shell(); err != nil {
		return fmt.Errorf("failed to start shell: %v", err)
	}
	
	fmt.Printf("Shell started, waiting for completion\n")
	// Wait for session to complete
	if err := session.Wait(); err != nil {
		// Check if it's just a normal exit
		if strings.Contains(err.Error(), "exit status") {
			fmt.Printf("Session exited normally\n")
			return nil
		}
		return fmt.Errorf("session error: %v", err)
	}
	
	fmt.Printf("Session completed successfully\n")
	return nil
}

func (c *Client) attachToSessionHTTP(target, sessionName string) error {
	fmt.Printf("Attaching to session '%s' via HTTP: %s\n", sessionName, target)
	
	// Parse the HTTP URL
	u, err := url.Parse(target)
	if err != nil {
		return fmt.Errorf("invalid HTTP target: %v", err)
	}
	
	// Build the WebSocket URL for the session
	wsScheme := "ws"
	if u.Scheme == "https" {
		wsScheme = "wss"
	}
	
	wsURL := fmt.Sprintf("%s://%s/attach/%s", wsScheme, u.Host, sessionName)
	fmt.Printf("WebSocket URL: %s\n", wsURL)
	
	// Connect to the WebSocket
	fmt.Printf("Connecting to WebSocket...\n")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to WebSocket: %v", err)
	}
	defer conn.Close()
	
	fmt.Printf("Connected to session '%s' via HTTP WebSocket\n", sessionName)
	
	// Set up terminal if we're in a terminal
	if terminal.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Printf("Terminal detected, setting raw mode\n")
		// Set raw mode
		oldState, err := terminal.MakeRaw(int(os.Stdin.Fd()))
		if err != nil {
			return fmt.Errorf("failed to set raw mode: %v", err)
		}
		defer func() {
			fmt.Printf("Restoring terminal state\n")
			terminal.Restore(int(os.Stdin.Fd()), oldState)
			fmt.Printf("Terminal state restored\n")
		}()
		
		fmt.Printf("Raw mode set, starting WebSocket bridge\n")
	} else {
		fmt.Printf("Not a terminal, starting WebSocket bridge\n")
	}
	
	// Bridge stdin/stdout with WebSocket
	done := make(chan bool, 2)
	
	// Read from WebSocket and write to stdout
	go func() {
		defer func() { done <- true }()
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				return
			}
			os.Stdout.Write(message)
		}
	}()
	
	// Read from stdin and send to WebSocket
	go func() {
		defer func() { done <- true }()
		buffer := make([]byte, 1024)
		for {
			n, err := os.Stdin.Read(buffer)
			if err != nil {
				return
			}
			if err := conn.WriteMessage(websocket.TextMessage, buffer[:n]); err != nil {
				return
			}
		}
	}()
	
	fmt.Printf("WebSocket bridge established, session active\n")
	// Wait for either direction to close
	<-done
	
	fmt.Printf("WebSocket session ended\n")
	return nil
}

