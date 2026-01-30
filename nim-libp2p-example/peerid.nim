import std/[json, os]
import chronicles
import libp2p/crypto/crypto
import libp2p/peerid as libp2pPeerId

logScope:
  topics = "peerid"

const PEER_ID_FILE = "./relay-peer-id.json"

type
  PeerIDData = object
    id: string
    privKey: string
    pubKey: string

proc loadOrGeneratePrivateKey*(): (PrivateKey, ref HmacDrbgContext) =
  ## Load existing peer ID from file or generate a new one.
  ## Returns (PrivateKey, RNG) tuple.
  
  var rng = newRng()
  
  # Try to load existing peer ID
  if fileExists(PEER_ID_FILE):
    try:
      let jsonData = readFile(PEER_ID_FILE)
      let peerData = parseJson(jsonData).to(PeerIDData)
      
      # Decode private key from base64 (protobuf format)
      let privKeyResult = PrivateKey.init(peerData.privKey)
      if privKeyResult.isOk:
        let privKey = privKeyResult.get()
        info "Loaded existing PeerId", peerId = peerData.id
        return (privKey, rng)
      else:
        warn "Failed to decode private key from file, generating new one"
      
    except CatchableError as e:
      warn "Failed to load peer ID file, generating new one", error = e.msg
  
  # Generate new peer ID
  info "Generating new PeerId..."
  
  let privKeyResult = PrivateKey.random(Ed25519, rng[])
  if privKeyResult.isErr:
    raise newException(CatchableError, "Failed to generate private key")
  let privKey = privKeyResult.get()
  
  let peerIdResult = PeerId.init(privKey)
  if peerIdResult.isErr:
    raise newException(CatchableError, "Failed to get peer ID from private key")
  let peerId = peerIdResult.get()
  
  # Save peer ID to file
  let pubKeyResult = privKey.getPublicKey()
  if pubKeyResult.isErr:
    raise newException(CatchableError, "Failed to get public key")
  let pubKey = pubKeyResult.get()
  
  let privKeyBytesResult = privKey.getBytes()
  if privKeyBytesResult.isErr:
    raise newException(CatchableError, "Failed to get private key bytes")
  let privKeyBytes = privKeyBytesResult.get()
  
  let pubKeyBytesResult = pubKey.getBytes()
  if pubKeyBytesResult.isErr:
    raise newException(CatchableError, "Failed to get public key bytes")
  let pubKeyBytes = pubKeyBytesResult.get()
  
  let jsonNode = %* {
    "id": $peerId,
    "privKey": privKeyBytes,
    "pubKey": pubKeyBytes
  }
  
  try:
    writeFile(PEER_ID_FILE, jsonNode.pretty())
    info "Generated new PeerId", peerId = $peerId
  except CatchableError as e:
    warn "Failed to write peer ID file", error = e.msg
  
  return (privKey, rng)
