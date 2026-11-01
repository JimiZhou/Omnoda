package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
)

func main() {
	keyPath := flag.String("key", "", "32-byte seed or 64-byte private key")
	file := flag.String("file", "", "file to sign")
	out := flag.String("out", "", "signature output")
	pubOut := flag.String("pubout", "", "write raw public key")
	gen := flag.Bool("generate", false, "generate a keypair: writes <key> and <key>.pub")
	flag.Parse()
	if *gen {
		_, priv, err := ed25519.GenerateKey(nil)
		if err != nil {
			fatal(err)
		}
		if err := os.WriteFile(*keyPath, priv, 0o600); err != nil {
			fatal(err)
		}
		if err := os.WriteFile(*keyPath+".pub", priv.Public().(ed25519.PublicKey), 0o644); err != nil {
			fatal(err)
		}
		fmt.Println(base64.StdEncoding.EncodeToString(priv.Public().(ed25519.PublicKey)))
		return
	}
	raw, err := os.ReadFile(*keyPath)
	if err != nil {
		fatal(err)
	}
	var priv ed25519.PrivateKey
	switch len(raw) {
	case ed25519.SeedSize:
		priv = ed25519.NewKeyFromSeed(raw)
	case ed25519.PrivateKeySize:
		priv = ed25519.PrivateKey(raw)
	default:
		fatal(fmt.Errorf("bad key length %d", len(raw)))
	}
	if *pubOut != "" {
		if err := os.WriteFile(*pubOut, priv.Public().(ed25519.PublicKey), 0o644); err != nil {
			fatal(err)
		}
	}
	body, err := os.ReadFile(*file)
	if err != nil {
		fatal(err)
	}
	sig := ed25519.Sign(priv, body)
	text := base64.StdEncoding.EncodeToString(sig) + "\n"
	if *out == "" {
		fmt.Print(text)
		return
	}
	if err := os.WriteFile(*out, []byte(text), 0o644); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
