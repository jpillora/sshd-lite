# SSH Test Harness Design

This document describes the design for an AI-friendly end-to-end testing harness
for SSH servers and clients in sshd-lite.

## Design Goals

1. **AI-Friendly** - Clear, declarative syntax that AI can generate and understand
2. **Self-Documenting** - Tests read like specifications
3. **Composable** - Small primitives that combine into complex scenarios
4. **Deterministic** - Reproducible tests without external state (key files, ports)
5. **Fast** - In-memory connections where possible, real connections when needed
6. **Flexible** - Support both sshd-lite and real sshd(8)/ssh(1) for compatibility testing

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                         Test Code                               │
│  env := sshtest.New(t).WithServer(...).WithClient(...)         │
│  env.Run(scenario)                                              │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                      Environment                                 │
│  - Manages server/client lifecycle                              │
│  - Port allocation                                              │
│  - Key generation                                               │
│  - Log capture                                                  │
│  - Event bus                                                    │
└─────────────────────────────────────────────────────────────────┘
        │                     │                      │
        ▼                     ▼                      ▼
┌───────────────┐    ┌───────────────┐    ┌───────────────────────┐
│    Server     │    │    Client     │    │    Scenario Runner    │
│  - sshd-lite  │    │  - Go client  │    │  - Action execution   │
│  - real sshd  │    │  - OS ssh(1)  │    │  - Expectation check  │
│  - mock       │    │  - mock       │    │  - Event matching     │
└───────────────┘    └───────────────┘    └───────────────────────┘
```

## Core Components

### 1. Environment

The `Environment` is the top-level container that manages all test resources.

```go
// Environment manages the test lifecycle for SSH server/client testing.
type Environment struct {
    t        testing.TB
    ctx      context.Context
    cancel   context.CancelFunc

    server   Server
    clients  map[string]Client
    events   *EventBus
    logs     *LogCapture

    // For deterministic key generation
    keySeed  string
}

// New creates a new test environment.
func New(t testing.TB) *Environment

// Builder methods (fluent API)
func (e *Environment) WithServer(opts ...ServerOption) *Environment
func (e *Environment) WithClient(name string, opts ...ClientOption) *Environment
func (e *Environment) WithKeySeed(seed string) *Environment
func (e *Environment) WithTimeout(d time.Duration) *Environment

// Lifecycle
func (e *Environment) Start() *Environment
func (e *Environment) Stop()

// Access
func (e *Environment) Server() Server
func (e *Environment) Client(name string) Client
func (e *Environment) Events() *EventBus
func (e *Environment) Logs() *LogCapture

// Scenario execution
func (e *Environment) Run(scenario *Scenario) error
func (e *Environment) RunYAML(yaml string) error
```

### 2. Server Interface

Abstraction over different server implementations.

```go
// Server represents an SSH server for testing.
type Server interface {
    // Lifecycle
    Start(ctx context.Context) error
    Stop() error

    // Connection info
    Addr() string
    Host() string
    Port() int

    // For client authentication
    HostKey() ssh.PublicKey
    AddAuthorizedKey(name string, key ssh.PublicKey)

    // Observability
    Events() <-chan Event
}

// ServerOption configures a server.
type ServerOption func(*serverConfig)

// Server options
func ServerWithPort(port int) ServerOption           // 0 = auto-assign
func ServerWithSFTP(enabled bool) ServerOption
func ServerWithTCPForwarding(enabled bool) ServerOption
func ServerWithShell(shell string) ServerOption
func ServerWithPassword(user, pass string) ServerOption
func ServerWithHandler(name string, h any) ServerOption  // Custom handlers
func ServerReal(path string) ServerOption            // Use real sshd(8)
```

### 3. Client Interface

Abstraction over different client implementations.

```go
// Client represents an SSH client for testing.
type Client interface {
    // Connection
    Connect() error
    Close() error

    // Session operations
    Shell() (*Session, error)
    Exec(cmd string) (*ExecResult, error)

    // SFTP
    SFTP() (*SFTPClient, error)

    // Port forwarding
    LocalForward(localAddr, remoteAddr string) (net.Listener, error)
    RemoteForward(remoteAddr, localAddr string) error

    // For interactive tests
    PTY() *PTY
}

// Session represents an interactive shell session.
type Session struct {
    io.Reader
    io.Writer
    io.Closer

    Resize(cols, rows uint32) error
    Screen() string  // Captured terminal output (VT100 parsed)
}

// ExecResult contains the result of command execution.
type ExecResult struct {
    Stdout   string
    Stderr   string
    ExitCode int
}

// ClientOption configures a client.
type ClientOption func(*clientConfig)

// Client options
func ClientWithUser(user string) ClientOption
func ClientWithPassword(password string) ClientOption
func ClientWithKey(key ssh.Signer) ClientOption
func ClientWithKeySeed(seed string) ClientOption     // Deterministic key
func ClientWithPTY(cols, rows uint32) ClientOption
func ClientReal() ClientOption                        // Use real ssh(1)
```

### 4. Event System

Event-driven assertions for reliable test verification.

```go
// Event represents something that happened during the test.
type Event struct {
    ID        string            // e.g., "auth.success", "exec.started"
    Timestamp time.Time
    Attrs     map[string]string // Key-value attributes
}

// EventBus collects and queries events.
type EventBus struct {
    events []Event
    ch     chan Event
    mu     sync.RWMutex
}

// Emit sends an event.
func (eb *EventBus) Emit(id string, attrs ...string)

// Wait blocks until an event matching the criteria is received.
func (eb *EventBus) Wait(id string, attrs ...string) (Event, error)

// WaitTimeout waits with a timeout.
func (eb *EventBus) WaitTimeout(timeout time.Duration, id string, attrs ...string) (Event, error)

// All returns all events (for debugging).
func (eb *EventBus) All() []Event

// Clear removes all events.
func (eb *EventBus) Clear()

// Predefined event IDs
const (
    EventAuthSuccess      = "auth.success"
    EventAuthFailure      = "auth.failure"
    EventSessionStarted   = "session.started"
    EventSessionEnded     = "session.ended"
    EventExecStarted      = "exec.started"
    EventExecCompleted    = "exec.completed"
    EventPTYRequested     = "pty.requested"
    EventSFTPStarted      = "sftp.started"
    EventForwardRequested = "forward.requested"
)
```

### 5. Scenario DSL

Declarative test scenarios that can be written in Go or YAML.

```go
// Scenario describes a test scenario.
type Scenario struct {
    Name        string
    Description string
    Setup       []Action
    Steps       []Step
    Cleanup     []Action
}

// Step is a single step in a scenario.
type Step struct {
    Client  string   // Which client performs this step
    Actions []Action
    Expect  []Expectation
}

// Action is something a client does.
type Action interface {
    Execute(ctx context.Context, client Client) error
}

// Expectation is something we verify after actions.
type Expectation interface {
    Check(ctx context.Context, env *Environment) error
}
```

#### Built-in Actions

```go
// Connection actions
func Connect() Action
func Disconnect() Action

// Shell actions
func StartShell() Action
func SendInput(text string) Action
func SendKey(key Key) Action          // Enter, Tab, Ctrl+C, etc.
func SendLine(text string) Action     // text + Enter
func ResizePTY(cols, rows uint32) Action
func CloseShell() Action

// Exec actions
func Exec(cmd string) Action
func ExecAsync(cmd string) Action     // Don't wait for completion

// SFTP actions
func SFTPUpload(local, remote string) Action
func SFTPDownload(remote, local string) Action
func SFTPMkdir(path string) Action
func SFTPRemove(path string) Action
func SFTPList(path string) Action

// Forwarding actions
func LocalForward(local, remote string) Action
func RemoteForward(remote, local string) Action

// Timing
func Sleep(d time.Duration) Action
func WaitFor(eventID string, attrs ...string) Action
```

#### Built-in Expectations

```go
// Output expectations
func ExpectOutput(contains string) Expectation
func ExpectOutputMatch(regex string) Expectation
func ExpectScreen(contains string) Expectation      // PTY screen capture
func ExpectExitCode(code int) Expectation

// Event expectations
func ExpectEvent(id string, attrs ...string) Expectation
func ExpectNoEvent(id string, attrs ...string) Expectation

// State expectations
func ExpectConnected() Expectation
func ExpectDisconnected() Expectation
func ExpectFileExists(path string) Expectation      // SFTP
func ExpectPortOpen(addr string) Expectation        // Forwarding
```

#### Go DSL Example

```go
func TestShellEcho(t *testing.T) {
    env := sshtest.New(t).
        WithServer(sshtest.ServerWithPassword("user", "pass")).
        WithClient("alice", sshtest.ClientWithPassword("pass")).
        Start()
    defer env.Stop()

    env.Run(&sshtest.Scenario{
        Name: "echo command in shell",
        Steps: []sshtest.Step{
            {
                Client: "alice",
                Actions: []sshtest.Action{
                    sshtest.Connect(),
                    sshtest.StartShell(),
                    sshtest.SendLine("echo hello"),
                },
                Expect: []sshtest.Expectation{
                    sshtest.ExpectOutput("hello"),
                    sshtest.ExpectEvent(sshtest.EventSessionStarted),
                },
            },
        },
    })
}
```

#### YAML DSL Example

```yaml
name: echo command in shell
description: Test that echo works in an interactive shell

steps:
  - client: alice
    actions:
      - connect
      - shell
      - input: "echo hello"
      - key: Enter
      - sleep: 100ms
    expect:
      - output: "hello"
      - event: session.started
```

### 6. Log Capture

Capture and assert on server/client logs.

```go
// LogCapture collects log output for assertions.
type LogCapture struct {
    entries []LogEntry
    mu      sync.RWMutex
}

type LogEntry struct {
    Timestamp time.Time
    Level     string
    Message   string
    Attrs     map[string]any
}

// Assert checks that a log message exists.
func (lc *LogCapture) Assert(contains string) error

// AssertLevel checks for a log at a specific level.
func (lc *LogCapture) AssertLevel(level, contains string) error

// All returns all log entries.
func (lc *LogCapture) All() []LogEntry

// Clear removes all entries.
func (lc *LogCapture) Clear()

// String returns all logs as a formatted string (for debugging).
func (lc *LogCapture) String() string
```

### 7. Deterministic Key Generation

Support reproducible tests without external key files.

```go
// KeyFromSeed generates a deterministic SSH key from a seed string.
// This enables reproducible tests without managing key files.
func KeyFromSeed(seed string) (ssh.Signer, error)

// PublicKeyFromSeed returns just the public key.
func PublicKeyFromSeed(seed string) (ssh.PublicKey, error)

// Example: The seed "alice" always produces the same key pair.
// This is done by using the seed as input to a deterministic PRNG.
```

### 8. Port Management

Automatic port allocation for parallel test safety.

```go
// FindFreePort returns an available port.
// Uses port 0 binding to let the OS assign a free port.
func FindFreePort() (int, error)

// The Environment automatically manages ports when ServerWithPort(0) is used.
```

## Usage Patterns

### Pattern 1: Simple Command Execution

```go
func TestExecCommand(t *testing.T) {
    env := sshtest.New(t).
        WithServer().
        WithClient("test", sshtest.ClientWithKeySeed("test")).
        Start()
    defer env.Stop()

    result, err := env.Client("test").Exec("echo hello")
    require.NoError(t, err)
    assert.Equal(t, "hello\n", result.Stdout)
    assert.Equal(t, 0, result.ExitCode)
}
```

### Pattern 2: Interactive Shell with PTY

```go
func TestInteractiveShell(t *testing.T) {
    env := sshtest.New(t).
        WithServer().
        WithClient("test",
            sshtest.ClientWithKeySeed("test"),
            sshtest.ClientWithPTY(80, 24)).
        Start()
    defer env.Stop()

    sess, err := env.Client("test").Shell()
    require.NoError(t, err)
    defer sess.Close()

    sess.Write([]byte("echo $TERM\n"))
    time.Sleep(100 * time.Millisecond)

    screen := sess.Screen()
    assert.Contains(t, screen, "xterm")
}
```

### Pattern 3: SFTP File Transfer

```go
func TestSFTPUploadDownload(t *testing.T) {
    env := sshtest.New(t).
        WithServer(sshtest.ServerWithSFTP(true)).
        WithClient("test", sshtest.ClientWithKeySeed("test")).
        Start()
    defer env.Stop()

    env.Run(&sshtest.Scenario{
        Name: "upload and download file",
        Steps: []sshtest.Step{
            {
                Client: "test",
                Actions: []sshtest.Action{
                    sshtest.Connect(),
                    sshtest.SFTPUpload("testdata/file.txt", "/tmp/uploaded.txt"),
                    sshtest.SFTPDownload("/tmp/uploaded.txt", "testdata/downloaded.txt"),
                },
                Expect: []sshtest.Expectation{
                    sshtest.ExpectFileExists("/tmp/uploaded.txt"),
                    sshtest.ExpectEvent(sshtest.EventSFTPStarted),
                },
            },
        },
    })
}
```

### Pattern 4: TCP Port Forwarding

```go
func TestLocalPortForward(t *testing.T) {
    // Start a simple HTTP server
    httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Write([]byte("hello from backend"))
    }))
    defer httpServer.Close()

    env := sshtest.New(t).
        WithServer(sshtest.ServerWithTCPForwarding(true)).
        WithClient("test", sshtest.ClientWithKeySeed("test")).
        Start()
    defer env.Stop()

    client := env.Client("test")
    client.Connect()

    // Forward local:8080 -> httpServer
    listener, err := client.LocalForward("127.0.0.1:0", httpServer.Listener.Addr().String())
    require.NoError(t, err)
    defer listener.Close()

    // Connect through the tunnel
    resp, err := http.Get("http://" + listener.Addr().String())
    require.NoError(t, err)
    body, _ := io.ReadAll(resp.Body)
    assert.Equal(t, "hello from backend", string(body))
}
```

### Pattern 5: Multi-Client Scenario

```go
func TestMultipleClients(t *testing.T) {
    env := sshtest.New(t).
        WithServer().
        WithClient("alice", sshtest.ClientWithKeySeed("alice")).
        WithClient("bob", sshtest.ClientWithKeySeed("bob")).
        Start()
    defer env.Stop()

    env.Run(&sshtest.Scenario{
        Name: "two clients connect simultaneously",
        Steps: []sshtest.Step{
            {
                Client: "alice",
                Actions: []sshtest.Action{sshtest.Connect(), sshtest.Exec("echo alice")},
                Expect:  []sshtest.Expectation{sshtest.ExpectOutput("alice")},
            },
            {
                Client: "bob",
                Actions: []sshtest.Action{sshtest.Connect(), sshtest.Exec("echo bob")},
                Expect:  []sshtest.Expectation{sshtest.ExpectOutput("bob")},
            },
        },
    })
}
```

### Pattern 6: Authentication Failure Testing

```go
func TestAuthFailure(t *testing.T) {
    env := sshtest.New(t).
        WithServer(sshtest.ServerWithPassword("user", "correct")).
        WithClient("bad", sshtest.ClientWithPassword("wrong")).
        Start()
    defer env.Stop()

    err := env.Client("bad").Connect()
    assert.Error(t, err)

    event, err := env.Events().WaitTimeout(time.Second, sshtest.EventAuthFailure)
    require.NoError(t, err)
    assert.Equal(t, "bad", event.Attrs["client"])
}
```

### Pattern 7: Real SSH Client/Server Testing

```go
func TestWithRealSSH(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping real SSH test in short mode")
    }

    env := sshtest.New(t).
        WithServer().  // sshd-lite server
        WithClient("real",
            sshtest.ClientWithKeySeed("test"),
            sshtest.ClientReal()).  // Use actual ssh(1) binary
        Start()
    defer env.Stop()

    result, err := env.Client("real").Exec("echo from real ssh")
    require.NoError(t, err)
    assert.Contains(t, result.Stdout, "from real ssh")
}
```

### Pattern 8: YAML-Driven Test Suite

```go
func TestFromYAML(t *testing.T) {
    env := sshtest.New(t).
        WithServer(sshtest.ServerWithSFTP(true), sshtest.ServerWithTCPForwarding(true)).
        WithClient("test", sshtest.ClientWithKeySeed("test")).
        Start()
    defer env.Stop()

    // Load all YAML test files from testdata/
    files, _ := filepath.Glob("testdata/*.yaml")
    for _, f := range files {
        t.Run(filepath.Base(f), func(t *testing.T) {
            data, _ := os.ReadFile(f)
            err := env.RunYAML(string(data))
            require.NoError(t, err)
        })
    }
}
```

## Implementation Roadmap

### Phase 1: Core Infrastructure
- [ ] `Environment` with lifecycle management
- [ ] `ServerLite` - sshd-lite server wrapper
- [ ] `ClientGo` - Go SSH client wrapper
- [ ] `EventBus` - event collection and queries
- [ ] `LogCapture` - log capture and assertions
- [ ] `KeyFromSeed` - deterministic key generation
- [ ] `FindFreePort` - port allocation

### Phase 2: Actions & Expectations
- [ ] Connection actions (Connect, Disconnect)
- [ ] Exec actions (Exec, ExecAsync)
- [ ] Shell actions (StartShell, SendInput, SendLine, SendKey)
- [ ] Basic expectations (ExpectOutput, ExpectExitCode, ExpectEvent)

### Phase 3: Advanced Features
- [ ] PTY support with screen capture
- [ ] SFTP actions and expectations
- [ ] TCP forwarding actions and expectations
- [ ] Resize actions

### Phase 4: DSL & Extensibility
- [ ] Scenario execution engine
- [ ] YAML parser for scenarios
- [ ] Custom action/expectation registration

### Phase 5: Real Client/Server Support
- [ ] `ServerReal` - wrap real sshd(8)
- [ ] `ClientReal` - wrap real ssh(1)
- [ ] Compatibility test suite

## AI-Friendliness Considerations

The design prioritizes AI-friendliness in several ways:

1. **Fluent Builder API** - Natural language-like chaining that's easy to generate:
   ```go
   env := sshtest.New(t).WithServer().WithClient("test").Start()
   ```

2. **Declarative Actions** - Actions describe "what" not "how":
   ```go
   sshtest.SendLine("echo hello")  // Clear intent
   ```

3. **Named Clients** - Explicit client identification:
   ```go
   env.Client("alice").Exec("whoami")
   ```

4. **Structured Events** - Machine-readable event matching:
   ```go
   ExpectEvent("auth.success", "user", "alice")
   ```

5. **YAML DSL** - AI can generate YAML test cases:
   ```yaml
   - client: alice
     actions:
       - connect
       - exec: "echo hello"
     expect:
       - output: "hello"
   ```

6. **Good Defaults** - Minimal boilerplate for common cases:
   ```go
   env := sshtest.New(t).WithServer().WithClient("test").Start()
   result, _ := env.Client("test").Exec("echo hello")
   // That's it! Server with auto-port, client with auto-key
   ```

7. **Descriptive Errors** - Clear failure messages:
   ```
   expectation failed: ExpectOutput("hello")
     got output: "goodbye"
     in step 2 of scenario "echo test"
     client: alice
     after actions: [Connect, Exec("echo goodbye")]
   ```

8. **Composable Primitives** - Actions and expectations compose:
   ```go
   scenario.Steps = append(scenario.Steps, sshtest.Step{
       Client:  "test",
       Actions: []sshtest.Action{sshtest.Connect(), sshtest.Exec("cmd")},
       Expect:  []sshtest.Expectation{sshtest.ExpectExitCode(0)},
   })
   ```

## File Structure

```
sshd/sshtest/
├── harness.go          # Environment, lifecycle
├── server.go           # Server interface, ServerLite, ServerReal
├── client.go           # Client interface, ClientGo, ClientReal
├── event.go            # EventBus, Event
├── log.go              # LogCapture
├── key.go              # KeyFromSeed, key utilities
├── port.go             # FindFreePort
├── action.go           # Action interface, built-in actions
├── expect.go           # Expectation interface, built-in expectations
├── scenario.go         # Scenario, Step, runner
├── yaml.go             # YAML parser
├── pty.go              # PTY wrapper with screen capture
└── testdata/
    ├── echo.yaml       # Example YAML scenario
    └── sftp.yaml       # Example YAML scenario
```

## References

- Inspired by patterns from [multish](https://github.com/jpillora/multish) project
- gRPC's [bufconn](https://pkg.go.dev/google.golang.org/grpc/test/bufconn) for in-memory connections
- [vt10x](https://github.com/hinshun/vt10x) for terminal emulation
