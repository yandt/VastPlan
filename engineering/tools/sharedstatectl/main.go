// sharedstatectl creates signed Shared State backups, verifies archives, and
// restores only into an absent JetStream stream.
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "keygen":
		err = runKeygen(os.Args[2:])
	case "backup":
		err = runBackup(os.Args[2:])
	case "verify":
		err = runVerify(os.Args[2:])
	case "restore":
		err = runRestore(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: sharedstatectl <keygen|backup|verify|restore> [flags]")
}
