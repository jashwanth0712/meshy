package enskit

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// LabelRegistered, LabelUnregistered and SubregistrySet event signatures.
//
// TopicLabelRegistered is keccak256("LabelRegistered(uint256,bytes32,string,address,uint64,address)").
// Verified correct against live Sepolia events.
//
// TopicSubregistrySet: the ENSv2 PermissionedRegistry/UserRegistry deployed on
// Sepolia emits a SubregistrySet event with 3 indexed params (uint256 tokenId,
// address subregistry, address sender) and no data.  The on-chain topic hash is
// 0xca9c8d51... which does NOT equal keccak256("SubregistrySet(uint256,address,address)").
// This is likely because the Solidity source uses interface or user-defined types
// whose canonical ABI names differ from plain "address".  We use the observed
// hash directly.
//
// TopicLabelUnregistered: similarly observed on-chain on Sepolia (0x5293e8...).
var (
	TopicLabelRegistered   = crypto.Keccak256Hash([]byte("LabelRegistered(uint256,bytes32,string,address,uint64,address)"))
	TopicLabelUnregistered = common.HexToHash("0x5293e83951c7b759c1ef192ceed240dc2caa652e29ddbd95cafe1d88e5a9cea8")
	TopicSubregistrySet    = common.HexToHash("0xca9c8d517128edd416adf5719242ca6ff93ce234442d95234da53c0ae8a10540")
)

// LogFilter is the subset of ethclient.Client needed for log queries.
type LogFilter interface {
	FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error)
	HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error)
}

// SubregAddr extracts the subregistry address from the State.Resource field.
//
// ENSv2's RegistryDatastore packs the subregistry as:
//   resource = address(subregistry) << 96 | flags
//
// So the address is in bits [255:96]: address(uint160(resource >> 96)).
func SubregAddr(resource *big.Int) common.Address {
	if resource == nil || resource.Sign() == 0 {
		return common.Address{}
	}
	shifted := new(big.Int).Rsh(resource, 96)
	return common.BigToAddress(shifted)
}

// initialChunkSize is the first block range tried. We try large first and
// fall back on RPC error. Alchemy free tier limits eth_getLogs to ~500 blocks
// for some methods, but many Sepolia deployments allow more.
const initialChunkSize = 10_000
const minChunkSize = 1

// ChildLabels returns the set of label strings that are currently registered
// (appeared in LabelRegistered but not subsequently in LabelUnregistered) in
// the given registry between fromBlock and the latest block.
//
// It queries logs in chunks, trying large chunks first and backing off on
// RPC errors (Alchemy free tier limits some methods to ≤ 500 blocks).
func ChildLabels(ctx context.Context, lf LogFilter, registry common.Address, fromBlock uint64) ([]string, error) {
	head, err := lf.HeaderByNumber(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("enskit.ChildLabels: get head: %w", err)
	}
	toBlock := head.Number.Uint64()
	if toBlock < fromBlock {
		return nil, nil
	}

	type labelEntry struct {
		label   string
		tokenID *big.Int
	}

	registered := map[string]labelEntry{} // tokenId.String() → labelEntry
	unregistered := map[string]struct{}{} // tokenId.String() → revoked

	chunkSize := uint64(initialChunkSize)

	for start := fromBlock; start <= toBlock; {
		end := start + chunkSize - 1
		if end > toBlock {
			end = toBlock
		}
		from := new(big.Int).SetUint64(start)
		to := new(big.Int).SetUint64(end)

		logs, err := lf.FilterLogs(ctx, ethereum.FilterQuery{
			FromBlock: from,
			ToBlock:   to,
			Addresses: []common.Address{registry},
			Topics:    [][]common.Hash{{TopicLabelRegistered, TopicLabelUnregistered}},
		})
		if err != nil {
			// Halve range on error (may be too-large-range RPC error).
			half := chunkSize / 2
			if half < minChunkSize {
				return nil, fmt.Errorf("enskit.ChildLabels: FilterLogs [%d,%d]: %w", start, end, err)
			}
			chunkSize = half
			continue
		}

		for _, l := range logs {
			if len(l.Topics) == 0 {
				continue
			}
			switch l.Topics[0] {
			case TopicLabelRegistered:
				// LabelRegistered(uint256 indexed tokenId, bytes32 indexed labelHash, string label, address owner, uint64 expiry, address indexed sender)
				// topics[1] = tokenId, topics[2] = labelHash, topics[3] = sender
				// data = abi-encoded (string label, address owner, uint64 expiry)
				if len(l.Topics) < 2 {
					continue
				}
				tokenID := new(big.Int).SetBytes(l.Topics[1].Bytes())
				label := decodeStringFromLog(l.Data)
				if label != "" {
					registered[tokenID.String()] = labelEntry{label: label, tokenID: tokenID}
				}
			case TopicLabelUnregistered:
				// LabelUnregistered(uint256 indexed tokenId, address indexed sender)
				if len(l.Topics) < 2 {
					continue
				}
				tokenID := new(big.Int).SetBytes(l.Topics[1].Bytes())
				unregistered[tokenID.String()] = struct{}{}
			}
		}
		start = end + 1
		// Gradually increase chunk size after a successful request (up to initial).
		if chunkSize < initialChunkSize {
			chunkSize = min(chunkSize*2, initialChunkSize)
		}
	}

	var out []string
	for tid, e := range registered {
		if _, revoked := unregistered[tid]; !revoked {
			out = append(out, e.label)
		}
	}
	return out, nil
}

// ChildSubregs returns a map of label string → subregistry address for all
// currently registered children in registry.  Labels with no subregistry set
// are included with a zero address.  Revoked labels are excluded.
//
// It scans LabelRegistered, LabelUnregistered and SubregistrySet events in one
// adaptive-chunk pass.
func ChildSubregs(ctx context.Context, lf LogFilter, registry common.Address, fromBlock uint64) (map[string]common.Address, error) {
	head, err := lf.HeaderByNumber(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("enskit.ChildSubregs: get head: %w", err)
	}
	toBlock := head.Number.Uint64()
	if toBlock < fromBlock {
		return map[string]common.Address{}, nil
	}

	type labelEntry struct {
		label   string
		tokenID *big.Int
	}

	registered := map[string]labelEntry{}       // tokenId.String() → labelEntry
	unregistered := map[string]struct{}{}        // tokenId.String() → revoked
	subregs := map[string]common.Address{}       // tokenId.String() → subregistry

	chunkSize := uint64(initialChunkSize)

	for start := fromBlock; start <= toBlock; {
		end := start + chunkSize - 1
		if end > toBlock {
			end = toBlock
		}
		from := new(big.Int).SetUint64(start)
		to := new(big.Int).SetUint64(end)

		logs, err := lf.FilterLogs(ctx, ethereum.FilterQuery{
			FromBlock: from,
			ToBlock:   to,
			Addresses: []common.Address{registry},
			Topics: [][]common.Hash{{
				TopicLabelRegistered,
				TopicLabelUnregistered,
				TopicSubregistrySet,
			}},
		})
		if err != nil {
			half := chunkSize / 2
			if half < minChunkSize {
				return nil, fmt.Errorf("enskit.ChildSubregs: FilterLogs [%d,%d]: %w", start, end, err)
			}
			chunkSize = half
			continue
		}

		for _, l := range logs {
			if len(l.Topics) == 0 {
				continue
			}
			switch l.Topics[0] {
			case TopicLabelRegistered:
				if len(l.Topics) < 2 {
					continue
				}
				tokenID := new(big.Int).SetBytes(l.Topics[1].Bytes())
				label := decodeStringFromLog(l.Data)
				if label != "" {
					registered[tokenID.String()] = labelEntry{label: label, tokenID: tokenID}
				}
			case TopicLabelUnregistered:
				if len(l.Topics) < 2 {
					continue
				}
				tokenID := new(big.Int).SetBytes(l.Topics[1].Bytes())
				unregistered[tokenID.String()] = struct{}{}
			case TopicSubregistrySet:
				// SubregistrySet(uint256 indexed tokenId, address indexed subregistry, address indexed sender)
				if len(l.Topics) < 3 {
					continue
				}
				tokenID := new(big.Int).SetBytes(l.Topics[1].Bytes())
				subreg := common.BytesToAddress(l.Topics[2].Bytes())
				subregs[tokenID.String()] = subreg
			}
		}
		start = end + 1
		if chunkSize < initialChunkSize {
			chunkSize = min(chunkSize*2, initialChunkSize)
		}
	}

	out := map[string]common.Address{}
	for tid, e := range registered {
		if _, revoked := unregistered[tid]; !revoked {
			out[e.label] = subregs[tid] // zero if not set
		}
	}
	return out, nil
}

// decodeStringFromLog decodes the first string from ABI-encoded log data.
// ABI encoding of (string, address, uint64):
//   offset (32 bytes) | address (32 bytes) | expiry (32 bytes) | length (32 bytes) | data...
// But for (string, ...) the first word is the offset to the string data.
func decodeStringFromLog(data []byte) string {
	if len(data) < 64 {
		return ""
	}
	// data = abi.encode(string label, address owner, uint64 expiry)
	// offset to string = data[0:32] as uint256
	offset := new(big.Int).SetBytes(data[:32]).Uint64()
	if uint64(len(data)) < offset+32 {
		return ""
	}
	length := new(big.Int).SetBytes(data[offset : offset+32]).Uint64()
	if uint64(len(data)) < offset+32+length {
		return ""
	}
	raw := data[offset+32 : offset+32+length]
	// labels are lowercase ASCII
	return strings.ToLower(string(raw))
}
