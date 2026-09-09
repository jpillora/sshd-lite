package scenario

import (
	"context"
	"fmt"
)

// These interfaces remain here for source compatibility. Execution belongs to
// sshtest; this package otherwise describes and validates scenario data.
// Action is something a client does in a test scenario.
type Action interface {
	Execute(ctx context.Context, env interface{}, clientName string) error
	String() string
}

// Expectation is something we verify after actions.
type Expectation interface {
	Check(ctx context.Context, env interface{}, clientName string) error
	String() string
}

// Deprecated: run parsed scenarios through sshtest.Environment.Run.
func (a *ActionSpec) Execute(ctx context.Context, env interface{}, clientName string) error {
	return fmt.Errorf("parsed scenario specs must run through sshtest.Environment.Run")
}

// Deprecated: run parsed scenarios through sshtest.Environment.Run.
func (e *ExpectationSpec) Check(ctx context.Context, env interface{}, clientName string) error {
	return fmt.Errorf("parsed scenario specs must run through sshtest.Environment.Run")
}
