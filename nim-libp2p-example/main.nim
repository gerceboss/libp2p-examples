import std/[tables, sets]
import chronicles
import chronos
import chronos/apps/http/httpserver
import std/[json, strutils]
import libp2p
import libp2p/protocols/ping
import libp2p/protocols/identify
import libp2p/protocols/pubsub/gossipsub

import libp2p/observedaddrmanager
import libp2p/services/autorelayservice
import libp2p/services/hpservice
import libp2p/protocols/connectivity/autonat/[client as AutonatClient, service as Autonatservice]
import libp2p/protocols/connectivity/relay/[relay, client]

import relay_constants  
import peerid

proc createLibp2pHost(r: Relay = nil, hpService: Service = nil): Switch =
  info "Loading or generating identity key..."
  let (seckey, rng) = loadOrGeneratePrivateKey()
  info "Creating switch (host)..."

  let obsAddrManager = ObservedAddrManager.new()
  var builder: SwitchBuilder

  # --- Switch Builder ---
  if rng != nil:
    builder = SwitchBuilder
      .new()
      .withRng(rng)
      .withAddresses(@[
        MultiAddress.init("/ip4/0.0.0.0/tcp/9091").tryGet(),
        MultiAddress.init("/ip4/0.0.0.0/tcp/9092/ws").tryGet()
      ])
      .withTcpTransport() # TCP Transport
      .withWsTransport() # WebSocket Transport
      .withNoise() # Noise Transport
      .withYamux() # Yamux multiplexer
      .withAutonat() # AutoNAT protocol
      .withObservedAddrManager(obsAddrManager)
  else:
    builder = SwitchBuilder
      .new()
      .withPrivateKey(seckey)
      .withAddresses(@[
        MultiAddress.init("/ip4/0.0.0.0/tcp/9091").tryGet(),
        MultiAddress.init("/ip4/0.0.0.0/tcp/9092/ws").tryGet()
      ])
      .withTcpTransport() # TCP Transport
      .withWsTransport() # WebSocket Transport
      .withNoise() # Noise Transport
      .withYamux() # Yamux multiplexer
      .withAutonat() # AutoNAT protocol
      .withObservedAddrManager(obsAddrManager)

  if hpService != nil:
    builder = builder.withServices(@[hpService])

  if r != nil:
    builder = builder.withCircuitRelay(r)

  let sw = builder.build()

  # Ping protocol (mounted separately)
  sw.mount(Ping.new(rng = rng))

  # Identify protocol (mounted separately)
  sw.mount(Identify.new(sw.peerInfo))

  return sw
  


proc createPubsub(sw: Switch, rng: ref HmacDrbgContext) =
  # Configure GossipSub with floodPublish enabled for better message propagation
  let gossipParams = GossipSubParams.init(floodPublish = true)
  let gossip = GossipSub.init(sw, rng = rng, triggerSelf = false, parameters = gossipParams)
  sw.mount(gossip)

  # Subscribe to discovery topic to maintain mesh connectivity
  gossip.subscribe(relay_constants.DISCOVERY_TOPIC, nil)

  var subscribedTopics = initHashSet[string]()
  subscribedTopics.incl(relay_constants.DISCOVERY_TOPIC)

  # Auto-subscribe to topics that connected peers are in
  # GossipSub maintains a mesh/fanout/gossip map of peers per topic
  proc autoSubscribeLoop() {.async.} =
    while true:
      await sleepAsync(2.seconds)
      
      # Collect all topics that any connected peer is subscribed to
      var peerTopics = initHashSet[string]()
      
      # Check mesh (peers we have full pub/sub with)
      for topic, peers in gossip.mesh.pairs:
        if peers.len > 0:
          peerTopics.incl(topic)
      
      # Check fanout (peers we're publishing to but not fully meshed)
      for topic, peers in gossip.fanout.pairs:
        if peers.len > 0:
          peerTopics.incl(topic)
      
      # Check gossip (peers we exchange metadata with)
      for topic, peers in gossip.gossipsub.pairs:
        if peers.len > 0:
          peerTopics.incl(topic)
      
      if peerTopics.len > 0:
        info "Discovered peer topics", 
          peerTopicCount = peerTopics.len,
          myTopicCount = subscribedTopics.len
      
      # Subscribe to any new topics we find
      for topicName in peerTopics:
        if topicName == relay_constants.DISCOVERY_TOPIC:
          continue
        if topicName in subscribedTopics:
          continue
        
        # Subscribe to the new topic
        gossip.subscribe(topicName, nil)
        subscribedTopics.incl(topicName)
        info "Auto-subscribed to peer topic", topic = topicName
  
  asyncSpawn autoSubscribeLoop()

type AddressesResponse = object
  websocket*: seq[string]
  webrtcDirect*: seq[string]
  tcp*: seq[string]
  all*: seq[string]

proc startHttpServer(sw: Switch) {.async.} =
  proc processRequest(req: RequestFence): Future[HttpResponseRef] {.async.} =
    let request = req.get()
    
    # CORS headers
    let headers = HttpTable.init([
      ("Access-Control-Allow-Origin", "*"),
      ("Access-Control-Allow-Methods", "GET, OPTIONS"),
      ("Access-Control-Allow-Headers", "Content-Type")
    ])

    # OPTIONS preflight
    if request.meth == MethodOptions:
      return await request.respond(Http204, "", headers)

    # Route: /api/addresses
    if request.uri.path == "/api/addresses":
      if request.meth != MethodGet:
        return await request.respond(Http405, "Method not allowed", headers)

      var multiaddrs: seq[string] = @[]
      info "Getting addresses from switch", addrCount = sw.peerInfo.addrs.len
      
      for addr in sw.peerInfo.addrs:
        try:
          let addrStr = $addr
          # Fix: Add /ws suffix to port 9092 addresses (WebSocket port)
          var finalAddr = addrStr
          if "/tcp/9092" in addrStr and "/ws" notin addrStr:
            finalAddr = addrStr & "/ws"
            info "Added /ws suffix to WebSocket address", original = addrStr, fixed = finalAddr
          
          let fullAddr = finalAddr & "/p2p/" & $sw.peerInfo.peerId
          multiaddrs.add(fullAddr)
          info "Raw address", addr = addrStr, full = fullAddr
        except CatchableError as e:
          warn "Failed to add address", error = e.msg
          continue

      info "Total addresses collected", count = multiaddrs.len

      var resp = AddressesResponse(
        websocket: @[],
        webrtcDirect: @[],
        tcp: @[],
        all: multiaddrs
      )

      # Categorize addresses - check for ws/wss protocol
      for ma in multiaddrs:
        let maLower = ma.toLowerAscii()
        if "/ws/" in maLower or maLower.endsWith("/ws"):
          resp.websocket.add(ma)
          info "✓ Categorized as websocket", addr = ma
        elif "/wss/" in maLower or maLower.endsWith("/wss"):
          resp.websocket.add(ma)
          info "✓ Categorized as secure websocket", addr = ma
        elif "/webrtc-direct" in maLower:
          resp.webrtcDirect.add(ma)
          info "✓ Categorized as webrtc-direct", addr = ma
        elif "/tcp/" in maLower and "/ws" notin maLower:
          resp.tcp.add(ma)
          info "✓ Categorized as tcp", addr = ma
        else:
          info "⚠ Uncategorized address", addr = ma
      
      info "Response summary", ws = resp.websocket.len, tcp = resp.tcp.len, total = resp.all.len

      let body = %* resp
      let jsonHeaders = HttpTable.init([
        ("Access-Control-Allow-Origin", "*"),
        ("Content-Type", "application/json")
      ])
      return await request.respond(Http200, $body, jsonHeaders)

    # Root handler
    if request.uri.path == "/":
      return await request.respond(Http404, "Not found. Try /api/addresses", headers)

    return await request.respond(Http404, "", headers)

  echo "\nHTTP API listening on http://0.0.0.0:9094"
  echo "Get addresses at http://localhost:9094/api/addresses\n"

  let address = initTAddress("0.0.0.0:9094")
  var server = HttpServerRef.new(address, processRequest, {}).get()
  
  server.start()
  await server.join()


# --------------------------------------------------
# MAIN
# --------------------------------------------------
proc main() {.async.} =
  info "Booting node..."
  let (_, rng) = loadOrGeneratePrivateKey()
  
  # Create relay SERVER (not client) so this node can relay for others
  let relayServer = Relay.new()
  
  # Also create client for this node to use other relays if needed
  let relayClient = RelayClient.new()
  let autoRelayService = AutoRelayService.new(1, relayClient, nil, rng)
  let autonatClient = AutonatClient()
  let autonatService = Autonatservice.AutonatService.new(autonatClient, rng)
  let hpservice = HPservice.new(autonatService, autoRelayService)

  # Pass relay SERVER to enable circuit relay functionality
  let sw = createLibp2pHost(relayServer, hpservice)
  createPubsub(sw, rng)

  # Start the switch to begin listening
  await sw.start()

  info "Node started. Peer ID: ", peerId = $sw.peerInfo.peerId
  info "Circuit relay server enabled - peers can use this node as a relay"
  
  # Update peer info with actual listening addresses
  await sw.peerInfo.update()

  # Print listening addresses
  echo "\nRelay listening on:"
  echo "Total addresses: ", sw.peerInfo.addrs.len
  for addr in sw.peerInfo.addrs:
    let addrStr = $addr
    echo "  ", addrStr, "/p2p/", sw.peerInfo.peerId
    # Check if it's a websocket address
    if "/ws" in addrStr.toLowerAscii():
      echo "    -> WebSocket address ✓"
    elif "/tcp/" in addrStr.toLowerAscii():
      echo "    -> TCP address"
  
  if sw.peerInfo.addrs.len == 0:
    warn "No listening addresses found!"
  
  echo ""  # Empty line for readability

  asyncSpawn startHttpServer(sw)
  runForever()

waitFor main()