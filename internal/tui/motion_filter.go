package tui

import (
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// motionWindow is the shortest gap between two mouse-motion events that reach
// the model. A drag reports every cell the pointer crosses, and Bubble Tea
// redraws after each message, so an unthrottled fast drag queues far more work
// than the screen can show and the selection trails the pointer.
const motionWindow = time.Second / 144

// coalescedMotion carries a held motion event back into the loop so the filter
// can tell it apart from a fresh one and let it through.
type coalescedMotion struct{ tea.MouseMsg }

// MotionCoalescer throttles mouse-motion events. The first event in a burst
// passes at once; later ones are held and only the newest is delivered when the
// window ends (at most 144 events per second), so the pointer's final position is never lost.
type MotionCoalescer struct {
	mu      sync.Mutex
	send    func(tea.Msg)
	last    time.Time
	pending *tea.MouseMsg
	timer   *time.Timer
	now     func() time.Time
}

// NewMotionCoalescer returns a throttle for mouse-motion events. Bind must be
// called with the program's Send before it holds any event back.
func NewMotionCoalescer() *MotionCoalescer {
	return &MotionCoalescer{now: time.Now}
}

// Bind sets the function used to deliver a held event when its window ends.
func (c *MotionCoalescer) Bind(send func(tea.Msg)) {
	c.mu.Lock()
	c.send = send
	c.mu.Unlock()
}

// Filter implements tea.WithFilter.
func (c *MotionCoalescer) Filter(_ tea.Model, msg tea.Msg) tea.Msg {
	c.mu.Lock()
	defer c.mu.Unlock()

	switch v := msg.(type) {
	case coalescedMotion:
		c.last = c.now()
		return v.MouseMsg
	case tea.MouseMsg:
		if v.Action != tea.MouseActionMotion {
			c.dropPendingLocked() // the release or press carries the final position
			return msg
		}
		if c.send == nil || c.now().Sub(c.last) >= motionWindow && c.pending == nil {
			c.last = c.now()
			return msg
		}
		c.pending = &v
		if c.timer == nil {
			wait := motionWindow - c.now().Sub(c.last)
			c.timer = time.AfterFunc(max(wait, time.Millisecond), c.flush)
		}
		return nil
	}
	return msg
}

func (c *MotionCoalescer) dropPendingLocked() {
	c.pending = nil
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
}

func (c *MotionCoalescer) flush() {
	c.mu.Lock()
	msg, send := c.pending, c.send
	c.pending, c.timer = nil, nil
	c.mu.Unlock()
	if msg != nil && send != nil {
		send(coalescedMotion{*msg})
	}
}
