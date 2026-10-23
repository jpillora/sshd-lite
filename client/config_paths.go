package client

import (
	"errors"
	"path/filepath"
	"strings"
)

func (r *configReader) expand(p string, value bool) (string, error) {
	if len(p) > 4096 || strings.ContainsAny(p, "\x00\r\n") {
		return "", errors.New("ssh client: invalid configuration value")
	}
	if strings.HasPrefix(p, "~/") {
		if r.o.Sources.Home == "" {
			return "", errors.New("ssh client: home directory is required")
		}
		p = filepath.Join(r.o.Sources.Home, p[2:])
	} else if strings.HasPrefix(p, "~") {
		return "", &UnsupportedError{"~user expansion"}
	}
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		if p[i] != '%' {
			b.WriteByte(p[i])
			continue
		}
		i++
		if i == len(p) {
			return "", errors.New("ssh client: incomplete percent expansion")
		}
		switch p[i] {
		case '%':
			b.WriteByte('%')
		case 'd':
			b.WriteString(r.o.Sources.Home)
		case 'h':
			b.WriteString(r.o.Host)
		case 'r':
			b.WriteString(r.o.User)
		case 'p':
			b.WriteString(r.o.Port)
		case 'u':
			b.WriteString(r.o.Sources.LocalUser)
		default:
			return "", &UnsupportedError{"percent expansion"}
		}
		if b.Len() > 4096 {
			return "", errors.New("ssh client: expanded path limit")
		}
	}
	p = b.String()
	if strings.Contains(p, "${") {
		return "", &UnsupportedError{"environment expansion"}
	}
	if !value && !filepath.IsAbs(p) {
		if !filepath.IsAbs(r.o.Sources.Directory) {
			return "", errors.New("ssh client: absolute source directory is required")
		}
		p = filepath.Join(r.o.Sources.Directory, p)
	}
	return p, nil
}
