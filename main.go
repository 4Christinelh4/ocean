package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"ocean/internal/desk"
	"ocean/managedagent"
)

func main() {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		log.Fatal("set ANTHROPIC_API_KEY")
	}

	ctx := context.Background()
	d, err := desk.New(ctx)
	if err != nil {
		log.Fatalf("desk: %v", err)
	}
	fmt.Printf("Session: %s\n", d.SessionID())
	if v := strings.TrimSpace(os.Getenv("ANTHROPIC_AGENT_VERSION")); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			fmt.Printf("Pinned agent config version: %d\n", n)
		}
	}
	fmt.Println("输入问题后回车发送；空行忽略；quit 或 exit 退出；Ctrl-D 结束输入。")

	in := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")
		if !in.Scan() {
			if err := in.Err(); err != nil {
				log.Fatalf("stdin: %v", err)
			}
			fmt.Println()
			break
		}
		line := strings.TrimSpace(in.Text())
		if line == "" {
			continue
		}
		switch strings.ToLower(line) {
		case "quit", "exit", "q":
			return
		}

		roundCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
		err := d.Chat(roundCtx, "terminal", "terminal.user_input", line, func(ev managedagent.StreamEvent) error {
			typ, err := ev.Type()
			if err != nil {
				return err
			}
			switch typ {
			case "agent.message":
				if raw, ok := ev["content"]; ok {
					fmt.Println(string(raw))
				} else {
					fmt.Println(ev)
				}
			case "session.status_idle":
				return managedagent.ErrStopStream
			}
			return nil
		})
		cancel()
		if err != nil {
			log.Printf("round error: %v", err)
		}
		fmt.Println("---")
	}
}
