package main

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/google/uuid"
)

func (s *session) flood(args []string) error {
	n := 400
	if len(args) > 0 {
		if v, err := strconv.Atoi(args[0]); err == nil && v > 0 {
			n = v
		}
	}
	tok, err := s.token(s.provider)
	if err != nil {
		return err
	}
	counts := map[int]int{}
	retryAfter := ""
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 50)
	for i := 0; i < n; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			req, _ := http.NewRequest("GET", s.apiURL+"/wagering/transactions/"+uuid.NewString(), nil)
			req.Header.Set("Authorization", "Bearer "+tok)
			resp, err := httpClient.Do(req)
			if err != nil {
				return
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			mu.Lock()
			counts[resp.StatusCode]++
			if resp.StatusCode == 429 && retryAfter == "" {
				retryAfter = resp.Header.Get("Retry-After")
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	fmt.Printf("%d requisições GET rápidas com o token de %s:\n", n, s.provider)
	codes := make([]int, 0, len(counts))
	for c := range counts {
		codes = append(codes, c)
	}
	sort.Ints(codes)
	for _, c := range codes {
		fmt.Printf("  %4d x HTTP %d\n", counts[c], c)
	}
	if counts[429] > 0 {
		fmt.Printf("rate limit atingido (Retry-After: %ss). O limite é por instância: RATE_LIMIT_RPS=50, burst 100, vezes 3 instâncias atrás do nginx.\n", retryAfter)
	} else {
		fmt.Println("nenhum 429: aumente n (o burst é 100 por instância, e o nginx espalha entre 3).")
	}
	return nil
}

func (s *session) auth([]string) error {
	if err := s.requireWallet(); err != nil {
		return err
	}
	var lastExt string
	for i := len(s.order) - 1; i >= 0; i-- {
		if s.ops[s.order[i]].kind == "BET" {
			lastExt = s.order[i]
			break
		}
	}
	if lastExt == "" {
		return fmt.Errorf("faça uma aposta antes (bet <valor>) para testar a leitura entre provedores")
	}
	type check struct {
		name         string
		method, path string
		tok          string
		expect       int
	}
	provA, _ := s.token("provider-a")
	provB, _ := s.token("provider-b")
	admin, _ := s.token("wallet-admin")
	outsider, _ := s.token("outsider")
	checks := []check{
		{"sem token lendo carteira", "GET", "/wallets/" + s.walletID, "", 401},
		{"token inválido", "GET", "/wallets/" + s.walletID, "nao-e-um-jwt", 401},
		{"provider-a lendo carteira (rota interna)", "GET", "/wallets/" + s.walletID, provA, 403},
		{"client sem role lendo carteira", "GET", "/wallets/" + s.walletID, outsider, 403},
		{"provider-b lendo transação de provider-a", "GET", "/providers/provider-a/wagering/transactions/" + lastExt, provB, 403},
		{"provider-a lendo a própria transação", "GET", "/providers/provider-a/wagering/transactions/" + lastExt, provA, 200},
		{"wallet-admin lendo transação de qualquer provedor", "GET", "/providers/provider-a/wagering/transactions/" + lastExt, admin, 200},
		{"wallet-admin lendo carteira", "GET", "/wallets/" + s.walletID, admin, 200},
	}
	for _, c := range checks {
		req, _ := http.NewRequest(c.method, s.apiURL+c.path, nil)
		if c.tok != "" {
			req.Header.Set("Authorization", "Bearer "+c.tok)
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			return err
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		mark := "ok "
		if resp.StatusCode != c.expect {
			mark = "!! "
		}
		fmt.Printf("  %s HTTP %d (esperado %d)  %s\n", mark, resp.StatusCode, c.expect, c.name)
	}
	return nil
}

func (s *session) health([]string) error {
	targets := []string{s.apiURL}
	if strings.Contains(s.apiURL, ":8080") {
		for _, p := range []string{"8081", "8082", "8083"} {
			targets = append(targets, strings.Replace(s.apiURL, ":8080", ":"+p, 1))
		}
	}
	for _, base := range targets {
		for _, path := range []string{"/health/live", "/health/ready"} {
			resp, err := httpClient.Get(base + path)
			if err != nil {
				fmt.Printf("  %-28s %-14s erro: %v\n", base, path, err)
				continue
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			fmt.Printf("  %-28s %-14s HTTP %d %s\n", base, path, resp.StatusCode, strings.TrimSpace(string(body)))
		}
	}
	return nil
}

func (s *session) metrics(args []string) error {
	filter := "wager_"
	if len(args) > 0 {
		filter = args[0]
	}
	targets := []string{s.apiURL}
	if strings.Contains(s.apiURL, ":8080") {
		targets = nil
		for _, p := range []string{"8081", "8082", "8083"} {
			targets = append(targets, strings.Replace(s.apiURL, ":8080", ":"+p, 1))
		}
	}
	for _, base := range targets {
		resp, err := httpClient.Get(base + "/metrics")
		if err != nil {
			fmt.Printf("%s: %v\n", base, err)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		fmt.Println(base)
		for _, line := range strings.Split(string(body), "\n") {
			if strings.HasPrefix(line, "#") || !strings.Contains(line, filter) {
				continue
			}
			fmt.Println("  " + line)
		}
	}
	return nil
}
