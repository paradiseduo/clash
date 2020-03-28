package provider

import (
	"context"
	"time"

	C "github.com/Dreamacro/clash/constant"
)

const (
	defaultURLTestTimeout = time.Second * 5
	defaultURLTestURL     = "https://www.gstatic.com/generate_204"
)

type HealthCheckOption struct {
	URL      string
	Interval uint
}

type HealthCheck struct {
	url      string
	proxies  []C.Proxy
	interval uint
	done     chan struct{}
}

func (hc *HealthCheck) process() {
	ticker := time.NewTicker(time.Duration(hc.interval) * time.Second)

	go hc.check()
	for {
		select {
		case <-ticker.C:
			hc.check()
		case <-hc.done:
			ticker.Stop()
			return
		}
	}
}

func (hc *HealthCheck) setProxy(proxies []C.Proxy) {
	hc.proxies = proxies
}

func (hc *HealthCheck) auto() bool {
	return hc.interval != 0
}

func (hc *HealthCheck) check() {
	ctx, cancel := context.WithTimeout(context.Background(), defaultURLTestTimeout)
	proxies := hc.proxies
	count := len(proxies)
	result := make(chan struct{}, count)

	defer cancel()

	for _, proxy := range proxies {
		go func() {
			_, _ = proxy.URLTest(ctx, hc.url)
			result <- struct{}{}
		}()
	}

	for count > 0 {
		select {
		case <-ctx.Done():
			return
		case <-result:
			count--
		}
	}
}

func (hc *HealthCheck) close() {
	hc.done <- struct{}{}
}

func NewHealthCheck(proxies []C.Proxy, url string, interval uint) *HealthCheck {
	if url == "" {
		url = defaultURLTestURL
	}

	return &HealthCheck{
		proxies:  proxies,
		url:      url,
		interval: interval,
		done:     make(chan struct{}, 1),
	}
}
