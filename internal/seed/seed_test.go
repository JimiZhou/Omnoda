package seed

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestBootstrapIdempotentAndLocal(t *testing.T) {
	root := t.TempDir()
	rt, err := Bootstrap(root)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := rt.Reg.List()
	if err != nil || len(rows) == 0 {
		t.Fatal(err, len(rows))
	}
	rt.DB.Close()
	rt2, err := Bootstrap(root)
	if err != nil {
		t.Fatal(err)
	}
	defer rt2.DB.Close()
	rows2, _ := rt2.Reg.List()
	if len(rows2) != len(rows) {
		t.Fatalf("registry changed across restart: %d %d", len(rows), len(rows2))
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = rt2.Serve(ctx, 0) }()
	deadline := time.Now().Add(3 * time.Second)
	var body string
	for time.Now().Before(deadline) {
		st := ReadStatus(root)
		if st.Running && st.Addr != "" {
			req, _ := http.NewRequest(http.MethodPost, "http://"+st.Addr+"/api/v1/seed/bootstrap", strings.NewReader("{}"))
			resp, err := http.DefaultClient.Do(req)
			if err == nil {
				b, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				body = string(b)
				if resp.StatusCode == 200 {
					break
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if body == "" || !contains(body, "seed") {
		t.Fatalf("seed endpoint: %s", body)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || (len(s) > 0 && index(s, sub) >= 0))
}

func index(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
