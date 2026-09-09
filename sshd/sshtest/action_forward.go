package sshtest

import (
	"context"
	"fmt"
)

// localForwardAction creates a local port forward.
type localForwardAction struct {
	localAddr  string
	remoteAddr string
}

// LocalForward returns an action that creates a local port forward.
func LocalForward(localAddr, remoteAddr string) Action {
	return actionAdapter{&localForwardAction{localAddr: localAddr, remoteAddr: remoteAddr}}
}

func (a *localForwardAction) execute(ctx context.Context, env *Environment, clientName string) error {
	e := env
	client := e.clientByName(clientName)
	if client == nil {
		return fmt.Errorf("client %q not found", clientName)
	}
	listener, err := client.LocalForward(a.localAddr, a.remoteAddr)
	if err != nil {
		return err
	}
	// Store listener for cleanup
	if !e.storeForwardListener(clientName, listener) {
		_ = listener.Close()
		return fmt.Errorf("environment stopped while creating local forward for client %q", clientName)
	}
	return nil
}

func (a *localForwardAction) String() string {
	return fmt.Sprintf("LocalForward(%q, %q)", a.localAddr, a.remoteAddr)
}

// remoteForwardAction creates a remote port forward.
type remoteForwardAction struct {
	remoteAddr string
	localAddr  string
}

// RemoteForward returns an action that creates a remote port forward.
func RemoteForward(remoteAddr, localAddr string) Action {
	return actionAdapter{&remoteForwardAction{remoteAddr: remoteAddr, localAddr: localAddr}}
}

func (a *remoteForwardAction) execute(ctx context.Context, env *Environment, clientName string) error {
	e := env
	client := e.clientByName(clientName)
	if client == nil {
		return fmt.Errorf("client %q not found", clientName)
	}
	return client.RemoteForward(a.remoteAddr, a.localAddr)
}

func (a *remoteForwardAction) String() string {
	return fmt.Sprintf("RemoteForward(%q, %q)", a.remoteAddr, a.localAddr)
}
