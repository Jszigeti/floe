package inventory

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

const caskFixture = `[
 {"token": "google-chrome", "artifacts": [{"app": ["Google Chrome.app"]}, {"zap": [{"trash": ["~/Library/Caches/Google"]}]}]},
 {"token": "logi-options+", "artifacts": [{"pkg": ["LogiOptionsPlusInstaller.pkg"]}]},
 {"token": "renamed", "artifacts": [{"app": ["Source/Foo.app", {"target": "Bar.app"}]}]},
 {"token": "google-chrome@dev", "artifacts": [{"app": ["Google Chrome.app"]}]}
]`

func serve(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestLoadCasksMapsAppArtifacts(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "state", "cask.json")
	srv := serve(t, http.StatusOK, caskFixture)

	got, err := LoadCasks(context.Background(), cache, srv.URL, 7*24*time.Hour, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	want := CaskIndex{"Google Chrome.app": "google-chrome", "Bar.app": "renamed"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if _, err := os.Stat(cache); err != nil {
		t.Errorf("cache not written: %v", err)
	}
}

func TestLoadCasksKeepsCacheOnFailure(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "cask.json")
	writeFile(t, cache, caskFixture)
	old := time.Now().Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(cache, old, old); err != nil {
		t.Fatal(err)
	}
	srv := serve(t, http.StatusInternalServerError, "boom")

	got, err := LoadCasks(context.Background(), cache, srv.URL, 7*24*time.Hour, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	if got["Google Chrome.app"] != "google-chrome" {
		t.Fatalf("stale cache not used: %v", got)
	}
	if data, _ := os.ReadFile(cache); string(data) != caskFixture {
		t.Errorf("cache overwritten: %s", data)
	}
}

func TestLoadCasksSkipsRefreshWhileFresh(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "cask.json")
	writeFile(t, cache, caskFixture)
	srv := serve(t, http.StatusOK, `[]`)

	got, err := LoadCasks(context.Background(), cache, srv.URL, 7*24*time.Hour, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	if got["Google Chrome.app"] != "google-chrome" {
		t.Fatalf("fresh cache refreshed anyway: %v", got)
	}
}

func TestLoadCasksWithoutCacheOrNetworkIsEmpty(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "cask.json")

	got, err := LoadCasks(context.Background(), cache, "http://127.0.0.1:1/cask.json", 7*24*time.Hour, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 0 {
		t.Fatalf("want an empty index, got %v", got)
	}
}
