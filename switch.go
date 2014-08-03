// Copyright 2014 Google. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lutron

import "time"

// TODO(spearce) Change switch values to be bool instead of uint8.

// Maestro style switch (RRD-8ANS). Switches are modeled as dimmers
// that have only two levels, 0 (off) and 100 (on). The switch type
// is a lightweight wrapper around the dimmer to simplify its API.
//
// Maestro style dimmers (RRD-6CL, RRD-6NA, RRD-10ND, ...) are also
// supported by this type, but are limited to "instant full on" and
// "instant full off". To access the dimming controls, use Dimmer.
type Switch struct {
	dimmer *Dimmer
}
// Turn on the switched load, sending a value when acknowledged.
func (s *Switch) On() chan uint8 {
	return s.dimmer.Fade(100, 0*time.Second)
}

// Turn off the switched load, sending a value when acknowledged.
func (s *Switch) Off() chan uint8 {
	return s.dimmer.Fade(0, 0*time.Second)
}

// Creates a new channel receiving updates when the switch is adjusted.
// For a switch any non-zero level means "on", while 0 means "off".
func (s *Switch) Monitor() chan LevelChange {
	return s.dimmer.Monitor()
}

// Adds a channel to receive updates when the dimmer is adjusted.
// For a switch any non-zero level means "on", while 0 means "off".
func (s *Switch) AddMonitor(c chan LevelChange) {
	s.dimmer.AddMonitor(c)
}

// Get the status of the switch and send it once on the returned channel.
// If the switch's level has not yet been observed it will be queried
// and the value will be sent after the main repeater has replied.
// For a switch any non-zero level means "on", while 0 means "off".
func (s *Switch) IsOn() chan uint8 {
	return s.dimmer.Level()
}

// Get the status of the switch directly from the main repeater and send
// it once on the returned channel. ReadIsOn() takes longer than IsOn()
// as the read must be performed remotely on the main repeater.
// For a switch any non-zero level means "on", while 0 means "off".
func (s *Switch) ReadIsOn() chan uint8 {
	return s.dimmer.ReadLevel()
}
