package providers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLocalProviderOverlayConcurrentAccess(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.html"), []byte("disk"), 0o644))

	provider := NewLocalProvider(map[string][]string{
		"org": {"repo"},
	}, dir)

	var wg sync.WaitGroup
	errCh := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 64; j++ {
				provider.AddOverlay("index.html", []byte("overlay"))
				resp, err := provider.Open(context.Background(), "org", "repo", "id", "index.html", http.Header{})
				if err != nil {
					errCh <- err
					return
				}
				body, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				if err != nil {
					errCh <- err
					return
				}
				if string(body) != "overlay" {
					errCh <- fmt.Errorf("unexpected body %q", string(body))
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}
}
