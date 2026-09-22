package rxttelemetry

import (
	"context"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// SourceConfig is the persisted state required to start one independent RXT
// producer. Cursors must remain scoped to their endpoint.
type SourceConfig struct {
	Endpoint        string
	LastEventID     string
	LastBootID      string
	ResumeSupported bool
}

// Collection runs any number of independent RXT sources and presents their
// links as one map overlay. Each child Service retains its own health and
// history-resume state.
type Collection struct {
	mu      sync.RWMutex
	sources map[string]*Service
	cancels map[string]context.CancelFunc
	ctx     context.Context
	handler PacketHandler
	resume  ResumeHandler
	client  *http.Client
	logger  *slog.Logger
	wg      sync.WaitGroup
}

func NewCollection(configs []SourceConfig, client *http.Client, logger *slog.Logger) *Collection {
	if logger == nil {
		logger = slog.Default()
	}
	c := &Collection{
		sources: make(map[string]*Service), cancels: make(map[string]context.CancelFunc),
		client: client, logger: logger,
	}
	c.setSourcesLocked(configs)
	return c
}

func (c *Collection) SetPacketHandler(handler PacketHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handler = handler
	for _, source := range c.sources {
		source.SetPacketHandler(handler)
	}
}

func (c *Collection) SetResumeHandler(handler ResumeHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.resume = handler
	for _, source := range c.sources {
		source.SetResumeHandler(handler)
	}
}

func (c *Collection) Endpoints() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	endpoints := make([]string, 0, len(c.sources))
	for endpoint := range c.sources {
		endpoints = append(endpoints, endpoint)
	}
	sort.Strings(endpoints)
	return endpoints
}

func (c *Collection) Enabled() bool { return c != nil && len(c.Endpoints()) > 0 }

func (c *Collection) SetSources(configs []SourceConfig) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.setSourcesLocked(configs)
}

func (c *Collection) setSourcesLocked(configs []SourceConfig) {
	wanted := make(map[string]SourceConfig, len(configs))
	for _, cfg := range configs {
		cfg.Endpoint = strings.TrimSpace(cfg.Endpoint)
		if cfg.Endpoint != "" {
			wanted[cfg.Endpoint] = cfg
		}
	}
	for endpoint := range c.sources {
		if _, ok := wanted[endpoint]; ok {
			delete(wanted, endpoint)
			continue
		}
		if cancel := c.cancels[endpoint]; cancel != nil {
			cancel()
		}
		delete(c.cancels, endpoint)
		delete(c.sources, endpoint)
	}
	for endpoint, cfg := range wanted {
		source := New(endpoint, c.client, c.logger)
		source.SetResumeState(cfg.LastEventID, cfg.LastBootID, cfg.ResumeSupported)
		source.SetPacketHandler(c.handler)
		source.SetResumeHandler(c.resume)
		c.sources[endpoint] = source
		if c.ctx != nil {
			c.startLocked(endpoint, source)
		}
	}
}

func (c *Collection) startLocked(endpoint string, source *Service) {
	ctx, cancel := context.WithCancel(c.ctx)
	c.cancels[endpoint] = cancel
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		source.Run(ctx)
	}()
}

func (c *Collection) Run(ctx context.Context) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.ctx = ctx
	for endpoint, source := range c.sources {
		c.startLocked(endpoint, source)
	}
	c.mu.Unlock()
	<-ctx.Done()
	c.mu.Lock()
	for _, cancel := range c.cancels {
		cancel()
	}
	c.ctx = nil
	c.cancels = make(map[string]context.CancelFunc)
	c.mu.Unlock()
	c.wg.Wait()
}

func (c *Collection) Statuses(now time.Time) []Status {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	sources := make([]*Service, 0, len(c.sources))
	for _, source := range c.sources {
		sources = append(sources, source)
	}
	c.mu.RUnlock()
	statuses := make([]Status, 0, len(sources))
	for _, source := range sources {
		statuses = append(statuses, source.Status(now))
	}
	sort.Slice(statuses, func(i, j int) bool { return statuses[i].Endpoint < statuses[j].Endpoint })
	return statuses
}

func (c *Collection) Snapshot(now time.Time) []Link {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	sources := make([]*Service, 0, len(c.sources))
	for _, source := range c.sources {
		sources = append(sources, source)
	}
	c.mu.RUnlock()
	links := make(map[string]Link)
	for _, source := range sources {
		for _, link := range source.Snapshot(now) {
			key := link.From + ">" + link.To
			if old, ok := links[key]; !ok || link.ObservedAt.After(old.ObservedAt) {
				links[key] = link
			}
		}
	}
	out := make([]Link, 0, len(links))
	for _, link := range links {
		out = append(out, link)
	}
	return out
}
