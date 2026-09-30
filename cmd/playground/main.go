package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func main() {
	s := newSession()
	fmt.Println("wager-service playground")
	fmt.Printf("api=%s keycloak=%s sqs=%s\n", s.apiURL, s.keycloakURL, s.awsEndpoint)
	if err := s.checkAPI(); err != nil {
		fmt.Printf("aviso: a api não respondeu em %s (%v). Suba com 'make play' ou 'make demo'.\n", s.apiURL, err)
	}
	fmt.Println()
	_ = s.help(nil)
	fmt.Println("Comece com 'open'. 'help' (ou comandos, cmds, ações) repete esta lista, 'quit' sai.")
	fmt.Println()

	in := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print(s.prompt())
		if !in.Scan() {
			fmt.Println()
			return
		}
		line := strings.TrimSpace(in.Text())
		if line == "" {
			continue
		}
		if line == "quit" || line == "exit" {
			return
		}
		if err := s.run(strings.Fields(line)); err != nil {
			fmt.Println("erro:", err)
		}
		fmt.Println()
	}
}
