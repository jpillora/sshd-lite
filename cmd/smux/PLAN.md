# Implementation Plan for smux

## Overview
Build a terminal multiplexer using SSH protocol with daemon/client architecture over Unix sockets.

## Components Required

### 1. CLI Structure (using jpillora/opts)
- Main command with subcommands: daemon, attach, list
- Flag parsing for --foreground option
- Process management for daemon startup

### 2. Daemon Implementation
- **Process Management**
  - PID file handling at `/var/run/smux.pid`
  - Background/foreground mode switching
  - Log output to `/var/run/smux.log` (background) or stdout (foreground)
- **SSH Server**
  - Extend existing `server` package for Unix socket listening
  - Listen on `/var/run/smux.sock` by default
  - Force bash shells for all sessions
- **Session Management**
  - Track active shell sessions
  - Handle session creation/termination
  - Implement custom SSH request handler for "list" command

### 3. Client Package (New)
- SSH client implementation for Unix socket connections
- PTY handling and terminal replacement
- Connection to `/var/run/smux.sock`

### 4. Attach Command
- **Daemon Detection**
  - Check if daemon is running (PID file + process check)
  - Auto-start daemon in background if not running
- **SSH Client Connection**
  - Connect to Unix socket
  - Request named shell session
  - Replace current terminal with remote PTY

### 5. List Command
- SSH client connection to daemon
- Send custom "list" SSH request with want-reply:true
- Parse and display JSON response containing active sessions

## TODO List

- [ ] Setup CLI framework with jpillora/opts
  - [ ] Add jpillora/opts dependency to go.mod
  - [ ] Create basic CLI structure with daemon, attach, list subcommands
  - [ ] Add --foreground flag to daemon command
- [ ] Create client package for SSH connections
  - [ ] Implement SSH client for Unix socket connections
  - [ ] Add PTY handling and terminal replacement functions
  - [ ] Add connection utilities for `/var/run/smux.sock`
- [ ] Extend server package for Unix socket support
  - [ ] Add Unix socket listener capability
  - [ ] Add session tracking data structures
  - [ ] Implement custom SSH request handler for "list" command
  - [ ] Force bash shells for all sessions
- [ ] Implement daemon command
  - [ ] Add PID file handling at `/var/run/smux.pid`
  - [ ] Implement background/foreground mode switching
  - [ ] Add log output routing (stdout vs `/var/run/smux.log`)
  - [ ] Start SSH server on Unix socket
- [ ] Implement attach command
  - [ ] Add daemon detection (PID file + process check)
  - [ ] Add auto-start daemon functionality
  - [ ] Connect to Unix socket and request shell session
  - [ ] Replace current terminal with remote PTY
- [ ] Implement list command
  - [ ] Connect to daemon via SSH
  - [ ] Send "list" SSH request with want-reply:true
  - [ ] Parse and display JSON response
- [ ] Add comprehensive session management
  - [ ] Named shell session tracking
  - [ ] Session creation/termination handling
  - [ ] JSON serialization for session data

## Implementation Steps

1. **Setup CLI framework** - Configure jpillora/opts with subcommands and flags
2. **Extend server package** - Add Unix socket support and session tracking
3. **Create client package** - SSH client for Unix socket connections
4. **Implement daemon command** - Process management and SSH server startup
5. **Implement attach command** - Daemon detection, auto-start, and terminal replacement
6. **Implement list command** - SSH request handling and JSON response parsing
7. **Add session management** - Named shell tracking and custom SSH request handlers

## Key Technical Considerations

- Unix socket permissions and access control
- PTY handling and terminal state management
- Process lifecycle management (daemon startup/shutdown)
- SSH protocol extensions for custom requests
- Error handling for socket connection failures