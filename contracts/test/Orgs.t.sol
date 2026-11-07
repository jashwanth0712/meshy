// SPDX-License-Identifier: AGPL-3.0-only
pragma solidity ^0.8.24;

import "forge-std/Test.sol";
import "../src/ENSv2.sol";
import "../src/MeshyOrgFactory.sol";
import "../src/MeshyInvites.sol";

interface IRegistryState {
    function getExpiry(uint256 anyId) external view returns (uint64);
    function hasRootRoles(uint256 roleBitmap, address account) external view returns (bool);
}

/// @dev Fork test against the real ENSv2 contracts on Sepolia.
///      SEPOLIA_RPC=https://... forge test --match-contract OrgsTest -vv
contract OrgsTest is Test {
    address constant VF = 0xDa70306C98E97eCe36F997a21368e53298572991;
    address constant REG_IMPL = 0x9BD8a88719068D09ecee662f36C0E3856708366a;
    address constant RES_IMPL = 0x115eb53F0c60696633855F90b138178Fb40b2b2C;

    MeshyInvites inv;
    MeshyOrgFactory fac;
    address parent; // stand-in for meshy.eth's registry
    address alice = vm.addr(0x5eed0a11ce5eed0a11ce); // org owner
    uint256 bobKey = 0x5eed0b0b5eed0b0b;
    address bob = vm.addr(0x5eed0b0b5eed0b0b); // invitee
    uint256 aliceKey = 0x5eed0a11ce5eed0a11ce;

    function setUp() public {
        string memory rpc = vm.envOr("SEPOLIA_RPC", string(""));
        if (bytes(rpc).length == 0) {
            vm.skip(true);
            return;
        }
        vm.createSelectFork(rpc);
        inv = new MeshyInvites(IVerifiableFactory(VF), RES_IMPL);
        // Parent registry owned by this test; the factory gets REGISTRAR on it (as meshy.eth will).
        RoleGrant[] memory g = new RoleGrant[](1);
        g[0] = RoleGrant(address(this), ENSv2.ALL_ROLES);
        parent = IVerifiableFactory(VF).deployProxy(REG_IMPL, 1234, abi.encodeCall(IUserRegistryInit.initialize, (g)));
        fac = new MeshyOrgFactory(IVerifiableFactory(VF), REG_IMPL, RES_IMPL, IPermissionedRegistry(parent), "meshy.eth", address(inv));
        IPermissionedRegistry(parent).grantRootRoles(ENSv2.ROLE_REGISTRAR, address(fac));
    }

    function _create(string memory label) internal returns (MeshyOrgFactory.Org memory o) {
        vm.prank(alice);
        o = fac.createOrg(label, "orgKeyB64", "0x6Aafef03Fbbbeb56ff4422f042b25f754F6e0EC4");
    }

    function testCreateOrgOwnedByCaller() public {
        assertTrue(fac.isAvailable("acme"));
        MeshyOrgFactory.Org memory o = _create("acme");
        assertFalse(fac.isAvailable("acme"));
        assertGt(IRegistryState(parent).getExpiry(ENSv2.labelId("acme")), block.timestamp);
        assertGt(IRegistryState(o.orgRegistry).getExpiry(ENSv2.labelId("people")), block.timestamp);
        assertGt(IRegistryState(o.orgRegistry).getExpiry(ENSv2.labelId("agents")), block.timestamp);
        // owner has roles; factory renounced; invites may only register on people/agents
        assertTrue(IRegistryState(o.orgRegistry).hasRootRoles(ENSv2.ROLE_REGISTRAR, alice));
        assertFalse(IRegistryState(o.orgRegistry).hasRootRoles(ENSv2.ROLE_REGISTRAR, address(fac)));
        assertTrue(IRegistryState(o.peopleRegistry).hasRootRoles(ENSv2.ROLE_REGISTRAR, address(inv)));
        assertFalse(IRegistryState(o.peopleRegistry).hasRootRoles(ENSv2.ROLE_UNREGISTER, address(inv)));
        assertEq(fac.orgsOf(alice)[0], "acme.meshy.eth");
    }

    function testCreateOrgRejectsTakenAndInvalid() public {
        _create("acme");
        vm.prank(bob);
        vm.expectRevert(bytes("MOF: name taken or invalid"));
        fac.createOrg("acme", "k", "");
        vm.expectRevert(bytes("MOF: name taken or invalid"));
        fac.createOrg("Bad_Name", "k", "");
    }

    function _invite(MeshyOrgFactory.Org memory o, string memory label, address invitee, uint256 nonce)
        internal
        view
        returns (MeshyInvites.Invite memory i, bytes memory sig)
    {
        i = MeshyInvites.Invite({
            registry: o.peopleRegistry,
            parentName: "people.acme.meshy.eth",
            otherRegistry: o.agentsRegistry,
            label: label,
            invitee: invitee,
            expiresAt: uint64(block.timestamp + 1 days),
            nameExpiry: uint64(block.timestamp + 365 days),
            nonce: nonce
        });
        (uint8 v, bytes32 r, bytes32 s) = vm.sign(aliceKey, inv.hashInvite(i));
        sig = abi.encodePacked(r, s, v);
    }

    function testClaimRegistersMember() public {
        MeshyOrgFactory.Org memory o = _create("acme");
        (MeshyInvites.Invite memory i, bytes memory sig) = _invite(o, "", address(0), 1);
        assertEq(inv.check(i, sig, bob, "bob"), "");
        vm.prank(bob);
        string memory name = inv.claim(i, sig, "bob", "ssh-ed25519 AAAA test");
        assertEq(name, "bob.people.acme.meshy.eth");
        assertGt(IRegistryState(o.peopleRegistry).getExpiry(ENSv2.labelId("bob")), block.timestamp);
        // single use
        assertEq(inv.check(i, sig, bob, "bob2"), "invite used or cancelled");
    }

    function testClaimChecks() public {
        MeshyOrgFactory.Org memory o = _create("acme");
        (MeshyInvites.Invite memory i, bytes memory sig) = _invite(o, "carol", bob, 2);
        assertEq(inv.check(i, sig, address(0xdead), "carol"), "invite is for another wallet");
        assertEq(inv.check(i, sig, bob, "other"), "label mismatch");
        vm.warp(block.timestamp + 2 days);
        assertEq(inv.check(i, sig, bob, "carol"), "invite expired");
        vm.prank(bob);
        vm.expectRevert(bytes("MI: invite expired"));
        inv.claim(i, sig, "carol", "");
    }

    function testCancelAndNonAdminIssuer() public {
        MeshyOrgFactory.Org memory o = _create("acme");
        (MeshyInvites.Invite memory i, bytes memory sig) = _invite(o, "", address(0), 3);
        vm.prank(alice);
        inv.cancel(3);
        assertEq(inv.check(i, sig, bob, "bob"), "invite used or cancelled");
        // an invite signed by someone without REGISTRAR is refused
        (uint8 v, bytes32 r, bytes32 s) = vm.sign(bobKey, inv.hashInvite(i));
        assertEq(inv.check(i, abi.encodePacked(r, s, v), bob, "bob"), "issuer is not an admin");
    }

    function testLabelUniqueAcrossPeopleAndAgents() public {
        MeshyOrgFactory.Org memory o = _create("acme");
        (MeshyInvites.Invite memory i, bytes memory sig) = _invite(o, "", address(0), 4);
        vm.prank(bob);
        inv.claim(i, sig, "zed", "");
        // an agents invite for the same label must fail
        MeshyInvites.Invite memory a = MeshyInvites.Invite({
            registry: o.agentsRegistry,
            parentName: "agents.acme.meshy.eth",
            otherRegistry: o.peopleRegistry,
            label: "",
            invitee: address(0),
            expiresAt: uint64(block.timestamp + 1 days),
            nameExpiry: uint64(block.timestamp + 365 days),
            nonce: 5
        });
        (uint8 v, bytes32 r, bytes32 s) = vm.sign(aliceKey, inv.hashInvite(a));
        assertEq(inv.check(a, abi.encodePacked(r, s, v), bob, "zed"), "name taken");
    }
}
