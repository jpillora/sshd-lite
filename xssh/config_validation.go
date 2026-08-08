package xssh

import "fmt"

// HandlerConflictError reports a custom handler whose name is reserved by an
// enabled built-in handler.
type HandlerConflictError struct {
	Category string
	Name     string
}

func (e *HandlerConflictError) Error() string {
	return fmt.Sprintf("invalid configuration: custom %s handler %q conflicts with built-in handler", e.Category, e.Name)
}

// ValidateConfig checks that enabled built-in handlers do not collide with
// custom handlers. A nil Config is valid. Checks use a fixed category and name
// order, and map presence is a conflict even when the mapped handler is nil.
func ValidateConfig(config *Config) error {
	if config == nil {
		return nil
	}

	if config.RemoteForwarding {
		for _, name := range [...]string{
			TCPIPForwardRequestType,
			CancelTCPIPForwardRequestType,
		} {
			if _, exists := config.GlobalRequestHandlers[name]; exists {
				return &HandlerConflictError{Category: "global request", Name: name}
			}
		}
	}

	if config.LocalForwarding {
		if _, exists := config.ChannelHandlers[DirectTCPIPChannelType]; exists {
			return &HandlerConflictError{Category: "channel", Name: DirectTCPIPChannelType}
		}
	}

	if config.Session {
		for _, name := range [...]string{
			PTYRequestType,
			WindowChangeRequestType,
			EnvRequestType,
			ShellRequestType,
			ExecRequestType,
		} {
			if _, exists := config.SessionRequestHandlers[name]; exists {
				return &HandlerConflictError{Category: "session request", Name: name}
			}
		}
	}

	if config.SFTP {
		if _, exists := config.SubsystemHandlers[SFTPSubsystem]; exists {
			return &HandlerConflictError{Category: "subsystem", Name: SFTPSubsystem}
		}
	}
	return nil
}
