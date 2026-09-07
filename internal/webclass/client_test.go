package webclass

import (
	"fmt"
	"io"
	"net/http/httptest"
	"net/url"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRequestIntervalSerializesAndDelaysRequests(t *testing.T) {
	var active int32
	var maxActive int32
	var mu sync.Mutex
	var starts []time.Time

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := atomic.AddInt32(&active, 1)
		for {
			old := atomic.LoadInt32(&maxActive)
			if current <= old || atomic.CompareAndSwapInt32(&maxActive, old, current) {
				break
			}
		}
		mu.Lock()
		starts = append(starts, time.Now())
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		atomic.AddInt32(&active, -1)
		fmt.Fprint(w, "ok")
	}))
	defer server.Close()

	base, err := url.Parse(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{Base: base, HTTP: server.Client()}
	client.SetRequestInterval(50 * time.Millisecond)

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := client.get(server.URL + "/resource")
			if err != nil {
				errs <- err
				return
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	if got := atomic.LoadInt32(&maxActive); got != 1 {
		t.Fatalf("max concurrent requests = %d, want 1", got)
	}
	mu.Lock()
	gotStarts := append([]time.Time(nil), starts...)
	mu.Unlock()
	if len(gotStarts) != 2 {
		t.Fatalf("request starts = %d, want 2", len(gotStarts))
	}
	sort.Slice(gotStarts, func(i, j int) bool { return gotStarts[i].Before(gotStarts[j]) })
	if gap := gotStarts[1].Sub(gotStarts[0]); gap < 60*time.Millisecond {
		t.Fatalf("request start gap = %s, expected handler time plus quiet interval", gap)
	}
}
