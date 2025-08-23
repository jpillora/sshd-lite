package smux

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

func AttachToSession(sessionName string) error {
	// Check if daemon is running on HTTP port
	if !isHTTPDaemonRunning() {
		log.Println("Daemon not running, starting in background...")
		if err := StartDaemonBackground(); err != nil {
			return fmt.Errorf("failed to start daemon: %v", err)
		}
		// Give daemon time to start
		for i := 0; i < 10; i++ {
			time.Sleep(500 * time.Millisecond)
			if isHTTPDaemonRunning() {
				break
			}
			log.Println("Waiting for daemon to start...")
		}
		if !isHTTPDaemonRunning() {
			return fmt.Errorf("daemon failed to start")
		}
	}

	// Get list of sessions
	sessions, err := getSessionList()
	if err != nil {
		return fmt.Errorf("failed to get session list: %v", err)
	}

	// Find or create session
	var targetSessionID string
	if sessionName == "" {
		sessionName = "default"
	}

	// Look for existing session by name
	for _, session := range sessions {
		if session.Name == sessionName {
			targetSessionID = session.ID
			break
		}
	}

	// If no session found, create one
	if targetSessionID == "" {
		sessionID, err := createSession(sessionName)
		if err != nil {
			return fmt.Errorf("failed to create session: %v", err)
		}
		targetSessionID = sessionID
	}

	// Open browser to the session
	url := fmt.Sprintf("http://localhost:%d/attach/%s", HTTPPort, targetSessionID)
	fmt.Printf("Opening browser to: %s\n", url)
	fmt.Printf("Or visit: http://localhost:%d\n", HTTPPort)
	
	// Try to open browser (this is a simple approach)
	// In a real implementation, you might want to use a more sophisticated method
	return nil
}

func ListSessions() error {
	if !isHTTPDaemonRunning() {
		fmt.Println("Daemon not running")
		return nil
	}

	sessions, err := getSessionList()
	if err != nil {
		return fmt.Errorf("failed to get session list: %v", err)
	}

	if len(sessions) == 0 {
		fmt.Println("No active sessions")
		return nil
	}

	fmt.Printf("Active sessions (%d):\n", len(sessions))
	for _, session := range sessions {
		fmt.Printf("  %s: %s (%d clients, started: %s)\n",
			session.ID, session.Name, session.ClientCount, session.StartTime)
	}
	fmt.Printf("\nWebUI available at: http://localhost:%d\n", HTTPPort)

	return nil
}

type SessionInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	StartTime   string `json:"start_time"`
	ClientCount int    `json:"client_count"`
}

func isHTTPDaemonRunning() bool {
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/api/sessions", HTTPPort))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func getSessionList() ([]SessionInfo, error) {
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/api/sessions", HTTPPort))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var sessions []SessionInfo
	if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
		return nil, err
	}

	return sessions, nil
}

func createSession(name string) (string, error) {
	reqBody := map[string]string{
		"name": name,
	}
	
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	resp, err := http.Post(
		fmt.Sprintf("http://localhost:%d/api/sessions/create", HTTPPort),
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

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	sessionID, ok := result["id"].(string)
	if !ok {
		return "", fmt.Errorf("invalid response format")
	}

	return sessionID, nil
}

func CreateNewSession(sessionName, initialCommand string) error {
	// Check if daemon is running on HTTP port
	if !isHTTPDaemonRunning() {
		log.Println("Daemon not running, starting in background...")
		if err := StartDaemonBackground(); err != nil {
			return fmt.Errorf("failed to start daemon: %v", err)
		}
		// Give daemon time to start
		for i := 0; i < 10; i++ {
			time.Sleep(500 * time.Millisecond)
			if isHTTPDaemonRunning() {
				break
			}
			log.Println("Waiting for daemon to start...")
		}
		if !isHTTPDaemonRunning() {
			return fmt.Errorf("daemon failed to start")
		}
	}

	// Create session via API
	sessionID, err := createSessionWithCommand(sessionName, initialCommand)
	if err != nil {
		return fmt.Errorf("failed to create session: %v", err)
	}

	fmt.Printf("Created session: %s\n", sessionID)
	fmt.Printf("WebUI: http://localhost:%d/attach/%s\n", HTTPPort, sessionID)
	fmt.Printf("Or visit: http://localhost:%d\n", HTTPPort)

	return nil
}

func createSessionWithCommand(name, command string) (string, error) {
	reqBody := map[string]string{
		"name": name,
	}
	
	if command != "" {
		reqBody["command"] = command
	}
	
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	resp, err := http.Post(
		fmt.Sprintf("http://localhost:%d/api/sessions/create", HTTPPort),
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

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	sessionID, ok := result["id"].(string)
	if !ok {
		return "", fmt.Errorf("invalid response format")
	}

	return sessionID, nil
}