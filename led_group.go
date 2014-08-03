// Copyright 2014 Google. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lutron

// Collection of keypad buttons that should behave like radio buttons.
// At most one button in the group should have LedOn state.
type LedGroup struct {
	members []*KeypadButton
}

// Create a "radio group" of keypad buttons. Select() can be used to
// set LedOn for one button and LedOff for the others in the group.
func NewLedGroup(buttons ...*KeypadButton) *LedGroup {
	return &LedGroup{buttons}
}

// "Select" one button from a group of buttons by turning on the selected
// button's LED and turning off all other LEDs in the group. A wait object
// is returned to allow the caller to wait for the updates to process.
//
// Selecting nil will turn off all LEDs in the group.
func (g *LedGroup) Select(sel *KeypadButton) *PendingLedUpdates {
	r := []chan uint8{}
	if sel != nil {
		r = append(r, sel.SetLed(LedOn))
	}
	for _, b := range g.members {
		if sel == nil || b.id != sel.id {
			r = append(r, b.SetLed(LedOff))
		}
	}
	return &PendingLedUpdates{r}
}

// Partial results after selecting an LED in a group.
type PendingLedUpdates struct {
	ch []chan uint8
}

// Wait for LED updates to be acknowledged by the main repeater.
func (u *PendingLedUpdates) Wait() {
	for _, c := range u.ch {
		<-c
	}
}
