package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func isHelp(cmd string) bool {
	switch cmd {
	case "help", "comandos", "comando", "cmds", "cmd", "acoes", "ações":
		return true
	}
	return false
}

func stdinIsTerminal() bool {
	info, err := os.Stdin.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func wantsColors(in *bufio.Scanner, interactive bool) bool {
	if !colorsEnabled || !interactive {
		return colorsEnabled
	}
	for {
		fmt.Print("Quer a saída com cores? [S/n] ")
		if !in.Scan() {
			return true
		}
		switch strings.ToLower(strings.TrimSpace(in.Text())) {
		case "", "s", "sim", "y", "yes":
			return true
		case "n", "nao", "não", "no":
			return false
		}
		fmt.Println("responda S (sim) ou N (não)")
	}
}

func main() {
	s := newSession()
	in := bufio.NewScanner(os.Stdin)
	fmt.Println("wager-service playground")
	colorsEnabled = wantsColors(in, stdinIsTerminal())
	fmt.Printf("api=%s keycloak=%s sqs=%s\n", s.apiURL, s.keycloakURL, s.awsEndpoint)
	if err := s.checkAPI(); err != nil {
		fmt.Println(red(fmt.Sprintf("aviso: a api não respondeu em %s (%v). Suba com 'make play' ou 'make demo'.", s.apiURL, err)))
	}
	fmt.Println()
	_ = s.help(nil)
	fmt.Println(yellow("Comece com 'open'. 'help' (ou comandos, cmds, ações) repete esta lista, 'quit' sai."))
	fmt.Println()

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
		fields := strings.Fields(line)
		if err := s.run(fields); err != nil {
			fmt.Println(red("erro: " + err.Error()))
		}
		if !isHelp(fields[0]) {
			fmt.Println()
			fmt.Println(yellow("quer ver os comandos? digite help (ou comandos, cmds, ações)"))
		}
		fmt.Println()
	}
}
