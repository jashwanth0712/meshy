# Meshy Contracts

Foundry (solc 0.8.24) smart contracts for the Meshy decentralized network-access platform.

## Contracts

### `ProviderRegistry.sol`

Staked provider registry. Providers deposit ≥ 0.01 ETH of stake and register an endpoint, signer key, and per-seat USDC price. The signer key is used to sign **EIP-712 admission attestations** that prove a peer was admitted to a named resource at a given timestamp.

**Fraud proof (`slash`):** A reporter can slash a provider by submitting a signed attestation together with on-chain ENSv2 evidence that the named resource was already dead at `issuedAt`. Three fraud conditions:

| Condition | ENSv2 check |
|---|---|
| Ancestor dead | `UniversalHelper.findParentRegistry(dnsName) == address(0)` |
| Name expired at issuance | `IPermissionedRegistry.getExpiry(labelhash) <= a.issuedAt` |
| Resource re-registered (name unregistered then re-registered, or version changed) | `IPermissionedRegistry.getResource(labelhash) != a.resource` |

Reporter receives 50 % of the slashed stake; the rest is burned to `0x000...dEaD`.

**Limitation:** If the name was renewed _before_ the fraud proof is submitted, condition 2 will not fire even if the name was expired at `issuedAt`. Similarly, a re-registration that happens to produce the same resource id (extremely unlikely in ENSv2 but theoretically possible) would mask condition 3. Monitors should slash promptly after detecting a suspicious attestation.

**Unbonding:** `requestUnbond()` starts a delay (1 day on demo, 7 days in production), after which `withdraw()` returns the stake.

### `MeshySubscription.sol`

Per-seat monthly billing in MockUSDC (6 decimals). Escrow + claim design for safe cancellation refunds.

- `subscribe(orgNode, providerId, seats, months)` pulls USDC from the payer: 5% goes immediately to treasury, 95% is escrowed.
- `addSeats(orgNode, extra)` adds seats, prorated to the current `paidUntil` (ceiling of remaining days / 30).
- `renew(orgNode, months)` extends `paidUntil`.
- `cancel(orgNode)` refunds whole unelapsed months from escrow to the payer; elapsed months remain claimable by the provider.
- `claim(providerId)` releases all accumulated escrow to the provider owner.

## Sepolia Deployment

| Contract | Address |
|---|---|
| ProviderRegistry | `0x8B93557b315E825C657D48A4CB962A3fF7E4eEec` |
| MeshySubscription | `0x83989608D93858c1D835216B44B44469Dc8204Df` |
| MockUSDC (ENSv2) | `0x240b0316Df57887DBBE58b586508b19e633a14aa` |
| UniversalHelper (ENSv2) | `0xd453e5Bdb62CC3beA84341B1e306319C8Ffd7DFe` |

Both contracts verified (exact_match) on [Sourcify](https://sourcify.dev), [Blockscout Sepolia](https://eth-sepolia.blockscout.com), and Routescan.

Demo provider ID: **1** — endpoint `https://provider.meshy.local`, 2 USDC/seat/month.  
Demo org: `meshydemo.eth` subscribed for 5 seats × 1 month.

## Running Tests

```bash
forge test -v
```

Output: 31 tests pass, 1 fork test skipped (set `SEPOLIA_RPC` to run it).

```bash
SEPOLIA_RPC=https://eth-sepolia.g.alchemy.com/v2/<KEY> forge test --match-contract ForkTest -vv
```

## Deploying (reference)

The deploy script is `script/Deploy.s.sol`. For fresh deploys, use `cast send --create` directly since `forge script --broadcast` can have nonce issues with EIP-7702 delegation addresses on Sepolia. See `docs/PLAN.md` for the full workflow.

Required env: `PRIVATE_KEY`, `ATTESTOR_ADDRESS`.

## ABI Files

Machine-readable ABIs are at `abi/ProviderRegistry.json` and `abi/MeshySubscription.json`.
Full deployment metadata is at `deployments/sepolia.json`.
