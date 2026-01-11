package sshtest

import (
	"context"
	"fmt"
	"time"

	"github.com/jpillora/sshd-lite/sshd/sshtest/scenario"
)

type Runner struct {
	env     *Environment
	timeout time.Duration
}

func (r *Runner) Run(ctx context.Context, sc *scenario.Scenario) error {
	if sc == nil {
		return &scenario.ScenarioError{Err: ctx.Err()}
	}

	for stepNum, step := range sc.Steps {
		if err := r.runStep(ctx, sc.Name, stepNum, step); err != nil {
			return err
		}
	}

	return nil
}

func (r *Runner) runStep(ctx context.Context, scenarioName string, stepNum int, step scenario.Step) error {
	clientName := step.Client
	if clientName == "" {
		for name := range r.env.clients {
			clientName = name
			break
		}
	}

	for _, action := range step.Actions {
		if err := r.executeAction(ctx, scenarioName, stepNum, clientName, action); err != nil {
			return err
		}
	}

	for _, expect := range step.Expect {
		if err := r.checkExpectation(ctx, scenarioName, stepNum, clientName, expect); err != nil {
			return err
		}
	}

	return nil
}

func (r *Runner) executeAction(ctx context.Context, scenarioName string, stepNum int, clientName string, action scenario.ActionSpec) error {
	actionCtx := ctx
	if r.timeout > 0 {
		var cancel context.CancelFunc
		actionCtx, cancel = context.WithTimeout(ctx, r.timeout)
		defer cancel()
	}

	execAction, err := actionToInterface(action)
	if err != nil {
		return &scenario.ScenarioError{
			Scenario:   scenarioName,
			StepNum:    stepNum,
			ClientName: clientName,
			Action:     string(action.Type),
			Err:        err,
		}
	}

	err = execAction.Execute(actionCtx, r.env, clientName)
	if err != nil {
		return &scenario.ScenarioError{
			Scenario:   scenarioName,
			StepNum:    stepNum,
			ClientName: clientName,
			Action:     execAction.String(),
			Err:        err,
		}
	}
	return nil
}

func (r *Runner) checkExpectation(ctx context.Context, scenarioName string, stepNum int, clientName string, expect scenario.ExpectationSpec) error {
	expectCtx := ctx
	if r.timeout > 0 {
		var cancel context.CancelFunc
		expectCtx, cancel = context.WithTimeout(ctx, r.timeout)
		defer cancel()
	}

	execExpect, err := expectationToInterface(expect)
	if err != nil {
		return &scenario.ScenarioError{
			Scenario:    scenarioName,
			StepNum:     stepNum,
			ClientName:  clientName,
			Expectation: string(expect.Type),
			Err:         err,
		}
	}

	err = execExpect.Check(expectCtx, r.env, clientName)
	if err != nil {
		return &scenario.ScenarioError{
			Scenario:    scenarioName,
			StepNum:     stepNum,
			ClientName:  clientName,
			Expectation: execExpect.String(),
			Err:         err,
		}
	}
	return nil
}

func actionToInterface(as scenario.ActionSpec) (scenario.Action, error) {
	switch as.Type {
	case scenario.ActionConnect:
		return Connect(), nil
	case scenario.ActionDisconnect:
		return Disconnect(), nil
	case scenario.ActionShell:
		return StartShell(), nil
	case scenario.ActionCloseShell:
		return CloseShell(), nil
	case scenario.ActionExec:
		return Exec(as.Command()), nil
	case scenario.ActionInput:
		return SendInput(as.Text()), nil
	case scenario.ActionLine:
		return SendLine(as.Text()), nil
	case scenario.ActionKey:
		key, err := scenario.ParseKey(as.KeyName())
		if err != nil {
			return nil, err
		}
		return SendKey(key), nil
	case scenario.ActionSleep:
		return Sleep(as.Duration()), nil
	case scenario.ActionResize:
		return ResizePTY(as.Cols(), as.Rows()), nil
	case scenario.ActionWaitForEvent:
		return WaitForEventTimeout(as.Timeout(), as.EventID(), as.Attrs()...), nil
	case scenario.ActionLocalForward:
		return LocalForward(as.LocalAddr(), as.RemoteAddr()), nil
	case scenario.ActionRemoteForward:
		return RemoteForward(as.RemoteAddr(), as.LocalAddr()), nil
	default:
		return nil, fmt.Errorf("unknown action type: %s", as.Type)
	}
}

func expectationToInterface(es scenario.ExpectationSpec) (scenario.Expectation, error) {
	switch es.Type {
	case scenario.ExpectConnected:
		return ExpectConnected(), nil
	case scenario.ExpectDisconnected:
		return ExpectDisconnected(), nil
	case scenario.ExpectOutput:
		return ExpectOutput(es.Contains()), nil
	case scenario.ExpectOutputMatch:
		return ExpectOutputMatch(es.Pattern()), nil
	case scenario.ExpectStdout:
		return ExpectStdout(es.Contains()), nil
	case scenario.ExpectStderr:
		return ExpectStderr(es.Contains()), nil
	case scenario.ExpectExitCode:
		return ExpectExitCode(es.Code()), nil
	case scenario.ExpectScreen:
		return ExpectScreen(es.Contains()), nil
	case scenario.ExpectEvent:
		return ExpectEvent(es.EventID(), es.Attrs()...), nil
	case scenario.ExpectNoEvent:
		return ExpectNoEvent(es.EventID(), es.Attrs()...), nil
	case scenario.ExpectWaitForOutput:
		return ExpectWaitForOutput(es.Text(), es.Timeout()), nil
	default:
		return nil, fmt.Errorf("unknown expectation type: %s", es.Type)
	}
}

type ScenarioBuilder struct {
	scenario *scenario.Scenario
	current  *scenario.Step
}

func NewScenario(name string) *ScenarioBuilder {
	return &ScenarioBuilder{
		scenario: &scenario.Scenario{Name: name},
	}
}

func (b *ScenarioBuilder) Description(desc string) *ScenarioBuilder {
	b.scenario.Description = desc
	return b
}

func (b *ScenarioBuilder) Step(clientName string) *ScenarioBuilder {
	if b.current != nil {
		b.scenario.Steps = append(b.scenario.Steps, *b.current)
	}
	b.current = &scenario.Step{Client: clientName}
	return b
}

func (b *ScenarioBuilder) Do(actions ...scenario.ActionSpec) *ScenarioBuilder {
	if b.current == nil {
		b.current = &scenario.Step{}
	}
	b.current.Actions = append(b.current.Actions, actions...)
	return b
}

func (b *ScenarioBuilder) Then(expectations ...scenario.ExpectationSpec) *ScenarioBuilder {
	if b.current == nil {
		b.current = &scenario.Step{}
	}
	b.current.Expect = append(b.current.Expect, expectations...)
	return b
}

func (b *ScenarioBuilder) Build() *scenario.Scenario {
	if b.current != nil {
		b.scenario.Steps = append(b.scenario.Steps, *b.current)
	}
	return b.scenario
}
