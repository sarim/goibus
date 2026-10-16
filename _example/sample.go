package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	ibus "github.com/BambooEngine/goibus"
)

var embeded = flag.Bool("ibus", false, "Run the embeded ibus component")
var standalone = flag.Bool("standalone", false, "Run standalone by creating new component")
var generatexml = flag.String("xml", "", "Write xml representation of component to file or stdout if file == \"-\"")

func makeComponent() *ibus.Component {

	component := ibus.NewComponent(
		"org.freedesktop.IBus.Gittu",
		"Gittu Sample",
		"2.0",
		"MPL 1.1",
		"Sarim Khan <sarim2005@gmail.com>",
		"https://github.com/sarim/goibus",
		"/usr/bin/gittuengine",
		"gittu-sample")

	avroenginedesc := ibus.SmallEngineDesc(
		"gittu-sample",
		"Gittu Sample",
		"Gittu Sample Engine",
		"en",
		"MPL 1.1",
		"Sarim Khan <sarim2005@gmail.com>",
		"/usr/share/gittu/icon.png",
		"en",
		"/usr/bin/gittupref",
		"2.0")

	component.AddEngine(avroenginedesc)

	return component
}

func main() {

	var Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
		flag.CommandLine.VisitAll(func(f *flag.Flag) {
			format := "  -%s: %s\n"
			fmt.Fprintf(os.Stderr, format, f.Name, f.Usage)
		})
	}

	flag.Parse()

	if *generatexml != "" {
		c := makeComponent()

		if *generatexml == "-" {
			c.OutputXML(os.Stdout)
		} else {
			f, err := os.Create(*generatexml)
			if err != nil {
				panic(err)
			}

			c.OutputXML(f)
			f.Close()
		}
	} else if *embeded {
		// ibus-daemon spawns us, possibly before it has written its own address
		// file, so wait for it instead of dying and being respawned forever.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		bus, err := ibus.DialWhenAvailable(ctx, ibus.DefaultRetryInterval)
		if err != nil {
			log.Fatalf("cannot start: %v", err)
		}
		defer bus.Close()
		fmt.Println("Got Bus, Running Embeded")

		conn := bus.GetDbusConn()
		ibus.NewFactory(conn, GittuEngineCreator)
		if err := bus.RequestNameChecked("org.freedesktop.IBus.Gittu", 0); err != nil {
			log.Fatalf("cannot register: %v", err)
		}

		// Exit when ibus-daemon goes away; it will start us again.
		<-bus.Done()
	} else if *standalone {
		bus, err := ibus.NewBusE()
		if err != nil {
			log.Fatalf("cannot start: %v", err)
		}
		defer bus.Close()
		fmt.Println("Got Bus, Running Standalone")

		conn := bus.GetDbusConn()
		ibus.NewFactory(conn, GittuEngineCreator)
		bus.RegisterComponent(makeComponent())

		fmt.Println("Setting Global Engine to me")
		if call := bus.CallMethod("SetGlobalEngine", 0, "gittu-sample"); call.Err != nil {
			log.Fatalf("cannot activate: %v", call.Err)
		}

		<-bus.Done()
	} else {
		Usage()
		os.Exit(1)
	}
}
