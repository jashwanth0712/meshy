// SPDX-License-Identifier: AGPL-3.0
pragma solidity ^0.8.24;

import {ENSv2, RoleGrant, IVerifiableFactory, IPermissionedResolverInit, IPermissionedRegistry, IPermissionedResolver} from "./ENSv2.sol";

/// @title MeshyInvites
/// @notice Self-serve, trustless invitations into a Meshy org.
/// An org Owner/Admin signs an EIP-712 `Invite` off-chain (free). Anyone holding it can
/// `claim` with one transaction: the contract checks the signer still holds REGISTRAR on
/// the target registry (people.<org> or agents.<org>), the invite is unexpired/unused and
/// (optionally) addressed to the claimer, then registers `<label>.<parentName>` to the
/// claimer with a resolver of their own (addr + meshy.ssh set).
/// The org grants this contract REGISTRAR on its people/agents registries at creation.
contract MeshyInvites {
    bytes32 public constant INVITE_TYPEHASH = keccak256(
        "Invite(address registry,string parentName,address otherRegistry,string label,address invitee,uint64 expiresAt,uint64 nameExpiry,uint256 nonce)"
    );
    bytes32 public immutable DOMAIN_SEPARATOR;

    IVerifiableFactory public immutable verifiableFactory;
    address public immutable resolverImpl;

    struct Invite {
        address registry; // people.<org> or agents.<org> registry
        string parentName; // "people.acme.meshy.eth"
        address otherRegistry; // the sibling registry, for cross-registry label uniqueness (0 = skip)
        string label; // "" = claimer chooses
        address invitee; // 0 = anyone holding the invite
        uint64 expiresAt; // invite validity
        uint64 nameExpiry; // expiry of the claimed name
        uint256 nonce;
    }

    mapping(address => mapping(uint256 => bool)) public used; // issuer => nonce => used/cancelled
    uint256 private _salt;
    address private _lastIssuer; // for the Claimed event (avoids stack-too-deep)
    uint256 private _lastNonce;

    event Claimed(address indexed issuer, uint256 indexed nonce, address indexed claimer, string name, address resolver);
    event Cancelled(address indexed issuer, uint256 indexed nonce);

    constructor(IVerifiableFactory _verifiableFactory, address _resolverImpl) {
        verifiableFactory = _verifiableFactory;
        resolverImpl = _resolverImpl;
        DOMAIN_SEPARATOR = keccak256(
            abi.encode(
                keccak256("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"),
                keccak256("MeshyInvites"),
                keccak256("1"),
                block.chainid,
                address(this)
            )
        );
    }

    function hashInvite(Invite calldata inv) public view returns (bytes32) {
        return keccak256(
            abi.encodePacked(
                "\x19\x01",
                DOMAIN_SEPARATOR,
                keccak256(
                    abi.encode(
                        INVITE_TYPEHASH,
                        inv.registry,
                        keccak256(bytes(inv.parentName)),
                        inv.otherRegistry,
                        keccak256(bytes(inv.label)),
                        inv.invitee,
                        inv.expiresAt,
                        inv.nameExpiry,
                        inv.nonce
                    )
                )
            )
        );
    }

    function issuerOf(Invite calldata inv, bytes calldata sig) public view returns (address) {
        return _recover(hashInvite(inv), sig);
    }

    /// Why an invite can't be claimed right now ("" = claimable by `claimer` with `label`).
    function check(Invite calldata inv, bytes calldata sig, address claimer, string calldata label)
        public
        view
        returns (string memory)
    {
        address issuer = _recover(hashInvite(inv), sig);
        if (issuer == address(0)) return "bad signature";
        if (!IPermissionedRegistry(inv.registry).hasRootRoles(ENSv2.ROLE_REGISTRAR, issuer)) return "issuer is not an admin";
        if (used[issuer][inv.nonce]) return "invite used or cancelled";
        if (block.timestamp > inv.expiresAt) return "invite expired";
        if (inv.invitee != address(0) && inv.invitee != claimer) return "invite is for another wallet";
        if (bytes(inv.label).length > 0 && keccak256(bytes(inv.label)) != keccak256(bytes(label))) return "label mismatch";
        if (!ENSv2.validLabel(label)) return "invalid name";
        uint256 id = ENSv2.labelId(label);
        if (IPermissionedRegistry(inv.registry).getExpiry(id) > block.timestamp) return "name taken";
        if (inv.otherRegistry != address(0) && IPermissionedRegistry(inv.otherRegistry).getExpiry(id) > block.timestamp) {
            return "name taken";
        }
        return "";
    }

    /// @param label   the name to claim (must equal inv.label if that is set)
    /// @param sshKey  authorized_keys line for meshy.ssh (may be empty; settable later)
    function claim(Invite calldata inv, bytes calldata sig, string calldata label, string calldata sshKey)
        external
        returns (string memory name)
    {
        string memory why = check(inv, sig, msg.sender, label);
        require(bytes(why).length == 0, string.concat("MI: ", why));
        _consume(_recover(hashInvite(inv), sig), inv.nonce);
        name = string.concat(label, ".", inv.parentName);
        _register(inv.registry, label, name, sshKey, inv.nameExpiry);
    }

    function _consume(address issuer, uint256 nonce) internal {
        used[issuer][nonce] = true;
        _lastIssuer = issuer;
        _lastNonce = nonce;
    }

    function _register(address registry, string calldata label, string memory name, string calldata sshKey, uint64 expiry)
        internal
    {
        address resolver = _ownResolver(name, sshKey);
        IPermissionedRegistry(registry).register(label, msg.sender, address(0), resolver, 0, expiry);
        emit Claimed(_lastIssuer, _lastNonce, msg.sender, name, resolver);
    }

    /// Deploys the claimer's own resolver (they hold every role; this contract only while
    /// seeding addr + meshy.ssh) and returns it.
    function _ownResolver(string memory name, string calldata sshKey) internal returns (address resolver) {
        address me = address(this);
        RoleGrant[] memory g = new RoleGrant[](2);
        g[0] = RoleGrant(msg.sender, ENSv2.ALL_ROLES);
        g[1] = RoleGrant(me, ENSv2.ALL_ROLES);
        bytes[] memory calls = new bytes[](0);
        resolver = verifiableFactory.deployProxy(
            resolverImpl,
            uint256(keccak256(abi.encode(me, msg.sender, ++_salt))),
            abi.encodeCall(IPermissionedResolverInit.initialize, (g, calls))
        );
        bytes memory dns = ENSv2.dnsEncode(name);
        IPermissionedResolver(resolver).setAddress(dns, 60, abi.encodePacked(msg.sender));
        if (bytes(sshKey).length > 0) IPermissionedResolver(resolver).setText(dns, "meshy.ssh", sshKey);
        IPermissionedResolver(resolver).revokeRootRoles(ENSv2.ALL_ROLES, me);
    }

    /// Issuer cancels an outstanding invite.
    function cancel(uint256 nonce) external {
        used[msg.sender][nonce] = true;
        emit Cancelled(msg.sender, nonce);
    }

    function _recover(bytes32 digest, bytes calldata sig) internal pure returns (address) {
        if (sig.length != 65) return address(0);
        bytes32 r;
        bytes32 s;
        uint8 v;
        assembly {
            r := calldataload(sig.offset)
            s := calldataload(add(sig.offset, 32))
            v := byte(0, calldataload(add(sig.offset, 64)))
        }
        if (v < 27) v += 27;
        if (uint256(s) > 0x7FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF5D576E7357A4501DDFE92F46681B20A0) return address(0);
        return ecrecover(digest, v, r, s);
    }
}
