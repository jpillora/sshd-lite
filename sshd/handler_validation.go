package sshd

import "github.com/jpillora/sshd-lite/xssh"

// validateHandlerConflicts keeps sshd's fixed global/channel/session/subsystem
// category order while delegating all per-connection built-in knowledge to
// xssh. The session channel is the one additional handler installed by sshd.
func validateHandlerConflicts(c Config) error {
	if err := xssh.ValidateConfig(&xssh.Config{
		RemoteForwarding:      c.TCPForwarding,
		GlobalRequestHandlers: c.GlobalRequestHandlers,
	}); err != nil {
		return err
	}
	if _, exists := c.ChannelHandlers[xssh.SessionChannelType]; exists {
		return &xssh.HandlerConflictError{Category: "channel", Name: xssh.SessionChannelType}
	}
	return xssh.ValidateConfig(&xssh.Config{
		Session:                true,
		SFTP:                   c.SFTP,
		LocalForwarding:        c.TCPForwarding,
		ChannelHandlers:        c.ChannelHandlers,
		SessionRequestHandlers: c.SessionRequestHandlers,
		SubsystemHandlers:      c.SubsystemHandlers,
	})
}
