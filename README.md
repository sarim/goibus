goibus - golang implementation of libibus
==

goibus implements the libibus bindings in golang. goibus can be used to create IBus engines aka develop custom input methods.

IBus is an Intelligent Input Bus. It provides full featured and user friendly input method user interface. It also may help developers to develop input method easily.

This library is little bit different than other libibus bindings/wrappers. Instead of wrapping `libibus c library` or `GOBject-Introspection`, it implements whole functionality by communicating over DBus IPC. Because of that it is a independent 100% pure golang library without any native dependencies.

####NB:
libibus has various classes that are not absolutely required for creating engines. This library only implements engine related classes. Some uncommon class/methods are also skipped for now. You can always implement those and send PR ;)

This table shows the current status of implementation.

libibus | - | goibus
--- | --- | ---
[IBusAttrList](http://ibus.github.io/docs/ibus-1.5/IBusAttrList.html) | :white_check_mark: | Implemented In `text.go`
[IBusAttribute](http://ibus.github.io/docs/ibus-1.5/IBusAttribute.html) | :white_check_mark: | Implemented In `text.go`
[IBusBus](http://ibus.github.io/docs/ibus-1.5/IBusBus.html) | :white_check_mark: | Implemented In `bus.go`
[IBusComponent](http://ibus.github.io/docs/ibus-1.5/IBusComponent.html) | :white_check_mark: | Implemented In `component.go`
[IBusConfig](http://ibus.github.io/docs/ibus-1.5/IBusConfig.html) | :red_circle: | Ignored, not implemented
[IBusConfigService](http://ibus.github.io/docs/ibus-1.5/IBusConfigService.html) | :red_circle: | Ignored, not implemented
[IBusEngine](http://ibus.github.io/docs/ibus-1.5/IBusEngine.html) | :white_check_mark: | Implemented In `engine.go`
[IBusEngineDesc](http://ibus.github.io/docs/ibus-1.5/IBusEngineDesc.html) | :white_check_mark: | Implemented In `engineDesc.go`
[IBusFactory](http://ibus.github.io/docs/ibus-1.5/IBusFactory.html) | :white_check_mark: | Implemented In `factory.go`
[IBusHotkeyProfile](http://ibus.github.io/docs/ibus-1.5/IBusHotkeyProfile.html) | :red_circle: | Ignored, not implemented
[IBusInputContext](http://ibus.github.io/docs/ibus-1.5/IBusInputContext.html) | :large_blue_circle: | Ignored, relevant inherited signals implemented in `Engine`
[IBusKeymap](http://ibus.github.io/docs/ibus-1.5/IBusKeymap.html) | :large_blue_circle: | Ignored for now, will implement
[IBusLookupTable](http://ibus.github.io/docs/ibus-1.5/IBusLookupTable.html) | :white_check_mark: | Implemented In `lookupTable.go`
[IBusObject](http://ibus.github.io/docs/ibus-1.5/IBusObject.html) | :white_check_mark: | Ignored, Parent/Interface class, relevant inherited signals implemented in `Engine`
[IBusObservedPath](http://ibus.github.io/docs/ibus-1.5/IBusObservedPath.html) | :red_circle: | Ignored, not implemented
[IBusPanelService](http://ibus.github.io/docs/ibus-1.5/IBusPanelService.html) | :red_circle: | Ignored, not implemented
[IBusPropList](http://ibus.github.io/docs/ibus-1.5/IBusPropList.html) | :white_check_mark: | Implemented In `property.go`
[IBusProperty](http://ibus.github.io/docs/ibus-1.5/IBusProperty.html) | :white_check_mark: | Implemented In `property.go`
[IBusProxy](http://ibus.github.io/docs/ibus-1.5/IBusProxy.html) | :red_circle: | Ignored, not implemented
[IBusRegistry](http://ibus.github.io/docs/ibus-1.5/IBusRegistry.html) | :red_circle: | Ignored, not implemented
[IBusSerializable](http://ibus.github.io/docs/ibus-1.5/IBusSerializable.html) | :white_check_mark: | Not needed in golang, All implemented classes are Serializable
[IBusService](http://ibus.github.io/docs/ibus-1.5/IBusService.html) | :white_check_mark: | Ignored, not needed. Parent/Interface class
[IBusText](http://ibus.github.io/docs/ibus-1.5/IBusText.html) | :white_check_mark: | Implemented In `text.go`


Installation
==

```
go get github.com/godbus/dbus
go get github.com/BambooEngine/goibus
```

check `_example` directory for a sample engine and ~~ TODO:detailed tutorial ~~. Run the sample engine by `_example -standalone` to see it in action.
![sample engine](https://cloud.githubusercontent.com/assets/1235888/7563038/569ef518-f7fb-11e4-91af-2c2150199fe7.png)

Connecting to ibus-daemon
==

`IBusBus` is created with `NewBusE`, which reports failures instead of taking
the process down:

```go
bus, err := goibus.NewBusE()
if err != nil {
        log.Fatalf("cannot start: %v", err)
}
defer bus.Close()
```

`NewBus` is the older constructor. It still panics on failure, which is usually
not what you want in a component that `ibus-daemon` supervises, because the
component is respawned and crashes again in a loop.

A component that `ibus-daemon` spawns can start before the daemon has written
its address file, and a component started by hand can inherit a stale address
from a previous session. `DialWhenAvailable` retries until the daemon answers,
and re-resolves the address on every attempt so a restarted daemon is picked up:

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

bus, err := goibus.DialWhenAvailable(ctx, goibus.DefaultRetryInterval)
if err != nil {
        log.Fatalf("ibus-daemon never became available: %v", err)
}
```

Address resolution mirrors `ibus_get_address()`. `IBUS_ADDRESS` wins, otherwise
the address is read from the file named by `IBUS_ADDRESS_FILE` or
`$XDG_CONFIG_HOME/ibus/bus/<machine-id>-<hostname>-<display>`. Unlike the older
`GetAddress`, `GetAddressE` parses the `IBUS_DAEMON_PID` line and checks that
the recorded process is still alive, so a socket left behind by a crashed or
restarted `ibus-daemon` is reported as `ErrDaemonNotRunning` instead of failing
later with "connection refused".

Components should also watch for the daemon going away, otherwise they block
forever on a dead connection:

```go
// Exit when ibus-daemon restarts; it will respawn us.
<-bus.Done()
```

To be notified when a new daemon appears, watch the address file the way libibus
does with `g_file_monitor_file()`:

```go
go goibus.WatchAddress(ctx, goibus.GetSocketPath(), time.Second, func() {
        log.Println("the ibus daemon address changed")
})
```

License
==
**goibus** - golang implementation of libibus by **Sarim Khan**

Licensed under Mozilla Public License 1.1 ("MPL"), an open source/free software license.
