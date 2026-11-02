package slideshow

import (
	"context"
	"log/slog"
	"math"
	"slices"
	"sync"
	"time"

	"github.com/MateEke/picture-frame/internal/library"
	"github.com/MateEke/picture-frame/internal/slideplan"
	"github.com/MateEke/picture-frame/internal/state"
)

// Slideshow publishes slide events to the bus on a configurable interval. Slides
// come from the planner (a solo image, or a split-screen pair). Call Next to
// advance immediately and reset the timer.
type Slideshow struct {
	log      *slog.Logger
	lib      *library.Library
	planner  *slideplan.Planner
	bus      *state.Bus
	mu       sync.Mutex
	interval time.Duration
	advance  chan struct{}
	back     chan struct{}
	restart  chan struct{}
}

func New(log *slog.Logger, lib *library.Library, planner *slideplan.Planner, bus *state.Bus, interval time.Duration) *Slideshow {
	return &Slideshow{
		log:      log,
		lib:      lib,
		planner:  planner,
		bus:      bus,
		interval: interval,
		advance:  make(chan struct{}, 1),
		back:     make(chan struct{}, 1),
		restart:  make(chan struct{}, 1),
	}
}

// SetInterval updates the advance interval and triggers an immediate advance so the
// new duration takes effect right away rather than waiting out the remaining tick.
func (s *Slideshow) SetInterval(d time.Duration) {
	s.mu.Lock()
	s.interval = d
	s.mu.Unlock()
	select {
	case s.advance <- struct{}{}:
	default:
	}
}

// SetRandomize toggles random ordering; an actual change restarts the cycle.
func (s *Slideshow) SetRandomize(enabled bool) {
	if s.lib.SetRandomize(enabled) {
		s.RestartCycle()
	}
}

// RestartCycle starts a fresh cycle now (reshuffled when randomized, sequential
// otherwise) from the first slide. Safe from any goroutine.
func (s *Slideshow) RestartCycle() {
	select {
	case s.restart <- struct{}{}:
	default:
	}
}

// SetSplitConfig updates split-screen pairing (enable + threshold) and rebuilds
// the slide plan. Used for live config reload.
func (s *Slideshow) SetSplitConfig(enabled bool, thr slideplan.Threshold) {
	s.planner.SetConfig(enabled, thr)
	select {
	case s.advance <- struct{}{}:
	default:
	}
}

// Replan marks the slide plan stale so the next slide is grouped from the
// library's current order and album details; the cursor is kept.
func (s *Slideshow) Replan() {
	s.planner.Invalidate()
}

// Next advances immediately and resets the timer. Safe from any goroutine.
func (s *Slideshow) Next() {
	select {
	case s.advance <- struct{}{}:
	default:
	}
}

// Prev steps back immediately and resets the timer. Safe from any goroutine.
func (s *Slideshow) Prev() {
	select {
	case s.back <- struct{}{}:
	default:
	}
}

// Run publishes the current slide immediately (so SSE clients don't wait for
// the first tick), then advances on the interval. The ticker is paused while
// the library is empty or the panel is off, and resumed by an advance, a step
// back, or the panel waking.
func (s *Slideshow) Run(ctx context.Context) {
	// Track panel power from the bus. Subscribe before reading the snapshot so
	// no transition falls between the two; the snapshot's highest ID then marks
	// the events already reflected in the seed, which the loop skips.
	screenCh, unsub := s.bus.Subscribe()
	defer unsub()
	screenOn, seedID := s.screenAtStartup()

	if slide := s.servable(s.planner.Current(), s.planner.Next); slide != nil {
		// Publish even when the panel is off so the kiosk has a frame ready to
		// show on wake; only the dwell below is gated.
		s.publish(slide)
	}

	s.mu.Lock()
	interval := s.interval
	s.mu.Unlock()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// armed reports whether the dwell should run: there are slides to show and
	// the panel is on. A nil tickerC is never ready in select, which pauses the
	// dwell without disturbing the plan or the cursor.
	armed := func() bool { return s.lib.Len() > 0 && screenOn }
	// restart gives the next slide a full interval, unless the dwell is paused;
	// a paused ticker stays stopped.
	restart := func() <-chan time.Time {
		if !armed() {
			ticker.Stop()
			return nil
		}
		return s.restartDwell(ticker)
	}

	var tickerC <-chan time.Time
	if armed() {
		tickerC = ticker.C
	} else {
		ticker.Stop()
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-tickerC:
			if slide := s.servable(s.planner.Next(), s.planner.Next); slide != nil {
				s.publish(slide)
				continue
			}
			// Library went empty between ticks; pause until next advance.
			ticker.Stop()
			tickerC = nil
		case <-s.advance:
			tickerC = restart()
			if slide := s.servable(s.planner.Next(), s.planner.Next); slide != nil {
				s.publish(slide)
			}
		case <-s.back:
			tickerC = restart()
			if slide := s.servable(s.planner.Prev(), s.planner.Prev); slide != nil {
				s.publish(slide)
			}
		case <-s.restart:
			s.planner.RestartCycle()
			if slide := s.servable(s.planner.Current(), s.planner.Next); slide != nil {
				tickerC = restart()
				s.publish(slide)
			} else {
				ticker.Stop()
				tickerC = nil
			}
		case event, ok := <-screenCh:
			if !ok {
				return
			}
			if event.ID <= seedID {
				continue // already folded into the startup snapshot
			}
			on, isScreen := screenPower(event)
			if !isScreen || on == screenOn {
				continue
			}
			screenOn = on
			if !screenOn {
				// Nobody can see the frame: stop decoding and fetching slides.
				ticker.Stop()
				tickerC = nil
				s.log.Debug("slideshow: paused (screen off)")
				continue
			}
			// Wake: give the visible slide a full dwell before advancing.
			tickerC = restart()
			s.log.Debug("slideshow: resumed (screen on)")
		}
	}
}

// screenAtStartup seeds the pause gate from the bus's retained events. It
// subscribes before this is called, so the highest snapshot ID bounds which
// channel events are already reflected in the result. Defaults to on when no
// screen event has been published yet (Run starts before the policy settles).
func (s *Slideshow) screenAtStartup() (on bool, seedID uint64) {
	on = true
	for _, e := range s.bus.Snapshot() {
		if e.ID > seedID {
			seedID = e.ID
		}
		if power, ok := screenPower(e); ok {
			on = power
		}
	}
	return on, seedID
}

// screenPower reports the panel's live power from a screen event.
func screenPower(e state.Event) (on bool, ok bool) {
	sp, ok := e.Payload.(state.ScreenPayload)
	if !ok || e.Kind != state.KindScreen {
		return false, false
	}
	return sp.On, true
}

// restartDwell gives the new slide a full interval and unpauses the ticker.
func (s *Slideshow) restartDwell(ticker *time.Ticker) <-chan time.Time {
	s.mu.Lock()
	interval := s.interval
	s.mu.Unlock()
	ticker.Reset(interval)
	return ticker.C
}

// servable skips slides referencing an image deleted since the plan was built so
// the kiosk never 404s. The step follows the direction of travel, so a back tap
// keeps moving back. Skipping a whole plan forces a wrap, which rebuilds from the
// current library. Empty library yields nil to pause.
func (s *Slideshow) servable(slide *slideplan.Slide, step func() *slideplan.Slide) *slideplan.Slide {
	for i, limit := 0, s.planner.SlideCount()+1; slide != nil && i < limit && !s.allPresent(slide); i++ {
		slide = step()
	}
	return slide
}

func (s *Slideshow) allPresent(slide *slideplan.Slide) bool {
	for _, name := range slide.Names {
		if !s.lib.Has(name) {
			return false
		}
	}
	return true
}

func (s *Slideshow) publish(slide *slideplan.Slide) {
	album, year := s.slideDetails(slide)
	s.bus.Publish(state.Event{
		Kind:    state.KindImage,
		Payload: state.ImagePayload{Names: slide.Names, Album: album, Year: year, Next: s.preloadHint(slide)},
	})
	s.log.Debug("slideshow: displaying slide", "names", slide.Names, "album", album, "year", year)
}

// slideDetails returns the slide's album and earliest known year, read from
// the library at publish time. The plan's own album is only a fallback: the
// plan can be built from cached files before the syncer has attached details,
// and it keeps that empty album until the next rebuild.
func (s *Slideshow) slideDetails(slide *slideplan.Slide) (string, int) {
	album := slide.Album
	if s.lib == nil || len(slide.Names) == 0 {
		return album, 0
	}
	minYear := math.MaxInt
	cycle := s.lib.Cycle()
	for _, name := range slide.Names {
		for _, img := range cycle {
			if img.Name == name {
				if album == "" {
					album = img.Album
				}
				if img.Year > 0 && img.Year < minYear {
					minYear = img.Year
				}
				break
			}
		}
	}
	if minYear == math.MaxInt {
		return album, 0
	}
	return album, minYear
}

// preloadHint peeks at the slide after the published one so the kiosk can
// fetch+decode it while the current slide is on screen. It returns nil when
// the hint would be useless: same slide (single-slide plan), or any image
// missing from the library (a stale plan entry that servable would skip).
func (s *Slideshow) preloadHint(current *slideplan.Slide) []string {
	peek := s.planner.PeekNext()
	if peek == nil || slices.Equal(peek.Names, current.Names) {
		return nil
	}
	for _, name := range peek.Names {
		if !s.lib.Has(name) {
			return nil
		}
	}
	return peek.Names
}
