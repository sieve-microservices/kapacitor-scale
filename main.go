package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"syscall"

	"acos.alcatel-lucent.com/scmrepos/git/micro-analytics/kapacitor-scale/handler"
	"acos.alcatel-lucent.com/scmrepos/git/micro-analytics/kapacitor-scale/rancher"
	"acos.alcatel-lucent.com/scmrepos/git/micro-analytics/kapacitor-scale/scaling"
	"github.com/influxdata/kapacitor/udf/agent"
)

var (
	socketPath = flag.String("socket", "/tmp/kapacitor-scale.sock", "Where to create the unix socket")
)

type acceptor struct {
	count      int64
	scaleAgent scaling.Agent
}

// Create a new agent/handler for each new connection.
// Count and log each new connection and termination.
func (acc *acceptor) Accept(conn net.Conn) {
	count := acc.count
	acc.count++
	a := agent.New(conn, conn)
	h := handler.New(a, &acc.scaleAgent)
	a.Handler = h

	log.Println("Starting agent for connection", count)
	a.Start()
	go func() {
		err := a.Wait()
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("Agent for connection %d finished", count)
	}()
}

func parseArgs() *url.URL {
	flag.Parse()
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "USAGE: %s rancherurl\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "rancher url is expected as first argument, for example: http://accesskey:secretkey@localhost:8080")
		os.Exit(1)
	}
	url, err := url.Parse(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "provided url '%s' is malformed: %v", os.Args[1], err)
		os.Exit(1)
	}
	return url
}

func main() {
	rancherUrl := parseArgs()

	// Create unix socket
	addr, err := net.ResolveUnixAddr("unix", *socketPath)
	if err != nil {
		log.Fatal(err)
	}
	l, err := net.ListenUnix("unix", addr)
	if err != nil {
		log.Fatal(err)
	}

	// Create server that listens on the socket
	s := agent.NewServer(l, &acceptor{0, *scaling.New(rancher.New(*rancherUrl))})

	// Setup signal handler to stop Server on various signals
	s.StopOnSignals(os.Interrupt, syscall.SIGTERM)

	log.Println("Server listening on", addr.String())
	err = s.Serve()
	if err != nil {
		log.Fatal(err)
	}
	log.Println("Server stopped")
}
