//go:build integration

package integration

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/pedroegerland/wager-service/internal/config"
)

func TestFxStartStop(t *testing.T) {
	port := freePort()
	app := newApp(port, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	must(t, app.Start(ctx))
	base := fmt.Sprintf("http://127.0.0.1:%d", port)

	if r := httpRequest(t, base, "GET", "/health/ready", "", nil, nil); r.Status != 200 {
		t.Fatalf("ready: %d %s", r.Status, r.Raw)
	}
	if r := httpRequest(t, base, "GET", "/metrics", "", nil, nil); r.Status != 200 {
		t.Errorf("metrics: %d", r.Status)
	}

	start := time.Now()
	must(t, app.Stop(ctx))
	if d := time.Since(start); d > 20*time.Second {
		t.Errorf("stop took %s", d)
	}
	if _, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second); err == nil {
		t.Error("port still open after stop")
	}

	bad := newApp(freePort(), func(c *config.Config) { c.OIDCJWKSURL = "http://127.0.0.1:1/nothing" })
	shortCtx, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	if err := bad.Start(shortCtx); err == nil {
		_ = bad.Stop(context.Background())
		t.Error("expected start to fail with unreachable jwks")
	}
}

func TestRestartPreservesState(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	portA := freePort()
	a := newApp(portA, nil)
	must(t, a.Start(ctx))
	baseA := fmt.Sprintf("http://127.0.0.1:%d", portA)

	walletID, playerID := openWallet(t, "100.00")
	bet := operation{ext: "bet-" + shortID(), kind: "BET", amount: "10.00", walletID: walletID, playerID: playerID}
	first := submit(t, baseA, bet)
	if first.Status != 200 {
		t.Fatalf("bet on A: %s", first.Raw)
	}
	lateRef := "late-" + shortID()
	refund := operation{ext: "ref-" + shortID(), kind: "REFUND", amount: "10.00", ref: lateRef, walletID: walletID, playerID: playerID}
	if r := submit(t, baseA, refund); r.Status != 202 {
		t.Fatalf("refund on A: %s", r.Raw)
	}
	must(t, a.Stop(ctx))

	portB := freePort()
	b := newApp(portB, nil)
	must(t, b.Start(ctx))
	defer b.Stop(context.Background())
	baseB := fmt.Sprintf("http://127.0.0.1:%d", portB)

	r := submit(t, baseB, bet)
	if r.Status != 200 || !r.bool("idempotentReplay") || r.str("transactionId") != first.str("transactionId") || r.money("balance") != "90.00" {
		t.Errorf("replay on B: %d %s", r.Status, r.Raw)
	}
	r = submit(t, baseB, refund)
	if r.Status != 202 || !r.bool("idempotentReplay") {
		t.Errorf("pending replay on B: %d %s", r.Status, r.Raw)
	}

	if r := submit(t, baseB, operation{ext: lateRef, kind: "BET", amount: "10.00", walletID: walletID, playerID: playerID}); r.Status != 200 {
		t.Fatalf("late bet: %s", r.Raw)
	}
	waitUntil(t, 30*time.Second, "refund resolved after restart", func() bool {
		s, _ := transactionStatus(t, "provider-a", refund.ext)
		return s == "PROCESSED"
	})
	if storedBalance(t, walletID) != 9000 {
		t.Errorf("balance %d", storedBalance(t, walletID))
	}
	assertReconciled(t, walletID)
}
