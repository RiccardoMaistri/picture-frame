package slideshow_test

// End-to-end year propagation: fake Immich (API key) -> syncer (warm cache)
// -> library -> slideshow -> published ImagePayload. The kiosk overlay reads
// the year from that payload, so this test fails if the year is lost anywhere
// along the chain.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MateEke/picture-frame/internal/library"
	immich "github.com/MateEke/picture-frame/internal/library/adapter/immich"
	"github.com/MateEke/picture-frame/internal/slideshow"
	"github.com/MateEke/picture-frame/internal/state"
	"github.com/MateEke/picture-frame/internal/testutil"
)

const (
	chainAlbumID = "11111111-2222-3333-4444-555555555555"
	chainAssetID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	// Empty thumbhash maps to the sentinel version token.
	chainVersion = "0000000000000000"
	chainFile    = chainAssetID + "-" + chainVersion + ".jpg"
)

// chainImmich serves one album with one photo taken just before midnight UTC;
// the +1h offset lands the local taken date in 2023, so a 2023 label proves
// the offset was applied, not just the UTC date parsed.
func chainImmich() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		write := func(v any) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(v)
		}
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/albums/"):
			write(map[string]string{"id": chainAlbumID, "albumName": "Cina"})
		case r.URL.Path == "/api/timeline/buckets":
			write([]map[string]any{{"timeBucket": "2022-12-01", "count": 1}})
		case r.URL.Path == "/api/timeline/bucket":
			write(map[string]any{
				"id":               []string{chainAssetID},
				"isImage":          []bool{true},
				"thumbhash":        []string{""},
				"fileCreatedAt":    []string{"2022-12-31T23:30:00.000Z"},
				"localOffsetHours": []float64{1},
			})
		case strings.HasSuffix(r.URL.Path, "/thumbnail"):
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("img"))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestYearSurvivesWarmCacheToPayload(t *testing.T) {
	srv := chainImmich()
	defer srv.Close()

	// Warm cache: the file is on disk but the library starts with zero
	// details, exactly as adapter.Load provides after a reboot.
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	if err := os.WriteFile(filepath.Join(dir, chainFile), []byte("seed"), 0o600); err != nil {
		t.Fatal(err)
	}
	lib := library.New(nil, false)
	lib.Add(chainFile, "", 0)

	client, err := immich.NewAPIClient(immich.APIConfig{
		BaseURL:  srv.URL,
		APIKey:   "k",
		AlbumIDs: []string{chainAlbumID},
	})
	if err != nil {
		t.Fatal(err)
	}
	syncer := library.NewSyncer(testutil.NopLogger(), client, lib, root, time.Hour, nil)
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	syncer.Run(ctx)

	if got := lib.List(); len(got) != 1 || got[0].Year != 2023 || got[0].Album != "Cina" {
		t.Fatalf("library details = %+v, want [{Album:Cina Year:2023}]", got)
	}

	bus := state.NewBus()
	ss := slideshow.New(testutil.NopLogger(), lib, newPlannerFor(lib, unknownRatios), bus, 20*time.Millisecond)
	ch, unsub := bus.Subscribe()
	defer unsub()
	runCtx := t.Context()
	go ss.Run(runCtx)

	p := receivePayload(t, ch, 2*time.Second)
	if p.Album != "Cina" || p.Year != 2023 {
		t.Errorf("payload album/year = %q/%d, want Cina/2023 (names=%v)", p.Album, p.Year, p.Names)
	}
}
