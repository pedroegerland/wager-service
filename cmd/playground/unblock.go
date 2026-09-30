package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func tokenSubject(tok string) (string, error) {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("token não parece um JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", err
	}
	var claims struct {
		Sub string `json:"sub"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", err
	}
	return claims.Sub, nil
}

func (s *session) instanceURLs() []string {
	if !strings.Contains(s.apiURL, ":8080") {
		return []string{s.apiURL}
	}
	var out []string
	for _, p := range []string{"8081", "8082", "8083"} {
		out = append(out, strings.Replace(s.apiURL, ":8080", ":"+p, 1))
	}
	return out
}

func (s *session) unblock(args []string) error {
	client := s.provider
	if len(args) > 0 {
		client = args[0]
	}
	admin, err := s.token("wallet-admin")
	if err != nil {
		return err
	}
	path := "/rate-limits"
	if client != "all" {
		tok, err := s.token(client)
		if err != nil {
			return err
		}
		sub, err := tokenSubject(tok)
		if err != nil {
			return err
		}
		path += "/" + sub
	}
	for _, base := range s.instanceURLs() {
		req, _ := http.NewRequest(http.MethodDelete, base+path, nil)
		req.Header.Set("Authorization", "Bearer "+admin)
		resp, err := httpClient.Do(req)
		if err != nil {
			fmt.Printf("  %-28s erro: %v\n", base, err)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		fmt.Printf("  %-28s HTTP %d %s\n", base, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if client == "all" {
		fmt.Println("todos os buckets de rate limit foram zerados em cada instância")
		return nil
	}
	fmt.Printf("bucket de %s zerado em cada instância; a próxima chamada passa sem esperar o refill\n", client)
	return nil
}
