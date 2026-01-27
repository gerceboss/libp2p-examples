package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
)

// PeerIDData matches the JS relay peer ID JSON format.
type PeerIDData struct {
	ID      string `json:"id"`
	PrivKey string `json:"privKey"`
	PubKey  string `json:"pubKey"`
}

func loadOrGeneratePeerID() (crypto.PrivKey, peer.ID, error) {
	const PEER_ID_FILE = "./relay-peer-id.json"

	// Try to load existing peer ID
	if data, err := os.ReadFile(PEER_ID_FILE); err == nil {
		var peerData PeerIDData
		if err := json.Unmarshal(data, &peerData); err == nil {
			// Decode private key from base64 (protobuf format)
			privKeyBytes, err := crypto.ConfigDecodeKey(peerData.PrivKey)
			if err != nil {
				// Try direct base64 decode if ConfigDecodeKey fails
				privKeyBytes, err = base64.StdEncoding.DecodeString(peerData.PrivKey)
				if err != nil {
					return nil, "", fmt.Errorf("failed to decode private key: %w", err)
				}
			}

			privKey, err := crypto.UnmarshalPrivateKey(privKeyBytes)
			if err != nil {
				return nil, "", fmt.Errorf("failed to unmarshal private key: %w", err)
			}

			peerID, err := peer.Decode(peerData.ID)
			if err != nil {
				return nil, "", fmt.Errorf("failed to parse peer ID: %w", err)
			}

			fmt.Printf("Loaded existing PeerId: %s\n", peerID.String())
			return privKey, peerID, nil
		}
	}

	// Generate new peer ID
	fmt.Println("Generating new PeerId...")
	privKey, _, err := crypto.GenerateEd25519Key(nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate key: %w", err)
	}

	peerID, err := peer.IDFromPrivateKey(privKey)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get peer ID: %w", err)
	}

	// Save peer ID (matching JS format - base64 encoded protobuf)
	pubKeyBytes, err := crypto.MarshalPublicKey(privKey.GetPublic())
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal public key: %w", err)
	}

	privKeyBytes, err := crypto.MarshalPrivateKey(privKey)
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal private key: %w", err)
	}

	peerData := PeerIDData{
		ID:      peerID.String(),
		PrivKey: crypto.ConfigEncodeKey(privKeyBytes),
		PubKey:  crypto.ConfigEncodeKey(pubKeyBytes),
	}

	data, err := json.MarshalIndent(peerData, "", "  ")
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal peer data: %w", err)
	}

	if err := os.WriteFile(PEER_ID_FILE, data, 0644); err != nil {
		return nil, "", fmt.Errorf("failed to write peer ID file: %w", err)
	}

	fmt.Printf("Generated new PeerId: %s\n", peerID.String())
	return privKey, peerID, nil
}
