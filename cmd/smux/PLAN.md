# smux Implementation Plan

## Overview

`smux` is an SSH terminal multiplexer that provides both web-based and SSH client access to persistent terminal sessions. It consists of a daemon that manages PTY sessions and multiple client interfaces.

## Architecture

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   Web Browser   │    │   SSH Client    │    │  smux CLI Tool  │
│  (xterm.js +    │    │                 │    │                 │
│   WebSocket)    │    │                 │    │                 │
└─────────┬───────┘    └─────────┬───────┘    └─────────┬───────┘
          │                      │                      │
          │ WebSocket            │ SSH                  │ HTTP API
          │                      │                      │
          v                      v                      v
┌─────────────────────────────────────────────────────────────────┐
│                        smux daemon                              │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐  │
│  │   HTTP Server   │  │   SSH Server    │  │  Session Mgr    │  │
│  │  (web ui +      │  │                 │  │                 │  │
│  │   websocket)    │  │                 │  │                 │  │
│  └─────────────────┘  └─────────────────┘  └─────────────────┘  │
│                                              │                  │
│                                              v                  │
│                                        ┌─────────────────┐      │
│                                        │  PTY Sessions   │      │
│                                        │  (pty1, pty2,   │      │
│                                        │   pty3, ...)    │      │
│                                        └─────────────────┘      │
└─────────────────────────────────────────────────────────────────┘
```

## File Structure

### Current State
```
cmd/smux/
├── main.go          ✓ (needs refactoring)
├── SPEC.md          ✓
├── design.excalidraw.svg ✓
└── PLAN.md          ← creating now

pkg/smux/
├── daemon.go        ✓ (needs SSH server integration)
├── daemon_unix.go   ✓
├── daemon_windows.go ✓
├── daemon_test.go   ✓
├── session_mgr.go   ✓
├── session.go       ✓
├── client.go        ✓ (needs SSH client integration)
├── http.go          ✓
├── http_ws.go       ✓
├── smux_test.go     ✓
├── smux_test_windows.go ✓
└── static/
    └── index.html   ✓

pkg/server/          ✓ (SSH server implementation)
pkg/client/          ✓ (SSH client implementation)
```

### Target File Structure
```
cmd/smux/
├── main.go          → CLI entry point with opts parsing
└── PLAN.md          → this file

pkg/smux/
├── config.go        → Configuration structures
├── daemon.go        → Main daemon implementation
├── daemon_unix.go   → Unix-specific daemon operations
├── daemon_windows.go → Windows-specific daemon operations
├── session_mgr.go   → Session management logic
├── session.go       → Individual session handling
├── client.go        → Client operations (SSH + HTTP)
├── ssh_server.go    → SSH server integration
├── http_server.go   → HTTP server for web UI
├── websocket.go     → WebSocket handling for web UI
├── unix_socket.go   → Unix domain socket handling
└── static/          → Web UI assets
    └── index.html
```

## Implementation Plan

### Phase 1: Core Infrastructure ✓
- [x] Basic daemon structure with process management
- [x] Session manager with PTY handling
- [x] HTTP server with WebSocket support
- [x] Basic CLI structure with opts

### Phase 2: SSH Integration (Current Focus)
- [ ] Integrate `pkg/server` for SSH daemon functionality
- [ ] Integrate `pkg/client` for SSH client functionality
- [ ] Unix domain socket communication
- [ ] SSH protocol handlers for session management

### Phase 3: Client Operations
- [ ] `smux attach` - SSH client attachment to sessions
- [ ] `smux list` - Session listing via SSH protocol
- [ ] `smux new` - Session creation via SSH protocol
- [ ] Automatic daemon startup detection

### Phase 4: Web Interface Enhancement
- [ ] Session management UI
- [ ] Multiple tab support
- [ ] Session creation/deletion from web
- [ ] Real-time session list updates

### Phase 5: Testing & Reliability
- [ ] Comprehensive test coverage
- [ ] Cross-platform compatibility testing
- [ ] Error handling and recovery
- [ ] Performance optimization

## Detailed Implementation

### 1. SSH Server Integration

The daemon needs to integrate `pkg/server` to provide SSH access:

```go
// In daemon.go
type Daemon struct {
    config         Config
    sessionManager *SessionManager
    httpServer     *HTTPServer
    sshServer      *server.Server  // New
    unixSocket     *UnixSocket     // New
}
```

**Key Requirements:**
- SSH server listens on Unix domain socket (`/var/run/smux.sock`)
- Implements custom SSH request handlers for session operations
- Uses existing `pkg/server` SSH implementation
- Supports multiple concurrent SSH connections

### 2. SSH Client Integration

The client needs to use `pkg/client` for SSH connections:

```go
// In client.go
type Client struct {
    config     Config
    sshClient  *client.Client  // New
    httpClient *http.Client
}
```

**Key Requirements:**
- SSH client connects to Unix domain socket
- Implements session attachment with PTY forwarding
- Handles SSH requests for list/new operations
- Falls back to HTTP API when SSH unavailable

### 3. Session Management Protocol

**SSH Custom Requests:**
- `list-sessions` - Returns JSON array of active sessions
- `create-session` - Creates new session, returns session ID
- `attach-session` - Attaches to existing session with PTY forwarding

**HTTP API Endpoints:**
- `GET /api/sessions` - List sessions
- `POST /api/sessions` - Create session
- `WebSocket /attach/{sessionID}` - Attach to session

### 4. Unix Domain Socket Handling

**Security Model:**
- Socket owned by current user
- Permissions: 0600 (user read/write only)
- PID file prevents multiple daemon instances
- Automatic cleanup on daemon termination

### 5. Process Management

**Daemon Lifecycle:**
```
smux daemon [--background]
├── Check if already running (PID file)
├── Create Unix socket
├── Start SSH server on socket
├── Start HTTP server on configured port
├── Initialize session manager
└── Handle signals for graceful shutdown
```

**Background Mode:**
- Fork process with setsid()
- Redirect stdout/stderr to log file
- Write PID file
- Detach from terminal

### 6. Client Workflow

**Attach Operation:**
```
smux attach [session-name]
├── Check if daemon running (Unix socket exists)
├── If not running: start daemon in background
├── Connect via SSH to Unix socket
├── Request session list if no name provided
├── Attach to session with PTY forwarding
└── Replace current terminal with remote session
```

## Testing Strategy

### Unit Tests
- Session manager operations
- HTTP API endpoints
- SSH request handlers
- Process management functions

### Integration Tests
- Full daemon startup/shutdown cycle
- SSH client to SSH server communication
- WebSocket session attachment
- Cross-platform compatibility

### End-to-End Tests
- Complete user workflows
- Multiple concurrent sessions
- Client reconnection scenarios
- Error recovery testing

## Configuration

### Default Values
```go
type Config struct {
    SocketPath string `opts:"help=Unix socket path" default:"/var/run/smux.sock"`
    PIDPath    string `opts:"help=PID file path" default:"/var/run/smux.pid"`
    LogPath    string `opts:"help=Log file path" default:"/var/log/smux.log"`
    HTTPPort   int    `opts:"help=HTTP port for web interface" default:"6688"`
    SSHPort    int    `opts:"help=SSH port (0 for Unix socket only)" default:"0"`
}
```

### Environment Variables
- `SMUX_SOCKET_PATH` - Override default socket path
- `SMUX_HTTP_PORT` - Override default HTTP port
- `SMUX_LOG_LEVEL` - Set logging verbosity

## Security Considerations

1. **Unix Socket Permissions**: Restrict access to owner only
2. **Process Isolation**: Run with minimal privileges
3. **Input Validation**: Sanitize all SSH and HTTP inputs
4. **Session Isolation**: Ensure sessions cannot access each other
5. **Resource Limits**: Prevent resource exhaustion attacks

## Dependencies

### Current Dependencies
- `github.com/jpillora/opts` - CLI parsing
- `github.com/creack/pty` - PTY handling
- `github.com/gorilla/websocket` - WebSocket support

### Additional Dependencies Needed
- SSH libraries (likely already in `pkg/server` and `pkg/client`)
- Unix domain socket libraries (standard library)

## Error Handling

### Daemon Errors
- Socket creation failures
- Port binding conflicts
- Permission denied scenarios
- Resource exhaustion

### Client Errors
- Daemon not running
- Connection failures
- Session not found
- Authentication failures

## Performance Targets

- Support 100+ concurrent sessions
- Sub-100ms session attach latency
- Minimal memory overhead per session
- Graceful degradation under load

## Future Enhancements

### Session Persistence
- Session state survival across daemon restarts
- Configurable session timeout
- Session history and logging

### Advanced Features
- Session sharing between users
- Session recording and playback
- Integration with container runtimes
- Cloud deployment support

## Implementation Priorities

1. **Critical Path**: SSH server integration for `smux attach`
2. **High Priority**: Session management via SSH protocol
3. **Medium Priority**: Enhanced web interface
4. **Low Priority**: Advanced features and optimizations

This plan provides a roadmap for completing the smux implementation while maintaining the existing working components and ensuring robust, testable code.
