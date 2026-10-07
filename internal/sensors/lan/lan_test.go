package lan

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/MateEke/picture-frame/internal/config"
	"github.com/MateEke/picture-frame/internal/sensors"
)

func testSource(t *testing.T, up map[string]bool) *Source {
	t.Helper()
	old := pingHost
	t.Cleanup(func() { pingHost = old })
	pingHost = func(_ context.Context, host string) bool { return up[host] }
	s, err := New(slog.New(slog.DiscardHandler), config.SensorConfig{
		ID:    "phones",
		Type:  "lan",
		Hosts: []string{"192.168.1.50", "192.168.1.51"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func nextReading(t *testing.T, s *Source) sensors.Reading {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out := make(chan sensors.Reading, 1)
	go s.check(ctx, out)
	select {
	case r := <-out:
		return r
	case <-ctx.Done():
		t.Fatal("no reading emitted")
		return sensors.Reading{}
	}
}

// Any reachable host = home (motion keepalive).
func TestAnyHostUpIsHome(t *testing.T) {
	s := testSource(t, map[string]bool{"192.168.1.50": false, "192.168.1.51": true})
	r := nextReading(t, s)
	if r.Kind != sensors.KindMotion || r.Value != 1 {
		t.Errorf("got %s=%v, want motion=1", r.Kind, r.Value)
	}
}

// All unreachable = away (motion=0, ignored by the display policy).
func TestAllDownIsAway(t *testing.T) {
	s := testSource(t, map[string]bool{})
	r := nextReading(t, s)
	if r.Kind != sensors.KindMotion || r.Value != 0 {
		t.Errorf("got %s=%v, want motion=0", r.Kind, r.Value)
	}
}
