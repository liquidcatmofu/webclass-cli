package webclass

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestDoContentsRedirectToStartEndLauncher(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	startCalls := 0
	endCalls := 0

	mux.HandleFunc("/webclass/do_contents.php", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><script>window.top.location.href="/webclass/launcher";</script></html>`)
	})
	mux.HandleFunc("/webclass/launcher", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			switch r.Form.Get("cmd") {
			case "開始":
				startCalls++
				fmt.Fprint(w, `<html><body>installation instructions only</body></html>`)
				return
			case "終了":
				endCalls++
				fmt.Fprint(w, `<html><body>closed</body></html>`)
				return
			}
		}
		fmt.Fprint(w, `<html><body>
<form method="POST" action="/webclass/launcher">
  <input type="hidden" name="token" value="state-token">
  <input type="submit" name="cmd" value="開始">
  <input type="submit" name="cmd" value="終了">
</form>
</body></html>`)
	})

	base, err := url.Parse(server.URL + "/webclass/")
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{Base: base, HTTP: server.Client()}

	session, err := client.OpenMaterialSession(server.URL + "/webclass/do_contents.php?reset_status=1&set_contents_id=abc")
	if err != nil {
		t.Fatal(err)
	}
	if startCalls != 1 {
		t.Fatalf("start calls = %d, want 1", startCalls)
	}
	if !session.Started || session.State != "no-files" {
		t.Fatalf("session = %#v, want started no-files material", session)
	}
	if session.Close == nil {
		t.Fatal("launcher end action must be retained after start")
	}
	if session.Close.ImpliesStarted {
		t.Fatal("launcher end action must not imply that start already happened")
	}
	if got := session.Close.Values.Get("cmd"); got != "終了" {
		t.Fatalf("close cmd = %q, want 終了", got)
	}
	if got := session.Close.Values.Get("token"); got != "state-token" {
		t.Fatalf("close token = %q, want preserved state token", got)
	}

	if err := client.CloseMaterial(session.Close); err != nil {
		t.Fatal(err)
	}
	if endCalls != 1 {
		t.Fatalf("end calls = %d, want 1", endCalls)
	}
}
