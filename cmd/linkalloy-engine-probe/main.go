// linkalloy-engine-probe validates per-interface egress binding without
// creating a tunnel or modifying routes, proxies, or adapter settings.
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"sort"
	"time"

	"github.com/sayanpramanik2012/Link-Alloy/internal/engine"
)

func main() {
	interfaces, err := net.Interfaces()
	if err != nil {
		fatal(err)
	}
	sort.Slice(interfaces, func(i, j int) bool { return interfaces[i].Index < interfaces[j].Index })
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	active := make([]net.Interface, 0, len(interfaces))
	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		active = append(active, iface)
	}
	results := make(chan string, len(active))
	for _, iface := range active {
		iface := iface
		go func() {
			address, probeErr := engine.ProbePublicIP(ctx, uint32(iface.Index))
			if probeErr != nil {
				results <- fmt.Sprintf("%s (index %d): FAILED: %v", iface.Name, iface.Index, probeErr)
				return
			}
			results <- fmt.Sprintf("%s (index %d): egress %s", iface.Name, iface.Index, address)
		}()
	}
	for range len(active) {
		select {
		case result := <-results:
			fmt.Println(result)
		case <-ctx.Done():
			fmt.Println("Timed out before all interface probes finished")
			return
		}
	}
}

func fatal(err error) {
	log.Fatal("Link Alloy engine probe failed: ", err)
}
