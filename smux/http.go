package smux

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

//go:embed static/index.html
var indexHTML []byte

const HTTPPort = 6688

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for development
	},
}

type HTTPServer struct {
	sessionManager *SessionManager
	mux            *http.ServeMux
}

func NewHTTPServer(sessionManager *SessionManager) *HTTPServer {
	server := &HTTPServer{
		sessionManager: sessionManager,
		mux:            http.NewServeMux(),
	}
	
	server.setupRoutes()
	return server
}

func (hs *HTTPServer) setupRoutes() {
	hs.mux.HandleFunc("/", hs.handleIndex)
	hs.mux.HandleFunc("/api/sessions", hs.handleAPISessions)
	hs.mux.HandleFunc("/api/sessions/create", hs.handleAPICreateSession)
	hs.mux.HandleFunc("/attach/", hs.handleAttach)
}

func (hs *HTTPServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	
	w.Header().Set("Content-Type", "text/html")
	w.Write(indexHTML)
}

func (hs *HTTPServer) handleAPISessions(w http.ResponseWriter, r *http.Request) {
	sessions := hs.sessionManager.ListSessions()
	
	type sessionInfo struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		StartTime   string `json:"start_time"`
		ClientCount int    `json:"client_count"`
	}
	
	var sessionList []sessionInfo
	for _, session := range sessions {
		sessionList = append(sessionList, sessionInfo{
			ID:          session.ID,
			Name:        session.Name,
			StartTime:   session.StartTime.Format("2006-01-02 15:04:05"),
			ClientCount: session.GetClientCount(),
		})
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sessionList)
}

func (hs *HTTPServer) handleAPICreateSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	var req struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	
	if req.ID == "" {
		req.ID = generateSessionID()
	}
	if req.Name == "" {
		req.Name = fmt.Sprintf("session-%s", req.ID[:8])
	}
	
	session, err := hs.sessionManager.CreateSession(req.ID, req.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":         session.ID,
		"name":       session.Name,
		"start_time": session.StartTime.Format("2006-01-02 15:04:05"),
	})
}

func (hs *HTTPServer) handleAttach(w http.ResponseWriter, r *http.Request) {
	// Extract session ID from path like /attach/{id}
	path := r.URL.Path
	if !strings.HasPrefix(path, "/attach/") {
		http.NotFound(w, r)
		return
	}
	
	sessionID := path[8:] // Remove "/attach/" prefix
	if sessionID == "" {
		http.Error(w, "Session ID required", http.StatusBadRequest)
		return
	}
	
	session, exists := hs.sessionManager.GetSession(sessionID)
	if !exists {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}
	
	// Upgrade to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()
	
	clientID := generateSessionID()
	log.Printf("WebSocket client %s connecting to session %s", clientID, sessionID)
	
	// Create WebSocket wrapper for client
	wsClient := &WebSocketClient{
		conn:      conn,
		sessionID: sessionID,
		clientID:  clientID,
	}
	
	// Add client to session
	session.AddClient(clientID, wsClient, wsClient)
	
	// Handle WebSocket messages
	wsClient.handleMessages(session)
}

func (hs *HTTPServer) Start() error {
	log.Printf("Starting HTTP server on port %d", HTTPPort)
	return http.ListenAndServe(fmt.Sprintf(":%d", HTTPPort), hs.mux)
}

type WebSocketClient struct {
	conn      *websocket.Conn
	sessionID string
	clientID  string
}

func (wsc *WebSocketClient) Write(data []byte) (int, error) {
	err := wsc.conn.WriteMessage(websocket.TextMessage, data)
	if err != nil {
		return 0, err
	}
	return len(data), nil
}

func (wsc *WebSocketClient) Read(data []byte) (int, error) {
	// This will be handled by handleMessages instead
	return 0, fmt.Errorf("read not supported directly")
}

func (wsc *WebSocketClient) handleMessages(session *Session) {
	defer func() {
		session.RemoveClient(wsc.clientID)
		wsc.conn.Close()
	}()
	
	for {
		messageType, data, err := wsc.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}
		
		switch messageType {
		case websocket.TextMessage:
			// Handle different message types
			var msg map[string]interface{}
			if err := json.Unmarshal(data, &msg); err == nil {
				if msgType, ok := msg["type"].(string); ok {
					switch msgType {
					case "resize":
						if rows, ok := msg["rows"].(float64); ok {
							if cols, ok := msg["cols"].(float64); ok {
								session.Resize(int(rows), int(cols))
							}
						}
					case "input":
						if input, ok := msg["data"].(string); ok {
							session.PTY.Write([]byte(input))
						}
					}
					continue
				}
			}
			
			// Fallback: treat as raw input
			session.PTY.Write(data)
			
		case websocket.BinaryMessage:
			session.PTY.Write(data)
		}
	}
}

func generateSessionID() string {
	return strconv.FormatInt(time.Now().UnixNano(), 36)[:8]
}