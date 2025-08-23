package smux

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

//go:embed static/index.html
var indexHTML []byte

const HTTPPort = 6688

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
	hs.mux.HandleFunc("/api/sessions/new", hs.handleAPICreateSession)
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
		ID      string `json:"id"`
		Name    string `json:"name"`
		Command string `json:"command"`
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
	
	var session *Session
	var err error
	
	if req.Command != "" {
		session, err = hs.sessionManager.CreateSessionWithCommand(req.ID, req.Name, req.Command)
	} else {
		session, err = hs.sessionManager.CreateSession(req.ID, req.Name)
	}
	
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	
	w.Header().Set("Content-Type", "application/json")
	response := map[string]any{
		"id":         session.ID,
		"name":       session.Name,
		"start_time": session.StartTime.Format("2006-01-02 15:04:05"),
	}
	
	if req.Command != "" {
		response["command"] = req.Command
	}
	
	json.NewEncoder(w).Encode(response)
}

func (hs *HTTPServer) Start() error {
	log.Printf("Starting HTTP server on port %d", HTTPPort)
	return http.ListenAndServe(fmt.Sprintf(":%d", HTTPPort), hs.mux)
}