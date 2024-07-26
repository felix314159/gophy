package database

// peerdiscovery.go contains libp2p related code to allow peers behind NAT in different networks to find and communicate with each other

import (
	"context"
	"fmt"
	"sync"
	"time"
	
	"github.com/felix314159/gophy/logger"
	"github.com/felix314159/gophy/monitoring"

	// libp2p stuff
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	dutil "github.com/libp2p/go-libp2p/p2p/discovery/util"
)

// DiscoverPeers uses Kademlia DHT bootstrap nodes and the Rendezvous protocol.
func DiscoverPeers(ctx context.Context, h host.Host, ch chan bool) {
	kademliaDHT := InitDHT(ctx, h)	// returns *dht.IpfsDHT
	routingDiscovery := drouting.NewRoutingDiscovery(kademliaDHT)	// returns *RoutingDiscovery (https://github.com/libp2p/go-libp2p/blob/master/p2p/discovery/routing/routing.go#L21)

	// advertise rendezvous string ('entry point / meet me here' in blockchain, helps new nodes find relevant peers)
	dutil.Advertise(ctx, routingDiscovery, RendezvousString)	// returns nothing (persistently advertises) (https://github.com/libp2p/go-libp2p/blob/master/p2p/discovery/util/util.go#L32)

	// look for other peers who advertised the same rendezvous string, try to connect to them
	logger.L.Printf("Searching for peers...")
	
	isFirstSignal := true // only sent the 'continue with the rest of the program' signal once (unbuffered channel that is only read once from), but always keep looking for new peers and even if the rest of the program is already running connect to them but then don't send signals to avoid locking cuz no one reads from the unbuffered channel anymore

	// use map to keep track of nodes you have successfully connected to
	myConnectedPeers := make(map[string]bool)

	// always keep looking for new peers
	for {
		// get list of discovered peers
		peerChan, err := routingDiscovery.FindPeers(ctx, RendezvousString)  // returns (<-chan peer.AddrInfo, error) (https://github.com/libp2p/go-libp2p/blob/master/p2p/discovery/routing/routing.go#L58)
		if err != nil {
			logger.L.Panic(err)	// FindPeers() must work, otherwise just panic
		}
		// for each discovered peer, try to connect to it if you have not done so already (loop waits for discovered peers sent via this channel)
		for peer := range peerChan {
			// don't try to connect to yourself
			if peer.ID == h.ID() {
				continue
			}

			// don't connect to the same peer multiple times (also prevents duplicate performance stats reporting)
			if myConnectedPeers[peer.ID.String()] {
                continue
            }

			err := h.Connect(ctx, peer)		// try to connect to peer (either new connection is opened or an error is returned)
			if err != nil { // un-comment out below if you want to get spammed with failed connection attemps
				//logger.L.Printf("Failed connecting to %v due to error: %v", peer.ID.String(), err)	// this happens and can be annoying, so just dont show it
				time.Sleep(20 * time.Millisecond)
				continue
			} 

			// ok you successfully connected to new peer, remember this peer (libp2p's who-is-connected-list seems to contain many inactive nodes)
			myConnectedPeers[peer.ID.String()] = true
			logger.L.Printf("Connected to: %v", peer.ID.String())

			
			/*
			logger.L.Printf("List of addresses this node has:")
			for _, address := range peer.Addrs {
				logger.L.Printf("%v", address.String())
			}

			logger.L.Printf("List of nodes I have connected to since start:")
			for n := range myConnectedPeers {
				logger.L.Printf("%v", n)
			}
			*/

			// RA starts sending out problems when ANY node is connected, but other nodes want to wait until they connected to at least the RA
			justConnectedToThisNode := peer.ID.String()
			if IAmRA {
				// if this is the first time you have connected to a peer, you then are allowed to continue executing rest of main()
				if isFirstSignal {
					isFirstSignal = false
					ch <- true
				}

			} else { // you are not the RA and you just connected to a new peer
				//			case: you just connected to the RA
				if justConnectedToThisNode == RANodeID { // (if someone would impersonate this peer ID it would later be detected when a message from it is received so not a big issue)
					logger.L.Printf("Successfully connected to RA.")
					// congrats you found RA so you are allowed to continue executing main() (peer discovery keeps running in the background)
					if isFirstSignal {
						isFirstSignal = false
						ch <- true
					}
				} else { // case: you connected to a node that is not RA, that's good but don't continue running main until you connect to RA
					if isFirstSignal {
						logger.L.Printf("Will continue to search for RA.")
					}
				}

			}

			// report this event (connected to a new peer) to the performance stats server
			//			1. get current time
			curTime := time.Now().UnixNano()
			//			2. determine event type (i could split the event report into two and put them above in the if-else to avoid repeating this check but IMO this negatively affects code readability)
			var eventType monitoring.Event
			if len(myConnectedPeers) == 1 {
				eventType = monitoring.Event_FirstPeerConnected
			} else if justConnectedToThisNode == RANodeID { // RA would never trigger this because self-connections were already triggering 'continue' earlier
				eventType = monitoring.Event_RAConnected
			} else {
				eventType = monitoring.Event_NewPeerConnected
			}
			//			3. message field holds the libp2p address of the node you connected to (also if you are not RA: helps to understand whether firstPeerConnected was RA or not)
			msg := fmt.Sprintf("Connected to: %v", justConnectedToThisNode)
			// 		construct stat to be sent
			pStat := monitoring.NewPerformanceData(curTime, MyDockerAlias, eventType, msg)
			// 		post stat
			err = monitoring.SendPerformanceStatWithRetries(pStat)
			if err != nil {
				logger.L.Printf("Warning - Failed to report performance stat: %v", err)
			}


		} // end of looping over discovered peers

		time.Sleep(20 * time.Millisecond) // sleep 20 ms to put less burden on CPU
	}
}

// InitDHT connects to a few known default bootstrap nodes (they enable distributed node discovery and are the only centralized necessity in kademlia DHT).
func InitDHT(ctx context.Context, h host.Host) *dht.IpfsDHT {
	kademliaDHT, err := dht.New(ctx, h, dht.Mode(dht.ModeAuto))	// https://github.com/libp2p/go-libp2p-kad-dht/blob/master/dht.go#L183, at some point maybe experiment with dht.ModeAuto vs dht.ModeClient vs dht.ModeServer vs dht.ModeAutoServer	
	// kademliaDHT, err := dht.New(ctx, h, dht.Mode(dht.ModeServer))
	if err != nil {
		logger.L.Panic(err)
	}
	err = kademliaDHT.Bootstrap(ctx)
	if err != nil {	// get into a bootstrapped state satisfying the IpfsRouter interface [basically refreshes routing table]
		logger.L.Panic(err)
	}
	var wg sync.WaitGroup
	for _, peerAddr := range dht.DefaultBootstrapPeers {	// connect to known bootstrap nodes [register yourself in the DHT keyspace - will find neighbor nodes]
		peerinfo, _ := peer.AddrInfoFromP2pAddr(peerAddr)
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := h.Connect(ctx, *peerinfo)
			if err != nil {
				//logger.L.Printf("Bootstrap warning: %v", err) // not that important, even if it occurs occasionally everything works fine
			}
		}()
	}
	wg.Wait()

	return kademliaDHT
}
