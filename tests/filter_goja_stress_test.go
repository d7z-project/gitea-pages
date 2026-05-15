package tests

import (
	"context"
	"io"
	"net/http"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	testcore "gopkg.d7z.net/gitea-pages/tests/core"
)

func Test_GoJa_StressConcurrentSimpleHTTP(t *testing.T) {
	if os.Getenv("GOJA_STRESS") != "1" {
		t.Skip("set GOJA_STRESS=1 to run goja stress tests")
	}
	server := testcore.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/index.js", `
serve(async function(request) {
  await new Promise(resolve => setTimeout(resolve, 1))
  return Response.json({
    ok: true,
    path: new URL(request.url).pathname,
  })
})
`)
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "**"
  js:
    exec: "index.js"
`)

	httpServer := server.StartHTTPServer("org1.example.com")
	defer httpServer.Close()

	client := &http.Client{Timeout: 2 * time.Second}
	const concurrency = 64
	const rounds = 8

	errCh := make(chan error, concurrency*rounds)
	var wg sync.WaitGroup
	for i := 0; i < concurrency*rounds; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resp, err := client.Get(httpServer.URL + "/repo1/stress-" + strconv.Itoa(i))
			if err != nil {
				errCh <- err
				return
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				errCh <- err
				return
			}
			if resp.StatusCode != http.StatusOK {
				errCh <- assert.AnError
				t.Logf("unexpected status=%d body=%s", resp.StatusCode, string(body))
				return
			}
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent simple HTTP requests did not complete in time")
	}
	close(errCh)
	for err := range errCh {
		assert.NoError(t, err)
	}
}

func Test_GoJa_StressConcurrentCancelledHTTP(t *testing.T) {
	if os.Getenv("GOJA_STRESS") != "1" {
		t.Skip("set GOJA_STRESS=1 to run goja stress tests")
	}
	server := testcore.NewDefaultTestServer()
	defer server.Close()
	server.AddFile("org1/repo1/gh-pages/index.html", "hello world")
	server.AddFile("org1/repo1/gh-pages/index.js", `
serve(async function() {
  await new Promise(resolve => setTimeout(resolve, 50))
  return new Response("ok")
})
`)
	server.AddFile("org1/repo1/gh-pages/.pages.yaml", `
routes:
- path: "**"
  js:
    exec: "index.js"
`)

	httpServer := server.StartHTTPServer("org1.example.com")
	defer httpServer.Close()

	client := &http.Client{}
	const concurrency = 64

	errCh := make(chan error, concurrency)
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, httpServer.URL+"/repo1/cancel-"+strconv.Itoa(i), nil)
			if err != nil {
				errCh <- err
				return
			}
			resp, err := client.Do(req)
			if resp != nil && resp.Body != nil {
				_, _ = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}
			if err == nil {
				errCh <- assert.AnError
				t.Logf("expected cancellation for request %d", i)
			}
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent cancelled HTTP requests did not complete in time")
	}
	close(errCh)
	for err := range errCh {
		assert.NoError(t, err)
	}
}
