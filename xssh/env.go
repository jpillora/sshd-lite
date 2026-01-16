package xssh

import "strings"

func appendEnv(env []string, kv string) []string {
	p := strings.SplitN(kv, "=", 2)
	k := p[0] + "="
	for i, e := range env {
		if strings.HasPrefix(e, k) {
			env[i] = kv
			return env
		}
	}
	return append(env, kv)
}

func hasEnv(env []string, key string) bool {
	k := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, k) {
			return true
		}
	}
	return false
}
