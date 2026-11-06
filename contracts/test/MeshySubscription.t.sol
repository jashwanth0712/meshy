// SPDX-License-Identifier: AGPL-3.0-only
pragma solidity ^0.8.24;

import "forge-std/Test.sol";
import "../src/ProviderRegistry.sol";
import "../src/MeshySubscription.sol";
import "./Mocks.sol";

contract MeshySubscriptionTest is Test {
    ProviderRegistry pr;
    MeshySubscription ms;
    MockUSDC usdc;
    MockUniversalHelper helper;

    uint256 constant UNBOND_DELAY = 1 days;
    uint256 constant MONTH = 30 days;
    uint256 constant PRICE = 2_000_000; // 2 USDC per seat per month

    address providerOwner = address(0x1001);
    address payer = address(0x2002);
    address treasury = address(0x3003);

    uint256 providerKey = 0xbeefdead;
    address providerSigner;
    uint256 providerId;

    bytes32 orgNode = keccak256("meshydemo.eth");

    function setUp() public {
        providerSigner = vm.addr(providerKey);
        helper = new MockUniversalHelper();
        usdc = new MockUSDC();
        pr = new ProviderRegistry(address(helper), UNBOND_DELAY);
        ms = new MeshySubscription(address(usdc), address(pr), treasury);

        // Register a provider
        vm.deal(providerOwner, 1 ether);
        vm.prank(providerOwner);
        providerId = pr.register{value: 0.01 ether}("https://p.example.com", providerSigner, PRICE, "");
    }

    function _mintAndApprove(address who, uint256 amount) internal {
        usdc.mint(who, amount);
        vm.prank(who);
        usdc.approve(address(ms), amount);
    }

    // -------------------------------------------------------------------------
    // Subscribe
    // -------------------------------------------------------------------------

    function test_subscribe_basic() public {
        uint256 total = PRICE * 5 * 1; // 5 seats * 1 month
        _mintAndApprove(payer, total);

        vm.prank(payer);
        ms.subscribe(orgNode, providerId, 5, 1);

        (uint32 seats, uint64 paidUntil, uint256 pid) = ms.seatsOf(orgNode);
        assertEq(seats, 5);
        assertEq(pid, providerId);
        assertGt(paidUntil, block.timestamp);
        assertTrue(ms.isActive(orgNode));
    }

    function test_subscribe_treasury_split() public {
        uint256 total = PRICE * 10 * 2; // 10 seats * 2 months = 40 USDC
        _mintAndApprove(payer, total);

        vm.prank(payer);
        ms.subscribe(orgNode, providerId, 10, 2);

        // Treasury should have 5% = 2 USDC
        uint256 expectedTreasury = (total * 500) / 10_000;
        assertEq(usdc.balanceOf(treasury), expectedTreasury);

        // Contract holds provider escrow = 95%
        uint256 expectedEscrow = total - expectedTreasury;
        assertEq(ms.claimable(providerId), expectedEscrow);
    }

    function test_subscribe_revert_already_subscribed() public {
        uint256 total = PRICE * 1 * 1;
        _mintAndApprove(payer, total * 2);

        vm.startPrank(payer);
        ms.subscribe(orgNode, providerId, 1, 1);
        vm.expectRevert("MS: already subscribed");
        ms.subscribe(orgNode, providerId, 1, 1);
        vm.stopPrank();
    }

    // -------------------------------------------------------------------------
    // AddSeats proration
    // -------------------------------------------------------------------------

    function test_addSeats_proration() public {
        // Subscribe for 2 months
        uint256 total = PRICE * 2 * 2; // 2 seats, 2 months
        _mintAndApprove(payer, total + PRICE * 3 * 2); // extra for addSeats

        vm.prank(payer);
        ms.subscribe(orgNode, providerId, 2, 2);

        // Advance 1.5 months (45 days)
        vm.warp(block.timestamp + 45 days);

        // Add 3 seats: remaining = ~15 days, ceil to 1 month
        uint256 addCost = PRICE * 3 * 1; // 3 seats * 1 month (ceil of 15/30)
        _mintAndApprove(payer, addCost);

        vm.prank(payer);
        ms.addSeats(orgNode, 3);

        (uint32 seats,,) = ms.seatsOf(orgNode);
        assertEq(seats, 5);
    }

    function test_addSeats_revert_not_payer() public {
        uint256 total = PRICE * 1 * 1;
        _mintAndApprove(payer, total);
        vm.prank(payer);
        ms.subscribe(orgNode, providerId, 1, 1);

        vm.prank(address(0x9999));
        vm.expectRevert("MS: not payer");
        ms.addSeats(orgNode, 1);
    }

    function test_addSeats_revert_expired() public {
        uint256 total = PRICE * 1 * 1;
        _mintAndApprove(payer, total);
        vm.prank(payer);
        ms.subscribe(orgNode, providerId, 1, 1);

        vm.warp(block.timestamp + MONTH + 1);

        _mintAndApprove(payer, PRICE);
        vm.prank(payer);
        vm.expectRevert("MS: subscription expired");
        ms.addSeats(orgNode, 1);
    }

    // -------------------------------------------------------------------------
    // Renew
    // -------------------------------------------------------------------------

    function test_renew() public {
        uint256 total = PRICE * 2 * 1; // 2 seats, 1 month
        _mintAndApprove(payer, total);
        vm.prank(payer);
        ms.subscribe(orgNode, providerId, 2, 1);

        (,uint64 paidUntilBefore,) = ms.seatsOf(orgNode);

        uint256 renewCost = PRICE * 2 * 2;
        _mintAndApprove(payer, renewCost);
        vm.prank(payer);
        ms.renew(orgNode, 2);

        (,uint64 paidUntilAfter,) = ms.seatsOf(orgNode);
        assertEq(paidUntilAfter, paidUntilBefore + uint64(2 * MONTH));
    }

    // -------------------------------------------------------------------------
    // Cancel + refund
    // -------------------------------------------------------------------------

    function test_cancel_full_refund() public {
        // Subscribe, then cancel immediately (0 elapsed months)
        uint256 total = PRICE * 2 * 3; // 2 seats * 3 months = 12 USDC
        _mintAndApprove(payer, total);
        vm.prank(payer);
        ms.subscribe(orgNode, providerId, 2, 3);

        uint256 payerBalBefore = usdc.balanceOf(payer);

        vm.prank(payer);
        ms.cancel(orgNode);

        // Refund: 95% of total (treasury already paid out) for 3 months - 0 elapsed = 3 months
        uint256 providerShare = total - (total * 500) / 10_000;
        // 0 elapsed months, so full providerShare returned
        assertEq(usdc.balanceOf(payer) - payerBalBefore, providerShare);
        assertFalse(ms.isActive(orgNode));
    }

    function test_cancel_partial_refund() public {
        // Subscribe for 3 months, cancel after 1 month
        uint256 total = PRICE * 1 * 3; // 1 seat * 3 months = 6 USDC
        _mintAndApprove(payer, total);
        vm.prank(payer);
        ms.subscribe(orgNode, providerId, 1, 3);

        vm.warp(block.timestamp + MONTH + 1); // 1 month elapsed

        uint256 payerBalBefore = usdc.balanceOf(payer);
        vm.prank(payer);
        ms.cancel(orgNode);

        uint256 providerShare = total - (total * 500) / 10_000;
        uint256 perMonthProviderShare = providerShare / 3;
        uint256 expectedRefund = providerShare - perMonthProviderShare * 1; // 2 remaining months

        assertEq(usdc.balanceOf(payer) - payerBalBefore, expectedRefund);
    }

    function test_cancel_revert_not_payer() public {
        uint256 total = PRICE * 1 * 1;
        _mintAndApprove(payer, total);
        vm.prank(payer);
        ms.subscribe(orgNode, providerId, 1, 1);

        vm.prank(address(0x9999));
        vm.expectRevert("MS: not payer");
        ms.cancel(orgNode);
    }

    // -------------------------------------------------------------------------
    // Claim (escrow release)
    // -------------------------------------------------------------------------

    function test_claim() public {
        uint256 total = PRICE * 5 * 2; // 5 seats, 2 months
        _mintAndApprove(payer, total);
        vm.prank(payer);
        ms.subscribe(orgNode, providerId, 5, 2);

        uint256 providerShare = total - (total * 500) / 10_000;
        assertEq(ms.claimable(providerId), providerShare);

        uint256 balBefore = usdc.balanceOf(providerOwner);
        vm.prank(providerOwner);
        ms.claim(providerId);

        assertEq(usdc.balanceOf(providerOwner) - balBefore, providerShare);
        assertEq(ms.claimable(providerId), 0);
    }

    function test_claim_revert_not_provider_owner() public {
        uint256 total = PRICE;
        _mintAndApprove(payer, total);
        vm.prank(payer);
        ms.subscribe(orgNode, providerId, 1, 1);

        vm.prank(payer); // wrong person
        vm.expectRevert("MS: not provider owner");
        ms.claim(providerId);
    }

    function test_claim_revert_nothing_to_claim() public {
        vm.prank(providerOwner);
        vm.expectRevert("MS: nothing to claim");
        ms.claim(providerId);
    }

    // -------------------------------------------------------------------------
    // isActive
    // -------------------------------------------------------------------------

    function test_isActive_expires() public {
        uint256 total = PRICE;
        _mintAndApprove(payer, total);
        vm.prank(payer);
        ms.subscribe(orgNode, providerId, 1, 1);

        assertTrue(ms.isActive(orgNode));

        vm.warp(block.timestamp + MONTH + 1);
        assertFalse(ms.isActive(orgNode));
    }

    // -------------------------------------------------------------------------
    // Access control: subscribe with inactive provider
    // -------------------------------------------------------------------------

    function test_subscribe_revert_inactive_provider() public {
        // Unbond provider
        vm.prank(providerOwner);
        pr.requestUnbond(providerId);

        uint256 total = PRICE;
        _mintAndApprove(payer, total);
        vm.prank(payer);
        vm.expectRevert("MS: provider not active");
        ms.subscribe(orgNode, providerId, 1, 1);
    }
}
