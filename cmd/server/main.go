// Command server is the zv adventure backend.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"log"
	"net/http"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"git.sr.ht/~klahr/zv/internal/adventure"
	"git.sr.ht/~klahr/zv/internal/auth"
	"git.sr.ht/~klahr/zv/internal/db"
	"git.sr.ht/~klahr/zv/internal/logx"
	"git.sr.ht/~klahr/zv/internal/scheduler"
	"git.sr.ht/~klahr/zv/internal/server"
	"git.sr.ht/~klahr/zv/internal/ws"
)

func main() {
	var (
		addr              = flag.String("addr", ":8080", "listen address")
		dbPath            = flag.String("db", "zv.db", "sqlite database path")
		secureCookie      = flag.Bool("secure-cookie", false, "set Secure flag on session cookies (enable behind TLS)")
		logLevel          = flag.String("log-level", "INFO", "initial log level (DEBUG|INFO|WARN|ERROR)")
		seedAdmin         = flag.String("seed-admin", "", "if set, create/update a global admin as username:password")
		seedArranger      = flag.String("seed-arranger", "", "if set, create/update an org_admin arranger as username:password in the default Arrangers org")
		seedDemoAdvFlag   = flag.Bool("seed-demo-adventure", false, "install the demo adventure (puzzles + placeholder images)")
		seedDemoFlag      = flag.Bool("seed-demo", false, "install a full demo dataset and exit")
	)
	flag.Parse()
	logx.SetLevel(logx.ParseLevel(*logLevel))

	sqlDB, err := db.Open(*dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer sqlDB.Close()

	// Seeds run in a fixed order (admin → arranger → adventure → full demo)
	// so `make seed` can combine flags in one invocation. If any seed
	// flag is set the server exits without starting the listener.
	seeded := false
	if *seedAdmin != "" {
		if err := seedAdminUser(sqlDB, *seedAdmin); err != nil {
			log.Fatalf("seed admin: %v", err)
		}
		log.Printf("admin user ready")
		seeded = true
	}
	if *seedArranger != "" {
		if err := seedArrangerUser(sqlDB, *seedArranger); err != nil {
			log.Fatalf("seed arranger: %v", err)
		}
		log.Printf("arranger ready in org Arrangers")
		seeded = true
	}
	if *seedDemoAdvFlag {
		if err := seedDemoAdventure(sqlDB); err != nil {
			log.Fatalf("seed demo adventure: %v", err)
		}
		log.Printf("demo adventure ready")
		seeded = true
	}
	if *seedDemoFlag {
		if err := seedDemo(context.Background(), sqlDB); err != nil {
			log.Fatalf("seed demo: %v", err)
		}
		log.Printf(`demo data installed. Log in at /arrange as admin/admin, boss/s3cret (org_admin) or jane/jane (org_member). Players use per-slot magic links — copy them from Runs → Run #1 → Edit team.`)
		seeded = true
	}
	if seeded {
		return
	}

	lib := adventure.New(sqlDB)
	logx.Info("adventure.library.loaded", "count", len(lib.List()))

	hub := ws.NewHub()
	h := server.New(server.Config{
		DB:           sqlDB,
		Library:      lib,
		Hub:          hub,
		SecureCookie: *secureCookie,
	})

	srv := &http.Server{
		Addr:              *addr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Sweep for scheduled runs every 15s and auto-start any that are due.
	scheduler.Start(ctx, sqlDB, hub, 15*time.Second)

	go func() {
		logx.Info("http.listening", "addr", *addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("serve: %v", err)
		}
	}()

	<-ctx.Done()
	logx.Info("http.shutdown")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logx.Error("http.shutdown.failed", "err", err)
	}
}

// seedAdminUser upserts a global-admin user from a "username:password" flag
// value. Used to bootstrap the very first admin on a fresh database.
func seedAdminUser(d *sql.DB, creds string) error {
	i := strings.IndexByte(creds, ':')
	if i <= 0 || i == len(creds)-1 {
		return errors.New(`expected "username:password"`)
	}
	username, password := creds[:i], creds[i+1:]
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	_, err = d.Exec(`
		INSERT INTO users(username, password_hash, role, org_id) VALUES (?, ?, 'admin', NULL)
		ON CONFLICT(username) DO UPDATE SET password_hash = excluded.password_hash, role = 'admin', org_id = NULL
	`, username, hash)
	return err
}
