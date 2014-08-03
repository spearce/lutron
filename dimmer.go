// Copyright 2014 Google. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lutron

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DefaultFade = 2 * time.Second
)

// Maestro style dimmer (RRD-6CL, RRD-6NA, RRD-10ND, ...).
//
// Switches (RRD-8ANS) are also supported, but may be better accessed
// using the simplified Switch wrapper type.
type Dimmer struct {
	Component

	mu       sync.Mutex
	level    uint8
	valid    bool
	querying bool
	fade     *time.Duration
	readers  []chan uint8
	monitors []chan LevelChange
	pending  []adjustDimmer
}

type LevelChange struct {
	// Dimmer (or switch) whose lighting load has changed.
	Dimmer *Dimmer

	// New level, 0 (off) to 100 (fully on).
	Level uint8
}

type adjustDimmer struct {
	level uint8
	fade  time.Duration
	reply chan uint8
}

// Raise the dimmer to on (100%), sending the new level when acknowledged.
func (d *Dimmer) On() chan uint8 {
	return d.SetLevel(100)
}

// Fade the dimmer to off (0%), sending the new level when acknowledged.
func (d *Dimmer) Off() chan uint8 {
	return d.SetLevel(0)
}

// Set the level (0-100), sending the new level on the returned
// channel when the main repeater has acknowledged it.
func (d *Dimmer) SetLevel(level uint8) chan uint8 {
	return d.Fade(level, d.DefaultFade())
}

// Set the level (0-100) over the fade duration, sending the new level
// on the returned channel when the main repeater has acknowledged it.
func (d *Dimmer) Fade(level uint8, fade time.Duration) chan uint8 {
	c := make(chan uint8, 1)
	d.mu.Lock()
	defer d.mu.Unlock()

	// Repeater won't acknowledge the level change if the dimmer
	// is already at the requested level. Arrange to only send a
	// level change if there is a difference.

	if d.valid && d.level == level && len(d.pending) == 0 {
		c <- level
		close(c)
		return c
	}

	p := adjustDimmer{level: level, fade: fade, reply: c}
	if !d.valid {
		d.query()
	} else if len(d.pending) == 0 {
		d.setLevel(p)
	}
	d.pending = append(d.pending, p)
	return c
}

// Get the duration used to adjust the lighting level.
func (d *Dimmer) DefaultFade() time.Duration {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.fade != nil {
		return *d.fade
	}
	return DefaultFade
}

// Set the duration for adjusting the lighting level.
func (d *Dimmer) SetDefaultFade(fade time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.fade = &fade
}

// Creates a new channel receiving updates when the dimmer is adjusted.
func (d *Dimmer) Monitor() chan LevelChange {
	c := make(chan LevelChange, 5)
	d.AddMonitor(c)
	return c
}

// Adds a channel to receive updates when the dimmer is adjusted.
func (d *Dimmer) AddMonitor(c chan LevelChange) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.monitors = append(d.monitors, c)
	if d.valid {
		c <- LevelChange{d, d.level}
	} else {
		d.query()
	}
}

// Get the level of the dimmer and send it once on the returned channel.
// If the dimmer's level has not yet been observed it will be queried
// and the value will be sent after the main repeater has replied.
func (d *Dimmer) Level() chan uint8 {
	return d.readLevel(true)
}

// Get the level of the dimmer directly from the main repeater and send
// it once on the returned channel. ReadLevel() takes longer than Level()
// as the read must be performed remotely on the main repeater.
func (d *Dimmer) ReadLevel() chan uint8 {
	return d.readLevel(false)
}

func (d *Dimmer) readLevel(cached bool) chan uint8 {
	d.mu.Lock()
	defer d.mu.Unlock()

	w := make(chan uint8, 1)
	if cached && d.valid {
		w <- d.level
		close(w)
	} else {
		d.readers = append(d.readers, w)
		d.query()
	}
	return w
}

func (d *Dimmer) setLevel(p adjustDimmer) {
	d.Execute(fmt.Sprintf("1,%d,%s", p.level, formatFade(p.fade)))
}

func (d *Dimmer) query() {
	if !d.querying {
		d.querying = true
		d.Query("1")
	}
}

func (d *Dimmer) handleEvent(event string) error {
	n := strings.SplitN(event, ",", 2)
	action, err := strconv.Atoi(n[0])
	if err != nil {
		return err
	}

	switch action {
	case 1: // new target zone level 0-100 ("1,50.31")
		level, err := parseDimmerLevel(n[1])
		if err != nil {
			return err
		}
		d.handleLevel(uint8(level))

	case 29: // zone has reached target level ("29,0")
		break

	default:
		log.Printf("dimmer %d ignoring %s", d.Id, event)
	}
	return nil
}

func (d *Dimmer) handleLevel(level uint8) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for _, c := range d.readers {
		c <- level
		close(c)
	}
	d.readers = nil
	d.querying = false

	if !d.valid || d.level != level {
		for _, c := range d.monitors {
			c <- LevelChange{d, level}
		}
		d.level = level
		d.valid = true
	}

	next := len(d.pending)
	for i, p := range d.pending {
		if p.level == level {
			p.reply <- level
			close(p.reply)
		} else {
			next = i
			break
		}
	}
	if next < len(d.pending) {
		d.pending = d.pending[next:]
		d.setLevel(d.pending[0])
	} else {
		d.pending = nil
	}
}

func parseDimmerLevel(s string) (int, error) {
	i := strings.Index(s, ".")
	if i >= 0 {
		s = s[0:i]
	}
	return strconv.Atoi(s)
}

func formatFade(fade time.Duration) string {
	if fade.Minutes() >= 1 {
		mm := int(fade.Minutes())
		ss := int(fade.Seconds() - float64(mm*60))
		return fmt.Sprintf("%02d:%02d", mm, ss)
	} else {
		ss := int(fade.Seconds())
		hs := (fade.Nanoseconds()/1e6 - int64(ss*1000)) / 10
		return fmt.Sprintf("%02d.%02d", ss, hs)
	}
}

func (d *Dimmer) reconnect() {
	d.mu.Lock()
	defer d.mu.Unlock()

	for _, p := range d.pending {
		p.reply <- 0
		close(p.reply)
	}
	d.pending = nil

	if d.readers != nil || d.monitors != nil {
		d.querying = true
		d.Query("1")
	}
}
