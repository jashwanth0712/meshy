// Package arkivkit writes Meshy audit entities to Arkiv (Rabdi testnet) with
// raw transactions: there is no Go SDK, so we ABI-encode
// execute((uint8,bytes)[]) on the Arkiv system contract ourselves.
package arkivkit

import (
	"errors"
	"fmt"
	"math/big"
	"sort"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Rabdi testnet.
var (
	Address        = common.HexToAddress("0x4400000000000000000000000000000000000044")
	RabdiChainID   = big.NewInt(7794095)
	RabdiRPC       = "https://rpc.rabdi.db-chain.testnet.arkiv.network"
	RabdiExplorer  = "https://rabdi.explorer.arkiv.network"
	MaxLifetime    = uint64(7_884_000) // blocks (~182 days at 2s)
	Lifetime30Days = uint64(1_296_000) // blocks
)

// Operation codes.
const (
	OpCreate   uint8 = 1
	OpPatch    uint8 = 2
	OpExtend   uint8 = 3
	OpTransfer uint8 = 4
	OpDelete   uint8 = 5
)

// Attribute type ids (low 7 bits); Indexed is OR-ed in.
const (
	TBool  uint8 = 1
	TI32   uint8 = 2
	TU64   uint8 = 3
	TU256  uint8 = 4
	TDec   uint8 = 5
	TBytes uint8 = 7
	TStr   uint8 = 8
	TAddr  uint8 = 9
	TKey   uint8 = 10

	Indexed uint8 = 0x80
)

// Flags.
const (
	FlagReadOnly                uint8 = 1 << 0
	FlagPermissionlessExtension uint8 = 1 << 1
)

// Attr is one entity attribute.
type Attr struct {
	Name  string
	Type  uint8 // including Indexed bit
	Value []byte
}

func word(b []byte) []byte { return common.LeftPadBytes(b, 32) }

// Str is a string attribute (≤128 bytes).
func Str(name, v string, indexed bool) Attr { return Attr{name, TStr | idx(indexed), []byte(v)} }

// U64 is a uint64 attribute (sortable when indexed).
func U64(name string, v uint64, indexed bool) Attr {
	return Attr{name, TU64 | idx(indexed), word(new(big.Int).SetUint64(v).Bytes())}
}

// Bool is a boolean attribute.
func Bool(name string, v bool, indexed bool) Attr {
	b := []byte{0}
	if v {
		b = []byte{1}
	}
	return Attr{name, TBool | idx(indexed), word(b)}
}

// AddrA is an address attribute.
func AddrA(name string, a common.Address, indexed bool) Attr {
	return Attr{name, TAddr | idx(indexed), word(a.Bytes())}
}

// KeyA references another entity.
func KeyA(name string, k common.Hash, indexed bool) Attr {
	return Attr{name, TKey | idx(indexed), k.Bytes()}
}

// Bytes is a payload attribute (≤128 KiB, never indexed).
func Bytes(name string, v []byte) Attr { return Attr{name, TBytes, v} }

func idx(b bool) uint8 {
	if b {
		return Indexed
	}
	return 0
}

// Schema is the conventional schema tag attribute used for filtering.
func Schema(name string) Attr { return Str("arkiv.schema", name, true) }

var (
	ErrAttrs = errors.New("arkivkit: invalid attributes")
)

// Create is one create operation.
type Create struct {
	Salt        *big.Int // uint128; nil = 0
	ExpiresAt   uint64   // absolute block; 0 = use MinLifetime
	MinLifetime uint64   // blocks
	Flags       uint8
	Attrs       []Attr
}

type abiAttr struct {
	Name   [32]byte
	TypeId uint8
	Value  []byte
}

type abiOp struct {
	Operation     uint8
	OperationData []byte
}

var (
	tAttrs, _ = abi.NewType("tuple[]", "", []abi.ArgumentMarshaling{
		{Name: "name", Type: "bytes32"}, {Name: "typeId", Type: "uint8"}, {Name: "value", Type: "bytes"},
	})
	tOps, _ = abi.NewType("tuple[]", "", []abi.ArgumentMarshaling{
		{Name: "operation", Type: "uint8"}, {Name: "operationData", Type: "bytes"},
	})
	// Create data is abi.encode of ONE tuple (as in Arkiv's viem sample), which
	// differs from flat params by a leading offset word (dynamic tuple).
	tCreate, _ = abi.NewType("tuple", "", []abi.ArgumentMarshaling{
		{Name: "salt", Type: "uint128"}, {Name: "expiresAt", Type: "uint64"}, {Name: "minLifetime", Type: "uint64"},
		{Name: "creationFlags", Type: "uint8"},
		{Name: "attributes", Type: "tuple[]", Components: []abi.ArgumentMarshaling{
			{Name: "name", Type: "bytes32"}, {Name: "typeId", Type: "uint8"}, {Name: "value", Type: "bytes"},
		}},
	})
	createArgs  = abi.Arguments{{Type: tCreate}}
	executeArgs = abi.Arguments{{Type: tOps}}
	// execute((uint8,bytes)[]) — matches the documented selector 0x49650044.
	executeSelector = crypto.Keccak256([]byte("execute((uint8,bytes)[])"))[:4]
)

// Encode validates and ABI-encodes the create operation data.
func (c Create) Encode() ([]byte, error) {
	if len(c.Attrs) > 32 {
		return nil, fmt.Errorf("%w: %d attributes (max 32)", ErrAttrs, len(c.Attrs))
	}
	attrs := append([]Attr(nil), c.Attrs...)
	sort.Slice(attrs, func(i, j int) bool { return attrs[i].Name < attrs[j].Name })
	enc := make([]abiAttr, len(attrs))
	for i, a := range attrs {
		if a.Name == "" || len(a.Name) > 32 || a.Name[0] == '$' {
			return nil, fmt.Errorf("%w: bad name %q", ErrAttrs, a.Name)
		}
		if i > 0 && attrs[i-1].Name == a.Name {
			return nil, fmt.Errorf("%w: duplicate %q", ErrAttrs, a.Name)
		}
		switch a.Type &^ Indexed {
		case TStr:
			if len(a.Value) > 128 {
				return nil, fmt.Errorf("%w: %s is %d bytes (str max 128)", ErrAttrs, a.Name, len(a.Value))
			}
		case TBytes:
			if a.Type&Indexed != 0 {
				return nil, fmt.Errorf("%w: bytes %s cannot be indexed", ErrAttrs, a.Name)
			}
			if len(a.Value) > 128<<10 {
				return nil, fmt.Errorf("%w: %s is %d bytes (bytes max 128KiB)", ErrAttrs, a.Name, len(a.Value))
			}
		}
		copy(enc[i].Name[:], a.Name)
		enc[i].TypeId, enc[i].Value = a.Type, a.Value
	}
	salt := c.Salt
	if salt == nil {
		salt = new(big.Int)
	}
	life := c.MinLifetime
	if c.ExpiresAt == 0 && life == 0 {
		life = Lifetime30Days
	}
	if life > MaxLifetime {
		life = MaxLifetime
	}
	return createArgs.Pack(abiCreate{Salt: salt, ExpiresAt: c.ExpiresAt, MinLifetime: life, CreationFlags: c.Flags, Attributes: enc})
}

type abiCreate struct {
	Salt          *big.Int
	ExpiresAt     uint64
	MinLifetime   uint64
	CreationFlags uint8
	Attributes    []abiAttr
}

// ExecuteCalldata builds calldata for a batch of create operations
// (one transaction, atomic).
func ExecuteCalldata(creates ...Create) ([]byte, error) {
	ops := make([]abiOp, len(creates))
	for i, c := range creates {
		data, err := c.Encode()
		if err != nil {
			return nil, fmt.Errorf("op %d: %w", i, err)
		}
		ops[i] = abiOp{OpCreate, data}
	}
	packed, err := executeArgs.Pack(ops)
	if err != nil {
		return nil, err
	}
	return append(append([]byte{}, executeSelector...), packed...), nil
}

// EntityCreatedTopic is keccak("EntityCreated(bytes32,address,uint64,uint8)").
var EntityCreatedTopic = crypto.Keccak256Hash([]byte("EntityCreated(bytes32,address,uint64,uint8)"))
