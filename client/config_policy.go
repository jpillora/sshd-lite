package client

import (
	"errors"
	"strconv"
	"strings"
)

// UnsupportedError names an SSH policy that cannot be honored, without
// including its potentially secret value.
type UnsupportedError struct{ Directive string }

func (e *UnsupportedError) Error() string {
	return "ssh client: unsupported SSH directive: " + e.Directive
}

func (r *configReader) admit() error {
	scalar := map[string]bool{"hostname": true, "user": true, "port": true, "identityagent": true, "hostkeyalias": true, "connecttimeout": true, "stricthostkeychecking": true}
	bools := map[string]bool{"identitiesonly": true, "batchmode": true, "hashknownhosts": true, "pubkeyauthentication": true, "passwordauthentication": true, "gssapiauthentication": true}
	inert := map[string]string{"proxycommand": "none", "proxyjump": "none", "forwardagent": "no", "forwardx11": "no", "forwardx11trusted": "no", "requesttty": "no", "canonicalizehostname": "no", "updatehostkeys": "no", "checkhostip": "no", "verifyhostkeydns": "no", "controlmaster": "no", "controlpath": "none", "serveraliveinterval": "0", "addkeystoagent": "no", "permitlocalcommand": "no", "kbdinteractiveauthentication": "no", "challengeresponseauthentication": "no", "gssapidelegatecredentials": "no", "gssapikeyexchange": "no", "connectionattempts": "1", "addressfamily": "any"}
	for k, v := range r.scalars {
		if k == "userknownhostsfile" || k == "globalknownhostsfile" {
			continue
		}
		if (k == "proxycommand" || k == "proxyjump") && (len(v) != 1 || v[0] != "none") {
			return &UnsupportedError{k}
		}
		if len(v) != 1 {
			return errors.New("ssh client: invalid scalar directive")
		}
		if want, ok := inert[k]; ok {
			if strings.ToLower(v[0]) != want {
				return &UnsupportedError{k}
			}
			continue
		}
		if bools[k] {
			v[0] = strings.ToLower(v[0])
			if v[0] != "yes" && v[0] != "no" {
				return errors.New("ssh client: invalid boolean directive")
			}
			continue
		}
		if !scalar[k] {
			return &UnsupportedError{k}
		}
		if k == "stricthostkeychecking" {
			v[0] = strings.ToLower(v[0])
			if v[0] != "ask" && v[0] != "yes" {
				return &UnsupportedError{k}
			}
		}
		if k == "connecttimeout" {
			n, e := strconv.Atoi(v[0])
			if e != nil || n < 0 || n > 300 {
				return errors.New("ssh client: ConnectTimeout must be 0..300 seconds")
			}
		}
	}
	for _, p := range r.sendEnv {
		p = strings.TrimPrefix(p, "-")
		if p != "LANG" && p != "LC_*" && !strings.HasPrefix(p, "LC_") {
			return &UnsupportedError{"SendEnv"}
		}
	}
	return nil
}
