package client

import (
	"errors"
	"runtime"
	"strings"
)

// Lexer and bounded host matcher adapted from the preserved Tigo #59 snapshot.
func sshWords(line string) ([]string, error) { return sshLex(line, false) }
func sshLex(line string, include bool) ([]string, error) {
	var out []string
	var b strings.Builder
	quote := byte(0)
	escaped := false
	started := false
	flush := func() {
		if started {
			out = append(out, b.String())
			b.Reset()
			started = false
		}
	}
	for i := 0; i < len(line); i++ {
		c := line[i]
		if escaped {
			if include && (runtime.GOOS == "windows" || strings.ContainsRune("*?[]\\", rune(c))) {
				b.WriteByte('\\')
			}
			b.WriteByte(c)
			started = true
			escaped = false
			continue
		}
		if c == '\\' {
			escaped = true
			started = true
			continue
		}
		if quote != 0 {
			if c == quote {
				quote = 0
			} else {
				b.WriteByte(c)
			}
			continue
		}
		if c == '"' || c == '\'' {
			quote = c
			started = true
			continue
		}
		if c == '#' {
			break
		}
		if c == ' ' || c == '\t' || c == '\r' || (c == '=' && (len(out) == 0 || len(out) == 1 && !started)) {
			flush()
			continue
		}
		if c == 0 {
			return nil, errors.New("invalid byte")
		}
		b.WriteByte(c)
		started = true
	}
	if quote != 0 || escaped {
		return nil, errors.New("unfinished quote")
	}
	flush()
	return out, nil
}

func sshPatternList(patterns []string, host string, work *int) (bool, error) {
	return sshPatterns(patterns, strings.ToLower(host), work, true)
}

func sshPatterns(patterns []string, value string, work *int, foldCase bool) (bool, error) {
	positive := false
	for _, pattern := range patterns {
		negative := strings.HasPrefix(pattern, "!")
		pattern = strings.TrimPrefix(pattern, "!")
		if foldCase {
			pattern = strings.ToLower(pattern)
		}
		matched, err := sshGlob(pattern, value, work)
		if err != nil {
			return false, err
		}
		if matched && negative {
			return false, nil
		}
		if matched {
			positive = true
		}
	}
	return positive, nil
}
func sshGlob(pattern, value string, work *int) (bool, error) {
	p, v, star, retry := 0, 0, -1, 0
	for v < len(value) {
		*work--
		if *work < 0 {
			return false, errors.New("ssh client: matching work limit")
		}
		if p < len(pattern) && (pattern[p] == '?' || pattern[p] == value[v]) {
			p++
			v++
			continue
		}
		if p < len(pattern) && pattern[p] == '*' {
			star = p
			p++
			retry = v
			continue
		}
		if star >= 0 {
			p = star + 1
			retry++
			v = retry
			continue
		}
		return false, nil
	}
	for p < len(pattern) && pattern[p] == '*' {
		p++
	}
	return p == len(pattern), nil
}
