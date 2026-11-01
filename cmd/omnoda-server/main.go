package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/JimiZhou/Omnoda/internal/controlplane"
	"github.com/JimiZhou/Omnoda/internal/protocol"
	"github.com/JimiZhou/Omnoda/internal/storage"
)

func main() {
	addr := flag.String("addr", env("OMNODA_ADDR", "127.0.0.1:8787"), "listen address")
	data := flag.String("data", env("OMNODA_DATA", "data"), "data directory")
	publicURL := flag.String("public-url", env("OMNODA_PUBLIC_URL", ""), "external base URL used in enrollment commands")
	flag.Parse()
	if err := os.MkdirAll(*data, 0o700); err != nil {
		log.Fatal(err)
	}
	db, err := storage.Open(filepath.Join(*data, "omnoda.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	srv, err := controlplane.New(db, *data, *publicURL, protocol.Version)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Omnoda control plane %s listening on %s", protocol.Version, *addr)
	if err := http.ListenAndServe(*addr, srv.Handler()); err != nil {
		log.Fatal(err)
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// silence unused in tiny builds
var _ = fmt.Sprintf
