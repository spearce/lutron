// Copyright 2014 Google. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lutron

import "fmt"

// Any RadioRA2 compatible device.
type Component struct {
	Conn    *Conn
	command string
	id      int
}

// Integration id used to address the device through the main repeater.
// Ids are assigned by the RadioRA2 software and must be looked up on
// the integration report.
func (d *Component) Id() int {
	return d.id
}

// Sends a query of the form "?<command>,<id>,<rest>" to the repeater.
// The repeater will reply in the future with "~<command>,<id>,...".
func (d *Component) Query(rest string) {
	d.send('?', rest)
}

// Sends a command of the form "#<command>,<id>,<rest>" to the repeater.
func (d *Component) Execute(rest string) {
	d.send('#', rest)
}

func (d *Component) send(operation int, rest string) {
	cmd := fmt.Sprintf("%c%s,%d,%s", operation, d.command, d.id, rest)
	d.Conn.requests <- request{cmd: cmd}
}

type monitored interface {
	handleEvent(string) error
	reconnect()
}

