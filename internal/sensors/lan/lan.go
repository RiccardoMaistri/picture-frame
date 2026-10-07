package lan

import (
	"context"
	"log/slog"
	"os/exec"
	"time"

	"github.com/MateEke/picture-frame/internal/config"
	"github.com/MateEke/picture-frame/internal/sensors"
)

const (
	defaultPollInterval = 30 * time.Second
	pingTimeout         = 2 * time.Second
)

// pingHost reports whether host answers one ICMP echo. exec ping needs no
// raw-socket capability, unlike a native Go pinger. Overridable in tests.
var pingHost = func(ctx context.Context, host string) bool {
	ctx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	// Linux (prod) flags; the context timeout is the backstop elsewhere.
	err := exec.CommandContext(ctx, "ping", "-c1", "-W1", host).Run()
	return err == nil
}

// Source implements sensors.Source for LAN presence: it pings static phone
// IPs and emits motion=1 when any host answers (display keepalive), motion=0
// otherwise (ignored by the display policy, kept for overlay/debug). Grace on
// missed pings (sleeping phones) comes from display.blank_after, not from here.
type Source struct {
	id       string
	hosts    []string
	interval time.Duration
	log      *slog.Logger
}

func New(log *slog.Logger, cfg config.SensorConfig) (*Source, error) {
	interval := defaultPollInterval
	if cfg.PollInterval.Duration > 0 {
		interval = cfg.PollInterval.Duration
	}
	return &Source{
		id:       cfg.ID,
		hosts:    append([]string(nil), cfg.Hosts...),
		interval: interval,
		log:      log,
	}, nil
}

func (s *Source) ID() string { return s.id }

func (s *Source) Start(ctx context.Context, out chan<- sensors.Reading) error {
	s.check(ctx, out)
	t := time.NewTicker(s.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			s.check(ctx, out)
		}
	}
}

func (s *Source) check(ctx context.Context, out chan<- sensors.Reading) {
	home := false
	for _, h := range s.hosts {
		if pingHost(ctx, h) {
			home = true
			break
		}
	}
	var v float64
	if home {
		v = 1
	}
	select {
	case out <- sensors.Reading{DeviceID: s.id, Kind: sensors.KindMotion, Value: v, Timestamp: time.Now()}:
	case <-ctx.Done():
	}
}
