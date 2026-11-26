package exec

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http/httptrace"
	"strings"
	"sync"
	"time"
)

// Timing is the phase breakdown of one HTTP exchange, recorded with
// net/http/httptrace. It describes the final hop when redirects are followed.
//
// A zero phase means the phase did not happen: DNS is skipped for IP
// literals and reused connections, Connect and TLS are skipped for reused
// connections, and TLS is skipped for plain http.
type Timing struct {
	DNS      time.Duration // name lookup
	Connect  time.Duration // TCP connect
	TLS      time.Duration // TLS handshake
	TTFB     time.Duration // request written to first response byte
	Download time.Duration // first response byte to end of body
	// Reused reports that the request went over an idle kept-alive connection.
	Reused bool
}

// IsZero reports whether no phase was recorded (for example, a test
// transport that does not fire httptrace hooks).
func (t Timing) IsZero() bool {
	return t == Timing{}
}

// Compact formats the recorded phases on one line, e.g.
// "dns 2ms  connect 1ms  tls 12ms  ttfb 40ms  download 3ms". Phases that
// did not happen are left out; a reused connection is noted.
func (t Timing) Compact() string {
	if t.IsZero() {
		return ""
	}
	parts := make([]string, 0, 6)
	add := func(label string, d time.Duration) {
		if d > 0 {
			parts = append(parts, label+" "+formatPhase(d))
		}
	}
	add("dns", t.DNS)
	add("connect", t.Connect)
	add("tls", t.TLS)
	add("ttfb", t.TTFB)
	add("download", t.Download)
	if t.Reused {
		parts = append(parts, "reused conn")
	}
	return strings.Join(parts, "  ")
}

// formatPhase rounds to a readable precision: whole milliseconds from 10ms,
// tenths below that, and microseconds below 1ms.
func formatPhase(d time.Duration) string {
	switch {
	case d >= 10*time.Millisecond:
		return d.Round(time.Millisecond).String()
	case d >= time.Millisecond:
		return fmt.Sprintf("%.1fms", float64(d)/float64(time.Millisecond))
	default:
		return d.Round(time.Microsecond).String()
	}
}

// timingRecorder collects httptrace callbacks. The transport may call DNS
// and connect hooks from dialer goroutines, so every field is guarded.
type timingRecorder struct {
	mu sync.Mutex

	dnsStart, connectStart, tlsStart, wroteRequest, firstByte time.Time
	timing                                                    Timing
}

// withTrace returns ctx carrying an httptrace.ClientTrace that feeds r.
func (r *timingRecorder) withTrace(ctx context.Context) context.Context {
	return httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		GetConn: func(string) {
			// Each redirect hop starts a new connection attempt; keep only
			// the last hop so the phases describe the response returned.
			r.mu.Lock()
			defer r.mu.Unlock()
			r.timing = Timing{}
			r.dnsStart, r.connectStart, r.tlsStart = time.Time{}, time.Time{}, time.Time{}
			r.wroteRequest, r.firstByte = time.Time{}, time.Time{}
		},
		GotConn: func(info httptrace.GotConnInfo) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.timing.Reused = info.Reused
		},
		DNSStart: func(httptrace.DNSStartInfo) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.dnsStart = time.Now()
		},
		DNSDone: func(httptrace.DNSDoneInfo) {
			r.mu.Lock()
			defer r.mu.Unlock()
			if !r.dnsStart.IsZero() {
				r.timing.DNS = time.Since(r.dnsStart)
			}
		},
		ConnectStart: func(string, string) {
			r.mu.Lock()
			defer r.mu.Unlock()
			// With several addresses the dialer may race attempts; time
			// from the first start to the winning connection.
			if r.connectStart.IsZero() {
				r.connectStart = time.Now()
			}
		},
		ConnectDone: func(_, _ string, err error) {
			r.mu.Lock()
			defer r.mu.Unlock()
			if err == nil && !r.connectStart.IsZero() {
				r.timing.Connect = time.Since(r.connectStart)
			}
		},
		TLSHandshakeStart: func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.tlsStart = time.Now()
		},
		TLSHandshakeDone: func(_ tls.ConnectionState, err error) {
			r.mu.Lock()
			defer r.mu.Unlock()
			if err == nil && !r.tlsStart.IsZero() {
				r.timing.TLS = time.Since(r.tlsStart)
			}
		},
		WroteRequest: func(httptrace.WroteRequestInfo) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.wroteRequest = time.Now()
		},
		GotFirstResponseByte: func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.firstByte = time.Now()
			if !r.wroteRequest.IsZero() {
				r.timing.TTFB = r.firstByte.Sub(r.wroteRequest)
			}
		},
	})
}

// finish records the download phase as ending now and returns the result.
func (r *timingRecorder) finish() Timing {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.firstByte.IsZero() {
		r.timing.Download = time.Since(r.firstByte)
	}
	return r.timing
}
