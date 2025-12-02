package main

import (
	"flag"
	"fmt"
	"os"
)

var (
	// Version information (injected at build time)
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

func main() {
	versionFlag := flag.Bool("version", false, "Print version information")
	flag.Parse()

	if *versionFlag {
		fmt.Printf("queuekit version %s (commit: %s, built: %s)\n", Version, Commit, BuildTime)
		os.Exit(0)
	}

	fmt.Println("📋 QueueKit CLI")
	fmt.Println("===============")
	fmt.Printf("Version: %s\n", Version)
	fmt.Printf("Commit:  %s\n", Commit)
	fmt.Printf("Built:   %s\n\n", BuildTime)
	fmt.Println("This is a placeholder. CLI functionality will be implemented in Phase 5.")
	fmt.Println("Use --version to see version information.")
	fmt.Println("\nFuture commands:")
	fmt.Println("  queuekit enqueue <type> --queue <name> --payload <json>")
	fmt.Println("  queuekit inspect queues")
	fmt.Println("  queuekit inspect jobs --queue <name>")
	fmt.Println("  queuekit retry <job-id>")
}
