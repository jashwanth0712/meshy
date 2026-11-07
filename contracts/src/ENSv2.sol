// SPDX-License-Identifier: AGPL-3.0
pragma solidity ^0.8.24;

/// Minimal ENSv2 (Sepolia) interfaces used by the Meshy org factory and invites.
/// Addresses and role bits match meshy/enskit (docs.ens.domains/learn/deployments).

interface IVerifiableFactory {
    function deployProxy(address implementation, uint256 salt, bytes calldata data) external returns (address);
}

struct RoleGrant {
    address account;
    uint256 roleBitmap;
}

interface IUserRegistryInit {
    function initialize(RoleGrant[] calldata grants) external;
}

interface IPermissionedResolverInit {
    function initialize(RoleGrant[] calldata grants, bytes[] calldata calls) external;
}

interface IPermissionedRegistry {
    function register(string calldata label, address owner, address registry, address resolver, uint256 roleBitmap, uint64 expiry)
        external
        returns (uint256);
    function getExpiry(uint256 anyId) external view returns (uint64);
    function hasRootRoles(uint256 roleBitmap, address account) external view returns (bool);
    function grantRootRoles(uint256 roleBitmap, address account) external returns (bool);
    function revokeRootRoles(uint256 roleBitmap, address account) external returns (bool);
}

interface IPermissionedResolver {
    function setText(bytes calldata name, string calldata key, string calldata value) external;
    function setAddress(bytes calldata name, uint256 coinType, bytes calldata addressBytes) external;
    function revokeRootRoles(uint256 roleBitmap, address account) external returns (bool);
}

library ENSv2 {
    uint256 internal constant ROLE_REGISTRAR = 1;
    uint256 internal constant ROLE_UNREGISTER = 1 << 12;
    uint256 internal constant ROLE_RENEW = 1 << 16;
    /// Every role and its admin bit (0x1111…1111, 64 nybbles), as enskit.AllRoles.
    uint256 internal constant ALL_ROLES = 0x1111111111111111111111111111111111111111111111111111111111111111;

    /// DNS wire encoding of a dotted name ("a.b.eth" -> 0x01 'a' 0x01 'b' 0x03 'eth' 0x00).
    function dnsEncode(string memory name) internal pure returns (bytes memory out) {
        bytes memory s = bytes(name);
        out = new bytes(s.length + 2);
        uint256 lenPos = 0;
        uint256 n = 0;
        for (uint256 i = 0; i < s.length; i++) {
            if (s[i] == ".") {
                require(n > 0 && n < 64, "ENSv2: bad label");
                out[lenPos] = bytes1(uint8(n));
                lenPos = i + 1;
                n = 0;
            } else {
                out[i + 1] = s[i];
                n++;
            }
        }
        require(n > 0 && n < 64, "ENSv2: bad label");
        out[lenPos] = bytes1(uint8(n));
        out[s.length + 1] = 0x00;
    }

    /// Lowercase a-z, 0-9 and '-', 1..32 chars, no leading/trailing '-'.
    function validLabel(string memory label) internal pure returns (bool) {
        bytes memory b = bytes(label);
        if (b.length == 0 || b.length > 32 || b[0] == "-" || b[b.length - 1] == "-") return false;
        for (uint256 i = 0; i < b.length; i++) {
            bytes1 c = b[i];
            if (!((c >= "a" && c <= "z") || (c >= "0" && c <= "9") || c == "-")) return false;
        }
        return true;
    }

    function labelId(string memory label) internal pure returns (uint256) {
        return uint256(keccak256(bytes(label)));
    }
}
