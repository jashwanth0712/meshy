# Arkiv friction

> "What should Arkiv improve today, including gaps that made building harder?
> When something broke, what did the team expect, what happened, and how can it be reproduced?"

---

## 1. No Go SDK — had to hand-encode `execute((uint8,bytes)[])`

**Expected:** a first-party Go package equivalent to the viem plugin.

**Actual:** nothing exists. We wrote `meshy/arkivkit` from scratch: ABI-encode the
tuple, build a raw EIP-1559 transaction, sign it, send it over `eth_sendRawTransaction`.

**Repro:** `go get arkiv.network/go-sdk` — not found. See `meshy/arkivkit/encode.go` for
our encoding and `meshy/arkivkit/writer.go` for the transaction layer.

---

## 2. Create op data must be a **single ABI tuple** — encoding 5 fields flat reverts

**Expected (from the viem sample in the docs):** encode the 5 create fields
(`salt`, `expiresAt`, `minLifetime`, `creationFlags`, `attributes`) as plain positional arguments.

**Actual:** the contract requires them wrapped in a *single dynamic tuple*, which adds a
leading 32-byte offset word (`0x0000…0020`). Encoding them flat (as 5 separate `abi.Pack`
arguments) causes the contract to revert with the custom error
`NonCanonicalOperationData(1)` — selector `0x89bc9448`, not registered in any 4byte
directory at the time we hit it.

We found the error name only by cross-referencing the full error list in the Arkiv source
after brute-forcing the selector.

**Repro:**
```go
// WRONG — reverts NonCanonicalOperationData(1)
flat, _ := abi.Arguments{saltArg, expiresAtArg, ...}.Pack(salt, exp, life, flags, attrs)

// CORRECT — the tuple wrapper adds the offset word
type create struct { Salt *big.Int; ExpiresAt, MinLifetime uint64; CreationFlags uint8; Attributes []attr }
packed, _ := abi.Arguments{{Type: tCreate}}.Pack(create{...})
```

See `meshy/arkivkit/encode.go`, `createArgs`, `Encode()`.

The viem sample implies the tuple wrapper but doesn't state it explicitly. A note such as
"the operationData for `create` is `abi.encode((uint128,uint64,uint64,uint8,Attribute[]))`,
not `abi.encode(uint128,uint64,uint64,uint8,Attribute[])`" would prevent hours of debugging.

---

## 3. `NOT` / `!=` and cursor pagination disagree between docs pages and Rabdi

**Expected:** `NOT` / `!=` and cursor-based pagination work as documented.

**Actual:** the reference page and the Rabdi-specific page use different syntax and return
different shapes in some cursor responses. We avoided `NOT` and `!=` entirely and
paginated with `ORDER BY ts:u64 DESC` + a `limit` cap.

**Repro:** query `arkiv.schema != str('meshy.session')` on Rabdi via the public RPC.

---

## 4. Quota / cost-unit numbers are not published

**Expected:** a table or formula: "X cost units per indexed attribute, Y per byte of
payload, Z per block of lifetime."

**Actual:** nothing is published. We learned by trying small batches and watching the
access-key balance. We cannot predict whether a production org with 1,000 sessions/day
stays within any tier.

---

## 5. `$createdAt` is not sortable

**Expected:** `ORDER BY $createdAt DESC` works as a natural chronological sort.

**Actual:** the query returns an error. We work around it by writing our own `ts:u64`
attribute (milliseconds since epoch) on every entity and sorting by that.

This costs one indexed u64 slot per entity but is necessary for the feed ordering
the dashboard depends on.

---

## 6. `watchEntityEvents` events carry no attributes — must re-read

**Expected:** the subscription callback includes the entity's attributes so the dashboard
can render an update without an extra round trip.

**Actual:** the event carries only the entity key. The dashboard must call
`arkiv_query` again to get the attributes, doubling latency for live updates.

We currently poll every 5 s instead of using `watchEntityEvents`. Subscriptions would
reduce latency and load, but the extra round-trip cost made the polling approach simpler
to reason about and test.

---

## 7. Faucet requires a wallet connection — awkward for headless daemons

**Expected:** paste an address, receive testnet funds.

**Actual:** the Rabdi faucet UI requires MetaMask (or equivalent) to be connected before
it will send funds. Funding the resource daemon key (`0x7Dec34A86907eeF26c70A6578df7bAaE7F223384`)
in a CI or headless environment requires either a funded EOA signing a transfer or a
developer manually connecting a wallet.

A `POST /faucet?address=0x...` endpoint (no wallet required) would let daemons self-fund
on first boot.
