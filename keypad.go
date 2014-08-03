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
)

const (
	ButtonPress   = 3
	ButtonRelease = 4

	// Valid states for an LED on a keypad.
	LedOff         = 0
	LedOn          = 1
	LedNormalFlash = 2 // 1 flash every second.
	LedRapidFlash  = 3 // 10 flashes every second.
	LedUndefined   = 255

	// IDs of buttons on a Pico remote "keypad".
	PicoButtonOn     = 2
	PicoButtonPreset = 3
	PicoButtonOff    = 4
	PicoButtonRaise  = 5
	PicoButtonLower  = 6
)

type Keypad struct {
	Component

	mu      sync.Mutex
	buttons []keypadMonitor
	pressed []keypadMonitor
	leds    []*ledMonitor
	pending []ledMonitor
}

type keypadMonitor struct {
	id     uint8
	events uint8
	signal chan uint8
}

type ledMonitor struct {
	keypadMonitor
	state uint8
	valid bool
}

type KeypadButton struct {
	k  *Keypad
	id uint8
}

// Create a reference to a button on the keypad. Buttons are numbered 1-N.
// See integration guide for mapping, e.g. 1B keypads use only button 4.
func (k *Keypad) Button(button uint8) *KeypadButton {
	return &KeypadButton{k, button}
}

// Press the button on the keypad by sending ButtonPress immediately
// followed by ButtonRelease. The returned channel is signaled once
// with ButtonRelease when the repeater has acknowledged the action.
//
// If the application is monitoring the button the monitoring channel(s)
// will also be signaled as the repeater acknowledges the action.
func (b *KeypadButton) Press() chan uint8 {
	k := b.k
	k.mu.Lock()
	defer k.mu.Unlock()

	c := make(chan uint8, 1)
	m := keypadMonitor{id: b.id, events: 1 << ButtonRelease, signal: c}
	k.pressed = append(k.pressed, m)
	k.Execute(fmt.Sprintf("%d,%d", b.id, ButtonPress))
	k.Execute(fmt.Sprintf("%d,%d", b.id, ButtonRelease))
	return c
}

// Set the state of a button's LED to LedOn, LedOff, LedNormalFlash
// or LedRapidFlash. LED states can only be set if the button is
// unconfigured in the RadioRA2 software.
func (b *KeypadButton) SetLed(state uint8) chan uint8 {
	k := b.k
	k.mu.Lock()
	defer k.mu.Unlock()

	m := ledMonitor{
		keypadMonitor: keypadMonitor{
			id:     b.id,
			signal: make(chan uint8, 1)},
		state: state}
	k.pending = append(k.pending, m)
	k.Execute(fmt.Sprintf("%d,9,%d", 80+b.id, state))
	return m.signal
}

// Creates a new channel receiving ButtonPress each time the button
// is pressed. ButtonRelease events are not sent.
func (b *KeypadButton) Monitor() chan uint8 {
	k := b.k
	m := keypadMonitor{
		id:     b.id,
		events: 1 << ButtonPress,
		signal: make(chan uint8, 5)}

	k.mu.Lock()
	defer k.mu.Unlock()
	k.buttons = append(k.buttons, m)
	return m.signal
}

// Creates a new channel receiving ButtonPress followed by ButtonRelease
// each time the button is pressed. The common usage is to monitor only
// ButtonPress with Monitor() as most callers do not need to observe
// ButtonRelease.
func (b *KeypadButton) MonitorButton() chan uint8 {
	k := b.k
	m := keypadMonitor{
		id:     b.id,
		events: (1 << ButtonPress) | (1 << ButtonRelease),
		signal: make(chan uint8, 10)}

	k.mu.Lock()
	defer k.mu.Unlock()
	k.buttons = append(k.buttons, m)
	return m.signal
}

// Creates a new channel receiving LED state change events. Monitoring LEDs
// can be a useful way to react when a specific scene is selected or lights
// in a room are turned on or turned off.  If no events are selected LedOff
// and LedOn will be selected by default.
func (b *KeypadButton) MonitorLed(events ...uint8) chan uint8 {
	var mask uint8 = 0
	if len(events) == 0 {
		mask = (1 << LedOff) | (1 << LedOn)
	} else {
		for _, e := range events {
			mask = mask | uint8(1<<e)
		}
	}

	k := b.k
	m := &ledMonitor{keypadMonitor: keypadMonitor{
		id:     b.id,
		events: mask,
		signal: make(chan uint8, 5)}}

	k.mu.Lock()
	defer k.mu.Unlock()

	for _, e := range k.leds {
		if e.valid && e.id == m.id {
			m.state = e.state
			m.valid = true
			m.signal <- e.state
		}
	}

	if !m.valid {
		k.Query(fmt.Sprintf("%d,9", 80+m.id))
	}
	k.leds = append(k.leds, m)
	return m.signal
}

func (k *Keypad) handleEvent(event string) error {
	n := strings.Split(event, ",")
	c, err := strconv.Atoi(n[0])
	if err != nil {
		return err
	}

	if 1 <= c && c <= 25 && len(n) == 2 {
		// Button press or release on keypad.
		action, err := strconv.Atoi(n[1])
		if err != nil {
			return err
		}
		k.handleButton(uint8(c), uint8(action))
	} else if 81 <= c && c <= 95 && len(n) == 3 && n[1] == "9" {
		// LED state change on keypad.
		state, err := strconv.Atoi(n[2])
		if err != nil {
			return err
		}
		k.handleLed(uint8(c-80), uint8(state))
	} else {
		log.Printf("keypad %d ignoring %s", k.Id, event)
	}
	return nil
}

func (k *Keypad) handleButton(button, action uint8) {
	k.mu.Lock()
	defer k.mu.Unlock()

	for _, b := range k.buttons {
		if b.id == button && b.events&(1<<action) != 0 {
			b.signal <- action
		}
	}

	var r []keypadMonitor = nil
	for _, b := range k.pressed {
		if b.id == button && b.events&(1<<action) != 0 {
			b.signal <- action
			close(b.signal)
		} else {
			r = append(r, b)
		}
	}
	k.pressed = r
}

func (k *Keypad) handleLed(led, state uint8) {
	k.mu.Lock()
	defer k.mu.Unlock()

	for _, e := range k.leds {
		if e.id == led && e.events&(1<<state) != 0 {
			if !e.valid || e.state != state {
				e.signal <- state
				e.state = state
				e.valid = true
			}
		}
	}

	var r []ledMonitor = nil
	for _, b := range k.pending {
		if b.id == led && b.state == state {
			b.signal <- state
			close(b.signal)
		} else {
			r = append(r, b)
		}
	}
	k.pending = r
}

func (k *Keypad) reconnect() {
	k.mu.Lock()
	defer k.mu.Unlock()

	// Key presses sent before connection lost have unknown results.
	// Signal waiters to prevent deadlocking the integration.
	for _, m := range k.pressed {
		m.signal <- 0
		close(m.signal)
	}
	k.pressed = nil

	// Query LED states as they may have changed before reconnect.
	p := 0
	for _, l := range k.leds {
		m := 1 << l.id
		if p&m == 0 {
			p = p | m
			k.Query(fmt.Sprintf("%d,9", 80+l.id))
		}
	}
}
