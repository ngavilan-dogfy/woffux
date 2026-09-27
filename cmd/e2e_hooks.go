//go:build e2e

package cmd

// The e2e build (make e2e) records the setup (scripts/record-setup.sh)
// against a fake Woffu. It is never part of a release:
//
//   WOFFUX_E2E_WOFFU=<url>   every sign-in, and every company, goes there
//   WOFFUX_E2E_GITHUB=<url>  release checks go there
//
// The keychain is kept in memory, this Mac's signer is never really
// installed and no browser is opened, so a recording can't touch the
// machine it runs on.

import (
	"fmt"
	"os"

	"github.com/zalando/go-keyring"

	"github.com/ngavilan-dogfy/woffux/internal/selfupdate"
)

func init() {
	if u := os.Getenv("WOFFUX_E2E_WOFFU"); u != "" {
		woffuAPI = u
		companyAddress = func(string) string { return u }
	}
	if u := os.Getenv("WOFFUX_E2E_GITHUB"); u != "" {
		selfupdate.APIBase, selfupdate.WebBase = u, u
	}
	keyring.MockInit()
	installAgent = func() error { return nil }
	openURL = func(url string) {
		if log := os.Getenv("WOFFUX_E2E_LOG"); log != "" {
			if f, err := os.OpenFile(log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
				fmt.Fprintln(f, "open", url)
				f.Close()
			}
		}
	}
}
