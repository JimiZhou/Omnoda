package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/JimiZhou/Omnoda/internal/capabilities/manifest"
)

// modelGenerator asks an OpenAI-compatible chat API to write a capability package.
// The response must be a JSON object of relative path -> file text.
// This is a real model call. There is no template fallback.
type modelGenerator struct {
	base  string
	model string
	key   string
}

func (g *modelGenerator) Generate(ctx context.Context, goal, dest string) (int, error) {
	if g.key == "" || g.base == "" || g.model == "" {
		return 0, errors.New("model provider is not configured")
	}
	sys := `You write one Omnoda capability package.
Return JSON only: {"files":{"capability.json":"...","src/main.py":"...","builder_test.py":"...","verifier_test.py":"...","verifier/expected.json":"...","README.md":"..."}}.
The capability.json must use apiVersion omnoda.dev/v1alpha1, kind Capability, runtime python, network false.
Python reads --input and --output. Do not use the network. Do not read files outside the input path.
verifier/expected.json must describe an independent sample that verifier_test.py embeds as a string literal, not a file the builder creates.`
	body := map[string]any{
		"model": g.model,
		"messages": []map[string]string{
			{"role": "system", "content": sys},
			{"role": "user", "content": goal},
		},
	}
	raw, _ := json.Marshal(body)
	cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, strings.TrimRight(g.base, "/")+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+g.key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	buf, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return 0, fmt.Errorf("model http %d", resp.StatusCode)
	}
	var parsed struct {
		Usage struct {
			TotalTokens int `json:"total_tokens"`
		} `json:"usage"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(buf, &parsed); err != nil || len(parsed.Choices) == 0 {
		return 0, errors.New("model response was not a chat completion")
	}
	content := parsed.Choices[0].Message.Content
	var files struct {
		Files map[string]string `json:"files"`
	}
	if err := json.Unmarshal([]byte(extractObject(content)), &files); err != nil || len(files.Files) == 0 {
		return parsed.Usage.TotalTokens, errors.New("model did not return capability files")
	}
	for name, text := range files.Files {
		if filepath.IsAbs(name) || strings.Contains(name, "..") {
			return parsed.Usage.TotalTokens, errors.New("model returned an unsafe path")
		}
		path := filepath.Join(dest, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return parsed.Usage.TotalTokens, err
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			return parsed.Usage.TotalTokens, err
		}
	}
	if _, err := manifest.Parse(mustBytes(filepath.Join(dest, "capability.json"))); err != nil {
		return parsed.Usage.TotalTokens, err
	}
	return parsed.Usage.TotalTokens, nil
}

func extractObject(s string) string {
	i := strings.Index(s, "{")
	j := strings.LastIndex(s, "}")
	if i >= 0 && j > i {
		return s[i : j+1]
	}
	return s
}

func mustBytes(p string) []byte {
	b, _ := os.ReadFile(p)
	return b
}
