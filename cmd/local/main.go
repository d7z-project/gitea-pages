package main

import (
	"flag"
	"os"
)

var (
	org    = "pub"
	domain = "fbi.com"
	repo   = org + "." + domain
	path   = ""

	port = 8080
)

func init() {
	dir, _ := os.Getwd()
	path = dir
	flag.StringVar(&org, "org", org, "org")
	flag.StringVar(&repo, "repo", repo, "repo")
	flag.StringVar(&domain, "domain", domain, "domain")
	flag.StringVar(&path, "path", path, "path")
	flag.IntVar(&port, "port", port, "port")
	flag.Parse()
}

func main() {
}
