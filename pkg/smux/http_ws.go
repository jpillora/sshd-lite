package smux

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
	sshclient "github.com/jpillora/sshd-lite/pkg/client"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func (hs *httpServer) handleAttach(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if !strings.HasPrefix(path, "/attach/") {
		http.NotFound(w, r)
		return
	}
	
	sessionID := path[8:]
	if sessionID == "" {
		http.Error(w, "Session ID required", http.StatusBadRequest)
		return
	}
	
	// Upgrade to WebSocket first
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()
	
	// Use SSH client to connect to daemon instead of direct session manager access
	sshClient := sshclient.NewClient()
	sshClient.SetUser(sessionID)
	
	// Connect to the daemon via SSH (using same socket path as daemon)
	socketPath := hs.socketPath
	if socketPath == "" {
		socketPath = GetDefaultSocketPath()
	}
	if err := sshClient.ConnectUnixSocket(socketPath); err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Failed to connect to daemon: %v\n", err)))
		return
	}
	defer sshClient.Close()
	
	// Create SSH session
	session, err := sshClient.NewSession()
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Failed to create SSH session: %v\n", err)))
		return
	}
	defer session.Close()
	
	clientID := hs.sessionManager.generateSessionID()
	log.Printf("WebSocket client %s connecting to session %s via SSH", clientID, sessionID)
	
	// Request PTY
	if err := session.RequestPty("xterm-256color", 24, 80, nil); err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Failed to request PTY: %v\n", err)))
		return
	}
	
	// Create pipes for SSH session I/O
	sessionStdin, err := session.StdinPipe()
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Failed to get stdin pipe: %v\n", err)))
		return
	}
	
	sessionStdout, err := session.StdoutPipe()
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Failed to get stdout pipe: %v\n", err)))
		return
	}
	
	// Start shell
	if err := session.Shell(); err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Failed to start shell: %v\n", err)))
		return
	}
	
	// Bridge WebSocket and SSH session I/O
	done := make(chan bool, 2)
	
	// Read from SSH stdout and send to WebSocket
	go func() {
		defer func() { done <- true }()
		buffer := make([]byte, 1024)
		for {
			n, err := sessionStdout.Read(buffer)
			if err != nil {
				return
			}
			if err := conn.WriteMessage(websocket.TextMessage, buffer[:n]); err != nil {
				return
			}
		}
	}()
	
	// Read from WebSocket and send to SSH stdin
	go func() {
		defer func() { done <- true }()
		for {
			messageType, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			
			switch messageType {
			case websocket.TextMessage:
				var msg map[string]interface{}
				if err := json.Unmarshal(data, &msg); err == nil {
					if msgType, ok := msg["type"].(string); ok {
						switch msgType {
						case "resize":
							if rows, ok := msg["rows"].(float64); ok {
								if cols, ok := msg["cols"].(float64); ok {
									// Handle resize (would need SSH session resize support)
									hs.logger.Debug("Window resize", "cols", int(cols), "rows", int(rows))
								}
							}
							continue
						case "input":
							if input, ok := msg["data"].(string); ok {
								sessionStdin.Write([]byte(input))
							}
							continue
						}
					}
				}
				
				// Fallback to treating as direct input
				sessionStdin.Write(data)
				
			case websocket.BinaryMessage:
				sessionStdin.Write(data)
			}
		}
	}()
	
	// Wait for either direction to close
	<-done
}

type WebSocketWrapper struct {
	conn *websocket.Conn
}

func (w *WebSocketWrapper) Read(p []byte) (int, error) {
	_, data, err := w.conn.ReadMessage()
	if err != nil {
		return 0, err
	}
	
	n := copy(p, data)
	if n < len(data) {
		log.Printf("Warning: WebSocket message truncated (%d bytes lost)", len(data)-n)
	}
	
	return n, nil
}

func (w *WebSocketWrapper) Write(p []byte) (int, error) {
	err := w.conn.WriteMessage(websocket.TextMessage, p)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}