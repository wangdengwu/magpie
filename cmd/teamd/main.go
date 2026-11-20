// teamd is the team portal: the sidecar beside magpie that lets a team's
// lead manage only that team's accounts, and the commands ops makes teams
// with. What it does, and the boundary it keeps, is in internal/teamd; this
// is only where it starts.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"

	"github.com/yetone/magpie/internal/teamd"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	cmd := "serve"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	switch cmd {
	case "serve":
		return serve(args, stderr)
	case "teams":
		return teams(args, stdout, stderr)
	case "help":
		usage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "teamd: %q is not a command\n\n", cmd)
		usage(stderr)
		return 2
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `teamd — the team portal beside magpie.

  teamd serve [flags]        answer the portal's pages
  teamd teams add <name> --group <id> --key <id>
                             record a team, after checking magpie has the
                             account group and the gateway key
  teamd teams list           the teams, and what each one's group holds
  teamd teams token <team>   issue or rotate a team's link, printed once
  teamd teams rm <team>      forget a team, leaving magpie untouched
                             (--force: even while its group holds accounts)
  teamd help                 this

Every command takes -admin-url, -admin-key-file and -state-dir, or
TEAMD_ADMIN_URL, TEAMD_ADMIN_KEY_FILE and TEAMD_STATE_DIR. The admin key
itself is never an argument or an environment variable, which every process
on the machine can read: give the file it is in.

serve also takes -gateway-url (TEAMD_GATEWAY_URL), the address members
reach magpie at, and -key-file (TEAMD_KEY_FILE), a folder of per-team key
files for the connect page to print. A team's key is mounted at
<folder>/<team-id>.key, one key on one line; with no file for a team, its
page says ops keeps the key elsewhere and prints everything else. Only the
team's own link shows it.

-icon-file (TEAMD_ICON_FILE) is an image drawn beside each page's head:
PNG, JPEG, GIF, WebP, ICO or SVG, at most 64 KiB. -favicon-file
(TEAMD_FAVICON_FILE) is the browser tab's icon, which a browser draws
square; without it the tab shows the -icon-file, which squeezes a wide
wordmark. Both are read once at start and put in the pages themselves;
nothing is fetched. A file that isn't such an image stops the portal at
start.

-supervise runs the portal as a child of this process and starts it again
whenever it ends, waiting 1s, then twice as long each time to 30s. It is
how the sidecar runs: a portal that can't start, or dies, leaves this
process and its container up, so the Pod, and magpie's gateway in it, stay
Ready.

-audit-max (TEAMD_AUDIT_MAX) is how big audit.jsonl may grow before it
rotates: 4 MiB by default, 0 for that default, and -1 to keep every line
forever. One generation is kept beside it, so a page doesn't go blank the
moment the file turns over.
`)
}

// commonFlags are the flags every command takes: which magpie, which admin
// key file, and which state folder.
func commonFlags(name string, stderr io.Writer) (*flag.FlagSet, *teamd.Config) {
	cfg := &teamd.Config{
		AdminURL:     envOr("TEAMD_ADMIN_URL", teamd.DefaultAdminURL),
		AdminKeyFile: os.Getenv("TEAMD_ADMIN_KEY_FILE"),
		Listen:       envOr("TEAMD_LISTEN", teamd.DefaultListen),
		StateDir:     envOr("TEAMD_STATE_DIR", defaultStateDir()),
		GatewayURL:   os.Getenv("TEAMD_GATEWAY_URL"),
		KeyFile:      os.Getenv("TEAMD_KEY_FILE"),
		IconFile:     os.Getenv("TEAMD_ICON_FILE"),
		FaviconFile:  os.Getenv("TEAMD_FAVICON_FILE"),
		AuditMax:     envInt("TEAMD_AUDIT_MAX"),
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&cfg.AdminURL, "admin-url", cfg.AdminURL, "magpie's gateway, over loopback ("+teamd.DefaultAdminURL+")")
	fs.StringVar(&cfg.AdminKeyFile, "admin-key-file", cfg.AdminKeyFile, "the file holding magpie's admin key")
	fs.StringVar(&cfg.StateDir, "state-dir", cfg.StateDir, "where teams.json and audit.jsonl are kept")
	return fs, cfg
}

// parse reads args, with the first bare word moved to the end: a command
// reads as it is documented, `teamd teams add team-a --group X`, and Go's
// flag package stops at the first word that isn't a flag.
func parse(fs *flag.FlagSet, args []string) error {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		rest := make([]string, 0, len(args))
		rest = append(rest, args[1:]...)
		args = append(rest, args[0])
	}
	return fs.Parse(args)
}

func serve(args []string, stderr io.Writer) int {
	fs, cfg := commonFlags("teamd serve", stderr)
	fs.StringVar(&cfg.Listen, "listen", cfg.Listen, "the address teamd answers on ("+teamd.DefaultListen+")")
	fs.Int64Var(&cfg.AuditMax, "audit-max", cfg.AuditMax, "how big audit.jsonl may grow before it rotates, in bytes (0 for the default, -1 for never)")
	fs.StringVar(&cfg.GatewayURL, "gateway-url", cfg.GatewayURL, "the address members reach magpie at, for the connect page")
	fs.StringVar(&cfg.KeyFile, "key-file", cfg.KeyFile, "a folder of per-team key files for the connect page")
	fs.StringVar(&cfg.IconFile, "icon-file", cfg.IconFile, "an image (PNG, SVG, …, at most 64 KiB) drawn beside each page's head")
	fs.StringVar(&cfg.FaviconFile, "favicon-file", cfg.FaviconFile, "a square image for the browser tab's icon (the -icon-file when unset)")
	supervise := fs.Bool("supervise", false, "run the portal as a child and start it again whenever it ends (the sidecar's mode)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "teamd: %q is not a flag\n", fs.Arg(0))
		return 2
	}
	if *supervise {
		return superviseServe(args, stderr)
	}
	srv, err := teamd.New(*cfg)
	if err != nil {
		fmt.Fprintf(stderr, "teamd: %v\n", err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// The key is not in this line, and no line below names it.
	log.Printf("teamd answers on %s, calls %s, keeps its files in %s", cfg.Listen, cfg.AdminURL, cfg.StateDir)
	if err := srv.Serve(ctx); err != nil {
		fmt.Fprintf(stderr, "teamd: %v\n", err)
		return 1
	}
	return 0
}

func teams(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, "teamd: teams needs add, list, token or rm\n")
		return 2
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "add":
		return teamsAdd(rest, stdout, stderr)
	case "list":
		return teamsList(rest, stdout, stderr)
	case "token":
		return teamsToken(rest, stdout, stderr)
	case "rm":
		return teamsRemove(rest, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "teamd: %q is not a teams command (add, list, token, rm)\n", sub)
		return 2
	}
}

func teamsAdd(args []string, stdout, stderr io.Writer) int {
	fs, cfg := commonFlags("teamd teams add", stderr)
	group := fs.String("group", "", "the account group id ops made for this team")
	key := fs.String("key", "", "the gateway key id the team's members use")
	if err := parse(fs, args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprint(stderr, "teamd: teams add takes one name: teamd teams add <name> --group <id> --key <id>\n")
		return 2
	}
	if *group == "" || *key == "" {
		fmt.Fprint(stderr, "teamd: teams add needs --group and --key: the account group and the gateway key ops made in magpie\n")
		return 2
	}
	ops, code := openOps(*cfg, stderr)
	if ops == nil {
		return code
	}
	team, err := ops.Add(context.Background(), teamd.NewTeam{Name: fs.Arg(0), GroupID: *group, KeyID: *key})
	if err != nil {
		fmt.Fprintf(stderr, "teamd: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", team.ID, team.Name, team.GroupID, team.KeyID)
	fmt.Fprintf(stderr, "teamd: give the team its link with `teamd teams token %s`\n", team.ID)
	return 0
}

func teamsList(args []string, stdout, stderr io.Writer) int {
	fs, cfg := commonFlags("teamd teams list", stderr)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	ops, code := openOps(*cfg, stderr)
	if ops == nil {
		return code
	}
	rows, err := ops.List(context.Background())
	if err != nil {
		fmt.Fprintf(stderr, "teamd: %v\n", err)
		return 1
	}
	if len(rows) == 0 {
		fmt.Fprintln(stderr, "teamd: no teams yet: make one with `teamd teams add <name> --group <id> --key <id>`")
		return 0
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tGROUP\tKEY\tTOKEN\tACCOUNTS")
	for _, r := range rows {
		token := "none"
		if r.Team.TokenHash != "" {
			token = "live"
		}
		accounts := "?"
		if r.Known {
			accounts = fmt.Sprint(r.Accounts)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", r.Team.ID, r.Team.Name, r.Team.GroupID, r.Team.KeyID, token, accounts)
	}
	if err := tw.Flush(); err != nil {
		fmt.Fprintf(stderr, "teamd: %v\n", err)
		return 1
	}
	return 0
}

func teamsToken(args []string, stdout, stderr io.Writer) int {
	fs, cfg := commonFlags("teamd teams token", stderr)
	if err := parse(fs, args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprint(stderr, "teamd: teams token takes one team: teamd teams token <team>\n")
		return 2
	}
	ops, code := openOps(*cfg, stderr)
	if ops == nil {
		return code
	}
	token, team, err := ops.Token(fs.Arg(0))
	if err != nil {
		fmt.Fprintf(stderr, "teamd: %v\n", err)
		return 1
	}
	// The token alone on stdout, so it can be piped; where it goes and that
	// this is the only time it is shown, on stderr.
	fmt.Fprintln(stdout, token)
	fmt.Fprintf(stderr, "teamd: team %s's link is /team/<token>. This is the only time it is printed — teamd keeps only its hash, and running this again replaces it, which cuts the old link off.\n", team.ID)
	return 0
}

func teamsRemove(args []string, stdout, stderr io.Writer) int {
	fs, cfg := commonFlags("teamd teams rm", stderr)
	force := fs.Bool("force", false, "forget the team even while its account group still holds accounts")
	if err := parse(fs, args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprint(stderr, "teamd: teams rm takes one team: teamd teams rm <team> [--force]\n")
		return 2
	}
	ops, code := openOps(*cfg, stderr)
	if ops == nil {
		return code
	}
	gone, err := ops.Remove(context.Background(), fs.Arg(0), *force)
	if err != nil {
		fmt.Fprintf(stderr, "teamd: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "forgot team %s (%s), group %s, key %s\n", gone.Team.ID, gone.Team.Name, gone.Team.GroupID, gone.Team.KeyID)
	switch {
	case !gone.Read:
		fmt.Fprintln(stderr, "teamd: magpie's group couldn't be read, so what it still holds wasn't looked at: the accounts, the group and the key are as they were")
	case len(gone.Accounts()) == 0:
		fmt.Fprintf(stderr, "teamd: magpie's account group %s holds no account; the group and the key are untouched\n", gone.Team.GroupID)
	default:
		fmt.Fprintf(stderr, "teamd: magpie's account group %s still holds %d accounts, and the key %s still serves them: nothing in magpie was changed\n", gone.Team.GroupID, len(gone.Accounts()), gone.Team.KeyID)
	}
	return 0
}

// openOps opens what a teams command works on, saying why when it can't.
func openOps(cfg teamd.Config, stderr io.Writer) (*teamd.Ops, int) {
	ops, err := teamd.NewOps(cfg)
	if err != nil {
		fmt.Fprintf(stderr, "teamd: %v\n", err)
		return nil, 1
	}
	return ops, 0
}

func envOr(name, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return fallback
}

// envInt is a whole number from the environment, 0 when it isn't one: a
// misconfigured size is the default size, not a portal that won't start.
func envInt(name string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(os.Getenv(name)), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// defaultStateDir is a folder of teamd's own under the user's config
// folder, for a run outside the container. The Deployment names the volume
// path explicitly.
func defaultStateDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "teamd"
	}
	return filepath.Join(dir, "magpie-teamd")
}
