package smux

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

func AttachToSession(sessionID string) error {
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
	if sessionID == "" {
		sessionID = "1"
	}

	// Look for existing session by ID
	for _, session := range sessions {
		if session.ID == sessionID {
			targetSessionID = session.ID
			break
		}
	}

	// If no session found, create one
	if targetSessionID == "" {
		sessionIDResult, err := createSession(sessionID)
		if err != nil {
			return fmt.Errorf("failed to create session: %v", err)
		}
		targetSessionID = sessionIDResult
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
		fmt.Printf("  %s (%d clients, started: %s)\n",
			session.ID, session.ClientCount, session.StartTime)
	}
	fmt.Printf("\nWebUI available at: http://localhost:%d\n", HTTPPort)

	return nil
}

type sessionInfo struct {
	ID          string `json:"id"`
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

func getSessionList() ([]sessionInfo, error) {
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/api/sessions", HTTPPort))
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

func createSession(id string) (string, error) {
	reqBody := map[string]string{}
	if id != "" {
		reqBody["id"] = id
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

func CreateNewSession(sessionID, initialCommand string) error {
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
	sessionIDResult, err := createSessionWithCommand(sessionID, initialCommand)
	if err != nil {
		return fmt.Errorf("failed to create session: %v", err)
	}

	fmt.Printf("Created session: %s\n", sessionIDResult)
	fmt.Printf("WebUI: http://localhost:%d/attach/%s\n", HTTPPort, sessionIDResult)
	fmt.Printf("Or visit: http://localhost:%d\n", HTTPPort)

	return nil
}

func createSessionWithCommand(id, command string) (string, error) {
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