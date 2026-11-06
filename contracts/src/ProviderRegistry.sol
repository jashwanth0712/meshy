// SPDX-License-Identifier: AGPL-3.0-only
pragma solidity ^0.8.24;

/// @notice Minimal interface for the ENSv2 UniversalHelper on Sepolia.
/// Address: 0xd453e5bdb62cc3bea84341b1e306319c8ffd7dfe
interface IUniversalHelper {
    /// @notice Finds the innermost live registry that owns the DNS-encoded name.
    /// Returns address(0) if any ancestor in the chain is dead.
    function findParentRegistry(bytes calldata dnsName) external view returns (address);
}

/// @notice Minimal interface for ENSv2 PermissionedRegistry.
interface IPermissionedRegistry {
    /// @notice Returns the expiry timestamp of the label (keccak256 hash as uint256).
    function getExpiry(uint256 labelHash) external view returns (uint64);
    /// @notice Returns the resource id of the label. Changes on re-registration.
    function getResource(uint256 labelHash) external view returns (uint256);
}

/// @title ProviderRegistry
/// @notice Staked provider registry for Meshy with EIP-712 attestation fraud proofs.
/// @dev ENSv2 Sepolia: UniversalHelper 0xd453e5bdb62cc3bea84341b1e306319c8ffd7dfe
contract ProviderRegistry {
    // -------------------------------------------------------------------------
    // Constants & immutables
    // -------------------------------------------------------------------------

    uint256 public constant MIN_STAKE = 0.01 ether;
    address public constant BURN_ADDRESS = 0x000000000000000000000000000000000000dEaD;

    uint256 public immutable UNBOND_DELAY; // seconds; 7 days on mainnet, small for tests
    IUniversalHelper public immutable universalHelper;

    // EIP-712 domain
    bytes32 public immutable DOMAIN_SEPARATOR;
    bytes32 public constant ATTESTATION_TYPEHASH = keccak256(
        "Attestation(uint256 providerId,bytes dnsName,uint256 resource,uint64 issuedAt,uint64 validUntil,bytes32 peerKey)"
    );

    // -------------------------------------------------------------------------
    // Types
    // -------------------------------------------------------------------------

    enum ProviderStatus { Active, Unbonding, Slashed, Withdrawn }

    struct Provider {
        address owner;
        address signer;        // Signs admission & World attestations
        string endpoint;       // e.g. "https://provider.meshy.local"
        uint256 pricePerSeatPerMonth; // in USDC 6-decimal units
        uint256 stake;
        uint256 unbondTime;    // timestamp when unbond was requested
        ProviderStatus status;
        string metadataURI;
    }

    struct Attestation {
        uint256 providerId;
        bytes   dnsName;       // DNS-wire-encoded name, e.g. db1.prod.meshydemo.eth
        uint256 resource;      // getResource(labelhash) at grant time
        uint64  issuedAt;      // block.timestamp when provider attested
        uint64  validUntil;
        bytes32 peerKey;
    }

    // -------------------------------------------------------------------------
    // State
    // -------------------------------------------------------------------------

    uint256 public providerCount;
    mapping(uint256 => Provider) private _providers;
    mapping(bytes32 => bool) private _slashedAttestations; // prevent double-slash

    // -------------------------------------------------------------------------
    // Events
    // -------------------------------------------------------------------------

    event Registered(uint256 indexed providerId, address indexed owner, string endpoint);
    event Updated(uint256 indexed providerId);
    event StakeAdded(uint256 indexed providerId, uint256 added, uint256 total);
    event UnbondRequested(uint256 indexed providerId, uint256 unbondTime);
    event Withdrawn(uint256 indexed providerId, address indexed to, uint256 amount);
    event Slashed(uint256 indexed providerId, address indexed reporter, uint256 amount, string reason);

    // -------------------------------------------------------------------------
    // Constructor
    // -------------------------------------------------------------------------

    constructor(address _universalHelper, uint256 _unbondDelay) {
        universalHelper = IUniversalHelper(_universalHelper);
        UNBOND_DELAY = _unbondDelay;

        DOMAIN_SEPARATOR = keccak256(abi.encode(
            keccak256("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"),
            keccak256("ProviderRegistry"),
            keccak256("1"),
            block.chainid,
            address(this)
        ));
    }

    // -------------------------------------------------------------------------
    // Registration
    // -------------------------------------------------------------------------

    /// @notice Register a new provider. Requires at least MIN_STAKE in msg.value.
    function register(
        string calldata endpoint,
        address signer,
        uint256 pricePerSeatPerMonth,
        string calldata metadataURI
    ) external payable returns (uint256 providerId) {
        require(msg.value >= MIN_STAKE, "PR: insufficient stake");
        require(signer != address(0), "PR: zero signer");

        providerId = ++providerCount;
        _providers[providerId] = Provider({
            owner: msg.sender,
            signer: signer,
            endpoint: endpoint,
            pricePerSeatPerMonth: pricePerSeatPerMonth,
            stake: msg.value,
            unbondTime: 0,
            status: ProviderStatus.Active,
            metadataURI: metadataURI
        });

        emit Registered(providerId, msg.sender, endpoint);
    }

    /// @notice Update provider metadata. Only owner.
    function update(
        uint256 providerId,
        string calldata endpoint,
        address signer,
        uint256 pricePerSeatPerMonth,
        string calldata metadataURI
    ) external {
        Provider storage p = _requireOwner(providerId);
        require(signer != address(0), "PR: zero signer");
        p.endpoint = endpoint;
        p.signer = signer;
        p.pricePerSeatPerMonth = pricePerSeatPerMonth;
        p.metadataURI = metadataURI;
        emit Updated(providerId);
    }

    /// @notice Add more stake to a provider.
    function addStake(uint256 providerId) external payable {
        Provider storage p = _requireOwner(providerId);
        require(p.status == ProviderStatus.Active, "PR: not active");
        require(msg.value > 0, "PR: zero value");
        p.stake += msg.value;
        emit StakeAdded(providerId, msg.value, p.stake);
    }

    /// @notice Start unbonding. Must wait UNBOND_DELAY before withdraw.
    function requestUnbond(uint256 providerId) external {
        Provider storage p = _requireOwner(providerId);
        require(p.status == ProviderStatus.Active, "PR: not active");
        p.status = ProviderStatus.Unbonding;
        p.unbondTime = block.timestamp;
        emit UnbondRequested(providerId, p.unbondTime);
    }

    /// @notice Withdraw stake after UNBOND_DELAY. Only when Unbonding.
    function withdraw(uint256 providerId) external {
        Provider storage p = _requireOwner(providerId);
        require(p.status == ProviderStatus.Unbonding, "PR: not unbonding");
        require(block.timestamp >= p.unbondTime + UNBOND_DELAY, "PR: delay not elapsed");

        uint256 amount = p.stake;
        p.stake = 0;
        p.status = ProviderStatus.Withdrawn;

        emit Withdrawn(providerId, msg.sender, amount);
        (bool ok,) = msg.sender.call{value: amount}("");
        require(ok, "PR: transfer failed");
    }

    // -------------------------------------------------------------------------
    // Slashing (EIP-712 fraud proof)
    // -------------------------------------------------------------------------

    /// @notice Slash a provider that attested access to a name which was already dead
    ///         at issuedAt. Reporter gets 50 %; rest is burned.
    ///
    /// Fraud conditions (any one is sufficient):
    ///   1. Ancestor dead: UniversalHelper.findParentRegistry(dnsName) == address(0)
    ///   2. Name expired at issuedAt: getExpiry(labelhash) <= a.issuedAt
    ///   3. Resource changed (re-registered/unregistered then re-registered):
    ///      getResource(labelhash) != a.resource
    ///
    /// LIMITATION: If the name was re-registered with the SAME resource id before
    /// on-chain verification, condition 3 would not fire. In practice ENSv2
    /// increments the resource on every register call, so collisions are extremely
    /// unlikely. Additionally, a valid renewal after expiry would mask fraud under
    /// condition 2 (the name appears live again). Monitors should slash promptly.
    function slash(Attestation calldata a, bytes calldata sig) external {
        // Verify the attestation was signed by the provider's signer
        bytes32 digest = _hashAttestation(a);
        require(!_slashedAttestations[digest], "PR: already slashed");

        Provider storage p = _providers[a.providerId];
        require(p.status == ProviderStatus.Active || p.status == ProviderStatus.Unbonding, "PR: not slashable");
        require(p.stake > 0, "PR: no stake");

        address recovered = _ecrecover(digest, sig);
        require(recovered == p.signer, "PR: bad signature");

        // Check fraud conditions using ENSv2 on-chain state
        (bool fraud, string memory reason) = _isFraud(a);
        require(fraud, "PR: no fraud detected");

        // Mark as slashed before external calls
        _slashedAttestations[digest] = true;
        p.status = ProviderStatus.Slashed;

        uint256 total = p.stake;
        p.stake = 0;

        uint256 reporterShare = total / 2;
        uint256 burnShare = total - reporterShare;

        emit Slashed(a.providerId, msg.sender, total, reason);

        (bool ok1,) = msg.sender.call{value: reporterShare}("");
        require(ok1, "PR: reporter transfer failed");
        (bool ok2,) = BURN_ADDRESS.call{value: burnShare}("");
        require(ok2, "PR: burn transfer failed");
    }

    // -------------------------------------------------------------------------
    // Views
    // -------------------------------------------------------------------------

    function getProvider(uint256 providerId) external view returns (Provider memory) {
        return _providers[providerId];
    }

    function isActive(uint256 providerId) external view returns (bool) {
        return _providers[providerId].status == ProviderStatus.Active;
    }

    // -------------------------------------------------------------------------
    // Internal helpers
    // -------------------------------------------------------------------------

    function _requireOwner(uint256 providerId) internal view returns (Provider storage p) {
        p = _providers[providerId];
        require(p.owner == msg.sender, "PR: not owner");
    }

    /// @dev Returns true + reason if the attestation is fraudulent.
    function _isFraud(Attestation calldata a) internal view returns (bool, string memory) {
        address reg = universalHelper.findParentRegistry(a.dnsName);
        if (reg == address(0)) {
            return (true, "ancestor dead");
        }

        // Extract the first DNS label (length byte + label bytes)
        bytes memory label = _firstLabel(a.dnsName);
        uint256 labelHash = uint256(keccak256(label));

        uint64 exp = IPermissionedRegistry(reg).getExpiry(labelHash);
        if (exp <= a.issuedAt) {
            return (true, "expired at issuance");
        }

        uint256 res = IPermissionedRegistry(reg).getResource(labelHash);
        if (res != a.resource) {
            return (true, "resource mismatch (re-registered)");
        }

        return (false, "");
    }

    /// @dev Extract the raw label bytes (without length prefix) from DNS wire format.
    ///      DNS wire: <len><label><len><label>...<0x00>
    function _firstLabel(bytes calldata dnsName) internal pure returns (bytes memory) {
        require(dnsName.length > 0, "PR: empty dnsName");
        uint8 len = uint8(dnsName[0]);
        require(len > 0 && dnsName.length > len, "PR: malformed dnsName");
        return bytes(dnsName[1:1 + len]);
    }

    function _hashAttestation(Attestation calldata a) internal view returns (bytes32) {
        return keccak256(abi.encodePacked(
            "\x19\x01",
            DOMAIN_SEPARATOR,
            keccak256(abi.encode(
                ATTESTATION_TYPEHASH,
                a.providerId,
                keccak256(a.dnsName),
                a.resource,
                a.issuedAt,
                a.validUntil,
                a.peerKey
            ))
        ));
    }

    function _ecrecover(bytes32 digest, bytes calldata sig) internal pure returns (address) {
        require(sig.length == 65, "PR: bad sig length");
        bytes32 r;
        bytes32 s;
        uint8 v;
        assembly {
            r := calldataload(sig.offset)
            s := calldataload(add(sig.offset, 32))
            v := byte(0, calldataload(add(sig.offset, 64)))
        }
        if (v < 27) v += 27;
        require(v == 27 || v == 28, "PR: bad v");
        address addr = ecrecover(digest, v, r, s);
        require(addr != address(0), "PR: ecrecover failed");
        return addr;
    }

    // Allow receiving ETH for stake refunds
    receive() external payable {}
}
