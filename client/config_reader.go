package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type configReader struct {
	ctx                                context.Context
	o                                  DialOptions
	scalars                            map[string][]string
	identities, sendEnv                []string
	bytes, files, lines, work, entries int
	active                             bool
}

func (r *configReader) read(name, base string, depth int) error {
	if depth > 16 || r.files >= 256 {
		return errors.New("ssh client: configuration include limit")
	}
	r.files++
	b, err := readSource(r.ctx, name, 1<<20)
	if err != nil {
		return fmt.Errorf("ssh client: read config: %w", err)
	}
	r.bytes += len(b)
	if r.bytes > 4<<20 {
		return errors.New("ssh client: configuration byte limit")
	}
	for _, line := range strings.Split(string(b), "\n") {
		if err := r.ctx.Err(); err != nil {
			return err
		}
		r.lines++
		if r.lines > 65536 || len(line) > 64<<10 {
			return errors.New("ssh client: configuration line limit")
		}
		w, err := sshWords(line)
		if err != nil {
			return err
		}
		if len(w) == 0 {
			continue
		}
		if len(w) < 2 {
			return errors.New("ssh client: missing directive value")
		}
		k := strings.ToLower(w[0])
		v := w[1:]
		if k == "host" {
			r.active, err = sshPatternList(v, r.o.Host, &r.work)
			if err != nil {
				return err
			}
			continue
		}
		if k == "match" {
			return &UnsupportedError{"Match"}
		}
		if !r.active {
			continue
		}
		if k == "include" {
			for _, p := range v {
				if err := r.include(p, base, depth+1); err != nil {
					return err
				}
			}
			continue
		}
		if k == "identityfile" {
			if len(v) != 1 {
				return errors.New("ssh client: invalid IdentityFile")
			}
			if len(r.identities) >= 64 {
				return errors.New("ssh client: identity count limit")
			}
			r.identities = append(r.identities, v[0])
			continue
		}
		if k == "sendenv" {
			if len(r.sendEnv)+len(v) > 128 {
				return errors.New("ssh client: SendEnv limit")
			}
			r.sendEnv = append(r.sendEnv, v...)
			continue
		}
		if _, ok := r.scalars[k]; !ok {
			if len(r.scalars) >= 256 {
				return errors.New("ssh client: directive count limit")
			}
			r.scalars[k] = v
		}
	}
	return nil
}

func (r *configReader) include(pattern, base string, depth int) error {
	if !filepath.IsAbs(pattern) && !strings.HasPrefix(pattern, "~") {
		pattern = filepath.Join(base, pattern)
	}
	p, err := r.expand(pattern, false)
	if err != nil {
		return err
	}
	if strings.ContainsAny(filepath.Dir(p), "*?[") {
		return &UnsupportedError{"Include directory wildcard"}
	}
	if !strings.ContainsAny(filepath.Base(p), "*?[") {
		return r.read(p, base, depth)
	}
	d, err := os.Open(filepath.Dir(p))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer d.Close()
	var matches []string
	for {
		batch, e := d.ReadDir(64)
		for _, entry := range batch {
			r.entries++
			if r.entries > 4096 {
				return errors.New("ssh client: Include directory entry limit")
			}
			yes, err := filepath.Match(filepath.Base(p), entry.Name())
			if err != nil {
				return err
			}
			if yes && !entry.IsDir() {
				matches = append(matches, entry.Name())
			}
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
		if err := r.ctx.Err(); err != nil {
			return err
		}
	}
	sort.Strings(matches)
	for _, name := range matches {
		if err := r.read(filepath.Join(filepath.Dir(p), name), base, depth); err != nil {
			return err
		}
	}
	return nil
}
