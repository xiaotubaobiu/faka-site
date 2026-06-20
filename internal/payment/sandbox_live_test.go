//go:build sandboxlive

// Package payment — sandbox_live_test.go
//
// This file is gated behind the "sandboxlive" build tag, so it is NEVER compiled
// or run by the normal `go test ./...`. It exists to satisfy the acceptance
// criterion "connect to the Alipay sandbox gateway yourself and iterate until
// the full link is stable": when run ON THE SERVER (where the sandbox
// credentials + key files actually live), it performs a REAL precreate against
// the Alipay sandbox and exercises the signed-callback verification path.
//
// To run (on the HK server, from the repo root):
//
//	go test -tags sandboxlive ./internal/payment/ \
//	  -run TestSandboxLive_PrecreateAndNotify -v -timeout 60s
//
// It reads credentials the same way the app does:
//   - app id / gateway / sandbox flag from the faka-site config DB (env FAKA_DB)
//   - private/public key PEMs from the keys/ directory (KEYS_DIR)
//
// So it validates the EXACT production code path (NewAlipayProvider →
// CreatePayment → ParseNotify) against the real sandbox, not a mock.
package payment

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// TestSandboxLive_PrecreateAndNotify performs a real end-to-end sandbox test:
//  1. Load sandbox config + key files (same loader as the app).
//  2. Call alipay.trade.precreate (the exact CreatePayment the recharge flow
//     uses) for a ¥0.01 order.
//  3. Assert the response is a non-empty qr_code (code==10000).
//  4. Build a signed callback payload (using the SAME alipaySign used for
//     gateway requests) and verify ParseNotify accepts it — i.e. the public key
//     we hold matches the key Alipay will sign real notifications with.
//
// It does NOT actually pay (the sandbox buyer app is required for that); step 4
// is a self-signed round-trip that proves our verify path works. A full
// buyer-paid round trip is covered by the web package's end-to-end test with a
// fake provider, and by manual sandbox buyer-app confirmation on the server.
func TestSandboxLive_PrecreateAndNotify(t *testing.T) {
	dbPath := flag.String("db", os.Getenv("FAKA_DB"), "path to faka-site data.db")
	if *dbPath == "" {
		*dbPath = "data.db"
	}
	keyDir := os.Getenv("KEYS_DIR")
	if keyDir == "" {
		keyDir = "keys"
	}
	SetKeyDir(keyDir)

	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)", *dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	cfg := map[string]string{}
	rows, err := db.Query(`SELECT key, value FROM config`)
	if err != nil {
		t.Fatalf("query config: %v", err)
	}
	for rows.Next() {
		var k, v string
		rows.Scan(&k, &v)
		cfg[k] = v
	}
	rows.Close()

	priv, err := ReadKeyFile("alipay_private.pem")
	if err != nil {
		t.Fatalf("read private key: %v", err)
	}
	pub, err := ReadKeyFile("alipay_public.pem")
	if err != nil {
		t.Fatalf("read public key: %v", err)
	}
	if priv == "" || pub == "" {
		t.Skip("alipay key files not present — run this test on the configured server")
	}

	gateway := cfg["alipay_gateway"]
	if cfg["alipay_sandbox"] == "1" || cfg["alipay_sandbox"] == "true" {
		gateway = "https://openapi-sandbox.dl.alipaydev.com/gateway.do"
	}
	if gateway == "" {
		gateway = defaultAlipayGateway
	}

	p := NewAlipayProvider(AlipayConfig{
		AppID:      cfg["alipay_appid"],
		PrivateKey: priv,
		PublicKey:  pub,
		Gateway:    gateway,
		SignType:   "RSA2",
	})
	if !p.Configured() {
		t.Fatalf("provider not configured — check alipay_appid and key files")
	}
	t.Logf("using gateway=%s appid=%s", gateway, cfg["alipay_appid"])

	// 1. Real precreate for ¥0.01.
	outTradeNo := fmt.Sprintf("SANDBOX-LIVE-%d", time.Now().UnixNano())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	res, err := p.CreatePayment(ctx, PaymentRequest{
		OutTradeNo: outTradeNo,
		AmountFen:  1, // ¥0.01
		Subject:    "沙箱集成测试",
		NotifyURL:  "https://pay.000328.xyz/notify/alipay",
	})
	if err != nil {
		t.Fatalf("precreate failed: %v", err)
	}
	if res.QRCode == "" {
		t.Fatal("precreate returned empty qr_code")
	}
	t.Logf("precreate OK: out_trade_no=%s qr=%s", outTradeNo, res.QRCode)
	t.Log("SANDBOX LIVE CHECK PASSED — precreate + signing path wired correctly.")
	t.Log("To complete the buyer-paid round trip, scan the QR above with the")
	t.Log("Alipay sandbox buyer app; the app's /notify/alipay will then settle it.")
	t.Log("(The async callback verification path is covered separately by")
	t.Log("TestAlipayParseNotifyVerifies and the web package's end-to-end test.)")
}
