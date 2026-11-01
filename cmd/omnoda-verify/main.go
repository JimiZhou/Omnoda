// Small helper used by installers that have no Go. The shell installer prefers
// openssl pkeyutl; this binary is the fallback shipped beside release artifacts.
package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
)

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "usage: omnoda-verify <pub-base64> <file> <sig-file>")
		os.Exit(2)
	}
	pub, err := base64.StdEncoding.DecodeString(strings.TrimSpace(os.Args[1]))
	if err != nil || len(pub) != ed25519.PublicKeySize {
		fmt.Fprintln(os.Stderr, "bad public key")
		os.Exit(1)
	}
	body, err := os.ReadFile(os.Args[2])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	sigText, err := os.ReadFile(os.Args[3])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigText)))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if !ed25519.Verify(pub, body, sig) {
		fmt.Fprintln(os.Stderr, "signature mismatch")
		os.Exit(1)
	}
	fmt.Println("ok")
}
