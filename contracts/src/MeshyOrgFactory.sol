// SPDX-License-Identifier: AGPL-3.0
pragma solidity ^0.8.24;

import {ENSv2, RoleGrant, IVerifiableFactory, IUserRegistryInit, IPermissionedResolverInit, IPermissionedRegistry, IPermissionedResolver} from "./ENSv2.sol";

/// @title MeshyOrgFactory
/// @notice One transaction creates a Meshy org as a free ENSv2 subname:
///   <label>.<parent>            org (owner = caller), its own registry + resolver
///   people.<label>.<parent>     members registry (MeshyInvites may register)
///   agents.<label>.<parent>     agents registry  (MeshyInvites may register)
/// plus `meshy.orgkey` and `meshy.attestor` text records.
/// Every registry and resolver is owned by the caller. The factory holds roles only
/// during the call and renounces them before returning; afterwards Meshy cannot
/// change the org's access rules (it can only decline to renew the parent name).
contract MeshyOrgFactory {
    IVerifiableFactory public immutable verifiableFactory;
    address public immutable registryImpl;
    address public immutable resolverImpl;
    /// Registry of the parent name (e.g. meshy.eth); this factory holds REGISTRAR on it.
    IPermissionedRegistry public immutable parentRegistry;
    string public parentName; // "meshy.eth"
    address public immutable invites;
    uint64 public constant ORG_TERM = 365 days;

    uint256 private _salt;

    event OrgCreated(
        string indexed labelIndexed,
        string label,
        string name,
        address indexed owner,
        address orgRegistry,
        address resolver,
        address peopleRegistry,
        address agentsRegistry
    );

    struct Org {
        address owner;
        address orgRegistry;
        address resolver;
        address peopleRegistry;
        address agentsRegistry;
        uint64 createdAt;
    }

    mapping(bytes32 => Org) public orgs; // keccak(label) -> org
    mapping(address => string[]) private _orgsOf;

    constructor(
        IVerifiableFactory _verifiableFactory,
        address _registryImpl,
        address _resolverImpl,
        IPermissionedRegistry _parentRegistry,
        string memory _parentName,
        address _invites
    ) {
        verifiableFactory = _verifiableFactory;
        registryImpl = _registryImpl;
        resolverImpl = _resolverImpl;
        parentRegistry = _parentRegistry;
        parentName = _parentName;
        invites = _invites;
    }

    function isAvailable(string calldata label) public view returns (bool) {
        if (!ENSv2.validLabel(label)) return false;
        return parentRegistry.getExpiry(ENSv2.labelId(label)) <= block.timestamp;
    }

    function orgsOf(address owner) external view returns (string[] memory) {
        return _orgsOf[owner];
    }

    /// @param label     org label, e.g. "acme" -> acme.meshy.eth
    /// @param orgKey    base64 X25519 public key for encrypted policies/audit
    /// @param attestor  provider signer address(es), comma-separated (may be empty)
    function createOrg(string calldata label, string calldata orgKey, string calldata attestor)
        external
        returns (Org memory o)
    {
        require(isAvailable(label), "MOF: name taken or invalid");
        address me = address(this);
        address owner = msg.sender;

        // Registries/resolver: owner gets every role; the factory temporarily too.
        o.resolver = _deployResolver(owner, me);
        o.orgRegistry = _deployRegistry(owner, me, address(0));
        o.peopleRegistry = _deployRegistry(owner, address(0), invites);
        o.agentsRegistry = _deployRegistry(owner, address(0), invites);
        o.owner = owner;
        o.createdAt = uint64(block.timestamp);

        uint64 expiry = uint64(block.timestamp) + ORG_TERM;
        parentRegistry.register(label, owner, o.orgRegistry, o.resolver, 0, expiry);
        IPermissionedRegistry(o.orgRegistry).register("people", owner, o.peopleRegistry, o.resolver, 0, expiry);
        IPermissionedRegistry(o.orgRegistry).register("agents", owner, o.agentsRegistry, o.resolver, 0, expiry);

        string memory name = string.concat(label, ".", parentName);
        bytes memory dns = ENSv2.dnsEncode(name);
        IPermissionedResolver(o.resolver).setText(dns, "meshy.orgkey", orgKey);
        if (bytes(attestor).length > 0) IPermissionedResolver(o.resolver).setText(dns, "meshy.attestor", attestor);
        IPermissionedResolver(o.resolver).setText(dns, "meshy.created-by", "MeshyOrgFactory");

        // Renounce: from here on only the owner (and whoever they delegate to) has roles.
        IPermissionedResolver(o.resolver).revokeRootRoles(ENSv2.ALL_ROLES, me);
        IPermissionedRegistry(o.orgRegistry).revokeRootRoles(ENSv2.ALL_ROLES, me);

        orgs[keccak256(bytes(label))] = o;
        _orgsOf[owner].push(name);
        emit OrgCreated(label, label, name, owner, o.orgRegistry, o.resolver, o.peopleRegistry, o.agentsRegistry);
    }

    function _deployRegistry(address owner, address temp, address registrar) internal returns (address) {
        uint256 n = 1 + (temp != address(0) ? 1 : 0) + (registrar != address(0) ? 1 : 0);
        RoleGrant[] memory g = new RoleGrant[](n);
        uint256 i = 0;
        g[i++] = RoleGrant(owner, ENSv2.ALL_ROLES);
        if (temp != address(0)) g[i++] = RoleGrant(temp, ENSv2.ALL_ROLES);
        if (registrar != address(0)) g[i++] = RoleGrant(registrar, ENSv2.ROLE_REGISTRAR);
        return verifiableFactory.deployProxy(registryImpl, _nextSalt(), abi.encodeCall(IUserRegistryInit.initialize, (g)));
    }

    function _deployResolver(address owner, address temp) internal returns (address) {
        RoleGrant[] memory g = new RoleGrant[](2);
        g[0] = RoleGrant(owner, ENSv2.ALL_ROLES);
        g[1] = RoleGrant(temp, ENSv2.ALL_ROLES);
        bytes[] memory calls = new bytes[](0);
        return verifiableFactory.deployProxy(
            resolverImpl, _nextSalt(), abi.encodeCall(IPermissionedResolverInit.initialize, (g, calls))
        );
    }

    function _nextSalt() internal returns (uint256) {
        return uint256(keccak256(abi.encode(address(this), msg.sender, block.chainid, ++_salt)));
    }
}
