//go:build integration

package integration

import (
	"os"
	"strings"
	"sync"
	"testing"
)

func instances() []string {
	if v := os.Getenv("API_INSTANCES"); v != "" {
		return strings.Split(v, ",")
	}
	return []string{apiURL}
}

func TestConcurrentSameBet50(t *testing.T) {
	walletID, playerID := openWallet(t, "100.00")
	bet := operation{ext: "bet-" + uid(), kind: "BET", amount: "10.00", walletID: walletID, playerID: playerID}
	targets := instances()

	var wg sync.WaitGroup
	results := make([]resp, 50)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = submit(t, targets[i%len(targets)], bet)
		}(i)
	}
	wg.Wait()

	fresh, replays := 0, 0
	for _, r := range results {
		if r.Status != 200 || r.str("status") != "PROCESSED" || r.money("balance") != "90.00" {
			t.Errorf("unexpected: %d %s", r.Status, r.Raw)
			continue
		}
		if r.bool("idempotentReplay") {
			replays++
		} else {
			fresh++
		}
	}
	if fresh != 1 || replays != 49 {
		t.Errorf("fresh=%d replays=%d", fresh, replays)
	}
	if storedBalance(t, walletID) != 9000 || ledgerCount(t, walletID) != 2 {
		t.Errorf("balance %d ledger %d", storedBalance(t, walletID), ledgerCount(t, walletID))
	}
	assertReconciled(t, walletID)
}

func TestConcurrentTwoBetsOnHundred(t *testing.T) {
	for round := 0; round < 5; round++ {
		walletID, playerID := openWallet(t, "100.00")
		a := operation{ext: "bet-a-" + uid(), kind: "BET", amount: "80.00", walletID: walletID, playerID: playerID}
		b := operation{ext: "bet-b-" + uid(), kind: "BET", amount: "80.00", walletID: walletID, playerID: playerID}
		targets := instances()

		var wg sync.WaitGroup
		var ra, rb resp
		wg.Add(2)
		go func() { defer wg.Done(); ra = submit(t, targets[0], a) }()
		go func() { defer wg.Done(); rb = submit(t, targets[len(targets)-1], b) }()
		wg.Wait()

		codes := []int{ra.Status, rb.Status}
		if !((codes[0] == 200 && codes[1] == 422) || (codes[0] == 422 && codes[1] == 200)) {
			t.Fatalf("round %d: %d %s / %d %s", round, ra.Status, ra.Raw, rb.Status, rb.Raw)
		}
		for _, r := range []resp{ra, rb} {
			if r.Status == 422 && r.str("failureCode") != "INSUFFICIENT_FUNDS" {
				t.Errorf("rejection code %s", r.Raw)
			}
			if r.Status == 200 && r.money("balance") != "20.00" {
				t.Errorf("processed balance %s", r.Raw)
			}
		}
		if storedBalance(t, walletID) != 2000 || ledgerCount(t, walletID) != 2 {
			t.Errorf("round %d: balance %d ledger %d", round, storedBalance(t, walletID), ledgerCount(t, walletID))
		}

		ra2, rb2 := submit(t, targets[0], a), submit(t, targets[0], b)
		if ra2.Status != ra.Status || rb2.Status != rb.Status || !ra2.bool("idempotentReplay") || !rb2.bool("idempotentReplay") {
			t.Errorf("resend changed the outcome: %s / %s", ra2.Raw, rb2.Raw)
		}
		if storedBalance(t, walletID) != 2000 {
			t.Errorf("resend moved money")
		}
		assertReconciled(t, walletID)
	}
}

func TestIndependentWalletsInParallel(t *testing.T) {
	const wallets = 8
	const betsPer = 10
	type w struct{ id, player string }
	ws := make([]w, wallets)
	for i := range ws {
		ws[i].id, ws[i].player = openWallet(t, "100.00")
	}
	targets := instances()
	var wg sync.WaitGroup
	errs := make(chan string, wallets*betsPer)
	for i, wl := range ws {
		for j := 0; j < betsPer; j++ {
			wg.Add(1)
			go func(i, j int, wl w) {
				defer wg.Done()
				r := submit(t, targets[(i+j)%len(targets)], operation{ext: "bet-" + uid(), kind: "BET", amount: "1.00", walletID: wl.id, playerID: wl.player})
				if r.Status != 200 {
					errs <- string(r.Raw)
				}
			}(i, j, wl)
		}
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
	for _, wl := range ws {
		if storedBalance(t, wl.id) != 9000 || ledgerCount(t, wl.id) != betsPer+1 {
			t.Errorf("wallet %s: balance %d ledger %d", wl.id, storedBalance(t, wl.id), ledgerCount(t, wl.id))
		}
		assertReconciled(t, wl.id)
	}
}

func TestMultiInstanceReplay(t *testing.T) {
	targets := instances()
	if len(targets) < 2 {
		t.Skip("needs API_INSTANCES with several processes")
	}
	walletID, playerID := openWallet(t, "100.00")
	bet := operation{ext: "bet-" + uid(), kind: "BET", amount: "40.00", walletID: walletID, playerID: playerID}
	first := submit(t, targets[0], bet)
	if first.Status != 200 {
		t.Fatalf("bet: %s", first.Raw)
	}
	submit(t, targets[1], operation{ext: "win-" + uid(), kind: "WIN", amount: "5.00", walletID: walletID, playerID: playerID})
	for _, base := range targets[1:] {
		r := submit(t, base, bet)
		if r.Status != 200 || !r.bool("idempotentReplay") || r.money("balance") != "60.00" || r.str("transactionId") != first.str("transactionId") {
			t.Errorf("%s: %d %s", base, r.Status, r.Raw)
		}
	}
}
