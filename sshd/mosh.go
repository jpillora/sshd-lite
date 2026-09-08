package sshd

import (
	"encoding/json"
	"maps"

	"github.com/jpillora/sshd-lite/internal/mosh"
	"github.com/jpillora/sshd-lite/xssh"
)

func (s *Server) moshConfig(server *mosh.Server) *xssh.Config {
	cfg := *s.xsshConfig
	cfg.GlobalRequestHandlers = maps.Clone(cfg.GlobalRequestHandlers)
	cfg.GlobalRequestHandlers[mosh.RequestName] = func(conn xssh.Conn, req *xssh.Request) error {
		var request mosh.Request
		if !req.WantReply || len(req.Payload) > 1024 || json.Unmarshal(req.Payload, &request) != nil || request.Validate() != nil {
			return req.Reply(false, nil)
		}
		credentials, revoke, err := server.Issue(request, func() (mosh.Terminal, error) {
			return xssh.StartTerminal(conn.Config(), conn.RemoteAddr(), conn.LocalAddr(), request.Term, request.Cols, request.Rows)
		})
		if err != nil {
			return req.Reply(false, nil)
		}
		payload, _ := json.Marshal(credentials)
		if err := req.Reply(true, payload); err != nil {
			revoke()
			return err
		}
		return nil
	}
	return &cfg
}
