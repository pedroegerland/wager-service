package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

func (s *session) token(client string) (string, error) {
	s.mu.Lock()
	t, ok := s.tokens[client]
	s.mu.Unlock()
	if ok {
		return t, nil
	}
	form := url.Values{"grant_type": {"client_credentials"}, "client_id": {client}, "client_secret": {client + "-secret"}}
	resp, err := httpClient.PostForm(s.keycloakURL+"/realms/wager/protocol/openid-connect/token", form)
	if err != nil {
		return "", fmt.Errorf("keycloak: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("keycloak %d: %s", resp.StatusCode, body)
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.Unmarshal(body, &out)
	s.mu.Lock()
	s.tokens[client] = out.AccessToken
	s.mu.Unlock()
	return out.AccessToken, nil
}

type reply struct {
	status int
	body   map[string]any
	raw    string
}

func (r reply) str(k string) string {
	v, _ := r.body[k].(string)
	return v
}

func (r reply) amount(k string) string {
	m, _ := r.body[k].(map[string]any)
	v, _ := m["amount"].(string)
	return v
}

func (s *session) call(method, path, client string, body any, headers map[string]string) (reply, error) {
	tok, err := s.token(client)
	if err != nil {
		return reply{}, err
	}
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, s.apiURL+path, rd)
	if err != nil {
		return reply{}, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return reply{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := reply{status: resp.StatusCode, raw: strings.TrimSpace(string(raw))}
	_ = json.Unmarshal(raw, &out.body)
	return out, nil
}

func (s *session) show(r reply) {
	if r.status == 409 && (r.str("code") == "IDEMPOTENCY_KEY_CONFLICT" || r.str("code") == "EXTERNAL_TRANSACTION_ID_REUSED") {
		fmt.Println("esse externalTransactionId já foi usado por este provedor (talvez em outra sessão ou carteira). Use outro id, ou omita o id para o playground gerar um.")
	}
	pretty := r.raw
	var buf bytes.Buffer
	if json.Indent(&buf, []byte(r.raw), "  ", "  ") == nil {
		pretty = buf.String()
	}
	fmt.Printf("HTTP %d\n  %s\n", r.status, pretty)
	if b := r.amount("balance"); b != "" {
		s.balance = b
	}
}

func (s *session) requireWallet() error {
	if s.walletID == "" {
		return fmt.Errorf("abra uma carteira primeiro: open [valor]")
	}
	return nil
}

func (s *session) submitBody(op operation) map[string]any {
	b := map[string]any{
		"providerId": s.providerForOps(), "externalTransactionId": op.ext, "playerId": s.playerID, "walletId": s.walletID,
		"roundId": "round-1", "gameId": "fortune-chimp", "kind": op.kind,
		"money": map[string]string{"amount": op.amount, "currency": "BRL"},
	}
	if op.ref != "" {
		b["referenceExternalTransactionId"] = op.ref
	}
	return b
}

func (s *session) providerForOps() string {
	if s.provider == "wallet-admin" {
		return "provider-a"
	}
	return s.provider
}

func (s *session) submit(op operation) (reply, error) {
	if err := s.requireWallet(); err != nil {
		return reply{}, err
	}
	if op.key == "" {
		op.key = s.providerForOps() + ":" + op.ext
	}
	s.remember(op)
	return s.call("POST", "/wagering/transactions", s.provider, s.submitBody(op), map[string]string{"Idempotency-Key": op.key})
}
