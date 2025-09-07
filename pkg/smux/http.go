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

type httpServer struct {
	sessionManager *sessionManager
	mux            *http.ServeMux
	port           int
	socketPath     string
}

func newHTTPServer(sessionManager *sessionManager, port int, socketPath string) *httpServer {
	server := &httpServer{
		sessionManager: sessionManager,
		mux:            http.NewServeMux(),
		port:           port,
		socketPath:     socketPath,
	}
	
	server.setupRoutes()
	return server
}

func (hs *httpServer) setupRoutes() {
	hs.mux.HandleFunc("/", hs.handleIndex)
	hs.mux.HandleFunc("/api/sessions", hs.handleAPISessions)
	hs.mux.HandleFunc("/api/sessions/create", hs.handleAPICreateSession)
	hs.mux.HandleFunc("/api/sessions/new", hs.handleAPICreateSession)
	hs.mux.HandleFunc("/attach/", hs.handleAttach)
}

func (hs *httpServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	
	w.Header().Set("Content-Type", "text/html")
	w.Write(indexHTML)
}

func (hs *httpServer) handleAPISessions(w http.ResponseWriter, r *http.Request) {
	sessions := hs.sessionManager.ListSessions()
	
	type sessionInfo struct {
		ID          string `json:"id"`
		StartTime   string `json:"start_time"`
		ClientCount int    `json:"client_count"`
	}
	
	var sessionList []sessionInfo
	for _, session := range sessions {
		sessionList = append(sessionList, sessionInfo{
			ID:          session.ID,
			StartTime:   session.StartTime.Format("2006-01-02 15:04:05"),
			ClientCount: session.GetClientCount(),
		})
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sessionList)
}

func (hs *httpServer) handleAPICreateSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	var req struct {
		ID      string `json:"id"`
		Command string `json:"command"`
	}
	
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	
	if req.ID == "" {
		req.ID = hs.sessionManager.generateSessionID()
	}
	
	var session *session
	var err error
	
	if req.Command != "" {
		session, err = hs.sessionManager.CreateSessionWithCommand(req.ID, req.Command)
	} else {
		session, err = hs.sessionManager.CreateSession(req.ID)
	}
	
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	
	w.Header().Set("Content-Type", "application/json")
	response := map[string]any{
		"id":         session.ID,
		"start_time": session.StartTime.Format("2006-01-02 15:04:05"),
	}
	
	if req.Command != "" {
		response["command"] = req.Command
	}
	
	json.NewEncoder(w).Encode(response)
}

func (hs *httpServer) Start() error {
	log.Printf("Starting HTTP server on port %d", hs.port)
	return http.ListenAndServe(fmt.Sprintf(":%d", hs.port), hs.mux)
}