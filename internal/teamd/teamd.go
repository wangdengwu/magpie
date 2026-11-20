// Package teamd is the team portal: a small service beside magpie that
// lets a team's lead manage only that team's accounts, in the account
// group ops made for it.
//
// The boundary this package promises is its own, not magpie's. magpie's
// management API is global — POST /v1/magpie/admin/login/off takes any
// agent and user, and POST /v1/magpie/admin/account-groups/save takes any
// group id — so a team lead holding a key that could call it would hold
// every team. Authorize is that boundary: it turns a team token into the
// one team it names, reads that team's group from magpie as it is now,
// and refuses any account or group the group doesn't hold. The admin
// client's writes take the Grant only Authorize answers, so a write that
// never went through it cannot be made.
package teamd

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// DefaultAdminURL is magpie's gateway on this computer. The admin key
// teamd holds is AdminLocal, which magpie accepts over loopback alone, so
// this must name this computer.
const DefaultAdminURL = "http://127.0.0.1:3425"

// DefaultListen is where teamd answers. The portal is reached through
// Istio, which routes /team to it; nothing else in the Pod talks to it.
const DefaultListen = "127.0.0.1:3440"

// Config is what teamd is started with.
type Config struct {
	// AdminURL is magpie's gateway, over loopback, without a trailing
	// slash: http://127.0.0.1:3425.
	AdminURL string
	// AdminKeyFile is the file holding the admin key. It is read once at
	// start and never taken from an argument or an environment variable,
	// both of which other processes on the machine can read.
	AdminKeyFile string
	// Listen is the address teamd answers on, host:port.
	Listen string
	// StateDir is where teamd keeps its own files: teams.json,
	// audit.jsonl and their lock.
	StateDir string
	// GatewayURL is the address members reach magpie's gateway at, with no
	// trailing slash: https://gateway.example.com. It is named rather
	// than read from a request, because the portal and the gateway are
	// reached at two different names through the same proxy.
	GatewayURL string
	// AuditMax is how big audit.jsonl may grow before it rotates, in bytes;
	// 0 for DefaultAuditMax and a negative for never. The file lives on the
	// portal's volume, and a portal outlives its volume's free space.
	AuditMax int64
	// KeyFile is the folder a team's gateway key may be mounted in for the connect
	// page to print, "" for the default under StateDir. One file per team,
	// named for its id; no file is not an error: ops may keep the key out of
	// teamd on purpose.
	KeyFile string
	// IconFile is an image drawn beside each page's head, "" for none
	// (brand.go).
	IconFile string
	// FaviconFile is the browser tab's icon, which a browser draws square:
	// "" draws IconFile there, which suits a square icon and squeezes a
	// wordmark.
	FaviconFile string
}

// Validate says what is missing or unusable in c, before anything is
// opened. A problem with the admin address or key is refused here rather
// than on the first request, so a misconfigured sidecar fails at start.
func (c Config) Validate() error {
	if _, err := loopbackBase(c.AdminURL); err != nil {
		return err
	}
	if strings.TrimSpace(c.AdminKeyFile) == "" {
		return errors.New("teamd needs the file holding magpie's admin key (-admin-key-file)")
	}
	if _, _, err := net.SplitHostPort(c.Listen); err != nil {
		return fmt.Errorf("teamd's listen address %q is not host:port: %w", c.Listen, err)
	}
	if strings.TrimSpace(c.StateDir) == "" {
		return errors.New("teamd needs a state folder (-state-dir)")
	}
	return nil
}

// loopbackBase is raw as a base URL, without its trailing slash. A URL
// that names another machine is refused: the key is AdminLocal, which
// magpie answers over loopback alone, and a key that would travel further
// is not one this service is built to hold.
func loopbackBase(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("teamd needs magpie's address (-admin-url)")
	}
	if !strings.Contains(raw, "://") {
		return "", fmt.Errorf("magpie's address %q is not an http:// or https:// URL", raw)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("magpie's address %q doesn't read: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("magpie's address %q is not an http:// or https:// URL", raw)
	}
	if u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return "", fmt.Errorf("magpie's address %q must be a plain host, with no query, fragment or sign-in", raw)
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
		return "", fmt.Errorf("magpie's address %q is not this computer: teamd holds a key magpie answers over loopback alone", raw)
	}
	return u.Scheme + "://" + u.Host + strings.TrimRight(u.Path, "/"), nil
}
