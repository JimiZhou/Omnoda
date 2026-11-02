package sandbox

import "time"

// Untrusted is the policy for model-generated and third-party code.
// No network, no host secrets, a short timeout, and a bounded output.
func Untrusted(workDir string) Request {
	return Request{
		WorkDir:     workDir,
		Timeout:     20 * time.Second,
		MemoryMB:    256,
		Network:     false,
		OutputLimit: 32 * 1024,
	}
}
