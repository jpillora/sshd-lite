package scenario

import "time"

// Action spec constructors

func ConnectSpec() ActionSpec {
	return ActionSpec{Type: ActionConnect, Params: map[string]interface{}{}}
}

func DisconnectSpec() ActionSpec {
	return ActionSpec{Type: ActionDisconnect, Params: map[string]interface{}{}}
}

func ShellSpec() ActionSpec {
	return ActionSpec{Type: ActionShell, Params: map[string]interface{}{}}
}

func CloseShellSpec() ActionSpec {
	return ActionSpec{Type: ActionCloseShell, Params: map[string]interface{}{}}
}

func ExecSpec(cmd string) ActionSpec {
	return ActionSpec{Type: ActionExec, Params: map[string]interface{}{"command": cmd}}
}

func InputSpec(text string) ActionSpec {
	return ActionSpec{Type: ActionInput, Params: map[string]interface{}{"text": text}}
}

func LineSpec(text string) ActionSpec {
	return ActionSpec{Type: ActionLine, Params: map[string]interface{}{"text": text}}
}

func KeySpec(key string) ActionSpec {
	return ActionSpec{Type: ActionKey, Params: map[string]interface{}{"key": key}}
}

func SleepSpec(d time.Duration) ActionSpec {
	return ActionSpec{Type: ActionSleep, Params: map[string]interface{}{"duration": d}}
}

func ResizeSpec(cols, rows uint32) ActionSpec {
	return ActionSpec{Type: ActionResize, Params: map[string]interface{}{"cols": int(cols), "rows": int(rows)}}
}

func LocalForwardSpec(local, remote string) ActionSpec {
	return ActionSpec{Type: ActionLocalForward, Params: map[string]interface{}{"local": local, "remote": remote}}
}

func RemoteForwardSpec(remote, local string) ActionSpec {
	return ActionSpec{Type: ActionRemoteForward, Params: map[string]interface{}{"local": local, "remote": remote}}
}

// Expectation spec constructors

func ConnectedSpec() ExpectationSpec {
	return ExpectationSpec{Type: ExpectConnected, Params: map[string]interface{}{}}
}

func DisconnectedSpec() ExpectationSpec {
	return ExpectationSpec{Type: ExpectDisconnected, Params: map[string]interface{}{}}
}

func OutputSpec(contains string) ExpectationSpec {
	return ExpectationSpec{Type: ExpectOutput, Params: map[string]interface{}{"contains": contains}}
}

func OutputMatchSpec(pattern string) ExpectationSpec {
	return ExpectationSpec{Type: ExpectOutputMatch, Params: map[string]interface{}{"pattern": pattern}}
}

func StdoutSpec(contains string) ExpectationSpec {
	return ExpectationSpec{Type: ExpectStdout, Params: map[string]interface{}{"contains": contains}}
}

func StderrSpec(contains string) ExpectationSpec {
	return ExpectationSpec{Type: ExpectStderr, Params: map[string]interface{}{"contains": contains}}
}

func ExitCodeSpec(code int) ExpectationSpec {
	return ExpectationSpec{Type: ExpectExitCode, Params: map[string]interface{}{"code": code}}
}

func ScreenSpec(contains string) ExpectationSpec {
	return ExpectationSpec{Type: ExpectScreen, Params: map[string]interface{}{"contains": contains}}
}

func EventSpec(eventID string, attrs ...string) ExpectationSpec {
	params := map[string]interface{}{"event": eventID}
	if len(attrs) > 0 {
		params["attrs"] = attrs
	}
	return ExpectationSpec{Type: ExpectEvent, Params: params}
}

func NoEventSpec(eventID string, attrs ...string) ExpectationSpec {
	params := map[string]interface{}{"event": eventID}
	if len(attrs) > 0 {
		params["attrs"] = attrs
	}
	return ExpectationSpec{Type: ExpectNoEvent, Params: params}
}

func WaitForOutputSpec(text string, timeout time.Duration) ExpectationSpec {
	return ExpectationSpec{Type: ExpectWaitForOutput, Params: map[string]interface{}{"text": text, "timeout": timeout}}
}
