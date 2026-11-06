// SPDX-License-Identifier: AGPL-3.0-only
pragma solidity ^0.8.24;

import "forge-std/Test.sol";
import "../src/ProviderRegistry.sol";
import "./Mocks.sol";

contract ProviderRegistryTest is Test {
    ProviderRegistry pr;
    MockUniversalHelper helper;
    MockRegistry mockReg;

    uint256 constant UNBOND_DELAY = 1 days;

    // Test signer key
    uint256 signerKey = 0xdeadbeef1234567890abcdef1234567890abcdef1234567890abcdef12345678;
    address signer;

    address owner = address(0x1001);
    address reporter = address(0x2002);

    function setUp() public {
        signer = vm.addr(signerKey);
        helper = new MockUniversalHelper();
        mockReg = new MockRegistry();
        pr = new ProviderRegistry(address(helper), UNBOND_DELAY);
    }

    // -------------------------------------------------------------------------
    // Registration
    // -------------------------------------------------------------------------

    function test_register() public {
        vm.deal(owner, 1 ether);
        vm.prank(owner);
        uint256 id = pr.register{value: 0.01 ether}("https://p.example.com", signer, 2_000_000, "ipfs://meta");

        assertEq(id, 1);
        assertEq(pr.providerCount(), 1);
        assertTrue(pr.isActive(id));

        ProviderRegistry.Provider memory p = pr.getProvider(id);
        assertEq(p.owner, owner);
        assertEq(p.signer, signer);
        assertEq(p.stake, 0.01 ether);
        assertEq(p.pricePerSeatPerMonth, 2_000_000);
    }

    function test_register_revert_insufficient_stake() public {
        vm.deal(owner, 1 ether);
        vm.prank(owner);
        vm.expectRevert("PR: insufficient stake");
        pr.register{value: 0.001 ether}("https://p.example.com", signer, 2_000_000, "");
    }

    function test_register_revert_zero_signer() public {
        vm.deal(owner, 1 ether);
        vm.prank(owner);
        vm.expectRevert("PR: zero signer");
        pr.register{value: 0.01 ether}("https://p.example.com", address(0), 2_000_000, "");
    }

    // -------------------------------------------------------------------------
    // Staking
    // -------------------------------------------------------------------------

    function test_addStake() public {
        vm.deal(owner, 1 ether);
        vm.startPrank(owner);
        uint256 id = pr.register{value: 0.01 ether}("ep", signer, 0, "");
        pr.addStake{value: 0.05 ether}(id);
        vm.stopPrank();

        ProviderRegistry.Provider memory p = pr.getProvider(id);
        assertEq(p.stake, 0.06 ether);
    }

    function test_addStake_revert_not_owner() public {
        vm.deal(owner, 1 ether);
        vm.prank(owner);
        uint256 id = pr.register{value: 0.01 ether}("ep", signer, 0, "");

        vm.deal(reporter, 1 ether);
        vm.prank(reporter);
        vm.expectRevert("PR: not owner");
        pr.addStake{value: 0.01 ether}(id);
    }

    // -------------------------------------------------------------------------
    // Unbond / Withdraw
    // -------------------------------------------------------------------------

    function test_unbond_withdraw() public {
        vm.deal(owner, 1 ether);
        vm.startPrank(owner);
        uint256 id = pr.register{value: 0.01 ether}("ep", signer, 0, "");
        pr.requestUnbond(id);
        vm.stopPrank();

        ProviderRegistry.Provider memory p = pr.getProvider(id);
        assertEq(uint8(p.status), 1); // Unbonding

        // Should revert before delay
        vm.prank(owner);
        vm.expectRevert("PR: delay not elapsed");
        pr.withdraw(id);

        // Advance time
        vm.warp(block.timestamp + UNBOND_DELAY + 1);

        uint256 balBefore = owner.balance;
        vm.prank(owner);
        pr.withdraw(id);

        assertEq(owner.balance - balBefore, 0.01 ether);
        p = pr.getProvider(id);
        assertEq(uint8(p.status), 3); // Withdrawn
        assertEq(p.stake, 0);
    }

    function test_unbond_revert_not_active() public {
        vm.deal(owner, 1 ether);
        vm.startPrank(owner);
        uint256 id = pr.register{value: 0.01 ether}("ep", signer, 0, "");
        pr.requestUnbond(id);
        vm.expectRevert("PR: not active");
        pr.requestUnbond(id);
        vm.stopPrank();
    }

    // -------------------------------------------------------------------------
    // Slashing helpers
    // -------------------------------------------------------------------------

    function _buildAttestation(uint256 providerId, bytes memory dnsName, uint256 resource, uint64 issuedAt, uint64 validUntil)
        internal pure returns (ProviderRegistry.Attestation memory)
    {
        return ProviderRegistry.Attestation({
            providerId: providerId,
            dnsName: dnsName,
            resource: resource,
            issuedAt: issuedAt,
            validUntil: validUntil,
            peerKey: bytes32(0)
        });
    }

    function _signAttestation(ProviderRegistry.Attestation memory a) internal view returns (bytes memory) {
        bytes32 domainSep = pr.DOMAIN_SEPARATOR();
        bytes32 typehash = pr.ATTESTATION_TYPEHASH();
        bytes32 structHash = keccak256(abi.encode(
            typehash,
            a.providerId,
            keccak256(a.dnsName),
            a.resource,
            a.issuedAt,
            a.validUntil,
            a.peerKey
        ));
        bytes32 digest = keccak256(abi.encodePacked("\x19\x01", domainSep, structHash));
        (uint8 v, bytes32 r, bytes32 s) = vm.sign(signerKey, digest);
        return abi.encodePacked(r, s, v);
    }

    // DNS-encode "db1.prod.meshydemo.eth" minimally: just the first label matters for _firstLabel
    // DNS wire: <len>db1<len>prod<len>meshydemo<len>eth<0x00>
    function _dnsEncode(bytes memory firstLabel, bytes memory rest) internal pure returns (bytes memory) {
        return abi.encodePacked(uint8(firstLabel.length), firstLabel, rest, uint8(0));
    }

    function _setupProvider() internal returns (uint256 id) {
        vm.deal(owner, 1 ether);
        vm.prank(owner);
        id = pr.register{value: 0.1 ether}("ep", signer, 0, "");
    }

    // -------------------------------------------------------------------------
    // Slash: ancestor dead
    // -------------------------------------------------------------------------

    function test_slash_ancestor_dead() public {
        uint256 id = _setupProvider();
        helper.setDead();

        bytes memory dnsName = _dnsEncode("db1", abi.encodePacked(uint8(4), "prod"));
        uint64 issuedAt = uint64(block.timestamp);
        ProviderRegistry.Attestation memory a = _buildAttestation(id, dnsName, 1, issuedAt, issuedAt + 3600);
        bytes memory sig = _signAttestation(a);

        vm.deal(reporter, 0);
        vm.prank(reporter);
        pr.slash(a, sig);

        assertEq(uint8(pr.getProvider(id).status), 2); // Slashed
        assertEq(reporter.balance, 0.05 ether);
        assertEq(pr.BURN_ADDRESS().balance, 0.05 ether);
    }

    // -------------------------------------------------------------------------
    // Slash: expired at issuance
    // -------------------------------------------------------------------------

    function test_slash_expired_at_issuance() public {
        uint256 id = _setupProvider();
        helper.setRegistry(address(mockReg));

        bytes memory label = "db1";
        bytes memory dnsName = _dnsEncode(label, abi.encodePacked(uint8(4), "prod"));
        uint256 labelHash = uint256(keccak256(label));

        uint64 issuedAt = uint64(block.timestamp);
        // Expiry at or before issuedAt => fraud
        mockReg.setExpiry(labelHash, issuedAt - 1);
        mockReg.setResource(labelHash, 42);

        ProviderRegistry.Attestation memory a = _buildAttestation(id, dnsName, 42, issuedAt, issuedAt + 3600);
        bytes memory sig = _signAttestation(a);

        vm.prank(reporter);
        pr.slash(a, sig);

        assertEq(uint8(pr.getProvider(id).status), 2); // Slashed
    }

    // -------------------------------------------------------------------------
    // Slash: resource mismatch (re-registered)
    // -------------------------------------------------------------------------

    function test_slash_resource_mismatch() public {
        uint256 id = _setupProvider();
        helper.setRegistry(address(mockReg));

        bytes memory label = "db1";
        bytes memory dnsName = _dnsEncode(label, abi.encodePacked(uint8(4), "prod"));
        uint256 labelHash = uint256(keccak256(label));

        uint64 issuedAt = uint64(block.timestamp);
        // Current resource is 99, but attestation claims resource=42
        mockReg.setExpiry(labelHash, issuedAt + 3600); // not expired
        mockReg.setResource(labelHash, 99);

        ProviderRegistry.Attestation memory a = _buildAttestation(id, dnsName, 42, issuedAt, issuedAt + 3600);
        bytes memory sig = _signAttestation(a);

        vm.prank(reporter);
        pr.slash(a, sig);

        assertEq(uint8(pr.getProvider(id).status), 2); // Slashed
    }

    // -------------------------------------------------------------------------
    // No slash for valid attestation
    // -------------------------------------------------------------------------

    function test_no_slash_valid_attestation() public {
        uint256 id = _setupProvider();
        helper.setRegistry(address(mockReg));

        bytes memory label = "db1";
        bytes memory dnsName = _dnsEncode(label, abi.encodePacked(uint8(4), "prod"));
        uint256 labelHash = uint256(keccak256(label));

        uint64 issuedAt = uint64(block.timestamp);
        // Valid: not expired at issuance, resource matches
        mockReg.setExpiry(labelHash, issuedAt + 3600);
        mockReg.setResource(labelHash, 42);

        ProviderRegistry.Attestation memory a = _buildAttestation(id, dnsName, 42, issuedAt, issuedAt + 3600);
        bytes memory sig = _signAttestation(a);

        vm.expectRevert("PR: no fraud detected");
        vm.prank(reporter);
        pr.slash(a, sig);
    }

    // -------------------------------------------------------------------------
    // Bad signature rejected
    // -------------------------------------------------------------------------

    function test_slash_bad_sig() public {
        uint256 id = _setupProvider();
        helper.setDead();

        bytes memory dnsName = _dnsEncode("db1", "");
        uint64 issuedAt = uint64(block.timestamp);
        ProviderRegistry.Attestation memory a = _buildAttestation(id, dnsName, 1, issuedAt, issuedAt + 3600);

        // Sign with wrong key
        uint256 wrongKey = 0xaaaa;
        bytes32 domainSep = pr.DOMAIN_SEPARATOR();
        bytes32 typehash = pr.ATTESTATION_TYPEHASH();
        bytes32 structHash = keccak256(abi.encode(typehash, a.providerId, keccak256(a.dnsName), a.resource, a.issuedAt, a.validUntil, a.peerKey));
        bytes32 digest = keccak256(abi.encodePacked("\x19\x01", domainSep, structHash));
        (uint8 v, bytes32 r, bytes32 s) = vm.sign(wrongKey, digest);
        bytes memory sig = abi.encodePacked(r, s, v);

        vm.expectRevert("PR: bad signature");
        vm.prank(reporter);
        pr.slash(a, sig);
    }

    // -------------------------------------------------------------------------
    // Double slash rejected
    // -------------------------------------------------------------------------

    function test_slash_double_slash() public {
        uint256 id = _setupProvider();
        helper.setDead();

        bytes memory dnsName = _dnsEncode("db1", "");
        uint64 issuedAt = uint64(block.timestamp);
        ProviderRegistry.Attestation memory a = _buildAttestation(id, dnsName, 1, issuedAt, issuedAt + 3600);
        bytes memory sig = _signAttestation(a);

        vm.prank(reporter);
        pr.slash(a, sig);

        vm.prank(reporter);
        vm.expectRevert("PR: already slashed");
        pr.slash(a, sig);
    }

    // -------------------------------------------------------------------------
    // Cannot slash Withdrawn or already Slashed
    // -------------------------------------------------------------------------

    function test_slash_revert_not_slashable() public {
        vm.deal(owner, 1 ether);
        vm.startPrank(owner);
        uint256 id = pr.register{value: 0.01 ether}("ep", signer, 0, "");
        pr.requestUnbond(id);
        vm.warp(block.timestamp + UNBOND_DELAY + 1);
        pr.withdraw(id);
        vm.stopPrank();

        helper.setDead();
        bytes memory dnsName = _dnsEncode("db1", "");
        uint64 issuedAt = uint64(block.timestamp);
        ProviderRegistry.Attestation memory a = _buildAttestation(id, dnsName, 1, issuedAt, issuedAt + 3600);
        bytes memory sig = _signAttestation(a);

        vm.expectRevert("PR: not slashable");
        vm.prank(reporter);
        pr.slash(a, sig);
    }

    // -------------------------------------------------------------------------
    // Update / access control
    // -------------------------------------------------------------------------

    function test_update() public {
        vm.deal(owner, 1 ether);
        vm.prank(owner);
        uint256 id = pr.register{value: 0.01 ether}("old", signer, 0, "");

        vm.prank(owner);
        pr.update(id, "new", signer, 5_000_000, "ipfs://new");

        ProviderRegistry.Provider memory p = pr.getProvider(id);
        assertEq(p.endpoint, "new");
        assertEq(p.pricePerSeatPerMonth, 5_000_000);
    }

    function test_update_revert_not_owner() public {
        vm.deal(owner, 1 ether);
        vm.prank(owner);
        uint256 id = pr.register{value: 0.01 ether}("old", signer, 0, "");

        vm.prank(reporter);
        vm.expectRevert("PR: not owner");
        pr.update(id, "new", signer, 0, "");
    }
}
