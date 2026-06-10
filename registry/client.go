// Package registry is a thin client for the on-chain KeeperRegistry: keeper
// registration (staking) and registration checks.
package registry

import (
	"fmt"
	"strings"

	"github.com/stellar/go/keypair"
	"github.com/stellar/go/xdr"

	"github.com/Nectar-Network/keeper-sdk/soroban"
)

// Register registers the keeper with the on-chain KeeperRegistry. Registration
// stakes USDC per the registry's minimum, so it moves operator funds.
// Returns nil if already registered.
func Register(rpc *soroban.Client, horizonURL string, kp *keypair.Full, passphrase, registryAddr, name string) error {
	operatorVal, err := soroban.ScvAddress(kp.Address())
	if err != nil {
		return err
	}
	nameVal := soroban.ScvString(name)

	_, err = rpc.InvokeWithRetry(horizonURL, kp, passphrase, registryAddr, "register",
		soroban.DefaultRetry(), operatorVal, nameVal)
	if err != nil {
		if isAlreadyRegistered(err.Error()) {
			return nil
		}
		return fmt.Errorf("registry register: %w", err)
	}
	return nil
}

// IsRegistered checks whether the keeper address is currently registered. A
// get_keeper call that errors with NotRegistered or returns no value (a void /
// Option::None result) reports false.
func IsRegistered(rpc *soroban.Client, passphrase, registryAddr, addr string) (bool, error) {
	addrVal, err := soroban.ScvAddress(addr)
	if err != nil {
		return false, err
	}
	sim, err := rpc.SimulateRead(passphrase, registryAddr, "get_keeper", addrVal)
	if err != nil {
		return false, fmt.Errorf("get_keeper: %w", err)
	}
	if sim.Error != "" {
		if isNotRegistered(sim.Error) {
			return false, nil
		}
		return false, fmt.Errorf("get_keeper: %s", sim.Error)
	}
	if len(sim.Results) == 0 {
		return false, nil
	}
	var val xdr.ScVal
	if err := xdr.SafeUnmarshalBase64(sim.Results[0].XDR, &val); err != nil {
		return false, fmt.Errorf("get_keeper: decode result: %w", err)
	}
	switch val.Type {
	case xdr.ScValTypeScvVoid:
		return false, nil
	case xdr.ScValTypeScvBool:
		return val.B != nil && bool(*val.B), nil
	}
	return true, nil
}

func isAlreadyRegistered(s string) bool {
	ls := strings.ToLower(s)
	return strings.Contains(ls, "alreadyregistered") || strings.Contains(ls, "already registered")
}

func isNotRegistered(s string) bool {
	ls := strings.ToLower(s)
	return strings.Contains(ls, "notregistered") || strings.Contains(ls, "not registered")
}
