# meshnet: `meshy-mesh`

`meshy-mesh` joins a Meshy provider's WireGuard mesh in userspace using NetBird's [`client/embed`](https://github.com/netbirdio/netbird/tree/main/client/embed) (BSD-3). It needs no TUN device, no root, and no NET_ADMIN.

```bash
go build -o meshy-mesh .
# resource: mesh :22 -> meshyd on localhost
meshy-mesh serve --setup-key KEY --management https://provider.example --forward 22=127.0.0.1:2222
# agent: ssh ProxyCommand, so the agent is its own mesh peer
ssh -o ProxyCommand='meshy-mesh proxy --management https://provider.example %h %p' claude-1@db1.prod.acme.eth
```

It is a separate Go module because NetBird v0.82 and go-ethereum, which meshyd uses, need incompatible `pion/stun/v3` versions.
