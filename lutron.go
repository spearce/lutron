// Copyright 2014 Google. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package lutron interfaces with Lutron RadioRA2 home automation.

Applications can monitor lighting levels, monitor keypad LEDs,
respond to keypad presses, and manage unprogrammed LEDs.
*/
package lutron

import (
	"errors"
	"github.com/ziutek/telnet"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	timeout = 40 * time.Second
)

type request struct {
	cmd string
}

type Conn struct {
	addr  string
	user  string
	pass  string
	Trace bool

	sock     *telnet.Conn
	requests chan request

	mu       sync.Mutex
	monitors []chan LevelChange
	dimmers  map[int]*Dimmer
	keypads  map[int]*Keypad
}

func Dial(addr, user, pass string) (*Conn, error) {
	c := Conn{addr: addr, user: user, pass: pass}
	t, err := c.dial()
	if err != nil {
		return nil, err
	}
	c.sock = t
	c.requests = make(chan request, 100)
	c.dimmers = make(map[int]*Dimmer)
	c.keypads = make(map[int]*Keypad)

	go c.controller()
	setup := []string{
		"#MONITORING,1,2", // Disable diagnostic monitoring
		"#MONITORING,3,1", // Enable button (device) monitoring
		"#MONITORING,4,1", // Enable LED (device) monitoring
		"#MONITORING,5,1", // Enable zone (output) monitoring
	}
	for _, s := range setup {
		if err := sendln(t, s); err != nil {
			return nil, err
		}
	}
	return &c, nil
}

func reader(sock *telnet.Conn, d chan string, e chan error) {
	for {
		str, err := sock.ReadString('\n')
		if err != nil {
			e <- err
			return
		}
		str = strings.TrimSpace(strings.Replace(str, "GNET> ", "", -1))
		if str != "" {
			d <- str
		}
	}
}

func (c *Conn) controller() {
	evtCh := make(chan string, 5)
	errCh := make(chan error)
	go reader(c.sock, evtCh, errCh)

	for {
		select {
		case str := <-evtCh:
			if c.Trace {
				log.Println(str)
			}
			c.eventFromRepeater(str)

		case err := <-errCh:
			// TODO(spearce) retry connection
			log.Fatalln("read error:", err)
			c.afterReconnect()

		case req := <-c.requests:
			if c.Trace {
				log.Println(req.cmd)
			}
			sendln(c.sock, req.cmd)
		}
	}
}

func (c *Conn) eventFromRepeater(s string) {
	if !strings.HasPrefix(s, "~") {
		log.Printf("expected ~EVENT, received %#v\n", s)
		return
	}

	cmd, id, rest, err := parseEvent(s)
	if err != nil {
		log.Printf("cannot parse %#v: %v\n", s, err)
		return
	}
	c.processEvent(cmd, id, rest)
}

func parseEvent(s string) (string, int, string, error) {
	n := strings.SplitN(s, ",", 3)
	if len(n) != 3 {
		return "", 0, "", errors.New("lutron: invalid message")
	}
	id, err := strconv.Atoi(n[1])
	if err != nil {
		return "", 0, "", err
	}
	return strings.TrimPrefix(n[0], "~"), id, n[2], nil
}

func (c *Conn) processEvent(cmd string, id int, rest string) {
	var i monitored
	switch cmd {
	case "OUTPUT":
		i = c.Dimmer(id)
	case "DEVICE":
		i = c.Keypad(id)
	case "MONITORING":
		return
	default:
		log.Printf("unsupported %v %v %d", cmd, id, rest)
		return
	}
	if err := i.handleEvent(rest); err != nil {
		log.Printf("invalid event %v %v %v", cmd, id, rest)
	}
}

func (c *Conn) afterReconnect() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, d := range c.dimmers {
		d.reconnect()
	}
	for _, k := range c.keypads {
		k.reconnect()
	}
}

// Adds a channel to receive updates when any dimmer is adjusted. The current
// level of every known dimmer will be sent on the channel.
//
// Monitoring all dimmers is useful for logging lighting usage over time.
// Specific dimmer monitoring simplifies reacting to lighting changes with
// other automated actions. To monitor only specific dimmers use:
//   m := c.Dimmer(id).Monitor()
// or
//   c.Dimmer(id).AddMonitor(m)
func (c *Conn) AddDimmerMonitor(m chan LevelChange) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.monitors = append(c.monitors, m)

	// Add to existing dimmers in the background in case the
	// caller is not yet receiving from the channel.
	q := make([]*Dimmer, 16)
	for _, d := range c.dimmers {
		q = append(q, d)
	}
	go func() {
		for _, d := range q {
			d.AddMonitor(m)
		}
	}()
}

// Get a reference to a Maestro style dimmer, switch or hybrid keypad.
// The integration id must be obtained from the RadioRA2 software.
func (c *Conn) Dimmer(id int) *Dimmer {
	c.mu.Lock()
	defer c.mu.Unlock()

	d := c.dimmers[id]
	if d == nil {
		d = &Dimmer{Component: Component{
			Conn:    c,
			command: "OUTPUT",
			id:      id}}
		c.dimmers[id] = d
		for _, m := range c.monitors {
			d.AddMonitor(m)
		}
	}
	return d
}

// Get a reference to a Maestro style switch. This is a lightweight
// simplified API wrapper around the same numbered Dimmer object.
// The integration id must be obtained from the RadioRA2 software.
func (c *Conn) Switch(id int) *Switch {
	return &Switch{c.Dimmer(id)}
}

// Get a reference to a hybrid keypad. This is a union of the Dimmer
// and Keypad objects on the same integration id. Callers may either
// use the HybridKeypad object, or access the Dimmer and Keypad directly.
// The integration id must be obtained from the RadioRA2 software.
func (c *Conn) HybridKeypad(id int) *HybridKeypad {
	d := c.Dimmer(id)
	k := c.Keypad(id)
	return &HybridKeypad{d, k}
}

// Get a reference to a seeTouch keypad, hybrid keypad, or Pico remote.
// The integration id must be obtained from the RadioRA2 software.
func (c *Conn) Keypad(id int) *Keypad {
	c.mu.Lock()
	defer c.mu.Unlock()

	k := c.keypads[id]
	if k == nil {
		k = &Keypad{Component: Component{
			Conn:    c,
			command: "DEVICE",
			id:      id}}
		c.keypads[id] = k
	}
	return k
}

func (c *Conn) dial() (*telnet.Conn, error) {
	t, err := telnet.Dial("tcp", net.JoinHostPort(c.addr, "23"))
	if err != nil {
		return nil, err
	}

	if err := c.login(t); err != nil {
		t.Close()
		return nil, err
	}
	return t, nil
}

func (c *Conn) login(t *telnet.Conn) error {
	if err := expect(t, "login: "); err != nil {
		return err
	}
	if err := sendln(t, c.user); err != nil {
		return err
	}
	if err := expect(t, "ssword: "); err != nil {
		return err
	}
	if err := sendln(t, c.pass); err != nil {
		return err
	}

	// Expect and disable prompt.
	if err := expect(t, "GNET> "); err != nil {
		return err
	}
	if err := sendln(t, "#MONITORING,12,2"); err != nil {
		// caseta doesn't support this, reporting ~ERROR,6
		// this would be an ideal place to perform caseta detection
		return err
	}
	t.SetReadDeadline(time.Time{})
	return nil
}

func expect(t *telnet.Conn, d string) error {
	if err := t.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}
	if err := t.SkipUntil(d); err != nil {
		return err
	}
	return nil
}

func sendln(t *telnet.Conn, s string) error {
	if err := t.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}

	buf := make([]byte, len(s)+2)
	copy(buf, s)
	buf[len(s)] = '\r'
	buf[len(s)+1] = '\n'
	_, err := t.Write(buf)
	return err
}
