golang package to communicate with Lutron's RadioRA2 devices.

Start a conenction with the main repeater, selecting a user id
dedicated to this application:

```Go
conn, err := lutron.Dial("192.168.1.5", "lutron", "integration")
```

To dim specific lights:

```Go
// fade switch "8" to 25% over 2 seconds
a := conn.Dimmer(8).Fade(25, 2*time.Second)

// fade switch "11" to 33% over 0.50 seconds
b := conn.Dimmer(11).Fade(33, 500*time.Millisecond)

// wait for prior fades to complete
<- a
<- b
```

Virtually press a button on a keypad, selecting its scene, and wait
for the command to be acknowledged:

```Go
k := conn.Keypad(4)
<- k.Button(1).Press()
```

Listen to a keypad button, e.g. for custom behavior:

```Go
k := conn.Keypad(4)
fivePressed := k.Button(5).Monitor()
for {
  select {
  case <-fivePressed:
    k.Button(5).SetLed(lutron.LedOff)
    musicPlayer.Stop()
  }
}
```
