package enskit

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Sepolia ENSv2 contracts used by admin flows.
var (
	SepoliaETHRegistry       = common.HexToAddress("0xd4ebcbbdf463c9c45784603db0ddd499bc44a8b4")
	SepoliaETHRegistrar      = common.HexToAddress("0xf633e7fc17e2bbe0d0965d18ec1821dcb754a3d3")
	SepoliaVerifiableFactory = common.HexToAddress("0xda70306c98e97ece36f997a21368e53298572991")
	SepoliaUserRegistryImpl  = common.HexToAddress("0x9bd8a88719068d09ecee662f36c0e3856708366a")
	SepoliaResolverImpl      = common.HexToAddress("0x115eb53f0c60696633855f90b138178fb40b2b2c")
	SepoliaMockUSDC          = common.HexToAddress("0x240b0316df57887dbbe58b586508b19e633a14aa")
)

// Role bits (RegistryRolesLib / PermissionedResolverLib). Admin = role << 128.
var (
	RoleRegistrar       = big.NewInt(1)
	RoleUnregister      = new(big.Int).Lsh(big.NewInt(1), 12)
	RoleRenew           = new(big.Int).Lsh(big.NewInt(1), 16)
	RoleSetSubregistry  = new(big.Int).Lsh(big.NewInt(1), 20)
	RoleSetResolver     = new(big.Int).Lsh(big.NewInt(1), 24)
	ResolverSetAddr     = big.NewInt(1)
	ResolverSetText     = new(big.Int).Lsh(big.NewInt(1), 4)
	ResolverSetData     = new(big.Int).Lsh(big.NewInt(1), 24)
	// AllRoles = every role and its admin (0x1111...1111, 64 nybbles).
	AllRoles, _ = new(big.Int).SetString(strings.Repeat("1", 64), 16)
)

const adminABI = `[
 {"type":"function","name":"deployProxy","inputs":[{"name":"implementation","type":"address"},{"name":"salt","type":"uint256"},{"name":"data","type":"bytes"}],"outputs":[{"type":"address"}]},
 {"type":"event","name":"ProxyDeployed","inputs":[{"name":"sender","type":"address","indexed":true},{"name":"proxyAddress","type":"address","indexed":true},{"name":"salt","type":"uint256","indexed":false},{"name":"implementation","type":"address","indexed":false}]},
 {"type":"function","name":"initializeRegistry","inputs":[{"name":"grants","type":"tuple[]","components":[{"name":"account","type":"address"},{"name":"roleBitmap","type":"uint256"}]}],"outputs":[]},
 {"type":"function","name":"initializeResolver","inputs":[{"name":"grants","type":"tuple[]","components":[{"name":"account","type":"address"},{"name":"roleBitmap","type":"uint256"}]},{"name":"calls","type":"bytes[]"}],"outputs":[]},
 {"type":"function","name":"register","inputs":[{"name":"label","type":"string"},{"name":"owner","type":"address"},{"name":"registry","type":"address"},{"name":"resolver","type":"address"},{"name":"roleBitmap","type":"uint256"},{"name":"expiry","type":"uint64"}],"outputs":[{"type":"uint256"}]},
 {"type":"function","name":"unregister","inputs":[{"name":"anyId","type":"uint256"}],"outputs":[]},
 {"type":"function","name":"renew","inputs":[{"name":"anyId","type":"uint256"},{"name":"newExpiry","type":"uint64"}],"outputs":[]},
 {"type":"function","name":"setSubregistry","inputs":[{"name":"anyId","type":"uint256"},{"name":"registry","type":"address"}],"outputs":[]},
 {"type":"function","name":"setResolver","inputs":[{"name":"anyId","type":"uint256"},{"name":"resolver","type":"address"}],"outputs":[]},
 {"type":"function","name":"grantRootRoles","inputs":[{"name":"roleBitmap","type":"uint256"},{"name":"account","type":"address"}],"outputs":[{"type":"bool"}]},
 {"type":"function","name":"setText","inputs":[{"name":"name","type":"bytes"},{"name":"key","type":"string"},{"name":"value","type":"string"}],"outputs":[]},
 {"type":"function","name":"setAddress","inputs":[{"name":"name","type":"bytes"},{"name":"coinType","type":"uint256"},{"name":"addressBytes","type":"bytes"}],"outputs":[]},
 {"type":"function","name":"multicall","inputs":[{"name":"data","type":"bytes[]"}],"outputs":[{"type":"bytes[]"}]},
 {"type":"function","name":"makeCommitment","stateMutability":"view","inputs":[{"name":"label","type":"string"},{"name":"owner","type":"address"},{"name":"secret","type":"bytes32"},{"name":"subregistry","type":"address"},{"name":"resolver","type":"address"},{"name":"duration","type":"uint64"},{"name":"referrer","type":"bytes32"}],"outputs":[{"type":"bytes32"}]},
 {"type":"function","name":"commit","inputs":[{"name":"commitment","type":"bytes32"}],"outputs":[]},
 {"type":"function","name":"registerETH","inputs":[{"name":"label","type":"string"},{"name":"owner","type":"address"},{"name":"secret","type":"bytes32"},{"name":"subregistry","type":"address"},{"name":"resolver","type":"address"},{"name":"duration","type":"uint64"},{"name":"paymentToken","type":"address"},{"name":"referrer","type":"bytes32"}],"outputs":[{"type":"uint256"}]},
 {"type":"function","name":"getRegisterPrice","stateMutability":"view","inputs":[{"name":"label","type":"string"},{"name":"duration","type":"uint64"},{"name":"paymentToken","type":"address"}],"outputs":[{"name":"base","type":"uint256"},{"name":"premium","type":"uint256"}]},
 {"type":"function","name":"isAvailable","stateMutability":"view","inputs":[{"name":"label","type":"string"}],"outputs":[{"type":"bool"}]},
 {"type":"function","name":"mint","inputs":[{"name":"to","type":"address"},{"name":"amount","type":"uint256"}],"outputs":[]},
 {"type":"function","name":"approve","inputs":[{"name":"spender","type":"address"},{"name":"amount","type":"uint256"}],"outputs":[{"type":"bool"}]}
]`

var admABI = func() abi.ABI {
	a, err := abi.JSON(strings.NewReader(adminABI))
	if err != nil {
		panic(err)
	}
	return a
}()

// pack encodes a call, renaming our disambiguated helper names to the real
// Solidity function names (same selector rules: name + arg types).
func pack(method string, args ...any) ([]byte, error) {
	m, ok := admABI.Methods[method]
	if !ok {
		return nil, fmt.Errorf("enskit: unknown method %s", method)
	}
	real := map[string]string{"initializeRegistry": "initialize", "initializeResolver": "initialize", "registerETH": "register"}[method]
	if real == "" {
		real = m.Name
	}
	sig := real + "(" + strings.Join(typesOf(m.Inputs), ",") + ")"
	in, err := m.Inputs.Pack(args...)
	if err != nil {
		return nil, fmt.Errorf("enskit: pack %s: %w", method, err)
	}
	return append(crypto.Keccak256([]byte(sig))[:4], in...), nil
}

func typesOf(args abi.Arguments) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = a.Type.String()
	}
	return out
}

type grant struct {
	Account    common.Address
	RoleBitmap *big.Int
}

// Admin sends ENSv2 write transactions from one key.
type Admin struct {
	Eth     *ethclient.Client
	Key     *ecdsa.PrivateKey
	ChainID *big.Int
	// Wait is how admin flows wait out the registrar commitment age. On a
	// local fork pass a function that warps time (evm_increaseTime).
	Wait func(ctx context.Context, d time.Duration) error
}

// NewAdmin dials rpc and prepares an admin.
func NewAdmin(ctx context.Context, rpc string, key *ecdsa.PrivateKey) (*Admin, error) {
	c, err := ethclient.DialContext(ctx, rpc)
	if err != nil {
		return nil, err
	}
	id, err := c.ChainID(ctx)
	if err != nil {
		return nil, err
	}
	return &Admin{Eth: c, Key: key, ChainID: id, Wait: func(ctx context.Context, d time.Duration) error {
		select {
		case <-time.After(d):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}, nil
}

// From is the admin address.
func (a *Admin) From() common.Address { return crypto.PubkeyToAddress(a.Key.PublicKey) }

// Send signs a call, sends it and waits for a successful receipt.
func (a *Admin) Send(ctx context.Context, to common.Address, data []byte) (*types.Receipt, error) {
	from := a.From()
	nonce, err := a.Eth.PendingNonceAt(ctx, from)
	if err != nil {
		return nil, err
	}
	gas, err := a.Eth.EstimateGas(ctx, ethereum.CallMsg{From: from, To: &to, Data: data})
	if err != nil {
		return nil, fmt.Errorf("enskit: estimate (would revert): %w", err)
	}
	tip, err := a.Eth.SuggestGasTipCap(ctx)
	if err != nil {
		tip = big.NewInt(2_000_000_000)
	}
	// Clamp to [1, 10] gwei. Never multiply the suggestion: on quiet chains
	// (or a fork) it is derived from our own previous txs and would grow
	// exponentially. 1+ gwei is plenty for next-block inclusion of revokes.
	tip = ClampTip(tip)
	head, err := a.Eth.HeaderByNumber(ctx, nil)
	if err != nil {
		return nil, err
	}
	feeCap := new(big.Int).Add(new(big.Int).Mul(head.BaseFee, big.NewInt(3)), tip)
	tx, err := types.SignTx(types.NewTx(&types.DynamicFeeTx{
		ChainID: a.ChainID, Nonce: nonce, GasTipCap: tip, GasFeeCap: feeCap,
		Gas: gas + gas/4, To: &to, Data: data,
	}), types.LatestSignerForChainID(a.ChainID), a.Key)
	if err != nil {
		return nil, err
	}
	if err := a.Eth.SendTransaction(ctx, tx); err != nil {
		return nil, err
	}
	for {
		r, err := a.Eth.TransactionReceipt(ctx, tx.Hash())
		if err == nil {
			if r.Status != types.ReceiptStatusSuccessful {
				return r, fmt.Errorf("enskit: tx %s reverted", tx.Hash().Hex())
			}
			return r, nil
		}
		if !errors.Is(err, ethereum.NotFound) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (a *Admin) call(ctx context.Context, to common.Address, method string, args ...any) ([]any, error) {
	data, err := pack(method, args...)
	if err != nil {
		return nil, err
	}
	out, err := a.Eth.CallContract(ctx, ethereum.CallMsg{From: a.From(), To: &to, Data: data}, nil)
	if err != nil {
		return nil, err
	}
	return admABI.Methods[method].Outputs.Unpack(out)
}

func (a *Admin) tx(ctx context.Context, to common.Address, method string, args ...any) (*types.Receipt, error) {
	data, err := pack(method, args...)
	if err != nil {
		return nil, err
	}
	return a.Send(ctx, to, data)
}

// ClampTip bounds a suggested priority fee to [1, 10] gwei.
func ClampTip(tip *big.Int) *big.Int {
	lo, hi := big.NewInt(1_000_000_000), big.NewInt(10_000_000_000)
	switch {
	case tip == nil || tip.Cmp(lo) < 0:
		return lo
	case tip.Cmp(hi) > 0:
		return hi
	}
	return tip
}

func randomSalt() *big.Int {
	b := make([]byte, 32)
	rand.Read(b)
	return new(big.Int).SetBytes(b)
}

func (a *Admin) deploy(ctx context.Context, impl common.Address, init []byte) (common.Address, error) {
	r, err := a.tx(ctx, SepoliaVerifiableFactory, "deployProxy", impl, randomSalt(), init)
	if err != nil {
		return common.Address{}, err
	}
	ev := admABI.Events["ProxyDeployed"]
	for _, l := range r.Logs {
		if len(l.Topics) == 3 && l.Topics[0] == ev.ID {
			return common.BytesToAddress(l.Topics[2].Bytes()), nil
		}
	}
	return common.Address{}, errors.New("enskit: ProxyDeployed event not found")
}

// DeployRegistry deploys a UserRegistry where admins hold every role.
func (a *Admin) DeployRegistry(ctx context.Context, admins ...common.Address) (common.Address, error) {
	if len(admins) == 0 {
		admins = []common.Address{a.From()}
	}
	gs := make([]grant, len(admins))
	for i, ad := range admins {
		gs[i] = grant{ad, AllRoles}
	}
	init, err := pack("initializeRegistry", gs)
	if err != nil {
		return common.Address{}, err
	}
	return a.deploy(ctx, SepoliaUserRegistryImpl, init)
}

// DeployResolver deploys a PermissionedResolver where admins hold every role.
func (a *Admin) DeployResolver(ctx context.Context, admins ...common.Address) (common.Address, error) {
	if len(admins) == 0 {
		admins = []common.Address{a.From()}
	}
	gs := make([]grant, len(admins))
	for i, ad := range admins {
		gs[i] = grant{ad, AllRoles}
	}
	init, err := pack("initializeResolver", gs, [][]byte{})
	if err != nil {
		return common.Address{}, err
	}
	return a.deploy(ctx, SepoliaResolverImpl, init)
}

// RegisterETH registers label.eth (commit, wait, pay MockUSDC, register).
func (a *Admin) RegisterETH(ctx context.Context, label string, owner, subregistry, resolver common.Address, duration time.Duration) (*types.Receipt, error) {
	var secret [32]byte
	rand.Read(secret[:])
	dur := uint64(duration.Seconds())
	res, err := a.call(ctx, SepoliaETHRegistrar, "makeCommitment", label, owner, secret, subregistry, resolver, dur, [32]byte{})
	if err != nil {
		return nil, fmt.Errorf("makeCommitment: %w", err)
	}
	if _, err := a.tx(ctx, SepoliaETHRegistrar, "commit", res[0].([32]byte)); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	price, err := a.call(ctx, SepoliaETHRegistrar, "getRegisterPrice", label, dur, SepoliaMockUSDC)
	if err != nil {
		return nil, fmt.Errorf("price: %w", err)
	}
	total := new(big.Int).Add(price[0].(*big.Int), price[1].(*big.Int))
	if _, err := a.tx(ctx, SepoliaMockUSDC, "mint", a.From(), total); err != nil {
		return nil, fmt.Errorf("mint MockUSDC: %w", err)
	}
	if _, err := a.tx(ctx, SepoliaMockUSDC, "approve", SepoliaETHRegistrar, total); err != nil {
		return nil, fmt.Errorf("approve: %w", err)
	}
	if err := a.Wait(ctx, 65*time.Second); err != nil {
		return nil, err
	}
	return a.tx(ctx, SepoliaETHRegistrar, "registerETH", label, owner, secret, subregistry, resolver, dur, SepoliaMockUSDC, [32]byte{})
}

// Register registers label in registry.
func (a *Admin) Register(ctx context.Context, registry common.Address, label string, owner, subregistry, resolver common.Address, roles *big.Int, expiry time.Time) (*types.Receipt, error) {
	if roles == nil {
		roles = new(big.Int)
	}
	return a.tx(ctx, registry, "register", label, owner, subregistry, resolver, roles, uint64(expiry.Unix()))
}

// Unregister revokes label in registry immediately.
func (a *Admin) Unregister(ctx context.Context, registry common.Address, label string) (*types.Receipt, error) {
	return a.tx(ctx, registry, "unregister", LabelID(label))
}

// SetSubregistry points label (in registry) at sub.
func (a *Admin) SetSubregistry(ctx context.Context, registry common.Address, label string, sub common.Address) (*types.Receipt, error) {
	return a.tx(ctx, registry, "setSubregistry", LabelID(label), sub)
}

// SetResolver sets label's resolver in registry.
func (a *Admin) SetResolver(ctx context.Context, registry common.Address, label string, resolver common.Address) (*types.Receipt, error) {
	return a.tx(ctx, registry, "setResolver", LabelID(label), resolver)
}

// Records is a batch of record writes for one name.
type RecordSet struct {
	Name  Name
	Addr  *common.Address
	Texts map[string]string
}

// SetRecords writes records via one multicall on resolver.
func (a *Admin) SetRecords(ctx context.Context, resolver common.Address, sets ...RecordSet) (*types.Receipt, error) {
	var calls [][]byte
	for _, s := range sets {
		dns, err := DNSEncode(string(s.Name))
		if err != nil {
			return nil, err
		}
		if s.Addr != nil {
			c, err := pack("setAddress", dns, big.NewInt(60), s.Addr.Bytes())
			if err != nil {
				return nil, err
			}
			calls = append(calls, c)
		}
		for k, v := range s.Texts {
			c, err := pack("setText", dns, k, v)
			if err != nil {
				return nil, err
			}
			calls = append(calls, c)
		}
	}
	if len(calls) == 0 {
		return nil, nil
	}
	return a.tx(ctx, resolver, "multicall", calls)
}
