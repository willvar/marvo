package main

import (
	"flag"
	"fmt"
	"os"

	"marvo/config"
)

func main() {
	configPath := flag.String("c", "config.yaml", "path to config file")
	flag.Parse()
	if flag.NArg() != 1 || flag.Arg(0) != "public-url" {
		fmt.Fprintln(os.Stderr, "usage: marvo-config [-c config.yaml] public-url")
		os.Exit(2)
	}
	publicURL, err := config.PublicURLFromFile(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(publicURL)
}
