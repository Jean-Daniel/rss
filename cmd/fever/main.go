// Command fever is the Fever v2 RSS reader: web server, database migrations,
// the importer for the old Fever data, and password management.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/term"

	"fever/internal/auth"
	"fever/internal/config"
	"fever/internal/db"
	"fever/internal/feeds"
	"fever/internal/importer"
	"fever/internal/server"
	"fever/internal/store"
	"fever/web"
)

const usage = `Usage: fever <command> [options]

Commands:
  serve          run the web server (applies database migrations first)
  migrate        apply database migrations and exit
  set-password   set the login password (reads it from the terminal or stdin)
  import-fever   copy feeds, groups, items and favicons from the old Fever tables
  healthcheck    exit 0 if the local server answers /healthz (for the container)

Configuration comes from environment variables: DATABASE_URL, LISTEN_ADDR,
PUBLIC_ORIGIN, TRUST_PROXY, DB_MAX_CONNS, REFRESH_INTERVAL,
FETCH_ALLOW_PRIVATE. See README.md.
`

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "serve":
		err = runServe(ctx, log)
	case "migrate":
		err = withDB(ctx, log, 1, func(*pgxpool.Pool) error { return nil })
	case "set-password":
		err = runSetPassword(ctx, log)
	case "import-fever":
		err = runImport(ctx, log, args)
	case "healthcheck":
		err = runHealthcheck()
	case "help", "-h", "--help":
		fmt.Print(usage)
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}
}

// withDB opens the database with at least minConns connections, applies
// migrations, and runs fn.
func withDB(ctx context.Context, log *slog.Logger, minConns int32, fn func(*pgxpool.Pool) error) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	pool, err := db.Open(ctx, cfg.DatabaseURL, max(cfg.DBMaxConns, minConns))
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool, log); err != nil {
		return err
	}
	return fn(pool)
}

func runServe(ctx context.Context, log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	pool, err := db.Open(ctx, cfg.DatabaseURL, cfg.DBMaxConns)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool, log); err != nil {
		return err
	}

	refresher := feeds.NewRefresher(pool, log, feeds.Options{
		Interval:     cfg.RefreshInterval,
		AllowPrivate: cfg.FetchAllowPrivate,
	})
	go refresher.Run(ctx)

	srv := server.New(cfg, pool, log, refresher, web.Files())
	go srv.SweepSessions(ctx)

	httpServer := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errc := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.ListenAddr, "frontend", web.Files() != nil)
		errc <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return httpServer.Shutdown(shutdownCtx)
}

func runSetPassword(ctx context.Context, log *slog.Logger) error {
	password, err := readPassword()
	if err != nil {
		return err
	}
	return withDB(ctx, log, 1, func(pool *pgxpool.Pool) error {
		hash, err := auth.HashPassword(password)
		if err != nil {
			return err
		}
		// All or nothing: a new password that left the old sessions open
		// would not lock anyone out.
		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		q := store.New(tx)
		if err := q.SetPasswordHash(ctx, &hash); err != nil {
			return err
		}
		// A new password signs every device out and forgets them, so they
		// are subject to the global login limit until they log in again.
		if err := q.DeleteAllSessions(ctx); err != nil {
			return err
		}
		if err := q.DeleteAllDevices(ctx); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		fmt.Println("Password updated. All devices have been signed out.")
		return nil
	})
}

func readPassword() (string, error) {
	const minLen = 8
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		// Piped input: the first line is the password.
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			return "", errors.New("no password given on stdin")
		}
		p := strings.TrimRight(line, "\r\n")
		if len(p) < minLen {
			return "", fmt.Errorf("the password must have at least %d characters", minLen)
		}
		return p, nil
	}
	fmt.Fprint(os.Stderr, "New password: ")
	p1, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	if len(p1) < minLen {
		return "", fmt.Errorf("the password must have at least %d characters", minLen)
	}
	fmt.Fprint(os.Stderr, "Repeat it: ")
	p2, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	if string(p1) != string(p2) {
		return "", errors.New("the passwords do not match")
	}
	return string(p1), nil
}

func runImport(ctx context.Context, log *slog.Logger, args []string) error {
	fs := flag.NewFlagSet("import-fever", flag.ContinueOnError)
	schema := fs.String("schema", "public", "schema holding the Fever tables")
	prefix := fs.String("prefix", "fever_", "Fever table prefix")
	replace := fs.Bool("replace", false, "replace feeds, groups, items and favicons already in v2")
	if err := fs.Parse(args); err != nil {
		return err
	}
	// The import reads and writes at the same time: it needs 2 connections.
	return withDB(ctx, log, 2, func(pool *pgxpool.Pool) error {
		start := time.Now()
		st, err := importer.Run(ctx, pool, log, importer.Options{SourceSchema: *schema, Prefix: *prefix, Replace: *replace})
		if err != nil {
			return err
		}
		log.Info("import finished",
			"groups", st.Groups, "feeds", st.Feeds, "memberships", st.Memberships,
			"items", st.Items, "favicons", st.Favicons, "skipped_favicons", st.SkippedFavicons,
			"took", time.Since(start).Round(time.Millisecond))
		return nil
	})
}

func runHealthcheck() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	host, port, err := net.SplitHostPort(cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("LISTEN_ADDR: %w", err)
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	client := http.Client{Timeout: 3 * time.Second}
	res, err := client.Get("http://" + net.JoinHostPort(host, port) + "/healthz")
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz returned %s", res.Status)
	}
	return nil
}
