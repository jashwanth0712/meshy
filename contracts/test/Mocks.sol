// SPDX-License-Identifier: AGPL-3.0-only
pragma solidity ^0.8.24;

/// @dev Mock ERC-20 (USDC) for tests.
contract MockUSDC {
    string public name = "Mock USDC";
    string public symbol = "USDC";
    uint8  public decimals = 6;

    mapping(address => uint256) public balanceOf;
    mapping(address => mapping(address => uint256)) public allowance;

    uint256 public totalSupply;

    event Transfer(address indexed from, address indexed to, uint256 amount);
    event Approval(address indexed owner, address indexed spender, uint256 amount);

    function mint(address to, uint256 amount) external {
        balanceOf[to] += amount;
        totalSupply += amount;
        emit Transfer(address(0), to, amount);
    }

    function approve(address spender, uint256 amount) external returns (bool) {
        allowance[msg.sender][spender] = amount;
        emit Approval(msg.sender, spender, amount);
        return true;
    }

    function transfer(address to, uint256 amount) external returns (bool) {
        return _transfer(msg.sender, to, amount);
    }

    function transferFrom(address from, address to, uint256 amount) external returns (bool) {
        require(allowance[from][msg.sender] >= amount, "ERC20: allowance");
        allowance[from][msg.sender] -= amount;
        return _transfer(from, to, amount);
    }

    function _transfer(address from, address to, uint256 amount) internal returns (bool) {
        require(balanceOf[from] >= amount, "ERC20: balance");
        balanceOf[from] -= amount;
        balanceOf[to] += amount;
        emit Transfer(from, to, amount);
        return true;
    }
}

/// @dev Mock UniversalHelper that can be configured per-test.
contract MockUniversalHelper {
    address public returnReg;
    bool public dead; // if true, returns address(0)

    function setRegistry(address reg) external { returnReg = reg; dead = false; }
    function setDead() external { dead = true; returnReg = address(0); }

    function findParentRegistry(bytes calldata) external view returns (address) {
        if (dead) return address(0);
        return returnReg;
    }
}

/// @dev Mock PermissionedRegistry.
contract MockRegistry {
    mapping(uint256 => uint64) public expiries;
    mapping(uint256 => uint256) public resources;

    function setExpiry(uint256 labelHash, uint64 expiry) external {
        expiries[labelHash] = expiry;
    }

    function setResource(uint256 labelHash, uint256 resource) external {
        resources[labelHash] = resource;
    }

    function getExpiry(uint256 labelHash) external view returns (uint64) {
        return expiries[labelHash];
    }

    function getResource(uint256 labelHash) external view returns (uint256) {
        return resources[labelHash];
    }
}
