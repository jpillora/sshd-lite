package smux

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jpillora/sshd-lite/pkg/client"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func (hs *HTTPServer) handleAttach(w http.ResponseWriter, r *http.Request) {
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
	
	session, exists := hs.sessionManager.GetSession(sessionID)
	if !exists {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}
	
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()
	
	clientID := generateSessionID()
	log.Printf("WebSocket client %s connecting to session %s", clientID, sessionID)
	
	wsWrapper := &WebSocketWrapper{conn: conn}
	ptySession := session.GetPTYSession()
	wsSession := client.AttachWebSocketToSession(ptySession, wsWrapper, wsWrapper)
	defer wsSession.Close()
	
	for {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
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
								wsSession.WindowChange(int(rows), int(cols))
							}
						}
						continue
					case "input":
						if input, ok := msg["data"].(string); ok {
							wsSession.Write([]byte(input))
						}
						continue
					}
				}
			}
			
			wsSession.Write(data)
			
		case websocket.BinaryMessage:
			wsSession.Write(data)
		}
	}
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

func generateSessionID() string {
	return strconv.FormatInt(time.Now().UnixNano(), 36)[:8]
}