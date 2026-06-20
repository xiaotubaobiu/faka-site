package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"faka-site/internal/auth"
	"faka-site/internal/payment"
	"faka-site/internal/store"
	"faka-site/internal/web"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	dbPath := env("FAKA_DB", "data.db")
	listen := env("FAKA_LISTEN", ":8080")
	secret := []byte(env("SESSION_SECRET", "change-me"))
	secureCookie := env("COOKIE_SECURE", "true") != "false"

	// Payment key files (PEMs / APIv3 key) live in this directory; configured
	// once at startup. Sensitivity stays on disk, never in the config DB.
	payment.SetKeyDir(env("KEYS_DIR", "keys"))

	st, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	if email := os.Getenv("ADMIN_EMAIL"); email != "" {
		if _, err := st.UserByEmail(email); err == store.ErrNotFound {
			hash, _ := auth.HashPassword(env("ADMIN_PASSWORD", ""))
			if _, err := st.CreateUser(email, hash, "admin"); err == nil {
				log.Printf("bootstrap admin created: %s", email)
			}
		}
	}

	srv := web.NewServer(st, secret, secureCookie)
	go startOrderSweeper(st)
	log.Printf("faka-site listening on %s", listen)
	log.Fatal(http.ListenAndServe(listen, srv.Routes()))
}

// startOrderSweeper periodically closes recharge/epay orders that were created
// but never paid: every minute it marks unpaid orders (status 0) older than the
// configured epay_order_timeout (minutes, default 5) as expired (status 2).
// Alipay also auto-closes the trade via timeout_express, so an expired QR can
// no longer be paid after this window.
func startOrderSweeper(st *store.Store) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for range t.C {
		mins := 5
		if v, _ := st.GetConfig(context.Background(), "epay_order_timeout"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				mins = n
			}
		}
		cutoff := time.Now().Add(-time.Duration(mins) * time.Minute)
		if n, err := st.EpayExpireStale(cutoff); err != nil {
			log.Printf("order sweeper: %v", err)
		} else if n > 0 {
			log.Printf("order sweeper: closed %d unpaid epay order(s) older than %dm", n, mins)
		}
		if n, err := st.ExpireStaleRecharges(context.Background(), cutoff.Unix()); err != nil {
			log.Printf("recharge sweeper: %v", err)
		} else if n > 0 {
			log.Printf("recharge sweeper: expired %d unpaid recharge order(s) older than %dm", n, mins)
		}
	}
}
