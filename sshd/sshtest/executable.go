package sshtest

import (
	"context"
	"fmt"

	"github.com/jpillora/sshd-lite/sshd/sshtest/scenario"
)

type Action = scenario.Action
type Expectation = scenario.Expectation

type executableAction interface {
	execute(context.Context, *Environment, string) error
	String() string
}
type executableExpectation interface {
	check(context.Context, *Environment, string) error
	String() string
}
type actionAdapter struct{ executableAction }
type expectationAdapter struct{ executableExpectation }

func (a actionAdapter) Execute(ctx context.Context, raw interface{}, name string) error {
	env, ok := raw.(*Environment)
	if !ok || env == nil {
		return fmt.Errorf("invalid test environment %T", raw)
	}
	return a.execute(ctx, env, name)
}
func (e expectationAdapter) Check(ctx context.Context, raw interface{}, name string) error {
	env, ok := raw.(*Environment)
	if !ok || env == nil {
		return fmt.Errorf("invalid test environment %T", raw)
	}
	return e.check(ctx, env, name)
}
