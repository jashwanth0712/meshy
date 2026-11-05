package arkivkit

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Chain is the subset of ethclient.Client the writer needs.
type Chain interface {
	PendingNonceAt(ctx context.Context, account common.Address) (uint64, error)
	SuggestGasTipCap(ctx context.Context) (*big.Int, error)
	HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error)
	EstimateGas(ctx context.Context, msg ethereum.CallMsg) (uint64, error)
	SendTransaction(ctx context.Context, tx *types.Transaction) error
}

// Writer signs and sends Arkiv execute() transactions.
type Writer struct {
	Chain   Chain
	Key     *ecdsa.PrivateKey
	ChainID *big.Int

	mu sync.Mutex
}

// Dial connects a writer to an Arkiv RPC (e.g. RabdiRPC + access key).
func Dial(ctx context.Context, rpcURL string, key *ecdsa.PrivateKey) (*Writer, error) {
	c, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return nil, err
	}
	id, err := c.ChainID(ctx)
	if err != nil {
		return nil, fmt.Errorf("arkivkit: chain id: %w", err)
	}
	return &Writer{Chain: c, Key: key, ChainID: id}, nil
}

// From is the writer's address (shown in the README for judges).
func (w *Writer) From() common.Address { return crypto.PubkeyToAddress(w.Key.PublicKey) }

// Execute sends one atomic batch of creates and returns the tx hash.
func (w *Writer) Execute(ctx context.Context, creates ...Create) (common.Hash, error) {
	data, err := ExecuteCalldata(creates...)
	if err != nil {
		return common.Hash{}, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	from := w.From()
	nonce, err := w.Chain.PendingNonceAt(ctx, from)
	if err != nil {
		return common.Hash{}, fmt.Errorf("arkivkit: nonce: %w", err)
	}
	tip, err := w.Chain.SuggestGasTipCap(ctx)
	if err != nil {
		tip = big.NewInt(1_000_000_000)
	}
	head, err := w.Chain.HeaderByNumber(ctx, nil)
	if err != nil {
		return common.Hash{}, fmt.Errorf("arkivkit: head: %w", err)
	}
	base := head.BaseFee
	if base == nil {
		base = big.NewInt(1_000_000_000)
	}
	feeCap := new(big.Int).Add(new(big.Int).Mul(base, big.NewInt(2)), tip)
	to := Address
	gas, err := w.Chain.EstimateGas(ctx, ethereum.CallMsg{From: from, To: &to, Data: data})
	if err != nil {
		return common.Hash{}, fmt.Errorf("arkivkit: estimate gas (operation rejected?): %w", err)
	}
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID: w.ChainID, Nonce: nonce, GasTipCap: tip, GasFeeCap: feeCap,
		Gas: gas + gas/3, To: &to, Value: new(big.Int), Data: data,
	})
	signed, err := types.SignTx(tx, types.LatestSignerForChainID(w.ChainID), w.Key)
	if err != nil {
		return common.Hash{}, err
	}
	if err := w.Chain.SendTransaction(ctx, signed); err != nil {
		return common.Hash{}, fmt.Errorf("arkivkit: send: %w", err)
	}
	return signed.Hash(), nil
}
