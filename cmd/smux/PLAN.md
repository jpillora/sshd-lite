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

- [x] Setup CLI framework with jpillora/opts
  - [x] Add jpillora/opts dependency to go.mod
  - [x] Create basic CLI structure with daemon, attach, list subcommands
  - [x] Add --foreground flag to daemon command
- [x] Create client package for SSH connections
  - [x] Implement SSH client for Unix socket connections
  - [x] Add PTY handling and terminal replacement functions
  - [x] Add connection utilities for `/var/run/smux.sock`
- [x] Extend server package for Unix socket support
  - [x] Add Unix socket listener capability
  - [x] Add session tracking data structures
  - [x] Implement custom SSH request handler for "list" command
  - [x] Force bash shells for all sessions
- [x] Implement daemon command
  - [x] Add PID file handling at `/var/run/smux.pid`
  - [x] Implement background/foreground mode switching
  - [x] Add log output routing (stdout vs `/var/run/smux.log`)
  - [x] Start SSH server on Unix socket
- [x] Implement attach command
  - [x] Add daemon detection (PID file + process check)
  - [x] Add auto-start daemon functionality
  - [x] Connect to Unix socket and request shell session
  - [x] Replace current terminal with remote PTY
- [x] Implement list command
  - [x] Connect to daemon via SSH
  - [x] Send "list" SSH request with want-reply:true
  - [x] Parse and display JSON response
- [x] Add comprehensive session management
  - [x] Named shell session tracking
  - [x] Session creation/termination handling
  - [x] JSON serialization for session data

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