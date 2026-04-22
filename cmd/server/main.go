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
	"git.sr.ht/~klahr/zv/internal/server"
)

func main() {
	var (
		addr         = flag.String("addr", ":8080", "listen address")
		dbPath       = flag.String("db", "zv.db", "sqlite database path")
		advDir       = flag.String("adventures", "adventures", "adventures directory")
		secureCookie = flag.Bool("secure-cookie", false, "set Secure flag on session cookies (enable behind TLS)")
		seedArranger = flag.String("seed-arranger", "", "if set, create/update an arranger user as username:password and exit")
		seedRoot     = flag.String("seed-root", "", "if set, create/update a root user as username:password and exit")
		seedDemoFlag = flag.Bool("seed-demo", false, "install a full demo dataset (root, arranger, teams, players, pings, events) and exit")
	)
	flag.Parse()

	sqlDB, err := db.Open(*dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer sqlDB.Close()

	if *seedArranger != "" {
		if err := seedUser(sqlDB, *seedArranger, auth.RoleArranger); err != nil {
			log.Fatalf("seed: %v", err)
		}
		log.Printf("arranger user ready")
		return
	}
	if *seedRoot != "" {
		if err := seedUser(sqlDB, *seedRoot, auth.RoleRoot); err != nil {
			log.Fatalf("seed: %v", err)
		}
		log.Printf("root user ready")
		return
	}
	if *seedDemoFlag {
		if err := seedDemo(context.Background(), sqlDB, *advDir); err != nil {
			log.Fatalf("seed demo: %v", err)
		}
		log.Printf(`demo data installed. Log in at /arrange as boss/s3cret (arranger) or owner/owner (root). Players: alice..ingrid with password "pw".`)
		return
	}

	lib, err := adventure.Load(*advDir)
	if err != nil {
		log.Fatalf("load adventures: %v", err)
	}
	log.Printf("loaded %d adventure(s) from %s", len(lib.List()), *advDir)

	h := server.New(server.Config{
		DB:           sqlDB,
		Library:      lib,
		SecureCookie: *secureCookie,
	})

	srv := &http.Server{
		Addr:              *addr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("listening on %s", *addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("serve: %v", err)
		}
	}()

	<-ctx.Done()
	log.Printf("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}

// seedUser upserts a user from a "username:password" flag value with a
// fixed role. Intended only for bootstrapping the very first admins.
func seedUser(d *sql.DB, creds, role string) error {
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
		INSERT INTO users(username, password_hash, role) VALUES (?, ?, ?)
		ON CONFLICT(username) DO UPDATE SET password_hash = excluded.password_hash, role = excluded.role
	`, username, hash, role)
	return err
}
