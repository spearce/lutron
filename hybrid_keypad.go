// Copyright 2014 Google. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lutron

// Hybrid keypads contain both a dimmer and a keypad. The dimmer controls
// the lighting load wired into the back of the hybrid keypad component,
// while the keys on the face can be programmed for any purpose.
type HybridKeypad struct {
	*Dimmer
	*Keypad
}
