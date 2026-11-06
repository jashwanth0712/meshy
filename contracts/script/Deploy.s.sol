// SPDX-License-Identifier: AGPL-3.0-only
pragma solidity ^0.8.24;

import "forge-std/Script.sol";
import "../src/ProviderRegistry.sol";
import "../src/MeshySubscription.sol";

interface IMockUSDC {
    function mint(address to, uint256 amount) external;
    function approve(address spender, uint256 amount) external returns (bool);
    function balanceOf(address) external view returns (uint256);
}

contract Deploy is Script {
    // ENSv2 Sepolia addresses
    address constant UNIVERSAL_HELPER = 0xd453e5Bdb62CC3beA84341B1e306319C8Ffd7DFe;
    address constant MOCK_USDC        = 0x240b0316Df57887DBBE58b586508b19e633a14aa;

    // Demo provider params
    uint256 constant UNBOND_DELAY         = 1 days;
    uint256 constant PRICE_PER_SEAT_MONTH = 2_000_000; // 2 USDC
    uint256 constant STAKE                = 0.01 ether;
    uint256 constant DEMO_SEATS           = 5;
    uint256 constant DEMO_MONTHS          = 1;

    function run() external {
        uint256 deployerKey = vm.envUint("PRIVATE_KEY");
        address deployer    = vm.addr(deployerKey);

        // Attestor signer address (passed via ATTESTOR_ADDRESS env)
        address attestorAddr = vm.envAddress("ATTESTOR_ADDRESS");

        // org node for meshydemo.eth
        bytes32 orgNode = keccak256(abi.encodePacked(
            keccak256(abi.encodePacked(bytes32(0), keccak256("eth"))),
            keccak256("meshydemo")
        ));

        vm.startBroadcast(deployerKey);

        // --- Deploy contracts ---
        ProviderRegistry pr = new ProviderRegistry(UNIVERSAL_HELPER, UNBOND_DELAY);
        MeshySubscription ms = new MeshySubscription(address(MOCK_USDC), address(pr), deployer);

        console2.log("ProviderRegistry:", address(pr));
        console2.log("MeshySubscription:", address(ms));

        // --- Register demo provider ---
        uint256 providerId = pr.register{value: STAKE}(
            "https://provider.meshy.local",
            attestorAddr,
            PRICE_PER_SEAT_MONTH,
            "ipfs://QmMeshyProviderDemo"
        );
        console2.log("Demo providerId:", providerId);

        // --- Mint MockUSDC and subscribe ---
        uint256 usdcNeeded = PRICE_PER_SEAT_MONTH * DEMO_SEATS * DEMO_MONTHS;
        IMockUSDC usdc = IMockUSDC(MOCK_USDC);
        usdc.mint(deployer, usdcNeeded);
        usdc.approve(address(ms), usdcNeeded);
        ms.subscribe(orgNode, providerId, uint32(DEMO_SEATS), uint32(DEMO_MONTHS));

        console2.log("Subscribed orgNode (meshydemo.eth):", vm.toString(orgNode));
        console2.log("Seats:", DEMO_SEATS);
        console2.log("Months:", DEMO_MONTHS);

        vm.stopBroadcast();
    }
}
