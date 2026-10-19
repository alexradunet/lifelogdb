// Fakeext is the extension of the tests of cmd/lifelog: it prints what lifelog gave it, one item per line.
package main

import (
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
)

func main() {
	args := os.Args[1:]
	fmt.Println("self=" + os.Args[0])
	fmt.Println("args=" + strings.Join(args, "|"))
	for _, v := range []string{"LIFELOG_URL", "LIFELOG_EXE", "LIFELOG_DB"} {
		fmt.Println(v + "=" + os.Getenv(v))
	}
	if u := os.Getenv("LIFELOG_URL"); u != "" {
		resp, err := http.Get(u + "/actions")
		if err != nil {
			fmt.Println("status=error")
		} else {
			resp.Body.Close()
			fmt.Println("status=" + strconv.Itoa(resp.StatusCode))
		}
	}
	if len(args) > 0 && strings.HasPrefix(args[0], "exit=") {
		n, _ := strconv.Atoi(strings.TrimPrefix(args[0], "exit="))
		os.Exit(n)
	}
	if len(args) > 0 && args[0] == "wait" {
		// An extension that ends cleanly on an interrupt: it writes "ready" beside the file, then waits for the signal.
		interrupted := make(chan os.Signal, 1)
		signal.Notify(interrupted, os.Interrupt)
		if err := os.WriteFile(filepath.Join(filepath.Dir(os.Getenv("LIFELOG_DB")), "ready"), nil, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		<-interrupted
		fmt.Println("interrupted=yes")
	}
}
