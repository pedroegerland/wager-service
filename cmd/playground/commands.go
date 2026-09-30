package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

func (s *session) open(args []string) error {
	amount := "100.00"
	owner := s.owner
	for _, a := range args {
		if strings.EqualFold(a, unknownOwner) {
			owner = unknownOwner
			continue
		}
		amount = normalizeAmount(a)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !isUnknown(owner) {
		if existing, err := s.registeredWallets(ctx, owner); err != nil {
			return err
		} else if len(existing) > 0 {
			return fmt.Errorf("%s já tem a carteira %s (saldo %s); retome com 'use %s', ou abra uma sem dono com 'open %s unknown'",
				owner, existing[0].walletID, fmtCents(existing[0].balance), owner, strings.TrimSuffix(amount, ".00"))
		}
	}
	s.playerID = uuid.NewString()
	r, err := s.call("POST", "/wallets", "wallet-admin", map[string]any{
		"playerId": s.playerID, "initialBalance": map[string]string{"amount": amount, "currency": "BRL"},
	}, nil)
	if err != nil {
		return err
	}
	s.show(r)
	if r.status == 201 {
		s.walletID = r.str("id")
		s.ops = map[string]operation{}
		s.order = nil
		if err := s.registerWallet(ctx, uuid.MustParse(s.walletID), owner); err != nil {
			fmt.Println(yellow("aviso: carteira aberta, mas não consegui registrar o dono: " + err.Error()))
		} else if isUnknown(owner) {
			fmt.Println(yellow("carteira aberta sem dono; vincule depois com: apply " + s.walletID + " <nome>"))
		} else {
			fmt.Println(green(fmt.Sprintf("carteira registrada no nome de %s; retome depois com: use %s", owner, owner)))
		}
	}
	return nil
}

func (s *session) wallet([]string) error {
	if err := s.requireWallet(); err != nil {
		return err
	}
	r, err := s.call("GET", "/wallets/"+s.walletID, "wallet-admin", nil, nil)
	if err != nil {
		return err
	}
	s.show(r)
	return nil
}

func (s *session) ledger([]string) error {
	if err := s.requireWallet(); err != nil {
		return err
	}
	r, err := s.call("GET", "/wallets/"+s.walletID+"/ledger?limit=50", "wallet-admin", nil, nil)
	if err != nil {
		return err
	}
	entries, _ := r.body["entries"].([]any)
	fmt.Printf("%s, %d lançamentos\n", statusLine(r.status), len(entries))
	for _, e := range entries {
		m := e.(map[string]any)
		amt := m["money"].(map[string]any)["amount"]
		before := m["balanceBefore"].(map[string]any)["amount"]
		after := m["balanceAfter"].(map[string]any)["amount"]
		fmt.Printf("  %-6s %8s   %s -> %s   tx=%s\n", m["direction"], amt, before, after, m["transactionId"])
	}
	return nil
}

func (s *session) reconcile([]string) error {
	if err := s.requireWallet(); err != nil {
		return err
	}
	r, err := s.call("POST", "/wallets/"+s.walletID+"/reconciliation", "wallet-admin", nil, nil)
	if err != nil {
		return err
	}
	s.show(r)
	return nil
}

func (s *session) movement(kind string, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("uso: %s <valor> [id]", kindLower(kind))
	}
	op := operation{kind: kind, amount: normalizeAmount(args[0]), ext: s.newExt(kind)}
	if len(args) > 1 {
		op.ext = args[1]
	}
	r, err := s.submit(op)
	if err != nil {
		return err
	}
	fmt.Println("externalTransactionId:", op.ext)
	s.show(r)
	return nil
}

func kindLower(k string) string { return map[string]string{"BET": "bet", "WIN": "win"}[k] }

func (s *session) bet(args []string) error { return s.movement("BET", args) }
func (s *session) win(args []string) error { return s.movement("WIN", args) }

func (s *session) loginCmd([]string) error {
	clients := []string{s.provider}
	if s.provider != "wallet-admin" {
		clients = append(clients, "wallet-admin")
	}
	for _, c := range clients {
		s.forgetToken(c)
		if _, err := s.login(c); err != nil {
			return err
		}
		s.mu.Lock()
		exp := s.tokens[c].expiresAt
		s.mu.Unlock()
		fmt.Println(green(fmt.Sprintf("token de %s renovado, vale até %s", c, exp.Local().Format("15:04:05"))))
	}
	return nil
}

func (s *session) loss(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("LOSS é sempre 0.00 e não recebe valor; use apenas: loss")
	}
	op := operation{kind: "LOSS", amount: "0.00", ext: s.newExt("LOSS")}
	r, err := s.submit(op)
	if err != nil {
		return err
	}
	fmt.Println("externalTransactionId:", op.ext)
	s.show(r)
	return nil
}

func (s *session) reversal(kind string, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("uso: %s <id-da-referência> [valor]", map[string]string{"REFUND": "refund", "ROLLBACK": "rollback"}[kind])
	}
	ref := args[0]
	amount := ""
	if len(args) > 1 {
		amount = normalizeAmount(args[1])
	} else if known, ok := s.ops[ref]; ok {
		amount = known.amount
	} else {
		return fmt.Errorf("não conheço %q nesta sessão; informe o valor: %s %s <valor>", ref, kind, ref)
	}
	op := operation{kind: kind, amount: amount, ext: s.newExt(kind), ref: ref}
	r, err := s.submit(op)
	if err != nil {
		return err
	}
	fmt.Println("externalTransactionId:", op.ext)
	s.show(r)
	return nil
}

func (s *session) refund(args []string) error   { return s.reversal("REFUND", args) }
func (s *session) rollback(args []string) error { return s.reversal("ROLLBACK", args) }

func (s *session) replay(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("uso: replay <id>")
	}
	op, ok := s.ops[args[0]]
	if !ok {
		return fmt.Errorf("não conheço %q nesta sessão (ops lista)", args[0])
	}
	r, err := s.submit(op)
	if err != nil {
		return err
	}
	s.show(r)
	return nil
}

func (s *session) conflict(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("uso: conflict <id> <novo-valor>")
	}
	op, ok := s.ops[args[0]]
	if !ok {
		return fmt.Errorf("não conheço %q nesta sessão (ops lista)", args[0])
	}
	changed := op
	changed.amount = normalizeAmount(args[1])
	r, err := s.call("POST", "/wagering/transactions", s.provider, s.submitBody(changed), map[string]string{"Idempotency-Key": op.key})
	if err != nil {
		return err
	}
	fmt.Printf("mesma chave %q com valor %s em vez de %s\n", op.key, changed.amount, op.amount)
	s.show(r)
	return nil
}

func (s *session) tx(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("uso: tx <id>")
	}
	r, err := s.call("GET", "/providers/"+s.providerForOps()+"/wagering/transactions/"+args[0], s.provider, nil, nil)
	if err != nil {
		return err
	}
	s.show(r)
	return nil
}

func (s *session) race(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("uso: race <valor> [n]")
	}
	n := 50
	if len(args) > 1 {
		if v, err := strconv.Atoi(args[1]); err == nil && v > 0 {
			n = v
		}
	}
	if err := s.requireWallet(); err != nil {
		return err
	}
	op := operation{kind: "BET", amount: normalizeAmount(args[0]), ext: s.newExt("BET")}
	op.key = s.providerForOps() + ":" + op.ext
	s.remember(op)
	body := s.submitBody(op)

	results := make([]reply, n)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], _ = s.call("POST", "/wagering/transactions", s.provider, body, map[string]string{"Idempotency-Key": op.key})
		}(i)
	}
	wg.Wait()

	counts := map[string]int{}
	for _, r := range results {
		label := fmt.Sprintf("%s %s", statusLine(r.status), r.str("status"))
		if replay, _ := r.body["idempotentReplay"].(bool); replay {
			label += " replay"
		}
		if r.status >= 400 && r.str("code") != "" {
			label += " " + r.str("code")
		}
		counts[label]++
	}
	fmt.Printf("%d envios da aposta %s (%s):\n", n, op.ext, op.amount)
	for label, c := range counts {
		fmt.Printf("  %3d x %s\n", c, label)
	}
	return s.wallet(nil)
}

func (s *session) race2(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("uso: race2 <valor-a> <valor-b>")
	}
	if err := s.requireWallet(); err != nil {
		return err
	}
	a := operation{kind: "BET", amount: normalizeAmount(args[0]), ext: s.newExt("BET")}
	b := operation{kind: "BET", amount: normalizeAmount(args[1]), ext: s.newExt("BET")}
	var ra, rb reply
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); ra, _ = s.submit(a) }()
	go func() { defer wg.Done(); rb, _ = s.submit(b) }()
	wg.Wait()
	for _, pair := range []struct {
		op operation
		r  reply
	}{{a, ra}, {b, rb}} {
		fmt.Printf("%s %s -> %s %s %s\n", pair.op.ext, pair.op.amount, statusLine(pair.r.status), pair.r.str("status"), pair.r.str("failureCode"))
	}
	return s.wallet(nil)
}
