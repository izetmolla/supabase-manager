package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/supabase-manager/manager/config"
	"github.com/supabase-manager/manager/internal/auth"
	"github.com/supabase-manager/manager/internal/db"
	"github.com/supabase-manager/manager/internal/docker"
	"github.com/supabase-manager/manager/internal/models"
	"github.com/supabase-manager/manager/internal/selfupdate"
	"golang.org/x/term"
	"gorm.io/gorm"
)

const usage = `Usage:
  supabase-manager                                   Start the server
  supabase-manager user list [--json]                List all users
  supabase-manager user get <id|email> [--json]      Show a single user
  supabase-manager user set-password <id|email> [--password-stdin] [--allow-weak]
                                                     Set a user's password (prompts if not piped)
  supabase-manager user set-email <id|email> <new-email>
                                                     Change a user's email (login name)
  supabase-manager proxy panel-host <domain|ip> [--email <email>] [--no-tls]
                                                     Serve this panel through the Proxy Manager on
                                                     <domain> (HTTPS via Let's Encrypt when an
                                                     instance has an HTTPS port); needs the running
                                                     server, e.g. docker exec supabase-manager ...
  supabase-manager healthcheck                       Exit 0 if the local server is healthy
`

// runCLI handles subcommands. It returns false when no subcommand was given and the server should start.
func runCLI(args []string) bool {
	if len(args) == 0 {
		return false
	}
	var err error
	switch args[0] {
	case "user", "users":
		err = userCmd(args[1:])
	case "healthcheck":
		err = healthcheck()
	case "proxy":
		err = proxyCmd(args[1:])
	case "self-update-apply":
		err = selfUpdateApply(args[1:])
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		err = usageError(fmt.Sprintf("unknown command %q", args[0]))
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		if _, ok := errors.AsType[usageError](err); ok {
			fmt.Fprint(os.Stderr, "\n"+usage)
		}
		os.Exit(1)
	}
	return true
}

// selfUpdateApply runs in the helper container started by the panel's "Update" button and
// re-creates the manager container on the new image. It is not listed in usage.
func selfUpdateApply(args []string) error {
	fs := flag.NewFlagSet("self-update-apply", flag.ContinueOnError)
	container := fs.String("container", "", "container to re-create")
	image := fs.String("image", "", "image to re-create it from")
	if _, err := parseArgs(fs, args); err != nil {
		return err
	}
	if *container == "" || *image == "" {
		return usageError("self-update-apply needs --container and --image")
	}
	socket := os.Getenv("DOCKER_SOCKET")
	if socket == "" {
		socket = "/var/run/docker.sock"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := selfupdate.ApplyInHelper(ctx, docker.New(socket), *container, *image); err != nil {
		return err
	}
	fmt.Printf("updated %s to %s\n", *container, *image)
	return nil
}

// localURL is the base URL of the server running in this container.
func localURL() (string, error) {
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", err
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port), nil
}

// healthcheck probes the local /api/health endpoint; used by the container HEALTHCHECK.
func healthcheck() error {
	base, err := localURL()
	if err != nil {
		return err
	}
	client := http.Client{Timeout: 3 * time.Second}
	res, err := client.Get(base + "/api/health")
	if err != nil {
		return err
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("health endpoint returned %s", res.Status)
	}
	return nil
}

type usageError string

func (e usageError) Error() string { return string(e) }

func userCmd(args []string) error {
	if len(args) == 0 {
		return usageError("missing user subcommand")
	}
	gdb, err := db.Open(config.Load())
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	switch args[0] {
	case "list", "ls":
		return userList(gdb, args[1:])
	case "get", "show":
		return userGet(gdb, args[1:])
	case "set-password", "passwd":
		return userSetPassword(gdb, args[1:])
	case "set-email":
		return userSetEmail(gdb, args[1:])
	default:
		return usageError(fmt.Sprintf("unknown user subcommand %q", args[0]))
	}
}

// parseArgs parses flags that may appear before or after the positional arguments.
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

func findUser(gdb *gorm.DB, ref string) (*models.User, error) {
	var u models.User
	q := gdb
	if id, err := strconv.ParseUint(ref, 10, 64); err == nil {
		q = q.Where("id = ?", id)
	} else {
		q = q.Where("email = ?", strings.ToLower(strings.TrimSpace(ref)))
	}
	res := q.Limit(1).Find(&u)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, fmt.Errorf("user %q not found", ref)
	}
	return &u, nil
}

func printUsers(users []models.User, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(users)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "ID\tEMAIL\tNAME\tROLE\tCREATED")
	for _, u := range users {
		_, _ = fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\n", u.ID, u.Email, u.Name, u.Role, u.CreatedAt.Format(time.DateTime))
	}
	return w.Flush()
}

func userList(gdb *gorm.DB, args []string) error {
	fs := flag.NewFlagSet("user list", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "output JSON")
	if _, err := parseArgs(fs, args); err != nil {
		return err
	}
	var users []models.User
	if err := gdb.Order("id").Find(&users).Error; err != nil {
		return err
	}
	return printUsers(users, *asJSON)
}

func userGet(gdb *gorm.DB, args []string) error {
	fs := flag.NewFlagSet("user get", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "output JSON")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usageError("user get takes exactly one <id|email>")
	}
	u, err := findUser(gdb, pos[0])
	if err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(u)
	}
	return printUsers([]models.User{*u}, false)
}

func readPassword(fromStdin bool) (string, error) {
	fd := int(os.Stdin.Fd())
	if fromStdin || !term.IsTerminal(fd) {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		return strings.TrimRight(line, "\r\n"), nil
	}
	fmt.Fprint(os.Stderr, "New password: ")
	pw, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	fmt.Fprint(os.Stderr, "Confirm password: ")
	confirm, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	if string(pw) != string(confirm) {
		return "", errors.New("passwords do not match")
	}
	return string(pw), nil
}

func userSetPassword(gdb *gorm.DB, args []string) error {
	fs := flag.NewFlagSet("user set-password", flag.ContinueOnError)
	fromStdin := fs.Bool("password-stdin", false, "read the new password from stdin")
	allowWeak := fs.Bool("allow-weak", false, "accept passwords shorter than 8 characters")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usageError("user set-password takes exactly one <id|email>")
	}
	u, err := findUser(gdb, pos[0])
	if err != nil {
		return err
	}
	pw, err := readPassword(*fromStdin)
	if err != nil {
		return err
	}
	switch {
	case pw == "":
		return errors.New("password must not be empty")
	case len(pw) < 8 && !*allowWeak:
		return errors.New("password must be at least 8 characters (use --allow-weak to override)")
	case len(pw) < 8:
		fmt.Fprintln(os.Stderr, "WARNING: setting a password shorter than 8 characters")
	}
	hash, err := auth.HashPassword(pw)
	if err != nil {
		return err
	}
	err = gdb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(u).Update("password_hash", hash).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", u.ID).Delete(&models.RefreshToken{}).Error; err != nil {
			return err
		}
		return tx.Create(&models.AuditLog{Action: "user.update", Target: u.Email, Metadata: `{"source":"cli","password":true}`}).Error
	})
	if err != nil {
		return err
	}
	fmt.Printf("Password updated for %s (id %d); existing sessions were signed out.\n", u.Email, u.ID)
	return nil
}

func userSetEmail(gdb *gorm.DB, args []string) error {
	fs := flag.NewFlagSet("user set-email", flag.ContinueOnError)
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 2 {
		return usageError("user set-email takes <id|email> <new-email>")
	}
	u, err := findUser(gdb, pos[0])
	if err != nil {
		return err
	}
	email := strings.ToLower(strings.TrimSpace(pos[1]))
	if !strings.Contains(email, "@") || len(email) > 255 {
		return errors.New("a valid email is required")
	}
	if existing, err := findUser(gdb, email); err == nil && existing.ID != u.ID {
		return fmt.Errorf("%s is already used by user %d", email, existing.ID)
	}
	old := u.Email
	err = gdb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(u).Update("email", email).Error; err != nil {
			return err
		}
		meta, _ := json.Marshal(map[string]string{"source": "cli", "old_email": old})
		return tx.Create(&models.AuditLog{Action: "user.update", Target: email, Metadata: string(meta)}).Error
	})
	if err != nil {
		return err
	}
	fmt.Printf("Email changed from %s to %s (id %d).\n", old, email, u.ID)
	return nil
}
