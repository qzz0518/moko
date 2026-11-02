// Mokoctl prints what a KI-SX100 on the network reports. It only reads.
//
//	go run ./cmd/mokoctl [ip]
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/qzz0518/moko/internal/device"
	"github.com/qzz0518/moko/internal/echonet"
)

func main() {
	client, err := echonet.Listen()
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	got := make(chan device.Snapshot, 8)
	cfg := device.Config{OnChange: func(s device.Snapshot) {
		select {
		case got <- s:
		default:
		}
	}}
	if len(os.Args) > 1 {
		cfg.ManualIP = os.Args[1]
	}
	go device.New(client, cfg).Run(ctx)
	for {
		select {
		case s := <-got:
			if s.Phase != device.PhaseOnline {
				fmt.Fprintln(os.Stderr, "searching:", s.Err)
				continue
			}
			fmt.Printf("%+v\n%+v\n", s.Info, s.State)
			return
		case <-ctx.Done():
			log.Fatal("no device found")
		}
	}
}
