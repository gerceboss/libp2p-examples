package main

import (
	"context"
	"fmt"
	"log"
	"time"

	badger "github.com/ipfs/go-ds-badger"
	"github.com/libp2p/go-libp2p"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	autonat "github.com/libp2p/go-libp2p/p2p/host/autonat"
	"github.com/libp2p/go-libp2p/p2p/host/peerstore/pstoreds"
	"github.com/libp2p/go-libp2p/p2p/muxer/yamux"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
	"github.com/libp2p/go-libp2p/p2p/protocol/holepunch"
	"github.com/libp2p/go-libp2p/p2p/protocol/identify"
	"github.com/libp2p/go-libp2p/p2p/protocol/ping"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"
	"github.com/libp2p/go-libp2p/p2p/transport/websocket"
	"github.com/multiformats/go-multiaddr"
)

// createLibp2pHost builds a libp2p host with transports and security/muxer.
func createLibp2pHost(privKey crypto.PrivKey) (host.Host, error) {
	ds, err := badger.NewDatastore("./peerstore", &badger.DefaultOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to create datastore: %w", err)
	}

	ps, err := pstoreds.NewPeerstore(context.Background(), ds, pstoreds.DefaultOpts())
	if err != nil {
		return nil, fmt.Errorf("failed to create peerstore: %w", err)
	}

	h, err := libp2p.New(
		libp2p.Identity(privKey),
		libp2p.ListenAddrs(
			multiaddr.StringCast("/ip4/0.0.0.0/tcp/9091"),
			multiaddr.StringCast("/ip4/0.0.0.0/tcp/9092/ws"),
		),
		libp2p.Transport(tcp.NewTCPTransport),
		libp2p.Transport(websocket.New),
		libp2p.Security(noise.ID, noise.New),
		libp2p.Muxer("/yamux/1.0.0", yamux.DefaultTransport),
		libp2p.Peerstore(ps),
		libp2p.EnableNATService(),
		libp2p.EnableRelay(),
		libp2p.EnableHolePunching(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create libp2p host: %w", err)
	}

	return h, nil
}

// setupRelay configures relay/pubsub/discovery services on the host.
func setupRelay(ctx context.Context, h host.Host) error {
	// Ping + identify
	ping.NewPingService(h)

	idService, err := identify.NewIDService(h)
	if err != nil {
		log.Printf("Warning: Failed to create identify service: %v", err)
	}

	// AutoNAT
	if _, err := autonat.New(h); err != nil {
		log.Printf("Warning: Failed to create autonat service: %v", err)
	}

	// Hole punching
	if _, err := holepunch.NewService(h, idService, func() []multiaddr.Multiaddr {
		return h.Addrs()
	}); err != nil {
		log.Printf("Warning: Failed to create holepunch service: %v", err)
	}

	// Relay service
	if _, err := relay.New(h); err != nil {
		return fmt.Errorf("failed to create relay service: %w", err)
	}

	// Pubsub
	ps, err := pubsub.NewGossipSub(ctx, h,
		pubsub.WithMessageIdFn(pubsub.DefaultMsgIdFn),
		pubsub.WithPeerOutboundQueueSize(128),
		pubsub.WithValidateQueueSize(128),
		pubsub.WithMaxMessageSize(pubsub.DefaultMaxMessageSize),
		pubsub.WithFloodPublish(true),
	)
	if err != nil {
		return fmt.Errorf("failed to create pubsub: %w", err)
	}

	// Discovery topic
	discoveryTopic, err := ps.Join(DISCOVERY_TOPIC)
	if err != nil {
		return fmt.Errorf("failed to join discovery topic: %w", err)
	}
	fmt.Printf("Subscribed to discovery topic: %s\n", DISCOVERY_TOPIC)

	// Track subscribed topics
	subscribedTopics := make(map[string]*pubsub.Topic)
	subscribedTopics[DISCOVERY_TOPIC] = discoveryTopic

	// Auto-subscribe to new topics (polling)
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				allTopics := ps.GetTopics()
				for _, topicName := range allTopics {
					if topicName == DISCOVERY_TOPIC {
						continue
					}
					if _, exists := subscribedTopics[topicName]; exists {
						continue
					}
					topic, err := ps.Join(topicName)
					if err != nil {
						if DEBUG {
							log.Printf("Failed to auto-subscribe to topic %s: %v", topicName, err)
						}
						continue
					}
					subscribedTopics[topicName] = topic
					fmt.Printf("Auto-subscribed to topic: %s\n", topicName)

					sub, err := topic.Subscribe()
					if err == nil {
						go func(tn string, sub *pubsub.Subscription) {
							for {
								_, err := sub.Next(ctx)
								if err != nil {
									return
								}
								// FloodPublish already routes; subscription keeps us in mesh.
							}
						}(topicName, sub)
					}
				}
			}
		}
	}()

	// Subscribe to discovery messages to keep routing active
	if sub, err := discoveryTopic.Subscribe(); err == nil {
		go func() {
			for {
				_, err := sub.Next(ctx)
				if err != nil {
					return
				}
			}
		}()
	} else {
		log.Printf("Warning: Failed to subscribe to discovery topic messages: %v", err)
	}

	// Network notifications
	h.Network().Notify(&networkNotifier{
		pubsub: ps,
	})

	return nil
}

// networkNotifier logs basic peer events.
type networkNotifier struct {
	pubsub *pubsub.PubSub
}

func (n *networkNotifier) Listen(network.Network, multiaddr.Multiaddr)      {}
func (n *networkNotifier) ListenClose(network.Network, multiaddr.Multiaddr) {}
func (n *networkNotifier) Connected(net network.Network, conn network.Conn) {
	if DEBUG {
		fmt.Printf("Peer connected: %s\n", conn.RemotePeer().ShortString())
	}
}
func (n *networkNotifier) Disconnected(net network.Network, conn network.Conn) {
	if DEBUG {
		fmt.Printf("Peer disconnected: %s\n", conn.RemotePeer().ShortString())
	}
}
func (n *networkNotifier) OpenedStream(network.Network, network.Stream) {}
func (n *networkNotifier) ClosedStream(network.Network, network.Stream) {}
