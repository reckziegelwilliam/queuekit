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
		fmt.Printf("queuekitd version %s (commit: %s, built: %s)\n", Version, Commit, BuildTime)
		os.Exit(0)
	}

	fmt.Println("🚀 QueueKit Server (queuekitd)")
	fmt.Println("================================")
	fmt.Printf("Version: %s\n", Version)
	fmt.Printf("Commit:  %s\n", Commit)
	fmt.Printf("Built:   %s\n\n", BuildTime)
	fmt.Println("This is a placeholder. Server functionality will be implemented in Phase 3.")
	fmt.Println("Use --version to see version information.")
}
