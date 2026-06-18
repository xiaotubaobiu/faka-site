package main

import (
	"log"
	"net/http"
	"os"

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
	log.Printf("faka-site listening on %s", listen)
	log.Fatal(http.ListenAndServe(listen, srv.Routes()))
}
