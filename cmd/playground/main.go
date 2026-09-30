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
	fmt.Printf("api=%s keycloak=%s sqs=%s\n\n", s.apiURL, s.keycloakURL, s.awsEndpoint)
	_ = s.help(nil)
	fmt.Println("Comece com 'open'. 'help' repete esta lista, 'quit' sai.")
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
