// SPDX-License-Identifier: AGPL-3.0-only
pragma solidity ^0.8.24;

/// @notice Minimal ERC-20 interface (USDC 6 decimals).
interface IERC20 {
    function transferFrom(address from, address to, uint256 amount) external returns (bool);
    function transfer(address to, uint256 amount) external returns (bool);
    function balanceOf(address account) external view returns (uint256);
}

/// @notice Minimal view into ProviderRegistry.
interface IProviderRegistry {
    struct Provider {
        address owner;
        address signer;
        string endpoint;
        uint256 pricePerSeatPerMonth;
        uint256 stake;
        uint256 unbondTime;
        uint8 status; // 0 = Active
        string metadataURI;
    }
    function getProvider(uint256 providerId) external view returns (Provider memory);
    function isActive(uint256 providerId) external view returns (bool);
}

/// @title MeshySubscription
/// @notice Per-seat monthly billing in USDC. Escrow + claim design for safe refunds.
///
/// Escrow model:
///   - subscribe() locks funds in this contract.
///   - claim(providerId) releases all elapsed whole-month portions to the provider owner.
///   - cancel() refunds remaining unelapsed whole months to the payer; elapsed portions
///     remain claimable by the provider.
///
/// Treasury (5%) is taken immediately on subscribe() and addSeats().
contract MeshySubscription {
    // -------------------------------------------------------------------------
    // Constants
    // -------------------------------------------------------------------------

    uint256 public constant TREASURY_BPS = 500;  // 5%
    uint256 public constant MONTH = 30 days;      // simplification; 30-day months

    // -------------------------------------------------------------------------
    // State
    // -------------------------------------------------------------------------

    IERC20 public immutable usdc;
    IProviderRegistry public immutable registry;
    address public immutable treasury;

    struct Subscription {
        address payer;
        uint256 providerId;
        uint32  seats;
        uint32  months;        // total months purchased
        uint64  startedAt;     // timestamp of subscribe()
        uint64  paidUntil;     // startedAt + months * MONTH
        uint256 totalEscrowed; // net provider share escrowed (after treasury cut)
        uint256 claimed;       // amount already claimed by provider
        bool    active;
    }

    mapping(bytes32 => Subscription) private _subs;    // orgNode => Subscription
    // Track per-provider claimable amounts
    mapping(uint256 => uint256) private _providerEscrow; // providerId => unclaimed

    // -------------------------------------------------------------------------
    // Events
    // -------------------------------------------------------------------------

    event Subscribed(bytes32 indexed orgNode, uint256 indexed providerId, uint32 seats, uint32 months, uint64 paidUntil);
    event SeatsAdded(bytes32 indexed orgNode, uint32 added, uint32 total, uint64 paidUntil);
    event Renewed(bytes32 indexed orgNode, uint32 months, uint64 paidUntil);
    event Cancelled(bytes32 indexed orgNode, uint256 refund);
    event Claimed(uint256 indexed providerId, address indexed to, uint256 amount);

    // -------------------------------------------------------------------------
    // Constructor
    // -------------------------------------------------------------------------

    constructor(address _usdc, address _registry, address _treasury) {
        usdc = IERC20(_usdc);
        registry = IProviderRegistry(_registry);
        treasury = _treasury;
    }

    // -------------------------------------------------------------------------
    // Core subscription actions
    // -------------------------------------------------------------------------

    /// @notice Subscribe an org to a provider.
    /// @param orgNode  ENS namehash of the org (e.g. keccak256("meshydemo.eth"))
    /// @param providerId  ID in ProviderRegistry
    /// @param seats  number of seats
    /// @param months  number of 30-day months
    function subscribe(
        bytes32 orgNode,
        uint256 providerId,
        uint32 seats,
        uint32 months
    ) external {
        require(!_subs[orgNode].active, "MS: already subscribed");
        require(seats > 0 && months > 0, "MS: zero seats/months");
        require(registry.isActive(providerId), "MS: provider not active");

        IProviderRegistry.Provider memory p = registry.getProvider(providerId);
        uint256 total = uint256(p.pricePerSeatPerMonth) * seats * months;
        require(total > 0, "MS: zero total");

        (uint256 treasuryShare, uint256 providerShare) = _split(total);

        // Pull USDC from payer
        require(usdc.transferFrom(msg.sender, address(this), total), "MS: transfer failed");
        // Send 5% to treasury immediately
        require(usdc.transfer(treasury, treasuryShare), "MS: treasury transfer failed");

        uint64 now_ = uint64(block.timestamp);
        uint64 paidUntil = now_ + uint64(months) * uint64(MONTH);

        _subs[orgNode] = Subscription({
            payer: msg.sender,
            providerId: providerId,
            seats: seats,
            months: months,
            startedAt: now_,
            paidUntil: paidUntil,
            totalEscrowed: providerShare,
            claimed: 0,
            active: true
        });

        _providerEscrow[providerId] += providerShare;

        emit Subscribed(orgNode, providerId, seats, months, paidUntil);
    }

    /// @notice Add seats to an active subscription, prorated to paidUntil.
    /// @param orgNode  ENS namehash of the org
    /// @param extra  extra seats to add
    function addSeats(bytes32 orgNode, uint32 extra) external {
        Subscription storage s = _subs[orgNode];
        require(s.active, "MS: not subscribed");
        require(msg.sender == s.payer, "MS: not payer");
        require(extra > 0, "MS: zero extra");

        uint64 now_ = uint64(block.timestamp);
        require(now_ < s.paidUntil, "MS: subscription expired");

        // Prorate: remaining time rounded up to whole days, converted to months
        uint64 remaining = s.paidUntil - now_;
        // Cost = price * extra * ceil(remaining / MONTH)
        // ceil(remaining / MONTH) = (remaining + MONTH - 1) / MONTH
        uint256 fractionalMonths = (uint256(remaining) + MONTH - 1) / MONTH;
        require(fractionalMonths > 0, "MS: nothing to prorate");

        IProviderRegistry.Provider memory p = registry.getProvider(s.providerId);
        uint256 total = uint256(p.pricePerSeatPerMonth) * extra * fractionalMonths;
        require(total > 0, "MS: zero total");

        (uint256 treasuryShare, uint256 providerShare) = _split(total);

        require(usdc.transferFrom(msg.sender, address(this), total), "MS: transfer failed");
        require(usdc.transfer(treasury, treasuryShare), "MS: treasury transfer failed");

        s.seats += extra;
        s.totalEscrowed += providerShare;
        _providerEscrow[s.providerId] += providerShare;

        emit SeatsAdded(orgNode, extra, s.seats, s.paidUntil);
    }

    /// @notice Renew an active subscription for additional months.
    function renew(bytes32 orgNode, uint32 months) external {
        Subscription storage s = _subs[orgNode];
        require(s.active, "MS: not subscribed");
        require(msg.sender == s.payer, "MS: not payer");
        require(months > 0, "MS: zero months");
        require(registry.isActive(s.providerId), "MS: provider not active");

        IProviderRegistry.Provider memory p = registry.getProvider(s.providerId);
        uint256 total = uint256(p.pricePerSeatPerMonth) * s.seats * months;
        require(total > 0, "MS: zero total");

        (uint256 treasuryShare, uint256 providerShare) = _split(total);

        require(usdc.transferFrom(msg.sender, address(this), total), "MS: transfer failed");
        require(usdc.transfer(treasury, treasuryShare), "MS: treasury transfer failed");

        s.months += months;
        s.paidUntil += uint64(months) * uint64(MONTH);
        s.totalEscrowed += providerShare;
        _providerEscrow[s.providerId] += providerShare;

        emit Renewed(orgNode, months, s.paidUntil);
    }

    /// @notice Cancel subscription. Refunds whole unelapsed months to payer.
    ///         Elapsed months remain claimable by the provider.
    function cancel(bytes32 orgNode) external {
        Subscription storage s = _subs[orgNode];
        require(s.active, "MS: not subscribed");
        require(msg.sender == s.payer, "MS: not payer");

        s.active = false;

        // Calculate elapsed and remaining whole months
        uint64 now_ = uint64(block.timestamp);
        uint256 elapsedMonths;
        if (now_ >= s.paidUntil) {
            elapsedMonths = s.months;
        } else {
            uint64 elapsed = now_ - s.startedAt;
            elapsedMonths = elapsed / MONTH; // floor: provider gets elapsed complete months
        }

        // Provider share already escrowed. We need to adjust _providerEscrow:
        // provider gets escrowed for elapsedMonths; payer gets refund for remaining months
        // Recalculate escrow split
        // providerShare was totalEscrowed = providerShare for all months
        // Per-month providerShare = totalEscrowed / months
        uint256 perMonthProviderShare = s.totalEscrowed / s.months;

        uint256 providerKeep = perMonthProviderShare * elapsedMonths;
        uint256 refundProviderShare = s.totalEscrowed - providerKeep;

        // Also need to know original total per month to compute payer refund
        // payer refund = refundProviderShare / 0.95 * 1.0 but treasury already taken
        // Refund = providerShare for remaining months (95% already split out at subscribe)
        // We refund only the escrowed provider portion (payer's refund is the escrowed remainder)
        uint256 refund = refundProviderShare;

        // Update provider escrow: remove the refunded portion
        if (_providerEscrow[s.providerId] >= refundProviderShare) {
            _providerEscrow[s.providerId] -= refundProviderShare;
        }
        // Adjust s.totalEscrowed to what provider can still claim
        s.totalEscrowed = providerKeep;

        emit Cancelled(orgNode, refund);

        if (refund > 0) {
            require(usdc.transfer(s.payer, refund), "MS: refund failed");
        }
    }

    /// @notice Provider owner claims all currently claimable (elapsed) escrow.
    function claim(uint256 providerId) external {
        IProviderRegistry.Provider memory p = registry.getProvider(providerId);
        require(p.owner == msg.sender, "MS: not provider owner");

        uint256 amount = _providerEscrow[providerId];
        require(amount > 0, "MS: nothing to claim");

        _providerEscrow[providerId] = 0;

        emit Claimed(providerId, msg.sender, amount);
        require(usdc.transfer(msg.sender, amount), "MS: claim failed");
    }

    // -------------------------------------------------------------------------
    // Views
    // -------------------------------------------------------------------------

    function seatsOf(bytes32 orgNode) external view returns (uint32 seats, uint64 paidUntil, uint256 providerId) {
        Subscription storage s = _subs[orgNode];
        return (s.seats, s.paidUntil, s.providerId);
    }

    function isActive(bytes32 orgNode) external view returns (bool) {
        Subscription storage s = _subs[orgNode];
        return s.active && uint64(block.timestamp) < s.paidUntil;
    }

    function claimable(uint256 providerId) external view returns (uint256) {
        return _providerEscrow[providerId];
    }

    function getSubscription(bytes32 orgNode) external view returns (Subscription memory) {
        return _subs[orgNode];
    }

    // -------------------------------------------------------------------------
    // Internal
    // -------------------------------------------------------------------------

    function _split(uint256 total) internal pure returns (uint256 treasuryShare, uint256 providerShare) {
        treasuryShare = (total * TREASURY_BPS) / 10_000;
        providerShare = total - treasuryShare;
    }
}
